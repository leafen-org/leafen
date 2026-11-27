package app

import (
	"encoding/json"
	"strings"

	"github.com/reearth/ygo/crdt"
)

// A revert plan compares each replayed version's blocks with the version
// before's by reading again only what the version changed: the blocks whose
// structure entry or text a transaction wrote or deleted (crdt
// Transaction.ChangedKeys and ChangedRoots), and the blocks of each parent
// whose order changed or that a block left or joined. The state's blocks were
// built and compared whole after every version from the first reverted one to
// the latest, a plan growing with the page times those versions, as the
// restore's replay did before page_history_replay_blocks.go.

// pageHistoryRevertTracker keeps a replayed state's blocks as the revert plan
// reads them.
type pageHistoryRevertTracker struct {
	structural, collaboration *crdt.Doc
	stop                      []func()
	blocks                    map[string]pageHistoryRevertBlock
	// parents is each structure entry's parent; children each parent's
	// placed blocks in order.
	parents  map[string]string
	children map[string][]string
	// changed are the blocks whose entry or text changed since the last
	// sync, reordered the parents whose order did.
	changed   map[string]struct{}
	reordered map[string]struct{}
}

// pageHistoryRevertChange is a block before and after a sync, each the zero
// block where it isn't placed.
type pageHistoryRevertChange struct {
	id            string
	before, after pageHistoryRevertBlock
}

// sync brings the blocks up to the state and gives each block whose place,
// structure or text may have changed since the last sync. A state whose
// documents are new to the tracker is read whole, every block given.
func (tracker *pageHistoryRevertTracker) sync(state pageHistoryMaterializedState) ([]pageHistoryRevertChange, error) {
	if tracker.structural != state.Structural || tracker.collaboration != state.Collaboration {
		return tracker.rebuild(state)
	}
	if len(tracker.changed) == 0 && len(tracker.reordered) == 0 {
		return nil, nil
	}
	root := state.Structural.GetMap(ygoBlockMetadataRoot)
	dirty := make(map[string]struct{}, len(tracker.reordered)+2)
	for parentID := range tracker.reordered {
		dirty[parentID] = struct{}{}
	}
	for id := range tracker.changed {
		oldParent, had := tracker.parents[id]
		value, exists := root.Get(id)
		entry := structuralBlockMetadata{}
		if exists {
			var err error
			if entry, err = structuralMetadataEntry(value); err != nil {
				return nil, err
			}
			tracker.parents[id] = entry.ParentID
		} else {
			delete(tracker.parents, id)
		}
		if had != exists || exists && entry.ParentID != oldParent {
			if had {
				dirty[oldParent] = struct{}{}
			}
			if exists {
				dirty[entry.ParentID] = struct{}{}
			}
		}
	}
	before := map[string]pageHistoryRevertBlock{}
	note := func(id string) {
		if _, noted := before[id]; !noted {
			before[id] = tracker.blocks[id]
		}
	}
	placed, left := map[string]struct{}{}, []string{}
	for parentID := range dirty {
		order, err := structuralOrder(state.Structural, parentID)
		if err != nil {
			return nil, err
		}
		children, afterID := make([]string, 0, len(order)), ""
		for _, id := range order {
			if parent, found := tracker.parents[id]; !found || parent != parentID {
				continue
			}
			old, wasPlaced := tracker.blocks[id]
			next := old
			next.AfterID = afterID
			if _, changed := tracker.changed[id]; changed || !wasPlaced || old.ParentID != parentID {
				if next, err = pageHistoryRevertBlockOf(state, root, id, parentID, afterID); err != nil {
					return nil, err
				}
			}
			if !wasPlaced || !samePageHistoryRevertBlock(old, next) {
				note(id)
				tracker.blocks[id] = next
			}
			placed[id] = struct{}{}
			children = append(children, id)
			afterID = id
		}
		for _, id := range tracker.children[parentID] {
			if _, still := placed[id]; !still {
				left = append(left, id)
			}
		}
		tracker.children[parentID] = children
	}
	for _, id := range left {
		if _, elsewhere := placed[id]; !elsewhere {
			note(id)
			delete(tracker.blocks, id)
		}
	}
	// A block whose parent's order didn't change keeps its place.
	for id := range tracker.changed {
		old, wasPlaced := tracker.blocks[id]
		if _, done := placed[id]; done || !wasPlaced {
			continue
		}
		next, err := pageHistoryRevertBlockOf(state, root, id, old.ParentID, old.AfterID)
		if err != nil {
			return nil, err
		}
		if !samePageHistoryRevertBlock(old, next) {
			note(id)
			tracker.blocks[id] = next
		}
	}
	clear(tracker.changed)
	clear(tracker.reordered)
	changes := make([]pageHistoryRevertChange, 0, len(before))
	for id, block := range before {
		changes = append(changes, pageHistoryRevertChange{id: id, before: block, after: tracker.blocks[id]})
	}
	return changes, nil
}

// rebuild reads the state's blocks whole and watches its documents.
func (tracker *pageHistoryRevertTracker) rebuild(state pageHistoryMaterializedState) ([]pageHistoryRevertChange, error) {
	for _, stop := range tracker.stop {
		stop()
	}
	previous := tracker.blocks
	*tracker = pageHistoryRevertTracker{
		structural: state.Structural, collaboration: state.Collaboration,
		blocks: map[string]pageHistoryRevertBlock{}, parents: map[string]string{},
		children: map[string][]string{}, changed: map[string]struct{}{}, reordered: map[string]struct{}{},
	}
	metadata, err := structuralMetadata(state.Structural)
	if err != nil {
		return nil, err
	}
	dirty := map[string]struct{}{"": {}}
	for id, entry := range metadata {
		tracker.parents[id] = entry.ParentID
		dirty[entry.ParentID] = struct{}{}
	}
	root := state.Structural.GetMap(ygoBlockMetadataRoot)
	for parentID := range dirty {
		order, err := structuralOrder(state.Structural, parentID)
		if err != nil {
			return nil, err
		}
		afterID := ""
		for _, id := range order {
			if entry, found := metadata[id]; !found || entry.ParentID != parentID {
				continue
			}
			if tracker.blocks[id], err = pageHistoryRevertBlockOf(state, root, id, parentID, afterID); err != nil {
				return nil, err
			}
			tracker.children[parentID] = append(tracker.children[parentID], id)
			afterID = id
		}
	}
	tracker.stop = []func(){
		state.Structural.OnAfterTransaction(func(transaction *crdt.Transaction) {
			for _, id := range transaction.ChangedKeys(root) {
				tracker.changed[id] = struct{}{}
			}
			for _, name := range transaction.ChangedRoots() {
				if parentID, isOrder := strings.CutPrefix(name, ygoBlockOrderRootPrefix); isOrder {
					if parentID == ygoRootParentToken {
						parentID = ""
					}
					tracker.reordered[parentID] = struct{}{}
				}
			}
		}),
		state.Collaboration.OnAfterTransaction(func(transaction *crdt.Transaction) {
			for _, name := range transaction.ChangedRoots() {
				if id, isText := strings.CutPrefix(name, ygoBlockTextRootPrefix); isText {
					tracker.changed[id] = struct{}{}
				}
			}
		}),
	}
	changes := make([]pageHistoryRevertChange, 0, len(previous)+len(tracker.blocks))
	for id := range unionKeys(previous, tracker.blocks) {
		changes = append(changes, pageHistoryRevertChange{id: id, before: previous[id], after: tracker.blocks[id]})
	}
	return changes, nil
}

// pageHistoryRevertBlockOf is the block with the id, placed under the parent
// after the sibling given, as the state holds it.
func pageHistoryRevertBlockOf(
	state pageHistoryMaterializedState, root *crdt.YMap, id, parentID, afterID string,
) (pageHistoryRevertBlock, error) {
	value, _ := root.Get(id)
	entry, err := structuralMetadataEntry(value)
	if err != nil {
		return pageHistoryRevertBlock{}, err
	}
	content, err := json.Marshal(entry.Content)
	if err != nil {
		return pageHistoryRevertBlock{}, err
	}
	spans := graphTextSpansFromYText(state.Collaboration.GetText(ygoBlockTextRootPrefix + id))
	text, err := json.Marshal(spans)
	if err != nil {
		return pageHistoryRevertBlock{}, err
	}
	return pageHistoryRevertBlock{
		Present: true, ParentID: parentID, AfterID: afterID, Type: entry.Type,
		Content: content, Spans: spans, key: pageHistoryRevertBlockKey(parentID, entry.Type, content, text),
	}, nil
}

func samePageHistoryRevertBlock(left, right pageHistoryRevertBlock) bool {
	return left.Present == right.Present && left.ParentID == right.ParentID &&
		left.AfterID == right.AfterID && left.key == right.key
}

func finalRevertParent(plan *pageHistoryRevertPlan, id string) string {
	if target, found := plan.Blocks[id]; found && target.Present {
		return target.ParentID
	}
	return plan.Current[id].ParentID
}
