package app

import (
	"encoding/json"

	"github.com/pocketbase/pocketbase/core"
)

// A page's own block: Notion's workspace updates tell a page coming back from
// the trash as its block, not as the page made again (seen 2026-11-17 04:1x in
// the free test workspace's '모든 워크스페이스 업데이트', first account —
// private pages at the top restored from the trashed page's banner, from the
// trash window and by ⌘Z after a move from the sidebar's menu, a teamspace's
// page at the top and a sub-page from the banner, a database's row from the
// trash window):
//   - a page at the top or under another is "〈x〉 블록을 복원했습니다",
//     placed as its page's own line is ("· 〈teamspace〉" for a teamspace's
//     page at the top, "· 〈page above〉" for a sub-page, none for a private
//     page at the top), with no "made" line;
//   - a database's row is made again, "〈x〉 페이지를 생성했습니다 ·
//     〈database〉" (Notion's line of its making took that time, seen once
//     within its making's window; here it is a line of its own).

// A page at the top put in the trash is its block deleted too: Notion's
// updates list "〈x〉 블록을 삭제했습니다" beside the page's own line
// ("개인 페이지인 〈x〉 페이지를 삭제했습니다", which says no place), the block's
// placed as its page's line is — "· 〈teamspace〉" for a teamspace's page,
// none for a private one (seen 2026-11-17 04:0x by worker w1582 and 04:1x
// again: private pages A, D, R1–R3 and teamspace pages B, T1). A sub-page's is
// its block alone, deleted from the page that held it
// (workspace_activity_graph.go), and a database's row its page line alone.
//
// A page's block lines, deleted and restored, are one line each, never
// joined, where its page's deleted line is one in its window (seen
// 2026-11-17 04:1x: R3 put in the trash and brought back twice within three
// minutes, two lines each, one page line).

// workspaceActivityPageBlockKind is the kind of a line of a page's own block,
// named as the page is named now.
const workspaceActivityPageBlockKind = "page"

// recordWorkspacePageBlockDeleted records a page at the top put in the trash
// as its block deleted, besides the page's own deleted line: a millisecond
// before it, so it lists under the page's, as Notion's always did (each of
// the pages above). Lines of one time list as first written
// (workspaceActivityOrder), and the page's line put in the trash again within
// its window is its group written before, which the block's new line would
// stand over.
func recordWorkspacePageBlockDeleted(app core.App, page *core.Record, actor, origin string, at int64) error {
	metadata := workspaceActivityMetadata{Name: page.GetString("title"), Kind: workspaceActivityPageBlockKind, SubjectPageID: page.Id}
	return recordWorkspaceActivity(app, page, actor, origin, "block", page.Id, "delete", metadata, at-1)
}

// workspaceActivityOwnBlock says whether a line is a page's own block's, as
// its page's line is placed.
func workspaceActivityOwnBlock(entityType string, metadata workspaceActivityMetadata) bool {
	return entityType == "block" && metadata.Kind == workspaceActivityPageBlockKind
}

// workspaceActivityOneEach says whether a change is a line of its own, never
// joined to another of its kind in its window: a page's block deleted — a
// sub-page's or a page's own at the top — or a page back from the trash.
func workspaceActivityOneEach(entry workspaceActivityEntry) bool {
	if entry.EntityType == "block" {
		return entry.Action == "delete" && entry.Metadata.SubjectPageID != ""
	}
	return entry.EntityType == "page" && entry.Action == "restore" && entry.Metadata.Kind == "trashRestore"
}

// readWorkspaceActivityTrashRestore says a page's way back from the trash as
// Notion's updates do: a database's row made again, any other page its own
// block restored. It says whether the line is now the page's own block's.
func readWorkspaceActivityTrashRestore(item *workspaceActivityRecord, page *core.Record) bool {
	if item.EntityType != "page" || item.EntityID != item.PageID || item.Action != "restore" || item.Kind != "trashRestore" {
		return false
	}
	if isDatabaseRowPageRecord(page) {
		item.Action, item.Kind = "create", ""
		return false
	}
	item.EntityType, item.Kind = "block", workspaceActivityPageBlockKind
	return true
}

// workspaceActivityWikiLinkedPages lists the sub-pages whose page blocks a
// change swapped for their wiki's links to them, as a page becomes its wiki's
// page (childPageBlockWikiLinks): a page made in a wiki from the sidebar's
// "+", or the pages of a page turned into a wiki. Such a block going is no
// deletion of the page's block: Notion's updates of a page made in a wiki
// from the sidebar's "+" list no "〈x〉 블록을 삭제했습니다", its own edit
// alone (seen 2026-11-17 at 1440, twice), nor do the two pages of a page
// turned into a wiki (seen 2026-11-17 once each: their own edits alone).
func workspaceActivityWikiLinkedPages(deltas []pageHistoryGraphDelta) map[string]bool {
	linked := map[string]bool{}
	for _, delta := range deltas {
		if delta.Deleted || delta.Type != graphBlockTypeLinkToPage {
			continue
		}
		content := map[string]any{}
		if json.Unmarshal(delta.Content, &content) != nil || content[wikiPageLinkMark] != true {
			continue
		}
		if pageID, _ := content["childPageId"].(string); pageID != "" {
			linked[pageID] = true
		}
	}
	return linked
}
