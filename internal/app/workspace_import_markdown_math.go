package app

import (
	"encoding/json"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Equations in imported Markdown, as Notion's Markdown import reads them
// (seen 2026-11-13, 59 forms):
//
//   - $$…$$ is a math block, its TeX without the spaces and line breaks at
//     its ends, wherever it stands in a paragraph: the paragraph's words
//     before and after it are paragraphs of their own ("앞 $$c$$ 뒤" is "앞",
//     a math block "c" and "뒤"; "$$⏎x^2 + 1⏎$$" a math block; "$$x$$ $$y$$"
//     two). It may run over a paragraph's lines ("$$ m⏎n $$" is "m⏎n") but
//     not over a blank line, it closes at the first $$ ("$$a$b$$" is "a$b"),
//     and one of nothing is nothing.
//   - $…$ is an inline equation when no space follows its first $ and none
//     stands before its last, which no digit follows: "a$b$c", "$a$b",
//     "($a$)", "$5 and 6$" and "$a⏎b$" hold one; "$5 그리고 $6", "$a$5" and
//     "$ a $" are words. It keeps its words' bold, italic and strikethrough
//     but not their link.
//   - "\$" is a dollar, outside an equation; inside one it is its TeX.
//   - Where a block can't stand — in a heading, a table's cell, emphasis or
//     a link, a to-do's words — a $$…$$ is its TeX as words ("## $$h$$" is a
//     heading "h", "*a $$d$$ b*" an italic "a d b").
//   - In a list item's or a quote's first words, a $$…$$ that opens them is
//     its TeX as words ("1. $$w$$" is an item "w"); one after words is a
//     math block first inside the item, the words around it staying its
//     words ("- 앞 $$a$$ 가운데 $$b$$ 끝" is an item "앞  가운데  끝" holding
//     "a" and "b"). Notion's quote drops the words after such a block ("> 앞
//     $$q$$ 뒤" is a quote "앞"); Leafen keeps them. A list item's later
//     paragraph holding one gives its words to the item's and its math
//     blocks inside the item, opening it or not (itemLaterParts).

type workspaceImportMath struct {
	ast.BaseInline
	TeX     string
	Display bool
}

var workspaceImportMathKind = ast.NewNodeKind("WorkspaceImportMath")

func (node *workspaceImportMath) Kind() ast.NodeKind { return workspaceImportMathKind }

func (node *workspaceImportMath) Dump(source []byte, level int) {
	ast.DumpHelper(node, source, level, map[string]string{"TeX": node.TeX}, nil)
}

type workspaceImportMathParser struct{}

func (workspaceImportMathParser) Trigger() []byte { return []byte{'$'} }

func (workspaceImportMathParser) Parse(_ ast.Node, block text.Reader, _ parser.Context) ast.Node {
	line, _ := block.PeekLine()
	display := len(line) > 1 && line[1] == '$'
	if !display {
		// No space after the first $.
		if next, _ := utf8.DecodeRune(line[1:]); len(line) < 2 || unicode.IsSpace(next) {
			return nil
		}
	}
	startLine, startSegment := block.Position()
	opening := 1
	if display {
		opening = 2
	}
	block.Advance(opening)
	tex := []byte{}
	for {
		line, _ := block.PeekLine()
		if line == nil {
			block.SetPosition(startLine, startSegment)
			return nil
		}
		for index := 0; index < len(line); index++ {
			switch {
			case line[index] == '\\':
				// An escaped character closes nothing; it stays the TeX's.
				index++
			case line[index] != '$':
			case display && index+1 < len(line) && line[index+1] == '$':
				tex = append(tex, line[:index]...)
				block.Advance(index + 2)
				return &workspaceImportMath{TeX: strings.TrimSpace(string(tex)), Display: true}
			case !display && workspaceImportMathCloses(line, index):
				tex = append(tex, line[:index]...)
				block.Advance(index + 1)
				return &workspaceImportMath{TeX: string(tex)}
			}
		}
		tex = append(tex, line...)
		block.AdvanceLine()
	}
}

// workspaceImportMathCloses says whether the $ at line[index] closes an
// inline equation: no space before it (a line's start follows its line
// break) and no digit after it.
func workspaceImportMathCloses(line []byte, index int) bool {
	if index == 0 {
		return false
	}
	if previous, _ := utf8.DecodeLastRune(line[:index]); unicode.IsSpace(previous) {
		return false
	}
	return index+1 >= len(line) || line[index+1] < '0' || line[index+1] > '9'
}

type workspaceImportMathExtension struct{}

func (workspaceImportMathExtension) Extend(markdown goldmark.Markdown) {
	markdown.Parser().AddOptions(parser.WithInlineParsers(util.Prioritized(workspaceImportMathParser{}, 150)))
}

// displayMathParagraph reads a paragraph holding a math block as its parts:
// its words before, between and after them each a paragraph of their own.
// It says whether the paragraph held one, and whether its parts fit.
func (reader *workspaceImportMarkdownReader) displayMathParagraph(paragraph ast.Node, indent int) (bool, bool) {
	parts := []ast.Node{}
	words := paragraph
	var next ast.Node
	for child := paragraph.FirstChild(); child != nil; child = next {
		next = child.NextSibling()
		if math, ok := child.(*workspaceImportMath); ok && math.Display {
			paragraph.RemoveChild(paragraph, child)
			parts = append(parts, words, math)
			words = ast.NewParagraph()
			continue
		}
		if words != paragraph {
			paragraph.RemoveChild(paragraph, child)
			words.AppendChild(words, child)
		}
	}
	if len(parts) == 0 {
		return false, true
	}
	for _, part := range append(parts, words) {
		if math, ok := part.(*workspaceImportMath); ok {
			if !reader.equation(math.TeX, indent) {
				return true, false
			}
		} else if !reader.block(part, indent) {
			return true, false
		}
	}
	return true, true
}

// equation adds a math block, unless it holds nothing.
func (reader *workspaceImportMarkdownReader) equation(tex string, indent int) bool {
	if tex == "" {
		return true
	}
	content, _ := json.Marshal(map[string]string{"expression": tex})
	return reader.list.add(graphBlockTypeEquation, "", nil, content, indent)
}

// openingWithMath reads an item's or a quote's first words, taking out the
// math blocks that follow words in a paragraph opening it (lifted), to
// stand first inside it.
func (reader *workspaceImportMarkdownReader) openingWithMath(opening ast.Node, read func()) []string {
	switch opening.(type) {
	case *ast.Paragraph, *ast.TextBlock:
	default:
		read()
		return nil
	}
	lifted := []string{}
	reader.liftFrom, reader.lifted = opening, &lifted
	read()
	reader.liftFrom, reader.lifted = nil, nil
	return lifted
}

// liftedEquations adds the math blocks taken out of an item's words inside it.
func (reader *workspaceImportMarkdownReader) liftedEquations(lifted []string, indent int) bool {
	for _, tex := range lifted {
		if !reader.equation(tex, indent) {
			return false
		}
	}
	return true
}

// inlineMath puts an equation into a block's words: an inline one as an
// equation piece, a math block lifted out of an item's words after them, or
// otherwise as its TeX in words.
func (reader *workspaceImportMarkdownReader) inlineMath(spans *workspaceImportInlineParser, parent ast.Node, math *workspaceImportMath, style workspaceImportInlineStyle) {
	switch {
	case !math.Display:
		spans.add(graphTextSpan{
			Text: math.TeX, Equation: math.TeX, Bold: style.bold, Italic: style.italic, Strikethrough: style.strikethrough,
			Underline: style.underline, Color: style.color, Background: style.background,
		})
	case parent == reader.liftFrom && reader.lifted != nil && (reader.liftAll || workspaceImportInlineHasWords(spans)):
		*reader.lifted = append(*reader.lifted, math.TeX)
	default:
		spans.emit(math.TeX, style)
	}
}

// workspaceImportInlineHasWords says whether words other than spaces were
// read into a block's spans.
func workspaceImportInlineHasWords(spans *workspaceImportInlineParser) bool {
	if strings.TrimSpace(spans.pending.String()) != "" {
		return true
	}
	for _, span := range spans.spans {
		if strings.TrimSpace(span.Text) != "" || span.Mention != "" || span.Equation != "" {
			return true
		}
	}
	return false
}
