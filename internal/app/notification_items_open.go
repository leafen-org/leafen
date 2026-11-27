package app

import (
	"encoding/json"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Notices about database items are judged together, not item by item: a
// badge's count reads every notice on pages the person doesn't open, an
// inbox's window, its archive and its marking read a window of them, and a
// paste naming someone in a thousand rows tells them of each (2026-11-21:
// judged one by one — each item's page, its table's page, its shares and the
// path above it — a badge counted for someone shared 500 items asked 31,072
// statements, and such a paste judged its thousand rows holding the write).
// notificationItemsOpen reads which of the named pages are items and what
// judging them asks, at once, into a read scope (primeListedPages); each
// table's page is then judged once for all its items, and each item is
// decided as authorizeDatabaseRowPage decides it, from what was read. An
// item of a wiki still reads its place in the wiki on its own.

// notificationItemOpening is what a person's notice about an item finds of
// the item.
type notificationItemOpening struct {
	// Item is whether the notice names a database item that is there.
	Item bool
	// Opens is whether the person opens the item's page — through its
	// table's page or its own share — in the trash too.
	Opens bool
	// Container is the table's page holding the item, and Trashed whether
	// the item itself is in the trash, for an item that opens.
	Container string
	Trashed   bool
}

// notificationItemsOpen reads, for the pages a person's notices name, which
// are database items and whether each opens to them.
func notificationItemsOpen(app core.App, userID string, items []string) (map[string]notificationItemOpening, error) {
	opening := make(map[string]notificationItemOpening, len(items))
	if len(items) == 0 {
		return opening, nil
	}
	app = withAccessMemo(app)
	encoded, err := json.Marshal(items)
	if err != nil {
		return nil, err
	}
	found := []string{}
	if err := app.DB().NewQuery(`SELECT [[id]] FROM {{leafen_database_rows}}
		WHERE [[id]] IN (SELECT [[value]] FROM json_each({:ids}))`).Bind(dbx.Params{"ids": string(encoded)}).Column(&found); err != nil {
		return nil, err
	}
	if _, err := primeListedPages(app, userID, found); err != nil {
		return nil, err
	}
	// An item whose page the priming read none for has none in this read:
	// its table's data source is in the trash, or its page is going.
	memo := app.(accessMemoHolder).accessMemo()
	for _, item := range found {
		if _, known := memo[rowPageAuthorityMemoKey{item}]; !known {
			memo[rowPageAuthorityMemoKey{item}] = accessMemoEntry{err: databaseRowPageNotFoundError()}
		}
	}
	for _, item := range found {
		authority, opens, err := notificationItemReadable(app, userID, item)
		if err != nil {
			return nil, err
		}
		opening[item] = notificationItemOpening{
			Item: true, Opens: opens, Container: authority.ContainerPageID, Trashed: authority.TrashedAt != 0,
		}
	}
	return opening, nil
}

// notificationItemsTold says, of the pages notices on one page would open,
// which the person may be told of now (authorizeNotificationSubject): an
// item they open, out of the trash with its table's page; any other by the
// page the notices sit on.
func notificationItemsTold(app core.App, userID, pageID string, targets []string) (map[string]bool, error) {
	told := make(map[string]bool, len(targets))
	app = withAccessMemo(app)
	items := []string{}
	for _, target := range targets {
		if item := notificationItemOf(target); item != "" && item != pageID && !pagePermissionNoticeShown(pageID) {
			items = append(items, item)
		}
	}
	opening, err := notificationItemsOpen(app, userID, items)
	if err != nil {
		return nil, err
	}
	pageTold, pageJudged := false, false
	trashedPages := map[string]bool{}
	for _, target := range targets {
		if _, judged := told[target]; judged {
			continue
		}
		item, found := opening[notificationItemOf(target)]
		if !found || notificationItemOf(target) == pageID || pagePermissionNoticeShown(pageID) {
			if !pageJudged {
				_, err := authorizeCommentNotificationTarget(app, userID, pageID)
				if err != nil && !isPermissionDenied(err) {
					return nil, err
				}
				pageTold, pageJudged = err == nil, true
			}
			told[target] = pageTold
			continue
		}
		if !item.Opens || item.Trashed {
			told[target] = false
			continue
		}
		trashed, known := trashedPages[item.Container]
		if !known {
			if trashed, err = pageInTrashPath(app, item.Container); err != nil {
				return nil, err
			}
			trashedPages[item.Container] = trashed
		}
		told[target] = !trashed
	}
	return told, nil
}
