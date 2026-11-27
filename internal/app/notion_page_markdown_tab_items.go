package app

import "strings"

// A tab's item, the block whose text names the tab and whose blocks are the
// tab's, may be any block, as Notion's (seen 2026-11-14): "v1"'s
// "<tabs>\n# h\n</tabs>" is a tab block whose tab is the heading "h",
// "<tabs>\n<callout>\nc\n</callout>\n</tabs>" one whose tab is that callout,
// and "<tabs>\n$$\nx\n$$\n</tabs>" one whose tab is the equation "x"; a list
// item holds the items further in under it, a heading nothing. "v2" names a
// tab by the text first in its <tab> and reads a <tab> starting with another
// block as an empty paragraph holding it ("<tab>\n# h\n</tab>" is a tab named
// nothing holding the heading "h"); a <p> first is its name and the blocks
// that <p> holds its blocks.

// notionMarkdownGroupRun counts the lines from start that no item of a
// group holds, up to the next item's tag: a block written on several lines
// — code, an equation, a callout, a toggle, a list with its items — stays
// whole, and a tag holding its own items (tabs in tabs) with them.
func notionMarkdownGroupRun(body []string, start int, item string) int {
	for index := start; index < len(body); index++ {
		trimmed := strings.TrimSpace(body[index])
		match := notionMarkdownContainerOpen.FindStringSubmatch(trimmed)
		switch {
		case strings.HasPrefix(trimmed, "```"):
			fence := notionMarkdownFenceOf(trimmed)
			for index+1 < len(body) {
				index++
				if closing := strings.TrimSpace(body[index]); strings.HasPrefix(closing, fence) && strings.Trim(closing, "`") == "" {
					break
				}
			}
		case match == nil:
		case match[1] == item && index > start:
			return index - start
		case strings.TrimSpace(match[3]) == "":
			if end := notionMarkdownClosing(body, index, match[1]); end > index {
				index = end
			}
		}
	}
	return len(body) - start
}

// notionMarkdownGroupBeside reads a group's lines that no item holds, as one
// run of blocks at that level, without the blank lines at its ends.
func notionMarkdownGroupBeside(lines []string, level int) []string {
	out := notionMarkdownFlow(lines, level)
	for len(out) > 0 && strings.TrimSpace(out[0]) == "" {
		out = out[1:]
	}
	for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
		out = out[:len(out)-1]
	}
	return out
}

// notionMarkdownTabItem reads a <tab>'s lines as its item's text and the
// blocks the item holds.
func notionMarkdownTabItem(inner []string) (string, []string) {
	if text, rest, held := notionMarkdownTitleParagraph(inner); held {
		_, children := notionMarkdownHeldTitleBlocks("tab", text, notionMarkdownTitleParagraphBlocks(inner), rest, 0)
		return text, children
	}
	for _, line := range inner {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		// What Leafen's own form writes, a tab in, is its name as written.
		if !strings.HasPrefix(line, "\t") && notionMarkdownFlowItemHolds(trimmed) {
			return "", inner
		}
		break
	}
	return notionMarkdownTitle(inner)
}

// Notion writes a tab block's tabs so (seen 2026-11-14): "v1" each <tab>
// with its item's text alone on the line after it, whatever block the item is
// ("# h" as "h", a list item without its marker, an equation its
// expression), none when that is empty, and then the item's blocks; "v2" a
// paragraph's text, or <p /> before blocks that start with a paragraph, then
// its blocks, and another item as the block it is. A tab named nothing and
// holding nothing is <tab></tab> in "v2".

// tabItemText is a tab's item's text as "v1" names its tab.
func (w *notionMarkdownWriter) tabItemText(item workspaceExportMarkdownBlock) (string, bool) {
	if item.Type == graphBlockTypeEquation {
		return mapString(item.Content, "expression"), true
	}
	return notionPageMarkdownText(item, w.mentions)
}

// apiTabs writes a tab block as "v1" does.
func (w *notionMarkdownWriter) apiTabs(block workspaceExportMarkdownBlock, depth int) ([]string, bool) {
	names := []string{}
	for _, tab := range w.children[block.ID] {
		name, written := w.tabItemText(tab)
		if !written {
			return nil, false
		}
		names = append(names, strings.ReplaceAll(name, "\n", "<br>"))
	}
	indent := strings.Repeat("\t", depth)
	lines := []string{indent + "<tabs>"}
	w.rendered = append(w.rendered, block.ID)
	for index, tab := range w.children[block.ID] {
		w.visited[tab.ID] = true
		w.rendered = append(w.rendered, tab.ID)
		lines = append(lines, indent+"\t<tab"+notionMarkdownTabIcon(tab.Content, w.mentions)+">")
		if names[index] != "" {
			lines = append(lines, indent+"\t\t"+names[index])
		}
		lines = append(lines, w.apiFlow(w.children[tab.ID], depth+2)...)
		lines = append(lines, indent+"\t</tab>")
	}
	return append(lines, indent+"</tabs>"), true
}

// tabs writes a tab block as "v2" does.
func (w *notionMarkdownWriter) tabs(block workspaceExportMarkdownBlock, prefix string) ([]string, bool) {
	items := w.children[block.ID]
	names := make([]string, len(items))
	// A name that would start a block is text, as a paragraph's (seen
	// 2026-11-14: "# h" is written "\# h").
	paragraph := notionMarkdownParagraph
	if w.mentions.Version2 {
		paragraph = notionMarkdownVersion2Paragraph
	}
	for index, tab := range items {
		if tab.Type != graphBlockTypeParagraph {
			continue
		}
		name, written := notionPageMarkdownText(tab, w.mentions)
		if !written {
			return nil, false
		}
		if name != "" {
			names[index] = paragraph(strings.ReplaceAll(name, "\n", "<br />"))
		}
	}
	lines := []string{prefix + "<tabs>"}
	w.rendered = append(w.rendered, block.ID)
	for index, tab := range items {
		opening := "<tab" + notionMarkdownTabIcon(tab.Content, w.mentions) + ">"
		held := w.children[tab.ID]
		var section []string
		switch {
		case tab.Type != graphBlockTypeParagraph:
			section = append([]string{prefix + opening}, w.flow([]workspaceExportMarkdownBlock{tab}, prefix)...)
			section = append(section, prefix+"</tab>")
		case names[index] == "" && len(held) == 0:
			w.visited[tab.ID] = true
			w.rendered = append(w.rendered, tab.ID)
			section = []string{prefix + opening + "</tab>"}
		case names[index] == "" && held[0].Type != graphBlockTypeParagraph:
			w.visited[tab.ID] = true
			w.rendered = append(w.rendered, tab.ID)
			section = append(append([]string{prefix + opening}, w.flow(held, prefix)...), prefix+"</tab>")
		default:
			w.visited[tab.ID] = true
			section = w.held(tab, prefix, opening, names[index], "</tab>")
		}
		// Each tab is a section of its own where it stands in a page's
		// Markdown (notion_page_markdown_block_marks.go).
		lines = append(lines, w.marked(tab.ID, notionMarkdownTabSection, prefix, section)...)
	}
	return append(lines, prefix+"</tabs>"), true
}
