package app

import "strings"

// A "v1" inline equation holds its words as written, tags and all, the tags
// "v1" reads and those it keeps as text alike (seen 2026-11-14: "$a<b>c</b>$"
// is the equation "a<b>c</b>", "$a<details>b$", "$a<column>b$", "$a<span
// color='red'>b$" and "$a<https://example.com>b$" the equations of those
// words, in a list item, a heading, a quote, a table's cell, a callout and a
// toggle's summary too), so a "v1" write's line is put in Leafen's form
// around its equations (notionMarkdownVersion1Escaped). An equation stands
// in the text of the link, the <span> or the emphasis it starts in, as
// "v1" reads them first: "[a $b](…) <b>c$ d</b>", "<span color=\"red\">a
// $b</span><b>c$ d</b>" and "*a $b* <b>c$ d</b>" hold none, their <b>
// text (workspaceImportInlineParser.parse).

// notionMarkdownVersion1EquationReader finds a "v1" line's inline equations
// as its reading meets them from the left.
type notionMarkdownVersion1EquationReader struct {
	line string
	// ends are where the texts of the links, spans and emphasis open at the
	// byte being read end, and closes where each emphasis' closing marks
	// that are still to come end, by where they start.
	ends   []int
	closes map[int]int
}

// at is where the equation starting at line[index] ends, or index for none;
// a link or a span opening there bounds the equations in its text.
func (reader *notionMarkdownVersion1EquationReader) at(index int) int {
	kept := reader.ends[:0]
	for _, end := range reader.ends {
		if end > index {
			kept = append(kept, end)
		}
	}
	reader.ends = kept
	line := reader.line
	switch {
	case line[index] == '[':
		if closeText := strings.Index(line[index:], "]("); closeText > 0 &&
			notionMarkdownVersion1LinkClose(line[index+closeText+2:]) >= 0 {
			reader.ends = append(reader.ends, index+closeText)
		}
	case strings.HasPrefix(line[index:], "<span"):
		if _, textStart, ok := notionMarkdownVersion1SpanOpening(line, index, workspaceImportInlineStyle{}); ok {
			end := len(line)
			if closing := strings.Index(line[textStart:], "</span>"); closing >= 0 {
				end = textStart + closing
			}
			reader.ends = append(reader.ends, end)
		}
	case line[index] == '$':
		limit := len(line)
		for _, end := range reader.ends {
			limit = min(limit, end)
		}
		if _, end, ok := notionMarkdownVersion1Equation(line[:limit], index); ok {
			return end
		}
	}
	return index
}

// emphasis reads the marks at line[index], one of *, _ and ~: where its text
// starts when they pair, which bounds the equations in it, where they end
// when they close emphasis, or where the marks read as text end.
func (reader *notionMarkdownVersion1EquationReader) emphasis(index int) int {
	if next, closes := reader.closes[index]; closes {
		delete(reader.closes, index)
		return next
	}
	_, textStart, textEnd, next, ok := notionMarkdownVersion1Emphasis(reader.line, index, workspaceImportInlineStyle{})
	if !ok {
		return next
	}
	if reader.closes == nil {
		reader.closes = map[int]int{}
	}
	reader.ends, reader.closes[textEnd] = append(reader.ends, textEnd), next
	return textStart
}
