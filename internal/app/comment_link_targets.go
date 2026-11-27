package app

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A comment's address — /p/<page>#comment-<id>, as "링크 복사" and an
// inbox, mail or push notice name it, and an agent's discussion address —
// opens its discussion while the discussion is there. Notion's names the
// discussion itself (?d=<discussion>), and an old link to one whose first
// comment was deleted still opens it with the reply that is first now (seen
// 2026-11-21 twice, two tabs, synthetic page under a Notion test page: a text
// discussion whose first comment was deleted with "삭제하기"). Leafen's
// address names a comment, so one naming a deleted comment is followed
// through what stays of that comment (comment_tombstones.go) to its
// discussion, which keeps its discussion ID when a reply takes it over
// (comment_thread_handover.go): the address opens the discussion's first
// comment now. A block's discussion with no comment left still opens at its
// block, as Notion's old link chooses the block and opens a card that says
// none are left (seen 2026-11-21 twice, two tabs, a text discussion and a
// block's own discussion each deleted whole); a page's own discussion with
// none left opens nothing, as Notion's (seen 2026-11-21, a page's and a
// database page's).

func registerCommentLinkRoutes(event *core.ServeEvent) {
	event.Router.Route(commentLinkReadMethod, apiRoot+commentLinkReadPath, commentsOfRowPage(
		readPageCommentLinkRoute, readDatabaseRowCommentLinkRoute,
	))
}

func readPageCommentLinkRoute(event *core.RequestEvent) error {
	user, ok, err := commentActor(event)
	if !ok {
		return err
	}
	pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	response, err := readCommentLink(event.App, event.Request.PathValue("commentId"), func(app core.App) (commentAuthorization, error) {
		return authorizeCommentPage(app, user.Id, pageID, permissionWorkspaceRead, "")
	})
	if err != nil {
		return commentAPIError(event, err, persistentReadComment)
	}
	return event.JSON(http.StatusOK, response)
}

func readDatabaseRowCommentLinkRoute(event *core.RequestEvent) error {
	user, ok, err := commentActor(event)
	if !ok {
		return err
	}
	rowID := strings.TrimSpace(event.Request.PathValue("rowId"))
	response, err := readCommentLink(event.App, event.Request.PathValue("commentId"), func(app core.App) (commentAuthorization, error) {
		authorization, _, err := authorizeDatabaseRowCommentTarget(app, user.Id, rowID, permissionWorkspaceRead)
		return authorization, err
	})
	if err != nil {
		return databaseRowCommentAPIError(event, err, persistentReadComment)
	}
	return event.JSON(http.StatusOK, response)
}

// readCommentLink is the comment an address naming commentID opens on the
// target its reader may read: that comment while it is there, else its
// discussion's first comment now, else — the discussion deleted whole — the
// block it was left on while the block is there (BlockID, no comment).
func readCommentLink(
	app core.App, commentID string, authorize func(core.App) (commentAuthorization, error),
) (commentLinkResponse, error) {
	commentID = strings.TrimSpace(commentID)
	if !validCommentIdentifier(commentID) {
		return commentLinkResponse{}, errCommentNotFound
	}
	response := commentLinkResponse{}
	err := runReadSnapshot(context.Background(), app, func(txApp core.App) error {
		authorization, err := authorize(txApp)
		if err != nil {
			return err
		}
		target := authorization.Target
		if _, err := loadCommentForTarget(txApp, target, commentID); err == nil {
			response.CommentID = commentID
			return nil
		} else if !errors.Is(err, errCommentNotFound) {
			return err
		}
		workspaceID := authorization.Page.Workspace.WorkspaceID
		response.CommentID, err = commentLinkDiscussionNow(txApp, workspaceID, target, commentID)
		if errors.Is(err, errCommentNotFound) {
			response.BlockID, err = commentLinkGoneDiscussionBlock(txApp, workspaceID, target, commentID)
		}
		return err
	})
	return response, err
}

// commentLinkDiscussionNow is the first comment now of the discussion a
// deleted comment of the target was in.
func commentLinkDiscussionNow(
	app core.App, workspaceID string, target nativeCommentTarget, deletedID string,
) (string, error) {
	var threadID string
	err := app.DB().NewQuery(`
		SELECT [[thread]].[[id]]
		FROM {{leafen_comment_tombstones}} AS [[gone]]
		JOIN {{leafen_comments}} AS [[thread]] INDEXED BY [[idx_leafen_comments_notion_discussion]]
			ON [[thread]].[[workspace_id]]=[[gone]].[[workspace_id]]
			AND [[thread]].[[page_id]]=[[gone]].[[page_id]]
			AND [[thread]].[[notion_discussion_id]]=[[gone]].[[notion_discussion_id]]
		WHERE [[gone]].[[id]]={:id} AND [[gone]].[[workspace_id]]={:workspaceId}
			AND [[gone]].[[target_kind]]={:targetKind} AND [[gone]].[[target_id]]={:targetId}
			AND [[gone]].[[notion_discussion_id]]!=''
			AND [[thread]].[[target_kind]]={:targetKind} AND [[thread]].[[target_id]]={:targetId}
			AND [[thread]].[[parent_id]]=''
		ORDER BY [[thread]].[[created_at]],[[thread]].[[id]]
		LIMIT 1
	`).Bind(dbx.Params{
		"id": deletedID, "workspaceId": workspaceID,
		"targetKind": target.Kind, "targetId": target.ID,
	}).Row(&threadID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errCommentNotFound
	}
	return threadID, err
}

// commentLinkGoneDiscussionBlock is the block of the target a deleted
// comment's discussion was left on, once none of its comments is left: any
// of its comments kept as deleted names it (a reply the API made may name
// none). A page's own discussion, or a block deleted since, has none.
func commentLinkGoneDiscussionBlock(
	app core.App, workspaceID string, target nativeCommentTarget, deletedID string,
) (string, error) {
	var blockID string
	err := app.DB().NewQuery(`
		SELECT [[kept]].[[block_id]]
		FROM {{leafen_comment_tombstones}} AS [[gone]]
		JOIN {{leafen_comment_tombstones}} AS [[kept]] INDEXED BY [[idx_leafen_comment_tombstones_workspace]]
			ON [[kept]].[[workspace_id]]=[[gone]].[[workspace_id]]
			AND [[kept]].[[page_id]]=[[gone]].[[page_id]]
			AND [[kept]].[[notion_discussion_id]]=[[gone]].[[notion_discussion_id]]
		JOIN {{leafen_blocks}} AS [[block]]
			ON [[block]].[[id]]=[[kept]].[[block_id]]
			AND [[block]].[[workspace_id]]=[[gone]].[[workspace_id]]
			AND [[block]].[[page_id]]={:targetId}
		WHERE [[gone]].[[id]]={:id} AND [[gone]].[[workspace_id]]={:workspaceId}
			AND [[gone]].[[target_kind]]={:targetKind} AND [[gone]].[[target_id]]={:targetId}
			AND [[gone]].[[notion_discussion_id]]!=''
			AND [[kept]].[[target_kind]]={:targetKind} AND [[kept]].[[target_id]]={:targetId}
			AND [[kept]].[[block_id]]!=''
		ORDER BY [[kept]].[[created_at]],[[kept]].[[id]]
		LIMIT 1
	`).Bind(dbx.Params{
		"id": deletedID, "workspaceId": workspaceID,
		"targetKind": target.Kind, "targetId": target.ID,
	}).Row(&blockID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errCommentNotFound
	}
	return blockID, err
}
