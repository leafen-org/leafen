package app

import (
	"encoding/json"
	"net/url"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A notice about a database item sits on the table's page, but the item has
// access of its own: an unlinked page can hide it from someone who sees the
// table, and its own share opens it to someone who doesn't
// (notification_item_subjects.go). Whoever can't open the item gets no
// notice of it, and one they had stops showing, as a notice on a page they
// lost does.

// notificationItemOf is the page a notice's address opens, which may be a
// database item: a notice about an item opens it at /p/<item>
// (databaseRowPageTarget), a comment on it at /p/<item>#comment-…, and an
// older notice at /p/<page>?p=<item>. Its callers read which of these are
// items; the others are ordinary pages and change nothing.
func notificationItemOf(target string) string {
	parsed, err := url.Parse(target)
	if err != nil {
		return ""
	}
	if item := parsed.Query().Get("p"); item != "" {
		return item
	}
	id, found := strings.CutPrefix(parsed.Path, "/p/")
	if !found || id == "" || strings.Contains(id, "/") {
		return ""
	}
	return id
}

// notificationItemsHidden reads together which of the items named by a person's
// notices they can't open, as a table reads its rows: a long inbox doesn't ask
// after each one.
func notificationItemsHidden(app core.App, userID string, items map[string]struct{}) (map[string]bool, error) {
	hidden := map[string]bool{}
	if len(items) == 0 {
		return hidden, nil
	}
	ids := make([]string, 0, len(items))
	for item := range items {
		ids = append(ids, item)
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	rows := []struct {
		ID     string `db:"id"`
		Source string `db:"data_source_id"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[id]], [[data_source_id]] FROM {{leafen_database_rows}}
		WHERE [[id]] IN (SELECT [[value]] FROM json_each({:ids}))`).Bind(dbx.Params{"ids": string(encoded)}).All(&rows); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return hidden, nil
	}
	seen := map[string]bool{}
	sources := []string{}
	for _, row := range rows {
		if !seen[row.Source] {
			seen[row.Source] = true
			sources = append(sources, row.Source)
		}
	}
	access, err := loadDatabaseRowAccess(app, userID, sources)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		hidden[row.ID] = access.hides(row.ID)
	}
	return hidden, nil
}

// notificationInboxHiddenItems counts a tab's notices on pages the person sees
// that name an item they can't open. Only a workspace with unlinked pages
// can have any, so the others read nothing more for their count.
func notificationInboxHiddenItems(
	app core.App, principalID, workspaceID string, personalOnly bool, state string, visiblePages map[string]struct{},
) (int, error) {
	unlinked := 0
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM (SELECT 1 FROM {{leafen_page_share_unlinks}} WHERE [[workspace_id]] = {:workspaceId} LIMIT 1)`).
		Bind(dbx.Params{"workspaceId": workspaceID}).Row(&unlinked); err != nil {
		return 0, err
	}
	if unlinked == 0 {
		return 0, nil
	}
	rows := []struct {
		PageID string `db:"page_id"`
		Target string `db:"target"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[page_id]], [[target]]
		FROM {{leafen_notifications}}
		INDEXED BY [[idx_leafen_notifications_user_activity]]
		WHERE [[workspace_id]]={:workspaceId} AND [[user_id]]={:userId}
			AND ` + notificationShownSQL("") + `
			AND ({:personalOnly} = 0 OR [[personal]] = 1)
			AND ` + notificationInboxStateCondition(state) + `
			AND ` + notificationGroupedAwaySQL() + `
	`).Bind(dbx.Params{"workspaceId": workspaceID, "userId": principalID, "personalOnly": personalOnly}).All(&rows); err != nil {
		return 0, err
	}
	named := map[string]int{}
	items := map[string]struct{}{}
	for _, row := range rows {
		if _, shown := visiblePages[row.PageID]; !shown || pagePermissionNoticeShown(row.PageID) {
			continue
		}
		if item := notificationItemOf(row.Target); item != "" {
			named[item]++
			items[item] = struct{}{}
		}
	}
	unseen, err := notificationItemsHidden(app, principalID, items)
	if err != nil {
		return 0, err
	}
	hidden := 0
	for item, notices := range named {
		if unseen[item] {
			hidden += notices
		}
	}
	return hidden, nil
}
