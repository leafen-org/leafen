package app

import (
	"encoding/json"
	"regexp"
	"strings"
)

// A line a page's Markdown write gives a <span discussion-urls="…"> naming
// a discussion the page doesn't have, its words as they were, edits its
// block, as Notion's MCP and public API update_content do (seen 2026-11-22,
// worker w2339 twice and worker w2345 by MCP and the API: the block's
// last_edited_time and last_edited_by, its fetch's time and the page's last
// edit become the write's, on a page with discussions or none). Its words
// stay unmarked (notionMarkdownSitePages.discussionThreads); the block is
// written again as it stands (notionMarkdownTouch), so its row, the page's
// last edit and its history take the writer's edit. Neither an old_str
// written as it was, a "\." spelled out, nor a line an old_str spans and
// leaves edits its block, and get-version-changes lists no record for it
// alone (its before and after are the same) but counts it among the edits
// of a block listed for its making (mcpVersionTouchEdits).

// notionMarkdownDiscussionURLs finds the addresses a <span> names its
// discussions by.
var notionMarkdownDiscussionURLs = regexp.MustCompile(`<span\s[^>]*discussion-urls="([^"]*)"`)

// notionMarkdownTouchMark marks, in a second reading, the words a span
// naming no discussion of the page holds; no thread is named so.
const notionMarkdownTouchMark = "\x00touched"

// namesNoDiscussion reports whether a span's addresses name one the page
// has no discussion at; false for a reader that reads no discussions.
func (site *notionMarkdownSitePages) namesNoDiscussion(addresses string) bool {
	if site == nil {
		return false
	}
	for _, address := range strings.Split(addresses, ",") {
		if strings.TrimSpace(address) == "" {
			continue
		}
		if site.Discussions == nil || site.Discussions(address) == "" {
			return true
		}
	}
	return false
}

// touchesBlocks reports whether Markdown holds a span naming a discussion
// the page doesn't have.
func (site *notionMarkdownSitePages) touchesBlocks(markdown string) bool {
	if site == nil || !strings.Contains(markdown, "discussion-urls=") {
		return false
	}
	for _, match := range notionMarkdownDiscussionURLs.FindAllStringSubmatch(markdown, -1) {
		if site.namesNoDiscussion(match[1]) {
			return true
		}
	}
	return false
}

// sameMarkdownBlocksUntouched tells whether Markdown changes nothing of the
// body's blocks (sameMarkdownBlocks) and edits none of them either.
func sameMarkdownBlocksUntouched(markdown string, before notionPageMarkdownProjection) bool {
	return !before.SitePages.touchesBlocks(markdown) && sameMarkdownBlocks(markdown, before)
}

// notionMarkdownTouchedLines are the lines, by their items' places, that
// hold a span naming a discussion the page doesn't have; none when there is
// no such span or the second reading doesn't read as the first (count items).
func notionMarkdownTouchedLines(markdown string, keepLineEnds bool, site *notionMarkdownSitePages, count int) map[int]bool {
	if !site.touchesBlocks(markdown) {
		return nil
	}
	marked := *site
	marked.Discussions = func(addresses string) string {
		threads := site.discussionThreads(addresses)
		if site.namesNoDiscussion(addresses) {
			threads = commentThreadsWith(threads, notionMarkdownTouchMark)
		}
		return threads
	}
	items, err := parseNotionMarkdown(markdown, keepLineEnds, &marked)
	if err != nil || len(items) != count {
		return nil
	}
	touched := map[int]bool{}
	for index, item := range items {
		if strings.Contains(string(item.Content), notionMarkdownTouchMark) {
			touched[index] = true
			continue
		}
		for _, span := range item.Spans {
			if strings.Contains(span.CommentID, notionMarkdownTouchMark) {
				touched[index] = true
				break
			}
		}
	}
	return touched
}

// notionMarkdownTouch writes a text block that stays as it stands, its
// content merged from the line's own onto what it holds
// (resolveGraphBlockContentUpdates): its row, the page and the history take
// the edit, its words, content and place stay. None for a block the line
// doesn't read as the kind it is stored as, or one holding no words.
func notionMarkdownTouch(blockID string, parentID *string, item workspaceImportStagedItem, current *notionPageMarkdownProjection) []graphOperation {
	if current == nil || current.BlockTypes[blockID] != item.Type {
		return nil
	}
	if _, words := graphTextBlockTypes[item.Type]; !words {
		return nil
	}
	content := json.RawMessage(item.Content)
	if len(content) == 0 {
		content = json.RawMessage(`{}`)
	}
	return []graphOperation{{Kind: graphOperationKindUpsertBlock, Block: &graphBlock{
		ID: blockID, ParentID: parentID, Type: item.Type, Content: content, BaseContent: content,
	}}}
}
