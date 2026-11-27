package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

const (
	notionAPIBlocksPath        = notionAPIRoot + "/blocks"
	notionBlockDefaultPageSize = 100
	notionBlockMaximumPageSize = 100
)

type storedNotionBlock struct {
	ID           string `db:"id"`
	Owner        string `db:"owner"`
	WorkspaceID  string `db:"workspace_id"`
	PageID       string `db:"page_id"`
	ParentID     string `db:"parent_id"`
	Position     string `db:"position"`
	Type         string `db:"type"`
	Content      string `db:"content"`
	CreatedBy    string `db:"created_by"`
	LastEditedBy string `db:"last_edited_by"`
	Created      int64  `db:"created"`
	Updated      int64  `db:"updated"`
}

type notionBlockAuthorization struct {
	Identity notionOAuthBearerIdentity
	Page     pageAuthorization
	Block    storedNotionBlock
	// Written marks the answer to a write of the block (notionTableCellMentionItems).
	Written bool
}

func authorizeNotionBlock(
	event *core.RequestEvent,
	requestedBlockID string,
	requiredScope string,
	requirement permissionRequirement,
) (notionBlockAuthorization, bool, error) {
	return authorizeNotionBlockOrTrashed(event, requestedBlockID, requiredScope, requirement, nil)
}

// authorizeNotionBlockOrTrashed is authorizeNotionBlock for a read that
// answers a block in the trash with trashed (notion_trash_reads.go).
func authorizeNotionBlockOrTrashed(
	event *core.RequestEvent,
	requestedBlockID string,
	requiredScope string,
	requirement permissionRequirement,
	trashed func(notionTrashedBlockRead) error,
) (notionBlockAuthorization, bool, error) {
	blockID, ok := resolveNotionBlockID(event.App, requestedBlockID)
	if !ok {
		return notionBlockAuthorization{}, false, notionBlockNotFound(event)
	}
	workspace, authorized, err := authorizeNotionWorkspaceOperation(event, requiredScope, requirement)
	if err != nil || !authorized {
		return notionBlockAuthorization{}, false, err
	}
	block := storedNotionBlock{}
	err = event.App.DB().NewQuery(`
		SELECT [[id]],[[owner]],[[workspace_id]],[[page_id]],[[parent_id]],[[position]],
			[[type]],[[content]],[[created_by]],[[last_edited_by]],[[created]],[[updated]]
		FROM {{leafen_blocks}} WHERE [[id]]={:id} LIMIT 2
	`).Bind(dbx.Params{"id": blockID}).One(&block)
	if errors.Is(err, sql.ErrNoRows) {
		if requirement != permissionWorkspaceRead {
			// A block in the trash is refused as Notion refuses a change to it.
			if refused, answer := refuseNotionTrashedBlockEdit(event, workspace.Identity, blockID); refused {
				return notionBlockAuthorization{}, false, answer
			}
		} else if trashed != nil {
			read, found, err := loadNotionTrashedBlockRead(event.App, workspace.Identity, blockID)
			if err != nil {
				return notionBlockAuthorization{}, false, notionBlockProjectionError(event, err)
			}
			if found {
				return notionBlockAuthorization{}, false, trashed(read)
			}
		}
		return notionBlockAuthorization{}, false, notionBlockNotFound(event)
	}
	if err != nil {
		return notionBlockAuthorization{}, false, notionRequestNotCompleted(event, err)
	}
	if requirement != permissionWorkspaceRead && block.WorkspaceID == workspace.Identity.WorkspaceID {
		// A block in a page in the trash takes no change (notion_trash_refusal.go).
		if refused, err := refuseNotionTrashedBlockBodyEdit(event, workspace.Identity, block.PageID); refused {
			return notionBlockAuthorization{}, false, err
		}
	}
	page, err := authorizePageBody(event.App, workspace.Identity.PrincipalID, block.PageID, requirement)
	if isPermissionDenied(err) {
		return notionBlockAuthorization{}, false, notionBlockNotFound(event)
	}
	if err != nil {
		return notionBlockAuthorization{}, false, notionRequestNotCompleted(event, err)
	}
	if page.Workspace.WorkspaceID != workspace.Identity.WorkspaceID ||
		block.WorkspaceID != workspace.Identity.WorkspaceID || block.Owner != page.Workspace.OwnerID {
		return notionBlockAuthorization{}, false, notionBlockNotFound(event)
	}
	if !notionOAuthIdentityAllowsPage(event.App, workspace.Identity, page) {
		return notionBlockAuthorization{}, false, notionBlockNotFound(event)
	}
	return notionBlockAuthorization{Identity: workspace.Identity, Page: page, Block: block}, true, nil
}

func serveNotionBlockRetrieve(event *core.RequestEvent, blockID string, target notionBlockTarget) error {
	switch target {
	case notionBlockTargetPage:
		return serveNotionChildPageBlockRetrieve(event, blockID)
	case notionBlockTargetRowPage, notionBlockTargetTemplate:
		return serveNotionRowPageBlockRetrieve(event, blockID)
	}
	authorization, authorized, err := authorizeNotionBlockOrTrashed(
		event, blockID, "pages:read", permissionWorkspaceRead,
		func(read notionTrashedBlockRead) error { return serveNotionTrashedBlockRetrieve(event, read) },
	)
	if err != nil || !authorized {
		return err
	}
	fileIssuer := newNotionBlockFileCapabilityIssuer(event.Request, authorization.Identity)
	response, err := loadNotionBlockReadSnapshot(event.App, authorization, fileIssuer)
	if isPermissionDenied(err) {
		return notionBlockNotFound(event)
	}
	if err != nil {
		return notionBlockProjectionError(event, err)
	}
	// A block on a page in the trash, or under one, is in the trash with it.
	place, err := notionPageTrashPlace(event.App, authorization.Block.PageID)
	if err != nil {
		return notionBlockProjectionError(event, err)
	}
	response["in_trash"] = place.inTrash()
	event.Response.Header().Set("Cache-Control", "no-store")
	// A block read is a read of its page for the read log (read_log.go).
	recordAPIReads(event.App, authorization.Identity, []contentRead{contentReadOfPage(authorization.Page.Page)})
	return event.JSON(http.StatusOK, response)
}

func serveNotionBlockChildren(event *core.RequestEvent, blockID string) error {
	return serveNotionBlockChildrenFollowing(event, blockID, true)
}

func serveNotionBlockChildrenFollowing(event *core.RequestEvent, blockID string, followCopy bool) error {
	if _, err := resolveNotionPageRecordID(event.App, blockID); err == nil {
		return serveNotionPageBlockChildren(event, blockID)
	}
	if rowPage, err := notionDatabaseObjectIdentityExists(
		event.App, notionObjectIdentityRowPage, blockID,
	); err != nil {
		return notionBlockProjectionError(event, err)
	} else if rowPage {
		authorization, authorized, err := authorizeNotionRowPageBody(
			event, blockID, "pages:read", permissionWorkspaceRead,
		)
		if err != nil || !authorized {
			return err
		}
		return serveAuthorizedNotionPageBlockChildren(event, authorization)
	}
	authorization, authorized, err := authorizeNotionBlockOrTrashed(
		event, blockID, "pages:read", permissionWorkspaceRead,
		func(read notionTrashedBlockRead) error { return serveNotionTrashedBlockChildren(event, read) },
	)
	if err != nil || !authorized {
		return err
	}
	// A block on a page in the trash, or under one, lists no children.
	if place, err := notionPageTrashPlace(event.App, authorization.Block.PageID); err != nil {
		return notionBlockProjectionError(event, err)
	} else if place.inTrash() {
		return notionNoChildren(event)
	}
	// A synced copy's children are its original's, as Notion's API reads
	// them; with the original gone, in the trash or not the reader's, they
	// are not found or none (notionSyncedCopyOriginalMissing).
	if sourceID := notionSyncedCopySourceID(authorization.Block); followCopy && sourceID != "" {
		publicID, found, err := notionSyncedCopyOriginal(event.App, authorization.Page, sourceID)
		if err != nil || !found {
			return notionSyncedCopyOriginalMissing(event, authorization.Identity, sourceID, publicID, err, false)
		}
		return serveNotionBlockChildrenFollowing(event, publicID, false)
	}
	pageSize := notionBlockDefaultPageSize
	if raw := notionQueryDigits(event.Request.URL.Query().Get("page_size")); raw != "" {
		pageSize, err = strconv.Atoi(raw)
		if err != nil {
			return notionPageValidationError(event, "page_size must be a number.")
		}
		pageSize = notionListPageSize(pageSize, notionBlockDefaultPageSize)
	}
	startCursor := strings.TrimSpace(event.Request.URL.Query().Get("start_cursor"))
	normalizedCursor := ""
	if startCursor != "" {
		normalized, ok := resolveNotionBlockID(event.App, startCursor)
		if !ok {
			return notionStartCursorRefused(event, event.Request.URL.Query().Get("start_cursor"))
		}
		normalizedCursor = normalized
	}
	snapshot, err := loadNotionBlockChildrenReadSnapshot(
		event.App, authorization, normalizedCursor, pageSize,
		newNotionBlockFileCapabilityIssuer(event.Request, authorization.Identity),
	)
	if isPermissionDenied(err) {
		return notionBlockNotFound(event)
	}
	if errors.Is(err, errNotionBlockCursorInvalid) {
		return notionStartCursorRefused(event, event.Request.URL.Query().Get("start_cursor"))
	}
	if err != nil {
		return notionBlockProjectionError(event, err)
	}
	recordAPIReads(event.App, authorization.Identity, []contentRead{contentReadOfPage(authorization.Page.Page)})
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, map[string]any{
		"object": "list", "results": snapshot.Results, "next_cursor": snapshot.NextCursor,
		"has_more": snapshot.HasMore, "type": "block", "block": map[string]any{},
	})
}

func loadNotionBlockTextDocument(app core.App, pageID string) (*crdt.Doc, error) {
	document, _, err := loadYgoDoc(app, pageID)
	return document, err
}

func loadNotionBlockChildCounts(app core.App, blockIDs []string) (map[string]int, error) {
	counts := map[string]int{}
	if len(blockIDs) == 0 {
		return counts, nil
	}
	params := dbx.Params{}
	placeholders := make([]string, len(blockIDs))
	for index, id := range blockIDs {
		key := "block" + strconv.Itoa(index)
		params[key] = id
		placeholders[index] = "{:" + key + "}"
	}
	rows := []struct {
		ParentID string `db:"parent_id"`
		Count    int    `db:"count"`
	}{}
	// A block's children share its page, so each is found through its parent:
	// by the page's own order index, not across every page's blocks.
	err := app.DB().NewQuery(`
		SELECT [[child]].[[parent_id]] AS [[parent_id]], COUNT(*) AS [[count]]
		FROM {{leafen_blocks}} AS [[parent]]
		JOIN {{leafen_blocks}} AS [[child]] INDEXED BY [[idx_leafen_blocks_page_order]]
			ON [[child]].[[owner]] = [[parent]].[[owner]] AND [[child]].[[page_id]] = [[parent]].[[page_id]]
			AND [[child]].[[parent_id]] = [[parent]].[[id]]
		WHERE [[parent]].[[id]] IN (` + strings.Join(placeholders, ",") + `) AND ` + blockStandsSQL("child") + `
		GROUP BY [[child]].[[parent_id]]
	`).Bind(params).All(&rows)
	for _, row := range rows {
		counts[row.ParentID] = row.Count
	}
	if err != nil {
		return counts, err
	}
	return counts, countNotionSyncedCopyChildren(app, params, strings.Join(placeholders, ","), counts)
}

func notionBlockObject(
	app core.App,
	authorization notionBlockAuthorization,
	block storedNotionBlock,
	hasChildren bool,
	document *crdt.Doc,
	fileIssuer *notionBlockFileCapabilityIssuer,
) (map[string]any, error) {
	// A sub-page's block, or a wiki's link standing for its page, is that
	// page, as Notion shows it (notionPageBlockStandIn).
	if value, isPage, err := notionPageBlockStandIn(app, block); err != nil || isPage {
		return value, err
	}
	createdBy, lastEditedBy, err := connectionAuthorIDs(
		app, connectionAuthoredBlock, block.ID, notionBlockEditMark(block.Updated), block.CreatedBy, block.LastEditedBy,
	)
	if err != nil {
		return nil, err
	}
	publicID, err := notionBlockPublicID(app, block.WorkspaceID, block.ID)
	if err != nil {
		return nil, err
	}
	parent := map[string]any{"type": "block_id", "block_id": block.ParentID}
	if block.ParentID != "" {
		parentID, err := notionBlockPublicID(app, block.WorkspaceID, block.ParentID)
		if err != nil {
			return nil, err
		}
		parent["block_id"] = parentID
	} else if parent, err = notionParentInPage(app, block.PageID); err != nil {
		return nil, err
	}
	content := map[string]any{}
	if json.Unmarshal([]byte(block.Content), &content) != nil {
		return nil, errInvalidNotionPageIdentity
	}
	if block.Type == graphBlockTypeLinkToPage {
		content = projectNotionLinkToPageContent(app, authorization, content)
	} else if block.Type == graphBlockTypeSyncedBlock {
		content = projectNotionSyncedBlockContent(app, authorization, content)
	}
	if block.Type == graphBlockTypeParagraph && notionBlockIconStored(content) {
		allowed, err := notionParagraphIconAllowed(app, block)
		if err != nil {
			return nil, err
		}
		if !allowed {
			clearNotionBlockIcon(content)
		}
	}
	if err := projectNotionBlockCustomEmojiIcon(
		app, fileIssuer, authorization.Identity.WorkspaceID, content,
	); err != nil {
		return nil, err
	}
	textBlockID := block.ID
	if line, following, found, err := notionCalloutFirstLine(app, block, document); err != nil {
		return nil, err
	} else if found {
		textBlockID, hasChildren = line.ID, following > 0
	}
	text := document.GetText(ygoBlockTextRootPrefix + textBlockID)
	plainText := textAsItStands(text)
	richText, richTextSupported, err := notionBlockRichText(
		app, authorization, text, fileIssuer,
	)
	if err != nil {
		return nil, err
	}
	externalType, payload := notionUnsupportedBlockPayload(block.Type)
	if richTextSupported {
		externalType, payload = notionBlockPayloadWithRichText(block.Type, content, plainText, richText)
		notionNumberedListFields(block.Type, content, payload)
	}
	if block.Type == graphBlockTypeTableRow {
		payload, err = notionTableRowPayload(
			app, authorization, json.RawMessage(block.Content), fileIssuer,
		)
		externalType = graphBlockTypeTableRow
		if errors.Is(err, errNotionTableRowUnreadable) {
			// A cell with a reference the reader can no longer see leaves the
			// row unsupported, as block text does.
			externalType, payload = notionUnsupportedBlockPayload(block.Type)
		} else if err != nil {
			return nil, err
		}
	}
	managedFileContent := json.RawMessage(block.Content)
	if graphBlockCarriesManagedFileReference(&graphBlock{
		Type: block.Type, Content: managedFileContent,
	}) && notionManagedFileBlockCompatible(block.Type, managedFileContent) {
		payload, err = fileIssuer.payload(
			app, authorization.Identity.WorkspaceID, block.Type, managedFileContent,
		)
		if err != nil {
			return nil, err
		}
		externalType = block.Type
	}
	// A callout's uploaded icon reads as Notion's uploaded icon: a file with a
	// short-lived address (block_kept_file.go).
	if iconFileID := graphCalloutIconFileID(block.Type, json.RawMessage(block.Content)); iconFileID != "" && externalType == block.Type && payload != nil {
		icon, iconErr := notionPageManagedFileObject(app, fileIssuer, authorization.Identity.WorkspaceID, iconFileID)
		if iconErr != nil {
			return nil, iconErr
		}
		payload["icon"] = icon
	}
	externalType, payload, err = projectNotionReaderCaption(
		app, authorization, fileIssuer, block.Type, content, externalType, payload,
	)
	if err != nil {
		return nil, err
	}
	if block.Type == graphBlockTypeInlineDatabase {
		if title, ok, err := notionInlineDatabaseTitle(app, authorization, block); err != nil {
			return nil, err
		} else if ok {
			// A database's block is named by its database's ID.
			externalType, payload = "child_database", map[string]any{"title": title}
			if publicID, err = ensureNotionObjectIdentity(
				app, notionObjectIdentityDatabase, authorization.Identity.WorkspaceID, block.ID,
			); err != nil {
				return nil, err
			}
		}
	}
	if block.Type == graphBlockTypeTab {
		canonical, err := notionCanonicalTabBlock(app, block)
		if err != nil {
			return nil, err
		}
		if canonical {
			externalType, payload = graphBlockTypeTab, map[string]any{}
		}
	}
	response := map[string]any{
		"object":           "block",
		"id":               publicID,
		"parent":           parent,
		"created_time":     notionMinuteTimestamp(time.UnixMilli(block.Created)),
		"created_by":       map[string]any{"object": "user", "id": createdBy},
		"last_edited_time": notionMinuteTimestamp(time.UnixMilli(block.Updated)),
		"last_edited_by":   map[string]any{"object": "user", "id": lastEditedBy},
		"has_children":     hasChildren,
		"in_trash":         authorization.Page.Page.GetBool("in_trash"),
		"type":             externalType,
		externalType:       payload,
	}
	return response, nil
}

var errNotionTableRowUnreadable = errors.New("Notion-compatible table row cells are unavailable to this reader")

func notionTableRowPayload(
	app core.App,
	authorization notionBlockAuthorization,
	raw json.RawMessage,
	fileIssuer *notionBlockFileCapabilityIssuer,
) (map[string]any, error) {
	content, err := parseGraphTableRowBlockContent(raw)
	if err != nil {
		return nil, err
	}
	cells := make([][]map[string]any, len(content.Cells))
	for index, spans := range content.Cells {
		// A cell's pieces read as written, empty ones too, as Notion's
		// table row reads (seen 2026-11-10: "x", "", bold "y", bold "z"
		// read back four).
		richText, supported, err := notionSpansRichText(app, authorization, spans, fileIssuer)
		if err != nil {
			return nil, err
		}
		if !supported {
			return nil, errNotionTableRowUnreadable
		}
		cells[index] = notionTableCellMentionItems(authorization, richText)
	}
	return map[string]any{"cells": cells}, nil
}

// notionCanonicalTabBlock tells a tab block holding tabs, which reads as
// Notion's tab: its items may be any block, as Notion's (seen 2026-11-14:
// "<tabs>\n# h\n</tabs>" lists the heading under the tab).
func notionCanonicalTabBlock(app core.App, block storedNotionBlock) (bool, error) {
	var total int
	err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_blocks}}
		WHERE [[owner]]={:owner} AND [[page_id]]={:pageId} AND [[parent_id]]={:parentId}
	`).Bind(dbx.Params{
		"owner": block.Owner, "pageId": block.PageID, "parentId": block.ID,
	}).Row(&total)
	return total > 0, err
}

func notionBlockNotFound(event *core.RequestEvent) error {
	return notionCompatibilityError(
		event, http.StatusNotFound, notionErrorObjectNotFound,
		"There's no block with the requested ID.",
	)
}

func notionBlockProjectionError(event *core.RequestEvent, err error) error {
	if isDLPPolicyDecisionError(err) {
		return notionDLPPolicyError(event, err)
	}
	return notionRequestNotCompleted(event, err)
}
