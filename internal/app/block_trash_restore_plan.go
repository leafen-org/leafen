package app

import (
	"encoding/json"

	"github.com/pocketbase/pocketbase/core"
)

// prepareRecoverableBlockTrashRestore is what puts a trash entry's blocks
// back where they left, and after them the views its linked databases own,
// named as the entry kept them.
func prepareRecoverableBlockTrashRestore(
	app core.App,
	row storedRecoverableBlockTrash,
	snapshot recoverableBlockTrashSnapshot,
) ([]graphOperation, map[string]struct{}, error) {
	operations := make([]graphOperation, 0, len(snapshot.Blocks)+len(snapshot.Views))
	explicitStarts := map[string]struct{}{}
	for _, stored := range snapshot.Blocks {
		parentID := stored.ParentID
		var parent *string
		if parentID != "" {
			parent = &parentID
		}
		afterID := stored.AfterID
		if stored.ID == row.RootBlockID {
			afterID = row.PredecessorID
		}
		var after *string
		if afterID != "" {
			after = &afterID
		} else {
			explicitStarts[stored.ID] = struct{}{}
		}
		operations = append(operations, graphOperation{
			Kind: graphOperationKindUpsertBlock,
			Block: &graphBlock{
				ID: stored.ID, ParentID: parent, AfterID: after,
				Position: stored.Position, Type: stored.Type,
				Content: append(json.RawMessage(nil), stored.Content...),
			},
		})
	}
	return append(operations, restoreRecoverableLinkedDatabaseViews(snapshot)...), explicitStarts, nil
}
