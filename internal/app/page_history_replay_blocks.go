package app

import "github.com/reearth/ygo/crdt"

// A version's replay dates the blocks each structure update changed by
// reading only those: the blocks the update wrote or deleted, which the
// document names after each transaction (crdt Transaction.ChangedKeys),
// against the metadata kept from the event before. Every block read and
// compared before and after each event made a replay grow with the page times
// its versions (2026-11-18: a page with 8,400 blocks deleted a subtree at a
// time took 6 s on this Mac to replay its 756 versions for one restore,
// nearly all of it on blocks the versions didn't touch).

// pageHistoryReplayBlocks keeps a replayed structure's block metadata.
type pageHistoryReplayBlocks struct {
	doc      *crdt.Doc
	metadata map[string]structuralBlockMetadata
	// changed are the blocks transactions wrote or deleted since the last
	// settle.
	changed map[string]struct{}
}

// pageHistoryBlockChange is one block's metadata before and after a settle.
type pageHistoryBlockChange struct {
	id              string
	before, after   structuralBlockMetadata
	existed, exists bool
}

// replayBlocks is the state's kept block metadata, read whole the first time
// and for the structure the state holds now.
func (state *pageHistoryMaterializedState) replayBlocks() (*pageHistoryReplayBlocks, error) {
	if state.blocks != nil && state.blocks.doc == state.Structural {
		return state.blocks, nil
	}
	metadata, err := structuralMetadata(state.Structural)
	if err != nil {
		return nil, err
	}
	blocks := &pageHistoryReplayBlocks{
		doc: state.Structural, metadata: metadata, changed: map[string]struct{}{},
	}
	root := state.Structural.GetMap(ygoBlockMetadataRoot)
	state.Structural.OnAfterTransaction(func(transaction *crdt.Transaction) {
		for _, id := range transaction.ChangedKeys(root) {
			blocks.changed[id] = struct{}{}
		}
	})
	state.blocks = blocks
	return blocks, nil
}

// settle brings the kept metadata up to the structure and gives each block
// changed since the last settle, read as structuralMetadataView reads one.
func (blocks *pageHistoryReplayBlocks) settle() ([]pageHistoryBlockChange, error) {
	if len(blocks.changed) == 0 {
		return nil, nil
	}
	root := blocks.doc.GetMap(ygoBlockMetadataRoot)
	changes := make([]pageHistoryBlockChange, 0, len(blocks.changed))
	for id := range blocks.changed {
		change := pageHistoryBlockChange{id: id}
		change.before, change.existed = blocks.metadata[id]
		if value, found := root.Get(id); found {
			entry, err := structuralMetadataEntry(value)
			if err != nil {
				return nil, err
			}
			change.after, change.exists = entry, true
			blocks.metadata[id] = entry
		} else {
			delete(blocks.metadata, id)
		}
		changes = append(changes, change)
	}
	clear(blocks.changed)
	return changes, nil
}

// applyPageHistoryStructureUpdate applies a version's structure update and
// dates the blocks it changed at the version: one added or changed takes its
// time as its last edit (and as its creation when it had none), and one
// removed lets its times go.
func applyPageHistoryStructureUpdate(
	state *pageHistoryMaterializedState, envelope pageHistoryEventEnvelope, createdAt int64,
) error {
	blocks, err := state.replayBlocks()
	if err != nil {
		return err
	}
	// What changed the structure since the event before isn't this one's.
	if _, err := blocks.settle(); err != nil {
		return err
	}
	if err := crdt.ApplyUpdateV1(state.Structural, envelope.Update, nil); err != nil {
		return err
	}
	state.applyDatabaseChanges(envelope.Databases)
	changes, err := blocks.settle()
	if err != nil {
		return err
	}
	for _, change := range changes {
		switch {
		case change.exists:
			if change.existed && pageHistoryMetadataEqual(change.before, change.after) {
				continue
			}
			times := state.BlockTimes[change.id]
			if times.CreatedAt == 0 {
				times.CreatedAt = createdAt
			}
			times.UpdatedAt = createdAt
			state.BlockTimes[change.id] = times
		case change.existed:
			delete(state.BlockTimes, change.id)
		}
	}
	return nil
}
