package app

// childPageChangeParents is where a change has put the blocks it places, up
// to each of its operations: the parent each block it writes or moves takes,
// at the top for none, as the body applies them in order
// (applyStructuralGraphMutation). A deletion then takes only the blocks still
// inside the block it deletes, so a sub-page block the same change moved out
// of a deleted block stays with its page, as the body keeps the block (a
// page's Markdown writing a page tag out of a quote or list it leaves out,
// seen 2026-11-17: the block stayed in the body, its page went to the trash
// with no "would delete" refusal). A block moved into one later deleted goes
// with it.
type childPageChangeParents map[string]string

// note takes the parent an operation gives a block.
func (parents childPageChangeParents) note(operation graphOperation) {
	parentOf := func(parentID *string) string {
		if parentID == nil {
			return ""
		}
		return *parentID
	}
	switch {
	case operation.Kind == graphOperationKindUpsertBlock && operation.Block != nil:
		parents[operation.Block.ID] = parentOf(operation.Block.ParentID)
	case operation.Kind == graphOperationKindMoveBlock && operation.Move != nil:
		parents[operation.Move.BlockID] = parentOf(operation.Move.ParentID)
	}
}

// deleted is the body's blocks as the change has left them so far, each
// under the parent it now has, and the blocks a deletion of root takes with
// it; those leave the body for the operations after it.
func (parents childPageChangeParents) deleted(
	view *structuralMetadataView, root string,
) (map[string]structuralBlockMetadata, []string, error) {
	whole, err := view.all()
	if err != nil {
		return nil, nil, err
	}
	metadata := make(map[string]structuralBlockMetadata, len(whole))
	for id, entry := range whole {
		parent, placed := parents[id]
		if placed && parent == childPageChangeDeleted {
			continue
		}
		if placed {
			entry.ParentID = parent
		}
		metadata[id] = entry
	}
	if _, exists := metadata[root]; !exists {
		return metadata, nil, nil
	}
	ids, err := collectStructuralSubtree(metadata, root)
	if err != nil {
		return nil, nil, err
	}
	for _, id := range ids {
		parents[id] = childPageChangeDeleted
	}
	return metadata, ids, nil
}

// childPageChangeDeleted marks a block an earlier deletion of the change
// took; no block's ID is it.
const childPageChangeDeleted = "\x00deleted"
