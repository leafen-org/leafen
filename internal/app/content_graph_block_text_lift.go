package app

import (
	"encoding/json"

	"github.com/reearth/ygo/crdt"
)

// A text block's words live in its page's collaborative document. A change
// that writes them into the block's content instead — `rich`, or the other
// forms the row block table kept (rowBodyStoredTextKeys) — gives them to the
// document as the change's text, as a new page's first body does (2026-10-15:
// a database page's paragraph written with `content.rich` showed no words,
// was exported with them, and its duplicate lost them; a page's the same).
// The document keeps words a block already has there: content words for such
// a block are left out, so a block an older change stored that way gets its
// words the next time it is written.

// liftGraphBlockStoredText takes the words out of a change's text blocks'
// contents into its text updates, unless the change gives a block's text
// itself, and names the blocks whose words it took.
func liftGraphBlockStoredText(input *graphMutationInput) (map[string]struct{}, error) {
	given := make(map[string]struct{}, len(input.TextUpdates))
	for _, update := range input.TextUpdates {
		given[update.BlockID] = struct{}{}
	}
	lifted := map[string]struct{}{}
	for index, operation := range input.Operations {
		if operation.Kind != graphOperationKindUpsertBlock || operation.Block == nil {
			continue
		}
		if _, text := graphTextBlockTypes[operation.Block.Type]; !text {
			continue
		}
		content, spans, found, err := withoutStoredBlockText(operation.Block.Content)
		if err != nil || !found {
			if err != nil {
				return nil, err
			}
			continue
		}
		block := *operation.Block
		block.Content = content
		if base, _, baseFound, err := withoutStoredBlockText(block.BaseContent); err != nil {
			return nil, err
		} else if baseFound {
			block.BaseContent = base
		}
		input.Operations[index].Block = &block
		if _, own := given[block.ID]; own || len(spans) == 0 {
			continue
		}
		if _, twice := lifted[block.ID]; twice {
			continue
		}
		input.TextUpdates = append(input.TextUpdates, graphTextUpdate{BlockID: block.ID, Spans: spans})
		lifted[block.ID] = struct{}{}
	}
	return lifted, nil
}

// withoutStoredBlockText is a block's content without its words, and the
// words; found says whether it held any form of them.
func withoutStoredBlockText(raw json.RawMessage) (json.RawMessage, []graphTextSpan, bool, error) {
	if len(raw) == 0 {
		return raw, nil, false, nil
	}
	fields := map[string]json.RawMessage{}
	if json.Unmarshal(raw, &fields) != nil {
		// Content that isn't an object is checked where the block is made.
		return raw, nil, false, nil
	}
	stored := map[string]any{}
	for _, key := range rowBodyStoredTextKeys {
		value, present := fields[key]
		if !present {
			continue
		}
		var decoded any
		if json.Unmarshal(value, &decoded) == nil {
			stored[key] = decoded
		}
		delete(fields, key)
	}
	if len(stored) == 0 {
		return raw, nil, false, nil
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		return nil, nil, false, err
	}
	return encoded, databaseRowBlockTextSpans(stored), true, nil
}

// withoutLiftedTextOverWords leaves out the lifted text of blocks whose words
// the page's document already holds. It reads the document the change's own
// text goes into (applyContentGraphTextUpdates): read for this first and
// handed over through the kept documents, a change to a page people typed in
// read the page's whole document twice while it held the write connection
// (2026-10-15, and again 2026-10-20 whenever the hand-over missed).
func withoutLiftedTextOverWords(document *crdt.Doc, updates []graphTextUpdate, lifted map[string]struct{}) []graphTextUpdate {
	if len(lifted) == 0 {
		return updates
	}
	kept := make([]graphTextUpdate, 0, len(updates))
	for _, update := range updates {
		if _, fromContent := lifted[update.BlockID]; fromContent && document.GetText(ygoBlockTextRootPrefix+update.BlockID).Len() > 0 {
			continue
		}
		kept = append(kept, update)
	}
	return kept
}
