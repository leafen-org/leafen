package app

import (
	"encoding/json"

	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// Rejecting a suggested break joins the two blocks it stands between into
// the block that was there before it. In the middle of a block's text that
// is the block before the break, which takes the text after the break back
// at its end. At the start of a block's text (AtStart) it is the block after
// the break, the block the break was suggested in: it takes the new block's
// text, if any went in, at its start and keeps its id, kind, formatting and
// comments, as rejecting anything puts back what was there (Notion's own
// reject of a line break does nothing, seen 2026-11-22). The block that goes
// hands the blocks inside it to the block kept — after the kept block's own
// at the end, before them at the start — and a break suggested at its end
// now ends the block kept.

// suggestionSplitJoin is how a rejected break's blocks join: the block kept
// and the block that goes, and the break left at the joined block's end,
// with the block after it ("" for none).
type suggestionSplitJoin struct {
	kept, gone      string
	atStart         bool
	mark, markBlock string
}

// planSuggestionSplitJoin is the join of a rejected break (rejected) between
// the block before it (anchorID) and the block after it (splitID), and the
// changes to the page's blocks that make it.
func planSuggestionSplitJoin(
	doc *crdt.Doc, metadata map[string]structuralBlockMetadata, anchorID, splitID, rejected string, atStart bool,
) (suggestionSplitJoin, []graphOperation, error) {
	join := suggestionSplitJoin{kept: anchorID, gone: splitID}
	if atStart {
		join.kept, join.gone, join.atStart = splitID, anchorID, true
	}
	// The joined block ends where the block after the break ends.
	if mark, _ := metadata[splitID].Content[graphBlockSplitSuggestionKey].(string); mark != rejected {
		join.mark = mark
		join.markBlock, _ = metadata[splitID].Content[graphBlockSplitBlockKey].(string)
	}
	kept, gone := metadata[join.kept], metadata[join.gone]
	parentID, after := kept.ParentID, (*string)(nil)
	if kept.ParentID == join.gone {
		// The block kept stood inside the block that goes: it takes its place.
		goneID := join.gone
		parentID, after = gone.ParentID, &goneID
	}
	keptBlock, err := suggestionSplitJoinBlock(join.kept, parentID, after, kept)
	if err != nil {
		return join, nil, err
	}
	operations := []graphOperation{{Kind: graphOperationKindUpsertBlock, Block: keptBlock}}
	children, err := structuralOrder(doc, join.gone)
	if err != nil {
		return join, nil, err
	}
	// At the end the moved blocks follow the kept block's own; at the start
	// they go first.
	last := ""
	if !atStart {
		own, err := structuralOrder(doc, join.kept)
		if err != nil {
			return join, nil, err
		}
		if len(own) > 0 {
			last = own[len(own)-1]
		}
	}
	for _, childID := range children {
		if childID == join.kept {
			continue
		}
		var after *string
		if last != "" {
			previous := last
			after = &previous
		}
		child, err := suggestionSplitJoinBlock(childID, join.kept, after, metadata[childID])
		if err != nil {
			return join, nil, err
		}
		operations = append(operations, graphOperation{Kind: graphOperationKindUpsertBlock, Block: child})
		last = childID
	}
	operations = append(operations, graphOperation{
		Kind: graphOperationKindDeleteBlock, Deletion: &graphBlockDelete{BlockID: join.gone},
	})
	return join, operations, nil
}

// suggestionSplitJoinBlock is a block as it is, under a parent, after a
// sibling or first (nil) when it moves there.
func suggestionSplitJoinBlock(id, parentID string, after *string, entry structuralBlockMetadata) (*graphBlock, error) {
	content, err := json.Marshal(entry.Content)
	if err != nil {
		return nil, err
	}
	var parent *string
	if parentID != "" {
		parent = &parentID
	}
	return &graphBlock{ID: id, ParentID: parent, AfterID: after, Position: "p", Type: entry.Type, Content: content}, nil
}

// text moves the text of the block that goes into the block kept, as a
// change to the page's collaborative text: to its end, or to its start when
// the break was at the start.
func (join suggestionSplitJoin) text(app core.App, pageID, actorID string) error {
	_, err := changeSuggestionSplitText(app, pageID, actorID, func(document *crdt.Doc) func(*crdt.Transaction) {
		moved := suggestionTextDeltaFrom(document.GetText(ygoBlockTextRootPrefix+join.gone).ToDelta(), 0)
		return func(transaction *crdt.Transaction) {
			into := transaction.GetText(ygoBlockTextRootPrefix + join.kept)
			if len(moved) > 0 {
				if !join.atStart {
					moved = append([]crdt.Delta{{Op: crdt.DeltaOpRetain, Retain: into.Len()}}, moved...)
				}
				into.ApplyDelta(transaction, moved)
			}
			from := transaction.GetText(ygoBlockTextRootPrefix + join.gone)
			if length := from.Len(); length > 0 {
				from.Delete(transaction, 0, length)
			}
		}
	})
	return err
}

// startSplitBlockContent is the content of the new block a break at the
// start of a block's text puts before it, of the given kind: a list item of
// the block's own kind takes its box and number as Enter at a line's start
// does while editing (nativeTiptapKeys.ts) — Notion's empty to-do before a
// checked one is checked while suggesting too (seen 2026-11-22).
func startSplitBlockContent(anchor structuralBlockMetadata, blockType string) (json.RawMessage, error) {
	content := map[string]any{}
	if blockType == anchor.Type {
		keys := map[string][]string{
			graphBlockTypeToDo:             {"checked"},
			graphBlockTypeNumberedListItem: {"listStartIndex", "listFormat"},
			graphBlockTypeBulletedListItem: {"listFormat"},
		}[blockType]
		for _, key := range keys {
			if value, present := anchor.Content[key]; present {
				content[key] = value
			}
		}
	}
	return json.Marshal(content)
}
