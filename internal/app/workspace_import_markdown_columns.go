package app

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/yuin/goldmark/ast"
)

// workspaceImportLayoutKind reads the mark opening a list item's first
// paragraph that makes it a column list or a column
// (workspace_import_markdown_marks.go), taking it out: a column's content
// holds its share of the row in percent, as the editor keeps it
// (columnLayoutModel.ts), when its mark names one between 0 and 100.
func workspaceImportLayoutKind(paragraph ast.Node) (string, json.RawMessage, bool) {
	if _, columns := takeWorkspaceImportMark(paragraph, workspaceImportColumnsMark); columns {
		return graphBlockTypeColumnList, json.RawMessage(`{}`), true
	}
	width, column := takeWorkspaceImportMark(paragraph, workspaceImportColumnMark)
	if !column {
		return "", nil, false
	}
	content := json.RawMessage(`{}`)
	if share, err := strconv.ParseFloat(strings.TrimSpace(width), 64); err == nil && share > 0 && share < 100 {
		content, _ = json.Marshal(map[string]float64{"width": share})
	}
	return graphBlockTypeColumn, content, true
}

// layout is a column list's or a column's block, what follows its mark in the
// item inside it: a column list's columns, a column's blocks.
func (reader *workspaceImportMarkdownReader) layout(opening ast.Node, kind string, content json.RawMessage, indent int) bool {
	if !reader.list.add(kind, "", nil, content, indent) {
		return false
	}
	for rest := opening.NextSibling(); rest != nil; rest = rest.NextSibling() {
		if !reader.block(rest, indent+1) {
			return false
		}
	}
	return true
}
