package app

import (
	"bytes"
	"encoding/json"

	"github.com/reearth/ygo/crdt"
)

var errGraphBlockContentConflict = graphConflictError(errorCodeGraphRevisionConflict, "The block changed elsewhere. Try again.")

func graphBlockContentWasMerged(requested, resolved []graphOperation) bool {
	for index, operation := range requested {
		if operation.Kind == graphOperationKindUpsertBlock && operation.Block != nil &&
			len(operation.Block.BaseContent) != 0 && !bytes.Equal(operation.Block.Content, resolved[index].Block.Content) {
			return true
		}
	}
	return false
}

// The optional base is a writer's observation, never another stored authority.
// Resolve it inside the authorized native transaction, before validating and
// publishing the resulting canonical block. Legacy snapshot upserts stay intact.
func resolveGraphBlockContentUpdates(doc *crdt.Doc, operations []graphOperation) ([]graphOperation, error) {
	var metadata map[string]structuralBlockMetadata
	var resolved []graphOperation
	for index, operation := range operations {
		if operation.Kind != graphOperationKindUpsertBlock || operation.Block == nil || len(operation.Block.BaseContent) == 0 {
			continue
		}
		if metadata == nil {
			var err error
			metadata, err = structuralMetadata(doc)
			if err != nil {
				return nil, err
			}
		}
		block := *operation.Block
		current, exists := metadata[block.ID]
		if !exists || current.Type != block.Type {
			return nil, errGraphBlockContentConflict
		}
		base, next := map[string]any{}, map[string]any{}
		if json.Unmarshal(block.BaseContent, &base) != nil || json.Unmarshal(block.Content, &next) != nil {
			return nil, graphInputError(errorCodeGraphInvalid, "Check the block content.")
		}
		merged, err := mergeGraphBlockContent(base, next, current.Content)
		if err != nil {
			return nil, err
		}
		block.Content, err = json.Marshal(merged)
		if err != nil {
			return nil, err
		}
		block.BaseContent = nil
		if err := normalizeGraphBlock(&block); err != nil {
			return nil, err
		}
		if resolved == nil {
			resolved = append([]graphOperation(nil), operations...)
		}
		resolved[index].Block = &block
	}
	if resolved == nil {
		return operations, nil
	}
	return resolved, nil
}

func mergeGraphBlockContent(base, next, current map[string]any) (map[string]any, error) {
	merged := map[string]any{}
	for key, value := range current {
		merged[key] = value
	}
	keys := map[string]struct{}{}
	for _, side := range []map[string]any{base, next} {
		for key := range side {
			keys[key] = struct{}{}
		}
	}
	for key := range keys {
		before, wanted, stored := graphViewMergeValue(base, key), graphViewMergeValue(next, key), graphViewMergeValue(current, key)
		if graphViewValuesEqual(before, wanted) {
			continue
		}
		value := wanted
		if !graphViewValuesEqual(stored, before) && !graphViewValuesEqual(stored, wanted) {
			var err error
			switch key {
			case "cells":
				value, err = mergeGraphBlockCells(before, wanted, stored)
			case "caption":
				value, err = mergeGraphBlockRichValue(before, wanted, stored)
			default:
				err = errGraphBlockContentConflict
			}
			if err != nil {
				return nil, err
			}
		}
		if _, missing := value.(graphViewMergeMissing); missing {
			delete(merged, key)
		} else {
			merged[key] = value
		}
	}
	return merged, nil
}

func mergeGraphBlockCells(base, next, current any) (any, error) {
	b, bok := base.([]any)
	n, nok := next.([]any)
	c, cok := current.([]any)
	if !bok || !nok || !cok || len(b) != len(n) || len(n) != len(c) {
		return nil, errGraphBlockContentConflict
	}
	result := append([]any(nil), c...)
	for index := range b {
		if graphViewValuesEqual(b[index], n[index]) {
			continue
		}
		if graphViewValuesEqual(c[index], b[index]) || graphViewValuesEqual(c[index], n[index]) {
			result[index] = n[index]
		} else {
			var err error
			result[index], err = mergeGraphBlockRichValue(b[index], n[index], c[index])
			if err != nil {
				return nil, err
			}
		}
	}
	return result, nil
}

func mergeGraphBlockRichValue(base, next, current any) (any, error) {
	// An absent caption is the empty editable field. Concurrent first input
	// has two deltas from empty, while deletion of a peer-edited caption still
	// conflicts (the wanted/current missing value is never treated as empty).
	if _, missing := base.(graphViewMergeMissing); missing {
		base = []any{}
	}
	spans := make([][]graphTextSpan, 3)
	for index, value := range []any{base, next, current} {
		encoded, err := json.Marshal(value)
		if err != nil || json.Unmarshal(encoded, &spans[index]) != nil || spans[index] == nil {
			return nil, errGraphBlockContentConflict
		}
	}
	merged, err := mergeGraphRichText(spans[0], spans[1], spans[2])
	if err != nil {
		return nil, err
	}
	// Return the same JSON value representation as the native metadata map.
	encoded, err := json.Marshal(merged)
	var value any
	if err != nil || json.Unmarshal(encoded, &value) != nil {
		return nil, errGraphBlockContentConflict
	}
	return value, nil
}
