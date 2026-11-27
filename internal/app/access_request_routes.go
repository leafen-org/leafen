package app

import (
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

func registerAccessRequestRoutes(event *core.ServeEvent) {
	limit := apis.BodyLimit(accessRequestMaxBodyBytes)
	event.Router.Route(accessRequestPageCreateMethod, apiRoot+accessRequestPageCreatePath, createPageAccessRequestRoute).Bind(limit)
	event.Router.Route(accessRequestGuestCreateMethod, apiRoot+accessRequestGuestCreatePath, createGuestAccessRequestRoute).Bind(limit)
	registerWorkspaceMembershipRequestRoutes(event)
	event.Router.Route(accessRequestMemberCreateMethod, apiRoot+accessRequestMemberCreatePath, createMemberAccessRequestRoute).Bind(limit)
	event.Router.Route(accessRequestListMethod, apiRoot+accessRequestListPath, listAccessRequestsRoute)
	event.Router.Route(accessRequestApproveMethod, apiRoot+accessRequestApprovePath, func(event *core.RequestEvent) error {
		return decideAccessRequestRoute(event, true)
	}).Bind(limit)
	event.Router.Route(accessRequestDeclineMethod, apiRoot+accessRequestDeclinePath, func(event *core.RequestEvent) error {
		return decideAccessRequestRoute(event, false)
	}).Bind(limit)
}

func createPageAccessRequestRoute(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	input := accessRequestPageInput{}
	if err := event.BindBody(&input); err != nil {
		return accessRequestError(event, bodyRefusal(errAccessRequestInvalid, "page access request", err))
	}
	if strings.TrimSpace(input.Role) == "" {
		return refuseMissingInput(event, errorCodeAccessRequestInvalid, "role", "the access asked for: view, comment or edit.")
	}
	response, err := requestPageAccess(event.App, user.Id, strings.TrimSpace(event.Request.PathValue("pageId")), input.Role)
	if err != nil {
		return accessRequestError(event, err)
	}
	return event.JSON(http.StatusCreated, response)
}

func createGuestAccessRequestRoute(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	input := accessRequestGuestInput{}
	if err := event.BindBody(&input); err != nil {
		return accessRequestError(event, bodyRefusal(errAccessRequestInvalid, "guest access request", err))
	}
	response, err := requestGuestInvite(
		event.App, user.Id, strings.TrimSpace(event.Request.PathValue("pageId")), input,
	)
	if err != nil {
		return accessRequestError(event, err)
	}
	return event.JSON(http.StatusCreated, response)
}

func createMemberAccessRequestRoute(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	input := accessRequestMemberInput{}
	if err := event.BindBody(&input); err != nil {
		return accessRequestError(event, bodyRefusal(errAccessRequestInvalid, "membership request", err))
	}
	if strings.TrimSpace(input.Email) == "" {
		return refuseMissingInput(event, errorCodeAccessRequestInvalid, "email", "the email address of the person to add.")
	}
	response, err := requestMemberAdd(
		event.App, user.Id, strings.TrimSpace(event.Request.PathValue("workspaceId")), input.Email, input.Message,
	)
	if err != nil {
		return accessRequestError(event, err)
	}
	return event.JSON(http.StatusCreated, response)
}

// listAccessRequestsRoute lists the workspace's waiting requests, or with
// ids (comma separated) those requests as they stand, decided ones too, which
// the inbox's rows read.
func listAccessRequestsRoute(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	query := event.Request.URL.Query()
	var (
		response accessRequestListResponse
		err      error
	)
	if query.Has("ids") {
		response, err = readAccessRequests(event.App, user.Id, workspaceID, strings.Split(query.Get("ids"), ","))
	} else {
		response, err = listAccessRequests(event.App, user.Id, workspaceID, query.Get("cursor"))
	}
	if err != nil {
		return accessRequestError(event, err)
	}
	return event.JSON(http.StatusOK, response)
}

func decideAccessRequestRoute(event *core.RequestEvent, approve bool) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	// Approving may give another role than the one asked for, as Notion's inbox row offers.
	input := accessRequestDecisionInput{}
	if approve {
		if err := event.BindBody(&input); err != nil {
			return accessRequestError(event, bodyRefusal(errAccessRequestInvalid, "request's answer", err))
		}
	}
	requestID, role := strings.TrimSpace(event.Request.PathValue("requestId")), strings.TrimSpace(input.Role)
	// A key's call asks its person only for an approval that would be
	// made, made once and taken back under the page's sharing lock
	// (user_key_acting_ask.go).
	if userKeyAsksWhereItActs(event) {
		release, err := lockAccessRequestDecision(event.App, requestID)
		if err == nil {
			err = rehearseBeforeAsking(event, func(txApp core.App) error {
				_, err := decideStoredAccessRequest(event.Request.Context(), txApp, user.Id, requestID, approve, role)
				return err
			})
			release()
		}
		if err != nil {
			return accessRequestError(event, err)
		}
		if asked, err := askBeforeActing(event); asked {
			return err
		}
	}
	response, err := decideAccessRequestAs(event.Request.Context(), event.App, user.Id, requestID, approve, role)
	if err != nil {
		return accessRequestError(event, err)
	}
	return event.JSON(http.StatusOK, response)
}

func accessRequestError(event *core.RequestEvent, err error) error {
	// A request for a page an operation holds a while is decided once it's done.
	if pageID, busy := routeRefusedPageHeld(event, err); busy != "" {
		return answerPageHeld(event, pageID, busy)
	}
	switch {
	case errors.Is(err, errAccessRequestInvalid):
		return apiError(event, http.StatusBadRequest, errorCodeAccessRequestInvalid, refusalMessage("Check the request.", refusalReason(err)))
	case errors.Is(err, errAccessRequestNotFound) || isPermissionDenied(err) || errors.Is(err, errPublicMutationNotFound):
		return apiError(event, http.StatusNotFound, errorCodeAccessRequestNotFound, "Request not found.")
	case errors.Is(err, errAccessRequestNotAllowed):
		return apiError(event, http.StatusForbidden, errorCodeAccessRequestNotAllowed, "This workspace doesn't take this request.")
	case errors.Is(err, errAccessRequestDecided):
		return apiError(event, http.StatusConflict, errorCodeAccessRequestDecided, "This request was already decided.")
	case errors.Is(err, errOrganizationSharingSecurityBlocked):
		return apiError(event, http.StatusForbidden, errorCodeOrganizationSharingSecurityBlocked,
			"The organization's sharing settings don't allow this.")
	case errors.Is(err, errWorkspaceMemberAddDisabled):
		return apiError(event, http.StatusForbidden, errorCodeWorkspaceMemberAddDisabled, "Adding members is turned off.")
	}
	// What stopped it is logged by its code, for the diagnostic reference.
	return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeAccessRequestFailed, "The request couldn't be handled.",
		diagnosticOperation(event), "response", "", err)
}
