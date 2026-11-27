package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

type notionBlockAppendRequest struct {
	Children []json.RawMessage     `json:"children"`
	Position *notionAppendPosition `json:"position,omitempty"`
}

type notionParsedBlockMutation struct {
	Block     *graphBlock
	TextSpans *[]graphTextSpan
}

func serveNotionBlockUpdate(
	event *core.RequestEvent,
	ygo *pocketBaseYgoPersistence,
	blockID string,
	target notionBlockTarget,
) error {
	// A page, a database page too, is changed through the pages endpoint
	// (notion_block_target.go).
	if target == notionBlockTargetPage || target == notionBlockTargetRowPage {
		return serveNotionPageBlockUpdate(event, blockID, target)
	}
	body := map[string]json.RawMessage{}
	bindErr := bindNotionStrictJSON(event, &body)
	if bindErr == nil && notionBlockRestoreOnlyRequest(body) {
		return restoreNotionBlock(event, ygo, blockID)
	}
	authorization, authorized, err := authorizeNotionBlock(
		event, blockID, "pages:write", permissionWorkspaceWrite,
	)
	if err != nil || !authorized {
		return err
	}
	if bindErr != nil {
		return notionBindRefused(event, bindErr)
	}
	if len(body) == 0 {
		return notionPageValidationError(event, "The block update body is empty: give the block type's object, or in_trash.")
	}
	externalType, err := notionStoredBlockExternalType(event.App, authorization)
	if err != nil {
		return notionRequestNotCompleted(event, err)
	}
	// archived, which in_trash replaced, is not part of this API version.
	if _, ok := body["archived"]; ok {
		return notionPageValidationError(event, "archived isn't part of this API version; use in_trash.")
	}
	trashAfterUpdate := false
	if raw, ok := body["in_trash"]; ok {
		if json.Unmarshal(raw, &trashAfterUpdate) != nil {
			return notionPageValidationError(event, "in_trash must be a boolean.")
		}
		delete(body, "in_trash")
	}
	if raw, ok := body["type"]; ok {
		declared := ""
		if json.Unmarshal(raw, &declared) != nil || declared != externalType {
			return notionPageValidationError(event, "The block type cannot be changed.")
		}
		delete(body, "type")
	}
	if len(body) == 0 {
		if trashAfterUpdate {
			return deleteNotionBlock(event, ygo, authorization)
		}
		return writeCurrentNotionBlock(event, authorization)
	}
	if len(body) != 1 {
		return notionPageValidationError(event, "Exactly one block value must be updated.")
	}
	if externalType == "unsupported" {
		return notionPageValidationError(event, "This block type cannot be updated through the Notion-compatible API.")
	}
	raw, ok := body[externalType]
	if !ok {
		return notionPageValidationError(event, "The block type cannot be changed.")
	}
	raw = notionSyncedBlockUpdateTaken(event.App, authorization, notionBlockUpdateTakenContent(externalType, raw))
	// A table row's cells and a toggle heading's blocks hold what it may become.
	if refusal, err := notionBlockUpdateRefusal(event.App, authorization.Block, externalType, raw); err != nil {
		return notionRequestNotCompleted(event, err)
	} else if refusal != "" {
		return notionPageValidationError(event, refusal)
	}
	if externalType == graphBlockTypeParagraph && notionBlockFieldSpecified(raw, "icon") {
		allowed, err := notionParagraphIconAllowed(event.App, authorization.Block)
		if err != nil {
			return notionRequestNotCompleted(event, err)
		}
		if !allowed {
			return notionPageValidationError(event, notionParagraphIconRefused)
		}
	}
	parsed, err := parseNotionBlockMutation(
		authorization.Block.ID, authorization.Block.ParentID,
		authorization.Block.Position, externalType, raw, &authorization.Block,
	)
	if fault := notionContentFault(""); errors.As(err, &fault) {
		// Said as Notion says it, spaces and all ("Invalid URL   ").
		return notionCompatibilityError(event, http.StatusBadRequest, notionErrorValidation, string(fault))
	}
	if err != nil {
		return notionPageValidationError(event, err.Error())
	}
	operations := []graphOperation{{Kind: graphOperationKindUpsertBlock, Block: parsed.Block}}
	if trashAfterUpdate {
		operations = append(operations, graphOperation{
			Kind: graphOperationKindDeleteBlock,
			Deletion: &graphBlockDelete{
				BlockID: authorization.Block.ID,
			},
		})
	}
	input := graphMutationInput{
		MutationID: "notion-public-block-update:" + newDiagnosticReference(),
		Operations: operations,
	}
	page := authorization.Page.Page
	graphRevision, err := currentNotionGraphRevision(
		event.App, authorization.Block.Owner, authorization.Block.PageID,
	)
	if err != nil {
		return notionRequestNotCompleted(event, err)
	}
	pageCursor := page.GetInt("cursor")
	input.BasePageCursor = &pageCursor
	input.BaseGraphRevision = &graphRevision
	if parsed.TextSpans != nil {
		// A callout's text is its first line, where that stands as a paragraph.
		textBlockID := authorization.Block.ID
		if authorization.Block.Type == graphBlockTypeCallout {
			document, err := loadNotionBlockTextDocument(event.App, authorization.Block.PageID)
			if err != nil {
				return notionRequestNotCompleted(event, err)
			}
			line, _, found, err := notionCalloutFirstLine(event.App, authorization.Block, document)
			if err != nil {
				return notionRequestNotCompleted(event, err)
			}
			if found {
				textBlockID = line.ID
				input.Operations = append(input.Operations, graphOperation{Kind: graphOperationKindUpsertBlock, Block: &graphBlock{
					ID: line.ID, ParentID: &authorization.Block.ID, Position: line.Position, Type: line.Type, Content: json.RawMessage(line.Content),
				}})
			}
		}
		input.TextUpdates = []graphTextUpdate{{
			BlockID: textBlockID,
			Spans:   *parsed.TextSpans,
		}}
	}
	updatedBlocks := []map[string]any{}
	updatedAndTrashedBlock := map[string]any{}
	fileIssuer := newNotionBlockFileCapabilityIssuer(event.Request, authorization.Identity)
	preparedTextSpans := parsed.TextSpans
	prepareMentions := prepareNotionRichTextMentions(authorization.Identity)
	prepareInput := func(
		app core.App,
		current pageAuthorization,
		document *crdt.Doc,
		candidate graphMutationInput,
	) (graphMutationInput, error) {
		prepared, err := prepareMentions(app, current, document, candidate)
		if err == nil && len(prepared.TextUpdates) == 1 {
			preparedTextSpans = &prepared.TextUpdates[0].Spans
		}
		return prepared, err
	}
	// The pages hanging on it stay with it, or go with it (notion_block_anchor_settle.go).
	anchors := newNotionBlockWriteAnchors(authorization.Identity.PrincipalID)
	beforeApplyOperations := anchors.noting(notionBlockReferencesBeforeApply(authorization.Identity))
	afterApply := anchors.settling(captureNotionBlockAppendResponse(
		authorization.Identity, authorization.Block.PageID,
		authorization.Block.ParentID, []string{authorization.Block.ID}, fileIssuer, &updatedBlocks,
	))
	if trashAfterUpdate {
		captureResponse := captureNotionBlockBeforeUpdatedDeleteResponse(
			authorization.Identity, authorization.Block.PageID, authorization.Block.ID,
			parsed.Block, func() *[]graphTextSpan { return preparedTextSpans },
			fileIssuer, &updatedAndTrashedBlock,
		)
		captureTrash := captureRecoverableBlockTrashAfterUpdate(
			authorization.Block.ID, authorization.Identity.PrincipalID,
			parsed.Block, func() *[]graphTextSpan { return preparedTextSpans },
		)
		beforeApplyOperations = anchors.noting(func(app core.App, current pageAuthorization, document *crdt.Doc, operations []graphOperation) error {
			if err := notionBlockReferencesBeforeApply(authorization.Identity)(app, current, document, operations); err != nil {
				return err
			}
			if err := captureResponse(app, current, document, operations); err != nil {
				return err
			}
			return captureTrash(app, current, document, operations)
		})
		afterApply = anchors.settling(nil)
	}
	if _, err := ygo.applyContentGraphMutationContext(
		event.Request.Context(), authorization.Identity.PrincipalID,
		authorization.Block.PageID, input,
		contentGraphMutationHooks{
			Authorize: authorizeNotionPageMutation(
				authorization.Identity, authorization.Block.PageID,
			),
			PrepareInput:                 prepareInput,
			ReplaceExistingText:          parsed.TextSpans != nil,
			BeforeApplyOperations:        beforeApplyOperations,
			AfterApply:                   afterApply,
			AllowBlockUpdateBeforeDelete: trashAfterUpdate,
			// Its other steps check references, capture the answer and trash, settle sub-pages.
			SearchFromNamedBlocks: true, StepsKeepGraph: true,
		},
	); err != nil {
		return notionBlockMutationError(event, err)
	}
	if trashAfterUpdate {
		if len(updatedAndTrashedBlock) == 0 {
			return notionRequestNotCompleted(event, nil)
		}
		return event.JSON(http.StatusOK, updatedAndTrashedBlock)
	}
	if len(updatedBlocks) != 1 {
		return notionRequestNotCompleted(event, nil)
	}
	return event.JSON(http.StatusOK, updatedBlocks[0])
}

func serveNotionBlockDelete(
	event *core.RequestEvent,
	ygo *pocketBaseYgoPersistence,
	blockID string,
	target notionBlockTarget,
) error {
	switch target {
	case notionBlockTargetPage:
		return deleteNotionChildPageBlock(event, blockID)
	case notionBlockTargetRowPage:
		return deleteNotionRowPageBlock(event, ygo, blockID)
	}
	authorization, authorized, err := authorizeNotionBlock(
		event, blockID, "pages:write", permissionWorkspaceWrite,
	)
	if err != nil || !authorized {
		return err
	}
	return deleteNotionBlock(event, ygo, authorization)
}

func deleteNotionBlock(
	event *core.RequestEvent,
	ygo *pocketBaseYgoPersistence,
	authorization notionBlockAuthorization,
) error {
	response := map[string]any{}
	fileIssuer := newNotionBlockFileCapabilityIssuer(event.Request, authorization.Identity)
	deletion := []graphOperation{{
		Kind:     graphOperationKindDeleteBlock,
		Deletion: &graphBlockDelete{BlockID: authorization.Block.ID},
	}}
	// A synced block original's copies stay, still naming it, as Notion's
	// API leaves them: a copy reads as before and its children are not
	// found until the original is back (seen 2026-10-14). The app lets them
	// go first instead (synced_block_original_deletion.go). The pages hanging
	// on it go with it (notion_block_anchor_settle.go).
	anchors := newNotionBlockWriteAnchors(authorization.Identity.PrincipalID)
	_, err := ygo.applyContentGraphMutationContext(
		event.Request.Context(), authorization.Identity.PrincipalID,
		authorization.Block.PageID,
		graphMutationInput{
			MutationID: "notion-public-block-delete:" + newDiagnosticReference(),
			Operations: deletion,
		}, contentGraphMutationHooks{
			Authorize: authorizeNotionPageMutation(
				authorization.Identity, authorization.Block.PageID,
			),
			BeforeApply: captureNotionBlockBeforeDeleteResponse(
				authorization.Identity, authorization.Block.PageID,
				authorization.Block.ID, fileIssuer, &response,
			),
			BeforeApplyOperations: anchors.noting(captureRecoverableBlockTrash(authorization.Block.ID, authorization.Identity.PrincipalID)),
			AfterApply:            anchors.settling(nil),
			SearchFromNamedBlocks: true,
		},
	)
	if err != nil {
		return notionBlockMutationError(event, err)
	}
	response["in_trash"] = true
	return event.JSON(http.StatusOK, response)
}

func serveNotionBlockChildrenAppend(
	event *core.RequestEvent,
	ygo *pocketBaseYgoPersistence,
	blockID string,
	target notionBlockTarget,
) error {
	if target == notionBlockTargetRowPage || target == notionBlockTargetTemplate {
		pageAuthorization, authorized, err := authorizeNotionRowPageBody(
			event, blockID, "pages:write", permissionWorkspaceWrite,
		)
		if err != nil || !authorized {
			return err
		}
		return appendNotionBlockChildren(event, ygo, notionBlockAppendTarget{
			Identity: pageAuthorization.Identity, Page: pageAuthorization.Page,
			PageID: pageAuthorization.Page.Page.Id, Owner: pageAuthorization.Page.Page.GetString("owner"),
		})
	}
	if target == notionBlockTargetPage {
		pageAuthorization, authorized, err := authorizeNotionPage(
			event, blockID, "pages:write", permissionWorkspaceWrite,
		)
		if err != nil || !authorized {
			return err
		}
		if refused, err := refuseNotionTrashedEdit(event, pageAuthorization.Page.Page.Id, true, false, notionTrashedEdit); refused {
			return err
		}
		return appendNotionBlockChildren(event, ygo, notionBlockAppendTarget{
			Identity: pageAuthorization.Identity, Page: pageAuthorization.Page,
			PageID: pageAuthorization.Page.Page.Id, Owner: pageAuthorization.Page.Workspace.OwnerID,
		})
	}
	authorization, authorized, err := authorizeNotionBlock(
		event, blockID, "pages:write", permissionWorkspaceWrite,
	)
	if err != nil || !authorized {
		return err
	}
	return appendNotionBlockChildren(event, ygo, notionBlockAppendTarget{
		Identity: authorization.Identity, Page: authorization.Page,
		PageID: authorization.Block.PageID, Owner: authorization.Block.Owner,
		ParentID: authorization.Block.ID, ParentType: authorization.Block.Type,
	})
}

func appendNotionBlockChildren(
	event *core.RequestEvent,
	ygo *pocketBaseYgoPersistence,
	target notionBlockAppendTarget,
) error {
	input := notionBlockAppendRequest{}
	if err := bindNotionStrictJSON(event, &input); err != nil {
		return notionBindRefused(event, err)
	}
	appendOnly := input.Position == nil || input.Position.Type == "end"
	var afterID *string
	if input.Position != nil {
		switch input.Position.Type {
		case "start":
		case "end":
		case "after_block":
			if input.Position.AfterBlock == nil {
				return notionPageValidationError(event, "after_block.id is required.")
			}
			anchor, err := notionBodyAnchor(event.App, target.Identity, target.PageID, target.ParentID,
				notionBlockIDForDatabase(event.App, input.Position.AfterBlock.ID))
			if errors.Is(err, errNotionBodyAnchorTrashed) {
				appendOnly = true
			} else if err != nil {
				return notionBodyAnchorError(event, err, input.Position.AfterBlock.ID)
			}
			afterID = anchor
		default:
			return notionPageValidationError(event, "position.type is invalid.")
		}
	}
	// Under a synced copy blocks go under its original (notion_block_append_target.go).
	target, answered, err := notionAppendUnderOriginal(event, target, input.Position, afterID)
	if !answered {
		answered, err = serveNotionBlockAppendCount(event, target, len(input.Children))
	}
	if answered {
		return err
	}
	if appendOnly {
		tail := ""
		err := event.App.DB().NewQuery(`
			SELECT [[id]] FROM {{leafen_blocks}}
			WHERE [[owner]]={:owner} AND [[page_id]]={:pageId} AND [[parent_id]]={:parentId}
			ORDER BY [[position]] DESC,[[id]] DESC LIMIT 1
		`).Bind(map[string]any{
			"owner": target.Owner, "pageId": target.PageID,
			"parentId": target.ParentID,
		}).Row(&tail)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return notionRequestNotCompleted(event, err)
		}
		if tail != "" {
			afterID = &tail
		}
	}
	plan := notionBlockAppendPlan{ContentCheck: notionCustomEmojiContentCheck(
		event.App, target.Identity.PrincipalID, target.Page.Workspace.WorkspaceID,
	)}
	if target.ParentType == graphBlockTypeTable {
		// The rows are as many cells as the table is wide.
		parent, exists, err := loadStoredNotionBlock(event.App, target.ParentID)
		if err != nil {
			return notionRequestNotCompleted(event, err)
		}
		if width, ok := notionStoredTableWidth(json.RawMessage(parent.Content)); exists && ok {
			plan.TableWidths = map[string]int{target.ParentID: width}
		}
	}
	if err := plan.appendRequestBlocks(input.Children, target.ParentID, target.ParentType, afterID); err != nil {
		if len(plan.ContentFaults) > 0 {
			if refused := notionRequestTargetRefused(event.App, target.Identity, target.Page, plan.Operations); refused != nil {
				return notionBlockMutationError(event, refused)
			}
		}
		return notionPageValidationError(event, err.Error())
	}
	if input.Position != nil && input.Position.Type == "start" && len(plan.ResponseBlockIDs) > 0 {
		plan.ExplicitStartIDs = map[string]struct{}{plan.ResponseBlockIDs[0]: {}}
	}
	page := target.Page.Page
	graphRevision, err := currentNotionGraphRevision(event.App, target.Owner, target.PageID)
	if err != nil {
		return notionRequestNotCompleted(event, err)
	}
	pageCursor := page.GetInt("cursor")
	results := []map[string]any{}
	captureResponse := captureNotionBlockAppendResponse(
		target.Identity, target.PageID, target.ParentID,
		plan.ResponseBlockIDs,
		newNotionBlockFileCapabilityIssuer(event.Request, target.Identity), &results,
	)
	afterApply := captureResponse
	_, err = ygo.applyContentGraphMutationContext(
		event.Request.Context(), target.Identity.PrincipalID,
		target.PageID,
		graphMutationInput{
			MutationID: "notion-public-block-append:" + newDiagnosticReference(),
			Operations: plan.Operations, TextUpdates: plan.TextUpdates,
			BasePageCursor: &pageCursor, BaseGraphRevision: &graphRevision,
		}, contentGraphMutationHooks{
			Authorize:             authorizeNotionPageMutation(target.Identity, target.PageID),
			PrepareInput:          prepareNotionRichTextMentions(target.Identity),
			ExplicitStartBlockIDs: plan.ExplicitStartIDs,
			BeforeApplyOperations: notionBlockReferencesBeforeApply(target.Identity),
			AfterApply:            afterApply,
			// Its steps check references and read the answer back.
			SearchFromNamedBlocks: true, StepsKeepGraph: true,
			// Blocks added after the page's last go in without their siblings'
			// places read and written again (content_graph_append_projection.go).
			StructuralAppend: true,
		},
	)
	if err != nil {
		return notionBlockMutationError(event, err)
	}
	return serveNotionBlockAppendAnswer(event, target, plan.ResponseBlockIDs, results)
}

func notionBlockRequestValue(raw json.RawMessage) (string, json.RawMessage, error) {
	value := map[string]json.RawMessage{}
	if json.Unmarshal(raw, &value) != nil {
		return "", nil, errors.New("child block is invalid")
	}
	declared := ""
	if rawType, ok := value["type"]; ok {
		if json.Unmarshal(rawType, &declared) != nil {
			return "", nil, errors.New("child block type is invalid")
		}
		delete(value, "type")
	}
	// The keys of a block object a request repeats are left, as Notion's
	// leaves them: a new block is its own, not in trash (seen 2026-11-10).
	for _, echoed := range notionNewBlockEchoedKeys {
		delete(value, echoed)
	}
	if len(value) != 1 {
		return "", nil, errors.New("child block must contain exactly one block value")
	}
	for blockType, payload := range value {
		if declared != "" && declared != blockType {
			return "", nil, errors.New("child block type does not match its value")
		}
		return blockType, payload, nil
	}
	return "", nil, errors.New("child block is invalid")
}

func parseNotionBlockMutation(
	id, parentID, position, blockType string,
	raw json.RawMessage,
	existing *storedNotionBlock,
) (notionParsedBlockMutation, error) {
	blockType, content, textSpans, err := normalizeNotionBlockMutationPayload(blockType, raw, existing)
	if err != nil {
		return notionParsedBlockMutation{}, err
	}
	if existing == nil {
		sizeNewNotionAPIBlock(blockType, content)
	}
	plainNotionAPIBookmark(blockType, raw, existing == nil, content)
	encoded, err := json.Marshal(content)
	if err != nil {
		return notionParsedBlockMutation{}, err
	}
	var parent *string
	if parentID != "" {
		parent = &parentID
	}
	return notionParsedBlockMutation{
		Block: &graphBlock{
			ID: id, ParentID: parent, Position: position,
			Type: blockType, Content: encoded,
		},
		TextSpans: textSpans,
	}, nil
}

func writeCurrentNotionBlock(event *core.RequestEvent, authorization notionBlockAuthorization) error {
	document, err := loadNotionBlockTextDocument(event.App, authorization.Block.PageID)
	if err != nil {
		return notionRequestNotCompleted(event, err)
	}
	counts, err := loadNotionBlockChildCounts(event.App, []string{authorization.Block.ID})
	if err != nil {
		return notionRequestNotCompleted(event, err)
	}
	response, err := notionBlockObject(
		event.App, authorization, authorization.Block, counts[authorization.Block.ID] > 0,
		document, newNotionBlockFileCapabilityIssuer(event.Request, authorization.Identity),
	)
	if err != nil {
		return notionBlockProjectionError(event, err)
	}
	return event.JSON(http.StatusOK, response)
}

// notionTableRowUpdateRefusal is Notion's refusal of a table row's change,
// or "": one that gives it other than as many cells as its table is wide,
// and any change to a row of a table without width, which Notion's takes as
// no table's (seen 2026-11-14).
func notionTableRowUpdateRefusal(app core.App, row storedNotionBlock, raw json.RawMessage) (string, error) {
	cells := notionTableRowCellCount(raw)
	if cells < 0 {
		return "", nil
	}
	table, exists, err := loadStoredNotionBlock(app, row.ParentID)
	if err != nil || !exists || table.Type != graphBlockTypeTable {
		return "", err
	}
	width, ok := notionStoredTableWidth(json.RawMessage(table.Content))
	if ok && width < minGraphTableWidth {
		publicID, err := notionBlockPublicID(app, row.WorkspaceID, row.ID)
		if err != nil {
			return "", err
		}
		return "Block " + publicID + " isn't inside a table it can be a row of.", nil
	}
	if ok && cells != width {
		return notionTableRowCellsRefused + ".", nil
	}
	return "", nil
}

func loadStoredNotionBlock(app core.App, blockID string) (storedNotionBlock, bool, error) {
	block := storedNotionBlock{}
	err := app.DB().NewQuery(`
		SELECT [[id]],[[owner]],[[workspace_id]],[[page_id]],[[parent_id]],[[position]],
			[[type]],[[content]],[[created_by]],[[last_edited_by]],[[created]],[[updated]]
		FROM {{leafen_blocks}} WHERE [[id]]={:id} LIMIT 2
	`).Bind(map[string]any{"id": blockID}).One(&block)
	if errors.Is(err, sql.ErrNoRows) {
		return storedNotionBlock{}, false, nil
	}
	return block, err == nil, err
}

func currentNotionGraphRevision(app core.App, owner, pageID string) (int, error) {
	revision := 0
	err := app.DB().NewQuery(`
		SELECT COALESCE(MAX([[cursor]]),0) FROM {{leafen_graph_mutations}}
		WHERE [[owner]]={:owner} AND [[page_id]]={:pageId}
	`).Bind(map[string]any{"owner": owner, "pageId": pageID}).Row(&revision)
	return revision, err
}

func notionStoredBlockExternalType(
	app core.App,
	authorization notionBlockAuthorization,
) (string, error) {
	block := authorization.Block
	content := map[string]any{}
	if json.Unmarshal([]byte(block.Content), &content) != nil || content == nil {
		return "", errors.New("stored block content is invalid")
	}
	managedFileContent := json.RawMessage(block.Content)
	if graphBlockCarriesManagedFileReference(&graphBlock{
		Type: block.Type, Content: managedFileContent,
	}) && notionManagedFileBlockCompatible(block.Type, managedFileContent) {
		return block.Type, nil
	}
	if block.Type == graphBlockTypeLinkToPage {
		pageID, _ := content["childPageId"].(string)
		if validPageRecordID(strings.TrimSpace(pageID)) {
			return "link_to_page", nil
		}
		databaseID, _ := content["databaseContainerId"].(string)
		if normalized, ok := normalizeNotionUUIDRequest(databaseID); ok && normalized == databaseID {
			return "link_to_page", nil
		}
		commentID, _ := content["commentId"].(string)
		if validCommentIdentifier(strings.TrimSpace(commentID)) {
			return "link_to_page", nil
		}
	}
	if block.Type == graphBlockTypeSyncedBlock {
		content = projectNotionSyncedBlockContent(app, authorization, content)
	}
	if block.Type == graphBlockTypeTab {
		canonical, err := notionCanonicalTabBlock(app, block)
		if err != nil {
			return "", err
		}
		if canonical {
			return graphBlockTypeTab, nil
		}
	}
	if block.Type == graphBlockTypeTable || block.Type == graphBlockTypeTableRow {
		return block.Type, nil
	}
	externalType, _ := notionBlockPayload(block.Type, content, "")
	return externalType, nil
}

func notionBlockMutationError(event *core.RequestEvent, err error) error {
	if isDLPPolicyDecisionError(err) {
		return notionBlockProjectionError(event, err)
	}
	requestError := &graphRequestError{}
	if errors.As(err, &requestError) {
		switch requestError.status {
		case http.StatusNotFound:
			return notionBlockNotFound(event)
		case http.StatusBadRequest:
			return notionPageValidationError(event, requestError.message)
		case http.StatusConflict:
			return notionGraphConflictError(event, requestError)
		}
	}
	return notionRequestNotCompleted(event, err)
}
