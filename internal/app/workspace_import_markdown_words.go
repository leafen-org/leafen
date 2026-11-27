package app

import (
	"strings"

	"github.com/yuin/goldmark/ast"
)

// A list item's or a quote's words are the words of the first block inside
// it, whichever block Markdown reads there, as Notion's Markdown import reads
// them (seen 2026-11-13: "- # a", "- > a" and "- ```a```" are an item "a",
// "> # a" a quote of a bold "a", "> - a" a quote "• a"):
//
//   - a paragraph's words, a heading's (bold in a quote), and a paragraph's
//     on the line right under that heading, a line below them;
//   - a quote's, its blocks' words a line each;
//   - a code block's, as code;
//   - a divider's, none;
//   - in a quote, a list's, an item a line as Notion writes it: "• " before
//     a bulleted item's words (and its task box as written, a box alone
//     "[ ]" as a to-do's words keep it, taskBox), "◦ " four
//     spaces further in for each level below after a blank line, a numbered
//     item's words alone.
//
// A list at the start of a list item has no words and stays inside it, as
// does any other block.

// workspaceImportWords gathers a block's words from the blocks read into it.
type workspaceImportWords struct{ spans []graphTextSpan }

func (words *workspaceImportWords) add(plain string, spans []graphTextSpan) {
	if spans == nil && plain != "" {
		spans = []graphTextSpan{{Text: plain}}
	}
	words.spans = appendGraphRich(words.spans, spans)
}

func (words *workspaceImportWords) line() { words.add("\n", nil) }

// workspaceImportSpansText is the plain text of spans, and the spans when
// they carry formatting or an equation.
func workspaceImportSpansText(spans []graphTextSpan) (string, []graphTextSpan) {
	plain := strings.Builder{}
	styled := false
	for _, span := range spans {
		plain.WriteString(span.Text)
		styled = styled || span.Bold || span.Italic || span.Strikethrough || span.Underline || span.Code || span.Link != "" ||
			span.Mention != "" || span.Color != "" || span.Background != "" || span.Equation != ""
	}
	if !styled {
		return plain.String(), nil
	}
	return plain.String(), spans
}

// openingWords reads the words of the first block of a list item or, when
// quoted, a quote, and gives the block that follows them; a block without
// words is left for the item or quote to hold.
func (reader *workspaceImportMarkdownReader) openingWords(first ast.Node, quoted bool) (string, []graphTextSpan, ast.Node) {
	words := workspaceImportWords{}
	if first == nil || !reader.words(&words, first, quoted) {
		return "", nil, first
	}
	rest := first.NextSibling()
	if heading, ok := first.(*ast.Heading); ok {
		if paragraph, ok := rest.(*ast.Paragraph); ok && reader.nextLine(heading, paragraph) {
			below := workspaceImportWords{}
			reader.words(&below, paragraph, quoted)
			words.line()
			words.add("", below.spans)
			rest = paragraph.NextSibling()
		}
	}
	plain, spans := workspaceImportSpansText(words.spans)
	return plain, spans, rest
}

// words adds a block's words, and says whether it has words to give.
func (reader *workspaceImportMarkdownReader) words(words *workspaceImportWords, node ast.Node, quoted bool) bool {
	switch node := node.(type) {
	case *ast.Paragraph, *ast.TextBlock:
		words.add(reader.inline(node))
	case *ast.Heading:
		plain, spans := reader.inline(node)
		if quoted {
			if spans == nil && plain != "" {
				spans = []graphTextSpan{{Text: plain}}
			}
			for index := range spans {
				spans[index].Bold = spans[index].Mention == "" || spans[index].Bold
			}
		}
		words.add(plain, spans)
	case *ast.Blockquote:
		inner := workspaceImportWords{}
		for child := node.FirstChild(); child != nil; child = child.NextSibling() {
			part := workspaceImportWords{}
			if !reader.words(&part, child, quoted) {
				return false
			}
			if len(part.spans) > 0 && len(inner.spans) > 0 {
				inner.line()
			}
			inner.add("", part.spans)
		}
		words.add("", inner.spans)
	case *ast.FencedCodeBlock, *ast.CodeBlock:
		if code := reader.lines(node); code != "" {
			words.add(code, []graphTextSpan{{Text: code, Code: true}})
		}
	case *ast.ThematicBreak:
	case *ast.List:
		if !quoted {
			return false
		}
		return reader.quotedList(words, node, 0)
	default:
		return false
	}
	return true
}

// quotedList adds a list's items as lines of a quote's words.
func (reader *workspaceImportMarkdownReader) quotedList(words *workspaceImportWords, list *ast.List, depth int) bool {
	for item := list.FirstChild(); item != nil; item = item.NextSibling() {
		if len(words.spans) > 0 {
			if depth > 0 && item == list.FirstChild() {
				words.line()
			}
			words.line()
		}
		lead := strings.Repeat("    ", depth)
		if !list.IsOrdered() && depth == 0 {
			lead += "• "
		} else if !list.IsOrdered() {
			lead += "◦ "
		}
		words.add(lead, nil)
		opened := false
		for child := item.FirstChild(); child != nil; child = child.NextSibling() {
			if inner, ok := child.(*ast.List); ok {
				if !reader.quotedList(words, inner, depth+1) {
					return false
				}
				continue
			}
			box := workspaceImportTaskBox{}
			switch child.(type) {
			case *ast.Paragraph, *ast.TextBlock:
				box = reader.taskBox(child)
			}
			if box.todo && box.lead == "" && box.checked {
				words.add("[x] ", nil)
			} else if box.todo && box.lead == "" {
				words.add("[ ] ", nil)
			}
			if opened {
				words.line()
			}
			if box != (workspaceImportTaskBox{}) {
				words.add(box.words(reader.inline(child)))
			} else if !reader.words(words, child, true) {
				return false
			}
			opened = true
		}
	}
	return true
}

// nextLine says whether a paragraph starts on the line right under a block.
func (reader *workspaceImportMarkdownReader) nextLine(block, paragraph ast.Node) bool {
	above, below := block.Lines(), paragraph.Lines()
	if above.Len() == 0 || below.Len() == 0 {
		return false
	}
	end, start := above.At(above.Len()-1).Stop, below.At(0).Start
	return end <= start && strings.Count(string(reader.source[end:start]), "\n") == 1
}
