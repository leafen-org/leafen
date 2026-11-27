package app

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	extensionast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Asides in imported Markdown, as Notion's Markdown import reads them (seen
// 2026-11-16 twice, under a Notion test page: Leafen's export form "<aside>⏎💡
// ⏎⏎콜아웃 글⏎⏎</aside>" and Notion's own export "<aside>⏎💡 콜아웃 글⏎⏎안쪽
// 문단⏎⏎</aside>" came back as one paragraph "💡⏎콜아웃 글" and "💡 콜아웃 글⏎
// 안쪽 문단"): a line "<aside>" with a line "</aside>" further on, the asides
// between counted, holds the blocks between, and they become one paragraph,
// a line for each block's words — a heading's bold, code as code, a table
// row's cells run together, a divider none, a list's items a line each with
// "• " or their number — as Notion's HTML import reads an aside that is no
// callout (importDocumentConverters.ts asideLines), which its Markdown import
// goes through; the tags themselves are gone. No callout comes of it, where
// the editor's Markdown paste makes one, as Notion's does. An "<aside>" no
// line closes, and a "</aside>" closing none, stay words.

type workspaceImportAside struct {
	ast.BaseBlock
}

var workspaceImportAsideKind = ast.NewNodeKind("WorkspaceImportAside")

func (node *workspaceImportAside) Kind() ast.NodeKind { return workspaceImportAsideKind }

func (node *workspaceImportAside) Dump(source []byte, level int) {
	ast.DumpHelper(node, source, level, nil, nil)
}

type workspaceImportAsideParser struct{}

func (workspaceImportAsideParser) Trigger() []byte { return []byte{'<'} }

func (workspaceImportAsideParser) Open(_ ast.Node, reader text.Reader, _ parser.Context) (ast.Node, parser.State) {
	line, segment := reader.PeekLine()
	if !workspaceImportAsideLine(line, "<aside>") || !workspaceImportAsideCloses(reader.Source()[segment.Start:]) {
		return nil, parser.NoChildren
	}
	reader.AdvanceToEOL()
	return &workspaceImportAside{}, parser.HasChildren
}

func (workspaceImportAsideParser) Continue(node ast.Node, reader text.Reader, context parser.Context) parser.State {
	line, _ := reader.PeekLine()
	// A "</aside>" closes the innermost aside open, which reads it first.
	if !util.IsBlank(line) && workspaceImportAsideLine(line, "</aside>") && !workspaceImportAsideHoldsAside(node, context) {
		reader.AdvanceToEOL()
		return parser.Close
	}
	return parser.Continue | parser.HasChildren
}

func (workspaceImportAsideParser) Close(ast.Node, text.Reader, parser.Context) {}

func (workspaceImportAsideParser) CanInterruptParagraph() bool { return true }

func (workspaceImportAsideParser) CanAcceptIndentedLine() bool { return false }

// workspaceImportAsideLine reports whether a line is the tag alone, in any
// case, with spaces around it.
func workspaceImportAsideLine(line []byte, tag string) bool {
	return strings.EqualFold(string(bytes.TrimSpace(line)), tag)
}

// workspaceImportAsideCloses reports whether the "<aside>" starting rest has
// its "</aside>" further on, the asides between counted.
func workspaceImportAsideCloses(rest []byte) bool {
	open := 0
	for len(rest) > 0 {
		line := rest
		if end := bytes.IndexByte(rest, '\n'); end >= 0 {
			line, rest = rest[:end], rest[end+1:]
		} else {
			rest = nil
		}
		switch {
		case workspaceImportAsideLine(line, "<aside>"):
			open++
		case workspaceImportAsideLine(line, "</aside>"):
			if open--; open == 0 {
				return true
			}
		}
	}
	return false
}

// workspaceImportAsideHoldsAside reports whether an aside opened inside this
// one is still open.
func workspaceImportAsideHoldsAside(aside ast.Node, context parser.Context) bool {
	inside := false
	for _, block := range context.OpenedBlocks() {
		if block.Node == aside {
			inside = true
		} else if _, nested := block.Node.(*workspaceImportAside); nested && inside {
			return true
		}
	}
	return false
}

// workspaceImportAsideWords is one line of an aside's paragraph.
type workspaceImportAsideWords struct {
	plain string
	spans []graphTextSpan
}

// aside reads an aside into the one paragraph of its lines; one with no
// words makes none.
func (reader *workspaceImportMarkdownReader) aside(aside *workspaceImportAside, indent int) bool {
	lines := reader.asideLines(aside, nil)
	plain, spans, formatted := "", []graphTextSpan{}, false
	for index, line := range lines {
		if index > 0 {
			plain += "\n"
			spans = append(spans, graphTextSpan{Text: "\n"})
		}
		plain += line.plain
		if line.spans == nil && line.plain != "" {
			spans = append(spans, graphTextSpan{Text: line.plain})
		} else if line.spans != nil {
			spans, formatted = append(spans, line.spans...), true
		}
	}
	if strings.TrimSpace(plain) == "" {
		return true
	}
	if !formatted {
		spans = nil
	}
	return reader.list.add(graphBlockTypeParagraph, plain, spans, json.RawMessage(`{}`), indent)
}

// asideLines adds the lines of the blocks under parent.
func (reader *workspaceImportMarkdownReader) asideLines(parent ast.Node, lines []workspaceImportAsideWords) []workspaceImportAsideWords {
	for node := parent.FirstChild(); node != nil; node = node.NextSibling() {
		lines = reader.asideBlockLines(node, lines)
	}
	return lines
}

// asideBlockLines adds one block's lines.
func (reader *workspaceImportMarkdownReader) asideBlockLines(node ast.Node, lines []workspaceImportAsideWords) []workspaceImportAsideWords {
	switch node := node.(type) {
	case *ast.Paragraph, *ast.TextBlock:
		if plain, spans := reader.inline(node); strings.TrimSpace(plain) != "" {
			lines = append(lines, workspaceImportAsideWords{plain, spans})
		}
	case *ast.Heading:
		if plain, spans := reader.inline(node); strings.TrimSpace(plain) != "" {
			if spans == nil {
				spans = []graphTextSpan{{Text: plain}}
			}
			for index := range spans {
				spans[index].Bold = true
			}
			lines = append(lines, workspaceImportAsideWords{plain, spans})
		}
	case *ast.List:
		lines = reader.asideListLines(node, 0, lines)
	case *ast.FencedCodeBlock, *ast.CodeBlock:
		for _, code := range strings.Split(reader.lines(node), "\n") {
			if code = strings.TrimSpace(code); code != "" {
				lines = append(lines, workspaceImportAsideWords{code, []graphTextSpan{{Text: code, Code: true}}})
			}
		}
	case *extensionast.Table:
		for row := node.FirstChild(); row != nil; row = row.NextSibling() {
			cells := ""
			for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
				cells += strings.TrimSpace(reader.plain(cell))
			}
			if cells != "" {
				lines = append(lines, workspaceImportAsideWords{plain: cells})
			}
		}
	case *ast.ThematicBreak:
	case *ast.HTMLBlock:
		for _, raw := range strings.Split(reader.lines(node), "\n") {
			if raw = strings.TrimSpace(raw); raw != "" {
				lines = append(lines, workspaceImportAsideWords{plain: raw})
			}
		}
	default:
		lines = reader.asideLines(node, lines)
	}
	return lines
}

// asideListLines adds a list's items as lines: "• " or the item's number
// before its words, a list in an item after a blank line, four spaces
// further in for each level, "◦ " before its bulleted items' words.
func (reader *workspaceImportMarkdownReader) asideListLines(list *ast.List, depth int, lines []workspaceImportAsideWords) []workspaceImportAsideWords {
	number := 0
	for item := list.FirstChild(); item != nil; item = item.NextSibling() {
		number++
		mark := "• "
		if list.IsOrdered() {
			mark = strconv.Itoa(number) + ". "
		} else if depth > 0 {
			mark = "◦ "
		}
		if depth > 0 && number == 1 && len(lines) > 0 {
			lines = append(lines, workspaceImportAsideWords{})
		}
		words, nested := []workspaceImportAsideWords{}, []*ast.List{}
		for child := item.FirstChild(); child != nil; child = child.NextSibling() {
			if inner, isList := child.(*ast.List); isList {
				nested = append(nested, inner)
			} else {
				words = reader.asideBlockLines(child, words)
			}
		}
		lead := strings.Repeat("    ", depth) + mark
		first := workspaceImportAsideWords{plain: lead}
		if len(words) > 0 {
			first.plain += words[0].plain
			if words[0].spans != nil {
				first.spans = append([]graphTextSpan{{Text: lead}}, words[0].spans...)
			}
			words = words[1:]
		}
		lines = append(append(lines, first), words...)
		for _, inner := range nested {
			lines = reader.asideListLines(inner, depth+1, lines)
		}
	}
	return lines
}

type workspaceImportAsideExtension struct{}

// Extend reads asides before the HTML blocks would take their tags as words.
func (workspaceImportAsideExtension) Extend(markdown goldmark.Markdown) {
	markdown.Parser().AddOptions(parser.WithBlockParsers(util.Prioritized(workspaceImportAsideParser{}, 850)))
}
