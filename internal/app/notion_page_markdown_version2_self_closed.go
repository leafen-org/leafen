package app

import (
	"regexp"
	"strings"
)

// A "v2" block tag closed in itself on a line of its own makes the block it
// names when that block may hold nothing, as Notion's "v2" reads it (seen
// 2026-11-14): "<page />" is a new untitled sub-page, "<database />" a new
// database, "<equation />" an empty equation, its color too, "<tr />" in a
// table an empty row, "<td />" in a row an empty cell and "<li />" in a <ul>
// or <ol> an empty item — each as its tag opened and closed at once, the
// first three in a list item or a quote too. One
// whose block holds something is refused (notionMarkdownRepair.selfClosed),
// <mail /> and <meeting-notes /> stay text as <mail> and <meeting-notes>
// do, and a line of more than one of them is text ("<h3 /><h2 />" and
// "<p /><p />" are a paragraph of their words, notionMarkdownSelfClosedRun).

var (
	notionMarkdownVersion2SelfClosedBlock  = regexp.MustCompile(`^<(page|database|equation|tr|td|li)((?:[ \t]+[^\s=<>/]+="[^"]*")*)[ \t]*/>$`)
	notionMarkdownVersion2GroupOpening     = regexp.MustCompile(`^<(/?)(table|tr|ul|ol)(?:[ \t]+[^<>]*)?>$`)
	notionMarkdownVersion2SpacedEmptyBlock = regexp.MustCompile(`^<empty-block((?:[ \t]+[^\s=<>/]+="[^"]*")*)[ \t]*/[ \t]*>$`)
	notionMarkdownSelfClosedTags           = regexp.MustCompile(`^(?:[ \t]*<[a-z][a-z0-9-]*(?:[ \t]+[^\s=<>/]+="[^"]*")*[ \t]*/>)+$`)
)

// notionMarkdownVersion2SelfClosedBlocks writes those tags opened and closed
// at once, outside code and equations; a row's, a cell's and an item's only
// where their table, row or list is open.
func notionMarkdownVersion2SelfClosedBlocks(lines []string) []string {
	fence, equation := "", false
	open := map[string]int{}
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case fence != "":
			if strings.HasPrefix(trimmed, fence) && strings.Trim(trimmed, "`") == "" {
				fence = ""
			}
			continue
		case notionMarkdownVersion2Fence(trimmed):
			fence = notionMarkdownFenceOf(trimmed)
			continue
		case equation:
			equation = trimmed != "$$"
			continue
		case trimmed == "$$" && notionMarkdownVersion2EquationCloses(lines[index+1:], 0):
			equation = true
			continue
		}
		// In a list item or a quote too ("- <page />" is an item holding a
		// new sub-page).
		prefix := notionMarkdownVersion2MarkerRun.FindString(line)
		// An empty block's tag closes in itself with spaces before and after
		// its slash too (seen 2026-11-15: "<empty-block />", "<empty-block\t/>",
		// "<empty-block / >" and "<empty-block/ >" are empty paragraphs, in an
		// item, a quote or a callout too; "<empty-block />x" is words).
		if empty := notionMarkdownVersion2SpacedEmptyBlock.FindStringSubmatch(strings.TrimSpace(line[len(prefix):])); empty != nil {
			lines[index] = prefix + "<empty-block" + empty[1] + "/>"
			continue
		}
		match := notionMarkdownVersion2SelfClosedBlock.FindStringSubmatch(strings.TrimSpace(line[len(prefix):]))
		if group := notionMarkdownVersion2GroupOpening.FindStringSubmatch(trimmed); match == nil && group != nil {
			if group[1] == "" {
				open[group[2]]++
			} else if open[group[2]] > 0 {
				open[group[2]]--
			}
			continue
		}
		if match == nil {
			continue
		}
		item := strings.TrimSpace(prefix) != ""
		switch match[1] {
		case "tr":
			if open["table"] == 0 || item {
				continue
			}
		case "td":
			if open["tr"] == 0 || item {
				continue
			}
		case "li":
			if open["ul"]+open["ol"] == 0 || item {
				continue
			}
		}
		lines[index] = prefix + "<" + match[1] + match[2] + "></" + match[1] + ">"
	}
	return lines
}

// notionMarkdownSelfClosedRun tells a line of more than one tag closed in
// itself and nothing else, which is text.
func notionMarkdownSelfClosedRun(trimmed string) bool {
	return strings.Count(trimmed, "/>") > 1 && notionMarkdownSelfClosedTags.MatchString(trimmed)
}
