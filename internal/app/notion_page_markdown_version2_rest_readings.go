package app

import (
	"regexp"
	"strings"
)

// After an item read inline whose own closing tag a block's tag holds
// (notionMarkdownVersion2BrokenItems), Notion's "v2" reads what follows as
// it reads any Markdown, saying what that says where it stands (seen
// 2026-11-15, its API's create): the rest of the list the item stands in,
// past the item's closing tag's line, as Markdown of its own, no list
// holding its items, after the item's words ("<ul>⏎<li>a\</li>⏎<p>y </li> w⏎
// <li>c⏎</ul>" says the list's children, the <p>, the item's two, then
// "Trailing …: <li>." and c's bare <li>, as "<li>c" alone says them;
// "…⏎<li>c⏎<li>d⏎</ul>", "…⏎<p>z⏎</ul>", "…⏎<callout>z⏎</ul>" and
// "…⏎<ul>⏎<li>c⏎</ul>⏎</ul>" alike), and a list or a callout after it on a
// line of its own, to its closing tag, among the items after it bare
// ("<li>a\</li>⏎<p>y </li> w⏎<ul>⏎<li>c⏎</ul>⏎<li>d</li>" says the item's
// words, then "Missing closing </li> tag. The flow tag…", as "<ul>⏎<li>c⏎
// </ul>" says it, then d's bare <li>; "…⏎<callout>⏎<li>c⏎</callout>"
// "Trailing …: <li>." and c's bare <li> after the item's words). An item
// read inline whose closing tag holds more words after it ("<ul>⏎<li>a</li>
// w⏎<li>c⏎</ul>") leaves its list's items its list's: each left open is
// closed as a flow tag, the first right after the list's children, the
// others before their bare <li> ("…⏎<li>c⏎<li>d⏎</ul>" says the children,
// c's flow tag, a's inline, c's bare, d's flow tag and d's bare). Items
// after the list around it read inline are said before that list's words
// ("<ul>⏎<li>a\</li>⏎<p>y </li> w⏎<li>c</li>⏎</ul>⏎z <li>d</li>" says d's
// inline first), bare ones after.

// notionMarkdownVersion2RestContainer is a list's or a callout's opening
// tag alone on its line.
var notionMarkdownVersion2RestContainer = regexp.MustCompile(`^<(ul|ol|callout)(?:[ \t][^<>]*)?>$`)

// notionMarkdownVersion2RestSaid is what Notion says of lines read as
// Markdown of their own: its refusal's words, "" for none.
func notionMarkdownVersion2RestSaid(lines []string) string {
	text := strings.Join(lines, "\n")
	if strings.TrimSpace(text) == "" {
		return ""
	}
	_, _, err := notionMarkdownVersion2Input(strings.ReplaceAll(text, notionMarkdownListTagMark, ""))
	if err == nil {
		return ""
	}
	said, _ := strings.CutPrefix(err.Error(), notionMarkdownRefusedHead)
	return said
}

// notionMarkdownVersion2RestContainerEnd is the line of the closing tag
// alone on its line of the list or callout whose opening tag lines[at] is,
// those of its kind opened after it closing first; -1 for none.
func notionMarkdownVersion2RestContainerEnd(lines []string, at int) int {
	match := notionMarkdownVersion2RestContainer.FindStringSubmatch(strings.TrimSpace(lines[at]))
	if match == nil {
		return -1
	}
	depth := 0
	for index := at + 1; index < len(lines); index++ {
		text := strings.TrimSpace(lines[index])
		switch {
		case text == "</"+match[1]+">":
			if depth--; depth < 0 {
				return index
			}
		case strings.HasPrefix(text, "<"+match[1]) && notionMarkdownVersion2RestContainer.MatchString(text):
			depth++
		}
	}
	return -1
}
