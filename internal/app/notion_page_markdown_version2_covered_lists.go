package app

import (
	"regexp"
	"strings"
)

// After the line on which an item a list making no list left open closes
// over a block's tag left open to the end (notionMarkdownVersion2CoveredTags),
// a list whose closing tag stands on the Markdown's last line, nothing but
// spaces after it, stands in that block's text as Notion's "v2" reads it
// (seen 2026-11-15, its API's create): where its opening tag holds more on
// its line, that tag is a list tag in text, said to stand inline, and the
// items on its line are words said nothing of ("…⏎<p>y </li>⏎<ul><li>c</li>
// </ul>" says "Trailing …: <p>.", the <ul> inline, a's bare, then the <p>
// closed as a flow tag; "<ol>…</ol>", "<ul><li>c</li><li>d</li></ul>",
// "<ul> <li>c</li></ul>" and "<ul><li>c</li>⏎</ul>" alike; "…⏎<ul><li>c</li>⏎
// <li>d</li>⏎</ul>" says its <ul> left open as an inline tag too, and d's
// bare after the <p>); and an item whose own closing tag that closing tag
// follows on the last line has more after it there, said inline ("…⏎<ul>⏎
// <li>c</li></ul>" says c's inline, not bare). With a line break after it
// the list is the list it is ("…⏎<ul><li>c</li></ul>⏎" says a's bare and the
// <p> alone).

// notionMarkdownVersion2CoveredListOpening is a list's opening tag starting
// a line's text.
var notionMarkdownVersion2CoveredListOpening = regexp.MustCompile(`^<(ul|ol)(?:[ \t][^<>]*)?>`)

// notionMarkdownVersion2CoveredListLine marks such a list opening rows[index]
// with more on its line, its items there escaped and its closing tag a tag
// in text; it says whether it did.
func notionMarkdownVersion2CoveredListLine(rows []string, index int, written string) bool {
	line := rows[index]
	at := len(notionMarkdownVersion2MarkerRun.FindString(line))
	at += len(line[at:]) - len(strings.TrimLeft(line[at:], " \t"))
	match := notionMarkdownVersion2CoveredListOpening.FindStringSubmatch(line[at:])
	if match == nil || strings.TrimSpace(line[at+len(match[0]):]) == "" {
		return false
	}
	last, rest := len(rows)-1, line[at+len(match[0]):]
	closing := strings.Index(rest, "</"+match[1]+">")
	switch {
	case closing >= 0 && index == last && strings.TrimRight(written, " \t") != "":
		rest = notionMarkdownVersion2WordItems(rest[:closing]) + notionMarkdownListTagMark + rest[closing:]
	case closing < 0 && notionMarkdownVersion2ListClosingLine(rows, index, match[1]) == last:
		rows[last] = notionMarkdownVersion2MarkListTag(rows[last])
		rest = notionMarkdownVersion2WordItems(rest)
	default:
		return false
	}
	rows[index] = line[:at] + notionMarkdownListTagMark + match[0] + rest
	return true
}

// notionMarkdownVersion2WordItems escapes the item tags text holds, so that
// they are its words.
func notionMarkdownVersion2WordItems(text string) string {
	return notionMarkdownVersion2LiTag.ReplaceAllStringFunc(text, func(tag string) string { return `\` + tag })
}

// notionMarkdownVersion2ClosingSplit tells rows ending in a list's closing
// tag in such text that stood after text on the Markdown's last line,
// written, before it stood on a line of its own.
func notionMarkdownVersion2ClosingSplit(rows []string, written string) bool {
	last := strings.TrimSpace(rows[len(rows)-1])
	last = strings.TrimSpace(last[len(notionMarkdownVersion2MarkerRun.FindString(last)):])
	text := strings.TrimSpace(written[len(notionMarkdownVersion2MarkerRun.FindString(written)):])
	return strings.HasPrefix(last, notionMarkdownListTagMark+"</") && text != "" && !strings.HasPrefix(text, "</")
}
