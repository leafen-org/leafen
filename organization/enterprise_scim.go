package organization

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

func registerEnterpriseSCIMRoutes(event *core.ServeEvent, operations *scimOperationRunner) {
	serveProtocol := func(request *core.RequestEvent) error {
		return serveEnterpriseSCIM(request, operations)
	}
	event.Router.GET(leafen.APIRoot+"/organizations/{organizationId}/scim", getSCIMConfiguration)
	event.Router.PUT(leafen.APIRoot+"/organizations/{organizationId}/scim", updateSCIMConfiguration).
		Bind(apis.BodyLimit(leafen.SCIMMaxBodyBytes))
	event.Router.POST(leafen.APIRoot+"/organizations/{organizationId}/scim/tokens", createSCIMTokenRoute).
		Bind(apis.BodyLimit(leafen.SCIMMaxBodyBytes))
	event.Router.DELETE(leafen.APIRoot+"/organizations/{organizationId}/scim/tokens/{tokenId}", revokeSCIMTokenRoute)
	event.Router.GET(leafen.APIRoot+"/scim/v2/{path...}", serveProtocol)
	event.Router.POST(leafen.APIRoot+"/scim/v2/{path...}", serveProtocol).
		Bind(apis.BodyLimit(leafen.SCIMMaxBodyBytes))
	event.Router.PUT(leafen.APIRoot+"/scim/v2/{path...}", serveProtocol).
		Bind(apis.BodyLimit(leafen.SCIMMaxBodyBytes))
	event.Router.PATCH(leafen.APIRoot+"/scim/v2/{path...}", serveProtocol).
		Bind(apis.BodyLimit(leafen.SCIMMaxBodyBytes))
	event.Router.DELETE(leafen.APIRoot+"/scim/v2/{path...}", serveProtocol)
}

func getSCIMConfiguration(event *core.RequestEvent) error {
	authorization, ok := authorizeSCIMManagement(event)
	if !ok {
		return nil
	}
	response, err := loadSCIMConfigurationResponse(event.App, authorization.OrganizationID)
	if err != nil {
		return scimManagementError(event, err, "configuration-read")
	}
	return event.JSON(http.StatusOK, response)
}

func updateSCIMConfiguration(event *core.RequestEvent) error {
	authorization, ok := authorizeSCIMManagement(event)
	if !ok {
		return nil
	}
	input := leafen.SCIMConfigurationInput{}
	if err := event.BindBody(&input); err != nil {
		return scimManagementError(event, errOrganizationDomainNotVerified, "configuration-bind")
	}
	// A key's call asks its person only for a change that would be made
	// (internal/app/user_key_acting_ask.go).
	if err := leafen.RehearseBeforeAsking(event, func(txApp core.App) error {
		_, err := saveSCIMConfiguration(txApp, authorization, input)
		return err
	}); err != nil {
		return scimManagementError(event, err, "configuration-write")
	}
	if asked, err := leafen.AskBeforeActing(event); asked {
		return err
	}
	response, err := saveSCIMConfiguration(event.App, authorization, input)
	if err != nil {
		return scimManagementError(event, err, "configuration-write")
	}
	return event.JSON(http.StatusOK, response)
}

func createSCIMTokenRoute(event *core.RequestEvent) error {
	authorization, ok := authorizeSCIMManagement(event)
	if !ok {
		return nil
	}
	input := leafen.SCIMTokenCreateInput{}
	if err := event.BindBody(&input); err != nil {
		return scimManagementError(event, errOrganizationDomainNotVerified, "token-bind")
	}
	response, err := createSCIMToken(event.App, authorization, input)
	if err != nil {
		return scimManagementError(event, err, "token-create")
	}
	return event.JSON(http.StatusCreated, response)
}

func revokeSCIMTokenRoute(event *core.RequestEvent) error {
	authorization, ok := authorizeSCIMManagement(event)
	if !ok {
		return nil
	}
	err := revokeSCIMToken(
		event.App, authorization, strings.TrimSpace(event.Request.PathValue("tokenId")),
	)
	if err != nil {
		return scimManagementError(event, err, "token-revoke")
	}
	return event.NoContent(http.StatusNoContent)
}

func authorizeSCIMManagement(event *core.RequestEvent) (leafen.OrganizationAuthorization, bool) {
	user, ok := leafen.AuthenticatedUser(event)
	if !ok {
		_ = leafen.APIError(event, http.StatusUnauthorized, leafen.ErrorCodeUnauthorized, "Login required.")
		return leafen.OrganizationAuthorization{}, false
	}
	organizationID := strings.TrimSpace(event.Request.PathValue("organizationId"))
	authorization, err := leafen.AuthorizeOrganization(
		event.App, user.Id, organizationID, leafen.PermissionOrganizationIdentityAdmin,
	)
	if leafen.IsPermissionDenied(err) {
		_ = leafen.APIError(event, http.StatusNotFound, leafen.ErrorCodeOrganizationNotFound, "Organization not found.")
		return leafen.OrganizationAuthorization{}, false
	}
	if err != nil {
		_ = leafen.APIErrorWithCause(
			event, http.StatusInternalServerError, leafen.ErrorCodeOrganizationAuthorityInvalid,
			"Couldn't check organization permission.", "enterprise_scim", "management-authorization",
			leafen.ErrorCodeOrganizationAuthorityInvalid, err,
		)
		return leafen.OrganizationAuthorization{}, false
	}
	return authorization, true
}

func scimManagementError(event *core.RequestEvent, err error, stage string) error {
	status := http.StatusServiceUnavailable
	code := leafen.ErrorCodeSCIMUnavailable
	message := "Couldn't process the SCIM settings."
	switch {
	case errors.Is(err, errOrganizationDomainNotVerified):
		status, code, message = http.StatusBadRequest, leafen.ErrorCodeSCIMConfigurationInvalid, "Check the SCIM settings."
	case errors.Is(err, sql.ErrNoRows):
		status, code, message = http.StatusNotFound, leafen.ErrorCodeSCIMTokenNotFound, "SCIM token not found."
	}
	return leafen.APIErrorWithCause(event, status, code, message, "enterprise_scim", stage, code, err)
}
