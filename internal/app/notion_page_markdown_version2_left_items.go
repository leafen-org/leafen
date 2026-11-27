package app

import "strings"

// What Notion's "v2" says of the <li> lines after a closing tag that code
// hid from its pairing of tags, which leaves its tag open
// (notionMarkdownVersion2PairFences), after the tags left open (seen
// 2026-11-14): the first <li> starting a line out of a list, closed on its
// line, is inline — "<callout>⏎- ```⏎  a⏎  ```⏎</callout>⏎<li>x</li>" says
// that <li> is a list tag standing inside text (notionMarkdownListTagInline),
// a blank line or text before it alike — and one its line doesn't close
// ("<li>x\</li>", "<li>") its flow tag closed at the end, and bare; every
// other <li> starting a line, a list's too, is bare. In a list left open
// its items are bare as notionMarkdownRepair.bareItems says them instead
// ("<ul>⏎<li>⏎- ```⏎  a⏎  ```⏎</li>⏎<li>b</li>⏎</ul>").

// notionMarkdownVersion2LeftItems is what Notion says of the <li> lines
// after the fence put in at lines[left], or none when left is -1.
func notionMarkdownVersion2LeftItems(lines []string, left int) []string {
	if left < 0 || left >= len(lines) {
		return nil
	}
	said, depth, first := []string{}, 0, true
	for _, line := range lines[left+1:] {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "<ul") || strings.HasPrefix(trimmed, "<ol"):
			depth++
		case strings.HasPrefix(trimmed, "</ul>") || strings.HasPrefix(trimmed, "</ol>"):
			depth--
		case strings.HasPrefix(trimmed, "<li") && notionMarkdownVersion2FlowOpening.MatchString(trimmed):
			opening := notionMarkdownVersion2FlowOpening.FindString(trimmed)
			closed := notionMarkdownVersion2FlowClosing(trimmed[len(opening):], "li") >= 0
			switch {
			case first && depth <= 0 && closed:
				said = append(said, notionMarkdownListTagInline("li"))
			case first && depth <= 0:
				said = append(said, notionMarkdownUnclosedBlock("li"), notionMarkdownBareItemSaid["li"])
			default:
				said = append(said, notionMarkdownBareItemSaid["li"])
			}
			first = false
		}
	}
	return said
}
