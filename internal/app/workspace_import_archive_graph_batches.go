package app

func workspaceImportGraphOperationBatches(
	operations []graphOperation,
	limit int,
) [][]graphOperation {
	if len(operations) == 0 || limit < 2 {
		return nil
	}
	batches := make([][]graphOperation, 0, (len(operations)+limit-1)/limit)
	for start := 0; start < len(operations); {
		end := min(start+limit, len(operations))
		if end < len(operations) && (workspaceImportTableParentAndFirstRowOperations(
			operations[end-1], operations[end],
		) || workspaceImportArchiveLinkedPair(&operations[end-1], &operations[end])) {
			end--
		}
		batches = append(batches, operations[start:end])
		start = end
	}
	return batches
}

func workspaceImportTableParentAndFirstRowOperations(parent, row graphOperation) bool {
	return parent.Kind == graphOperationKindUpsertBlock && parent.Block != nil &&
		parent.Block.Type == graphBlockTypeTable && row.Kind == graphOperationKindUpsertBlock &&
		row.Block != nil && row.Block.Type == graphBlockTypeTableRow && row.Block.ParentID != nil &&
		parent.Block.ID == *row.Block.ParentID
}
