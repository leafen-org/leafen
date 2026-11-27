package app

import (
	"regexp"
	"slices"
	"strings"
)

// The block's tag holding the own closing tag of an item read inline
// (notionMarkdownVersion2ItemHolder) is said closed as a flow tag
// (notionMarkdownUnclosedBlock) unless Notion's "v2" finds it closed, as its API answered (seen
// 2026-11-15, "<li>a\</li>⏎<p>y </li> w" then more): by a closing tag of
// its name after the item's that no tag of its name opened since closes,
// as text — in code too ("…⏎z </p> q", "…⏎```⏎z </p>⏎```", "…⏎`</p>`"),
// not escaped; by the Markdown ending in a closing tag of its name, line
// breaks after it but no spaces ("…⏎<p>c</p>", "…⏎z⏎<p>c</p>⏎",
// "…⏎<p>c</p>⏎<li>d</li>⏎<p>e</p>"; but "…⏎<p>c</p>⏎z",
// "…⏎<p>c</p>⏎<li>d</li>", "…⏎<p>c</p>  " and "…⏎z \</p>" say it); or by
// the first block's tag after it left open to the end being of its name
// ("…⏎<p>z", "…⏎z <p>c", "…⏎<li>c</li>⏎<p>z" and "…⏎<p>z⏎<callout>c" say
// it not, "…⏎<callout>z⏎<p>c" and "…⏎<li>c⏎<p>z" say it). Another block's
// tag left open to the end stands before the end ("…⏎<callout>z⏎<p>c</p>"
// says it); what a list or a callout after it opens its closing tag closes
// ("…⏎<ul>⏎<li>c⏎</ul>⏎<p>z" says it not). In a list the closing tag of the
// item's list is that end, and a block's tag left open there closes nothing
// ("<ul>⏎<li>a\</li>⏎<p>y </li> w⏎<p>c</p>⏎</ul>" says it not;
// "…⏎<p>z⏎</ul>" and "…⏎</ul>⏎<p>c</p>" say it).

// notionMarkdownVersion2HolderClosed tells the block's tag of the name
// holding an item's own closing tag (end) closed so, the lines before
// scope standing in the item's list (scope len(lines) for none).
func notionMarkdownVersion2HolderClosed(lines []string, end notionMarkdownVersion2ItemEnd, name string, scope int) bool {
	rest := append([]string{lines[end.line][end.at:]}, lines[end.line+1:scope]...)
	after := strings.Join(rest, "\n")
	if notionMarkdownVersion2ClosesAsText(after, name) {
		return true
	}
	if first := notionMarkdownVersion2FirstLeftOpen(rest); first != "" {
		return first == name && scope == len(lines)
	}
	text, closing := strings.TrimRight(after, "\n"), "</"+name+">"
	return strings.HasSuffix(text, closing) && !notionMarkdownEscapedAt(text, len(text)-len(closing))
}

// notionMarkdownVersion2ClosesAsText tells text after a tag of the name
// holding a closing tag of that name that closes it, tags of its name
// opened since closing first; escaped ones are none, ones in code are.
func notionMarkdownVersion2ClosesAsText(text, name string) bool {
	depth := 0
	for _, found := range notionMarkdownVersion2LeftOpenTag.FindAllStringSubmatchIndex(text, -1) {
		if text[found[4]:found[5]] != name || found[7] > found[6] || notionMarkdownEscapedAt(text, found[0]) {
			continue
		}
		if found[3] == found[2] {
			depth++
		} else if depth--; depth < 0 {
			return true
		}
	}
	return false
}

// notionMarkdownVersion2HolderOpenTag is a tag notionMarkdownVersion2LeftOpenTag
// reads, or an item's.
var notionMarkdownVersion2HolderOpenTag = regexp.MustCompile(`<(/?)(b|i|s|u|em|strong|code|span|a|p|h[1-4]|quote|toggle|callout|details|todo|li|ul|ol)(?:[ \t][^<>]*)?(/?)>`)

// notionMarkdownVersion2FirstLeftOpen is the name of the first block's tag
// lines open, outside code and not escaped, that no closing tag of its own
// closes after it, an item's among them; "" for none.
func notionMarkdownVersion2FirstLeftOpen(lines []string) string {
	type opened struct {
		name     string
		line, at int
	}
	open, fence := []opened{}, ""
	for index, line := range lines {
		text := strings.TrimSpace(line[len(notionMarkdownVersion2MarkerRun.FindString(line)):])
		if fence != "" || notionMarkdownVersion2Fence(text) {
			if fence == "" {
				fence = notionMarkdownFenceOf(text)
			} else if notionMarkdownFenceCloses(text, fence, true) {
				fence = ""
			}
			continue
		}
		codes := notionMarkdownInlineCodeRanges(line)
		for _, found := range notionMarkdownVersion2HolderOpenTag.FindAllStringSubmatchIndex(line, -1) {
			name := line[found[4]:found[5]]
			if found[7] > found[6] || notionMarkdownVersion2InlineTags[name] || notionMarkdownEscapedAt(line, found[0]) ||
				slices.ContainsFunc(codes, func(code [2]int) bool { return code[0] <= found[0] && found[0] < code[1] }) {
				continue
			}
			if found[3] == found[2] {
				open = append(open, opened{name: name, line: index, at: found[0]})
				continue
			}
			// A list's or a callout's closing tag closes what was opened in it,
			// read as Markdown of its own (notionMarkdownVersion2RestSaid).
			for at := len(open) - 1; at >= 0; at-- {
				if open[at].name == name && (name == "ul" || name == "ol" || name == "callout") {
					open = open[:at]
					break
				} else if open[at].name == name {
					open = slices.Delete(open, at, at+1)
					break
				}
			}
		}
	}
	if len(open) == 0 {
		return ""
	}
	return open[0].name
}
