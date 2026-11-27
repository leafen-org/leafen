package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A page's new title is an edit of it in Notion's workspace updates, never a
// line of its own: "〈x〉 페이지를 편집했습니다" with "Aa 제목" over the new
// title first under it, and a database's row its title property's name, "Aa
// 이름" (seen 2026-11-17 04:0x by worker w1582 and 04:1x again: new pages at
// the top R1–R3 and T1 titled as they were made — the made line and that edit
// line, each by the final name — a sub-page S1 made by "/페이지" and titled,
// its own edit line alone, a row DR named, its made line and that edit line,
// and R3 renamed ten minutes on, an edit line). The names typed on the way
// show nowhere: the line has the title before the person's edit and after
// it. A full-page database's title is its source's name, whose own line
// says it ("〈DB〉 컬렉션을 생성했습니다", no edit line of its page, seen
// once), so its page's new name stays as it was.

// workspaceActivityTitleIsEdit says whether a page's new title is an edit of
// it: any page but a full-page database.
func workspaceActivityTitleIsEdit(page *core.Record) bool {
	return page.GetString("kind") != pageKindDatabase
}

// workspaceActivityTitleVersion is the version a change of a page's details
// just recorded in its history, so the edit it is knows it.
func workspaceActivityTitleVersion(app core.App, pageID string) (int, error) {
	var version int
	err := app.DB().NewQuery(`SELECT COALESCE(MAX([[version_cursor]]),0) FROM {{leafen_page_history_versions}} WHERE [[page_id]]={:page}`).
		Bind(dbx.Params{"page": pageID}).Row(&version)
	return version, err
}

// withWorkspaceActivityTitle puts a page's new title first among what a
// person's edit of it changed: its title before the edit's first version and
// at its last, where they differ, or the name a sub-page's making gave it
// (workspace_activity_sub_page.go). A database page's is its title
// property's, read with its other properties (workspace_activity_properties.go).
func withWorkspaceActivityTitle(app core.App, pageID string, row storedWorkspaceActivity, changes []workspaceActivityChange, more int) ([]workspaceActivityChange, int, error) {
	title, found, err := workspaceActivityTitleChange(app, pageID, row)
	if err != nil || !found {
		return changes, more, err
	}
	changes = append([]workspaceActivityChange{title}, changes...)
	if over := len(changes) - workspaceActivityChangeItems; over > 0 {
		changes, more = changes[:workspaceActivityChangeItems], more+over
	}
	return changes, more, nil
}

// workspaceActivityTitleChange is the title change withWorkspaceActivityTitle
// lists, if any.
func workspaceActivityTitleChange(app core.App, pageID string, row storedWorkspaceActivity) (workspaceActivityChange, bool, error) {
	made, named := workspaceActivityMadeTitle(row)
	if !row.FirstVersion.Valid || !row.LastVersion.Valid {
		return made, named, nil
	}
	first, last := int(row.FirstVersion.Int64), int(row.LastVersion.Int64)
	start, err := pageHistoryStartCursor(app, pageID)
	if err != nil || first-1 < start {
		return made, named && err == nil, err
	}
	before, err := readPageHistorySnapshotPageState(app, pageID, first-1)
	if err != nil {
		return made, false, err
	}
	after, err := readPageHistorySnapshotPageState(app, pageID, last)
	if err != nil {
		return made, false, err
	}
	if workspaceActivityName(before.Title) == workspaceActivityName(after.Title) {
		return made, named, nil
	}
	return workspaceActivityChange{Kind: workspaceActivityChangeProperty, BlockID: graphValueTypeTitle, PropertyType: graphValueTypeTitle,
		Before: workspaceActivityClip(before.Title), After: workspaceActivityClip(after.Title)}, true, nil
}
