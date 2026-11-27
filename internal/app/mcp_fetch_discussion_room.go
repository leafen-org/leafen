package app

import (
	"strings"
	"unicode/utf16"
)

// How many of a discussion's comments fetch shows: from its first, as many
// as fit in about 915 characters of <comment> elements as Notion's MCP
// writes them, the first always (seen 2026-11-11, 11-21 and 11-22 — four
// short comments on a line show three, seven of some fifty characters on the
// page two, four with titled mentions one or two). Notion's elements are
// counted as JavaScript counts a string's length, with Notion's address for
// a comment, which leads to the page at the discussion (?d=…&pvs=42) and,
// for one on a block, to the block (#…), so the count doesn't hang on this
// server's own address. No fourth comment ever fits.
const mcpFetchCommentRoom = 915

// mcpFetchNotionCommentAddressLength is the length of the address Notion's
// MCP writes for a comment, as an attribute.
func mcpFetchNotionCommentAddressLength(onBlock bool) int {
	id := strings.Repeat("0", 32)
	address := "https://app.notion.com/p/" + id + "?d=" + id + "&amp;pvs=42"
	if onBlock {
		address += "#" + id
	}
	return len(address)
}

// mcpFetchShownComments is how many of a discussion's first comments its
// fetch shows.
func mcpFetchShownComments(discussion notionMCPDiscussion) int {
	notionAddress := mcpFetchNotionCommentAddressLength(discussion.Context != "page")
	used, shown := 0, 0
	for _, comment := range discussion.Comments {
		alone := discussion
		alone.Comments = []notionMCPComment{comment}
		// Its element stands between the discussion's opening and closing lines.
		lines := mcpDiscussionElement(alone, 1, false)
		element := strings.Join(lines[1:len(lines)-1], "\n")
		used += mcpJavaScriptLength(element) - mcpJavaScriptLength(mcpFetchAttribute(comment.URL)) + notionAddress
		if shown > 0 && used > mcpFetchCommentRoom {
			break
		}
		shown++
	}
	return shown
}

// mcpJavaScriptLength is a string's length as JavaScript counts it, in
// UTF-16 code units.
func mcpJavaScriptLength(text string) int {
	length := 0
	for _, r := range text {
		length += utf16.RuneLen(r)
	}
	return length
}
