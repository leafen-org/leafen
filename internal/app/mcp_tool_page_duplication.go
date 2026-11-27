package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/pocketbase/core"
)

type notionMCPDuplicatePageInput struct {
	PageID string `json:"page_id" jsonschema:"ID of the page to duplicate"`
}

type notionMCPDuplicatePageOperation struct {
	Surface       string `json:"surface"`
	OperationName string `json:"name"`
}

// notionMCPDuplicatePageOutput is the copy as Notion's answer names it
// (2026-11-21): its ID and address. A page's copy fills in behind the answer,
// as Notion's does; a database page's is made when the call answers.
type notionMCPDuplicatePageOutput struct {
	PageID  string `json:"page_id"`
	PageURL string `json:"page_url"`
}

func duplicateMCPPage(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	app core.App,
	input notionMCPDuplicatePageInput,
) (*mcp.CallToolResult, notionMCPDuplicatePageOutput, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return nil, notionMCPDuplicatePageOutput{}, err
	}
	// A page goes by any ID or address the other tools take (mcpLocalID).
	pageID := mcpLocalID(app, strings.TrimSpace(input.PageID))
	if pageID == "" || len(pageID) > maxWorkspacePermissionIdentifierLength {
		return nil, notionMCPDuplicatePageOutput{}, mcpInvalid("page_id names the page to copy")
	}
	// The source needs only read access, as a current duplication fences its
	// writes; admitPageDuplicationFrom remains the single owner of mutation
	// authorization; MCP copies regardless of page locks, as Notion's API does.
	authorization, err := authorizePage(app, authority.PrincipalID, pageID, permissionWorkspaceRead)
	if err != nil {
		return nil, notionMCPDuplicatePageOutput{}, mcpPageDuplicationError(err)
	}
	workspace, err := selectMCPWorkspace(app, authority, mcpWorkspaceInput{
		WorkspaceID: authorization.Workspace.WorkspaceID,
	}, permissionWorkspaceWrite)
	if err != nil || workspace.WorkspaceID != authorization.Workspace.WorkspaceID {
		return nil, notionMCPDuplicatePageOutput{}, mcpRefusedUnlessStorage(errMCPOutsideConnection, err)
	}
	requestID := mcpPageDuplicationRequestID(authority)
	if isDatabaseRowPageRecord(authorization.Page) {
		return duplicateMCPDatabaseRowPage(ctx, app, authority, pageID, requestID)
	}
	admission, err := admitPageDuplicationFrom(
		app, pageDuplicationFromIntegration, authority.PrincipalID, pageID, pageDuplicationInput{
			RequestID: requestID, DestinationParentID: authorization.Page.GetString("parent_id"),
		},
	)
	if err != nil {
		return nil, notionMCPDuplicatePageOutput{}, mcpPageDuplicationError(err)
	}
	response, err := pageDuplicationResponseForJob(admission.Job, admission.Duplicate)
	if err != nil {
		return nil, notionMCPDuplicatePageOutput{}, failedBecause(errMCPToolFailed, err)
	}
	if runner, ok := app.Store().Get(durableJobRunnerStoreKey).(*durableJobRunner); ok {
		runner.notify()
	}
	return nil, notionMCPDuplicatePageOutput{
		PageID: response.TargetRootPageID, PageURL: mcpPageURL(authority, response.TargetRootPageID),
	}, nil
}

// duplicateMCPDatabaseRowPage copies a database page as the app's
// duplicate does: with its sub-items, into its data source right after it,
// titled with " (1)" and the rest, as Notion's MCP copies one (seen
// 2026-11-14: "row with synced (1)" in the same data source). The copy is
// made when the call answers, so there is no task to poll.
func duplicateMCPDatabaseRowPage(
	ctx context.Context, app core.App, authority mcpAuthority, rowID, requestID string,
) (*mcp.CallToolResult, notionMCPDuplicatePageOutput, error) {
	persistence, ok := app.Store().Get(ygoPersistenceStoreKey).(*pocketBaseYgoPersistence)
	if !ok || persistence == nil {
		return nil, notionMCPDuplicatePageOutput{}, errMCPToolFailed
	}
	preliminary, err := authorizeDatabaseRowPage(app, authority.PrincipalID, rowID, permissionWorkspaceWrite)
	if err != nil {
		return nil, notionMCPDuplicatePageOutput{}, mcpDatabasePageError(err)
	}
	_, _, err = persistence.copyDatabaseRowPage(ctx, app, authority.PrincipalID, rowID, databaseRowPageActionInput{
		RequestID: requestID, ExpectedAuthorityRevision: preliminary.AuthorityRevision,
	}, preliminary)
	if err != nil {
		return nil, notionMCPDuplicatePageOutput{}, mcpDatabasePageError(err)
	}
	copyID := databaseRowPageDuplicateRowID(authority.PrincipalID, rowID, requestID)
	return nil, notionMCPDuplicatePageOutput{PageID: copyID, PageURL: mcpPageURL(authority, copyID)}, nil
}

func mcpPageDuplicationRequestID(authority mcpAuthority) string {
	seed := mcpCallSeed()
	prefixDigest := sha256.Sum256([]byte(
		"leafen:mcp-page-duplication-connection:v1\x00" + authority.ConnectionID,
	))
	digest := sha256.Sum256([]byte(
		"leafen:mcp-page-duplication:v1\x00" + authority.ConnectionID + "\x00" + seed,
	))
	return "mcp-page-duplication-" + hex.EncodeToString(prefixDigest[:])[:16] + "-" + hex.EncodeToString(digest[:])
}

func mcpPageDuplicationRequestPrefix(connectionID string) string {
	digest := sha256.Sum256([]byte(
		"leafen:mcp-page-duplication-connection:v1\x00" + connectionID,
	))
	return "mcp-page-duplication-" + hex.EncodeToString(digest[:])[:16] + "-"
}

func mcpPageDuplicationError(err error) error {
	switch {
	case err == nil:
		return nil
	case isPermissionDenied(err), errors.Is(err, errPageDuplicationNotFound):
		return mcpDenied("this account can't copy that page, or it is gone")
	case errors.Is(err, errPageDuplicationInTrash):
		return mcpInvalid("that page is in the trash, or under a page that is — restore that page from the trash (notion-restore-pages), then copy it")
	case errors.Is(err, errPageDuplicationInvalid),
		errors.Is(err, errPageDuplicationRequestIDReused),
		errors.Is(err, errPageDuplicationConflict),
		errors.Is(err, errPageDuplicationUnsupported):
		return mcpInvalid("that page can't be copied now: it holds what a copy can't take, a copy of it is under way, or this request's ID was used for another")
	default:
		return failedBecause(errMCPToolFailed, err)
	}
}
