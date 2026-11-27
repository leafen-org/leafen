package app

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A change that would take something of one workspace into another is
// refused, as the user decided on 2026-11-26 (a workspace divides its
// members, their roles and a partner contract's seats, and Notion relates and
// links within one workspace too): a row into a database there, a page or a
// row under a page or into a database there, a relation to a data source
// there, a linked view of one. What takes a page over is its move whole, with
// what is under it (POST /pages/{id}/workspace-move, the user's decision of
// 2026-11-23), and its copy (POST /pages/{id}/duplicate with
// destinationWorkspaceId). Every way that refuses one says the same sentence
// and what can be done instead: create it there and copy its content over,
// or take a page over whole. A REST route answers it 400 under
// LEAFEN_OTHER_WORKSPACE, "Check <field>: …" (otherWorkspaceRefused), an MCP
// tool under LEAFEN_MCP_OTHER_WORKSPACE (mcp_other_workspace.go) — never as a
// change made meanwhile (409 changed), a place not found or missing access.
// A place in a workspace the person can't read stays a place not found.

const errorCodeOtherWorkspace = "LEAFEN_OTHER_WORKSPACE"

// otherWorkspaceSentence is the refusal's sentence, wherever it is said.
const otherWorkspaceSentence = "Leafen doesn't move or link anything between workspaces"

// How an agent creates in the other workspace what can't cross, said after
// "copy its content over".
const (
	otherWorkspaceMovedWay = " (notion-create-pages with a parent there, giving its values and its content), " +
		"then move this one to the Trash if it should go"
	otherWorkspaceLinkedWay = " — the database it would link (notion-create-database, then its rows with notion-create-pages)"
)

// workspaceCrossing is a change that would cross, as its refusal says it.
type workspaceCrossing struct {
	// subject is what would cross and place where to, as the request named
	// them; from and to are their workspaces' IDs.
	subject, from, place, to string
	// link: the subject links the place (a relation, a linked view), so what
	// it would link is created on the subject's side.
	link bool
	// whole is the page that can go over whole, with what is under it; "" names none.
	whole string
	// made: the subject is being made, so it is made there instead.
	made bool
}

// words are the refusal's words: what, where, the sentence and the ways.
func (crossing workspaceCrossing) words(app core.App) string {
	from, to := mcpWorkspaceNamed(app, crossing.from), mcpWorkspaceNamed(app, crossing.to)
	createIn, way := to, otherWorkspaceMovedWay
	if crossing.link {
		createIn, way = from, otherWorkspaceLinkedWay
	}
	text := fmt.Sprintf("%s is in workspace %s, and %s is in %s: %s. Create it in %s and copy its content over%s.",
		crossing.subject, from, crossing.place, to, otherWorkspaceSentence, createIn, way)
	if crossing.made {
		text = fmt.Sprintf("%s is in workspace %s, and %s is in %s: %s. Create it in %s instead: name that workspace (workspaceId) "+
			"with a place there.", crossing.subject, from, crossing.place, to, otherWorkspaceSentence, to)
	}
	if crossing.whole != "" {
		text += fmt.Sprintf(" A page goes to another workspace whole, with what is under it: POST /pages/%s/workspace-move moves it "+
			"(destinationWorkspaceId, dryRun true first to show its plan), POST /pages/%s/duplicate with destinationWorkspaceId copies it.",
			crossing.whole, crossing.whole)
	}
	return text
}

// otherWorkspaceRefused is a REST route's refusal of the crossing, field
// naming what in the request names the other workspace's place.
func otherWorkspaceRefused(app core.App, field string, crossing workspaceCrossing) error {
	return refusedFor(http.StatusBadRequest, errorCodeOtherWorkspace, "Check "+field+": "+crossing.words(app),
		apiErrorField{Name: field, Problem: "in another workspace"})
}

// pageInOtherWorkspace is the workspace of a page or a database's item the
// person reads, when it isn't here; "" for one of here, or one they can't read.
func pageInOtherWorkspace(app core.App, principalID, pageID, here string) string {
	if strings.TrimSpace(pageID) == "" {
		return ""
	}
	workspaceID := ""
	if row, err := authorizeDatabaseRowPage(app, principalID, pageID, permissionWorkspaceRead); err == nil {
		workspaceID = row.WorkspaceID
	} else if page, err := authorizePage(app, principalID, pageID, permissionWorkspaceRead); err == nil {
		workspaceID = page.Workspace.WorkspaceID
	}
	if workspaceID == here {
		return ""
	}
	return workspaceID
}

// dataSourceInOtherWorkspace is the workspace of a data source whose
// database's page the person reads, when it isn't here, and that page; ""
// for one of here, or one they can't read.
func dataSourceInOtherWorkspace(app core.App, principalID, dataSourceID, here string) (workspaceID, pageID string) {
	if strings.TrimSpace(dataSourceID) == "" {
		return "", ""
	}
	if err := app.DB().NewQuery(`SELECT [[page_id]] FROM {{leafen_data_sources}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": dataSourceID}).Row(&pageID); err != nil || pageID == "" {
		return "", ""
	}
	if workspaceID = pageInOtherWorkspace(app, principalID, pageID, here); workspaceID == "" {
		return "", ""
	}
	return workspaceID, pageID
}

// rowDatabasePage is the page that holds a database's item, which goes over
// whole with its database; "" when it isn't one.
func rowDatabasePage(app core.App, rowID string) string {
	pageID := ""
	_ = app.DB().NewQuery(`SELECT [[page_id]] FROM {{leafen_database_rows}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": rowID}).Row(&pageID)
	return pageID
}

// rowUnderPageCrossing refuses a database's item moved out of its database
// under a page of another workspace (targetPageId).
func rowUnderPageCrossing(app core.App, authority databaseRowPageAuthority, parentID, workspaceID string) error {
	return otherWorkspaceRefused(app, "targetPageId", workspaceCrossing{
		subject: "this row", from: authority.WorkspaceID, place: "the page " + parentID, to: workspaceID, whole: authority.ContainerPageID,
	})
}

// pageIntoDatabaseRefusal refuses a page moved into a database of another
// workspace (dataSourceId).
func pageIntoDatabaseRefusal(app core.App, request pageToDatabaseRowRequest, from, to string) error {
	return otherWorkspaceRefused(app, "dataSourceId", workspaceCrossing{
		subject: "this page", from: from, place: "the data source " + request.DataSourceID, to: to, whole: request.PageID,
	})
}
