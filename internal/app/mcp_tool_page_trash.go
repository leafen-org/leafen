package app

import (
	"context"
	"errors"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/pocketbase/core"
)

// An agent puts a page it may change in the trash, and takes it back out,
// as a person does on the screen (page_metadata.go): leafen_update_page's
// trash and restore, the page's title and place as they are (2026-10-22: a
// team trial's manager left an empty page it made, finding no way to trash
// it; Notion's tools have none). The answer gives the page with its content.

// A base_cursor given holds the trash to the version it names (mcp_page_version.go).
func trashMCPPage(ctx context.Context, app core.App, workspaceID, pageID string, inTrash bool, mutationID string, base *int) (*mcp.CallToolResult, mcpPageMutationOutput, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return nil, mcpPageMutationOutput{}, err
	}
	pageID = strings.TrimSpace(pageID)
	if pageID == "" {
		return nil, mcpPageMutationOutput{}, mcpInvalid("page_id names the page")
	}
	// A database page goes as its row does (mcp_row_page_writes.go).
	if record, err := app.FindRecordById(pagesCollection, pageID); err == nil && isDatabaseRowPageRecord(record) {
		return trashMCPRowPage(ctx, app, authority, workspaceID, record, inTrash, mutationID, base)
	}
	current, err := authorizePage(app, authority.PrincipalID, pageID, permissionWorkspaceWrite)
	if isPermissionDenied(err) {
		return nil, mcpPageMutationOutput{}, mcpDenied("this account can't change that page: ask someone who can, or its owner")
	}
	if err != nil {
		return nil, mcpPageMutationOutput{}, failedBecause(errMCPToolFailed, err)
	}
	if !mcpAuthorityReachesPage(app, authority, current.Page) {
		return nil, mcpPageMutationOutput{}, errMCPOutsideTokenPages
	}
	if workspaceID = strings.TrimSpace(workspaceID); !mcpInWorkspace(app, workspaceID, current.Workspace.WorkspaceID) {
		return nil, mcpPageMutationOutput{}, mcpDenied("that page isn't in workspace %s", workspaceID)
	}
	if workspace, err := selectMCPWorkspace(app, authority, mcpWorkspaceInput{WorkspaceID: current.Workspace.WorkspaceID},
		permissionWorkspaceWrite); err != nil || workspace.WorkspaceID != current.Workspace.WorkspaceID {
		return nil, mcpPageMutationOutput{}, mcpDenied("that page is outside the workspaces this connection was given")
	}
	page, err := app.FindRecordById(pagesCollection, pageID)
	if err != nil {
		return nil, mcpPageMutationOutput{}, mcpRefusedUnlessStorage(errMCPPageMissing, err)
	}
	if err := mcpHoldPageVersion(ctx, app, authority, pageID, base); err != nil {
		return nil, mcpPageMutationOutput{}, err
	}
	if page.GetBool("in_trash") == inTrash {
		return nil, mcpPageMutationOutput{}, mcpInvalid("that page is %s already", map[bool]string{true: "in the trash", false: "out of the trash"}[inTrash])
	}
	mutationID = strings.TrimSpace(mutationID)
	if mutationID == "" {
		mutationID = "mcp-page-trash:" + newDiagnosticReference()
	}
	input := normalizePageMetadataInput(pageMetadataInput{
		Title: page.GetString("title"), ParentID: page.GetString("parent_id"), InTrash: inTrash,
		MutationID: mutationID, BaseCursor: page.GetInt("cursor"),
	})
	if !validPageMetadataMutationInput(pageID, input) {
		return nil, mcpPageMutationOutput{}, mcpInvalid("mutation_id is at most 255 characters")
	}
	response, status, err := applyPageMetadataWithLock(app, authority.PrincipalID, pageID, input, pageMetadataRequestHash(pageID, input), pageLockEnforced)
	switch status {
	case pageMetadataPageLocked:
		return nil, mcpPageMutationOutput{}, mcpInvalid("the page is locked: unlock it first")
	case pageMetadataPageMissing:
		return nil, mcpPageMutationOutput{}, errMCPPageMissing
	case pageMetadataParentConflict:
		if refusal := pageParentConflictReason(app, authority.PrincipalID, pageID, input, pageLockEnforced); refusal != nil {
			return nil, mcpPageMutationOutput{}, mcpInvalid("the page can't be put back where it was: %s", refusal.reason)
		}
		return nil, mcpPageMutationOutput{}, mcpInvalid("the page can't be put back where it was: move it with notion-move-pages")
	case pageMetadataIdentityReused:
		return nil, mcpPageMutationOutput{}, errMCPMutationIDReused
	case pageMetadataCursorConflict:
		return nil, mcpPageMutationOutput{}, errMCPCursorConflict
	case pageMetadataReplacementLocked:
		return nil, mcpPageMutationOutput{}, mcpInvalid("the page's content is being replaced: try again when that is done")
	}
	if err != nil {
		var requestError *graphRequestError
		if errors.As(err, &requestError) {
			return nil, mcpPageMutationOutput{}, mcpGraphReason(errMCPToolInvalid, err)
		}
		return nil, mcpPageMutationOutput{}, failedBecause(errMCPToolFailed, err)
	}
	return mcpTrashAnswer(ctx, app, authority, current.Workspace.OwnerID, mcpPageMutationOutput{Page: response.Page, Receipt: mutationReceiptResponse{
		MutationID: response.Receipt.MutationID, Cursor: response.Receipt.Cursor, Duplicate: response.Receipt.Duplicate,
	}})
}

// mcpTrashAnswer gives a trash or restore answer the page's content, which
// the trash keeps, as replace_content's answer gives it (2026-11-16: an
// agent read a trashed page's answer as an emptied page).
func mcpTrashAnswer(ctx context.Context, app core.App, authority mcpAuthority, ownerID string, output mcpPageMutationOutput) (*mcp.CallToolResult, mcpPageMutationOutput, error) {
	// Its content as a fetch reads it, its cursor the version it gives.
	record, err := app.FindRecordById(pagesCollection, output.Page.ID)
	content, truncated := "", false
	if err == nil {
		content, truncated, output.Page.Cursor, err = mcpFetchedPageBody(ctx, app, authority, record, ownerID, nil)
		output.Receipt.Cursor = output.Page.Cursor
	}
	if err != nil {
		return nil, mcpPageMutationOutput{}, failedBecause(errMCPPageUpdateFailed, err)
	}
	output.Page.Content, output.Truncated = content, truncated
	return nil, output, nil
}
