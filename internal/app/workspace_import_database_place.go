package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A file import can go into a database, each file its row, as Notion's file
// windows (Text & Markdown, HTML, PDF, Word, Zip) list the databases under
// "데이터베이스" beside the pages (seen 2026-11-15; its CSV window doesn't).
// The import's page is made, filled and published as any other, at the top
// of the importer's private pages, and then becomes the database's row by
// the same change that moves a page into a database (page_row_conversion.go),
// after its last row. That change is a change to the content graph of the
// page holding the database, so it goes outside the import's own
// transactions: a batch's top pages as soon as they are made, before their
// text goes in, and a single import's page once it is published. A database
// that went, or that the importer can no longer add rows to, leaves the page
// where it was made, as an import whose parent page went does.

// authorizeWorkspaceImportDataSource checks a database an import asks to go
// into: a live data source of the workspace that takes added rows
// (database_added_rows.go) and whose page the importer may change, as a
// CSV's rows joining it are checked, and gives that page. A database whose
// page is in the trash, itself or under a page that is, or deleted for good
// is refused as that page (workspaceImportTargetGone) to one who may change
// it.
func authorizeWorkspaceImportDataSource(app core.App, ownerID, workspaceID, dataSourceID string) (string, error) {
	var source struct {
		PageID      string `db:"page_id"`
		WorkspaceID string `db:"workspace_id"`
	}
	err := app.DB().NewQuery(`
		SELECT [[page_id]], [[workspace_id]] FROM {{leafen_data_sources}}
		WHERE [[id]] = {:id} AND ` + databaseSourceNotTrashedSQL("{:id}") + `
			AND ` + databaseSourceTakesAddedRowsSQL("{:id}") + `
	`).Bind(dbx.Params{"id": dataSourceID}).One(&source)
	if errors.Is(err, sql.ErrNoRows) {
		return "", permissionDenied()
	}
	if err != nil {
		return "", err
	}
	if source.WorkspaceID != workspaceID {
		return "", permissionDenied()
	}
	container, err := authorizePageBody(app, ownerID, source.PageID, permissionWorkspaceWrite)
	if err != nil {
		return "", err
	}
	if container.Workspace.WorkspaceID != workspaceID {
		return "", permissionDenied()
	}
	if gone := workspaceImportTargetGone(app, source.PageID); gone != nil {
		return "", gone
	}
	if container.Page.GetBool("notion_import_staging") {
		return "", permissionDenied()
	}
	return source.PageID, nil
}

// turnWorkspaceImportPageIntoRow makes an import's page a row of the
// database it was asked to go into; a page that is a row already (a turn
// done again) stays as it is.
func (r *durableJobRunner) turnWorkspaceImportPageIntoRow(ctx context.Context, ownerID, workspaceID, dataSourceID, pageID string) error {
	if r.ygo == nil || dataSourceID == "" || pageID == "" {
		return nil
	}
	var rowOf string
	err := r.app.DB().NewQuery(`SELECT [[data_source_id]] FROM {{leafen_pages}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": pageID}).Row(&rowOf)
	if errors.Is(err, sql.ErrNoRows) || err == nil && rowOf != "" {
		return nil
	}
	if err != nil {
		return err
	}
	containerPageID, err := authorizeWorkspaceImportDataSource(r.app, ownerID, workspaceID, dataSourceID)
	if workspaceImportParentLost(err) {
		return nil
	}
	if err != nil {
		return err
	}
	digest := sha256.Sum256([]byte("workspace-import-row\x00" + pageID + "\x00" + dataSourceID))
	_, err = r.ygo.convertPageToDatabaseRow(ctx, pageToDatabaseRowRequest{
		PrincipalID: ownerID, PageID: pageID, DataSourceID: dataSourceID, ContainerPageID: containerPageID,
		MutationID: "workspace-import-row:" + pageID, RequestHash: hex.EncodeToString(digest[:]),
	})
	if err != nil && ctx.Err() == nil {
		// The database refused it (it changed meanwhile): the page stays a page.
		r.app.Logger().Warn("workspace import page not made a database row", "pageId", pageID, "dataSourceId", dataSourceID, "error", err)
		return nil
	}
	return err
}

// settleWorkspaceImportBatchPage finishes a page a batch just made: a top
// page of a batch asked to go into a database becomes its row; any other
// joins the wiki it was made under, as making a page does (page_creation.go).
func (r *durableJobRunner) settleWorkspaceImportBatchPage(
	ctx context.Context, job storedJob, payload workspaceImportJobPayload, row workspaceImportBatchPageRow,
) error {
	if payload.DataSourceID != "" && row.ParentPosition < 0 {
		return r.turnWorkspaceImportPageIntoRow(ctx, job.Owner, job.WorkspaceID, payload.DataSourceID, row.PageID)
	}
	_, err := r.ygo.adoptPageTreeIntoWiki(ctx, job.Owner, row.PageID)
	return err
}

// workspaceImportBatchRowSource is the database a batch's top file goes into
// as its row: a file the batch imports on its own (its own page, made and
// published by its import) rather than a page the batch made first.
func workspaceImportBatchRowSource(payload workspaceImportJobPayload, item storedWorkspaceImportBatchItem) string {
	if item.ParentPosition >= 0 || item.Kind == workspaceImportBatchItemKindDatabase || item.Format != workspaceImportFormatMarkdown {
		return ""
	}
	return payload.DataSourceID
}

// cleanupPublishedWorkspaceImportIntoRow turns the databases a published
// import's page holds inline there (workspace_import_inline_databases.go),
// makes the page the row of the database it was asked to go into, then
// cleans up as any published import does.
func (r *durableJobRunner) cleanupPublishedWorkspaceImportIntoRow(ctx context.Context, job storedJob, payload workspaceImportJobPayload) error {
	if len(payload.InlineDatabases) > 0 && r.ygo != nil {
		return r.turnWorkspaceImportDatabasesInline(ctx, job, payload)
	}
	if payload.DataSourceID != "" && !payload.AppendToExistingPage && !payload.ReplaceExistingPage &&
		payload.Format == workspaceImportFormatMarkdown {
		if err := r.turnWorkspaceImportPageIntoRow(
			ctx, job.Owner, job.WorkspaceID, payload.DataSourceID, workspaceImportPrimaryRootPageID(payload),
		); err != nil {
			return err
		}
	}
	return r.cleanupPublishedWorkspaceImport(job, payload)
}
