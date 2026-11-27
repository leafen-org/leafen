package app

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// A link_to_page block a write makes or changes names its page or database
// by an ID as a mention does (notion_address_mentions_here.go): one the
// workspace has stays, and one nothing here answers to but a Notion move into
// the workspace brought — a page or a database page by page_id, a database by
// database_id — names what the move made, so a public API JSON write naming a
// moved page by the ID Notion gave it links that page, as its mention of the
// same ID mentions it (before 2026-11-21 the link kept the Notion ID and was
// refused as one to nothing the integration reaches). Any other is left to
// the link's check (notionLinkTargetOf), which refuses one of nothing.
//
// So does a synced block a write makes name its original: by an ID nothing
// here answers to but a Notion move brought, what the move made of it — a
// block's block, which the synced block's check
// (authorizeNotionSyncedBlockOperations) takes when it is an original synced
// block and refuses as Notion does when not, and a page's page, refused as a
// page is — as its Markdown reference does by such an ID or address
// (notionMarkdownSyncedOriginal). Before 2026-11-21 one by a moved block's
// Notion ID was refused as one to nothing the integration reaches.

// linkBlock points a link_to_page block's page or database here, in place,
// and tells whether it changed.
func (here *notionAddressLinksHere) linkBlock(block *graphBlock) (bool, error) {
	content := map[string]any{}
	if json.Unmarshal(block.Content, &content) != nil {
		return false, nil
	}
	key, kinds, suffix := "childPageId", []string{"page", "row"}, ""
	written, _ := content[key].(string)
	if written == "" {
		key, kinds, suffix = "databaseContainerId", []string{"block"}, ":block"
		written, _ = content[key].(string)
	}
	moved, err := here.movedLinkTarget(written, kinds, suffix)
	if err != nil || moved == "" {
		return false, err
	}
	content[key] = moved
	encoded, err := json.Marshal(content)
	if err != nil {
		return false, err
	}
	block.Content = encoded
	return true, nil
}

// syncedBlock points a synced block's original here, in place, and tells
// whether it changed.
func (here *notionAddressLinksHere) syncedBlock(block *graphBlock) (bool, error) {
	content := map[string]any{}
	if json.Unmarshal(block.Content, &content) != nil {
		return false, nil
	}
	written, _ := content["syncedBlockId"].(string)
	moved, err := here.mentionedHere(written)
	if err != nil || moved.here || moved.nativeID == "" {
		return false, err
	}
	content["syncedBlockId"] = moved.nativeID
	encoded, err := json.Marshal(content)
	if err != nil {
		return false, err
	}
	block.Content = encoded
	return true, nil
}

// copiedGraphOperationsHere is operations with blocks of their own to change
// (copiedGraphOperations), their links and synced blocks pointed here as the
// write points its own (notionWriteTargetsHere), for the checks Notion's API
// makes of their targets before their content's faults
// (notionRequestTargetRefused).
func copiedGraphOperationsHere(app core.App, workspaceID string, operations []graphOperation) ([]graphOperation, error) {
	copied := copiedGraphOperations(operations)
	return copied, notionWriteTargetsHere(app, workspaceID, nil, copied)
}

// copiedGraphOperations is operations with blocks of their own to change.
func copiedGraphOperations(operations []graphOperation) []graphOperation {
	copied := make([]graphOperation, len(operations))
	for index, operation := range operations {
		copied[index] = operation
		if operation.Block != nil {
			block := *operation.Block
			block.Content = append(json.RawMessage(nil), operation.Block.Content...)
			copied[index].Block = &block
		}
	}
	return copied
}

// movedLinkTarget is what a Notion move made of the Notion object a link's
// ID names when nothing here answers to it, the latest move's first: a page's
// or a database page's page (kinds page and row), or a database's block,
// which is its database (kind block, the database's key ending in suffix);
// "" for none. The ID is a UUID as written, or behind
// notionPageMentionRowPrefix, as the public API's reading keeps one no page
// has by it.
func (here *notionAddressLinksHere) movedLinkTarget(written string, kinds []string, suffix string) (string, error) {
	id, isUUID := normalizeNotionUUIDRequest(strings.TrimPrefix(strings.TrimSpace(written), notionPageMentionRowPrefix))
	if !isUUID {
		return "", nil
	}
	if found, err := here.mentionedHere(id); err != nil || found.here || found.kind == "" {
		return "", err
	}
	mapped, err := notionMovedMappings(here.app, here.workspaceID, id, "")
	if err != nil {
		return "", err
	}
	for _, item := range mapped {
		if item.SourceID != id+suffix || !slices.Contains(kinds, item.Kind) {
			continue
		}
		if item.Kind == "block" {
			return item.BlockID, nil
		}
		return item.PageID, nil
	}
	return "", nil
}
