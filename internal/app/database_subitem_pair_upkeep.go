package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// Data sources whose sub-items are on without their Parent item and
// Sub-items get them through the job runner: a look when it starts, each
// minute after while any is left, and again when asked — as sub-items go on
// while the pair removed when they last went off is still being deleted,
// which ensureDatabaseSubitemPair leaves to this look, and as an import
// brings sub-items on without their pair, as an archive from before
// sub-items became a relation carries (publishWorkspaceArchiveTaskSettings).
// Each such source gets its pair, and each row with a sub-item parent and no
// Parent item — nested while the source had no pair — gets that parent as
// its Parent item, which relates the parent back. A row in the trash keeps
// its parent until it is restored and changed. A source on a page in the
// trash waits for the page to come back, the look going on each minute
// meanwhile, while the other sources move. The same look binds dependencies
// (database_dependency_pair_upkeep.go), and looks again when a pair is
// removed, until the removal is done. Each of those minutes reads only the
// sources with something left to move: one found with none is passed by
// until the look is asked for again or the server starts, so a source
// waiting in the trash for weeks never has every other source's rows read
// again each minute. Its reads and writes take the runner's context and it
// looks at the context between sources, so a stopping server ends it at
// once (jobs_scheduler_scans.go).

const (
	databaseSubitemMigrationSources = 8
	databaseSubitemMigrationRows    = 32
)

// requestDatabaseTaskPairsAfterCommit has the job runner look for sources
// waiting for their pair once the change is committed.
func requestDatabaseTaskPairsAfterCommit(app core.App) {
	request := func() {
		if runner, ok := app.Store().Get(durableJobRunnerStoreKey).(*durableJobRunner); ok {
			runner.taskPairsRequested.Store(true)
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

type databaseSubitemPairScan struct {
	pending bool
	next    time.Time
	// settled are the sources with a pair found with no sub-item left to
	// move since the look was last asked for.
	settled map[string]struct{}
}

func (r *durableJobRunner) scanDatabaseSubitemPairs(ctx context.Context, now time.Time, scan *databaseSubitemPairScan) {
	if r.taskPairsRequested.Swap(false) && r.ygo != nil {
		// What asked may have left sub-items to move in any source.
		scan.pending, scan.next, scan.settled = true, time.Time{}, nil
	}
	if !scan.pending || now.Before(scan.next) {
		return
	}
	if scan.settled == nil {
		scan.settled = map[string]struct{}{}
	}
	pending, err := r.ygo.migrateDatabaseSubitemPairsPassing(ctx, scan.settled)
	if err == nil {
		var more bool
		more, err = r.ygo.migrateDatabaseDependencyPairs(ctx)
		pending = pending || more
	}
	if err == nil {
		// A feature turned on again while its pair is still being deleted
		// gets a new pair once the deletion is done.
		var removing bool
		removing, err = databasePairRemovalsRunning(ctx, r.app)
		pending = pending || removing
	}
	scan.pending, scan.next = pending || err != nil, now.Add(time.Minute)
	if !scan.pending {
		scan.settled = nil
	}
	if err != nil && ctx.Err() == nil {
		logDurableJobDiagnostic(
			r.app, errorCodeJobRunnerFailed, newDiagnosticReference(), "", "database.subitems", "migrate", err,
		)
	}
}

type databaseSubitemMigrationSource struct {
	Source    string `db:"data_source_id"`
	Page      string `db:"page_id"`
	Workspace string `db:"workspace_id"`
	Owner     string `db:"owner"`
	Parent    string `db:"parent"`
}

// migrateDatabaseSubitemPairs takes a few steps of the move and reports
// whether any is left.
func (p *pocketBaseYgoPersistence) migrateDatabaseSubitemPairs(ctx context.Context) (bool, error) {
	return p.migrateDatabaseSubitemPairsPassing(ctx, map[string]struct{}{})
}

// migrateDatabaseSubitemPairsPassing takes a few steps of the move, passing
// by the settled sources, and adds to them each source it finds with
// nothing left to move: a step reads the rows of the sources that still
// have some, at most a window of each of a few that move.
func (p *pocketBaseYgoPersistence) migrateDatabaseSubitemPairsPassing(ctx context.Context, settled map[string]struct{}) (bool, error) {
	// A wiki's pages keep their places without the pair made for them before
	// (wiki_subpage_pair.go), which goes first, so none of it moves here.
	waiting, err := p.dropWikiNestingPairs(ctx)
	if err != nil {
		return true, err
	}
	made, held, err := p.pairDatabaseTaskSources(ctx, `
		SELECT [[data_source_id]], [[page_id]], [[workspace_id]], [[owner]], '' AS [[parent]]
		FROM {{leafen_database_task_features}}
		WHERE json_extract([[subitems_json]], '$.enabled') = 1 AND json_type([[subitems_json]], '$.parentPropertyId') IS NULL
			AND [[data_source_id]] > {:after}
		ORDER BY [[data_source_id]] LIMIT {:limit}
	`, func(source databaseSubitemMigrationSource, actorID string, authorize func(core.App) (pageAuthorization, error)) error {
		return p.ensureDatabaseSubitemPair(ctx, actorID, source.Page, source.Source, authorize)
	})
	if err != nil {
		return true, err
	}
	waiting = waiting || held
	paired := []databaseSubitemMigrationSource{}
	if err := p.app.DB().NewQuery(`
		SELECT [[data_source_id]], [[page_id]], [[workspace_id]], [[owner]],
			json_extract([[subitems_json]], '$.parentPropertyId') AS [[parent]]
		FROM {{leafen_database_task_features}}
		WHERE json_type([[subitems_json]], '$.parentPropertyId') = 'text'
		ORDER BY [[data_source_id]]
	`).WithContext(ctx).All(&paired); err != nil {
		return true, err
	}
	windows := 0
	for _, source := range paired {
		if windows == databaseSubitemMigrationSources {
			return true, nil
		}
		if err := ctx.Err(); err != nil {
			return true, err
		}
		if _, done := settled[source.Source]; done {
			continue
		}
		// Old sub-items move into the pair made for them only: a relation the
		// person chose as the sub-items' is never given parents as values no one set.
		if source.Parent != databaseSubitemPairIDs(source.Source).Parent {
			continue
		}
		moved, held, err := p.migrateDatabaseSubitemWindow(ctx, source)
		if err != nil {
			return true, err
		}
		if moved {
			windows++
		} else if !held {
			settled[source.Source] = struct{}{}
		}
		waiting = waiting || held
	}
	return made > 0 || windows > 0 || waiting, nil
}

// pairDatabaseTaskSources gives the sources a query names their pair, a few
// in the order of their IDs: the query reads on after {:after}, {:limit} at
// a time. A source on a page in the trash waits without taking one of the
// few places, so however many wait there, the sources after them are paired.
// It reports how many it paired and whether any waits.
func (p *pocketBaseYgoPersistence) pairDatabaseTaskSources(
	ctx context.Context,
	query string,
	pair func(source databaseSubitemMigrationSource, actorID string, authorize func(core.App) (pageAuthorization, error)) error,
) (int, bool, error) {
	made, waiting, after := 0, false, ""
	for made < databaseSubitemMigrationSources {
		sources := []databaseSubitemMigrationSource{}
		if err := p.app.DB().NewQuery(query).WithContext(ctx).
			Bind(dbx.Params{"after": after, "limit": databaseSubitemMigrationSources}).All(&sources); err != nil {
			return made, waiting, err
		}
		for _, source := range sources {
			if made == databaseSubitemMigrationSources {
				break
			}
			if err := ctx.Err(); err != nil {
				return made, waiting, err
			}
			after = source.Source
			if trashed, err := pageInTrashPath(p.app, source.Page); err != nil {
				return made, waiting, err
			} else if trashed {
				waiting = true
				continue
			}
			actorID, authorize, err := databaseSubitemMigrationActor(p.app, source)
			if err != nil {
				return made, waiting, err
			}
			if err := pair(source, actorID, authorize); err != nil {
				return made, waiting, err
			}
			made++
		}
		if len(sources) < databaseSubitemMigrationSources {
			break
		}
	}
	return made, waiting, nil
}

// databaseSubitemMigrationActor moves a source as its workspace's permission
// owner, whatever pages that person can open.
func databaseSubitemMigrationActor(app core.App, source databaseSubitemMigrationSource) (
	string, func(core.App) (pageAuthorization, error), error,
) {
	workspace, err := app.FindRecordById(workspacesCollection, source.Workspace)
	if err != nil {
		return "", nil, err
	}
	actorID := workspacePermissionOwnerID(workspace)
	return actorID, func(txApp core.App) (pageAuthorization, error) {
		authorization, err := authorizeWorkspace(txApp, actorID, source.Workspace, permissionWorkspaceWrite)
		if err != nil {
			return pageAuthorization{}, err
		}
		page, err := txApp.FindRecordById(pagesCollection, source.Page)
		if err != nil || page.GetString("workspace") != source.Workspace {
			return pageAuthorization{}, refusedUnlessStorage(errWikiConflict, err)
		}
		return pageAuthorization{Workspace: authorization, Page: page}, nil
	}, nil
}

// migrateDatabaseSubitemWindow gives a window of the source's live rows with
// a parent that parent as their Parent item, and reports whether it did, or
// whether the rows wait for the source's page to leave the trash. Each row's
// Parent item is looked for by the row and the property, its key; the owner
// is read after it is found (+), or SQLite walks every relation item of the
// owner for each row (2026-11-20, 100 sources of 1,000 rows: 257 million
// steps for one source's look, 31 thousand with the key).
func (p *pocketBaseYgoPersistence) migrateDatabaseSubitemWindow(ctx context.Context, source databaseSubitemMigrationSource) (bool, bool, error) {
	rows := []struct {
		ID       string `db:"id"`
		Position string `db:"position"`
		Parent   string `db:"subitem_parent_id"`
	}{}
	if err := p.app.DB().NewQuery(`
		SELECT [[row]].[[id]], [[row]].[[position]], [[row]].[[subitem_parent_id]] FROM {{leafen_database_rows}} AS [[row]]
		WHERE [[row]].[[owner]] = {:owner} AND [[row]].[[page_id]] = {:page} AND [[row]].[[data_source_id]] = {:source}
			AND COALESCE([[row]].[[subitem_parent_id]], '') != ''
			AND NOT EXISTS (SELECT 1 FROM {{leafen_database_row_trash}} WHERE [[row_id]] = [[row]].[[id]])
			AND NOT EXISTS (SELECT 1 FROM {{leafen_database_relation_items}} AS [[item]]
				WHERE +[[item]].[[owner]] = {:owner} AND [[item]].[[row_id]] = [[row]].[[id]] AND [[item]].[[property_id]] = {:parent})
		ORDER BY [[row]].[[id]] LIMIT {:limit}
	`).WithContext(ctx).Bind(dbx.Params{
		"owner": source.Owner, "page": source.Page, "source": source.Source, "parent": source.Parent,
		"limit": databaseSubitemMigrationRows,
	}).All(&rows); err != nil || len(rows) == 0 {
		return false, false, err
	}
	if trashed, err := pageInTrashPath(p.app, source.Page); err != nil || trashed {
		return false, trashed, err
	}
	actorID, authorize, err := databaseSubitemMigrationActor(p.app, source)
	if err != nil {
		return false, false, err
	}
	operations := make([]graphOperation, 0, len(rows))
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		parentID := row.Parent
		value, err := json.Marshal([]string{parentID})
		if err != nil {
			return false, false, err
		}
		operations = append(operations, graphOperation{Kind: graphOperationKindUpsertRow, Row: &graphDatabaseRow{
			ID: row.ID, DataSourceID: source.Source, SubitemParentID: &parentID, Position: row.Position,
			Values: map[string]graphRowValue{source.Parent: {Type: graphValueTypeRelation, Value: value}},
		}})
		ids = append(ids, row.ID)
	}
	digest := sha256.Sum256([]byte("leafen:subitem-migration\x00" + source.Source + "\x00" + strings.Join(ids, "\x00")))
	_, err = p.applyContentGraphMutationContext(ctx, actorID, source.Page,
		graphMutationInput{MutationID: "subitem-migration:" + source.Source + ":" + ids[0]},
		contentGraphMutationHooks{
			// The move is Leafen's own upkeep, no one's act, as a migration's
			// change (recordNativeMigrationGraphMutation).
			Authorize: authorize, SuppressAutomationEvents: true, SuppressNotifications: true,
			PreserveRowEditStamps: true, LeafenOwn: true,
			ResolveRequestHash: func(core.App, pageAuthorization, *crdt.Doc) (string, error) {
				return hex.EncodeToString(digest[:]), nil
			},
			Prepare: func(core.App, pageAuthorization, *crdt.Doc) (contentGraphMutationPreparation, error) {
				return contentGraphMutationPreparation{Operations: operations}, nil
			},
		})
	return err == nil, false, err
}
