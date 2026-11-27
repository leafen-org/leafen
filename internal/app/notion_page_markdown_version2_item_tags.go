package app

import (
	"regexp"
	"strings"
)

// A list item whose text starts with a block's tag is, as Notion's "v2"
// reads it (seen 2026-11-14), an empty item holding that block, its tag's
// lines its own wherever they stand: "- <page></page>", "- <equation />",
// "1. <page></page>", "- <callout>a</callout>", "- <callout>⏎a⏎</callout>",
// "- <details>…", "- <columns>…", "- <table>…", "- <table-of-contents />",
// "- <image …>c</image>", "- <p>a</p>", "- <h2>a</h2>", "- <empty-block/>" and
// v1's "- <table_of_contents></table_of_contents>"
// alike, a line further in after it the item's too ("- <callout>a
// </callout>⏎  b"). Text after the closing tag makes it text ("- <callout>a
// </callout> b", notionMarkdownVersion2BrokenPairs), and a to-do's is
// refused as inline ("- [ ] <page></page>"). An <li> so is an item no list
// holds, bare ("- <li>a⏎  b</li>", "1. <li>a</li>", "- <li>a" alike), save a
// to-do's, which is inline ("- [ ] <li>a⏎  b</li>"), and one whose closing
// tag text follows ("- <li>a⏎  b</li> c" is inline, as a block's tag so is
// text, notionMarkdownVersion2BrokenPairs).

var notionMarkdownVersion2ItemTag = regexp.MustCompile(`^<(p|h[1-4]|quote|toggle|callout|ul|ol|li|columns|tabs|table|equation|table-of-contents|table_of_contents|image|file|pdf|video|audio|bookmark|embed|page|database|synced-block|synced-block-reference|details|empty-block)(?:[ \t/>]|$)`)

// notionMarkdownVersion2ItemTags puts a block's tag a list item's text starts
// with on a line of its own, as far in as the item's blocks stand, after the
// item's mark alone.
func notionMarkdownVersion2ItemTags(lines []string) []string {
	out, fence := make([]string, 0, len(lines)), ""
	for index, line := range lines {
		prefix := notionMarkdownVersion2MarkerRun.FindString(line)
		body := strings.TrimSpace(line[len(prefix):])
		switch {
		case fence != "":
			if trimmed := strings.TrimSpace(strings.TrimLeft(line, " \t>")); strings.HasPrefix(trimmed, fence) && strings.Trim(trimmed, "`") == "" {
				fence = ""
			}
		case notionMarkdownVersion2Fence(body):
			fence = notionMarkdownFenceOf(body)
		case strings.Trim(prefix, " \t>") != "" && notionMarkdownVersion2ItemTag.MatchString(body) &&
			!notionMarkdownVersion2ItemTagBroken(lines, index, len(line)-len(strings.TrimLeft(line[len(prefix):], " \t")), body):
			opened, _ := notionMarkdownVersion2Opens(line, 0)
			out = append(out, strings.TrimRight(prefix, " \t"), notionMarkdownVersion2HeldPrefix(opened)+body)
			continue
		}
		out = append(out, line)
	}
	return out
}

// notionMarkdownVersion2ItemTagBroken tells an <li> a list item's text,
// body at lines[index][at:], starts with whose closing tag text follows on
// its line.
func notionMarkdownVersion2ItemTagBroken(lines []string, index, at int, body string) bool {
	opening := notionMarkdownVersion2FlowOpening.FindString(body)
	if !strings.HasPrefix(opening, "<li") {
		return false
	}
	closer, _ := notionMarkdownVersion2PairBroken(lines, index, at+len(opening), "li")
	return closer >= 0
}
