package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A page's own made and deleted lines say where it is as Notion's do, read
// when the line is shown, not when it was written (seen 2026-11-17 03:4x–03:5x
// in the free test workspace's '모든 워크스페이스 업데이트', in two tabs):
//   - a page at the top that everyone in the workspace is given — its general
//     access the workspace — is "워크스페이스에 〈x〉 페이지를 생성했습니다" /
//     "워크스페이스의 〈x〉 페이지를 삭제했습니다": a private page given the
//     workspace read so on its old line, and "개인 페이지로" again once it
//     was put back;
//   - any other page at the top, a teamspace's as much as a private one, is
//     "〈x〉 페이지를 개인 페이지로 생성했습니다" / "개인 페이지인 〈x〉
//     페이지를 삭제했습니다": pages made in a teamspace, made through its add
//     window or copied there, a private page moved into one and then deleted
//     there;
//   - a database's row (and a page under another, one made at the top and
//     moved there since — a sub-page's making is no line,
//     workspace_activity_sub_page.go) says neither,
//     "〈x〉 페이지를 생성했습니다" with where it is, a private database's
//     rows too.
// Those two say no place under them; the row's line does.

const (
	workspaceActivityScopePrivate   = "private"
	workspaceActivityScopeWorkspace = "workspace"
)

// workspaceActivityPageScope is which of the three a page's made or
// deleted line reads as now: "workspace", "private" or "" for a page under
// another.
func workspaceActivityPageScope(app core.App, page *core.Record) (string, error) {
	if page == nil || page.GetString("parent_id") != "" || isDatabaseRowPageRecord(page) {
		return "", nil
	}
	workspaceID := page.GetString("workspace")
	var wide bool
	err := app.DB().NewQuery(`SELECT EXISTS (SELECT 1 FROM {{leafen_page_permissions}}
		WHERE [[workspace_id]]={:workspace} AND [[page_id]]={:page} AND [[principal_type]]='workspace' AND [[principal_id]]={:workspace})`).
		Bind(dbx.Params{"workspace": workspaceID, "page": page.Id}).Row(&wide)
	if err != nil {
		return "", err
	}
	if wide {
		return workspaceActivityScopeWorkspace, nil
	}
	return workspaceActivityScopePrivate, nil
}

// workspaceActivityScoped says whether a line is a page's own making or
// deleting, whose sentence says where the page is. A page back from the trash
// is its block restored (workspace_activity_page_block.go), placed as its
// page's line is.
func workspaceActivityScoped(item workspaceActivityRecord) bool {
	if item.EntityType != "page" || item.EntityID != item.PageID {
		return false
	}
	return item.Action == "create" || item.Action == "delete"
}
