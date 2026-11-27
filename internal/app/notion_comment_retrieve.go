package app

import (
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

func serveNotionCommentRetrieve(event *core.RequestEvent, publicCommentID string) error {
	if answered, err := serveNotionDeletedCommentRetrieve(event, publicCommentID); answered {
		return err
	}
	target, row, ok, err := authorizeNotionComment(
		event, publicCommentID, "comments:read", permissionWorkspaceRead,
	)
	if err != nil || !ok {
		return err
	}
	response, err := notionCommentObject(
		event.App, row, newNotionBlockFileCapabilityIssuer(event.Request, target.Identity), target.Identity,
		&notionCommentOriginals{},
	)
	if err != nil {
		return notionCommentAPIError(event, err)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, response)
}

// serveNotionDeletedCommentRetrieve answers GET /v1/comments/{id} for a
// deleted comment as Notion's (seen 2026-11-21, three deleted comments): 200
// with the comment as it was — parent, discussion, author and display name —
// its words gone (rich_text [], original_content_deleted true) and
// last_edited_time when it was deleted (comment_tombstones.go). It is read
// as its comment would be: the same token, scope, workspace and page access.
// answered is false where no deleted comment has the ID; the live read then
// answers, a 404 where there's none.
func serveNotionDeletedCommentRetrieve(event *core.RequestEvent, publicCommentID string) (answered bool, err error) {
	resolved, err := resolveNotionObjectIdentity(
		event.App, notionObjectIdentityComment, strings.TrimSpace(publicCommentID),
	)
	if err != nil {
		return false, nil
	}
	row, found, err := loadCommentTombstone(event.App, resolved.InternalID)
	if err != nil {
		return true, notionCommentInternalError(event, err)
	}
	if !found {
		return false, nil
	}
	workspace, ok, err := authorizeNotionWorkspaceOperation(event, "comments:read", permissionWorkspaceRead)
	if err != nil || !ok {
		return true, err
	}
	identity := workspace.Identity
	if resolved.Workspace != identity.WorkspaceID {
		return true, notionCommentNotFound(event)
	}
	target, err := authorizeNotionStoredCommentTarget(event.App, identity, row, permissionWorkspaceRead)
	if errors.Is(err, errCommentNotFound) || isPermissionDenied(err) ||
		errors.Is(err, errCommentDeleteBusy) || errors.Is(err, errDatabaseRowPageNotFound) {
		return true, notionCommentNotFound(event)
	}
	if err != nil {
		return true, notionCommentInternalError(event, err)
	}
	response, err := notionCommentObject(
		event.App, row, newNotionBlockFileCapabilityIssuer(event.Request, target.Identity), target.Identity,
		&notionCommentOriginals{},
	)
	if err != nil {
		return true, notionCommentAPIError(event, err)
	}
	response.RichText, response.Attachments = []map[string]any{}, nil
	response.OriginalContentDeleted, response.HasPendingAttachments = true, false
	event.Response.Header().Set("Cache-Control", "no-store")
	return true, event.JSON(http.StatusOK, response)
}
