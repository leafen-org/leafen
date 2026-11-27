package app

import (
	"bytes"
	"regexp"
	"strings"
	"unicode"

	"github.com/yuin/goldmark/ast"
	extensionast "github.com/yuin/goldmark/extension/ast"
)

// A list item's words opening with a box are a to-do, as Notion's Markdown
// import reads them (seen 2026-11-13): a box holds spaces or tabs, or an
// "x" for a done one, between brackets, an "X" only before more words. A box
// with a space and more words after it is a to-do of those words ("- [ ] a",
// "1. [x] a", "* [  ] a", "- [X] a"); a box alone, or with words right after
// it, is a to-do whose words keep the box, written "[ ]" or "[x]" ("- [ ]"
// and "- [  ]" are a to-do "[ ]", "- [x]a" a done to-do "[x]a"); an "X" box
// without more words is no box ("- [X]" is a bulleted "[X]"), nor is "[]".
var workspaceImportTaskBoxPattern = regexp.MustCompile(`^\[(?:[ \t]+|x|X)\]`)

// workspaceImportTaskBox is what the box opening a list item's words makes
// of the item: a to-do or not, done or not, the words the box leaves before
// the item's others (lead), and how much of the words' start is the box as
// written there (drop) — none where Markdown read the box as a task box.
type workspaceImportTaskBox struct {
	todo, checked bool
	lead          string
	drop          int
}

// taskBox reads the box opening a list item's first paragraph.
func (reader *workspaceImportMarkdownReader) taskBox(paragraph ast.Node) workspaceImportTaskBox {
	lines := paragraph.Lines()
	if lines.Len() == 0 {
		return workspaceImportTaskBox{}
	}
	_, parsed := paragraph.FirstChild().(*extensionast.TaskCheckBox)
	if _, text := paragraph.FirstChild().(*ast.Text); !parsed && !text {
		return workspaceImportTaskBox{}
	}
	first := lines.At(0)
	line := first.Value(reader.source)
	box := workspaceImportTaskBoxPattern.Find(line)
	if box == nil {
		return workspaceImportTaskBox{}
	}
	rest := line[len(box):]
	spaced := len(bytes.TrimLeft(rest, " \t")) < len(rest) || lines.Len() > 1
	more := len(bytes.TrimSpace(rest)) > 0 || lines.Len() > 1
	checked := box[1] == 'x' || box[1] == 'X'
	drop := len(box)
	if parsed {
		drop = 0
	}
	switch {
	case spaced && more:
		return workspaceImportTaskBox{todo: true, checked: checked, drop: drop}
	case box[1] == 'X' && parsed:
		return workspaceImportTaskBox{lead: "[X]"}
	case box[1] == 'X':
		return workspaceImportTaskBox{}
	}
	lead := "[ ]"
	if checked {
		lead = "[x]"
	}
	return workspaceImportTaskBox{todo: true, checked: checked, lead: lead, drop: drop}
}

// words are the item's words as the box leaves them: without the box as
// written and the spaces after it, and with the box's own words before them.
func (box workspaceImportTaskBox) words(plain string, spans []graphTextSpan) (string, []graphTextSpan) {
	if box.drop > 0 {
		plain, spans = workspaceImportDropWords(plain, spans, box.drop, box.lead == "")
	}
	if box.lead == "" {
		return plain, spans
	}
	if spans == nil {
		return box.lead + plain, nil
	}
	return box.lead + plain, append([]graphTextSpan{{Text: box.lead}}, spans...)
}

// workspaceImportDropWords drops the first bytes of a block's words, and the
// spaces after them when spaced.
func workspaceImportDropWords(plain string, spans []graphTextSpan, count int, spaced bool) (string, []graphTextSpan) {
	plain = plain[min(count, len(plain)):]
	if spaced {
		plain = strings.TrimLeftFunc(plain, unicode.IsSpace)
	}
	if spans == nil {
		return plain, nil
	}
	spans = append([]graphTextSpan(nil), spans...)
	for len(spans) > 0 {
		cut := min(count, len(spans[0].Text))
		spans[0].Text, count = spans[0].Text[cut:], count-cut
		if spaced && count == 0 {
			spans[0].Text = strings.TrimLeftFunc(spans[0].Text, unicode.IsSpace)
		}
		if spans[0].Text != "" {
			break
		}
		spans = spans[1:]
	}
	if len(spans) == 0 {
		return plain, nil
	}
	return plain, spans
}
