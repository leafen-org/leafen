package app

import (
	"encoding/json"

	"github.com/pocketbase/pocketbase/core"
)

// A discussion that lost what it was left on — its block deleted, or none of
// the words it was left on still carrying its mark (a write that left its
// <span discussion-urls> out, words typed over) — stays among the page's
// open discussions, its words no longer quoted and no card beside the text,
// as Notion's screen shows one (seen 2026-11-27, a span left out of a
// replace_content through the public API: the words unmarked, no card in the
// margin, and in the discussions side view the thread under a line saying
// its original is deleted in place of its words) and as its public API says
// of it (original_content_deleted, notionCommentOriginals.deleted — the
// same rule here). Notion's MCP get-comments leaves such a discussion out of
// its list (mcp_discussion_places.go). The screen reads it on each
// discussion's first comment (originalGone).

// attachCommentListDetails sets a listed page's or database page's comment
// records' details (attachCommentDetails), and on each discussion's first
// comment whether it lost what it was left on; rows are the records' stored
// comments, in the same order.
func attachCommentListDetails(app core.App, actorID string, rows []storedComment, records []commentRecord) error {
	originals := &notionCommentOriginals{}
	for index, row := range rows {
		if row.ParentID != "" || index >= len(records) {
			continue
		}
		// A suggested edit marks its words its own way and settles there.
		if body := (suggestionThreadBody{}); json.Unmarshal([]byte(row.BodyJSON), &body) == nil && body.Suggestion.Kind != "" {
			continue
		}
		gone, err := originals.deleted(app, row)
		if err != nil {
			return err
		}
		records[index].OriginalGone = gone
	}
	return attachCommentDetails(app, actorID, records)
}
