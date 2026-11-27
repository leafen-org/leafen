package app

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

const (
	legacySimpleTableBlockType = "simple_table"
)

type nativeLegacySimpleTableContent struct {
	Table        [][]any `json:"table"`
	HeaderRow    *bool   `json:"headerRow"`
	HeaderColumn *bool   `json:"headerColumn"`
}

func nativeStructuredTableContents(
	raw json.RawMessage,
) (graphTableBlockContent, []graphTableRowBlockContent, error) {
	legacy := nativeLegacySimpleTableContent{}
	if json.Unmarshal(raw, &legacy) != nil {
		return graphTableBlockContent{}, nil, errors.New(errorCodeSchemaMigrationHistoryInvalid)
	}
	rowCount := len(legacy.Table)
	if rowCount < 2 {
		rowCount = 2
	}
	width := 2
	for _, row := range legacy.Table {
		if len(row) > width {
			width = len(row)
		}
	}
	if width > maxGraphTableWidth {
		return graphTableBlockContent{}, nil, errors.New(errorCodeSchemaMigrationHistoryInvalid)
	}
	hasColumnHeader := true
	if legacy.HeaderRow != nil {
		hasColumnHeader = *legacy.HeaderRow
	}
	hasRowHeader := false
	if legacy.HeaderColumn != nil {
		hasRowHeader = *legacy.HeaderColumn
	}
	rows := make([]graphTableRowBlockContent, rowCount)
	for rowIndex := 0; rowIndex < rowCount; rowIndex++ {
		cells := make([][]graphTextSpan, width)
		for columnIndex := 0; columnIndex < width; columnIndex++ {
			var value any = ""
			if rowIndex < len(legacy.Table) && columnIndex < len(legacy.Table[rowIndex]) {
				value = legacy.Table[rowIndex][columnIndex]
			}
			text := nativeLegacyTableCellText(value)
			if text != "" {
				cells[columnIndex] = []graphTextSpan{{Text: text}}
			} else {
				cells[columnIndex] = []graphTextSpan{}
			}
		}
		rows[rowIndex] = graphTableRowBlockContent{Cells: cells}
	}
	return graphTableBlockContent{
		TableWidth: width, HasColumnHeader: hasColumnHeader, HasRowHeader: hasRowHeader,
	}, rows, nil
}

func nativeLegacyTableCellText(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case bool:
		if typed {
			return "true"
		}
		return "false"
	case float64:
		return fmt.Sprintf("%v", typed)
	default:
		encoded, _ := json.Marshal(typed)
		return string(encoded)
	}
}

func nativeStructuredTableRowID(tableID string, index int) string {
	return uuid.NewSHA1(
		uuid.NameSpaceOID,
		[]byte(fmt.Sprintf("leafen:native-table-row:v1\x00%s\x00%d", tableID, index)),
	).String()
}
