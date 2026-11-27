package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A CSV import made before 2026-10-01 left its database on its page with no
// block to show it, so the page looked empty; the import has made the page
// the database since. No data a Leafen opens holds such a database (the
// schema baseline), but a workspace archive of that time (format 10) still
// brings one in as it was. Once an archive import is published the job
// runner gives each such database its block: a page holding nothing else
// becomes its full-page database, as the import now makes, and any other
// page shows it as an inline database at the end of its body. The import's
// databases are known by the place it gave their first view. A database it
// can't fill waits for the next archive import.

const (
	databaseBlockBackfillWindow   = 16
	databaseBlockBackfillViewMark = "import:00000000"
)

type databaseBlockBackfillScan struct {
	pending bool
	failed  map[string]struct{}
}

type unshownImportedDatabase struct {
	DataSourceID string `db:"data_source_id"`
	PageID       string `db:"page_id"`
	OwnerID      string `db:"owner"`
	ViewID       string `db:"view_id"`
}

func (r *durableJobRunner) scanDatabaseBlockBackfill(ctx context.Context, scan *databaseBlockBackfillScan) {
	if r.databaseBlocksRequested.Swap(false) {
		scan.pending = true
	}
	if !scan.pending || r.ygo == nil {
		return
	}
	databases, err := loadUnshownImportedDatabases(ctx, r.app, databaseBlockBackfillWindow+len(scan.failed))
	if err != nil {
		scan.pending = false
		if ctx.Err() == nil {
			logDurableJobDiagnostic(r.app, errorCodeJobRunnerFailed, newDiagnosticReference(), "", "database.blocks", "scan", err)
		}
		return
	}
	filled := 0
	for _, database := range databases {
		if _, failed := scan.failed[database.DataSourceID]; failed {
			continue
		}
		if filled == databaseBlockBackfillWindow || ctx.Err() != nil {
			r.notify()
			return
		}
		filled++
		if err := r.fillDatabaseBlock(ctx, database); err != nil {
			scan.failed[database.DataSourceID] = struct{}{}
			if ctx.Err() == nil {
				logDurableJobDiagnostic(r.app, errorCodeJobRunnerFailed, newDiagnosticReference(), "", "database.blocks", "fill", err)
			}
		}
	}
	scan.pending = false
}

// notifyWorkspaceArchiveImportBackfills asks, once an archive import's pages
// are committed, for their bodies' sub-page blocks and for the blocks of
// databases the archive brought with none.
func notifyWorkspaceArchiveImportBackfills(app core.App) {
	notifyChildPageBlockBackfillAfterCommit(app)
	request := func() {
		if runner, ok := app.Store().Get(durableJobRunnerStoreKey).(*durableJobRunner); ok {
			runner.databaseBlocksRequested.Store(true)
			runner.notify()
		}
	}
	if transaction := app.TxInfo(); transaction != nil {
		transaction.OnComplete(func(err error) error {
			if err == nil {
				request()
			}
			return nil
		})
		return
	}
	request()
}

// loadUnshownImportedDatabases are the databases a CSV import left on live
// pages with no block to show them. The look can read every data source, so it
// takes the runner's ctx and ends when the server stops.
func loadUnshownImportedDatabases(ctx context.Context, app core.App, limit int) ([]unshownImportedDatabase, error) {
	databases := []unshownImportedDatabase{}
	err := app.DB().NewQuery(`
		SELECT [[source]].[[id]] AS [[data_source_id]], [[source]].[[page_id]], [[page]].[[owner]],
			[[view]].[[id]] AS [[view_id]]
		FROM {{leafen_data_sources}} AS [[source]]
		JOIN {{leafen_pages}} AS [[page]] ON [[page]].[[id]] = [[source]].[[page_id]]
		JOIN {{leafen_database_views}} AS [[view]] ON [[view]].[[data_source_id]] = [[source]].[[id]]
			AND [[view]].[[page_id]] = [[source]].[[page_id]] AND [[view]].[[position]] = {:mark}
		WHERE [[page]].[[data_source_id]] = '' AND [[page]].[[is_wiki]] = FALSE
			AND [[page]].[[` + databaseTemplatePageField + `]] = FALSE
			AND [[page]].[[notion_import_staging]] = FALSE AND [[page]].[[in_trash]] = FALSE
			AND NOT EXISTS (
				SELECT 1 FROM {{leafen_database_container_sources}} AS [[member]]
				WHERE [[member]].[[data_source_id]] = [[source]].[[id]]
			)
			AND NOT EXISTS (
				SELECT 1 FROM {{leafen_database_mutation_jobs}} AS [[deletion]]
				WHERE [[deletion]].[[target_kind]] = 'source' AND [[deletion]].[[target_id]] = [[source]].[[id]]
					AND [[deletion]].[[status]] = 'pending'
			)
		ORDER BY [[source]].[[created]], [[source]].[[id]] LIMIT {:limit}
	`).WithContext(ctx).Bind(dbx.Params{"mark": databaseBlockBackfillViewMark, "limit": limit}).All(&databases)
	return databases, err
}

// fillDatabaseBlock gives a database its block on its page, as the system
// (system_job_access.go) in the page's owner's name.
func (r *durableJobRunner) fillDatabaseBlock(ctx context.Context, database unshownImportedDatabase) error {
	content, err := json.Marshal(map[string]any{"dataSourceId": database.DataSourceID, "viewId": database.ViewID})
	if err != nil {
		return err
	}
	block := graphBlock{
		ID: "imported-database-" + database.DataSourceID, Position: "p0000000000",
		Type: graphBlockTypeInlineDatabase, Content: content,
	}
	input := graphMutationInput{
		MutationID: "database-block-backfill:" + database.DataSourceID,
		Operations: []graphOperation{{Kind: graphOperationKindUpsertBlock, Block: &block}},
	}
	hooks := contentGraphMutationHooks{
		Authorize: func(txApp core.App) (pageAuthorization, error) {
			page, err := txApp.FindRecordById(pagesCollection, database.PageID)
			if err != nil {
				return pageAuthorization{}, err
			}
			workspace, err := txApp.FindRecordById(workspacesCollection, page.GetString("workspace"))
			if err != nil {
				return pageAuthorization{}, err
			}
			return systemJobSubjectFor(workspace).authorizePageBody(txApp, database.PageID, permissionWorkspaceWrite)
		},
		SuppressNotifications:    true,
		SuppressAutomationEvents: true,
	}
	alone, err := databaseBlockBackfillPageEmpty(r.app, database)
	if err != nil {
		return err
	}
	if alone {
		kind := pageKindDatabase
		input.PageKind = &kind
	} else {
		// Any other page shows it after what its body holds.
		hooks.StructuralAppend = true
		hooks.Prepare = func(txApp core.App, _ pageAuthorization, doc *crdt.Doc) (contentGraphMutationPreparation, error) {
			appended := block
			if order := doc.GetArray(blockOrderRoot("")); order.Len() > 0 {
				tail, ok := order.Get(order.Len() - 1).(string)
				if !ok || tail == "" {
					return contentGraphMutationPreparation{}, graphStructureAuthorityError()
				}
				appended.AfterID = &tail
			}
			return contentGraphMutationPreparation{
				Operations: []graphOperation{{Kind: graphOperationKindUpsertBlock, Block: &appended}},
			}, nil
		}
	}
	_, err = r.ygo.applyContentGraphMutationContext(ctx, database.OwnerID, database.PageID, input, hooks)
	return err
}

// databaseBlockBackfillPageEmpty reports whether a page holds only the
// database: no blocks, no sub-pages, no other database and no wiki above it,
// so it can become the database's full page.
func databaseBlockBackfillPageEmpty(app core.App, database unshownImportedDatabase) (bool, error) {
	var blocks, pages, sources int
	err := app.DB().NewQuery(`
		SELECT
			(SELECT COUNT(*) FROM {{leafen_blocks}} WHERE [[page_id]] = {:pageId} AND [[owner]] = {:owner}),
			(SELECT COUNT(*) FROM {{leafen_pages}} WHERE [[parent_id]] = {:pageId} AND [[data_source_id]] = ''),
			(SELECT COUNT(*) FROM {{leafen_data_sources}} WHERE [[page_id]] = {:pageId})
	`).Bind(dbx.Params{"pageId": database.PageID, "owner": database.OwnerID}).Row(&blocks, &pages, &sources)
	if err != nil {
		return false, err
	}
	if blocks > 0 || pages > 0 || sources != 1 {
		return false, nil
	}
	within, err := pageWithinWiki(app, database.PageID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil && !within, err
}
