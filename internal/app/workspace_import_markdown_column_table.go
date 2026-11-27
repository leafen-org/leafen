package app

import (
	"encoding/json"
	"strings"

	"github.com/yuin/goldmark/ast"
)

// columnTable reads a Markdown table of one column as Notion's Markdown and
// HTML import reads one (seen 2026-11-13, a cell of a header alone too):
// each row is its cell's blocks and a divider after them, an empty cell its
// divider alone. A cell's words between its formatted parts are a paragraph,
// each formatted part — bold, italic, strikethrough, a link — a paragraph of
// its own, inline code a code block, and a line break ends a paragraph
// ("| a **b** `c` |" is the paragraphs "a" and "b", the code "c" and a
// divider). A converted document's table that names its headers stays a
// table (workspaceImportTableHeaders).
func (reader *workspaceImportMarkdownReader) columnTable(rows [][]ast.Node, indent int) bool {
	for _, row := range rows {
		if len(row) > 0 && !reader.columnCell(row[0], indent) {
			return false
		}
		if !reader.list.add(graphBlockTypeDivider, "", nil, json.RawMessage(`{}`), indent) {
			return false
		}
	}
	return true
}

// columnCell is a cell's blocks as columnTable reads them.
func (reader *workspaceImportMarkdownReader) columnCell(cell ast.Node, indent int) bool {
	words := []ast.Node{}
	flush := func() bool {
		if len(words) == 0 {
			return true
		}
		paragraph := ast.NewParagraph()
		for _, node := range words {
			paragraph.AppendChild(paragraph, node)
		}
		words = words[:0]
		plain, spans := reader.inline(paragraph)
		if strings.TrimSpace(plain) == "" {
			return true
		}
		return reader.list.add(graphBlockTypeParagraph, plain, spans, json.RawMessage(`{}`), indent)
	}
	for node := cell.FirstChild(); node != nil; {
		next := node.NextSibling()
		switch part := node.(type) {
		case *ast.Text, *ast.String:
			words = append(words, node)
		case *ast.RawHTML:
			if !reader.lineBreak(part) {
				words = append(words, node)
			} else if !flush() {
				return false
			}
		case *ast.CodeSpan:
			if !flush() {
				return false
			}
			code := reader.plain(part)
			content, _ := json.Marshal(map[string]string{"language": ""})
			if strings.TrimSpace(code) != "" && !reader.list.add(graphBlockTypeCode, code, nil, content, indent) {
				return false
			}
		default:
			if !flush() {
				return false
			}
			words = append(words, node)
			if !flush() {
				return false
			}
		}
		node = next
	}
	return flush()
}

// lineBreak is whether inline HTML is a <br> alone.
func (reader *workspaceImportMarkdownReader) lineBreak(node *ast.RawHTML) bool {
	raw := strings.Builder{}
	for index := range node.Segments.Len() {
		segment := node.Segments.At(index)
		raw.Write(segment.Value(reader.source))
	}
	written := strings.TrimSpace(raw.String())
	return markdownLineBreakPattern.MatchString(written) && strings.TrimSpace(markdownLineBreakPattern.ReplaceAllString(written, "")) == ""
}
