package organization

import (
	"errors"
	"net/http"
	"strings"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

func registerEnterpriseLegalHoldRoutes(event *core.ServeEvent) {
	event.Router.GET(leafen.APIRoot+leafen.LegalHoldListPath, listOrganizationLegalHolds)
	event.Router.POST(leafen.APIRoot+leafen.LegalHoldCreatePath, createOrganizationLegalHoldRoute).
		Bind(apis.BodyLimit(leafen.LegalHoldMaxBodyBytes))
	event.Router.POST(leafen.APIRoot+leafen.LegalHoldReleasePath, releaseOrganizationLegalHoldRoute).
		Bind(apis.BodyLimit(leafen.LegalHoldMaxBodyBytes))
}

func listOrganizationLegalHolds(event *core.RequestEvent) error {
	authorization, ok := authorizeLegalHoldManagement(event)
	if !ok {
		return nil
	}
	response, err := listLegalHolds(
		event.App, authorization.OrganizationID,
		strings.TrimSpace(event.Request.URL.Query().Get("cursor")),
	)
	if err != nil {
		return legalHoldManagementError(event, err, "list")
	}
	return event.JSON(http.StatusOK, response)
}

func createOrganizationLegalHoldRoute(event *core.RequestEvent) error {
	authorization, ok := authorizeLegalHoldManagement(event)
	if !ok {
		return nil
	}
	input := leafen.LegalHoldCreateInput{}
	if err := event.BindBody(&input); err != nil {
		return legalHoldManagementError(event, legalHoldBodyRefused(err), "create-bind")
	}
	response, err := createOrganizationLegalHold(event.App, authorization, input)
	if err != nil {
		return legalHoldManagementError(event, err, "create")
	}
	status := http.StatusCreated
	if response.Duplicate {
		status = http.StatusOK
	}
	return event.JSON(status, response)
}

func releaseOrganizationLegalHoldRoute(event *core.RequestEvent) error {
	authorization, ok := authorizeLegalHoldManagement(event)
	if !ok {
		return nil
	}
	input := leafen.LegalHoldReleaseInput{}
	if err := event.BindBody(&input); err != nil {
		return legalHoldManagementError(event, legalHoldBodyRefused(err), "release-bind")
	}
	// A key's call asks its person only for a release that would be made
	// (internal/app/user_key_acting_ask.go).
	if err := leafen.RehearseBeforeAsking(event, func(txApp core.App) error {
		_, err := releaseOrganizationLegalHold(
			txApp, authorization,
			strings.TrimSpace(event.Request.PathValue("legalHoldId")), input,
		)
		return err
	}); err != nil {
		return legalHoldManagementError(event, err, "release")
	}
	if asked, err := leafen.AskBeforeActing(event); asked {
		return err
	}
	response, err := releaseOrganizationLegalHold(
		event.App, authorization,
		strings.TrimSpace(event.Request.PathValue("legalHoldId")), input,
	)
	if err != nil {
		return legalHoldManagementError(event, err, "release")
	}
	return event.JSON(http.StatusOK, response)
}

func authorizeLegalHoldManagement(
	event *core.RequestEvent,
) (leafen.OrganizationAuthorization, bool) {
	user, ok := leafen.AuthenticatedUser(event)
	if !ok {
		_ = leafen.APIError(event, http.StatusUnauthorized, leafen.ErrorCodeUnauthorized, "Login required.")
		return leafen.OrganizationAuthorization{}, false
	}
	organizationID := strings.TrimSpace(event.Request.PathValue("organizationId"))
	authorization, err := leafen.AuthorizeOrganization(
		event.App, user.Id, organizationID, leafen.PermissionOrganizationLegalHolds,
	)
	if leafen.IsPermissionDenied(err) {
		_ = leafen.APIError(event, http.StatusNotFound, leafen.ErrorCodeOrganizationNotFound, "Organization not found.")
		return leafen.OrganizationAuthorization{}, false
	}
	if err != nil {
		_ = leafen.APIErrorWithCause(
			event, http.StatusInternalServerError, leafen.ErrorCodeOrganizationAuthorityInvalid,
			"Couldn't check organization permission.", "enterprise_legal_hold", "authorization",
			leafen.ErrorCodeOrganizationAuthorityInvalid, err,
		)
		return leafen.OrganizationAuthorization{}, false
	}
	return authorization, true
}

// legalHoldManagementError answers a refused legal hold request. A refusal
// says why, as the check that refused it does (NotionAdminInvalid: someone
// not in the organization, all with particular people, a name too long, a
// body that doesn't decode) and as the Notion-compatible Admin API says it,
// after "Check the legal hold details:" — seen 2026-11-26, a real agent was
// told only "Check the legal hold details." three times for people it named
// by an ID this route didn't read then, and never learned which detail.
func legalHoldManagementError(event *core.RequestEvent, err error, stage string) error {
	status, code, message := http.StatusInternalServerError,
		leafen.ErrorCodeLegalHoldUnavailable, "Couldn't process the legal hold."
	switch {
	case errors.Is(err, errLegalHoldInvalid):
		status, code, message = http.StatusBadRequest,
			leafen.ErrorCodeLegalHoldInvalid, "Check the legal hold details."
		if reason := leafen.NotionAdminRefusalReason(err, ""); reason != "" {
			message = "Check the legal hold details: " + reason
		}
	case errors.Is(err, errLegalHoldNotFound):
		status, code, message = http.StatusNotFound,
			leafen.ErrorCodeLegalHoldNotFound, "Legal hold not found."
	case errors.Is(err, leafen.ErrLegalHoldPreventsPermanentDelete):
		status, code, message = http.StatusPreconditionFailed,
			leafen.ErrorCodeLegalHoldPreventsPermanentDelete,
			"Release the active legal hold, then permanently delete."
	case leafen.IsPermissionDenied(err):
		status, code, message = http.StatusNotFound,
			leafen.ErrorCodeOrganizationNotFound, "Organization not found."
	}
	return leafen.APIErrorWithCause(
		event, status, code, message, "enterprise_legal_hold", stage, code, err,
	)
}

// legalHoldBodyRefused is a body that doesn't decode as the request's,
// refused saying where (body.scope.userIds should be an array.).
func legalHoldBodyRefused(err error) error {
	return errors.Join(leafen.NotionAdminInvalid(errLegalHoldInvalid, leafen.NotionAdminValidationReason(err)), err)
}
