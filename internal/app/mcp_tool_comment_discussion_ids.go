package app

import (
	"fmt"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// mcpDiscussionID is a discussion's address, as Notion's names one: its
// page, the block it is on — the page's own ID for one on the page — and
// the discussion's own ID (mcpDiscussionKey).
func mcpDiscussionID(app core.App, workspaceID, pageID, blockID, rootID string) string {
	return mcpDiscussionAddress(pageID, blockID, mcpDiscussionKey(app, workspaceID, rootID))
}

// mcpDiscussionAddress is the address of a discussion by its own ID.
func mcpDiscussionAddress(pageID, blockID, key string) string {
	if blockID == "" {
		blockID = pageID
	}
	return fmt.Sprintf("discussion://%s/%s/%s", pageID, blockID, key)
}

// mcpDiscussionKey is a discussion's own ID, apart from its first comment's,
// as Notion's MCP gives one (seen 2026-11-13: create-comment's id and
// discussion_id differ): the ID Leafen's Notion API names it by, which each
// of its comments keeps — made from its first comment's ID, and kept when
// that comment is deleted and a reply takes over (comment_thread_handover.go).
func mcpDiscussionKey(app core.App, workspaceID, rootID string) string {
	kept := ""
	if err := app.DB().NewQuery(`
		SELECT [[notion_discussion_id]] FROM {{leafen_comments}}
		WHERE [[id]]={:id} AND [[workspace_id]]={:workspaceId}
	`).Bind(dbx.Params{"id": rootID, "workspaceId": workspaceID}).Row(&kept); err == nil && kept != "" {
		return kept
	}
	key, err := notionRootCommentDiscussionID(workspaceID, rootID)
	if err != nil {
		return rootID
	}
	return key
}
