package app

import (
	"errors"
	"math"
)

func normalizeNotionColumnMutation(
	blockType string,
	incoming map[string]any,
	current map[string]any,
	existing *storedNotionBlock,
) (string, map[string]any, *[]graphTextSpan, error) {
	switch blockType {
	case graphBlockTypeColumnList:
		if len(incoming) != 0 {
			return "", nil, nil, errors.New("column_list content must be empty")
		}
		if existing != nil {
			return graphBlockTypeColumnList, current, nil, nil
		}
		return blockType, map[string]any{}, nil, nil
	case graphBlockTypeColumn:
		if err := allowOnlyNotionBlockKeys(incoming, "width_ratio"); err != nil {
			return "", nil, nil, err
		}
		if rawRatio, specified := incoming["width_ratio"]; specified {
			ratio, ok := rawRatio.(float64)
			if !ok || math.IsNaN(ratio) || math.IsInf(ratio, 0) || ratio <= 0 || ratio >= 1 {
				return "", nil, nil, errors.New("column.width_ratio must be between zero and one")
			}
			current["width"] = ratio * 100
		}
		return blockType, current, nil, nil
	default:
		return "", nil, nil, errors.New("this block type is not a column block")
	}
}

func notionColumnPayload(content map[string]any) (map[string]any, bool) {
	rawWidth, present := content["width"]
	if !present {
		return map[string]any{}, true
	}
	// A column's weight goes up to Notion's ratio 1, which Notion reads back
	// after a drag (seen 2026-11-24), though its API takes none so wide.
	width, ok := rawWidth.(float64)
	if !ok || math.IsNaN(width) || math.IsInf(width, 0) || width <= 0 || width > 100 {
		return nil, false
	}
	return map[string]any{"width_ratio": width / 100}, true
}
