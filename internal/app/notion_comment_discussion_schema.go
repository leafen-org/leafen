package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func notionRootCommentDiscussionID(workspaceID, commentID string) (string, error) {
	return notionCompatibleObjectID(
		notionObjectIdentityComment, workspaceID, "discussion:"+commentID,
	)
}

func notionCommentDiscussionID(app core.App, comment storedComment) (string, error) {
	if comment.ParentID == "" {
		return notionRootCommentDiscussionID(comment.WorkspaceID, comment.ID)
	}
	parent, err := loadCommentForTarget(app, nativeCommentTarget{
		Kind: comment.TargetKind, ID: comment.TargetID, StoragePageID: comment.PageID,
	}, comment.ParentID)
	if err != nil || parent.WorkspaceID != comment.WorkspaceID ||
		parent.NotionDiscussionID == "" {
		return "", refusedUnlessStorage(errInvalidNotionObjectIdentity, err)
	}
	return parent.NotionDiscussionID, nil
}

func notionImportedCommentDiscussionID(
	app core.App,
	workspaceID string,
	comment workspaceImportStagedComment,
) (string, error) {
	if comment.ParentID == "" {
		return notionRootCommentDiscussionID(workspaceID, comment.ID)
	}
	var discussionID string
	err := app.DB().NewQuery(`
		SELECT [[notion_discussion_id]] FROM {{leafen_comments}}
		WHERE [[workspace_id]]={:workspaceId} AND [[target_kind]]={:targetKind}
			AND [[target_id]]={:targetId} AND [[id]]={:parentId}
	`).Bind(dbx.Params{
		"workspaceId": workspaceID, "targetKind": comment.TargetKind,
		"targetId": comment.TargetID, "parentId": comment.ParentID,
	}).Row(&discussionID)
	if err != nil || discussionID == "" {
		return "", refusedUnlessStorage(errInvalidNotionObjectIdentity, err)
	}
	return discussionID, nil
}
