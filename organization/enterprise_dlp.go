package organization

import (
	"errors"
	"net/http"
	"strings"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

func registerEnterpriseDLPRoutes(event *core.ServeEvent) {
	event.Router.GET(leafen.APIRoot+"/organizations/{organizationId}/dlp", getDLPPolicy)
	event.Router.PUT(leafen.APIRoot+"/organizations/{organizationId}/dlp", updateDLPPolicy).
		Bind(apis.BodyLimit(leafen.DLPMaxBodyBytes))
}

func getDLPPolicy(event *core.RequestEvent) error {
	authorization, ok := authorizeDLPManagement(event)
	if !ok {
		return nil
	}
	policy, err := loadOrganizationDLPPolicy(event.App, authorization.OrganizationID)
	if err != nil {
		return dlpManagementError(event, err, "policy-read")
	}
	return event.JSON(http.StatusOK, policy)
}

func updateDLPPolicy(event *core.RequestEvent) error {
	authorization, ok := authorizeDLPManagement(event)
	if !ok {
		return nil
	}
	input := leafen.DLPPolicyInput{}
	if err := event.BindBody(&input); err != nil {
		return dlpManagementError(event, leafen.ErrDLPPolicyInvalid, "policy-bind")
	}
	// A key's call asks its person only for a change that would be made
	// (internal/app/user_key_acting_ask.go).
	if err := leafen.RehearseBeforeAsking(event, func(txApp core.App) error {
		_, err := saveOrganizationDLPPolicy(txApp, authorization, input)
		return err
	}); err != nil {
		return dlpManagementError(event, err, "policy-write")
	}
	if asked, err := leafen.AskBeforeActing(event); asked {
		return err
	}
	policy, err := saveOrganizationDLPPolicy(event.App, authorization, input)
	if err != nil {
		return dlpManagementError(event, err, "policy-write")
	}
	return event.JSON(http.StatusOK, policy)
}

func authorizeDLPManagement(event *core.RequestEvent) (leafen.OrganizationAuthorization, bool) {
	user, ok := leafen.AuthenticatedUser(event)
	if !ok {
		_ = leafen.APIError(event, http.StatusUnauthorized, leafen.ErrorCodeUnauthorized, "Login required.")
		return leafen.OrganizationAuthorization{}, false
	}
	organizationID := strings.TrimSpace(event.Request.PathValue("organizationId"))
	authorization, err := leafen.AuthorizeOrganization(
		event.App, user.Id, organizationID, leafen.PermissionOrganizationSecurityAdmin,
	)
	if leafen.IsPermissionDenied(err) {
		_ = leafen.APIError(event, http.StatusNotFound, leafen.ErrorCodeOrganizationNotFound, "Organization not found.")
		return leafen.OrganizationAuthorization{}, false
	}
	if err != nil {
		_ = leafen.APIErrorWithCause(
			event, http.StatusInternalServerError, leafen.ErrorCodeOrganizationAuthorityInvalid,
			"Couldn't check organization permission.", "enterprise_dlp", "management-authorization",
			leafen.ErrorCodeOrganizationAuthorityInvalid, err,
		)
		return leafen.OrganizationAuthorization{}, false
	}
	return authorization, true
}

func dlpManagementError(event *core.RequestEvent, err error, stage string) error {
	if storageError, ok := leafen.DLPStorageAPIError(event, err, stage); ok {
		return storageError
	}
	status := http.StatusServiceUnavailable
	code := leafen.ErrorCodeDLPUnavailable
	message := "Couldn't process the DLP policy."
	if errors.Is(err, leafen.ErrDLPPolicyInvalid) {
		status, code, message = http.StatusBadRequest, leafen.ErrorCodeDLPPolicyInvalid, "Check the DLP policy."
	} else if leafen.IsPermissionDenied(err) {
		status, code, message = http.StatusNotFound, leafen.ErrorCodeOrganizationNotFound, "Organization not found."
	}
	return leafen.APIErrorWithCause(event, status, code, message, "enterprise_dlp", stage, code, err)
}
