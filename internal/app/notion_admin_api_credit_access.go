package app

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/pocketbase/pocketbase/core"
)

// Notion's Admin API credit access: policies under
// /v1/spaces/{space_id}/policies/credit_access and their assignments, ten
// operations, read with credit-limit-policy:read and changed with
// credit-limit-policy:write. Storage and the rules are in
// schema_baseline.json; reads in _store.go, changes in _mutation.go.

const (
	notionCreditAccessMaxBodyBytes      = 64 << 10
	notionCreditAccessMaxNameRunes      = 128
	notionCreditAccessMaxVersionRunes   = 256
	notionCreditAccessMaxScimValueRunes = 1024
	notionCreditAccessMaxCredits        = 100_000_000
	notionCreditAccessNoAccess          = "no_access"
	notionCreditAccessLimited           = "limited"
	notionCreditAccessUnlimited         = "unlimited"
	notionCreditAccessSelectorAll       = "all_space_members"
	notionCreditAccessSelectorUser      = "user"
	notionCreditAccessSelectorGroup     = "space_permission_group"
	notionCreditAccessSelectorSCIM      = "scim_attribute"
	notionCreditAccessPolicyObject      = "credit_access_policy"
	notionCreditAccessAssignmentObject  = "credit_access_policy_assignment"
)

var notionCreditAccessSCIMAttributes = map[string]bool{
	"cost_center": true, "department": true, "division": true, "user_type": true,
}

var (
	errNotionCreditAccessInvalid   = errors.New("invalid Notion Admin credit access request")
	errNotionCreditAccessForbidden = notionAdminInvalid(errors.New("Notion Admin credit access request forbidden"), notionAdminScopeChangedReason)
	errNotionCreditAccessNotFound  = errors.New("Notion Admin credit access target not found")
	errNotionCreditAccessConflict  = errors.New("Notion Admin credit access conflict")
)

type notionCreditAccessRoute int

const (
	notionCreditAccessRoutePolicies notionCreditAccessRoute = iota + 1
	notionCreditAccessRoutePolicy
	notionCreditAccessRoutePolicyAssignments
	notionCreditAccessRouteAssignment
)

type notionCreditAccessLimit struct {
	Kind    string `json:"kind"`
	Credits *int64 `json:"credits,omitempty"`
}

type notionCreditAccessPolicyResponse struct {
	Object  string                  `json:"object"`
	ID      string                  `json:"id"`
	SpaceID string                  `json:"space_id"`
	Version string                  `json:"version"`
	Limit   notionCreditAccessLimit `json:"limit"`
	Name    string                  `json:"name"`
}

// notionCreditAccessSelector holds internal IDs once a request is read and
// public ones in an answer.
type notionCreditAccessSelector struct {
	Type      string `json:"type"`
	UserID    string `json:"user_id,omitempty"`
	GroupID   string `json:"group_id,omitempty"`
	Attribute string `json:"attribute,omitempty"`
	Value     string `json:"value,omitempty"`
}

type notionCreditAccessAssignmentResponse struct {
	Object   string                     `json:"object"`
	ID       string                     `json:"id"`
	SpaceID  string                     `json:"space_id"`
	Version  string                     `json:"version"`
	PolicyID string                     `json:"policy_id"`
	Selector notionCreditAccessSelector `json:"selector"`
}

type notionCreditAccessDeletedResponse struct {
	Object  string `json:"object"`
	ID      string `json:"id"`
	Deleted bool   `json:"deleted"`
}

type notionCreditAccessListResponse[T any] struct {
	Object     string  `json:"object"`
	Type       string  `json:"type"`
	Results    []T     `json:"results"`
	HasMore    bool    `json:"has_more"`
	NextCursor *string `json:"next_cursor"`
}

type notionCreditAccessLimitBody struct {
	Kind    *string `json:"kind"`
	Credits *int64  `json:"credits"`
}

type notionCreditAccessPolicyCreateBody struct {
	Name  *string                      `json:"name"`
	Limit *notionCreditAccessLimitBody `json:"limit"`
}

type notionCreditAccessPolicyUpdateBody struct {
	ExpectedVersion *string                      `json:"expected_version"`
	Name            *string                      `json:"name"`
	Limit           *notionCreditAccessLimitBody `json:"limit"`
}

type notionCreditAccessSelectorBody struct {
	Type      *string `json:"type"`
	UserID    *string `json:"user_id"`
	GroupID   *string `json:"group_id"`
	Attribute *string `json:"attribute"`
	Value     *string `json:"value"`
}

type notionCreditAccessAssignmentCreateBody struct {
	Selector *notionCreditAccessSelectorBody `json:"selector"`
}

type notionCreditAccessAssignmentUpdateBody struct {
	PolicyID *string `json:"policy_id"`
}

// notionCreditAccessPolicyChange is a policy update read from its body:
// the version it was read at, and the name and limit when given.
type notionCreditAccessPolicyChange struct {
	ExpectedVersion string
	Name            *string
	Limit           *notionCreditAccessLimit
}

func notionAdminCreditAccessRoute(path string) (string, string, notionCreditAccessRoute, bool) {
	value, found := strings.CutPrefix(path, notionAdminAPISpacesPath+"/")
	parts := strings.Split(value, "/")
	if !found || len(parts) < 3 || parts[0] == "" || parts[1] != "policies" || parts[2] != "credit_access" {
		return "", "", 0, false
	}
	switch {
	case len(parts) == 3:
		return parts[0], "", notionCreditAccessRoutePolicies, true
	case len(parts) == 4 && parts[3] != "":
		return parts[0], parts[3], notionCreditAccessRoutePolicy, true
	case len(parts) == 5 && parts[3] == "assignments" && parts[4] != "":
		return parts[0], parts[4], notionCreditAccessRouteAssignment, true
	case len(parts) == 5 && parts[3] != "" && parts[4] == "assignments":
		return parts[0], parts[3], notionCreditAccessRoutePolicyAssignments, true
	}
	return "", "", 0, false
}

func serveNotionAdminCreditAccess(event *core.RequestEvent, rawWorkspaceID, rawTargetID string, route notionCreditAccessRoute) (bool, error) {
	method := event.Request.Method
	collection := route == notionCreditAccessRoutePolicies || route == notionCreditAccessRoutePolicyAssignments
	if collection && method != http.MethodGet && method != http.MethodPost ||
		!collection && method != http.MethodGet && method != http.MethodPatch && method != http.MethodDelete {
		return false, nil
	}
	secret := bearerToken(event.Request)
	release := acquireNotionAdminTokenLifecycleLock(notionAdminTokenHash(secret))
	defer release()
	capability := notionAdminCapabilityCreditLimitPolicyWrite
	if method == http.MethodGet {
		capability = notionAdminCapabilityCreditLimitPolicyRead
	}
	identity, authorized := authorizeNotionAdminPublicAPI(event, secret, capability)
	if !authorized {
		return true, nil
	}
	workspaceID, err := resolveNotionCompatibleWorkspaceID(strings.TrimSpace(rawWorkspaceID))
	if err != nil {
		return true, notionCreditAccessError(event, notionAdminInvalid(errNotionCreditAccessInvalid, "The workspace ID should be a UUID."))
	}
	if !identity.CanAccessWorkspace(workspaceID) {
		return true, notionCreditAccessError(event, errNotionCreditAccessNotFound)
	}
	targetID := ""
	if route == notionCreditAccessRouteAssignment {
		if targetID, err = resolveNotionCreditAccessID(notionCreditAccessAssignmentIdentity, rawTargetID); err != nil {
			return true, notionCreditAccessError(event, notionAdminInvalid(errNotionCreditAccessInvalid, "The assignment ID isn't a credit access assignment ID."))
		}
	} else if route != notionCreditAccessRoutePolicies {
		if targetID, err = resolveNotionCreditAccessID(notionCreditAccessPolicyIdentity, rawTargetID); err != nil {
			return true, notionCreditAccessError(event, notionAdminInvalid(errNotionCreditAccessInvalid, "The policy ID isn't a credit access policy ID."))
		}
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	request := notionCreditAccessRequest{event: event, secret: secret, identity: identity, workspaceID: workspaceID, targetID: targetID}
	var response any
	if method == http.MethodGet {
		response, err = request.read(route)
	} else {
		response, err = request.change(route)
	}
	if err != nil {
		return true, notionCreditAccessError(event, err)
	}
	return true, event.JSON(http.StatusOK, response)
}

type notionCreditAccessRequest struct {
	event       *core.RequestEvent
	secret      string
	identity    notionAdminIdentity
	workspaceID string
	targetID    string
}

func (request notionCreditAccessRequest) read(route notionCreditAccessRoute) (any, error) {
	httpRequest := request.event.Request
	if httpRequest.ContentLength > 0 {
		return nil, notionAdminInvalid(errNotionCreditAccessInvalid, "This request takes no body.")
	}
	ctx, app := httpRequest.Context(), request.event.App
	if route == notionCreditAccessRoutePolicy || route == notionCreditAccessRouteAssignment {
		if len(httpRequest.URL.Query()) != 0 {
			return nil, notionAdminInvalid(errNotionCreditAccessInvalid, "This request takes no query parameters.")
		}
		if route == notionCreditAccessRoutePolicy {
			return retrieveNotionCreditAccessPolicy(ctx, app, request.identity, request.workspaceID, request.targetID)
		}
		return retrieveNotionCreditAccessAssignment(ctx, app, request.identity, request.workspaceID, request.targetID)
	}
	pageSize, rawCursor, err := notionCreditAccessListQuery(httpRequest)
	if err != nil {
		return nil, err
	}
	kind := notionCreditAccessPolicyObject
	if route == notionCreditAccessRoutePolicyAssignments {
		kind = notionCreditAccessAssignmentObject
	}
	cursor, err := decodeNotionCreditAccessCursor(rawCursor, request.secret, request.identity, request.workspaceID, request.targetID, kind)
	if err != nil {
		return nil, err
	}
	if route == notionCreditAccessRoutePolicies {
		return listNotionCreditAccessPolicies(ctx, app, request.secret, request.identity, request.workspaceID, cursor, pageSize)
	}
	return listNotionCreditAccessAssignments(ctx, app, request.secret, request.identity, request.workspaceID, request.targetID, cursor, pageSize)
}

func (request notionCreditAccessRequest) change(route notionCreditAccessRoute) (any, error) {
	httpRequest := request.event.Request
	ctx, app, method := httpRequest.Context(), request.event.App, httpRequest.Method
	if method == http.MethodDelete {
		expectedVersion, err := notionCreditAccessDeleteQuery(request.event)
		if err != nil {
			return nil, err
		}
		if route == notionCreditAccessRoutePolicy {
			return deleteNotionCreditAccessPolicy(ctx, app, request.secret, request.identity, request.workspaceID, request.targetID, expectedVersion)
		}
		return deleteNotionCreditAccessAssignment(ctx, app, request.secret, request.identity, request.workspaceID, request.targetID, expectedVersion)
	}
	if len(httpRequest.URL.Query()) != 0 {
		return nil, notionAdminInvalid(errNotionCreditAccessInvalid, "This request takes no query parameters.")
	}
	switch route {
	case notionCreditAccessRoutePolicies:
		body := notionCreditAccessPolicyCreateBody{}
		if err := decodeNotionCreditAccessBody(request.event, &body); err != nil {
			return nil, err
		}
		if body.Name == nil || body.Limit == nil {
			return nil, notionAdminInvalid(errNotionCreditAccessInvalid, "body.name and body.limit are needed.")
		}
		name, limit, err := normalizeNotionCreditAccessPolicy(body.Name, body.Limit)
		if err != nil {
			return nil, err
		}
		return createNotionCreditAccessPolicy(ctx, app, request.secret, request.identity, request.workspaceID, *name, *limit)
	case notionCreditAccessRoutePolicy:
		body := notionCreditAccessPolicyUpdateBody{}
		if err := decodeNotionCreditAccessBody(request.event, &body); err != nil {
			return nil, err
		}
		if body.ExpectedVersion == nil || !validNotionCreditAccessVersion(*body.ExpectedVersion) {
			return nil, notionAdminInvalid(errNotionCreditAccessInvalid, "body.expected_version should be the policy's version: 1 to 256 characters.")
		}
		name, limit, err := normalizeNotionCreditAccessPolicy(body.Name, body.Limit)
		if err != nil {
			return nil, err
		}
		change := notionCreditAccessPolicyChange{ExpectedVersion: *body.ExpectedVersion, Name: name, Limit: limit}
		return updateNotionCreditAccessPolicy(ctx, app, request.secret, request.identity, request.workspaceID, request.targetID, change)
	case notionCreditAccessRoutePolicyAssignments:
		body := notionCreditAccessAssignmentCreateBody{}
		if err := decodeNotionCreditAccessBody(request.event, &body); err != nil {
			return nil, err
		}
		selector, err := normalizeNotionCreditAccessSelector(body.Selector)
		if err != nil {
			return nil, err
		}
		return assignNotionCreditAccessPolicy(ctx, app, request.secret, request.identity, request.workspaceID, request.targetID, selector)
	default:
		body := notionCreditAccessAssignmentUpdateBody{}
		if err := decodeNotionCreditAccessBody(request.event, &body); err != nil {
			return nil, err
		}
		if body.PolicyID == nil {
			return nil, notionAdminInvalid(errNotionCreditAccessInvalid, "body.policy_id is needed.")
		}
		policyID, err := resolveNotionCreditAccessID(notionCreditAccessPolicyIdentity, strings.TrimSpace(*body.PolicyID))
		if err != nil {
			return nil, notionAdminInvalid(errNotionCreditAccessInvalid, "body.policy_id isn't a credit access policy ID.")
		}
		return moveNotionCreditAccessAssignment(ctx, app, request.secret, request.identity, request.workspaceID, request.targetID, policyID)
	}
}

func decodeNotionCreditAccessBody(event *core.RequestEvent, target any) error {
	event.Request.Body = http.MaxBytesReader(event.Response, event.Request.Body, notionCreditAccessMaxBodyBytes)
	decoder := json.NewDecoder(event.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return notionAdminInvalid(errNotionCreditAccessInvalid, notionAdminValidationReason(err))
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return notionAdminInvalid(errNotionCreditAccessInvalid, "The body holds more than one JSON value.")
	}
	return nil
}

// normalizeNotionCreditAccessPolicy reads a policy's name and limit, each
// when given.
func normalizeNotionCreditAccessPolicy(rawName *string, rawLimit *notionCreditAccessLimitBody) (*string, *notionCreditAccessLimit, error) {
	if rawName != nil {
		if runes := utf8.RuneCountInString(*rawName); runes < 1 || runes > notionCreditAccessMaxNameRunes {
			return nil, nil, notionAdminInvalid(errNotionCreditAccessInvalid, "body.name should be 1 to 128 characters.")
		}
	}
	if rawLimit == nil {
		return rawName, nil, nil
	}
	limit := notionCreditAccessLimit{}
	if rawLimit.Kind != nil {
		limit.Kind = *rawLimit.Kind
	}
	switch limit.Kind {
	case notionCreditAccessNoAccess, notionCreditAccessUnlimited:
		if rawLimit.Credits != nil {
			return nil, nil, notionAdminInvalid(errNotionCreditAccessInvalid, "body.limit.credits goes only with kind limited.")
		}
	case notionCreditAccessLimited:
		if rawLimit.Credits == nil || *rawLimit.Credits < 1 || *rawLimit.Credits > notionCreditAccessMaxCredits {
			return nil, nil, notionAdminInvalid(errNotionCreditAccessInvalid, "body.limit.credits should be a whole number from 1 to 100000000.")
		}
		credits := *rawLimit.Credits
		limit.Credits = &credits
	default:
		return nil, nil, notionAdminInvalid(errNotionCreditAccessInvalid, "body.limit.kind should be `no_access`, `limited`, or `unlimited`.")
	}
	return rawName, &limit, nil
}

// normalizeNotionCreditAccessSelector reads an assignment's selector, its
// user and group as internal IDs; whether they are in the workspace is
// checked with the write.
func normalizeNotionCreditAccessSelector(raw *notionCreditAccessSelectorBody) (notionCreditAccessSelector, error) {
	if raw == nil || raw.Type == nil {
		return notionCreditAccessSelector{}, notionAdminInvalid(errNotionCreditAccessInvalid, "body.selector.type is needed.")
	}
	selector := notionCreditAccessSelector{Type: *raw.Type}
	// Each type takes exactly its own fields.
	user, group, scim := raw.UserID != nil, raw.GroupID != nil, raw.Attribute != nil || raw.Value != nil
	switch selector.Type {
	case notionCreditAccessSelectorAll:
		if user || group || scim {
			return selector, notionAdminInvalid(errNotionCreditAccessInvalid, "A selector of type all_space_members takes no other field.")
		}
	case notionCreditAccessSelectorUser:
		if !user || group || scim {
			return selector, notionAdminInvalid(errNotionCreditAccessInvalid, "A selector of type user takes user_id only.")
		}
		userID, err := resolveNotionCompatibleUserID(strings.TrimSpace(*raw.UserID))
		if err != nil {
			return selector, notionAdminInvalid(errNotionCreditAccessInvalid, "body.selector.user_id isn't a user ID.")
		}
		selector.UserID = userID
	case notionCreditAccessSelectorGroup:
		if !group || user || scim {
			return selector, notionAdminInvalid(errNotionCreditAccessInvalid, "A selector of type space_permission_group takes group_id only.")
		}
		groupID, err := resolveNotionCompatibleGroupID(strings.TrimSpace(*raw.GroupID))
		if err != nil {
			return selector, notionAdminInvalid(errNotionCreditAccessInvalid, "body.selector.group_id isn't a group ID.")
		}
		selector.GroupID = groupID
	case notionCreditAccessSelectorSCIM:
		if raw.Attribute == nil || raw.Value == nil || user || group {
			return selector, notionAdminInvalid(errNotionCreditAccessInvalid, "A selector of type scim_attribute takes attribute and value only.")
		}
		if !notionCreditAccessSCIMAttributes[*raw.Attribute] {
			return selector, notionAdminInvalid(errNotionCreditAccessInvalid, "body.selector.attribute should be `cost_center`, `department`, `division`, or `user_type`.")
		}
		if runes := utf8.RuneCountInString(*raw.Value); runes < 1 || runes > notionCreditAccessMaxScimValueRunes {
			return selector, notionAdminInvalid(errNotionCreditAccessInvalid, "body.selector.value should be 1 to 1024 characters.")
		}
		selector.Attribute, selector.Value = *raw.Attribute, *raw.Value
	default:
		return selector, notionAdminInvalid(errNotionCreditAccessInvalid, "body.selector.type should be `all_space_members`, `user`, `space_permission_group`, or `scim_attribute`.")
	}
	return selector, nil
}

func validNotionCreditAccessVersion(version string) bool {
	runes := utf8.RuneCountInString(version)
	return runes >= 1 && runes <= notionCreditAccessMaxVersionRunes
}

// notionCreditAccessDeleteQuery reads a delete's expected_version, the only
// thing it takes.
func notionCreditAccessDeleteQuery(event *core.RequestEvent) (string, error) {
	query := event.Request.URL.Query()
	values := query["expected_version"]
	if event.Request.ContentLength > 0 || len(query) != 1 || len(values) != 1 || !validNotionCreditAccessVersion(values[0]) {
		return "", notionAdminInvalid(errNotionCreditAccessInvalid, "A delete takes no body and the query expected_version once: the current version, 1 to 256 characters.")
	}
	return values[0], nil
}

func notionCreditAccessError(event *core.RequestEvent, err error) error {
	switch {
	case errors.Is(err, errNotionAdminTokenUnauthorized):
		return notionAdminPublicAPIError(event, http.StatusUnauthorized, notionAdminErrorUnauthorized, notionAdminUnauthorizedMessage, err)
	case errors.Is(err, errNotionCreditAccessForbidden):
		return notionAdminPublicAPIError(event, http.StatusForbidden, notionErrorRestrictedResource, notionAdminRefusalReason(err, notionAdminForbiddenMessage), err)
	case errors.Is(err, errNotionCreditAccessInvalid):
		return notionAdminPublicAPIError(event, http.StatusBadRequest, notionAdminErrorValidation, notionAdminValidationReason(err), err)
	case errors.Is(err, errNotionCreditAccessNotFound):
		return notionAdminPublicAPIError(event, http.StatusNotFound, notionErrorObjectNotFound, notionAdminNotFoundMessage, err)
	case errors.Is(err, errNotionCreditAccessConflict):
		return notionAdminPublicAPIError(event, http.StatusConflict, "conflict_error", notionAdminRefusalReason(err, "The request conflicts with the current resource state."), err)
	default:
		return notionAdminPublicAPIError(event, http.StatusInternalServerError, notionAdminErrorInternal, notionAdminInternalErrorMessage, err)
	}
}
