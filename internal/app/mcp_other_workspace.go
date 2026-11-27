package app

import (
	"cmp"
	"fmt"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A change that would take something of one workspace into another — a page
// moved under a page there, a database's item into a database there, a
// relation to a row there — is refused as what it is, under its own code,
// with the ways that do something like it, never as something the connection
// can't reach (2026-11-24, an agent on an administrator's connection reaching
// both workspaces: notion-move-pages to a parent in the other answered
// LEAFEN_MCP_SCOPE_DENIED, read as missing access). Its words are the REST
// routes' (other_workspace_refusal.go): Leafen doesn't move or link anything
// between workspaces; create it there and copy its content over, or take a
// page over whole (POST /pages/{id}/workspace-move, planned first with
// dryRun, or POST /pages/{id}/duplicate with destinationWorkspaceId), a
// database's item with its database. The refusal passes wherever a refusal
// that says why passes (mcpReasoned).

const errorCodeMCPOtherWorkspace = "LEAFEN_MCP_OTHER_WORKSPACE"

type mcpOtherWorkspaceKind struct{}

func (mcpOtherWorkspaceKind) Error() string { return errorCodeMCPOtherWorkspace }

// Is makes it a refusal of the input, as each tool's error handling knows one.
func (mcpOtherWorkspaceKind) Is(target error) bool { return target == errMCPToolInvalid }

var errMCPOtherWorkspace error = mcpOtherWorkspaceKind{}

// mcpOtherWorkspace is such a refusal, saying what and the way.
func mcpOtherWorkspace(format string, values ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{errMCPOtherWorkspace}, values...)...)
}

// mcpWorkspaceNamed is a workspace as a refusal names it: its name and ID.
func mcpWorkspaceNamed(app core.App, workspaceID string) string {
	if workspace, err := app.FindRecordById(workspacesCollection, workspaceID); err == nil {
		return fmt.Sprintf("“%s” (%s)", workspace.GetString("name"), workspaceID)
	}
	return workspaceID
}

// mcpMovedAcross refuses moving a page, or a database's item (row), named as
// written, from its workspace to a place in another.
func mcpMovedAcross(app core.App, written, pageID, fromWorkspaceID, toWorkspaceID string, row bool) error {
	crossing := workspaceCrossing{subject: written, from: fromWorkspaceID, place: "the new place", to: toWorkspaceID, whole: pageID}
	if row {
		crossing.subject += ", an item of a database,"
		crossing.whole = cmp.Or(rowDatabasePage(app, pageID), "<the database's page>")
	}
	return mcpOtherWorkspace("%s", crossing.words(app))
}

// mcpLinkedAcross refuses linking, as what ("a relation", "a linked view"), a
// data source named as written, of one workspace, from a place in another.
func mcpLinkedAcross(app core.App, what, written, sourceWorkspaceID, placeWorkspaceID string) error {
	crossing := workspaceCrossing{subject: what + " made here", from: placeWorkspaceID, place: written, to: sourceWorkspaceID, link: true}
	if dataSourceID, ok := mcpLocalDataSourceID(app, written); ok {
		_ = app.DB().NewQuery(`SELECT [[page_id]] FROM {{leafen_data_sources}} WHERE [[id]] = {:id}`).
			Bind(dbx.Params{"id": dataSourceID}).Row(&crossing.whole)
	}
	return mcpOtherWorkspace("%s", crossing.words(app))
}

// mcpViewSourceError is why a view tab can't show a data source: one of
// another workspace than its database's says so; else mcpDataSourceQueryError's.
func mcpViewSourceError(app core.App, err error, databaseWorkspaceID, dataSourceID, written string) error {
	if location, loadErr := loadMCPDataSourceLocation(app, dataSourceID); loadErr == nil && location.WorkspaceID != databaseWorkspaceID {
		return mcpLinkedAcross(app, "a view", written, location.WorkspaceID, databaseWorkspaceID)
	}
	return mcpDataSourceQueryError(err)
}
