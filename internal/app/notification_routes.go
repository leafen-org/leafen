package app

import (
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

func registerNotificationRoutes(event *core.ServeEvent) {
	event.Router.Route(
		notificationInboxListMethod, apiRoot+notificationInboxListPath, listNotificationInboxRoute,
	)
	event.Router.Route(
		notificationActivitySyncMethod, apiRoot+notificationActivitySyncPath,
		syncNotificationActivitiesRoute,
	).Bind(apis.BodyLimit(256 * 1024))
	event.Router.Route(
		notificationReadMutateMethod, apiRoot+notificationReadMutatePath,
		mutateNotificationReadRoute,
	).Bind(apis.BodyLimit(16 * 1024))
	event.Router.Route(
		notificationReadGetMethod, apiRoot+notificationReadGetPath,
		getNotificationReadSettlementRoute,
	)
	event.Router.Route(
		notificationReadContinueMethod, apiRoot+notificationReadContinuePath,
		continueNotificationReadSettlementRoute,
	).Bind(apis.BodyLimit(1024))
	event.Router.Route(
		notificationArchiveMutateMethod, apiRoot+notificationArchiveMutatePath,
		archiveNotificationsRoute,
	).Bind(apis.BodyLimit(16 * 1024))
	event.Router.Route(
		notificationStateMutateMethod, apiRoot+notificationStateMutatePath,
		changeNotificationStateRoute,
	).Bind(apis.BodyLimit(16 * 1024))
}

func syncNotificationActivitiesRoute(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	input := notificationActivitySyncInput{}
	if event.BindBody(&input) != nil {
		return notificationSyncAPIError(event, errNotificationSyncInvalid)
	}
	response, err := syncNotificationActivities(
		event.Request.Context(), event.App, user.Id,
		strings.TrimSpace(event.Request.PathValue("workspaceId")), input,
	)
	if err != nil {
		return notificationSyncAPIError(event, err)
	}
	return event.JSON(http.StatusOK, response)
}

func archiveNotificationsRoute(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	input := notificationArchiveInput{}
	if err := event.BindBody(&input); err != nil {
		return notificationArchiveAPIError(event, bodyRefusal(errNotificationArchiveInvalid, "notification archive change", err))
	}
	response, err := settleNotificationArchive(
		event.App, user.Id, strings.TrimSpace(event.Request.PathValue("workspaceId")), input,
	)
	if err != nil {
		return notificationArchiveAPIError(event, err)
	}
	return event.JSON(http.StatusOK, response)
}

func changeNotificationStateRoute(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	input := notificationStateInput{}
	if err := event.BindBody(&input); err != nil {
		return notificationStateAPIError(event, bodyRefusal(errNotificationStateInvalid, "notification state change", err))
	}
	response, err := changeNotificationState(
		event.App, user.Id, strings.TrimSpace(event.Request.PathValue("workspaceId")), input,
	)
	if err != nil {
		return notificationStateAPIError(event, err)
	}
	return event.JSON(http.StatusOK, response)
}

func mutateNotificationReadRoute(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	input := notificationReadInput{}
	if err := event.BindBody(&input); err != nil {
		return notificationReadAPIError(event, bodyRefusal(errNotificationReadInvalid, "notification read change", err), "admission")
	}
	response, err := settleNotificationRead(
		event.App, user.Id, strings.TrimSpace(event.Request.PathValue("workspaceId")), input,
	)
	if err != nil {
		return notificationReadAPIError(event, err, "admission")
	}
	status := http.StatusOK
	if response.Settlement.Status == notificationReadStatusPending {
		status = http.StatusAccepted
	}
	return event.JSON(status, response)
}

func getNotificationReadSettlementRoute(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	response, err := readNotificationReadSettlement(
		event.App, user.Id, strings.TrimSpace(event.Request.PathValue("workspaceId")),
		strings.TrimSpace(event.Request.PathValue("settlementId")),
	)
	if err != nil {
		return notificationReadAPIError(event, err, "read")
	}
	return event.JSON(http.StatusOK, response)
}

func continueNotificationReadSettlementRoute(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	response, err := continueNotificationReadSettlement(
		event.App, user.Id, strings.TrimSpace(event.Request.PathValue("workspaceId")),
		strings.TrimSpace(event.Request.PathValue("settlementId")),
	)
	if err != nil {
		return notificationReadAPIError(event, err, "continuation")
	}
	status := http.StatusOK
	if response.Settlement.Status == notificationReadStatusPending {
		status = http.StatusAccepted
	}
	return event.JSON(status, response)
}

func listNotificationInboxRoute(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	query := event.Request.URL.Query()
	response, err := readNotificationInboxPage(
		event.App, user.Id, strings.TrimSpace(event.Request.PathValue("workspaceId")),
		query.Get("limit"), query.Get("scope"), query.Get("state"), query.Get("cursor"),
	)
	if err != nil {
		return notificationInboxAPIError(event, err)
	}
	return event.JSON(http.StatusOK, response)
}

func notificationInboxAPIError(event *core.RequestEvent, err error) error {
	if storageError, ok := persistentWriteStorageAPIError(
		event, err, persistentReadNotificationInbox, "snapshot",
	); ok {
		return storageError
	}
	switch {
	case isGuestMFARequired(err):
		return guestMFAAPIError(event)
	case isPermissionDenied(err):
		return apiError(
			event, http.StatusForbidden, errorCodePermissionDenied,
			"You don't have permission to view notifications in this workspace.",
		)
	case errors.Is(err, errNotificationInboxInvalid):
		return apiError(
			event, http.StatusBadRequest, errorCodeCommentNotificationInboxInvalid,
			refusalMessage("Check the notification list request.", refusalReason(err)),
		)
	default:
		return apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodeCommentNotificationInboxFailed,
			"Couldn't load the notification list.", "notification-inbox", "snapshot",
			errorCodeCommentNotificationInboxFailed, err,
		)
	}
}

func notificationSyncAPIError(event *core.RequestEvent, err error) error {
	if storageError, ok := persistentWriteStorageAPIError(
		event, err, persistentWriteNotificationSync, "settlement",
	); ok {
		return storageError
	}
	switch {
	case isPermissionDenied(err):
		return apiError(
			event, http.StatusForbidden, errorCodePermissionDenied,
			"You don't have permission to sync notifications in this workspace.",
		)
	case errors.Is(err, errNotificationSyncInvalid):
		return apiError(
			event, http.StatusBadRequest, errorCodeCommentNotificationSyncInvalid,
			refusalMessage("Check the notification sync request.", refusalReason(err)),
		)
	case errors.Is(err, errNotificationSyncTargetChanged):
		return apiError(
			event, http.StatusConflict, errorCodeCommentNotificationSyncTargetChanged,
			"The notification target changed. Try again with the latest page state.",
		)
	default:
		return apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodeCommentNotificationSyncFailed,
			"Couldn't sync the notifications.", "notification-sync", "settlement",
			errorCodeCommentNotificationSyncFailed, err,
		)
	}
}

func notificationReadAPIError(event *core.RequestEvent, err error, stage string) error {
	if storageError, ok := persistentWriteStorageAPIError(
		event, err, persistentWriteNotificationRead, stage,
	); ok {
		return storageError
	}
	switch {
	case isPermissionDenied(err):
		return apiError(
			event, http.StatusForbidden, errorCodePermissionDenied,
			"You don't have permission to change notification read states in this workspace.",
		)
	case errors.Is(err, errNotificationReadInvalid):
		return apiError(
			event, http.StatusBadRequest, errorCodeCommentNotificationReadInvalid,
			refusalMessage("Check the notification read request.", refusalReason(err)),
		)
	case errors.Is(err, errNotificationReadRequestConflict):
		return apiError(
			event, http.StatusConflict, errorCodeCommentNotificationReadRequestConflict,
			"The same request ID can't be used for a different notification read operation.",
		)
	case errors.Is(err, errNotificationReadStateConflict):
		return apiError(
			event, http.StatusConflict, errorCodeCommentNotificationReadStateConflict,
			"The notification read job already progressed. Check its current state again.",
		)
	case errors.Is(err, errNotificationReadNotFound):
		return apiError(
			event, http.StatusNotFound, errorCodeCommentNotificationReadNotFound,
			"Notification read job not found.",
		)
	default:
		return apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodeCommentNotificationReadFailed,
			"Couldn't save the notification read state.", "notification-read", stage,
			errorCodeCommentNotificationReadFailed, err,
		)
	}
}

func notificationArchiveAPIError(event *core.RequestEvent, err error) error {
	if storageError, ok := persistentWriteStorageAPIError(
		event, err, persistentWriteNotificationArchive, "settlement",
	); ok {
		return storageError
	}
	switch {
	case isPermissionDenied(err):
		return apiError(
			event, http.StatusForbidden, errorCodePermissionDenied,
			"You don't have permission to archive notifications in this workspace.",
		)
	case errors.Is(err, errNotificationArchiveInvalid):
		return apiError(
			event, http.StatusBadRequest, errorCodeCommentNotificationArchiveInvalid,
			refusalMessage("Check the notification archive request.", refusalReason(err)),
		)
	case errors.Is(err, errNotificationArchiveRequestConflict):
		return apiError(
			event, http.StatusConflict, errorCodeCommentNotificationArchiveRequestConflict,
			"The same request ID can't be used for a different notification archive operation.",
		)
	default:
		return apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodeCommentNotificationArchiveFailed,
			"Couldn't save the notification archive state.", "notification-archive", "settlement",
			errorCodeCommentNotificationArchiveFailed, err,
		)
	}
}

func notificationStateAPIError(event *core.RequestEvent, err error) error {
	if storageError, ok := persistentWriteStorageAPIError(
		event, err, persistentWriteNotificationState, "settlement",
	); ok {
		return storageError
	}
	switch {
	case isPermissionDenied(err):
		return apiError(
			event, http.StatusForbidden, errorCodePermissionDenied,
			"You don't have permission to change notifications in this workspace.",
		)
	case errors.Is(err, errNotificationStateInvalid):
		return apiError(
			event, http.StatusBadRequest, errorCodeCommentNotificationStateInvalid,
			refusalMessage("Check the notification state request.", refusalReason(err)),
		)
	default:
		return apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodeCommentNotificationStateFailed,
			"Couldn't save the notification state.", "notification-state", "settlement",
			errorCodeCommentNotificationStateFailed, err,
		)
	}
}
