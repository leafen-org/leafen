package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A thread left on words chosen across blocks is listed under each block it
// marks, as Notion's comment list lists it under every one of their
// block_ids, its parent still the first block (seen 2026-11-09). The comment
// keeps its first block in its own row; the other blocks its root's body
// names in "quoteBlocks" (comment_text_anchors.go) are kept here, so a
// block's comments are found through an index rather than by reading every
// comment's body. The database keeps these rows to the root's body itself,
// on every insert and every change of its body, block or parent, and drops
// them with the comment.

// commentListedUnderBlock reports whether a comment's thread marks the block
// besides its first: a block lookup lists the whole thread there.
func commentListedUnderBlock(app core.App, comment storedComment, blockID string) (bool, error) {
	if blockID == "" || comment.NotionDiscussionID == "" {
		return false, nil
	}
	var listed bool
	err := app.DB().NewQuery(`
		SELECT EXISTS (
			SELECT 1 FROM {{leafen_comment_quote_blocks}} AS [[place]]
				INDEXED BY [[idx_leafen_comment_quote_blocks_block]]
			JOIN {{leafen_comments}} AS [[root]] ON [[root]].[[id]] = [[place]].[[comment_id]]
			WHERE [[place]].[[block_id]] = {:blockId} AND [[root]].[[parent_id]] = ''
				AND [[root]].[[workspace_id]] = {:workspaceId}
				AND [[root]].[[notion_discussion_id]] = {:discussionId}
				AND [[root]].[[target_kind]] = {:targetKind} AND [[root]].[[target_id]] = {:targetId})
	`).Bind(dbx.Params{
		"blockId": blockID, "workspaceId": comment.WorkspaceID,
		"discussionId": comment.NotionDiscussionID,
		"targetKind":   comment.TargetKind, "targetId": comment.TargetID,
	}).Row(&listed)
	return listed, err
}
