package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// interactiveAutomationOpenPageOutcome is what an "open_page" action tells
// the screen to open: "이 페이지" — the row whose button was pressed, else
// the button's own page, as Notion's (seen 2026-11-02) — or its target
// (interactiveAutomationOpenTarget), where the action says.
func interactiveAutomationOpenPageOutcome(
	app core.App, authorization pageAuthorization, source interactiveAutomationSource, action interactiveAutomationAction,
) (interactiveAutomationOutcome, bool) {
	if automationTargetIsTrigger(action.Target) {
		opened := source.TriggerRowID
		if opened == "" {
			opened = authorization.Page.Id
		}
		return interactiveAutomationOutcome{Type: "openPage", PageID: opened, OpenIn: action.OpenIn}, true
	}
	pageID, containerID, found := interactiveAutomationOpenTarget(app, authorization, action.PageID)
	return interactiveAutomationOutcome{Type: "openPage", PageID: pageID, ContainerID: containerID, OpenIn: action.OpenIn}, found
}

// interactiveAutomationOpenTarget is what a button's or an automation's
// "열기" opens for whoever presses it: the button's own page, a page of the
// workspace they may read — a database row anywhere among them, as Notion's
// "열기" list finds rows by their names and opens the one chosen over the
// button's page (seen 2026-11-24 in two tabs, a row of another page's
// database) — a row of a database on the button's page, or a database a
// page they may read holds, named by its block's ID — Notion's "열기" list
// offers databases among the pages and opens the one chosen (seen
// 2026-11-24 in two tabs: its own view over the button's page). One held in
// a page opens alone, the page holding it and its block answered for the
// screen to open it there (inlineDatabasePeekHref); a full-page one opens
// as its page. found is false for anything else.
func interactiveAutomationOpenTarget(app core.App, authorization pageAuthorization, targetID string) (pageID, containerID string, found bool) {
	if targetID == authorization.Page.Id {
		return targetID, "", true
	}
	if target, err := authorizePage(app, authorization.Workspace.PrincipalID, targetID, permissionWorkspaceRead); err == nil {
		return targetID, "", target.Workspace.OwnerID == authorization.Workspace.OwnerID
	}
	if database, err := databaseAddressRecord(app, targetID); err == nil && database.PageID != "" {
		holder, err := authorizePage(app, authorization.Workspace.PrincipalID, database.PageID, permissionWorkspaceRead)
		if err != nil || holder.Workspace.OwnerID != authorization.Workspace.OwnerID {
			return "", "", false
		}
		if holder.Page.GetString("kind") == pageKindDatabase {
			return database.PageID, "", true
		}
		return database.PageID, database.ID, true
	}
	var count int
	err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_database_rows}} AS [[row]]
		WHERE [[row]].[[owner]] = {:owner} AND [[row]].[[page_id]] = {:pageId} AND [[row]].[[id]] = {:id}
			AND ` + databaseSourceNotTrashedSQL("[[row]].[[data_source_id]]") + `
			AND ` + databaseRowNotTrashedSQL("[[row]].[[id]]")).Bind(dbx.Params{
		"owner": authorization.Workspace.OwnerID, "pageId": authorization.Page.Id, "id": targetID,
	}).Row(&count)
	if err != nil || count != 1 {
		return "", "", false
	}
	return targetID, "", true
}
