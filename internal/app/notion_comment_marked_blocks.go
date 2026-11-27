package app

import (
	"fmt"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Notion's comment list of a block gives the discussions whose marks stand in
// its text now, besides those left on it (seen 2026-11-22, worker w2339,
// after update_content moved marks between blocks): a discussion first left
// on "echo" whose mark now stands on "november" in another block is listed
// under that block, its parent still the block it was left on, and no longer
// under its first block, as one that lost its words (placed); one marked in
// two blocks is listed under both — as Notion's MCP names a discussion by the
// block its marks stand in (mcp_discussion_places.go). Listed there, it
// hasn't lost what it was left on (original_content_deleted false, seen
// 2026-11-22 twice), though the page's list says it has.

// notionCommentsMarkedInto reads at most limit of the open comments of the
// discussions marking a block's text that were left on another block, from
// a place in its list or after it.
func notionCommentsMarkedInto(
	app core.App, target notionCommentTarget, afterCreated, afterID string, including bool, limit int,
) ([]storedComment, error) {
	threads := notionBlockMarkThreads(app, target.PageID, target.BlockID)
	if len(threads) == 0 {
		return nil, nil
	}
	params := dbx.Params{
		"workspaceId": target.Identity.WorkspaceID, "targetKind": target.TargetKind, "targetId": target.PageID,
		"blockId": target.BlockID, "afterCreated": afterCreated, "afterId": afterID, "limit": limit,
	}
	names := make([]string, 0, len(threads))
	for index, thread := range threads {
		name := fmt.Sprintf("thread%d", index)
		params[name] = thread
		names = append(names, "{:"+name+"}")
	}
	rows := []storedComment{}
	err := app.DB().NewQuery(`
		SELECT [[thread]].[[id]],[[thread]].[[workspace_id]],[[thread]].[[page_id]],
			[[thread]].[[target_kind]],[[thread]].[[target_id]],
			[[thread]].[[block_id]],[[thread]].[[parent_id]],[[thread]].[[author_id]],
			[[thread]].[[body_json]],[[thread]].[[notion_discussion_id]],[[thread]].[[resolved]],
			[[thread]].[[revision]],[[thread]].[[created_at]],[[thread]].[[updated_at]],[[thread]].[[edited_at]]
		FROM {{leafen_comments}} AS [[root]]
		JOIN {{leafen_comments}} AS [[thread]] INDEXED BY [[idx_leafen_comments_discussion_lookup]]
			ON [[thread]].[[workspace_id]]=[[root]].[[workspace_id]]
				AND [[thread]].[[notion_discussion_id]]=[[root]].[[notion_discussion_id]]
		WHERE [[root]].[[id]] IN (` + strings.Join(names, ",") + `)
			AND [[root]].[[workspace_id]]={:workspaceId} AND [[root]].[[target_kind]]={:targetKind}
			AND [[root]].[[target_id]]={:targetId} AND [[root]].[[parent_id]]='' AND [[root]].[[resolved]]=0
			AND [[root]].[[block_id]]!={:blockId}
			AND NOT EXISTS (SELECT 1 FROM {{leafen_comment_quote_blocks}} AS [[place]]
				WHERE [[place]].[[comment_id]]=[[root]].[[id]] AND [[place]].[[block_id]]={:blockId})
			AND [[thread]].[[target_kind]]={:targetKind} AND [[thread]].[[target_id]]={:targetId}
			AND [[thread]].[[resolved]]=0
			AND ({:afterCreated}='' OR [[thread]].[[created_at]]>{:afterCreated}
				OR ([[thread]].[[created_at]]={:afterCreated} AND [[thread]].[[id]]` + notionCursorComparison(including) + `{:afterId}))
		ORDER BY [[thread]].[[created_at]],[[thread]].[[id]] LIMIT {:limit}
	`).Bind(params).All(&rows)
	return rows, err
}

// notionCommentMarkedInto reports whether a comment's discussion marks a
// block's text that it wasn't left on, so that block's list gives it.
func notionCommentMarkedInto(app core.App, comment storedComment, blockID string) bool {
	if blockID == "" || comment.NotionDiscussionID == "" {
		return false
	}
	root := ""
	if err := app.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_comments}}
		WHERE [[workspace_id]]={:workspaceId} AND [[notion_discussion_id]]={:discussionId}
			AND [[target_kind]]={:targetKind} AND [[target_id]]={:targetId} AND [[parent_id]]=''
		LIMIT 1
	`).Bind(dbx.Params{
		"workspaceId": comment.WorkspaceID, "discussionId": comment.NotionDiscussionID,
		"targetKind": comment.TargetKind, "targetId": comment.TargetID,
	}).Row(&root); err != nil {
		return false
	}
	for _, thread := range notionBlockMarkThreads(app, comment.TargetID, blockID) {
		if thread == root {
			return true
		}
	}
	return false
}

// notionBlockMarkThreads are the discussions, by their first comments' IDs,
// whose marks stand in a block's text in a page's or a database page's
// (room) text.
func notionBlockMarkThreads(app core.App, room, blockID string) []string {
	document, found, err := loadYgoDoc(app, room)
	if err != nil || !found {
		return nil
	}
	threads := []string{}
	for _, part := range document.GetText(ygoBlockTextRootPrefix + blockID).ToDelta() {
		marked, _ := part.Attributes["commentId"].(string)
		threads = append(threads, strings.Fields(marked)...)
	}
	return uniqueStrings(threads)
}

// standing tells an answer that the comments listed under a block their
// discussions' marks stand in haven't lost what they were left on.
func (originals *notionCommentOriginals) standing(rows []storedComment) {
	if originals.discussions == nil {
		originals.discussions = map[string]bool{}
	}
	for _, row := range rows {
		originals.discussions[row.WorkspaceID+"/"+row.TargetKind+"/"+row.TargetID+"/"+row.NotionDiscussionID] = false
	}
}
