package app

import (
	"regexp"
	"sort"
	"strings"
)

// A "v2" write refuses a block whose inline tags don't close, as Notion's API
// does (seen 2026-11-10, each block alone and in a page): an opening <b>,
// <i>, <s>, <u>, <em>, <strong>, <code>, <span>, <a>, <mention>, <date> or
// <link-preview> its block's text doesn't close is never closed
// (notionMarkdownUnclosedInline), and one a closing tag of a tag around it shuts too; a closing tag
// nothing opened is text, an opening one the block ends with is text, and an
// opening one inside another of its kind leaves that one text. A <b, <i, <s,
// <u, <em or <a, or one of their closing tags, the block ends in before its >
// is an unfinished opening dropped (notionMarkdownTrailingOpener), said
// first — a block tag's too, as
// "</p" and "<h2" (seen 2026-11-14: every block tag Notion's "v2" reads and
// <empty-block>, <unknown>, <mail>, <meeting-notes>, <custom-block> and
// <folder>; not <code>, <span>, <strong>, <summary>, a mention's, a date's
// or HTML's), and the start of any of their names, "<tabl"
// (notionMarkdownVersion2TrailingOpener), before the block tags left open.
// Code and an escaped tag are text.
//
// Notion says them a level at a time: the tags no other holds, block by
// block, then the tags those hold, and so on. A to-do's text is read twice,
// its outermost tags said again a level further in, and a tag that opens in
// a link's text, [like <b>this](…), is said twice.
//
// A list tag — <ul>, <ol>, <li> or <todo> — standing in text is read among
// them (seen 2026-11-10): a list tag standing inside text
// (notionMarkdownListTagInline), said where it opens; an opening tag inside it is text, so "x <ul> <b>y"
// says nothing of the <b>; and one a closing tag of a tag around it shuts,
// or that the block's end leaves open, is never closed — but
// in the last block, where Notion closes the block tags left open
// (notionMarkdownRepair.trailing).

var (
	notionMarkdownVersion2InlineTag   = regexp.MustCompile(`<(/?)(b|i|s|u|em|strong|code|span|a|mention|date|link-preview)((?:[ \t][^<>]*)?)>`)
	notionMarkdownVersion2BacktickRun = regexp.MustCompile("`+[^`]*`+")
	notionMarkdownVersion2TodoText    = regexp.MustCompile(`^[ \t>]*(?:(?:[-*+]|\d{1,9}[.)])[ \t]+[ \t>]*)*[-*+][ \t]+\[[ xX]\](?:[ \t]|$)`)
	notionMarkdownVersion2LinkText    = regexp.MustCompile(`\[[^\[\]]*\]\([^()]*\)`)
)

// notionMarkdownUnclosed is one thing Notion says of a block's inline tags:
// the level it is said at and where in the block its tag opens.
type notionMarkdownUnclosed struct {
	level, at int
	words     string
}

// notionMarkdownVersion2Said is what Notion says of blocks' texts whose inline
// tags don't close, in its order; none when they do.
func notionMarkdownVersion2Said(texts []string) []string {
	return notionMarkdownVersion2SaidOf(texts, len(texts)-1)
}

// notionMarkdownVersion2SaidOf is notionMarkdownVersion2Said of blocks one
// of which may be the Markdown's last, or -1.
func notionMarkdownVersion2SaidOf(texts []string, last int) []string {
	type said struct {
		notionMarkdownUnclosed
		block int
	}
	all := []said{}
	for block, text := range texts {
		for _, one := range notionMarkdownVersion2Unclosed(text, block == last) {
			all = append(all, said{one, block})
		}
	}
	sort.SliceStable(all, func(left, right int) bool {
		a, b := all[left], all[right]
		if a.level != b.level {
			return a.level < b.level
		}
		if a.block != b.block {
			return a.block < b.block
		}
		return a.at < b.at
	})
	words := make([]string, 0, len(all))
	for _, one := range all {
		// A tag the text ends in before its > is said apart
		// (notionMarkdownVersion2TrailingOpeners).
		if !strings.HasPrefix(one.words, notionMarkdownTrailingOpenerSaid) {
			words = append(words, one.words)
		}
	}
	return words
}

// notionMarkdownVersion2TrailingOpeners is what Notion says of the tags a
// block's text ends in before their > (notionMarkdownVersion2Unclosed), by
// the "v2" write's lines read and the blocks' texts by their lines: those the
// blocks no tag holds end in, said first, and those in a tag's blocks, said
// after the block tags left open. Notion reads such a tag on to the next <
// or >, so one is said only where nothing but text follows it to the end of
// the blocks it stands among — the Markdown's end or the closing tag of the
// tag holding it (seen 2026-11-14: "a</b⏎⏎b</i" says only the </i>,
// "a</p⏎⏎<callout>⏎b</h2⏎</callout>" only the </h2>, "<callout>⏎a</p⏎
// </callout>⏎⏎b</h2" the </h2> and then the </p>). A text on more lines
// than one ends on the line ends names (notionMarkdownVersion2QuoteTexts).
func notionMarkdownVersion2TrailingOpeners(lines []string, texts map[int]string, ends map[int]int) ([]string, []string) {
	free, held := []string{}, []string{}
	depth := 0
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if match := notionMarkdownVersion2ContainerLine.FindStringSubmatch(trimmed); match != nil &&
			notionMarkdownVersion2Tags[match[2]] && !notionMarkdownVersion2InlineTags[match[2]] {
			if match[1] == "" {
				depth++
			} else if depth > 0 {
				depth--
			}
			continue
		}
		text, ok := texts[index]
		if !ok || !strings.Contains(text, "<") {
			continue
		}
		text = notionMarkdownVersion2BacktickRun.ReplaceAllStringFunc(text, func(code string) string {
			return strings.Repeat("x", len(code))
		})
		found := notionMarkdownVersion2TrailingOpener(text)
		if found == nil {
			continue
		}
		last := index
		if end, ok := ends[index]; ok {
			last = end
		}
		stands, closed := notionMarkdownVersion2OpenerEnds(lines[last+1:])
		if !stands {
			continue
		}
		words := notionMarkdownTrailingOpener(text[found[2]:found[3]] + text[found[4]:found[5]])
		// One the Markdown's end ends, the tags holding it left open, is said
		// first too (seen 2026-11-15: "x⏎<li>b</li" and "<callout>⏎<li>b</li"
		// say it before the tags left open).
		if depth == 0 || !closed {
			free = append(free, words)
		} else {
			held = append(held, words)
		}
	}
	return free, held
}

var notionMarkdownVersion2ContainerLine = regexp.MustCompile(`^<(/?)([a-z][a-z0-9-]*)(?:[ \t][^<>]*[^/<>])?>$`)

// notionMarkdownVersion2OpenerEnds tells lines after a tag a block's text
// ends in before its > that hold no < or > before the end of the blocks it
// stands among: the end, or a closing tag's line (closed).
func notionMarkdownVersion2OpenerEnds(after []string) (ends, closed bool) {
	for _, line := range after {
		trimmed := strings.TrimSpace(line)
		if notionMarkdownVersion2ClosingLine.MatchString(trimmed) {
			return true, true
		}
		if strings.ContainsAny(trimmed, "<>") {
			return false, false
		}
	}
	return true, false
}

// notionMarkdownVersion2Unclosed is what Notion says of a block's text whose
// inline tags don't close, or that holds a list tag; none else. last is
// whether it is the Markdown's last block.
func notionMarkdownVersion2Unclosed(text string, last bool) []notionMarkdownUnclosed {
	if !strings.Contains(text, "<") {
		return nil
	}
	// Code between backticks is its text as written: neither tags nor space;
	// so is an equation (notionMarkdownVersion2EquationRanges).
	text = notionMarkdownVersion2BacktickRun.ReplaceAllStringFunc(notionMarkdownEquationsMasked(text), func(code string) string {
		return strings.Repeat("x", len(code))
	})
	todo := false
	if marker := notionMarkdownVersion2TodoText.FindString(text); marker != "" {
		todo, text = true, strings.Repeat(" ", len(marker))+text[len(marker):]
	}
	said := []notionMarkdownUnclosed{}
	if found := notionMarkdownVersion2TrailingOpener(text); found != nil {
		said = append(said, notionMarkdownUnclosed{at: found[0],
			words: notionMarkdownTrailingOpener(text[found[2]:found[3]] + text[found[4]:found[5]])})
		text = text[:found[0]]
	}
	// element is a tag opened in the text: where it opens and its text
	// starts, the one it stands in (-1 for none), and where its text ends,
	// -1 while open; a list tag is refused.
	type element struct {
		name                   string
		at, start, parent, end int
		missing, list          bool
	}
	elements, open := []element{}, []int{}
	for _, found := range notionMarkdownVersion2TextTags(text) {
		closing, name, list := found[3] > found[2], text[found[4]:found[5]], found[8] >= 0
		// Inside <code> a tag is its text, and so is an opening one inside a list tag.
		inList := false
		for _, index := range open {
			inList = inList || elements[index].list
		}
		inCode := len(open) > 0 && elements[open[len(open)-1]].name == "code"
		if inCode && name != "code" || inList && !closing {
			continue
		}
		// A backslash escapes no </code> inside code, whose text is as
		// written (seen 2026-11-14: "<code>a\</code>" is the code "a\").
		if found[10] == 1 && !(inCode && closing) {
			continue
		}
		at := -1
		for index := len(open) - 1; index >= 0; index-- {
			if elements[open[index]].name == name {
				at = index
				break
			}
		}
		switch {
		case !closing && !list && strings.TrimSpace(text[found[1]:]) == "":
		case list && !closing && found[9] <= found[8] && strings.TrimSpace(text[found[1]:]) == "" && !last:
			// One a block before the last ends with is text, the closing tag
			// after it closing it or none (seen 2026-11-14: "x <ul>⏎<li>a</li>⏎
			// </ul>" says the bare <li> alone, "x <ul>⏎⏎</ul>" is two
			// paragraphs, "x <ul>⏎⏎y" says only the <ul> left open).
		case list && (found[9] > found[8] || !closing && strings.TrimSpace(text[found[1]:]) == ""):
			// One closed in itself, or that the last block ends with, is
			// refused and holds nothing.
			parent := -1
			if len(open) > 0 {
				parent = open[len(open)-1]
			}
			elements = append(elements, element{name: name, at: found[0], start: found[1], parent: parent, end: found[1], list: true})
		case !closing:
			if at >= 0 {
				// The one already open is text; this one opens instead.
				elements[open[at]].end = -2
				open = append(open[:at], open[at+1:]...)
			}
			parent := -1
			if len(open) > 0 {
				parent = open[len(open)-1]
			}
			elements = append(elements, element{name: name, at: found[0], start: found[1], parent: parent, end: -1, list: list})
			open = append(open, len(elements)-1)
		case at >= 0:
			for index := len(open) - 1; index > at; index-- {
				elements[open[index]].missing, elements[open[index]].end = true, found[0]
			}
			elements[open[at]].end = found[0]
			open = open[:at]
		}
	}
	for _, index := range open {
		// Notion closes the last block's list tags at the Markdown's end.
		elements[index].missing, elements[index].end = !elements[index].list || !last, len(text)
	}
	for _, element := range elements {
		if !element.missing && !element.list {
			continue
		}
		level := 1
		for parent := element.parent; parent >= 0; parent = elements[parent].parent {
			level++
		}
		// A to-do's outermost tags are said again a level further in.
		levels := []int{level}
		if todo {
			levels = []int{level + 1}
			if level == 1 {
				levels = []int{1, 2}
			}
		}
		// Its tag opening in a link's text, of the text it stands in, is said twice.
		start, end := 0, len(text)
		if element.parent >= 0 {
			start, end = elements[element.parent].start, max(elements[element.parent].end, elements[element.parent].start)
		}
		times := 1
		for _, link := range notionMarkdownVersion2LinkText.FindAllStringIndex(text[start:end], -1) {
			if start+link[0] < element.at && element.at < start+strings.Index(text[start+link[0]:start+link[1]], "](")+link[0] {
				times = 2
			}
		}
		words := notionMarkdownUnclosedInline(element.name)
		for _, level := range levels {
			for range times {
				if element.missing {
					said = append(said, notionMarkdownUnclosed{level: level, at: element.at, words: words})
				}
			}
		}
		for _, level := range levels {
			if element.list {
				said = append(said, notionMarkdownUnclosed{level: level, at: element.at,
					words: notionMarkdownListTagInline(element.name)})
			}
		}
	}
	// What was said of a tag marked in the text is said where it stands, at
	// the level of the tags it stands in (notion_page_markdown_version2_inline_said.go).
	for _, mark := range notionMarkdownVersion2InlineSaidAt(text) {
		level := 1
		for _, element := range elements {
			if !element.list && element.end >= 0 && element.start <= mark[0] && mark[0] < element.end {
				level++
			}
		}
		said = append(said, notionMarkdownUnclosed{level: level, at: mark[0], words: text[mark[0]:mark[1]]})
	}
	return said
}

// notionMarkdownVersion2TextTags are the inline tags of a block's text, and the
// list tags standing in it (notionMarkdownListTagMark), in order: each the
// submatch indexes of whether it closes, its name and its attributes, then,
// for a list tag, whether it is closed in itself (-1 for an inline tag), and
// 1 for a </code> a backslash escapes, which closes code all the same.
func notionMarkdownVersion2TextTags(text string) [][]int {
	tags := [][]int{}
	for _, found := range notionMarkdownVersion2InlineTag.FindAllStringSubmatchIndex(text, -1) {
		escaped := notionMarkdownEscapedAt(text, found[0])
		if escaped && text[found[0]:found[1]] != "</code>" || strings.HasSuffix(text[found[6]:found[7]], "/") {
			continue
		}
		mark := 0
		if escaped {
			mark = 1
		}
		tags = append(tags, append(found[:8:8], -1, -1, mark))
	}
	// The mark is no character, which a pattern can't name: each is found by hand.
	for at := 0; at < len(text); {
		mark := strings.Index(text[at:], notionMarkdownListTagMark+"<")
		end := strings.IndexByte(text[at+max(mark, 0):], '>')
		if mark < 0 || end < 0 {
			break
		}
		start, end := at+mark, at+mark+end
		at = end + 1
		name := start + len(notionMarkdownListTagMark) + 1
		closing := name
		if text[name] == '/' {
			name++
		}
		names := name + len(text[name:end]) - len(strings.TrimLeft(text[name:end], "abcdefghijklmnopqrstuvwxyz"))
		// A block's tag so stands in the text and is no list tag of it
		// (notionMarkdownVersion2CoveredTags).
		if !notionMarkdownVersion2TextGroupList[text[name:names]] {
			continue
		}
		self := end
		if text[end-1] == '/' {
			self--
		}
		tags = append(tags, []int{start, end + 1, closing, name, name, names, names, self, self, end, 0})
	}
	sort.SliceStable(tags, func(left, right int) bool { return tags[left][0] < tags[right][0] })
	return tags
}
