package app

import (
	"context"
	"errors"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// MCP's page tools take a database page as they take a page, under the same
// rules: no base_cursor, no contention check; one given, checked against the
// cursor the tools gave. A database page is written through its row's
// writers, as the screen writes it, since the page metadata writer opens one
// only to read (2026-11-12: an agent's notion-update-page of a database
// page's width was always refused as a cursor conflict, and its
// leafen_update_page trash as a page it couldn't change).

// mcpPageResponse is a page as an MCP page write answers with it; an answer
// that gives its cursor gives the page's write version (mcp_page_version.go).
func mcpPageResponse(app core.App, pageID string) (pageResponse, error) {
	record, err := app.FindRecordById(pagesCollection, pageID)
	if err != nil {
		return pageResponse{}, err
	}
	page, err := mcpPageRecordResponse(app, record)
	if err != nil {
		return pageResponse{}, err
	}
	return page, err
}

// mcpPageRecordResponse is a page record as MCP's tools answer with it, its
// trash state the one trash rule's (mcpPageTrashState).
func mcpPageRecordResponse(app core.App, record *core.Record) (pageResponse, error) {
	page, err := pageRecordResponseWithWiki(app, record)
	if err == nil {
		err = mcpPageTrashState(app, &page)
	}
	return page, err
}

// mcpPageTrashState gives a page as MCP's tools answer with it the trash
// state of the one trash rule (pageInTrashSQL): a database page is in the
// trash with its row, its data source or its deleted database, and a
// template's page with its template, while their records keep in_trash for
// a page alone (2026-11-17: leafen_update_page's trash of a database page
// answered inTrash false, and the next call said it was in the trash).
func mcpPageTrashState(app core.App, page *pageResponse) error {
	state := struct {
		InTrash   bool   `db:"in_trash"`
		TrashedAt string `db:"trashed_at"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT ` + pageInTrashSQL("[[page]]") + ` AS [[in_trash]], COALESCE(` + pageTrashedAtSQL("[[page]]") + `, '') AS [[trashed_at]]
		FROM {{leafen_pages}} AS [[page]] WHERE [[page]].[[id]] = {:id}
	`).Bind(dbx.Params{"id": page.ID}).One(&state); err != nil {
		return err
	}
	page.InTrash, page.TrashedAt = state.InTrash, optionalPageTrashTimestamp(state.TrashedAt)
	return nil
}

// trashMCPRowPage puts a database page in the trash or takes it back out, as
// the row's menu does (changeDatabaseRowTrash): with its sub-items, under the
// row's write permission, at the row's revision as it is now.
func trashMCPRowPage(
	ctx context.Context, app core.App, authority mcpAuthority, workspaceID string, page *core.Record, inTrash bool, mutationID string, base *int,
) (*mcp.CallToolResult, mcpPageMutationOutput, error) {
	persistence, ok := app.Store().Get(ygoPersistenceStoreKey).(*pocketBaseYgoPersistence)
	if !ok || persistence == nil {
		return nil, mcpPageMutationOutput{}, errMCPToolFailed
	}
	row, err := authorizeDatabaseRowPageInTrash(app, authority.PrincipalID, page.Id, permissionWorkspaceWrite)
	if isPermissionDenied(err) || errors.Is(err, errDatabaseRowPageNotFound) {
		return nil, mcpPageMutationOutput{}, mcpDenied("this account can't change that page: ask someone who can, or its owner")
	}
	if err != nil {
		return nil, mcpPageMutationOutput{}, failedBecause(errMCPToolFailed, err)
	}
	if !mcpAuthorityReachesPage(app, authority, page) {
		return nil, mcpPageMutationOutput{}, errMCPOutsideTokenPages
	}
	if workspaceID = strings.TrimSpace(workspaceID); !mcpInWorkspace(app, workspaceID, row.WorkspaceID) {
		return nil, mcpPageMutationOutput{}, mcpDenied("that page isn't in workspace %s", workspaceID)
	}
	if workspace, err := selectMCPWorkspace(app, authority, mcpWorkspaceInput{WorkspaceID: row.WorkspaceID},
		permissionWorkspaceWrite); err != nil || workspace.WorkspaceID != row.WorkspaceID {
		return nil, mcpPageMutationOutput{}, mcpDenied("that page is outside the workspaces this connection was given")
	}
	if err := mcpHoldPageVersion(ctx, app, authority, page.Id, base); err != nil {
		return nil, mcpPageMutationOutput{}, err
	}
	if (row.TrashedAt != 0) == inTrash {
		return nil, mcpPageMutationOutput{}, mcpInvalid("that page is %s already", map[bool]string{true: "in the trash", false: "out of the trash"}[inTrash])
	}
	if mutationID = strings.TrimSpace(mutationID); mutationID == "" {
		mutationID = "mcp-page-trash:" + newDiagnosticReference()
	}
	if len([]rune(mutationID)) > 255 {
		return nil, mcpPageMutationOutput{}, mcpInvalid("mutation_id is at most 255 characters")
	}
	response, err := persistence.changeDatabaseRowTrash(ctx, authority.PrincipalID, page.Id, databaseRowPageActionInput{
		RequestID: mutationID, ExpectedAuthorityRevision: row.AuthorityRevision,
	}, inTrash)
	if err != nil {
		return nil, mcpPageMutationOutput{}, mcpDatabasePageError(err)
	}
	answer, err := mcpPageResponse(app, page.Id)
	if err != nil {
		return nil, mcpPageMutationOutput{}, failedBecause(errMCPPageUpdateFailed, err)
	}
	return mcpTrashAnswer(ctx, app, authority, row.OwnerID, mcpPageMutationOutput{Page: answer, Receipt: mutationReceiptResponse{
		MutationID: mutationID, Cursor: answer.Cursor, Duplicate: response.Duplicate,
	}})
}
