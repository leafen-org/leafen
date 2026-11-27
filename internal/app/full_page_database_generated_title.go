package app

import (
	"encoding/json"

	"github.com/pocketbase/pocketbase/core"
)

// The linked starter gives navigation a name while its large title invites
// writing. Once a title is written, that presentation marker is removed through
// the same canonical graph owner used for the database's name; so is the
// formatting a database's page keeps for its name (database_page_name_spans.go)
// once the title is other words, but on the words it keeps.
func clearGeneratedDatabaseTitle(app core.App, ownerID, pageID string, operations []graphOperation, edited bool, title string) ([]graphOperation, error) {
	stored, found, err := loadDatabasePageBlock(app, ownerID, pageID)
	if err != nil || !found {
		return operations, err
	}
	content := map[string]any{}
	if json.Unmarshal([]byte(stored.Content), &content) != nil {
		return operations, nil
	}
	clear := func(content map[string]any, raw []byte) bool {
		generated, _ := content["generatedPageTitle"].(string)
		cleared := false
		if content["linkedDatabaseSource"] == true && generated != "" && (edited || generated != title) {
			delete(content, "generatedPageTitle")
			cleared = true
		}
		if _, formatted := content["pageTitleSpans"]; formatted && databasePageNameSpans(raw, title) == nil {
			// Renamed, the name keeps the formatting and mentions of the words
			// it keeps (renamedDataSource); none left, none stay.
			if kept := respelledPageTitle(blockPageNameSpans(raw, "pageTitleSpans"), title); spansFormatted(kept) {
				content["pageTitleSpans"] = kept
			} else {
				delete(content, "pageTitleSpans")
			}
			cleared = true
		}
		return cleared
	}
	next := append([]graphOperation(nil), operations...)
	for index, operation := range next {
		if operation.Kind != graphOperationKindUpsertBlock || operation.Block == nil || operation.Block.ID != stored.ID {
			continue
		}
		block := *operation.Block
		changed := map[string]any{}
		if err := json.Unmarshal(block.Content, &changed); err != nil {
			return nil, err
		}
		if !clear(changed, block.Content) {
			return operations, nil
		}
		block.Content, _ = json.Marshal(changed)
		next[index].Block = &block
		return next, nil
	}
	if !clear(content, []byte(stored.Content)) {
		return operations, nil
	}
	encoded, _ := json.Marshal(content)
	block := graphBlock{ID: stored.ID, Type: graphBlockTypeInlineDatabase, Position: stored.Position, Content: encoded, BaseContent: json.RawMessage(stored.Content)}
	return append(next, graphOperation{Kind: graphOperationKindUpsertBlock, Block: &block}), nil
}

// spansFormatted reports spans with more than plain words: a mention, a
// link or a mark.
func spansFormatted(spans []graphTextSpan) bool {
	for _, span := range spans {
		if span != (graphTextSpan{Text: span.Text}) {
			return true
		}
	}
	return false
}
