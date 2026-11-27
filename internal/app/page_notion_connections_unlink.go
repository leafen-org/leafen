package app

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A page whose sharing is unlinked from its parent's takes no connection from
// the pages above: the connections it reached are its own copies from then
// on, as Notion's unlinked page holds its own bot permissions (seen
// 2026-11-21 in Notion's web app: "연결 해제" on a connection a page has from
// its parent asks "상위 페이지에서 〈이름〉의 연결을 해제하고 공유 설정을
// 해제하시겠습니까?", and yes unlinks the page's sharing, keeps the other
// connections as the page's own — their "›" no longer names a "연결 출처" —
// and sets that one to none on the page). Relinking takes the page's copies
// the pages above cover away again, as it does its shares
// (page_share_inheritance.go).

// pageNotionGrantsIn are the workspace's active connections listing any of ids.
func pageNotionGrantsIn(app core.App, workspaceID string, ids []string) ([]string, error) {
	grants := []string{}
	if len(ids) == 0 {
		return grants, nil
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	err = app.DB().NewQuery(`
		SELECT DISTINCT [[resource]].[[grant_id]] FROM {{leafen_notion_oauth_grant_resources}} [[resource]]
		JOIN {{leafen_notion_oauth_grants}} [[grant]] ON [[grant]].[[id]]=[[resource]].[[grant_id]]
		WHERE [[grant]].[[workspace_id]]={:workspaceId} AND [[grant]].[[status]]='active'
			AND [[resource]].[[resource_id]] IN (SELECT [[value]] FROM json_each({:ids}))
		ORDER BY [[resource]].[[grant_id]]
	`).Bind(dbx.Params{"workspaceId": workspaceID, "ids": string(encoded)}).Column(&grants)
	return grants, err
}

// listPageInNotionConnections puts the page in each of the connections.
func listPageInNotionConnections(app core.App, pageID string, grants []string) error {
	for _, grantID := range grants {
		if _, err := app.DB().NewQuery(`
			INSERT OR IGNORE INTO {{leafen_notion_oauth_grant_resources}} ([[grant_id]], [[resource_id]], [[resource_kind]])
			VALUES ({:grantId}, {:pageId}, 'page')
		`).Bind(dbx.Params{"grantId": grantID, "pageId": pageID}).Execute(); err != nil {
			return err
		}
	}
	return nil
}

// copyPageNotionConnectionsAbove gives a page whose sharing is about to be
// unlinked its own copy of every connection that reaches it, as
// cutPageShareInheritance copies its shares.
func copyPageNotionConnectionsAbove(app core.App, authorization pageAuthorization) error {
	chain, err := pageNotionReachChain(app, authorization.Page)
	if err != nil {
		return err
	}
	grants, err := pageNotionGrantsIn(app, authorization.Workspace.WorkspaceID, chain)
	if err != nil {
		return err
	}
	return listPageInNotionConnections(app, authorization.Page.Id, grants)
}

// dropPageNotionConnectionsCoveredAbove takes a relinked page out of the
// connections that reach it from above again, as its shares the pages above
// cover go.
func dropPageNotionConnectionsCoveredAbove(app core.App, authorization pageAuthorization) error {
	chain, err := pageNotionReachChain(app, authorization.Page)
	if err != nil || len(chain) < 2 {
		return err
	}
	grants, err := pageNotionGrantsIn(app, authorization.Workspace.WorkspaceID, chain[1:])
	if err != nil {
		return err
	}
	for _, grantID := range grants {
		if _, err := app.DB().NewQuery(`
			DELETE FROM {{leafen_notion_oauth_grant_resources}} WHERE [[grant_id]]={:grantId} AND [[resource_id]]={:pageId}
		`).Bind(dbx.Params{"grantId": grantID, "pageId": authorization.Page.Id}).Execute(); err != nil {
			return err
		}
	}
	return nil
}

// disconnectInheritedPageNotionConnection takes a connection the page has
// from a page above off it, as Notion's "연결 해제 및 링크 해제하기" (seen
// 2026-11-21): the page's sharing unlinks from its parent's, keeping its own
// copy of each connection it reached, and this one leaves the copy, so it
// no longer reaches the page or the pages under it. Unlinking the sharing
// takes full access to the page.
func disconnectInheritedPageNotionConnection(app core.App, authorization pageAuthorization, grantID string) error {
	chain, err := pageNotionReachChain(app, authorization.Page)
	if err != nil {
		return err
	}
	reaching, err := pageNotionGrantsIn(app, authorization.Workspace.WorkspaceID, chain)
	if err != nil {
		return err
	}
	found := false
	for _, id := range reaching {
		found = found || id == grantID
	}
	if !found {
		return publicMutationRefused("grantId's connection doesn't reach this page")
	}
	manager, err := authorizePublicMutationManager(app, authorization.Workspace.PrincipalID, authorization.Page.Id)
	if err != nil {
		return err
	}
	if err := refusePublicMutationInTrash(app, manager.Page.Id); err != nil {
		return err
	}
	if err := cutPageShareInheritance(app, manager, time.Now().UTC()); err != nil {
		return err
	}
	if _, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_notion_oauth_grant_resources}} WHERE [[grant_id]]={:grantId} AND [[resource_id]]={:pageId}
	`).Bind(dbx.Params{"grantId": grantID, "pageId": manager.Page.Id}).Execute(); err != nil {
		return err
	}
	// A database item's views read who sees it again (announceRowShareChange).
	if isDatabaseRowPageRecord(manager.Page) {
		return announceRowShareChange(app, manager.Page)
	}
	return nil
}

// copyConnectionsOntoUnlinkedPages is migration 837: each page whose
// sharing was unlinked before connections stopped at such a page gets its
// own copy of every connection that reached it then, so no connection
// loses a page it reached.
func copyConnectionsOntoUnlinkedPages(app core.App) error {
	unlinked := []struct {
		PageID      string `db:"page_id"`
		WorkspaceID string `db:"workspace_id"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[page_id]], [[workspace_id]] FROM {{leafen_page_share_unlinks}} ORDER BY [[page_id]]
	`).All(&unlinked); err != nil {
		return err
	}
	copied := 0
	for _, row := range unlinked {
		page, err := app.FindRecordById(pagesCollection, row.PageID)
		if err != nil {
			continue
		}
		chain, err := pageNotionClimb(app, page, true)
		if err != nil {
			return err
		}
		grants, err := pageNotionGrantsIn(app, row.WorkspaceID, chain)
		if err != nil {
			return err
		}
		if err := listPageInNotionConnections(app, page.Id, grants); err != nil {
			return err
		}
		copied += len(grants)
	}
	if copied > 0 {
		app.Logger().Info("connections kept on pages whose sharing is their own", "pages", len(unlinked), "connections", copied)
	}
	return nil
}

// validateConnectionsOntoUnlinkedPages is its check: the tables it reads and fills are there.
func validateConnectionsOntoUnlinkedPages(app core.App) error {
	for _, table := range []string{"leafen_page_share_unlinks", notionOAuthResourcesTable, notionOAuthGrantsTable} {
		if !app.HasTable(table) {
			return fmt.Errorf("connections on unlinked pages: the table %s is missing", table)
		}
	}
	return nil
}
