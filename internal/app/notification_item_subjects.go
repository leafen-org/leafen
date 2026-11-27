package app

import (
	"encoding/json"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A notice about a database item sits on the table's page but is about the
// item: whoever opens the item is told of it, whether or not they open the
// table's page — someone shared only the item, as a send shares it, hears of
// its person property naming them and of its comments, as Notion tells them
// (2026-11-21: a send answered notified, and its recipient had no notice).
// Every road a notice takes asks it so: its making and its mail, push and
// Slack (authorizeNotificationSubject), and the inbox's list, count, archive
// and reading, the desktop's notices and an agent's inbox
// (shownNotificationSubjects), many notices' items judged together
// (notification_items_open.go).

// notificationItemReadable is whether the person opens a database item's
// page — through its table's page or its own share — in the trash too.
func notificationItemReadable(app core.App, userID, itemID string) (databaseRowPageAuthority, bool, error) {
	authority, err := authorizeDatabaseRowPage(app, userID, itemID, permissionWorkspaceRead)
	if errors.Is(err, errDatabaseRowPageNotFound) || isPermissionDenied(err) {
		return authority, false, nil
	}
	return authority, err == nil, err
}

// authorizeNotificationSubject is whether the person may be told now of what
// a notice is about: an item they open, out of the trash with its table's
// page, for a notice about a database item; else its page — as many notices
// on a page are judged together (notificationItemsTold).
func authorizeNotificationSubject(app core.App, userID, pageID, target string) error {
	told, err := notificationItemsTold(app, userID, pageID, []string{target})
	if err != nil {
		return err
	}
	if !told[target] {
		return permissionDenied()
	}
	return nil
}

// notificationSubjectRef is a notice as its access is read: the page it sits
// on and where it opens.
type notificationSubjectRef struct {
	PageID string `db:"page_id"`
	Target string `db:"target"`
}

// shownNotificationSubjects says which of a person's notices in a workspace
// their inbox still shows: one on a page they open, in the trash too
// (visibleCommentNotificationPageSet), unless it names an item that page's
// sharing keeps from them (notificationItemsHidden); and one naming an item
// on a page they don't open where the item opens to them by its own share.
func shownNotificationSubjects(app core.App, userID, workspaceID string, notices []notificationSubjectRef) ([]bool, error) {
	pageIDs := make([]string, 0, len(notices))
	for _, notice := range notices {
		pageIDs = append(pageIDs, notice.PageID)
	}
	visible, err := visibleCommentNotificationPageSet(app, userID, workspaceID, pageIDs)
	if err != nil {
		return nil, err
	}
	return shownNotificationSubjectsOn(app, userID, notices, visible)
}

// shownNotificationSubjectsOn is shownNotificationSubjects with the pages the
// person opens already read; the items named on the others are judged
// together (notificationItemsOpen).
func shownNotificationSubjectsOn(app core.App, userID string, notices []notificationSubjectRef, visible map[string]struct{}) ([]bool, error) {
	items := map[string]struct{}{}
	closedItems, closedSeen := []string{}, map[string]bool{}
	for _, notice := range notices {
		item := notificationItemOf(notice.Target)
		if item == "" || pagePermissionNoticeShown(notice.PageID) {
			continue
		}
		if _, open := visible[notice.PageID]; open {
			items[item] = struct{}{}
		} else if item != notice.PageID && !closedSeen[item] {
			closedSeen[item] = true
			closedItems = append(closedItems, item)
		}
	}
	hidden, err := notificationItemsHidden(app, userID, items)
	if err != nil {
		return nil, err
	}
	opening, err := notificationItemsOpen(app, userID, closedItems)
	if err != nil {
		return nil, err
	}
	shown := make([]bool, len(notices))
	for index, notice := range notices {
		item := notificationItemOf(notice.Target)
		if _, open := visible[notice.PageID]; open {
			// A share of theirs taken away shows though the item is closed to them now.
			shown[index] = pagePermissionNoticeShown(notice.PageID) || !hidden[item]
			continue
		}
		if item == "" || item == notice.PageID || pagePermissionNoticeShown(notice.PageID) {
			continue
		}
		shown[index] = opening[item].Opens
	}
	return shown, nil
}

// notificationInboxItemsOnClosedPages counts a tab's notices on the given
// pages, which the person doesn't open, about items that open to them: the
// notices are read as how many each page's address has, and their items
// judged together.
func notificationInboxItemsOnClosedPages(
	app core.App, principalID, workspaceID string, personalOnly bool, state string, closedPages []string,
) (int, error) {
	if len(closedPages) == 0 {
		return 0, nil
	}
	encoded, err := json.Marshal(closedPages)
	if err != nil {
		return 0, err
	}
	rows := []struct {
		notificationSubjectRef
		Count int `db:"count"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[page_id]], [[target]], COUNT(*) AS [[count]]
		FROM {{leafen_notifications}}
		INDEXED BY [[idx_leafen_notifications_user_activity]]
		WHERE [[workspace_id]]={:workspaceId} AND [[user_id]]={:userId}
			AND [[page_id]] IN (SELECT [[value]] FROM json_each({:pages}))
			AND ` + notificationShownSQL("") + `
			AND ({:personalOnly} = 0 OR [[personal]] = 1)
			AND ` + notificationInboxStateCondition(state) + `
			AND ` + notificationGroupedAwaySQL() + `
		GROUP BY [[page_id]], [[target]]
	`).Bind(dbx.Params{"workspaceId": workspaceID, "userId": principalID, "personalOnly": personalOnly, "pages": string(encoded)}).All(&rows); err != nil {
		return 0, err
	}
	notices := make([]notificationSubjectRef, 0, len(rows))
	for _, row := range rows {
		notices = append(notices, row.notificationSubjectRef)
	}
	shown, err := shownNotificationSubjectsOn(app, principalID, notices, map[string]struct{}{})
	if err != nil {
		return 0, err
	}
	count := 0
	for index, open := range shown {
		if open {
			count += rows[index].Count
		}
	}
	return count, nil
}

// notificationInboxCount counts a tab's notifications on pages the person
// still sees, every one of them: by page, then each page's visibility.
func notificationInboxCount(
	app core.App, principalID, workspaceID string, personalOnly bool, state string,
) (int, error) {
	rows := []struct {
		PageID string `db:"page_id"`
		Count  int    `db:"count"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[page_id]], COUNT(*) AS [[count]]
		FROM {{leafen_notifications}}
		INDEXED BY [[idx_leafen_notifications_user_activity]]
		WHERE [[workspace_id]]={:workspaceId} AND [[user_id]]={:userId}
			AND ` + notificationShownSQL("") + `
			AND ({:personalOnly} = 0 OR [[personal]] = 1)
			AND ` + notificationInboxStateCondition(state) + `
			AND ` + notificationGroupedAwaySQL() + `
		GROUP BY [[page_id]]
	`).Bind(dbx.Params{"workspaceId": workspaceID, "userId": principalID, "personalOnly": personalOnly}).All(&rows); err != nil {
		return 0, err
	}
	pageIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		pageIDs = append(pageIDs, row.PageID)
	}
	visiblePages, err := visibleCommentNotificationPageSet(app, principalID, workspaceID, pageIDs)
	if err != nil {
		return 0, err
	}
	count, closed := 0, []string{}
	for _, row := range rows {
		if _, ok := visiblePages[row.PageID]; ok {
			count += row.Count
		} else {
			closed = append(closed, row.PageID)
		}
	}
	hidden, err := notificationInboxHiddenItems(app, principalID, workspaceID, personalOnly, state, visiblePages)
	if err != nil {
		return 0, err
	}
	shownItems, err := notificationInboxItemsOnClosedPages(app, principalID, workspaceID, personalOnly, state, closed)
	return count - hidden + shownItems, err
}
