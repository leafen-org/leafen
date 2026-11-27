package app

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/pocketbase/core"
)

const mcpMovePagesMaximumItems = 100

type notionMCPMovePagesParent struct {
	Type         string `json:"type,omitempty" jsonschema:"workspace to move the pages to the top of your private pages; otherwise page_id or data_source_id, or left out"`
	PageID       string `json:"page_id,omitempty" jsonschema:"Destination parent page ID"`
	DataSourceID string `json:"data_source_id,omitempty" jsonschema:"Destination database: its collection:// data source URL or bare Leafen data source ID; each page becomes one of its pages"`
}

type notionMCPMovePagesInput struct {
	NewParent         notionMCPMovePagesParent `json:"new_parent" jsonschema:"Destination page, a database's data source, or type workspace for the top of your private pages (database pages can't move there); exactly one; database_id parents are not supported by this Leafen vertical"`
	PageOrDatabaseIDs []string                 `json:"page_or_database_ids" jsonschema:"One to 100 page IDs moved atomically in the requested order; database IDs are not supported by this Leafen vertical"`
}

// leafenMCPMovePagesInput is the Leafen-native endpoint's move: a database's
// item leaves its database for a page only with leave_database (2026-11-16:
// an agent moved one under a page unawares). The Notion-compatible endpoint
// keeps Notion's fields and its move, which makes such an item a page of its
// own (measured 2026-11-16 through Notion's API), its answer saying so.
type leafenMCPMovePagesInput struct {
	NewParent         notionMCPMovePagesParent `json:"new_parent" jsonschema:"Destination page, a database's data source, or type workspace for the top of your private pages (database pages can't move there); exactly one; database_id parents are not supported by this Leafen vertical"`
	PageOrDatabaseIDs []string                 `json:"page_or_database_ids" jsonschema:"One to 100 page IDs moved atomically in the requested order; database IDs are not supported by this Leafen vertical"`
	LeaveDatabase     bool                     `json:"leave_database,omitzero" jsonschema:"true to move a database's item under a page, out of its database as a page of its own; without it such a move is refused and nothing moves"`
	DropValues        bool                     `json:"drop_values,omitzero" jsonschema:"true to move a database's item into another database even where that database lacks some of its properties, their values left behind; without it such a move is refused, naming them, and nothing moves"`
}

type notionMCPMovedPageParent struct {
	Type         string `json:"type"`
	PageID       string `json:"page_id,omitempty"`
	DataSourceID string `json:"data_source_id,omitempty"`
}

type notionMCPMovedPage struct {
	ID        string                   `json:"id"`
	Title     string                   `json:"title"`
	URL       string                   `json:"url"`
	Parent    notionMCPMovedPageParent `json:"parent"`
	Duplicate bool                     `json:"duplicate"`
	Warning   string                   `json:"warning,omitempty"`
}

type notionMCPMovePagesOutput struct {
	Moved []notionMCPMovedPage `json:"moved"`
}

func registerMCPPageMoveTool(server *mcp.Server, app core.App, surface mcpToolSurface) {
	tool := &mcp.Tool{
		Name: "notion-move-pages", Title: "Move Leafen pages",
		Description: "Move one to 100 authorized pages under one authorized destination page, atomically, into one database — a database's items too, as rows into another database — " +
			"or to the top of your private pages, which leaves them shared with no one, within their workspace: to another, Leafen's API " +
			"moves a page (POST /pages/{id}/workspace-move, leafen_call) or copies one (POST /pages/{id}/duplicate with destinationWorkspaceId). " +
			"A database page moved under a page leaves its database as a sub-page, its property values kept should it go back; each such move is its own change.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true},
	}
	if surface == mcpToolSurfaceNative {
		tool.Description = strings.Replace(tool.Description, "A database page moved under a page leaves", "A database page moves under a page only with leave_database true: it leaves", 1)
		mcp.AddTool(server, tool, func(ctx context.Context, request *mcp.CallToolRequest, input leafenMCPMovePagesInput) (*mcp.CallToolResult, notionMCPMoveAnswer, error) {
			result, output, err := moveMCPPages(ctx, request, app, notionMCPMovePagesInput{NewParent: input.NewParent, PageOrDatabaseIDs: input.PageOrDatabaseIDs},
				input.LeaveDatabase, input.DropValues)
			return mcpMoveAnswered(result, output, mcpNamedRefusal(ctx, app, err, mcpMovedPagesNamed(input.PageOrDatabaseIDs, input.NewParent)...))
		})
		return
	}
	mcp.AddTool(server, tool, func(ctx context.Context, request *mcp.CallToolRequest, input notionMCPMovePagesInput) (*mcp.CallToolResult, notionMCPMoveAnswer, error) {
		result, output, err := moveMCPPages(ctx, request, app, input, true, false)
		return mcpMoveAnswered(result, output, mcpNamedRefusal(ctx, app, err, mcpMovedPagesNamed(input.PageOrDatabaseIDs, input.NewParent)...))
	})
}

// mcpMovedPagesNamed are the pages a move names: those to move, then the new
// parent page.
func mcpMovedPagesNamed(pageIDs []string, parent notionMCPMovePagesParent) []mcpNamedTarget {
	return append(mcpPages(pageIDs), mcpPage(parent.PageID))
}

func moveMCPPages(
	ctx context.Context,
	request *mcp.CallToolRequest,
	app core.App,
	input notionMCPMovePagesInput,
	leaveDatabase, dropValues bool,
) (*mcp.CallToolResult, notionMCPMovePagesOutput, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return nil, notionMCPMovePagesOutput{}, err
	}
	// The public API's IDs name the pages and the parent page as the tools'
	// do (mcpLocalID), as notion-create-pages takes them.
	if strings.TrimSpace(input.NewParent.PageID) != "" {
		input.NewParent.PageID = mcpLocalID(app, input.NewParent.PageID)
	}
	written := append([]string(nil), input.PageOrDatabaseIDs...) // as the agent wrote them, for a refusal
	pageOrDatabaseIDs := make([]string, len(input.PageOrDatabaseIDs))
	for index, id := range input.PageOrDatabaseIDs {
		pageOrDatabaseIDs[index] = id
		if strings.TrimSpace(id) != "" {
			pageOrDatabaseIDs[index] = mcpLocalID(app, id)
		}
	}
	input.PageOrDatabaseIDs = pageOrDatabaseIDs
	parentID := strings.TrimSpace(input.NewParent.PageID)
	toPrivate := strings.TrimSpace(input.NewParent.Type) == "workspace"
	if source := strings.TrimSpace(input.NewParent.DataSourceID); source != "" {
		if parentID != "" || toPrivate {
			return nil, notionMCPMovePagesOutput{}, mcpInvalid("new_parent names one place: a data_source_id, or a page_id, or the workspace")
		}
		return moveMCPPagesIntoDataSource(ctx, request, app, authority, source, input.PageOrDatabaseIDs, written, dropValues)
	}
	pageIDs, valid := normalizeMCPMovePageIDs(input.PageOrDatabaseIDs, parentID)
	if toPrivate == (parentID != "") || len(parentID) > maxWorkspacePermissionIdentifierLength || !valid {
		return nil, notionMCPMovePagesOutput{}, mcpInvalid("new_parent is a page (page_id) or the workspace (type workspace), and page_or_database_ids lists the pages to move — none the new parent itself, at most %d", mcpMovePagesMaximumItems)
	}
	// The workspace parent adds the pages to the mover's private pages, as
	// Notion's does, in the workspace of the first page.
	placeID, teamspaceID := parentID, (*string)(nil)
	if toPrivate {
		placeID, teamspaceID = "", new(string)
	}
	anchor, err := authorizePage(app, authority.PrincipalID, cmp.Or(parentID, pageIDs[0]), permissionWorkspaceRead)
	if err != nil {
		return nil, notionMCPMovePagesOutput{}, mcpPageMoveError(err, pageMetadataApplied, nil)
	}
	workspace, err := selectMCPWorkspace(app, authority, mcpWorkspaceInput{
		WorkspaceID: anchor.Workspace.WorkspaceID,
	}, permissionWorkspaceWrite)
	if err != nil || workspace.WorkspaceID != anchor.Workspace.WorkspaceID {
		return nil, notionMCPMovePagesOutput{}, mcpDenied("the new place is outside the workspaces this connection was given, or this account can't change it")
	}
	seed := mcpCallSeed()
	batchHash := mcpPageMoveBatchHash(cmp.Or(parentID, "\x00private"), pageIDs)
	moved := map[int]notionMCPMovedPage{}
	if toPrivate {
		// A database page leaves its database only for a page; all go to the first's workspace.
		for index, pageID := range pageIDs {
			if row, err := app.FindRecordById(pagesCollection, pageID); err == nil && isDatabaseRowPageRecord(row) {
				return nil, notionMCPMovePagesOutput{}, mcpInvalid("a database's item leaves its database only for a page: give new_parent a page_id")
			} else if err == nil && row.GetString("workspace") != workspace.WorkspaceID {
				return nil, notionMCPMovePagesOutput{}, mcpMovedAcross(app, written[index], pageID, row.GetString("workspace"), workspace.WorkspaceID, false)
			}
		}
	} else if moved, err = moveMCPDatabasePagesOut(
		ctx, app, authority, workspace.WorkspaceID, parentID, seed, batchHash, pageIDs, written, leaveDatabase,
	); err != nil {
		return nil, notionMCPMovePagesOutput{}, err
	}
	output := notionMCPMovePagesOutput{Moved: make([]notionMCPMovedPage, 0, len(pageIDs))}
	err = app.RunInTransaction(func(txApp core.App) error {
		defer namePageHistoryConnection(txApp, ctx)()
		for index, pageID := range pageIDs {
			if entry, converted := moved[index]; converted {
				output.Moved = append(output.Moved, entry)
				continue
			}
			authorization, err := authorizePage(
				txApp, authority.PrincipalID, pageID, permissionWorkspaceRead,
			)
			if err != nil || authorization.Workspace.WorkspaceID != workspace.WorkspaceID {
				return &mcpPageMoveFailure{err: err, status: pageMetadataPageMissing}
			}
			position, err := nextPageSiblingPosition(txApp, workspace.WorkspaceID, placeID)
			if err != nil {
				return err
			}
			metadataInput := normalizePageMetadataInput(pageMetadataInput{
				Title: authorization.Page.GetString("title"), ParentID: placeID, TeamspaceID: teamspaceID,
				InTrash: authorization.Page.GetBool("in_trash"), Position: &position,
				MutationID: mcpPageMoveMutationID(authority.ConnectionID, seed, index),
				BaseCursor: authorization.Page.GetInt("cursor"),
			})
			if !validPageMetadataMutationInput(pageID, metadataInput) {
				// A page named as its own new parent (the tool's input check
				// refuses it first); the move's other input is the page's own.
				if placeID == pageID {
					return &mcpPageMoveFailure{status: pageMetadataParentConflict, refusal: refusedPlace(true, "the new parent is the page itself or a page inside it", false, false)}
				}
				return &mcpPageMoveFailure{}
			}
			response, status, err := applyPageMetadataTransaction(
				txApp, authority.PrincipalID, pageID, metadataInput, batchHash,
			)
			if err != nil || status != pageMetadataApplied {
				failure := &mcpPageMoveFailure{err: err, status: status}
				if status == pageMetadataParentConflict {
					failure.refusal = pageParentConflictReason(txApp, authority.PrincipalID, pageID, metadataInput, pageLockIgnored)
				}
				return failure
			}
			parent := notionMCPMovedPageParent{Type: "page_id", PageID: response.Page.ParentID}
			if toPrivate {
				parent = notionMCPMovedPageParent{Type: "workspace"}
			}
			output.Moved = append(output.Moved, notionMCPMovedPage{
				ID: response.Page.ID, Title: response.Page.Title,
				URL: mcpPageAddress(txApp, authority, response.Page.ID), Parent: parent, Duplicate: response.Receipt.Duplicate,
			})
		}
		return nil
	})
	if err != nil {
		failure := &mcpPageMoveFailure{}
		if errors.As(err, &failure) {
			return nil, notionMCPMovePagesOutput{}, mcpPageMoveError(failure.err, failure.status, failure.refusal)
		}
		return nil, notionMCPMovePagesOutput{}, mcpPageMoveError(err, pageMetadataApplied, nil)
	}
	return nil, output, nil
}

func normalizeMCPMovePageIDs(input []string, parentID string) ([]string, bool) {
	if len(input) == 0 || len(input) > mcpMovePagesMaximumItems {
		return nil, false
	}
	seen := make(map[string]struct{}, len(input))
	pageIDs := make([]string, len(input))
	for index, rawID := range input {
		pageID := strings.TrimSpace(rawID)
		if pageID == "" || len(pageID) > maxWorkspacePermissionIdentifierLength || pageID == parentID {
			return nil, false
		}
		if _, duplicate := seen[pageID]; duplicate {
			return nil, false
		}
		seen[pageID] = struct{}{}
		pageIDs[index] = pageID
	}
	return pageIDs, true
}

type mcpPageMoveFailure struct {
	err     error
	status  pageMetadataMutationStatus
	refusal *pageParentRefusal
}

func (failure *mcpPageMoveFailure) Error() string {
	return "mcp page move admission failed"
}

func mcpPageMoveMutationID(connectionID, seed string, index int) string {
	digest := sha256.Sum256([]byte(
		"leafen:mcp-page-move:v1\x00" + connectionID + "\x00" + seed + "\x00" + strconv.Itoa(index),
	))
	return "mcp-page-move-" + hex.EncodeToString(digest[:])
}

func mcpPageMoveBatchHash(parentID string, pageIDs []string) string {
	payload := "leafen:mcp-page-move-batch:v1\x00" + parentID
	for _, pageID := range pageIDs {
		payload += "\x00" + strconv.Itoa(len(pageID)) + ":" + pageID
	}
	digest := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(digest[:])
}

func mcpPageMoveError(err error, status pageMetadataMutationStatus, refusal *pageParentRefusal) error {
	if isPermissionDenied(err) || status == pageMetadataPageMissing {
		return mcpDenied("this account can't move that page, or it is gone")
	}
	// A replacement of the page's content under way is said as
	// notion-update-page's trash says it (mcp_tool_page_trash.go), not as a
	// failure to try once more.
	if status == pageMetadataReplacementLocked {
		return mcpInvalid("the page's content is being replaced: try again when that is done")
	}
	// The place's own reason, read again by the check the move ran
	// (page_metadata_place.go); none found again, the place changed meanwhile.
	if status == pageMetadataParentConflict && refusal != nil {
		return mcpInvalid("the new parent can't take it: %s", refusal.reason)
	}
	if status == pageMetadataParentConflict {
		return mcpInvalid("the new parent refused it but no longer says why, so it changed meanwhile: fetch the parent and the page, then move it again")
	}
	// A change met at the page's cursor, a move's ID met before (each call
	// moves under IDs of its own), or what failed: the call's failure, its
	// cause kept.
	return failedBecause(errMCPToolFailed, err)
}
