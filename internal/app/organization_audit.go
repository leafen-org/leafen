package app

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// The audit log reads in the core; its exports belong to the organization
// module, which registers their routes.
func registerOrganizationAuditRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+organizationAuditEventListPath, listOrganizationAuditEventsRoute)
	// An account's history and who sees what of it (account_history.go).
	event.Router.GET(apiRoot+accountHistoryReadPath, readAccountHistoryRoute)
	event.Router.GET(apiRoot+accountHistoryPolicyReadPath, readAccountHistoryPolicyRoute)
	event.Router.PUT(apiRoot+accountHistoryPolicyUpdatePath, updateAccountHistoryPolicyRoute)
	// Whom page analytics names as having viewed (view_display_policy.go).
	event.Router.GET(apiRoot+viewDisplayPolicyReadPath, readViewDisplayPolicyRoute)
	event.Router.PUT(apiRoot+viewDisplayPolicyUpdatePath, updateViewDisplayPolicyRoute)
}

func listOrganizationAuditEventsRoute(event *core.RequestEvent) error {
	authorization, ok := authorizeOrganizationAudit(event)
	if !ok {
		return nil
	}
	filter, err := organizationAuditFilterFromRequest(event, organizationAuditEventsPerPage)
	if err != nil {
		return organizationAuditError(event, err, "events-query")
	}
	// Reads gathered in the last second show too (read_log_buffer.go).
	flushReadLog(event.App)
	response, err := listOrganizationAuditEvents(
		event.App, authorization.OrganizationID, filter, organizationAuditEventsPerPage,
	)
	if err != nil {
		return organizationAuditError(event, err, "events-list")
	}
	return event.JSON(http.StatusOK, response)
}

func listOrganizationAuditExportsRoute(event *core.RequestEvent) error {
	authorization, ok := authorizeOrganizationAudit(event)
	if !ok {
		return nil
	}
	limit, err := organizationAuditQueryLimit(event, organizationAuditExportsPerPage)
	if err != nil {
		return organizationAuditError(event, err, "exports-query")
	}
	response, err := listOrganizationAuditExports(
		event.App, authorization.OrganizationID,
		strings.TrimSpace(event.Request.URL.Query().Get("cursor")), limit,
	)
	if err != nil {
		return organizationAuditError(event, err, "exports-list")
	}
	return event.JSON(http.StatusOK, response)
}

func organizationAuditFilterFromRequest(
	event *core.RequestEvent,
	maximum int,
) (organizationAuditFilter, error) {
	limit, err := organizationAuditQueryLimit(event, maximum)
	if err != nil {
		return organizationAuditFilter{}, err
	}
	since, err := organizationAuditQueryTimestamp(event, "since")
	if err != nil {
		return organizationAuditFilter{}, err
	}
	until, err := organizationAuditQueryTimestamp(event, "until")
	if err != nil {
		return organizationAuditFilter{}, err
	}
	return organizationAuditFilter{
		Cursor:     strings.TrimSpace(event.Request.URL.Query().Get("cursor")),
		Action:     strings.TrimSpace(event.Request.URL.Query().Get("action")),
		TargetType: strings.TrimSpace(event.Request.URL.Query().Get("targetType")),
		Since:      since, Until: until, Limit: limit,
		Actor:  strings.TrimSpace(event.Request.URL.Query().Get("actor")),
		Target: strings.TrimSpace(event.Request.URL.Query().Get("target")),
	}, nil
}

func organizationAuditQueryLimit(event *core.RequestEvent, maximum int) (int, error) {
	raw := strings.TrimSpace(event.Request.URL.Query().Get("limit"))
	if raw == "" {
		return maximum, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 || value > maximum {
		return 0, refuse(errOrganizationAuditInvalid, "limit is a number from 1 to %d, not %q", maximum, raw)
	}
	return value, nil
}

func organizationAuditQueryTimestamp(event *core.RequestEvent, name string) (int64, error) {
	raw := strings.TrimSpace(event.Request.URL.Query().Get(name))
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		return 0, refuse(errOrganizationAuditInvalid, "%s is a time in milliseconds since 1970, 0 or more, not %q", name, raw)
	}
	return value, nil
}

func authorizeOrganizationAudit(event *core.RequestEvent) (organizationAuthorization, bool) {
	user, ok := authenticatedUser(event)
	if !ok {
		_ = apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
		return organizationAuthorization{}, false
	}
	authorization, err := authorizeOrganization(
		event.App, user.Id, strings.TrimSpace(event.Request.PathValue("organizationId")),
		permissionOrganizationAuditLog,
	)
	if isPermissionDenied(err) {
		_ = apiError(event, http.StatusNotFound, errorCodeOrganizationNotFound, "Organization not found.")
		return organizationAuthorization{}, false
	}
	if err != nil {
		_ = apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodeOrganizationAuthorityInvalid,
			"Couldn't check organization permission.", "organization_audit", "authorization",
			errorCodeOrganizationAuthorityInvalid, err,
		)
		return organizationAuthorization{}, false
	}
	return authorization, true
}

func organizationAuditError(event *core.RequestEvent, err error, stage string) error {
	status, code, message := http.StatusServiceUnavailable,
		errorCodeOrganizationAuditUnavailable, "Couldn't process the organization audit log."
	switch {
	case errors.Is(err, errOrganizationPolicyRevisionChanged):
		status, code, message = http.StatusConflict, errorCodeOrganizationAuditInvalid,
			"The policy changed elsewhere: revision isn't its revision now. Read it again, then send the change with that revision."
	case errors.Is(err, errOrganizationAuditConflict):
		status, code, message = http.StatusConflict,
			errorCodeOrganizationAuditInvalid, "The same change ID can't be used for a different request."
	case errors.Is(err, errOrganizationAuditInvalid):
		status, code, message = http.StatusBadRequest,
			errorCodeOrganizationAuditInvalid, refusalMessage("Check the audit log request.", refusalReason(err))
	case errors.Is(err, errOrganizationAuditNotFound):
		status, code, message = http.StatusNotFound,
			errorCodeOrganizationAuditNotFound, "Audit export not found."
	case errors.Is(err, errOrganizationAuditTooLarge):
		status, code, message = http.StatusRequestEntityTooLarge,
			errorCodeOrganizationAuditTooLarge, "Reduce the audit export size."
	case isPermissionDenied(err):
		status, code, message = http.StatusNotFound,
			errorCodeOrganizationNotFound, "Organization not found."
	}
	return apiErrorWithCause(
		event, status, code, message, "organization_audit", stage, code, err,
	)
}
