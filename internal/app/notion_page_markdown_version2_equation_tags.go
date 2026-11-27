package app

import (
	"regexp"
	"strings"
)

// Tags in an inline equation, as Notion's "v2" and its MCP read them (seen
// 2026-11-14): an equation holds its tags as written — "$a<B>b$",
// "$a<b/>b$", "$<a href=\"…\">x</a>$", "$a<span>c$" and "$a<x>b$" are
// equations of those words, its marks' tags marking nothing, a tag in it
// left open refused for nothing and its attributes read for nothing — while
// what Notion puts right in HTML it puts right there too: <mark> and <kbd>
// become <span color="yellow_bg"> and <code>, the HTML it takes out goes
// (notionMarkdownRemovedSpaces), and v1's tags are written as their "v2" ones,
// "<mention-page url=\"…\"/>" "<mention url=\"…\"/>", "<mention-date …>"
// "<date …>", "<table_of_contents/>" "<table-of-contents />". A tag
// capitalized or of a name neither reads is said as outside one, and a
// block's tag still stands among the block tags left open at the end
// ("$a<ul>b$" is refused for its <ul>), saying nothing else. An equation
// that an element's tag opens before holds no closing tag of it: in
// "<b>x $a</b>b$" the bold "x $a" is read first.

// notionMarkdownRemovedSpace stands for HTML Notion takes out with a space
// in its place (notionMarkdownRemovedSpaces): no text a write sends holds
// the byte.
const notionMarkdownRemovedSpace = "\xfb"

// notionMarkdownVersion2EquationRanges are where the inline equations a
// "v2" line's text holds stand, as its reading meets them from the left
// (notionMarkdownWholeAt), an element of a mark's or a span's tag read
// within its own text.
func notionMarkdownVersion2EquationRanges(text string) [][2]int {
	if !strings.Contains(text, "$") {
		return nil
	}
	ranges := [][2]int{}
	var read func(from, to int)
	read = func(from, to int) {
		for at := from; at < to; {
			if text[at] == '<' {
				if code, _, start, end, closed, ok := notionMarkdownStyleTag(text[:to], at, workspaceImportInlineStyle{}); ok {
					if !code {
						read(start, end)
					}
					at = closed
					continue
				}
				if _, start, end, closed, ok := notionMarkdownSpan(text[:to], at, workspaceImportInlineStyle{}); ok {
					read(start, end)
					at = closed
					continue
				}
			}
			end, whole := notionMarkdownWholeAt(text[:to], at)
			if whole && text[at] == '$' {
				ranges = append(ranges, [2]int{at, end})
			}
			at = end
		}
	}
	read(0, len(text))
	return ranges
}

// notionMarkdownEquationsMasked is a block's text with its inline equations
// as letters, which hold no tag: the length of each kept.
func notionMarkdownEquationsMasked(text string) string {
	ranges := notionMarkdownVersion2EquationRanges(text)
	if len(ranges) == 0 {
		return text
	}
	masked := []byte(text)
	for _, equation := range ranges {
		for at := equation[0]; at < equation[1]; at++ {
			masked[at] = 'x'
		}
	}
	return string(masked)
}

// line puts one line right, its code spans as they are and its equations'
// tags as an equation holds them (equation). The HTML taken out with what it
// holds goes first, equations and all ("$a<style>c</style>$" is "a ").
func (repair *notionMarkdownRepair) line(line string) string {
	equations := notionMarkdownVersion2EquationRanges(line)
	if len(equations) > 0 {
		line = repair.outsideCode(line, repair.dropped)
		equations = notionMarkdownVersion2EquationRanges(line)
	}
	var out strings.Builder
	at := 0
	for _, equation := range equations {
		out.WriteString(repair.spans(line[at:equation[0]], false, line))
		out.WriteString(repair.equation(line[equation[0]:equation[1]]))
		at = equation[1]
	}
	out.WriteString(repair.spans(line[at:], at == 0, line))
	return notionMarkdownRemovedSpaces(out.String())
}

// spans puts a stretch of a line right, its code spans as they are — from
// a run of backticks to the next as long, as Markdown reads them, a single
// backtick standing inside two, and <code> tags; whole is whether the
// stretch is all the line.
func (repair *notionMarkdownRepair) spans(stretch string, whole bool, line string) string {
	var out strings.Builder
	at := 0
	for _, code := range notionMarkdownInlineCodeRanges(stretch) {
		out.WriteString(repair.text(stretch[at:code[0]], whole && at == 0 && code[1] == len(stretch), line))
		out.WriteString(stretch[code[0]:code[1]])
		at = code[1]
	}
	out.WriteString(repair.text(stretch[at:], whole && at == 0, line))
	return out.String()
}

// outsideCode is a line with put applied to its stretches outside code spans.
func (repair *notionMarkdownRepair) outsideCode(line string, put func(string) string) string {
	var out strings.Builder
	at := 0
	for _, code := range notionMarkdownInlineCodeRanges(line) {
		out.WriteString(put(line[at:code[0]]))
		out.WriteString(line[code[0]:code[1]])
		at = code[1]
	}
	out.WriteString(put(line[at:]))
	return out.String()
}

// notionMarkdownInCode reports whether a line's byte stands in one of its
// code spans.
func notionMarkdownInCode(line string, at int) bool {
	return notionMarkdownInRanges(notionMarkdownInlineCodeRanges(line), at)
}

// notionMarkdownInRanges reports whether at stands in one of the ranges.
func notionMarkdownInRanges(ranges [][2]int, at int) bool {
	for _, one := range ranges {
		if one[0] <= at && at < one[1] {
			return true
		}
	}
	return false
}

// dropped is a stretch of text without the HTML Notion takes out with what
// it holds, a space in its place (notionMarkdownRemovedSpaces; seen
// 2026-11-14: "a<script>x</script>b" is "a b").
func (repair *notionMarkdownRepair) dropped(text string) string {
	for _, name := range []string{"script", "style", "head"} {
		text = notionMarkdownHTMLDropped[name].ReplaceAllStringFunc(text, func(string) string {
			words := notionMarkdownHTMLTagSaid(name, "taken out with what it held.")
			repair.say("html_tag_autofix", name, words)
			if repair.removed == nil {
				repair.removed = map[string]bool{}
			}
			repair.removed[words] = true
			return notionMarkdownRemovedSpace
		})
	}
	return text
}

// notionMarkdownVersion1Renamed are v1's tags written as their "v2" ones in
// an equation, the attributes they hold as written.
var notionMarkdownVersion1Renamed = map[string]string{
	"mention-page": "mention", "mention-user": "mention", "mention-database": "mention", "mention-data-source": "mention",
	"mention-date": "date", "synced_block": "synced-block", "synced_block_reference": "synced-block-reference",
}

// notionMarkdownVersion1RenamedTag is one of v1's tags written as its "v2"
// one (notionMarkdownVersion1Renamed), the attributes it holds as written,
// a <mention-database> of no data source's address of type="database".
func notionMarkdownVersion1RenamedTag(tag string) string {
	found := notionMarkdownRepairTag.FindStringSubmatchIndex(tag)
	if found == nil || found[0] != 0 {
		return tag
	}
	name, attributes := tag[found[4]:found[5]], tag[found[6]:found[7]]
	if name == "mention-database" && found[3] == found[2] &&
		!strings.HasPrefix(strings.TrimSpace(notionMarkdownAttributes(attributes)["url"]), notionMarkdownDataSourceAddress) {
		attributes += ` type="database"`
	}
	return "<" + tag[found[2]:found[3]] + notionMarkdownVersion1Renamed[name] + attributes + tag[found[7]:]
}

// equation puts an inline equation's tags right as Notion's "v2" does.
func (repair *notionMarkdownRepair) equation(text string) string {
	var out strings.Builder
	at := 0
	for _, found := range notionMarkdownRepairTag.FindAllStringSubmatchIndex(text, -1) {
		out.WriteString(text[at:found[0]])
		tag := text[found[0]:found[1]]
		at = found[1]
		written, attributes := text[found[4]:found[5]], text[found[6]:found[7]]
		closing, closedInSelf, name := found[3] > found[2], found[9] > found[8], strings.ToLower(written)
		known := notionMarkdownVersion2Tags[name] || notionMarkdownVersion1Tags[name] != "" || name == "details" || name == "summary"
		switch {
		case notionMarkdownEscapedAt(text, found[0]):
		case written != name || !known && !notionMarkdownHTMLTag(name) || name == "summary":
			repair.unsupported(written, attributes)
		case notionMarkdownHTMLTag(name):
			// One Notion leaves, an unclosed <script>, stays as written.
			if put := repair.tag(tag, name, closing, notionMarkdownAttributes(attributes), false, false); put != `\`+tag {
				tag = put
			}
		case name == "table_of_contents":
			repair.tag(tag, name, closing, nil, false, false)
			if !closing {
				tag = "<table-of-contents" + strings.TrimRight(attributes, " \t") + " />"
			}
		case notionMarkdownVersion1Renamed[name] != "":
			repair.tag(tag, name, closing, nil, false, false)
			renamed := notionMarkdownVersion1Renamed[name]
			tag = notionMarkdownVersion1RenamedTag(tag)
			if renamed == "synced-block" || renamed == "synced-block-reference" {
				repair.stands(renamed, closing, closedInSelf)
			}
		case name == "details" || notionMarkdownVersion2Tags[name] && !notionMarkdownVersion2InlineTags[name] && name != "unknown":
			repair.stands(name, closing, closedInSelf)
		}
		out.WriteString(tag)
	}
	out.WriteString(text[at:])
	return out.String()
}

// unsupported says a tag neither reads, kept as its words: one holding an
// attribute without a value says nothing (seen 2026-11-14: "a<x y>b" and
// "a<B c>b" say nothing, "a<x y=\"1\">b" says <x>).
func (repair *notionMarkdownRepair) unsupported(written, attributes string) {
	if notionMarkdownValuedAttributes.MatchString(attributes) {
		repair.say("unsupported_tag", written, notionMarkdownUnsupportedSaid(written))
	}
}

// notionMarkdownValuedAttributes are a tag's attributes each with a value.
var notionMarkdownValuedAttributes = regexp.MustCompile(`^(?:[ \t]+` + notionMarkdownAttributeName + `="[^"]*")*[ \t]*$`)

// notionMarkdownRemovedSpaces is a line with each run of the HTML taken out with a space
// in its place one space, or nothing where a space or a tab stands beside
// it or the line starts or ends there (seen 2026-11-14: "a<div>b</div>c" is
// "a b c", "a <hr> b" "a  b", "a<hr><hr>b" "a b", "<div>a</div>b" "a b",
// "a<br><div>b" "a⏎ b").
func notionMarkdownRemovedSpaces(line string) string {
	if !strings.Contains(line, notionMarkdownRemovedSpace) {
		return line
	}
	var out strings.Builder
	for at := 0; at < len(line); {
		if line[at] != notionMarkdownRemovedSpace[0] {
			out.WriteByte(line[at])
			at++
			continue
		}
		end := at
		for end < len(line) && line[end] == notionMarkdownRemovedSpace[0] {
			end++
		}
		blank := func(index int) bool {
			return index < 0 || index >= len(line) || line[index] == ' ' || line[index] == '\t'
		}
		if !blank(at-1) && !blank(end) {
			out.WriteByte(' ')
		}
		at = end
	}
	return out.String()
}
