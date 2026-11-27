package app

import (
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

func registerCommentRoutes(event *core.ServeEvent, ygo *pocketBaseYgoPersistence) {
	event.Router.Route(commentListMethod, apiRoot+commentListPath, commentsOfRowPage(listComments, listDatabaseRowComments))
	event.Router.Route(commentCreateMethod, apiRoot+commentCreatePath, commentsOfRowPage(
		refuseCommentWritesInTrash(func(event *core.RequestEvent) error { return createComment(event, ygo) }),
		refuseCommentWritesInTrash(func(event *core.RequestEvent) error { return createDatabaseRowComment(event, ygo) }),
	)).Bind(apis.BodyLimit(commentBodyBytes + 8192))
	event.Router.Route(commentUpdateMethod, apiRoot+commentUpdatePath, commentsOfRowPage(
		refuseCommentWritesInTrash(updateComment), refuseCommentWritesInTrash(updateDatabaseRowComment),
	)).Bind(apis.BodyLimit(commentBodyBytes + 8192))
	event.Router.Route(commentDeleteMethod, apiRoot+commentDeletePath, commentsOfRowPage(
		refuseCommentWritesInTrash(deleteComment), refuseCommentWritesInTrash(deleteDatabaseRowComment),
	)).Bind(apis.BodyLimit(8192))
	event.Router.Route(
		commentDeletionReadMethod, apiRoot+commentDeletionReadPath, readCommentDeletionRoute,
	)
	event.Router.Route(
		commentDeletionContinueMethod, apiRoot+commentDeletionContinuePath, continueCommentDeletionRoute,
	).Bind(apis.BodyLimit(1024))
	registerDatabaseRowCommentRoutes(event, ygo)
	registerPropertyDiscussionRoutes(event)
	registerCommentReactionRoutes(event)
	registerCommentThreadStateRoutes(event)
	registerCommentLinkRoutes(event)
	registerPageMentionReachRoutes(event)
}

// commentActor is the person signed in or through their key; ok false when
// it answered 401 instead, err then that answer's write.
func commentActor(event *core.RequestEvent) (*core.Record, bool, error) {
	user, ok := authenticatedUser(event)
	if !ok {
		return nil, false, apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	return user, true, nil
}

func commentAPIError(
	event *core.RequestEvent, err error, storageOperation persistentWriteStorageOperation,
) error {
	if storageError, ok := persistentWriteStorageAPIError(
		event, err, storageOperation, "transaction",
	); ok {
		return storageError
	}
	switch {
	case isGuestMFARequired(err):
		return guestMFAAPIError(event)
	case isDLPPolicyDecisionError(err):
		return writeDLPPolicyError(event, err, "comment")
	case isPermissionDenied(err):
		return apiError(event, http.StatusForbidden, errorCodePermissionDenied, "You don't have permission to change comments on this page.")
	case errors.As(err, new(*commentBodyShapeError)):
		var shape *commentBodyShapeError
		errors.As(err, &shape)
		return apiError(event, http.StatusBadRequest, errorCodeCommentBodyShape,
			`A comment's body is {"rich": [{"text": "…"}]}: `+shape.reason+".")
	case errors.Is(err, errCommentInvalid):
		return apiError(event, http.StatusBadRequest, errorCodeCommentInvalid, refusalMessage("Check the comment request.", refusalReason(err)))
	case errors.Is(err, errCommentParentScope):
		return apiError(event, http.StatusBadRequest, errorCodeCommentParentScope, "The reply's parent comment isn't on this page.")
	case errors.Is(err, errCommentBlockScope):
		return apiError(event, http.StatusBadRequest, errorCodeCommentBlockScope, "The comment block isn't on this page.")
	case errors.Is(err, errCommentNotFound):
		return apiError(event, http.StatusNotFound, errorCodeCommentNotFound, "Comment not found.")
	case errors.Is(err, errCommentIDConflict):
		return apiError(event, http.StatusConflict, errorCodeCommentIdConflict, "A comment ID that was already used can't be reused.")
	case errors.Is(err, errCommentRequestConflict):
		return apiError(event, http.StatusConflict, errorCodeCommentRequestConflict, "The same comment request ID can't be used for a different operation.")
	case errors.Is(err, errCommentRevisionConflict):
		return apiError(event, http.StatusConflict, errorCodeCommentRevisionConflict, "The comment already changed. Load the latest comments again.")
	case errors.Is(err, errCommentSnapshotChanged):
		return apiError(event, http.StatusConflict, errorCodeCommentSnapshotChanged, "The comment list changed. Load it again from the start.")
	case errors.Is(err, errCommentDeleteBusy):
		return apiError(event, http.StatusConflict, errorCodeCommentDeleteBusy, "The comment thread is being deleted. Try again after it finishes.")
	default:
		return apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodeCommentFailed,
			"Couldn't process the comment.", "comment", "transaction", errorCodeCommentFailed, err,
		)
	}
}

func listComments(event *core.RequestEvent) error {
	user, ok, err := commentActor(event)
	if !ok {
		return err
	}
	response, err := listCommentRecords(
		event.App, user.Id, strings.TrimSpace(event.Request.PathValue("pageId")),
		event.Request.URL.Query().Get("limit"), event.Request.URL.Query().Get("cursor"),
	)
	if err != nil {
		return commentAPIError(event, err, persistentReadComment)
	}
	// What names a page closed to this reader (content_graph_reader_mentions.go).
	commentsAsRead(event.App, user.Id, response.Items)
	return event.JSON(http.StatusOK, response)
}

// createComment stores a new comment and marks the text a new thread was
// left on (comment_text_anchors.go).
func createComment(event *core.RequestEvent, ygo *pocketBaseYgoPersistence) error {
	user, ok, err := commentActor(event)
	if !ok {
		return err
	}
	input := commentCreateInput{}
	if err := event.BindBody(&input); err != nil {
		return commentAPIError(event, commentBodyRefused("comment request", err), persistentWriteComment)
	}
	pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	if err := refuseConnectionCommentPart(input.Body); err != nil {
		return commentAPIError(event, err, persistentWriteComment)
	}
	response, err := createConnectedPageComment(event.Request.Context(), event.App, user.Id, pageID, input)
	if err != nil {
		return commentAPIError(event, err, persistentWriteComment)
	}
	anchorNewCommentText(event, ygo, user.Id, response, func(app core.App) (commentAuthorization, error) {
		return authorizeCommentPage(app, user.Id, pageID, permissionWorkspaceWrite, "")
	})
	status := http.StatusCreated
	if response.Duplicate {
		status = http.StatusOK
	}
	return event.JSON(status, response)
}

func updateComment(event *core.RequestEvent) error {
	user, ok, err := commentActor(event)
	if !ok {
		return err
	}
	input := commentUpdateInput{}
	if err := event.BindBody(&input); err != nil {
		return commentAPIError(event, commentBodyRefused("comment edit", err), persistentWriteComment)
	}
	response, err := updateCommentRecord(
		event.App, user.Id, strings.TrimSpace(event.Request.PathValue("pageId")),
		strings.TrimSpace(event.Request.PathValue("commentId")), input,
	)
	if err != nil {
		return commentAPIError(event, err, persistentWriteComment)
	}
	return event.JSON(http.StatusOK, response)
}

func deleteComment(event *core.RequestEvent) error {
	user, ok, err := commentActor(event)
	if !ok {
		return err
	}
	input := commentDeleteInput{}
	if err := event.BindBody(&input); err != nil {
		return commentAPIError(event, commentBodyRefused("comment deletion", err), persistentWriteComment)
	}
	response, err := deleteCommentRecord(
		event.App, user.Id, strings.TrimSpace(event.Request.PathValue("pageId")),
		strings.TrimSpace(event.Request.PathValue("commentId")), input,
	)
	if err != nil {
		return commentAPIError(event, err, persistentWriteComment)
	}
	status := http.StatusOK
	if !response.Duplicate && response.Deletion.Status == commentDeletionStatusPending {
		status = http.StatusAccepted
	}
	return event.JSON(status, response)
}

func readCommentDeletionRoute(event *core.RequestEvent) error {
	user, ok, err := commentActor(event)
	if !ok {
		return err
	}
	response, err := readCommentDeletion(
		event.App, user.Id, strings.TrimSpace(event.Request.PathValue("pageId")),
		strings.TrimSpace(event.Request.PathValue("jobId")),
	)
	if err != nil {
		if errors.Is(err, errCommentDeletionMissing) {
			return apiError(event, http.StatusNotFound, errorCodeJobNotFound, "Comment deletion not found.")
		}
		return commentAPIError(event, err, persistentReadComment)
	}
	return event.JSON(http.StatusOK, response)
}

func continueCommentDeletionRoute(event *core.RequestEvent) error {
	user, ok, err := commentActor(event)
	if !ok {
		return err
	}
	response, err := continueCommentDeletion(
		event.App, user.Id, strings.TrimSpace(event.Request.PathValue("pageId")),
		strings.TrimSpace(event.Request.PathValue("jobId")),
	)
	if err != nil {
		if errors.Is(err, errCommentDeletionMissing) {
			return apiError(event, http.StatusNotFound, errorCodeJobNotFound, "Comment deletion not found.")
		}
		return commentAPIError(event, err, persistentWriteComment)
	}
	status := http.StatusOK
	if response.Deletion.Status == commentDeletionStatusPending {
		status = http.StatusAccepted
	}
	return event.JSON(status, response)
}
