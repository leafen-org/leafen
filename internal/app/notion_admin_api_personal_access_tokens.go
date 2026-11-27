package app

import (
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

var (
	errNotionAdminPATInvalid   = errors.New("invalid Notion Admin personal access token request")
	errNotionAdminPATForbidden = notionAdminInvalid(errors.New("Notion Admin personal access token request forbidden"), notionAdminScopeChangedReason)
	errNotionAdminPATNotFound  = errors.New("Notion Admin personal access token target not found")
)

func notionAdminSpacePersonalAccessTokenIDs(path string) (string, string, bool) {
	value, found := strings.CutPrefix(path, notionAdminAPISpacesPath+"/")
	parts := strings.Split(value, "/")
	if !found || len(parts) < 2 || len(parts) > 3 || parts[0] == "" ||
		parts[1] != "personal_access_tokens" {
		return "", "", false
	}
	if len(parts) == 2 {
		return parts[0], "", true
	}
	if parts[2] == "" {
		return "", "", false
	}
	return parts[0], parts[2], true
}

func serveNotionAdminPersonalAccessTokens(
	event *core.RequestEvent,
	rawWorkspaceID, rawTokenID string,
) (bool, error) {
	if rawTokenID == "" && event.Request.Method != http.MethodGet ||
		rawTokenID != "" && event.Request.Method != http.MethodDelete {
		return false, nil
	}
	secret := bearerToken(event.Request)
	release := acquireNotionAdminTokenLifecycleLock(notionAdminTokenHash(secret))
	defer release()
	capability := notionAdminCapabilityPersonalAccessTokenRead
	if rawTokenID != "" {
		capability = notionAdminCapabilityPersonalAccessTokenWriteHighImpact
	}
	identity, authorized := authorizeNotionAdminAPI(event, secret, capability)
	if !authorized {
		return true, nil
	}
	workspaceID, err := resolveNotionCompatibleWorkspaceID(strings.TrimSpace(rawWorkspaceID))
	if err != nil {
		return true, notionAdminPersonalAccessTokenError(event, notionAdminInvalid(errNotionAdminPATInvalid, "The workspace ID should be a UUID."))
	}
	if !identity.CanAccessWorkspace(workspaceID) {
		return true, notionAdminPersonalAccessTokenError(event, errNotionAdminPATNotFound)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	if rawTokenID == "" {
		return true, serveNotionAdminPersonalAccessTokenList(
			event, secret, identity, workspaceID,
		)
	}
	tokenID, err := resolveNotionPersonalAccessTokenID(strings.TrimSpace(rawTokenID))
	if err != nil {
		return true, notionAdminPersonalAccessTokenError(event, notionAdminInvalid(errNotionAdminPATInvalid, "The token ID isn't a personal access token ID."))
	}
	return true, serveNotionAdminPersonalAccessTokenRevoke(
		event, secret, identity, workspaceID, tokenID,
	)
}

func serveNotionAdminPersonalAccessTokenList(
	event *core.RequestEvent,
	secret string,
	identity notionAdminIdentity,
	workspaceID string,
) error {
	if event.Request.ContentLength > 0 {
		return notionAdminPersonalAccessTokenError(event, notionAdminInvalid(errNotionAdminPATInvalid, "This request takes no body."))
	}
	pageSize, rawCursor, filters, err := notionAdminPATListQuery(event.Request)
	if err != nil {
		return notionAdminPersonalAccessTokenError(event, err)
	}
	cursor, err := decodeNotionAdminPATCursor(rawCursor, secret, identity, workspaceID, filters)
	if err != nil {
		return notionAdminPersonalAccessTokenError(event, err)
	}
	response, err := listNotionAdminPersonalAccessTokens(
		event.Request.Context(), event.App, secret, identity, workspaceID, cursor, filters, pageSize,
	)
	if err != nil {
		return notionAdminPersonalAccessTokenError(event, err)
	}
	return event.JSON(http.StatusOK, response)
}

func serveNotionAdminPersonalAccessTokenRevoke(
	event *core.RequestEvent,
	secret string,
	identity notionAdminIdentity,
	workspaceID, tokenID string,
) error {
	if event.Request.ContentLength > 0 || len(event.Request.URL.Query()) != 0 {
		return notionAdminPersonalAccessTokenError(event, notionAdminInvalid(errNotionAdminPATInvalid, "This request takes no query parameters and no body."))
	}
	if err := revokeNotionAdminPersonalAccessToken(
		event.Request.Context(), event.App, secret, identity, workspaceID, tokenID,
	); err != nil {
		return notionAdminPersonalAccessTokenError(event, err)
	}
	return event.JSON(http.StatusOK, map[string]any{})
}

func notionAdminPersonalAccessTokenError(event *core.RequestEvent, err error) error {
	switch {
	case errors.Is(err, errNotionAdminTokenUnauthorized):
		return notionAdminAPIError(
			event, http.StatusUnauthorized, notionAdminErrorUnauthorized,
			notionAdminUnauthorizedMessage, err,
		)
	case errors.Is(err, errNotionAdminPATForbidden):
		return notionAdminAPIError(
			event, http.StatusForbidden, notionAdminErrorForbidden,
			notionAdminRefusalReason(err, notionAdminForbiddenMessage), err,
		)
	case errors.Is(err, errNotionAdminPATInvalid), errors.Is(err, errInvalidNotionAdminPATCursor):
		return notionAdminAPIError(
			event, http.StatusBadRequest, notionAdminErrorValidation,
			notionAdminValidationReason(err), err,
		)
	case errors.Is(err, errNotionAdminPATNotFound):
		return notionAdminAPIError(
			event, http.StatusNotFound, notionAdminErrorNotFound,
			notionAdminNotFoundMessage, err,
		)
	default:
		return notionAdminAPIError(
			event, http.StatusInternalServerError, notionAdminErrorInternal,
			notionAdminInternalErrorMessage, err,
		)
	}
}
