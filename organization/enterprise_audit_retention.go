package organization

import (
	"errors"
	"net/http"
	"strings"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

func registerEnterpriseAuditRetentionRoutes(event *core.ServeEvent) {
	event.Router.GET(leafen.APIRoot+leafen.AuditRetentionGetPath, getOrganizationAuditRetentionRoute)
	event.Router.PUT(leafen.APIRoot+leafen.AuditRetentionUpdatePath, updateOrganizationAuditRetentionRoute).
		Bind(apis.BodyLimit(leafen.AuditRetentionMaxBodyBytes))
}

func getOrganizationAuditRetentionRoute(event *core.RequestEvent) error {
	authorization, ok := authorizeAuditRetentionManagement(event)
	if !ok {
		return nil
	}
	response, err := leafen.LoadOrganizationAuditRetentionPolicy(
		event.App, authorization.OrganizationID,
	)
	if err != nil {
		return auditRetentionManagementError(event, err, "read")
	}
	return event.JSON(http.StatusOK, response)
}

func updateOrganizationAuditRetentionRoute(event *core.RequestEvent) error {
	authorization, ok := authorizeAuditRetentionManagement(event)
	if !ok {
		return nil
	}
	input := leafen.AuditRetentionUpdateInput{}
	if err := event.BindBody(&input); err != nil {
		return auditRetentionManagementError(event, leafen.ErrAuditRetentionInvalid, "update-bind")
	}
	// A key's call asks its person only for a change that would be made
	// (internal/app/user_key_acting_ask.go).
	if err := leafen.RehearseBeforeAsking(event, func(txApp core.App) error {
		_, err := updateOrganizationAuditRetentionPolicy(txApp, authorization, input.RetentionDays)
		return err
	}); err != nil {
		return auditRetentionManagementError(event, err, "update")
	}
	if asked, err := leafen.AskBeforeActing(event); asked {
		return err
	}
	response, err := updateOrganizationAuditRetentionPolicy(event.App, authorization, input.RetentionDays)
	if err != nil {
		return auditRetentionManagementError(event, err, "update")
	}
	return event.JSON(http.StatusOK, response)
}

func authorizeAuditRetentionManagement(
	event *core.RequestEvent,
) (leafen.OrganizationAuthorization, bool) {
	user, ok := leafen.AuthenticatedUser(event)
	if !ok {
		_ = leafen.APIError(event, http.StatusUnauthorized, leafen.ErrorCodeUnauthorized, "Login required.")
		return leafen.OrganizationAuthorization{}, false
	}
	organizationID := strings.TrimSpace(event.Request.PathValue("organizationId"))
	authorization, err := leafen.AuthorizeOrganization(
		event.App, user.Id, organizationID, leafen.PermissionOrganizationAuditLog,
	)
	if leafen.IsPermissionDenied(err) {
		_ = leafen.APIError(event, http.StatusNotFound, leafen.ErrorCodeOrganizationNotFound, "Organization not found.")
		return leafen.OrganizationAuthorization{}, false
	}
	if err != nil {
		_ = leafen.APIErrorWithCause(
			event, http.StatusInternalServerError, leafen.ErrorCodeOrganizationAuthorityInvalid,
			"Couldn't check organization permission.", "enterprise_audit_retention", "authorization",
			leafen.ErrorCodeOrganizationAuthorityInvalid, err,
		)
		return leafen.OrganizationAuthorization{}, false
	}
	return authorization, true
}

func auditRetentionManagementError(event *core.RequestEvent, err error, stage string) error {
	status, code, message := http.StatusInternalServerError,
		leafen.ErrorCodeAuditRetentionUnavailable, "Couldn't process the audit retention policy."
	if errors.Is(err, leafen.ErrAuditRetentionInvalid) {
		status, code, message = http.StatusBadRequest,
			leafen.ErrorCodeAuditRetentionInvalid, "Check the audit retention period."
	} else if leafen.IsPermissionDenied(err) {
		status, code, message = http.StatusNotFound,
			leafen.ErrorCodeOrganizationNotFound, "Organization not found."
	}
	return leafen.APIErrorWithCause(
		event, status, code, message, "enterprise_audit_retention", stage, code, err,
	)
}
