package app

import (
	"encoding/json"

	"github.com/pocketbase/pocketbase/core"
)

// A sub-page's making is no line of its own in Notion's workspace updates
// (seen 2026-11-17 04:4x by worker w1588 in two tabs, and 05:0x again in the
// free test workspace's '모든 워크스페이스 업데이트', first account): a page
// made under another — by "/페이지", by the "+" on its row in the sidebar, or
// as a copy of a sub-page — lists no "〈x〉 페이지를 생성했습니다 · 〈위〉".
// Its name is an edit of it, "〈x〉 페이지를 편집했습니다 · 〈위〉" with "Aa
// 제목" over the name: the name typed after it was made
// (workspace_activity_title_change.go), or the one it was made with — a
// copy's "〈x〉 (1)". The page above lists it among its own edit's changes
// (workspace_activity_graph.go). A page at the top and a database's row keep
// their made lines, and a full-page database made under a page is its
// database's lines alone ("〈DB〉 컬렉션을 생성했습니다", its name its
// source's). The organization's audit log keeps every making.

// workspaceActivitySubPageMade says whether a change is a sub-page's making.
func workspaceActivitySubPageMade(page *core.Record, entityType, entityID, action string) bool {
	return entityType == "page" && action == "create" && entityID == page.Id &&
		page.GetString("parent_id") != "" && !isDatabaseRowPageRecord(page)
}

// readWorkspaceActivitySubPageMade reads a sub-page's making as Notion's
// updates do: an edit of it where it was made with a name — a full-page
// database's name being its source's — and no line otherwise. It says
// whether the making is a line.
func readWorkspaceActivitySubPageMade(entry *workspaceActivityEntry) bool {
	if !entry.SubPageMade {
		return true
	}
	if !entry.SubPageNamed || entry.Metadata.Name == "" {
		return false
	}
	entry.Action, entry.Metadata.Made = "edit", true
	return true
}

// workspaceActivityMadeTitle is the title a sub-page's making named it with,
// first among its edit's changes, as Notion's "Aa 제목 〈x (1)〉" under a
// copy's edit line (seen 2026-11-17 05:0x in two tabs): none before it.
func workspaceActivityMadeTitle(row storedWorkspaceActivity) (workspaceActivityChange, bool) {
	var metadata workspaceActivityMetadata
	if json.Unmarshal([]byte(row.Metadata), &metadata) != nil || !metadata.Made || metadata.Name == "" {
		return workspaceActivityChange{}, false
	}
	return workspaceActivityChange{Kind: workspaceActivityChangeProperty, BlockID: graphValueTypeTitle, PropertyType: graphValueTypeTitle,
		After: workspaceActivityClip(metadata.Name)}, true
}
