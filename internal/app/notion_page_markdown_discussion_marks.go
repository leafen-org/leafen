package app

import "strings"

// A page's Markdown names the discussions runs of words were left on in
// <span discussion-urls="…">, and a write of it reads them back, as Notion's
// MCP does (seen 2026-11-22, worker w2334: update_content's old_str is
// matched against the Markdown with those spans, so "charlie." finds nothing
// when "bravo charlie" holds a discussion; a change inside a span keeps its
// mark on the new words, "india"→"india2 x" in "hotel india juliet" marking
// "hotel india2 x juliet"; a change beside one leaves it as it is; a span a
// write takes away or replace_content leaves out takes the mark away, one
// it adds naming a discussion of the page marks its words, another block's
// too, and one naming no discussion of the page is its words alone). The
// writer names them (notionMarkdownReader.Discussions) and its reading takes
// them back (notionMarkdownReader.Threads, notionMarkdownSitePages.Discussions).

func notionMarkdownSpanOpeningAs(value string, start int, style workspaceImportInlineStyle, dropBadColor bool) (workspaceImportInlineStyle, int, bool) {
	if !strings.HasPrefix(value[start:], "<span") {
		return style, 0, false
	}
	open := strings.IndexByte(value[start:], '>')
	if open < 0 {
		return style, 0, false
	}
	attributes := value[start+len("<span") : start+open]
	for _, attribute := range notionMarkdownAttributePattern.FindAllStringSubmatch(attributes, -1) {
		switch attribute[1] {
		case "underline":
			style.underline = attribute[2] == "true"
		case "discussion-urls":
			// A span inside another names its discussions too.
			style.discussions = strings.Trim(style.discussions+","+attribute[2], ",")
		case "color":
			color := strings.TrimSuffix(attribute[2], "_bg")
			if color != attribute[2] {
				color += "_background"
			}
			if !validNotionBlockColor(color) {
				if dropBadColor {
					continue
				}
				return style, 0, false
			}
			if strings.HasSuffix(color, "_background") {
				style.background = color
			} else if color != "default" {
				style.color = color
			}
		}
	}
	return style, start + open + 1, true
}

// discussionThreads are the threads a run of words its Markdown names
// discussions on is marked with, space-separated as a text's commentId holds
// them (commentAnchorRun); "" for a reader that reads no discussions back.
func (site *notionMarkdownSitePages) discussionThreads(addresses string) string {
	if site == nil || site.Discussions == nil || strings.TrimSpace(addresses) == "" {
		return ""
	}
	return site.Discussions(addresses)
}

// at is the mentions writing a block's text, whose discussions' addresses
// name that block (mcp_discussion_places.go).
func (mentions notionMarkdownMentions) at(blockID string) notionMarkdownMentions {
	mentions.TextBlock = blockID
	return mentions
}
