package app

import (
	"errors"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// captureNotionBlockAppendResponse captures the blocks an append or an
// update wrote, as the answer to that write gives them
// (notionTableCellMentionItems).
func captureNotionBlockAppendResponse(
	identity notionOAuthBearerIdentity,
	pageID string,
	parentID string,
	blockIDs []string,
	fileIssuer *notionBlockFileCapabilityIssuer,
	result *[]map[string]any,
) func(core.App, pageAuthorization, *crdt.Doc) error {
	return captureNotionBlockObjects(identity, pageID, parentID, blockIDs, fileIssuer, result, true)
}

// notionTableCellMentionItems is a table row cell's rich text as an answer
// gives it. Notion's API reads every page mention in a table row's cells as
// an unnamed one (notionTableCellMentionItem) — reading the row, its table's
// children, or the row deleted or restored — but the answer to the write of
// its cells, an update or an append, names each by its page's title now
// with its formatting, as a line's mention reads (seen twice 2026-11-18,
// Notion-Version 2022-06-28: "W1813R2" after a rename, "New page" for a page
// with no title, bold and red kept; the next read "Untitled", plain).
func notionTableCellMentionItems(authorization notionBlockAuthorization, items []map[string]any) []map[string]any {
	if authorization.Written {
		return items
	}
	for _, item := range items {
		notionTableCellMentionItem(item)
	}
	return items
}

// captureNotionBlockObjects captures blocks once the write is in, as the
// answer to that write (written) or as a read gives them.
func captureNotionBlockObjects(
	identity notionOAuthBearerIdentity,
	pageID string,
	parentID string,
	blockIDs []string,
	fileIssuer *notionBlockFileCapabilityIssuer,
	result *[]map[string]any,
	written bool,
) func(core.App, pageAuthorization, *crdt.Doc) error {
	return func(
		app core.App,
		current pageAuthorization,
		_ *crdt.Doc,
	) error {
		document, err := loadNotionBlockTextDocument(app, pageID)
		if err != nil {
			return err
		}
		counts, err := loadNotionBlockChildCounts(app, blockIDs)
		if err != nil {
			return err
		}
		values := make([]map[string]any, 0, len(blockIDs))
		for _, blockID := range blockIDs {
			row, found, err := loadStoredNotionBlock(app, blockID)
			if err != nil {
				return err
			}
			if !found || row.PageID != pageID || row.ParentID != parentID ||
				row.Owner != current.Workspace.OwnerID {
				return errors.New("created Notion block response changed before commit")
			}
			value, err := notionBlockObject(app, notionBlockAuthorization{
				Identity: identity, Page: current, Block: row, Written: written,
			}, row, counts[row.ID] > 0, document, fileIssuer)
			if err != nil {
				return err
			}
			values = append(values, value)
		}
		*result = values
		return nil
	}
}

func captureNotionBlockBeforeUpdatedDeleteResponse(
	identity notionOAuthBearerIdentity,
	pageID string,
	blockID string,
	updatedBlock *graphBlock,
	preparedTextSpans func() *[]graphTextSpan,
	fileIssuer *notionBlockFileCapabilityIssuer,
	result *map[string]any,
) func(core.App, pageAuthorization, *crdt.Doc, []graphOperation) error {
	return func(
		app core.App,
		current pageAuthorization,
		_ *crdt.Doc,
		operations []graphOperation,
	) error {
		if len(operations) != 2 || operations[0].Kind != graphOperationKindUpsertBlock ||
			operations[0].Block != updatedBlock || updatedBlock == nil || updatedBlock.ID != blockID ||
			operations[1].Kind != graphOperationKindDeleteBlock || operations[1].Deletion == nil ||
			operations[1].Deletion.BlockID != blockID {
			return errors.New("updated Notion block delete response plan changed before commit")
		}
		row, found, err := loadStoredNotionBlock(app, blockID)
		if err != nil {
			return err
		}
		parentID := ""
		if updatedBlock.ParentID != nil {
			parentID = *updatedBlock.ParentID
		}
		if !found || row.PageID != pageID || row.Owner != current.Workspace.OwnerID ||
			row.ParentID != parentID || row.Type != updatedBlock.Type {
			return errors.New("updated Notion block delete response changed before commit")
		}
		row.Content = string(updatedBlock.Content)
		row.LastEditedBy = identity.PrincipalID
		row.Updated = time.Now().UTC().UnixMilli()
		document, err := loadNotionBlockTextDocument(app, pageID)
		if err != nil {
			return err
		}
		if spans := preparedTextSpans(); spans != nil {
			replaceNotionBlockResponseText(document, blockID, *spans)
		}
		counts, err := loadNotionBlockChildCounts(app, []string{blockID})
		if err != nil {
			return err
		}
		value, err := notionBlockObject(app, notionBlockAuthorization{
			Identity: identity, Page: current, Block: row,
		}, row, counts[row.ID] > 0, document, fileIssuer)
		if err != nil {
			return err
		}
		value["in_trash"] = true
		*result = value
		return nil
	}
}

func replaceNotionBlockResponseText(
	document *crdt.Doc,
	blockID string,
	spans []graphTextSpan,
) {
	text := document.GetText(ygoBlockTextRootPrefix + blockID)
	document.Transact(func(transaction *crdt.Transaction) {
		if text.Len() > 0 {
			text.Delete(transaction, 0, text.Len())
		}
		insertGraphTextSpans(transaction, text, spans)
	})
}

func captureNotionBlockBeforeDeleteResponse(
	identity notionOAuthBearerIdentity,
	pageID string,
	blockID string,
	fileIssuer *notionBlockFileCapabilityIssuer,
	result *map[string]any,
) func(core.App, pageAuthorization, *crdt.Doc) error {
	return func(
		app core.App,
		current pageAuthorization,
		_ *crdt.Doc,
	) error {
		row, found, err := loadStoredNotionBlock(app, blockID)
		if err != nil {
			return err
		}
		if !found || row.PageID != pageID || row.Owner != current.Workspace.OwnerID {
			return graphNotFoundError()
		}
		document, err := loadNotionBlockTextDocument(app, pageID)
		if err != nil {
			return err
		}
		counts, err := loadNotionBlockChildCounts(app, []string{blockID})
		if err != nil {
			return err
		}
		value, err := notionBlockObject(app, notionBlockAuthorization{
			Identity: identity, Page: current, Block: row,
		}, row, counts[row.ID] > 0, document, fileIssuer)
		if err != nil {
			return err
		}
		*result = value
		return nil
	}
}
