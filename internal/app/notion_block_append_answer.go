package app

import (
	"errors"
	"net/http"

	"github.com/pocketbase/pocketbase/core"
)

// serveNotionBlockAppendAnswer answers an append that added blocks as
// Notion's does: the parent's children as a read of them lists them, from
// the first block it added to the end, their first 100 with has_more and
// next_cursor — one added at the start answers it and every block after it
// (seen 2026-11-21, Notion-Version 2026-03-11, synthetic pages under
// a Notion test page: a page and a toggle, with no position, at the start, at
// the end, after one of its blocks, at 2025-09-03 after one, a sub-page
// among them, and over 100). An append that adds none reads from the first
// child the same way (serveNotionBlockAppendCount). The added blocks read as
// their write gave them (added, notionTableCellMentionItems); were the
// first of them gone by the read, they alone are the answer.
func serveNotionBlockAppendAnswer(
	event *core.RequestEvent,
	target notionBlockAppendTarget,
	addedIDs []string,
	added []map[string]any,
) error {
	if len(addedIDs) == 0 {
		return notionBlockAppendList(event, added, false, nil)
	}
	fileIssuer := newNotionBlockFileCapabilityIssuer(event.Request, target.Identity)
	var (
		results []map[string]any
		hasMore bool
		next    any
		err     error
	)
	if target.ParentID == "" {
		var snapshot notionPageBlockChildrenSnapshot
		snapshot, err = loadNotionPageBlockChildrenSnapshot(event.App,
			notionPageAuthorization{Identity: target.Identity, Page: target.Page},
			notionPageBodyCursor{Kind: "block", ID: addedIDs[0]},
			notionBlockDefaultPageSize, fileIssuer)
		results, hasMore, next = snapshot.Results, snapshot.HasMore, snapshot.NextCursor
	} else {
		var snapshot notionBlockChildrenReadSnapshot
		snapshot, err = loadNotionBlockChildrenReadSnapshot(event.App, notionBlockAuthorization{
			Identity: target.Identity, Page: target.Page, Block: storedNotionBlock{ID: target.ParentID},
		}, addedIDs[0], notionBlockDefaultPageSize, fileIssuer)
		results, hasMore, next = snapshot.Results, snapshot.HasMore, snapshot.NextCursor
	}
	if isPermissionDenied(err) || errors.Is(err, errNotionBlockCursorInvalid) {
		return notionBlockAppendList(event, added, false, nil)
	}
	if err != nil {
		return notionBlockProjectionError(event, err)
	}
	written := make(map[any]map[string]any, len(added))
	for _, block := range added {
		written[block["id"]] = block
	}
	for index, block := range results {
		if value, ok := written[block["id"]]; ok {
			results[index] = value
		}
	}
	recordAPIReads(event.App, target.Identity, []contentRead{contentReadOfPage(target.Page.Page)})
	event.Response.Header().Set("Cache-Control", "no-store")
	return notionBlockAppendList(event, results, hasMore, next)
}

func notionBlockAppendList(event *core.RequestEvent, results []map[string]any, hasMore bool, next any) error {
	return event.JSON(http.StatusOK, map[string]any{
		"object": "list", "results": results, "next_cursor": next,
		"has_more": hasMore, "type": "block", "block": map[string]any{},
	})
}
