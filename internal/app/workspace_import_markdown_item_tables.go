package app

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extensionast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// workspaceImportTables reads GitHub tables as Notion's Markdown import reads
// them (seen 2026-11-13): a table starts a block of its own, its header row
// a paragraph's first line, so table lines right under a paragraph's line —
// in a list item, a quote or the page — are that paragraph's lines, written
// as they are.
type workspaceImportTables struct{}

func (workspaceImportTables) Extend(markdown goldmark.Markdown) {
	markdown.Parser().AddOptions(
		parser.WithParagraphTransformers(util.Prioritized(workspaceImportTableParagraphs{}, 200)),
		parser.WithASTTransformers(util.Prioritized(extension.NewTableASTTransformer(), 0)),
	)
}

type workspaceImportTableParagraphs struct{}

func (workspaceImportTableParagraphs) Transform(paragraph *ast.Paragraph, reader text.Reader, context parser.Context) {
	lines := paragraph.Lines()
	if lines.Len() < 2 {
		return
	}
	if second := lines.At(1); !workspaceImportTableDelimiter(second.Value(reader.Source())) {
		return
	}
	extension.NewTableParagraphTransformer().Transform(paragraph, reader, context)
}

var workspaceImportTableDelimiterCell = regexp.MustCompile(`^\s*:?-+:?\s*$`)

// workspaceImportTableDelimiter is whether a line is a table's delimiter row,
// as GitHub's tables read one: cells of dashes, a colon at either end.
func workspaceImportTableDelimiter(line []byte) bool {
	if width, _ := util.IndentWidth(line, 0); width > 3 {
		return false
	}
	dashes := true
	for _, char := range line {
		dashes = dashes && char == '-'
		if !util.IsSpace(char) && char != '-' && char != '|' && char != ':' {
			return false
		}
	}
	cells := bytes.Split(line, []byte{'|'})
	if util.IsBlank(cells[0]) {
		cells = cells[1:]
	}
	if len(cells) > 0 && util.IsBlank(cells[len(cells)-1]) {
		cells = cells[:len(cells)-1]
	}
	for _, cell := range cells {
		if !workspaceImportTableDelimiterCell.Match(cell) {
			return false
		}
	}
	return !dashes && len(cells) > 0
}

// A table in a list item or a quote, as Notion's Markdown and HTML import
// read one (seen 2026-11-13): the cells of a table opening the item are its
// words, and the cells' words of a table of one column right after the
// item's words join them, a divider for each row staying in the item. Where
// Notion runs cells' words together, each cell's words are a line of their
// own, so no words run into others.

// tableOpening reads the words of a table opening a list item or a quote, a
// cell a line, and gives what follows it; a table of one column gives no
// words, its blocks joining the item's (joinColumnTable).
func (reader *workspaceImportMarkdownReader) tableOpening(table *extensionast.Table) (string, []graphTextSpan, ast.Node) {
	if workspaceImportColumnTable(table) {
		return "", nil, table.NextSibling()
	}
	workspaceImportTableHeaders(table)
	words := workspaceImportWords{}
	for row := table.FirstChild(); row != nil; row = row.NextSibling() {
		for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
			plain, spans := reader.inline(cell)
			if plain == "" {
				continue
			}
			if len(words.spans) > 0 {
				words.line()
			}
			words.add(plain, spans)
		}
	}
	plain, spans := workspaceImportSpansText(words.spans)
	return plain, spans, table.NextSibling()
}

// columnAfterWords gives the table of one column a list item's or a quote's
// words are joined with — the one opening it, else one right after its
// words — and what follows that table.
func (reader *workspaceImportMarkdownReader) columnAfterWords(rest, opening ast.Node) (ast.Node, ast.Node) {
	if opening == nil && workspaceImportColumnTable(rest) {
		return rest.NextSibling(), rest
	}
	return rest, opening
}

// workspaceImportColumnTable is whether a table is one of a single column
// that names no headers, which columnTable reads.
func workspaceImportColumnTable(node ast.Node) bool {
	table, ok := node.(*extensionast.Table)
	if !ok {
		return false
	}
	for row := table.FirstChild(); row != nil; row = row.NextSibling() {
		if row.ChildCount() > 1 {
			return false
		}
	}
	header := table.FirstChild()
	if header == nil || header.FirstChild() == nil {
		return true
	}
	link, ok := header.FirstChild().FirstChild().(*ast.Link)
	return !ok || link.HasChildren() || !strings.HasPrefix(string(link.Destination), workspaceImportTableMark)
}

// joinColumnTable joins the words of a table of one column with those of
// the item or quote whose block is the last before it at indent, a line for
// each cell, and puts a divider for each row inside it.
func (reader *workspaceImportMarkdownReader) joinColumnTable(table ast.Node, indent int) bool {
	block := &reader.list.blocks[len(reader.list.blocks)-1]
	words := workspaceImportWords{}
	words.add(block.Text, block.Spans)
	rows := 0
	for row := table.FirstChild(); row != nil; row = row.NextSibling() {
		rows++
		if row.FirstChild() == nil {
			continue
		}
		if plain, spans := reader.inline(row.FirstChild()); strings.TrimSpace(plain) != "" {
			if len(words.spans) > 0 {
				words.line()
			}
			words.add(plain, spans)
		}
	}
	block.Text, block.Spans = workspaceImportSpansText(words.spans)
	for range rows {
		if !reader.list.add(graphBlockTypeDivider, "", nil, json.RawMessage(`{}`), indent+1) {
			return false
		}
	}
	return true
}
