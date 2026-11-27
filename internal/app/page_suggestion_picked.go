package app

import (
	"encoding/json"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A block a menu picks while suggesting — the "+" beside a block's handle,
// or a "/" row picked at the end of a line with words — is a suggestion of
// its own, as Notion's (measured 2026-11-18 in Chrome at 1440, the owner
// and a guest with comment access, two tabs each): it never grows a
// suggestion before it, its card reads "추가: 블록" and then its words, and
// the line before it shows no "⏎", since no line break is suggested, only
// the block (kind "block" with picked). Its content marks it alone
// (suggestionAlone) while its suggestion is open; settling it, or
// suggesting it again, clears the mark with the suggestion.

const graphBlockSuggestionAloneKey = "suggestionAlone"

// keepGraphBlockSuggestion is the content a change sends for a suggested
// block, with the block's suggestion and its alone mark kept.
func keepGraphBlockSuggestion(content, existing map[string]any, suggestionID string) (json.RawMessage, error) {
	content[graphBlockSuggestionKey] = suggestionID
	delete(content, graphBlockSuggestionAloneKey)
	if alone, _ := existing[graphBlockSuggestionAloneKey].(bool); alone {
		content[graphBlockSuggestionAloneKey] = true
	}
	return json.Marshal(content)
}

// markStructuralBlockAlone marks a suggested block as a block of its own,
// in the page's structure and its block row.
func markStructuralBlockAlone(app core.App, doc *crdt.Doc, owner, pageID, blockID string) error {
	metadata, err := structuralMetadata(doc)
	if err != nil {
		return err
	}
	entry, ok := metadata[blockID]
	if !ok {
		return graphNotFoundError()
	}
	content := make(map[string]any, len(entry.Content)+1)
	for key, value := range entry.Content {
		content[key] = value
	}
	content[graphBlockSuggestionAloneKey] = true
	doc.Transact(func(transaction *crdt.Transaction) {
		transaction.GetMap(ygoBlockMetadataRoot).Set(transaction, blockID, map[string]any{
			"parentId": entry.ParentID, "type": entry.Type, "content": content,
		})
	})
	encoded, err := json.Marshal(content)
	if err != nil {
		return err
	}
	_, err = app.DB().NewQuery(`
		UPDATE {{leafen_blocks}} SET [[content]]={:content}
		WHERE [[owner]]={:owner} AND [[page_id]]={:pageId} AND [[id]]={:id}
	`).Bind(dbx.Params{"content": string(encoded), "owner": owner, "pageId": pageID, "id": blockID}).Execute()
	return err
}
