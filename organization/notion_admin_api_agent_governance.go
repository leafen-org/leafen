package organization

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/pocketbase/core"
)

var (
	errNotionAdminAgentInvalid   = errors.New("invalid Notion Admin agent-governance request")
	errNotionAdminAgentForbidden = leafen.NotionAdminInvalid(errors.New("Notion Admin agent-governance request forbidden"), leafen.NotionAdminScopeChanged)
	errNotionAdminAgentNotFound  = errors.New("Notion Admin agent-governance target not found")
	errNotionAdminAgentConflict  = errors.New("Notion Admin agent-governance conflict")
)

type notionAdminAgentRouteKind int

const (
	notionAdminAgentRouteCollection notionAdminAgentRouteKind = iota + 1
	notionAdminAgentRouteCreationPolicy
	notionAdminAgentRouteWorkspaceCreditLimit
	notionAdminAgentRouteStatus
	notionAdminAgentRouteDelete
	notionAdminAgentRouteCreditUsage
	notionAdminAgentRoutePermissions
	notionAdminAgentRouteAggregateCreditUsage
	notionAdminAgentRouteCreditLimit
)

type notionAdminAgentCreationPolicyRequest struct {
	Policy                string `json:"policy"`
	DisableExistingAgents bool   `json:"disable_existing_agents,omitempty"`
}

type notionAdminAgentWorkspaceCreditLimitRequest struct {
	DefaultAgentCreditLimit notionAdminNullableInt64Input `json:"default_agent_credit_limit"`
}

type notionAdminAgentStatusRequest struct {
	AdminStatus string `json:"admin_status"`
}

type notionAdminAgentCreditLimitRequest struct {
	CreditLimit notionAdminNullableInt64Input `json:"credit_limit"`
}

type notionAdminNullableInt64Input struct {
	Present bool
	Value   *int64
}

func (input *notionAdminNullableInt64Input) UnmarshalJSON(data []byte) error {
	input.Present = true
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		input.Value = nil
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var number json.Number
	if err := decoder.Decode(&number); err != nil {
		return leafen.NotionAdminInvalid(errNotionAdminAgentInvalid, "The value should be a whole number.")
	}
	value, err := strconv.ParseInt(number.String(), 10, 64)
	if err != nil {
		return leafen.NotionAdminInvalid(errNotionAdminAgentInvalid, "The value should be a whole number.")
	}
	input.Value = &value
	return nil
}

func notionAdminAgentGovernanceRoute(path string) (string, string, notionAdminAgentRouteKind, bool) {
	value, found := strings.CutPrefix(path, leafen.NotionAdminAPISpacesPath+"/")
	parts := strings.Split(value, "/")
	if !found || len(parts) < 2 || parts[0] == "" {
		return "", "", 0, false
	}
	if len(parts) == 2 && parts[1] == "credit_limit" {
		return parts[0], "", notionAdminAgentRouteWorkspaceCreditLimit, true
	}
	if parts[1] != "agents" {
		return "", "", 0, false
	}
	switch {
	case len(parts) == 2:
		return parts[0], "", notionAdminAgentRouteCollection, true
	case len(parts) == 3 && parts[2] == "creation_policy":
		return parts[0], "", notionAdminAgentRouteCreationPolicy, true
	case len(parts) == 3 && parts[2] == "credit_usage":
		return parts[0], "", notionAdminAgentRouteAggregateCreditUsage, true
	case len(parts) == 3 && parts[2] != "":
		return parts[0], parts[2], notionAdminAgentRouteDelete, true
	case len(parts) == 4 && parts[2] != "" && parts[3] == "status":
		return parts[0], parts[2], notionAdminAgentRouteStatus, true
	case len(parts) == 4 && parts[2] != "" && parts[3] == "credit_usage":
		return parts[0], parts[2], notionAdminAgentRouteCreditUsage, true
	case len(parts) == 4 && parts[2] != "" && parts[3] == "permissions":
		return parts[0], parts[2], notionAdminAgentRoutePermissions, true
	case len(parts) == 4 && parts[2] != "" && parts[3] == "credit_limit":
		return parts[0], parts[2], notionAdminAgentRouteCreditLimit, true
	default:
		return "", "", 0, false
	}
}

func serveNotionAdminAgentGovernance(
	event *core.RequestEvent,
	rawWorkspaceID, rawAgentID string,
	route notionAdminAgentRouteKind,
) (bool, error) {
	method := event.Request.Method
	supported := route == notionAdminAgentRouteCollection && method == http.MethodGet ||
		route == notionAdminAgentRouteCreationPolicy && method == http.MethodPatch ||
		route == notionAdminAgentRouteWorkspaceCreditLimit && method == http.MethodPatch ||
		route == notionAdminAgentRouteStatus && method == http.MethodPatch ||
		route == notionAdminAgentRouteDelete && (method == http.MethodDelete || method == http.MethodPatch) ||
		route == notionAdminAgentRouteCreditUsage && method == http.MethodGet ||
		route == notionAdminAgentRoutePermissions && (method == http.MethodGet || method == http.MethodPatch) ||
		route == notionAdminAgentRouteAggregateCreditUsage && method == http.MethodGet ||
		route == notionAdminAgentRouteCreditLimit && method == http.MethodPut
	if !supported {
		return false, nil
	}
	secret := leafen.BearerToken(event.Request)
	release := leafen.AcquireNotionAdminTokenLifecycleLock(leafen.NotionAdminTokenHash(secret))
	defer release()
	capability := leafen.NotionAdminCapabilityWorkflowsWrite
	if method == http.MethodGet {
		capability = leafen.NotionAdminCapabilityWorkflowsRead
	}
	identity, authorized := leafen.AuthorizeNotionAdminPublicAPI(event, secret, capability)
	if !authorized {
		return true, nil
	}
	workspaceID, err := leafen.ResolveNotionCompatibleWorkspaceID(strings.TrimSpace(rawWorkspaceID))
	if err != nil {
		return true, notionAdminAgentError(event, leafen.NotionAdminInvalid(errNotionAdminAgentInvalid, "The workspace ID should be a UUID."))
	}
	if !identity.CanAccessWorkspace(workspaceID) {
		return true, notionAdminAgentError(event, errNotionAdminAgentNotFound)
	}
	agentID := ""
	if rawAgentID != "" {
		agentID, err = resolveNotionCompatibleAgentID(strings.TrimSpace(rawAgentID))
		if err != nil {
			return true, notionAdminAgentError(event, leafen.NotionAdminInvalid(errNotionAdminAgentInvalid, "The agent ID isn't an agent ID."))
		}
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	switch route {
	case notionAdminAgentRouteCollection:
		return true, serveNotionAdminAgentList(event, secret, identity, workspaceID)
	case notionAdminAgentRouteCreationPolicy:
		return true, serveNotionAdminAgentCreationPolicy(event, secret, identity, workspaceID)
	case notionAdminAgentRouteWorkspaceCreditLimit:
		return true, serveNotionAdminWorkspaceCreditLimit(event, secret, identity, workspaceID)
	case notionAdminAgentRouteStatus:
		return true, serveNotionAdminAgentStatus(event, secret, identity, workspaceID, agentID)
	case notionAdminAgentRouteDelete:
		if method == http.MethodPatch {
			return true, serveNotionAdminAgentUpdate(event, secret, identity, workspaceID, agentID)
		}
		return true, serveNotionAdminAgentDelete(event, secret, identity, workspaceID, agentID)
	case notionAdminAgentRouteCreditUsage:
		return true, serveNotionAdminAgentCreditUsage(event, secret, identity, workspaceID, agentID)
	case notionAdminAgentRoutePermissions:
		return true, serveNotionAdminAgentPermissions(event, secret, identity, workspaceID, agentID)
	case notionAdminAgentRouteAggregateCreditUsage:
		return true, serveNotionAdminAgentsCreditUsage(event, secret, identity, workspaceID)
	default:
		return true, serveNotionAdminAgentCreditLimit(event, secret, identity, workspaceID, agentID)
	}
}

func serveNotionAdminAgentCreationPolicy(
	event *core.RequestEvent, secret string, identity leafen.NotionAdminIdentity, workspaceID string,
) error {
	if len(event.Request.URL.Query()) != 0 {
		return notionAdminAgentError(event, leafen.NotionAdminInvalid(errNotionAdminAgentInvalid, "This request takes no query parameters."))
	}
	request := notionAdminAgentCreationPolicyRequest{}
	if decodeNotionAdminAgentBody(event, &request) != nil ||
		!governanceOneOf(request.Policy, "all_workspace_members", "disabled", "workspace_owners_only") ||
		request.DisableExistingAgents && request.Policy != "disabled" {
		return notionAdminAgentError(event, leafen.NotionAdminInvalid(errNotionAdminAgentInvalid, "policy takes all_workspace_members, disabled or workspace_owners_only; disable_existing_agents goes only with disabled."))
	}
	response, err := updateNotionAdminAgentCreationPolicy(
		event.Request.Context(), event.App, secret, identity, workspaceID, request,
	)
	if err != nil {
		return notionAdminAgentError(event, err)
	}
	return event.JSON(http.StatusOK, response)
}

func serveNotionAdminWorkspaceCreditLimit(
	event *core.RequestEvent, secret string, identity leafen.NotionAdminIdentity, workspaceID string,
) error {
	if len(event.Request.URL.Query()) != 0 {
		return notionAdminAgentError(event, leafen.NotionAdminInvalid(errNotionAdminAgentInvalid, "This request takes no query parameters."))
	}
	request := notionAdminAgentWorkspaceCreditLimitRequest{}
	if decodeNotionAdminAgentBody(event, &request) != nil || !request.DefaultAgentCreditLimit.Present ||
		request.DefaultAgentCreditLimit.Value != nil && *request.DefaultAgentCreditLimit.Value < 0 {
		return notionAdminAgentError(event, leafen.NotionAdminInvalid(errNotionAdminAgentInvalid, "default_agent_credit_limit is required: a number not below 0, or null."))
	}
	response, err := updateNotionAdminWorkspaceCreditLimit(
		event.Request.Context(), event.App, secret, identity, workspaceID,
		request.DefaultAgentCreditLimit.Value,
	)
	if err != nil {
		return notionAdminAgentError(event, err)
	}
	return event.JSON(http.StatusOK, response)
}

func serveNotionAdminAgentStatus(
	event *core.RequestEvent, secret string, identity leafen.NotionAdminIdentity, workspaceID, agentID string,
) error {
	if len(event.Request.URL.Query()) != 0 {
		return notionAdminAgentError(event, leafen.NotionAdminInvalid(errNotionAdminAgentInvalid, "This request takes no query parameters."))
	}
	request := notionAdminAgentStatusRequest{}
	if decodeNotionAdminAgentBody(event, &request) != nil ||
		!governanceOneOf(request.AdminStatus, "active", "disabled") {
		return notionAdminAgentError(event, leafen.NotionAdminInvalid(errNotionAdminAgentInvalid, "admin_status takes active or disabled."))
	}
	response, err := updateNotionAdminAgentStatus(
		event.Request.Context(), event.App, secret, identity, workspaceID, agentID, request.AdminStatus,
	)
	if err != nil {
		return notionAdminAgentError(event, err)
	}
	return event.JSON(http.StatusOK, response)
}

func serveNotionAdminAgentDelete(
	event *core.RequestEvent, secret string, identity leafen.NotionAdminIdentity, workspaceID, agentID string,
) error {
	if event.Request.ContentLength > 0 || len(event.Request.URL.Query()) != 0 {
		return notionAdminAgentError(event, leafen.NotionAdminInvalid(errNotionAdminAgentInvalid, "This request takes no query parameters and no body."))
	}
	if err := deleteNotionAdminAgent(
		event.Request.Context(), event.App, secret, identity, workspaceID, agentID,
	); err != nil {
		return notionAdminAgentError(event, err)
	}
	return event.JSON(http.StatusOK, map[string]any{})
}

func serveNotionAdminAgentCreditLimit(
	event *core.RequestEvent, secret string, identity leafen.NotionAdminIdentity, workspaceID, agentID string,
) error {
	if len(event.Request.URL.Query()) != 0 {
		return notionAdminAgentError(event, leafen.NotionAdminInvalid(errNotionAdminAgentInvalid, "This request takes no query parameters."))
	}
	request := notionAdminAgentCreditLimitRequest{}
	if decodeNotionAdminAgentBody(event, &request) != nil || !request.CreditLimit.Present ||
		request.CreditLimit.Value != nil && *request.CreditLimit.Value < 0 {
		return notionAdminAgentError(event, leafen.NotionAdminInvalid(errNotionAdminAgentInvalid, "credit_limit is required: a number not below 0, or null."))
	}
	response, err := updateNotionAdminAgentCreditLimit(
		event.Request.Context(), event.App, secret, identity, workspaceID, agentID, request.CreditLimit.Value,
	)
	if err != nil {
		return notionAdminAgentError(event, err)
	}
	return event.JSON(http.StatusOK, response)
}

func decodeNotionAdminAgentBody(event *core.RequestEvent, target any) error {
	event.Request.Body = http.MaxBytesReader(event.Response, event.Request.Body, notionAdminAgentMaxBodyBytes)
	decoder := json.NewDecoder(event.Request.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(target) != nil {
		return leafen.NotionAdminInvalid(errNotionAdminAgentInvalid, "The body has a field this request doesn't take, or a value of the wrong kind.")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return leafen.NotionAdminInvalid(errNotionAdminAgentInvalid, "The body holds more than one JSON value.")
	}
	return nil
}

func governanceOneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func notionAdminAgentError(event *core.RequestEvent, err error) error {
	switch {
	case errors.Is(err, leafen.ErrNotionAdminTokenUnauthorized):
		return leafen.NotionAdminPublicAPIError(event, http.StatusUnauthorized, leafen.NotionAdminErrorUnauthorized, leafen.NotionAdminUnauthorizedMessage, err)
	case errors.Is(err, errNotionAdminAgentForbidden):
		return leafen.NotionAdminPublicAPIError(event, http.StatusForbidden, leafen.NotionErrorRestrictedResource, leafen.NotionAdminRefusalReason(err, leafen.NotionAdminForbiddenMessage), err)
	case errors.Is(err, errNotionAdminAgentInvalid), errors.Is(err, errInvalidNotionAdminAgentCursor):
		return leafen.NotionAdminPublicAPIError(event, http.StatusBadRequest, leafen.NotionAdminErrorValidation, leafen.NotionAdminValidationReason(err), err)
	case errors.Is(err, errNotionAdminAgentNotFound):
		return leafen.NotionAdminPublicAPIError(event, http.StatusNotFound, leafen.NotionErrorObjectNotFound, leafen.NotionAdminNotFoundMessage, err)
	case errors.Is(err, errNotionAdminAgentConflict):
		return leafen.NotionAdminPublicAPIError(event, http.StatusConflict, "conflict_error", "Conflict.", err)
	default:
		return leafen.NotionAdminPublicAPIError(event, http.StatusInternalServerError, leafen.NotionAdminErrorInternal, leafen.NotionAdminInternalErrorMessage, err)
	}
}
