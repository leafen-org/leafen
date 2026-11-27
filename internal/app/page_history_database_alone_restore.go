package app

import (
	"database/sql"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A restore with databaseId takes back one database a page holds as a
// version had it — its views and, for one it holds its data sources itself,
// its properties and rows, as a restore with the page's databases does for
// each — and leaves the page and its other databases as they are, as
// Notion's restore-version does given an inline database (its tool restores
// "a page or database"). A linked database takes back its views alone. The
// restore is known by its requestId as a page's is: asked again, it finishes
// what the first left and answers as a duplicate.

// pageHistoryDatabaseScope narrows a database restore to one database: the
// views it shows and, unless ViewsOnly, its source's properties and rows. The
// zero scope is the source whole, as a page's restore takes it.
type pageHistoryDatabaseScope struct {
	ContainerID string
	ViewsOnly   bool
}

// holdsView is whether a view is the scope's database's: its own, or for a
// database holding its source, one stored without a database.
func (scope pageHistoryDatabaseScope) holdsView(containerID string) bool {
	return scope.ContainerID == "" || containerID == scope.ContainerID || containerID == "" && !scope.ViewsOnly
}

// errPageHistoryDatabaseNotThen refuses a database the version doesn't hold.
var errPageHistoryDatabaseNotThen = refuse(errPageHistoryRestoreConflict, "databaseId names a database the page didn't hold at that version")

// pageHistoryDatabaseScopes are the data sources a restore takes back, each
// with its scope: every one the version holds, or one database's.
func pageHistoryDatabaseScopes(
	app core.App, pageID string, state pageHistoryMaterializedState, databaseID string,
) (map[string]pageHistoryDatabaseScope, error) {
	scopes := map[string]pageHistoryDatabaseScope{}
	if databaseID == "" {
		for sourceID := range state.Databases {
			scopes[sourceID] = pageHistoryDatabaseScope{}
		}
		return scopes, nil
	}
	members := []struct {
		DataSourceID      string `db:"data_source_id"`
		SourceContainerID string `db:"source_container_id"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[membership]].[[data_source_id]], [[membership]].[[source_container_id]]
		FROM {{leafen_database_container_sources}} AS [[membership]]
		JOIN {{leafen_database_containers}} AS [[container]] ON [[container]].[[id]] = [[membership]].[[container_id]]
		WHERE [[container]].[[id]] = {:id} AND [[container]].[[page_id]] = {:pageId}
		ORDER BY [[membership]].[[position]], [[membership]].[[data_source_id]]
	`).Bind(dbx.Params{"id": databaseID, "pageId": pageID}).All(&members); err != nil {
		return nil, err
	}
	if len(members) == 0 {
		return nil, refuse(errPageHistoryRestoreConflict, "databaseId names no database on this page")
	}
	for _, member := range members {
		held, found := state.Databases[member.DataSourceID]
		scope := pageHistoryDatabaseScope{ContainerID: databaseID, ViewsOnly: member.SourceContainerID != ""}
		if !found || held.Source == nil && !pageHistoryVersionShowsDatabase(held, scope) {
			continue
		}
		scopes[member.DataSourceID] = scope
	}
	if len(scopes) == 0 {
		return nil, errPageHistoryDatabaseNotThen
	}
	return scopes, nil
}

// pageHistoryVersionShowsDatabase is whether a version holds one of a
// database's views.
func pageHistoryVersionShowsDatabase(held pageHistoryDatabaseState, scope pageHistoryDatabaseScope) bool {
	for _, view := range held.Views {
		if view.ContainerID == scope.ContainerID {
			return true
		}
	}
	return false
}

// admitPageHistoryDatabaseRestore checks a database's restore as a page's
// is checked — the person edits the page, it isn't locked, the version is
// kept and the page is as the person saw it — and the version holds the
// database, then keeps its receipt; asked again, it answers the receipt.
func admitPageHistoryDatabaseRestore(
	app core.App, actorID, pageID, versionID string, input pageHistoryRestoreRequest,
) (pageHistoryRestoreResponse, error) {
	response := pageHistoryRestoreResponse{}
	err := app.RunInTransaction(func(txApp core.App) error {
		authorization, err := authorizePageHistoryRestore(txApp, actorID, pageID)
		if err != nil {
			return err
		}
		requestHash := pageHistoryRestoreRequestHash(pageID, versionID, input.BaseRevision, input)
		existing, err := loadPageHistoryRestoreReceipt(txApp, actorID, input.RequestID)
		switch {
		case err == nil && (existing.PageID != pageID || existing.RequestHash != requestHash || existing.RestoredVersionID != versionID):
			return errPageHistoryRequestReused
		case err == nil:
			response.Receipt = pageHistoryReceiptAt(input.RequestID, versionID, existing.Revision, true)
		case !errors.Is(err, sql.ErrNoRows):
			return err
		default:
			revision, err := checkPageHistoryDatabaseRestore(txApp, authorization, actorID, pageID, versionID, input)
			if err != nil {
				return err
			}
			if _, err := txApp.DB().Insert("leafen_page_history_restore_receipts", dbx.Params{
				"actor_id": actorID, "workspace_id": authorization.Workspace.WorkspaceID,
				"page_id": pageID, "request_id": input.RequestID,
				"request_hash": requestHash, "restored_version_id": versionID,
				"revision": revision, "created_at": time.Now().UTC().UnixMilli(),
			}).Execute(); err != nil {
				return err
			}
			response.Receipt = pageHistoryReceiptAt(input.RequestID, versionID, revision, false)
		}
		page, err := pageRecordResponseWithWiki(txApp, authorization.Page)
		response.Page = page
		return err
	})
	return response, err
}

// checkPageHistoryDatabaseRestore is a new database restore's checks, giving
// the page's frontier.
func checkPageHistoryDatabaseRestore(
	app core.App, authorization pageAuthorization, actorID, pageID, versionID string, input pageHistoryRestoreRequest,
) (int, error) {
	if locked, err := pageLockedFor(app, authorization.Page, actorID); err != nil {
		return 0, err
	} else if locked {
		return 0, errPageHistoryPageLocked
	}
	version, err := loadPageHistoryVersion(app, pageID, versionID)
	if pageHistoryVersionMissing(err) {
		return 0, errPageHistoryVersionMissing
	}
	if err != nil {
		return 0, err
	}
	if start, err := pageHistoryStartCursor(app, pageID); err != nil {
		return 0, err
	} else if version.VersionCursor < start {
		return 0, errPageHistoryVersionMissing
	}
	frontier, err := pageHistorySnapshotFrontier(app, pageID)
	if err != nil {
		return 0, err
	}
	if frontier != input.BaseRevision {
		return 0, errPageHistoryRevisionChanged
	}
	state, err := materializePageHistoryVersion(app, pageID, version.VersionCursor)
	if err != nil {
		return 0, err
	}
	if _, err := pageHistoryDatabaseScopes(app, pageID, state, input.DatabaseID); err != nil {
		return 0, err
	}
	return frontier, nil
}
