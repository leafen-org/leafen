package app

import (
	"slices"
	"strings"
)

// A list, a table, columns or tabs whose tag stands in text and that no
// closing tag closes to the end holds, as Notion's "v2" reads it (seen
// 2026-11-14), the item, row, cell, column or tab its last line stands in or
// closes as a tag in text: that item is no bare one, a list's <li> said as a
// list tag that cannot stand inline — "x <ul>⏎<li>a</li>" says its <ul> left
// open and the <li> inline, "x <ul>⏎<li>a</li>⏎<li>b</li>" those and one bare
// item, "x <table>⏎<tr>⏎<td>a</td>⏎</tr>" a bare <td> alone, "x <columns>⏎
// <column>⏎a⏎</column>" its <columns> alone; a last line of other text
// ("x <ul>⏎<li>a</li>⏎y") leaves its items bare.
// So too where another block's tag stands in text that nothing closes to the
// end — after words on its line ("x <toggle>", "x <p>"), or first on its line
// with its opening refused ("<toggle x:y=\"1\">", "<p x:y=\"1\">", "<ul
// x:y=\"1\">", notionMarkdownVersion2RefusedOpening): Notion's "v2" reads the
// item tags of the Markdown's last line in that text, an <li> said inline
// for each one opening there and a <td> or a <column> said nothing of, the
// items before it bare (seen 2026-11-16, written twice: "x <toggle>⏎<li>a</li>"
// and "<toggle x:y=\"1\">⏎<li>a</li>" say the <li> inline, "…⏎<li>a</li>⏎
// <li>b</li>" it and one bare, "…⏎<li>a</li><li>b</li>" two inline,
// "<toggle x:y=\"1\">⏎<td>a</td>" and "…⏎<column>a</column>" nothing of
// them; "<toggle>⏎<li>a</li>", "<p>y⏎<li>c</li>" and a last line of other
// text or a line break after it leave it bare).

// notionMarkdownVersion2TextGroupItems are such a group's items' tags.
var notionMarkdownVersion2TextGroupItems = map[string][]string{
	"ul": {"li"}, "ol": {"li"}, "table": {"tr", "td", "th"}, "columns": {"column"}, "tabs": {"tab"},
}

// notionMarkdownVersion2TextHeldItems are the item tags another block's tag
// standing in text holds on the last line.
var notionMarkdownVersion2TextHeldItems = []string{"li", "tr", "td", "th", "column", "tab"}

// textBlockLeftOpen tells a block's tag standing in text that nothing
// closes or closes over to the end, other than an item's: a group's holds
// the other items so too ("<columns x:y=\"1\">⏎<li>a</li>" says the <li>
// inline).
func (repair *notionMarkdownRepair) textBlockLeftOpen() bool {
	for _, one := range notionMarkdownBlockTagsOpen(repair.inline) {
		if !one.over && repair.inline[one.event].text && !slices.Contains(notionMarkdownVersion2TextHeldItems, one.name) && one.name != "summary" {
			return true
		}
	}
	return false
}

// lastLineHeld puts what Notion says of the items the last of lines holds in
// such groups left open, and in another block's tag so left open, the
// Markdown's last line read as written (rows, before an item's text stands
// apart from its opening tag); and of the items on a line where the
// closing tag of another block's tag in text that a closing tag around it
// closes over lands (notionMarkdownVersion2Landings).
func (repair *notionMarkdownRepair) lastLineHeld(lines, rows []string) {
	repair.landedHeld(lines)
	lines = rows
	last := ""
	for at := len(lines) - 1; at >= 0 && last == ""; at-- {
		last = strings.TrimSpace(lines[at])
		last = strings.TrimSpace(last[len(notionMarkdownVersion2MarkerRun.FindString(last)):])
	}
	_, open, _ := notionMarkdownBlockTagsLeft(repair.inline)
	held := []string{}
	for _, group := range open {
		if repair.openInText(group) {
			held = append(held, notionMarkdownVersion2TextGroupItems[group]...)
		}
	}
	inText := repair.textBlockLeftOpen()
	if inText {
		held = append(held, notionMarkdownVersion2TextHeldItems...)
	}
	slices.Sort(held)
	for _, item := range slices.Compact(held) {
		if !notionMarkdownVersion2Opened(last, item) && !strings.Contains(last, "</"+item+">") {
			continue
		}
		// An item standing in text is said inline already
		// (notionMarkdownVersion2LastLineItems), one after words on its
		// line is said with them, and a line break ending the Markdown
		// leaves its items bare (seen 2026-11-15: "x <ul>⏎<li>a</li>⏎"
		// says the <ul> left open and a's bare, "x <ol>⏎w <li>c</li>" the
		// <ol> alone).
		if item == "li" && (notionMarkdownVersion2TextItemOpening(last) || strings.TrimSpace(lines[len(lines)-1]) == "" ||
			!strings.HasPrefix(last, "<li") && notionMarkdownVersion2Opened(last, "li")) {
			continue
		}
		said := notionMarkdownBareItemSaid[item]
		if said == "" {
			said = notionMarkdownTablePartAlone(item)
		}
		if item == "li" {
			// Each opening on the line is said inline, in place of the first said bare;
			// one a list opened in that text holds is said so too ("<toggle x:y=\"1\">⏎<ul>⏎
			// <li>a</li>" says it inline, the <ul> left open).
			for range max(1, notionMarkdownVersion2Openings(last, "li")) {
				if at := slices.Index(repair.missing, said); at >= 0 {
					repair.missing[at] = notionMarkdownListTagInline("li")
				} else if inText && strings.HasPrefix(last, "<li") {
					repair.missing = append(repair.missing, notionMarkdownListTagInline("li"))
				}
			}
			continue
		}
		for at := len(repair.missing) - 1; at >= 0; at-- {
			if repair.missing[at] == said {
				repair.missing = slices.Delete(repair.missing, at, at+1)
				break
			}
		}
	}
}

// notionMarkdownVersion2Openings counts the opening tags of the name text
// holds unescaped (notionMarkdownVersion2Opened).
func notionMarkdownVersion2Openings(text, name string) int {
	count := 0
	for at := strings.Index(text, "<"+name); at >= 0; {
		if end := at + 1 + len(name); !notionMarkdownEscapedAt(text, at) && (end == len(text) || strings.ContainsRune(" \t/>", rune(text[end]))) {
			count++
		}
		next := strings.Index(text[at+1:], "<"+name)
		if next < 0 {
			break
		}
		at += 1 + next
	}
	return count
}

// landedHeld says the items on a line where the closing tag of a block's
// tag a closing tag around it closes over lands inline, in place of bare,
// where that tag's opening is refused or stands in text.
func (repair *notionMarkdownRepair) landedHeld(lines []string) {
	for _, one := range notionMarkdownVersion2Landings(lines) {
		if !one.nothing || one.line < 0 || one.line == len(lines)-1 {
			continue
		}
		for range notionMarkdownVersion2LandedItems(lines[one.line]) {
			if at := slices.Index(repair.missing, notionMarkdownBareItemSaid["li"]); at >= 0 {
				repair.missing[at] = notionMarkdownListTagInline("li")
			}
		}
	}
}
