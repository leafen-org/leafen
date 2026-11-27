package app

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

func notionBlockRestoreOnlyRequest(body map[string]json.RawMessage) bool {
	if len(body) != 1 {
		return false
	}
	raw, ok := body["in_trash"]
	if !ok {
		return false
	}
	inTrash := true
	return json.Unmarshal(raw, &inTrash) == nil && !inTrash
}

// restoreNotionBlock answers in_trash false on a block: one in the block
// trash goes back last in the parent it left (restoreAtParentEnd).
func restoreNotionBlock(
	event *core.RequestEvent,
	ygo *pocketBaseYgoPersistence,
	requestedBlockID string,
) error {
	blockID, ok := resolveNotionBlockID(event.App, strings.TrimSpace(requestedBlockID))
	if !ok {
		return notionBlockNotFound(event)
	}
	workspace, authorized, err := authorizeNotionWorkspaceOperation(
		event, "pages:write", permissionWorkspaceWrite,
	)
	if err != nil || !authorized {
		return err
	}
	active, activeFound, err := loadStoredNotionBlock(event.App, blockID)
	if err != nil {
		return notionRequestNotCompleted(event, err)
	}
	if activeFound {
		// Taking a block in a page in the trash out is refused as any change
		// under it is (notion_trash_refusal.go).
		if refused, err := refuseNotionTrashedBlockBodyEdit(event, workspace.Identity, active.PageID); refused {
			return err
		}
		page, err := authorizePageBody(
			event.App, workspace.Identity.PrincipalID, active.PageID, permissionWorkspaceWrite,
		)
		if isPermissionDenied(err) {
			return notionBlockNotFound(event)
		}
		if err != nil {
			return notionRequestNotCompleted(event, err)
		}
		if page.Workspace.WorkspaceID != workspace.Identity.WorkspaceID ||
			active.WorkspaceID != workspace.Identity.WorkspaceID ||
			active.Owner != page.Workspace.OwnerID || page.Page.GetBool("in_trash") ||
			!notionOAuthIdentityAllowsPage(event.App, workspace.Identity, page) {
			return notionBlockNotFound(event)
		}
		return writeCurrentNotionBlock(event, notionBlockAuthorization{
			Identity: workspace.Identity, Page: page, Block: active,
		})
	}
	tombstone, found, err := loadRecoverableBlockTrash(event.App, blockID)
	if err != nil {
		return notionRequestNotCompleted(event, err)
	}
	if !found || tombstone.WorkspaceID != workspace.Identity.WorkspaceID {
		// A deleted database's block comes back as the trash restores it.
		if read, deleted, err := loadNotionDeletedDatabaseBlockRead(event.App, workspace.Identity, blockID); err != nil {
			return notionBlockProjectionError(event, err)
		} else if deleted {
			return restoreNotionDeletedDatabaseBlock(event, ygo, workspace.Identity, read)
		}
		// One that went to the trash inside another is refused as having an
		// archived ancestor (notion_trash_refusal.go).
		if refused, answer := refuseNotionTrashedBlockEdit(event, workspace.Identity, blockID); refused {
			return answer
		}
		return notionBlockNotFound(event)
	}
	if refused, err := refuseNotionTrashedBlockBodyEdit(event, workspace.Identity, tombstone.PageID); refused {
		return err
	}
	page, err := authorizePageBody(
		event.App, workspace.Identity.PrincipalID, tombstone.PageID, permissionWorkspaceWrite,
	)
	if isPermissionDenied(err) {
		return notionBlockNotFound(event)
	}
	if err != nil {
		return notionRequestNotCompleted(event, err)
	}
	if page.Workspace.WorkspaceID != workspace.Identity.WorkspaceID ||
		page.Workspace.OwnerID != tombstone.OwnerID || page.Page.GetBool("in_trash") ||
		!notionOAuthIdentityAllowsPage(event.App, workspace.Identity, page) {
		return notionBlockNotFound(event)
	}
	snapshot, err := parseRecoverableBlockTrashSnapshot(tombstone)
	if err != nil {
		return notionRequestNotCompleted(event, err)
	}
	operations, explicitStarts, err := prepareRecoverableBlockTrashRestore(event.App, tombstone, snapshot)
	if err != nil {
		return notionRequestNotCompleted(event, err)
	}
	revision, err := currentNotionGraphRevision(event.App, tombstone.OwnerID, tombstone.PageID)
	if err != nil {
		return notionRequestNotCompleted(event, err)
	}
	response := map[string]any{}
	fileIssuer := newNotionBlockFileCapabilityIssuer(event.Request, workspace.Identity)
	pageCursor := page.Page.GetInt("cursor")
	// The pages hanging on it that it still mentions come back with it
	// (notion_block_anchor_settle.go).
	anchors := newNotionBlockWriteAnchors(workspace.Identity.PrincipalID)
	_, err = ygo.applyStoredContentGraphMutationContext(
		event.Request.Context(), workspace.Identity.PrincipalID, tombstone.PageID,
		graphMutationInput{
			MutationID: "notion-public-block-restore:" + newDiagnosticReference(),
			Operations: operations, BasePageCursor: &pageCursor,
			BaseGraphRevision: &revision,
		}, contentGraphMutationHooks{
			Authorize: authorizeNotionPageMutation(workspace.Identity, tombstone.PageID),
			// Last in the parent it left, as Notion's (seen 2026-11-13).
			PrepareInput: func(_ core.App, _ pageAuthorization, doc *crdt.Doc, input graphMutationInput) (graphMutationInput, error) {
				return input, placeRecoverableBlockTrashRestoreAtParentEnd(doc, tombstone, input.Operations, explicitStarts, false)
			},
			BeforeApplyOperations: anchors.noting(validateRecoverableBlockTrashRestore(tombstone, snapshot, true)),
			ExplicitStartBlockIDs: explicitStarts,
			ApplyAdditionalText: func(
				app core.App, _ pageAuthorization,
			) (string, error) {
				return applyRecoverableBlockTrashText(
					app, tombstone.PageID, workspace.Identity.PrincipalID, snapshot,
				)
			},
			AfterApply: anchors.settling(captureRecoveredNotionBlockResponse(
				workspace.Identity, tombstone, fileIssuer, &response,
			)),
		},
	)
	if err != nil {
		return notionBlockMutationError(event, err)
	}
	response["in_trash"] = false
	return event.JSON(http.StatusOK, response)
}

func captureRecoveredNotionBlockResponse(
	identity notionOAuthBearerIdentity,
	tombstone storedRecoverableBlockTrash,
	fileIssuer *notionBlockFileCapabilityIssuer,
	response *map[string]any,
) func(core.App, pageAuthorization, *crdt.Doc) error {
	return func(
		app core.App,
		authorization pageAuthorization,
		structural *crdt.Doc,
	) error {
		if err := deleteRecoverableBlockTrash(
			app, tombstone.RootBlockID, tombstone.PageID,
		); err != nil {
			return err
		}
		values := []map[string]any{}
		// Read as Notion's answer to a restore reads (notionTableCellMentionItems).
		capture := captureNotionBlockObjects(
			identity, tombstone.PageID, tombstone.ParentID,
			[]string{tombstone.RootBlockID}, fileIssuer, &values, false,
		)
		if err := capture(app, authorization, structural); err != nil {
			return err
		}
		if len(values) != 1 {
			return graphConflictError(errorCodeGraphRevisionConflict, "The restored block response changed before commit.")
		}
		*response = values[0]
		return nil
	}
}
