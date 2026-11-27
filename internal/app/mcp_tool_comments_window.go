package app

import (
	"fmt"
	"strings"
)

// get-comments' answer is no longer than Notion's answers grow
// (mcpNotionAnswerLength): it writes a page's discussions in its order as
// far as they fit — at least one, however long —, its shown-count saying how
// many of total-count those are, and next_cursor, Leafen's own, naming where
// the rest go on: the same call with it as start_cursor writes those after
// (2026-11-24: once a page with 25,002 discussions could be read at all, they
// came in one answer of 32 million characters). Notion's answer counts what
// it shows apart from its total too; where it says to read on wasn't seen,
// as no Notion page compared had as many.

// mcpDiscussionWindow is the part of a page's ordered discussions an answer
// writes: from after the one after names ("" from the first), while they fit
// in what is left of its length.
type mcpDiscussionWindow struct {
	after string
	// total counts the discussions listed, left what the answer may still
	// hold, and last names the last one written while more follow.
	total, left int
	last        string
	cut         bool
}

// start is where the window starts among the ordered first comments of the
// discussions listed, which it counts; 0 for no window, which writes all.
func (window *mcpDiscussionWindow) start(roots []commentRecord) (int, error) {
	if window == nil {
		return 0, nil
	}
	window.total = len(roots)
	longest := 0
	for _, root := range roots {
		longest = max(longest, len(root.ID))
	}
	// The answer without a discussion, its counts and next_cursor at their longest.
	empty, err := mcpAnswerLength(notionMCPGetCommentsOutput{SuggestedEditsStatus: "available",
		Text: mcpDiscussionsHead(window.total, window.total) + "\n</discussions>", NextCursor: strings.Repeat("0", longest)})
	if err != nil {
		return 0, failedBecause(errMCPToolFailed, err)
	}
	window.left = mcpNotionAnswerLength - empty
	if window.after == "" {
		return 0, nil
	}
	for index, root := range roots {
		if root.ID == window.after {
			return index + 1, nil
		}
	}
	return 0, mcpInvalid("start_cursor is the next_cursor the last notion-get-comments gave, and the discussion it goes on after is gone or no longer listed: list again without start_cursor")
}

// takes says whether the window writes a discussion after shown others.
func (window *mcpDiscussionWindow) takes(discussion notionMCPDiscussion, shown int) (bool, error) {
	if window == nil {
		return true, nil
	}
	// Its lines as the answer's JSON writes them: without its own quotes,
	// with the line break before it ("\n", two characters).
	size, err := mcpAnswerLength(strings.Join(mcpDiscussionElement(discussion, len(discussion.Comments), true), "\n"))
	if err != nil {
		return false, failedBecause(errMCPToolFailed, err)
	}
	if shown > 0 && size > window.left {
		window.cut = true
		return false, nil
	}
	window.left -= size
	window.last = discussion.RootID
	return true, nil
}

// nextCursor is where the discussions go on after the answer; "" when it
// wrote the last.
func (window *mcpDiscussionWindow) nextCursor() string {
	if window == nil || !window.cut {
		return ""
	}
	return window.last
}

// text writes get-comments' discussions, those the window wrote of all it
// counted; "" for none, as Notion's answer then has no text.
func (window *mcpDiscussionWindow) text(discussions []notionMCPDiscussion) string {
	if len(discussions) == 0 {
		return ""
	}
	total := len(discussions)
	if window != nil {
		total = window.total
	}
	lines := []string{mcpDiscussionsHead(total, len(discussions))}
	for _, discussion := range discussions {
		lines = append(lines, mcpDiscussionElement(discussion, len(discussion.Comments), true)...)
	}
	return strings.Join(append(lines, "</discussions>"), "\n")
}

func mcpDiscussionsHead(total, shown int) string {
	return fmt.Sprintf(`<discussions total-count="%d" shown-count="%d">`, total, shown)
}
