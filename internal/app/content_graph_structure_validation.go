package app

func graphStructureAuthorityError() error {
	return graphConflictError(
		errorCodeGraphStructureAuthorityConflict,
		"Recover the content structure, then try again.",
	)
}

func normalizeOptionalGraphID(value **string) error {
	if *value == nil {
		return nil
	}
	normalized, err := normalizeGraphID(**value)
	if err != nil {
		return err
	}
	*value = &normalized
	return nil
}

func normalizeGraphBlockMove(move *graphBlockMove) error {
	var err error
	move.BlockID, err = graphIDField("blockId", move.BlockID)
	if err != nil {
		return err
	}
	if err := graphOptionalIDField("parentId", &move.ParentID); err != nil {
		return err
	}
	if err := graphOptionalIDField("afterId", &move.AfterID); err != nil {
		return err
	}
	if move.ParentID != nil && *move.ParentID == move.BlockID {
		return graphInputError(errorCodeGraphReferenceInvalid, "A block can't be its own parent.")
	}
	if move.AfterID != nil && *move.AfterID == move.BlockID {
		return graphInputError(errorCodeGraphReferenceInvalid, "A block can't move after itself.")
	}
	return nil
}

func normalizeGraphBlockDelete(deletion *graphBlockDelete) error {
	var err error
	deletion.BlockID, err = graphIDField("blockId", deletion.BlockID)
	return err
}

// validateStructuralGraphReferences checks each block change's parent and
// anchor where the change's earlier operations leave them, as they apply in
// order: a block moved under a new parent anchors a block made after it in
// the same change (a new list item indented as the next one is typed).
func validateStructuralGraphReferences(projection graphProjection, operations []graphOperation) error {
	parents := map[string]string{}
	for index, operation := range operations {
		var blockID, parentID, field string
		var afterID *string
		switch operation.Kind {
		case graphOperationKindUpsertBlock:
			field = "block"
			blockID = operation.Block.ID
			if operation.Block.ParentID != nil {
				parentID = *operation.Block.ParentID
			}
			afterID = operation.Block.AfterID
		case graphOperationKindMoveBlock:
			field = "move"
			blockID = operation.Move.BlockID
			if _, exists := projection.blocks[blockID]; !exists {
				return graphRefused(errorCodeGraphReferenceInvalid, "The block to move was not found.",
					"operations[%d].move.blockId %q names no block of this page", index, blockID)
			}
			if operation.Move.ParentID != nil {
				parentID = *operation.Move.ParentID
			}
			afterID = operation.Move.AfterID
		case graphOperationKindDeleteBlock:
			if _, exists := projection.blocks[operation.Deletion.BlockID]; !exists {
				return graphRefused(errorCodeGraphReferenceInvalid, "The block to delete was not found.",
					"operations[%d].deletion.blockId %q names no block of this page", index, operation.Deletion.BlockID)
			}
			continue
		default:
			continue
		}
		if parentID != "" {
			if _, exists := projection.blocks[parentID]; !exists {
				return graphRefused(errorCodeGraphReferenceInvalid, "Parent block not found.",
					"operations[%d].%s.parentId %q names no block of this page or of this change (null for the page itself)", index, field, parentID)
			}
		}
		if afterID != nil {
			after, exists := projection.blocks[*afterID]
			afterParentID := after.ParentID
			if moved, ok := parents[*afterID]; ok {
				afterParentID = moved
			}
			switch {
			case !exists:
				return graphRefused(errorCodeGraphReferenceInvalid, "Block order anchor not found.",
					"operations[%d].%s.afterId %q names no block of this page or of this change", index, field, *afterID)
			case afterParentID != parentID || *afterID == blockID:
				return graphRefused(errorCodeGraphReferenceInvalid, "Block order anchor not found.",
					"operations[%d].%s.afterId %q is under parent %q, not parentId %q: afterId is a sibling under the same parent, or null", index, field, *afterID, afterParentID, parentID)
			}
		}
		parents[blockID] = parentID
	}
	return nil
}

func structuralCycleOperations(operations []graphOperation) []graphOperation {
	result := make([]graphOperation, 0, len(operations))
	for _, operation := range operations {
		switch operation.Kind {
		case graphOperationKindUpsertBlock:
			result = append(result, operation)
		case graphOperationKindMoveBlock:
			result = append(result, graphOperation{
				Kind:  graphOperationKindUpsertBlock,
				Block: &graphBlock{ID: operation.Move.BlockID, ParentID: operation.Move.ParentID},
			})
		}
	}
	return result
}
