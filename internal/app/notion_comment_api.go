package app

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const notionAPICommentsPath = notionAPIRoot + "/comments"

func serveNotionCommentList(event *core.RequestEvent) error {
	// block_id is checked before (refuseNotionRequest); one this token
	// can't reach is a block Notion can't find.
	publicTargetID := strings.TrimSpace(event.Request.URL.Query().Get("block_id"))
	noteNotionAskedObject(event, "block", publicTargetID)
	target, ok, err := authorizeNotionCommentTarget(
		event, publicTargetID, "comments:read", permissionWorkspaceRead,
	)
	if err != nil || !ok {
		return err
	}
	pageSize := notionCommentDefaultPageSize
	if raw := notionQueryDigits(event.Request.URL.Query().Get("page_size")); raw != "" {
		pageSize, err = strconv.Atoi(raw)
		if err != nil || pageSize < 1 || pageSize > notionCommentMaximumPageSize {
			return notionPageValidationError(event, "page_size must be between 1 and 100.")
		}
	}
	originals := &notionCommentOriginals{}
	rows, next, blockGone, err := listNotionCommentRows(
		event.App, target, strings.TrimSpace(event.Request.URL.Query().Get("start_cursor")), pageSize, originals,
	)
	if err != nil {
		return notionCommentAPIError(event, err)
	}
	issuer := newNotionBlockFileCapabilityIssuer(event.Request, target.Identity)
	results := make([]notionComment, 0, len(rows))
	for _, row := range rows {
		projected, err := notionCommentObject(event.App, row, issuer, target.Identity, originals)
		if err != nil {
			return notionCommentAPIError(event, err)
		}
		// A deleted block's list gives its comments without their words,
		// as Notion's does (seen 2026-11-11).
		if blockGone {
			projected.RichText = []map[string]any{}
		}
		results = append(results, projected)
	}
	var nextCursor any
	if next != nil {
		nextCursor, err = notionObjectIdentityPublicID(event.App, notionObjectIdentityComment, next.WorkspaceID, next.ID)
		if err != nil {
			return notionCommentInternalError(event, err)
		}
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, map[string]any{
		"object": "list", "next_cursor": nextCursor, "has_more": next != nil,
		"results": results, "type": "comment", "comment": map[string]any{},
	})
}

// listNotionCommentRows lists a page's or a block's open comments in the
// order they were written, from the one a start_cursor names: a block's are
// those on it and, as Notion's list, the whole of each thread whose words
// run on into it (comment_quote_blocks_schema.go); a discussion that lost
// what it was left on is listed with the page's
// (notionCommentOriginals.placed). next is the comment after the page, which
// next_cursor names (notionListPage); blockGone is whether the block listed
// is deleted.
func listNotionCommentRows(
	app core.App,
	target notionCommentTarget,
	startCursor string,
	pageSize int,
	originals *notionCommentOriginals,
) (rows []storedComment, next *storedComment, blockGone bool, err error) {
	afterCreated, afterID := "", ""
	if startCursor != "" {
		resolved, err := resolveNotionObjectIdentity(app, notionObjectIdentityComment, startCursor)
		if err != nil || resolved.Workspace != target.Identity.WorkspaceID {
			return nil, nil, false, notionInvalid(errCommentInvalid, notionStartCursorInvalid+startCursor)
		}
		cursor, err := loadCommentByID(app, resolved.InternalID)
		// A page's list holds its blocks' discussions that lost their
		// content too.
		listed := err == nil && cursor.TargetKind == target.TargetKind && cursor.TargetID == target.PageID &&
			(cursor.BlockID == target.BlockID || target.BlockID == "")
		if err == nil && !listed && cursor.TargetKind == target.TargetKind && cursor.TargetID == target.PageID {
			if listed, err = commentListedUnderBlock(app, cursor, target.BlockID); err != nil {
				return nil, nil, false, err
			}
			listed = listed || notionCommentMarkedInto(app, cursor, target.BlockID)
		}
		if !listed {
			return nil, nil, false, notionInvalid(errCommentInvalid, notionStartCursorInvalid+startCursor)
		}
		afterCreated, afterID = cursor.CreatedAt, cursor.ID
	}
	return originals.placed(app, target, afterCreated, afterID, startCursor != "", pageSize, func(afterCreated, afterID string, including bool, limit int) ([]storedComment, error) {
		return listNotionCommentRowsAfter(app, target, afterCreated, afterID, including, limit)
	})
}

// notionCommentOpenSQL holds for a comment of {{leafen_comments}} whose
// discussion isn't resolved.
const notionCommentOpenSQL = `NOT EXISTS (
	SELECT 1 FROM {{leafen_comments}} AS [[root]]
		INDEXED BY [[idx_leafen_comments_discussion_lookup]]
	WHERE [[root]].[[workspace_id]]={{leafen_comments}}.[[workspace_id]]
		AND [[root]].[[target_kind]]={{leafen_comments}}.[[target_kind]]
		AND [[root]].[[target_id]]={{leafen_comments}}.[[target_id]]
		AND [[root]].[[notion_discussion_id]]={{leafen_comments}}.[[notion_discussion_id]]
		AND [[root]].[[parent_id]]='' AND [[root]].[[resolved]]=1)`

// listNotionCommentRowsAfter reads at most limit of the open comments left
// where the list's are, from a place in the list or after it.
func listNotionCommentRowsAfter(
	app core.App, target notionCommentTarget, afterCreated, afterID string, including bool, limit int,
) ([]storedComment, error) {
	rows := []storedComment{}
	err := app.DB().NewQuery(`
		SELECT [[id]],[[workspace_id]],[[page_id]],[[target_kind]],[[target_id]],
			[[block_id]],[[parent_id]],[[author_id]],
			[[body_json]],[[notion_discussion_id]],[[resolved]],[[revision]],
			[[created_at]],[[updated_at]],[[edited_at]]
		FROM {{leafen_comments}} INDEXED BY [[idx_leafen_comments_target_order]]
		WHERE [[workspace_id]]={:workspaceId} AND [[target_kind]]={:targetKind}
			AND [[target_id]]={:targetId}
			AND [[block_id]]={:blockId} AND [[resolved]]=0
			AND ({:afterCreated}='' OR [[created_at]]>{:afterCreated}
				OR ([[created_at]]={:afterCreated} AND [[id]]` + notionCursorComparison(including) + `{:afterId}))
			AND ` + notionCommentOpenSQL + `
		UNION ALL
		SELECT [[thread]].[[id]],[[thread]].[[workspace_id]],[[thread]].[[page_id]],
			[[thread]].[[target_kind]],[[thread]].[[target_id]],
			[[thread]].[[block_id]],[[thread]].[[parent_id]],[[thread]].[[author_id]],
			[[thread]].[[body_json]],[[thread]].[[notion_discussion_id]],[[thread]].[[resolved]],
			[[thread]].[[revision]],[[thread]].[[created_at]],[[thread]].[[updated_at]],[[thread]].[[edited_at]]
		FROM {{leafen_comment_quote_blocks}} AS [[place]]
			INDEXED BY [[idx_leafen_comment_quote_blocks_block]]
		JOIN {{leafen_comments}} AS [[root]] ON [[root]].[[id]]=[[place]].[[comment_id]]
		JOIN {{leafen_comments}} AS [[thread]] INDEXED BY [[idx_leafen_comments_discussion_lookup]]
			ON [[thread]].[[workspace_id]]=[[root]].[[workspace_id]]
				AND [[thread]].[[notion_discussion_id]]=[[root]].[[notion_discussion_id]]
		WHERE [[place]].[[block_id]]={:blockId}
			AND [[root]].[[workspace_id]]={:workspaceId} AND [[root]].[[target_kind]]={:targetKind}
			AND [[root]].[[target_id]]={:targetId} AND [[root]].[[parent_id]]='' AND [[root]].[[resolved]]=0
			AND [[thread]].[[target_kind]]={:targetKind} AND [[thread]].[[target_id]]={:targetId}
			AND [[thread]].[[block_id]]!={:blockId} AND [[thread]].[[resolved]]=0
			AND ({:afterCreated}='' OR [[thread]].[[created_at]]>{:afterCreated}
				OR ([[thread]].[[created_at]]={:afterCreated} AND [[thread]].[[id]]` + notionCursorComparison(including) + `{:afterId}))
		ORDER BY [[created_at]],[[id]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"workspaceId": target.Identity.WorkspaceID, "targetKind": target.TargetKind,
		"targetId": target.PageID,
		"blockId":  target.BlockID, "afterCreated": afterCreated, "afterId": afterID,
		"limit": limit,
	}).All(&rows)
	return rows, err
}

func serveNotionCommentCreate(event *core.RequestEvent) error {
	input := notionCommentCreateRequest{}
	if err := bindNotionStrictJSON(event, &input); err != nil {
		return notionBindRefused(event, err)
	}
	var target notionCommentTarget
	var err error
	parentID := ""
	if (input.Parent == nil) == (strings.TrimSpace(input.DiscussionID) == "") {
		return notionPageValidationError(event, "Exactly one comment target is required.")
	}
	if input.Parent != nil {
		publicTargetID, parentErr := notionCommentParentID(*input.Parent)
		if parentErr != nil {
			return notionPageValidationError(event, "The comment parent is invalid.")
		}
		// A parent this token can't reach is named a block, a page's too, as
		// Notion's API names it (seen 2026-11-10).
		noteNotionAskedObject(event, "block", publicTargetID)
		var ok bool
		target, ok, err = authorizeNotionCommentTarget(
			event, publicTargetID, "comments:write", permissionWorkspaceWrite,
		)
		if err != nil || !ok {
			return err
		}
	} else {
		var root storedComment
		var ok bool
		// A discussion this token can't reach is said as one (seen 2026-11-10).
		noteNotionAskedObject(event, "discussion", notionDashedUUID(input.DiscussionID))
		target, root, ok, err = authorizeNotionDiscussion(
			event, input.DiscussionID, "comments:write",
		)
		if err != nil || !ok {
			return err
		}
		parentID = root.ID
	}
	if notionCommentTargetInTrash(event, target) {
		return notionPageValidationError(event, notionArchivedCommentMessage)
	}
	attachmentIDs, err := notionCommentAttachmentIDs(event.App, target.Identity, input.Attachments)
	if err != nil {
		return notionCommentAPIError(event, err)
	}
	shown, err := notionCommentDisplayName(event.App, target.Identity, input.DisplayName)
	if err != nil {
		return notionCommentAPIError(event, err)
	}
	body, err := notionCommentBodyFromRequest(
		event.App, target.Identity, target.Page, input.RichText,
		notionCommentMarkdownOf(event, input.Markdown, input.MarkdownVersion), attachmentIDs, &shown,
	)
	if err != nil {
		return notionCommentAPIError(event, err)
	}
	commentID := "notion-comment-" + uuid.NewString()
	response, err := createConnectedComment(
		event.Request.Context(), event.App, target.Identity.PrincipalID, target.nativeTarget(), commentCreateInput{
			ID: commentID, RequestID: "notion-comment-request-" + uuid.NewString(),
			BlockID: target.BlockID, ParentID: parentID, Body: body,
		},
		func(txApp core.App) (commentAuthorization, error) {
			return target.authorizeCurrent(txApp, permissionWorkspaceWrite)
		},
	)
	if err != nil {
		return notionCommentAPIError(event, err)
	}
	publicID, err := notionObjectIdentityPublicID(
		event.App, notionObjectIdentityComment, response.Comment.WorkspaceID, response.Comment.ID,
	)
	if err == nil {
		err = recordConnectionAuthorship(event.App, target.Identity, connectionAuthoredComment, response.Comment.ID, true, "")
	}
	if err != nil {
		return notionCommentInternalError(event, err)
	}
	if !notionOAuthHasScope(target.Identity, "comments:read") {
		return event.JSON(http.StatusOK, map[string]any{"object": "comment", "id": publicID})
	}
	row, err := loadCommentForTarget(event.App, target.nativeTarget(), response.Comment.ID)
	if err != nil {
		return notionCommentAPIError(event, err)
	}
	projected, err := notionCommentObject(
		event.App, row, newNotionBlockFileCapabilityIssuer(event.Request, target.Identity), target.Identity,
		&notionCommentOriginals{},
	)
	if err != nil {
		return notionCommentAPIError(event, err)
	}
	return event.JSON(http.StatusOK, projected)
}

func notionCommentParentID(parent notionCommentParentRequest) (string, error) {
	pageID, blockID := strings.TrimSpace(parent.PageID), strings.TrimSpace(parent.BlockID)
	if (pageID == "") == (blockID == "") ||
		parent.Type != "" && parent.Type != "page_id" && parent.Type != "block_id" ||
		parent.Type == "page_id" && pageID == "" || parent.Type == "block_id" && blockID == "" {
		return "", notionInvalid(errCommentInvalid, "parent takes one of page_id or block_id, with its type.")
	}
	if pageID != "" {
		return pageID, nil
	}
	return blockID, nil
}

// notionCommentAttachmentIDs is the files a new comment's attachments name,
// in their order, as Notion takes them (seen 2026-11-11): its request shape
// has checked how many and of what type; an ID that isn't one is said so,
// one it can't find or not yet uploaded in the words a block's file says
// (notionFileUploadMissing, notionFileUploadUnsent), and the same upload
// twice is two attachments.
func notionCommentAttachmentIDs(
	app core.App,
	identity notionOAuthBearerIdentity,
	attachments []notionCommentAttachmentRequest,
) ([]string, error) {
	ids := make([]string, 0, len(attachments))
	for _, attachment := range attachments {
		if _, valid := normalizeNotionUUIDRequest(attachment.FileUploadID); !valid {
			return nil, notionInvalid(errCommentInvalid, "Invalid file upload ID: "+attachment.FileUploadID)
		}
		file, err := resolveNotionCompletedFileUpload(app, identity, attachment.FileUploadID)
		if err != nil {
			return nil, err
		}
		ids = append(ids, file.FileID)
	}
	return ids, nil
}

func serveNotionCommentUpdate(event *core.RequestEvent, publicCommentID string) error {
	target, row, ok, err := authorizeNotionComment(
		event, publicCommentID, "comments:write", permissionWorkspaceWrite,
	)
	if err != nil || !ok {
		return err
	}
	if notionCommentTargetInTrash(event, target) {
		return notionPageValidationError(event, notionArchivedCommentMessage)
	}
	existingBody, err := decodeNotionStoredCommentBody(row.BodyJSON)
	if err != nil || !notionCommentMetadataOwnedBy(existingBody, target.Identity) {
		return notionCommentNotFound(event)
	}
	input := notionCommentUpdateRequest{}
	if err := bindNotionStrictJSON(event, &input); err != nil {
		return notionBindRefused(event, err)
	}
	body, err := notionCommentBodyFromRequest(
		event.App, target.Identity, target.Page, input.RichText,
		notionCommentMarkdownOf(event, input.Markdown, input.MarkdownVersion),
		existingBody.AttachmentIDs, existingBody.Notion,
	)
	if err != nil {
		return notionCommentAPIError(event, err)
	}
	_, err = updateCommentRecordForTarget(
		event.App, target.Identity.PrincipalID, target.nativeTarget(), row.ID, commentUpdateInput{
			RequestID:        "notion-comment-request-" + uuid.NewString(),
			ExpectedRevision: row.Revision, Body: body,
		},
		func(txApp core.App) (commentAuthorization, error) {
			return target.authorizeCurrent(txApp, permissionWorkspaceWrite)
		},
	)
	if err != nil {
		return notionCommentAPIError(event, err)
	}
	publicID, err := notionObjectIdentityPublicID(
		event.App, notionObjectIdentityComment, row.WorkspaceID, row.ID,
	)
	if err != nil {
		return notionCommentInternalError(event, err)
	}
	if !notionOAuthHasScope(target.Identity, "comments:read") {
		return event.JSON(http.StatusOK, map[string]any{"object": "comment", "id": publicID})
	}
	updated, err := loadCommentForTarget(event.App, target.nativeTarget(), row.ID)
	if err != nil {
		return notionCommentAPIError(event, err)
	}
	projected, err := notionCommentObject(
		event.App, updated, newNotionBlockFileCapabilityIssuer(event.Request, target.Identity), target.Identity,
		&notionCommentOriginals{},
	)
	if err != nil {
		return notionCommentAPIError(event, err)
	}
	return event.JSON(http.StatusOK, projected)
}

func serveNotionCommentDelete(event *core.RequestEvent, publicCommentID string) error {
	target, row, ok, err := authorizeNotionComment(
		event, publicCommentID, "comments:write", permissionWorkspaceWrite,
	)
	if err != nil || !ok {
		return err
	}
	if notionCommentTargetInTrash(event, target) {
		return notionPageValidationError(event, notionArchivedCommentMessage)
	}
	body, err := decodeNotionStoredCommentBody(row.BodyJSON)
	if err != nil || !notionCommentMetadataOwnedBy(body, target.Identity) {
		return notionCommentNotFound(event)
	}
	deletedAt := time.Now().UTC().Format(time.RFC3339Nano)
	response, err := notionCommentDeleteAnswer(event, target, row, publicCommentID, deletedAt)
	if err != nil {
		return notionCommentAPIError(event, err)
	}
	if err := deleteNotionCommentRecord(event.App, target, row, deletedAt); err != nil {
		return notionCommentAPIError(event, err)
	}
	return event.JSON(http.StatusOK, response)
}

// deleteNotionCommentRecord deletes a comment a connection made, keeping
// what stays of it and its replies in their discussion
// (deleteCommentKeepingReplies); a discussion that lost its first comment
// keeps its marks (moveCommentThreadMarks).
func deleteNotionCommentRecord(app core.App, target notionCommentTarget, comment storedComment, deletedAt string) error {
	authorize := func(txApp core.App) (commentAuthorization, error) {
		authorization, err := target.authorizeCurrent(txApp, permissionWorkspaceWrite)
		if err == nil && authorization.Page.Workspace.WorkspaceID != target.Identity.WorkspaceID {
			return commentAuthorization{}, permissionDenied()
		}
		return authorization, err
	}
	var handover commentHandover
	var handedOver bool
	err := app.RunInTransaction(func(txApp core.App) error {
		if _, err := authorize(txApp); err != nil {
			return err
		}
		current, err := loadCommentForTarget(txApp, target.nativeTarget(), comment.ID)
		if err != nil {
			return err
		}
		currentBody, err := decodeNotionStoredCommentBody(current.BodyJSON)
		if err != nil || !notionCommentMetadataOwnedBy(currentBody, target.Identity) {
			return errCommentNotFound
		}
		handover, handedOver, err = deleteCommentKeepingReplies(txApp, current, deletedAt)
		return err
	})
	if err == nil && handedOver {
		moveCommentThreadMarks(app, target.Identity.PrincipalID, handover, authorize)
	}
	return err
}

func notionCommentAPIError(event *core.RequestEvent, err error) error {
	mentionRefusal, requestError := &notionMentionRefusal{}, &graphRequestError{}
	switch {
	// A mention or a link Notion refuses is said alone, as a title's is: a
	// person it can't find with 404 (seen 2026-11-11).
	case errors.As(err, &mentionRefusal) && mentionRefusal.missingUser != "":
		return notionCompatibilityError(event, http.StatusNotFound, notionErrorObjectNotFound, notionUserNotFoundMessage(mentionRefusal.missingUser))
	case errors.As(err, &mentionRefusal):
		return notionPageValidationError(event, mentionRefusal.said())
	// A file upload's refusal, in Leafen's words (notionFileUploadMissing).
	case errors.As(err, &requestError) && requestError.status == http.StatusBadRequest:
		return notionPageValidationError(event, requestError.message)
	case errors.Is(err, errCommentInvalid), errors.Is(err, errCommentParentScope),
		errors.Is(err, errCommentBlockScope):
		return notionPageValidationError(event, notionCommentRefusalReason(err))
	case isDLPPolicyDecisionError(err):
		return notionDLPPolicyError(event, err)
	case errors.Is(err, errCommentNotFound), isPermissionDenied(err):
		return notionCommentNotFound(event)
	case errors.Is(err, errCommentRevisionConflict), errors.Is(err, errCommentSnapshotChanged),
		errors.Is(err, errCommentDeleteBusy):
		return notionCompatibilityError(
			event, http.StatusConflict, "conflict_error", "The comment changed while the request was processed.",
		)
	default:
		return notionCommentInternalError(event, err)
	}
}

func notionCommentNotFound(event *core.RequestEvent) error {
	return notionCompatibilityError(
		event, http.StatusNotFound, notionErrorObjectNotFound,
		"There's no comment with the requested ID.",
	)
}

func notionCommentInternalError(event *core.RequestEvent, cause error) error {
	return notionRequestNotCompleted(event, cause)
}

// notionCommentRefusalReason says which value refused a comment
// (2026-10-16: each refusal read "The comment request is invalid.").
func notionCommentRefusalReason(err error) string {
	switch {
	case errors.Is(err, errCommentParentScope):
		return "The discussion isn't on this page."
	case errors.Is(err, errCommentBlockScope):
		return "The block isn't on this page."
	}
	return notionAdminRefusalReason(err, "The comment request is invalid.")
}
