package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// An inline database opened alone has its own version history, as Notion's
// (seen 2026-11-11: "버전 기록 / 〈database〉", its own versions only, each
// previewed as the database's table alone with the rows it changed marked).
// Its versions are its own updates (schema_baseline.json);
// a version's table is the database as its page held it then: the page's
// version standing at that time, its properties and views, and its rows with
// the values they had.

func registerPageHistoryDatabaseAtRoute(event *core.ServeEvent) {
	event.Router.GET(apiRoot+pageHistoryDatabaseAtPath, readPageHistoryDatabaseAt)
}

func readPageHistoryDatabaseAt(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	blockID := strings.TrimSpace(event.Request.PathValue("blockId"))
	at, err := strconv.ParseInt(event.Request.URL.Query().Get("at"), 10, 64)
	if err != nil || at <= 0 || pageID == "" || blockID == "" {
		return apiError(event, http.StatusBadRequest, errorCodePageHistoryInvalid, "Check the database history query.")
	}
	response := pageHistoryDatabaseAtResponse{PageID: pageID, BlockID: blockID, At: at}
	err = runReadSnapshot(event.Request.Context(), event.App, func(txApp core.App) error {
		if _, err := authorizePage(txApp, user.Id, pageID, permissionWorkspaceRead); err != nil {
			return err
		}
		cursor, found, err := pageHistoryCursorAt(txApp, pageID, at)
		if err != nil || !found {
			return err
		}
		databases, err := pageHistoryVersionDatabases(txApp, pageID, cursor, at)
		if err != nil {
			return err
		}
		version := pageHistorySnapshotResponse{PageID: pageID}
		for _, database := range databases {
			if database.BlockID == blockID {
				version.Databases = []pageHistoryDatabaseWire{database}
				break
			}
		}
		if len(version.Databases) == 0 {
			return nil
		}
		// Read as the page's own history reads it: what the reader may not see stays out.
		if err := redactPageHistorySnapshot(txApp, user.Id, pageID, &version); err != nil {
			return err
		}
		pageHistoryAsRead(txApp, user.Id, &version)
		if len(version.Databases) == 1 {
			response.Database = &version.Databases[0]
		}
		return nil
	})
	return writePageHistoryResult(event, response, err, "database-at")
}

// pageHistoryCursorAt is the page's version standing at a time: its latest
// made by then, or its first when the time comes before them all.
func pageHistoryCursorAt(app core.App, pageID string, at int64) (int, bool, error) {
	start, err := pageHistoryStartCursor(app, pageID)
	if err != nil {
		return 0, false, err
	}
	frontier, err := pageHistorySnapshotFrontier(app, pageID)
	if err != nil {
		return 0, false, err
	}
	var cursor int
	params := dbx.Params{"pageId": pageID, "start": start, "frontier": frontier, "at": at}
	err = app.DB().NewQuery(`
		SELECT [[version_cursor]] FROM {{leafen_page_history_versions}}
			INDEXED BY [[idx_leafen_page_history_versions_page_frontier]]
		WHERE [[page_id]]={:pageId} AND [[version_cursor]]>={:start} AND [[version_cursor]]<={:frontier}
			AND [[created_at]]<={:at}
		ORDER BY [[version_cursor]] DESC LIMIT 1
	`).Bind(params).Row(&cursor)
	if errors.Is(err, sql.ErrNoRows) {
		err = app.DB().NewQuery(`
			SELECT [[version_cursor]] FROM {{leafen_page_history_versions}}
				INDEXED BY [[idx_leafen_page_history_versions_page_frontier]]
			WHERE [[page_id]]={:pageId} AND [[version_cursor]]>={:start} AND [[version_cursor]]<={:frontier}
			ORDER BY [[version_cursor]] ASC LIMIT 1
		`).Bind(params).Row(&cursor)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	return cursor, err == nil, err
}
