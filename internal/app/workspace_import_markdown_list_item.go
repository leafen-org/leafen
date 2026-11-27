package app

import (
	"encoding/json"
	"strings"
	"unicode"

	"github.com/yuin/goldmark/ast"
	extensionast "github.com/yuin/goldmark/extension/ast"
)

// listItem is a list item's block: its first block's words are its text
// (openingWords), a box opening them a to-do (taskBox), a toggle's mark a toggle,
// closed as any toggle written from outside (closeNewToggle), a color mark
// its color, and what follows is inside it. An item holding a heading alone
// is that heading, as Notion's Markdown import reads one (seen 2026-11-13).
// An item a paragraph's mark opens is that paragraph, what follows inside it,
// and one a column list's or a column's mark opens that block (layout).
// A later paragraph of the item holding a math block is no paragraph of its
// own (itemLaterParts).
func (reader *workspaceImportMarkdownReader) listItem(item ast.Node, ordered bool, indent int) bool {
	kind, content := graphBlockTypeBulletedListItem, json.RawMessage(`{}`)
	if ordered {
		kind = graphBlockTypeNumberedListItem
	}
	rest := item.FirstChild()
	plain, spans, lifted := "", []graphTextSpan(nil), []string(nil)
	var column ast.Node
	switch first := rest.(type) {
	case *extensionast.Table:
		if plain, spans, rest = reader.tableOpening(first); workspaceImportColumnTable(first) {
			column = first
		}
	case *ast.Heading:
		if first.NextSibling() == nil {
			return reader.block(first, indent)
		}
		plain, spans, rest = reader.openingWords(first, false)
	case *ast.List:
		// An item opening with a list of one item holding a heading alone
		// has the heading's words ("- - # a" and "- 1. # a" are an item "a").
		if inner := first.FirstChild(); first.ChildCount() == 1 && inner.ChildCount() == 1 {
			if heading, ok := inner.FirstChild().(*ast.Heading); ok {
				plain, spans = reader.inline(heading)
				rest = first.NextSibling()
			}
		}
	case *ast.Paragraph, *ast.TextBlock:
		if address, callout := takeWorkspaceImportMark(first, workspaceImportCalloutMark); callout && !ordered {
			return reader.callout(first, address, indent)
		}
		if kind, content, laid := workspaceImportLayoutKind(first); laid && !ordered {
			return reader.layout(first, kind, content, indent)
		}
		toggle, marked := workspaceImportToggleKind(first)
		box := workspaceImportTaskBox{}
		if marked && !ordered {
			kind, content = toggle, json.RawMessage(`{"collapsed":true}`)
		} else if _, held := takeWorkspaceImportMark(first, workspaceImportParagraphMark); held && !ordered {
			kind, marked = graphBlockTypeParagraph, true
		} else if box = reader.taskBox(first); box.todo {
			kind = graphBlockTypeToDo
			content, _ = json.Marshal(map[string]bool{"checked": box.checked})
		}
		content = withNotionMarkdownBlockColor(content, reader.takeColorMark(first))
		// A to-do's words keep their math blocks as words, as Notion's.
		read := func() { plain, spans = box.words(reader.inline(first)) }
		if box.todo {
			read()
		} else {
			lifted = reader.openingWithMath(first, read)
		}
		if marked {
			plain, spans = workspaceImportTrimMarkSpace(plain, spans)
		}
		rest = first.NextSibling()
	default:
		plain, spans, rest = reader.openingWords(first, false)
	}
	later, parts := reader.itemLaterParts(rest)
	plain, spans = workspaceImportJoinedWords(plain, spans, later)
	if !reader.list.add(kind, plain, spans, content, indent) || !reader.liftedEquations(lifted, indent+1) {
		return false
	}
	if rest, column = reader.columnAfterWords(rest, column); column != nil && !reader.joinColumnTable(column, indent) {
		return false
	}
	for ; rest != nil; rest = rest.NextSibling() {
		if equations, dissolved := parts[rest]; dissolved {
			if !reader.liftedEquations(equations, indent+1) {
				return false
			}
		} else if !reader.block(rest, indent+1) {
			return false
		}
	}
	return true
}

// itemLaterParts reads the paragraphs after a list item's first block that
// hold a math block, as Notion's Markdown import reads them (seen 2026-11-15:
// a bulleted, numbered and to-do item alike): such a paragraph's words, its
// math blocks taken out wherever they stand in it, go on the item's own
// words with nothing put between ("- 라⏎⏎  앞 $$m$$ 뒤" is an item "라앞  뒤";
// "- 가⏎⏎  $$m$$ 둘째" an item "가 둘째"), and its math blocks stand inside
// the item where the paragraph stood, among its other blocks. A paragraph
// without one stays a paragraph inside the item, and a math block in
// emphasis or a link is its TeX as words, as anywhere.
func (reader *workspaceImportMarkdownReader) itemLaterParts(rest ast.Node) ([]graphTextSpan, map[ast.Node][]string) {
	words, parts := []graphTextSpan(nil), map[ast.Node][]string{}
	for node := rest; node != nil; node = node.NextSibling() {
		if !workspaceImportHoldsMathBlock(node) {
			continue
		}
		lifted := []string{}
		reader.liftFrom, reader.lifted, reader.liftAll = node, &lifted, true
		spans := workspaceImportInlineParser{}
		reader.inlineChildren(&spans, node, workspaceImportInlineStyle{})
		spans.flush()
		reader.liftFrom, reader.lifted, reader.liftAll = nil, nil, false
		words = append(words, workspaceImportTrimEndSpans(spans.spans)...)
		parts[node] = lifted
	}
	return words, parts
}

// workspaceImportHoldsMathBlock says whether a paragraph holds a math block
// among its own words.
func workspaceImportHoldsMathBlock(node ast.Node) bool {
	switch node.(type) {
	case *ast.Paragraph, *ast.TextBlock:
	default:
		return false
	}
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		if math, ok := child.(*workspaceImportMath); ok && math.Display {
			return true
		}
	}
	return false
}

// workspaceImportJoinedWords puts words after a block's words.
func workspaceImportJoinedWords(plain string, spans, more []graphTextSpan) (string, []graphTextSpan) {
	if len(more) == 0 {
		return plain, spans
	}
	if spans == nil && plain != "" {
		spans = []graphTextSpan{{Text: plain}}
	}
	return workspaceImportSpansText(append(spans, more...))
}

// workspaceImportTrimEndSpans drops the spaces at the end of words.
func workspaceImportTrimEndSpans(spans []graphTextSpan) []graphTextSpan {
	for len(spans) > 0 && spans[len(spans)-1].Mention == "" && spans[len(spans)-1].Equation == "" {
		last := &spans[len(spans)-1]
		if last.Text = strings.TrimRightFunc(last.Text, unicode.IsSpace); last.Text != "" {
			break
		}
		spans = spans[:len(spans)-1]
	}
	return spans
}
