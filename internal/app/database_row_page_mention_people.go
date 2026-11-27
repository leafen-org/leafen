package app

import (
	"database/sql"
	"errors"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The @ menu finds the workspace's People database itself by its name, as
// Notion's "[[" does ("[[People" offers People by its People icon, seen
// 2026-11-21 in Chrome in a free workspace with People), though none of its
// rows, which are its people (database_row_page_mention_search.go). People
// is in no one's page list (people_database.go), so the menu's own pages
// never hold it: the search the menu asks rows of finds it, for a reader who
// may open it (people_database_access.go) — none for an empty query, which
// the menu answers with recent pages.

// readPeopleDatabaseMentionCandidate is People when a typed query finds its
// name, marked as People, by its page's own icon (the menu draws People's
// where it has none, PageIcon) and its database.
func readPeopleDatabaseMentionCandidate(app core.App, readerID, workspaceID, query string) ([]databaseRowMentionCandidate, error) {
	if query == "" {
		return nil, nil
	}
	database, has, err := loadPeopleDatabase(app, workspaceID)
	if err != nil || !has {
		return nil, err
	}
	page, err := app.FindRecordById(pagesCollection, database.PageID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// Not while it is in the trash or under a page there (loadPageTrashEffects).
	if trashed, err := pageInTrashPath(app, database.PageID); err != nil || trashed {
		return nil, err
	}
	title := strings.TrimSpace(page.GetString("title"))
	if title == "" {
		title = peopleDatabaseTitle
	}
	// By its name's words, as a row is found (mentionSearchRank).
	if !mentionNameHolds(title, query) {
		return nil, nil
	}
	if _, err := authorizePage(app, readerID, database.PageID, permissionWorkspaceRead); err != nil {
		if isPermissionDenied(err) {
			return nil, nil
		}
		return nil, err
	}
	// Named by its own database, as any full-page database's mention is
	// (mentionMenuItems.ts), so its mention opens People and draws its icon.
	containerID := ""
	err = app.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_database_containers}}
		WHERE [[page_id]] = {:pageId} AND [[parent_block_id]] = '' AND [[kind]] = 'source'
		ORDER BY [[position]], [[id]] LIMIT 1
	`).Bind(dbx.Params{"pageId": database.PageID}).Row(&containerID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return []databaseRowMentionCandidate{{DatabaseID: containerID, Title: title, Icon: page.GetString("icon"),
		IconType: page.GetString("icon_type"), PeopleDatabase: true, Updated: page.GetDateTime("updated").Time().UnixMilli()}}, nil
}
