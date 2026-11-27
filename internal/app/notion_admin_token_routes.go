package app

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

func registerNotionAdminTokenRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+notionAdminTokenListPath, listNotionAdminTokenRoute)
	event.Router.POST(apiRoot+notionAdminTokenCreatePath, createNotionAdminTokenRoute).
		Bind(apis.BodyLimit(notionAdminTokenMaxBodyBytes))
	event.Router.POST(apiRoot+notionAdminTokenRevokePath, revokeNotionAdminTokenRoute).
		Bind(apis.BodyLimit(notionAdminTokenMaxBodyBytes))
}

func listNotionAdminTokenRoute(event *core.RequestEvent) error {
	authorization, ok := authorizeNotionAdminTokenManagement(event)
	if !ok {
		return nil
	}
	pageSize, err := notionAdminTokenPageSize(event.Request)
	if err != nil {
		return writeNotionAdminTokenError(event, err, "list-page-size")
	}
	response, err := listNotionAdminTokens(
		event.Request.Context(), event.App, authorization, pageSize,
		event.Request.URL.Query().Get("cursor"),
	)
	if err != nil {
		return writeNotionAdminTokenError(event, err, "list")
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, response)
}

func createNotionAdminTokenRoute(event *core.RequestEvent) error {
	authorization, ok := authorizeNotionAdminTokenManagement(event)
	if !ok {
		return nil
	}
	input := notionAdminTokenCreateInput{}
	if err := event.BindBody(&input); err != nil {
		return writeNotionAdminTokenError(event, errNotionAdminTokenInvalid, "create-bind")
	}
	response, err := createNotionAdminToken(
		event.Request.Context(), event.App, authorization, input,
	)
	if err != nil {
		return writeNotionAdminTokenError(event, err, "create")
	}
	status := http.StatusCreated
	if response.Duplicate {
		status = http.StatusOK
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(status, response)
}

func revokeNotionAdminTokenRoute(event *core.RequestEvent) error {
	authorization, ok := authorizeNotionAdminTokenManagement(event)
	if !ok {
		return nil
	}
	input := notionAdminTokenRevokeInput{}
	if err := event.BindBody(&input); err != nil {
		return writeNotionAdminTokenError(event, errNotionAdminTokenInvalid, "revoke-bind")
	}
	tokenID := strings.TrimSpace(event.Request.PathValue("tokenId"))
	// A key's call asks its person only for a token there is (user_key_acting_ask.go).
	if userKeyAsksWhereItActs(event) {
		if strings.TrimSpace(input.RequestID) == "" {
			return writeNotionAdminTokenError(event, errNotionAdminTokenInvalid, "revoke-bind")
		}
		if _, err := findNotionAdminTokenLifecycleKey(event.Request.Context(), event.App, authorization.OrganizationID, tokenID); err != nil {
			return writeNotionAdminTokenError(event, classifyNotionAdminTokenError(err), "revoke")
		}
		if asked, err := askBeforeActing(event); asked {
			return err
		}
	}
	response, err := revokeNotionAdminToken(event.Request.Context(), event.App, authorization, tokenID, input)
	if err != nil {
		return writeNotionAdminTokenError(event, err, "revoke")
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, response)
}

func authorizeNotionAdminTokenManagement(
	event *core.RequestEvent,
) (organizationAuthorization, bool) {
	user, ok := authenticatedUser(event)
	if !ok {
		_ = apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
		return organizationAuthorization{}, false
	}
	authorization, err := authorizeOrganizationContext(
		event.Request.Context(), event.App, user.Id,
		strings.TrimSpace(event.Request.PathValue("organizationId")),
		permissionOrganizationOwner,
	)
	if isPermissionDenied(err) {
		_ = apiError(event, http.StatusNotFound, errorCodeOrganizationNotFound, "Organization not found.")
		return organizationAuthorization{}, false
	}
	if err != nil {
		_ = apiErrorWithCause(
			event, http.StatusServiceUnavailable, errorCodeNotionAdminTokenUnavailable,
			"Couldn't check the Admin API token permission.", "notion_admin_token",
			"authorization", errorCodeNotionAdminTokenUnavailable, err,
		)
		return organizationAuthorization{}, false
	}
	return authorization, true
}

func notionAdminTokenPageSize(request *http.Request) (int, error) {
	raw := strings.TrimSpace(request.URL.Query().Get("pageSize"))
	if raw == "" {
		return notionAdminTokensPerPage, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 || value > notionAdminTokensPerPage {
		return 0, errNotionAdminTokenInvalid
	}
	return value, nil
}

func writeNotionAdminTokenError(event *core.RequestEvent, err error, stage string) error {
	status, code, message := http.StatusServiceUnavailable,
		errorCodeNotionAdminTokenUnavailable, "Couldn't process the Admin API token."
	switch {
	case errors.Is(err, errNotionAdminTokenInvalid):
		status, code, message = http.StatusBadRequest,
			errorCodeNotionAdminTokenInvalid, "Check the Admin API token details."
	case errors.Is(err, errNotionAdminTokenCursorInvalid):
		status, code, message = http.StatusBadRequest,
			errorCodeNotionAdminTokenCursorInvalid, "Check the Admin API token list position."
	case errors.Is(err, errNotionAdminTokenNotFound):
		status, code, message = http.StatusNotFound,
			errorCodeNotionAdminTokenNotFound, "Admin API token not found."
	case errors.Is(err, errNotionAdminTokenRequestReused):
		status, code, message = http.StatusConflict,
			errorCodeNotionAdminTokenRequestReused, "The request ID was used for a different token operation."
	case isPermissionDenied(err):
		status, code, message = http.StatusNotFound,
			errorCodeOrganizationNotFound, "Organization not found."
	}
	return apiErrorWithCause(
		event, status, code, message, "notion_admin_token", stage, code, err,
	)
}
