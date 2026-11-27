package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/pocketbase/pocketbase/core"
)

const (
	notionAdminSpaceGroupMaxBodyBytes = 64 << 10
	notionAdminSpaceGroupMaxNameRunes = 200
	notionAdminSpaceGroupRoleOwner    = "owner"
	notionAdminSpaceGroupRoleMember   = "member"
)

var (
	errNotionAdminSpaceGroupInvalid   = errors.New("invalid Notion Admin space-group request")
	errNotionAdminSpaceGroupForbidden = notionAdminInvalid(errors.New("Notion Admin space-group request forbidden"), notionAdminScopeChangedReason)
	errNotionAdminSpaceGroupNotFound  = errors.New("Notion Admin space-group target not found")
	errNotionAdminSpaceGroupConflict  = errors.New("Notion Admin space-group conflict")
)

type notionAdminSpaceGroupRouteKind int

const (
	notionAdminSpaceGroupRouteCollection notionAdminSpaceGroupRouteKind = iota + 1
	notionAdminSpaceGroupRouteItem
	notionAdminSpaceGroupRouteMembers
	notionAdminSpaceGroupRouteMember
)

type notionAdminSpaceGroupResponse struct {
	Object string `json:"object"`
	ID     string `json:"id"`
	Name   string `json:"name"`
}

type notionAdminSpaceGroupMemberReference struct {
	Type   string `json:"type"`
	UserID string `json:"user_id"`
}

type notionAdminSpaceGroupMembershipResponse struct {
	Object  string                               `json:"object"`
	GroupID string                               `json:"group_id"`
	Member  notionAdminSpaceGroupMemberReference `json:"member"`
	Role    string                               `json:"role"`
}

type notionAdminSpaceGroupListResponse struct {
	Object     string                          `json:"object"`
	Type       string                          `json:"type"`
	Results    []notionAdminSpaceGroupResponse `json:"results"`
	HasMore    bool                            `json:"has_more"`
	NextCursor *string                         `json:"next_cursor"`
}

type notionAdminSpaceGroupMembershipListResponse struct {
	Object     string                                    `json:"object"`
	Type       string                                    `json:"type"`
	Results    []notionAdminSpaceGroupMembershipResponse `json:"results"`
	HasMore    bool                                      `json:"has_more"`
	NextCursor *string                                   `json:"next_cursor"`
}

type notionAdminSpaceGroupNameRequest struct {
	Name string `json:"name"`
}

type notionAdminSpaceGroupMembershipRequest struct {
	Member notionAdminSpaceGroupMemberReference `json:"member"`
	Role   string                               `json:"role"`
}

type notionAdminSpaceGroupRoleRequest struct {
	Role string `json:"role"`
}

func notionAdminSpaceGroupRoute(path string) (string, string, string, notionAdminSpaceGroupRouteKind, bool) {
	value, found := strings.CutPrefix(path, notionAdminAPISpacesPath+"/")
	parts := strings.Split(value, "/")
	if !found || len(parts) < 2 || parts[0] == "" || parts[1] != "groups" {
		return "", "", "", 0, false
	}
	switch {
	case len(parts) == 2:
		return parts[0], "", "", notionAdminSpaceGroupRouteCollection, true
	case len(parts) == 3 && parts[2] != "":
		return parts[0], parts[2], "", notionAdminSpaceGroupRouteItem, true
	case len(parts) == 4 && parts[2] != "" && parts[3] == "members":
		return parts[0], parts[2], "", notionAdminSpaceGroupRouteMembers, true
	case len(parts) == 6 && parts[2] != "" && parts[3] == "members" &&
		parts[4] == "users" && parts[5] != "":
		return parts[0], parts[2], parts[5], notionAdminSpaceGroupRouteMember, true
	default:
		return "", "", "", 0, false
	}
}

func serveNotionAdminSpaceGroups(
	event *core.RequestEvent,
	rawWorkspaceID, rawGroupID, rawUserID string,
	route notionAdminSpaceGroupRouteKind,
) (bool, error) {
	method := event.Request.Method
	supported := route == notionAdminSpaceGroupRouteCollection && (method == http.MethodGet || method == http.MethodPost) ||
		route == notionAdminSpaceGroupRouteItem && (method == http.MethodGet || method == http.MethodPatch || method == http.MethodDelete) ||
		route == notionAdminSpaceGroupRouteMembers && (method == http.MethodGet || method == http.MethodPost) ||
		route == notionAdminSpaceGroupRouteMember && (method == http.MethodPatch || method == http.MethodDelete)
	if !supported {
		return false, nil
	}
	secret := bearerToken(event.Request)
	release := acquireNotionAdminTokenLifecycleLock(notionAdminTokenHash(secret))
	defer release()
	capability := notionAdminCapabilityPermissionGroupWrite
	if method == http.MethodGet {
		capability = notionAdminCapabilityPermissionGroupRead
	}
	identity, authorized := authorizeNotionAdminPublicAPI(event, secret, capability)
	if !authorized {
		return true, nil
	}
	workspaceID, err := resolveNotionCompatibleWorkspaceID(strings.TrimSpace(rawWorkspaceID))
	if err != nil {
		return true, notionAdminSpaceGroupError(event, notionAdminInvalid(errNotionAdminSpaceGroupInvalid, "The workspace ID should be a UUID."))
	}
	if !identity.CanAccessWorkspace(workspaceID) {
		return true, notionAdminSpaceGroupError(event, errNotionAdminSpaceGroupNotFound)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	groupID := ""
	if route != notionAdminSpaceGroupRouteCollection {
		groupID, err = resolveNotionCompatibleGroupID(strings.TrimSpace(rawGroupID))
		if err != nil {
			return true, notionAdminSpaceGroupError(event, notionAdminInvalid(errNotionAdminSpaceGroupInvalid, "The group ID isn't a group ID."))
		}
	}
	userID := ""
	if route == notionAdminSpaceGroupRouteMember {
		userID, err = resolveNotionCompatibleUserID(strings.TrimSpace(rawUserID))
		if err != nil {
			return true, notionAdminSpaceGroupError(event, notionAdminInvalid(errNotionAdminSpaceGroupInvalid, "The user ID isn't a user ID."))
		}
	}
	if method != http.MethodGet {
		if route == notionAdminSpaceGroupRouteMember || route == notionAdminSpaceGroupRouteMembers {
			return true, mutateNotionAdminSpaceGroupMember(event, secret, identity, workspaceID, groupID, userID, route)
		}
		return true, mutateNotionAdminSpaceGroup(event, secret, identity, workspaceID, groupID, route)
	}
	return true, readNotionAdminSpaceGroup(event, secret, identity, workspaceID, groupID, route)
}

func readNotionAdminSpaceGroup(
	event *core.RequestEvent,
	secret string,
	identity notionAdminIdentity,
	workspaceID, groupID string,
	route notionAdminSpaceGroupRouteKind,
) error {
	if event.Request.ContentLength > 0 {
		return notionAdminSpaceGroupError(event, notionAdminInvalid(errNotionAdminSpaceGroupInvalid, "This request takes no body."))
	}
	if route == notionAdminSpaceGroupRouteItem {
		if len(event.Request.URL.Query()) != 0 {
			return notionAdminSpaceGroupError(event, notionAdminInvalid(errNotionAdminSpaceGroupInvalid, "This request takes no query parameters."))
		}
		response, err := retrieveNotionAdminSpaceGroup(event.Request.Context(), event.App, identity, workspaceID, groupID)
		if err != nil {
			return notionAdminSpaceGroupError(event, err)
		}
		event.Response.Header().Set("Cache-Control", "no-store")
		return event.JSON(http.StatusOK, response)
	}
	pageSize, rawCursor, err := notionAdminSpaceGroupListQuery(event.Request)
	if err != nil {
		return notionAdminSpaceGroupError(event, err)
	}
	kind := notionAdminSpaceGroupCursorGroups
	if route == notionAdminSpaceGroupRouteMembers {
		kind = notionAdminSpaceGroupCursorMembers
	}
	cursor, err := decodeNotionAdminSpaceGroupCursor(rawCursor, secret, identity, workspaceID, groupID, kind)
	if err != nil {
		return notionAdminSpaceGroupError(event, err)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	if route == notionAdminSpaceGroupRouteCollection {
		response, err := listNotionAdminSpaceGroups(event.Request.Context(), event.App, secret, identity, workspaceID, cursor, pageSize)
		if err != nil {
			return notionAdminSpaceGroupError(event, err)
		}
		return event.JSON(http.StatusOK, response)
	}
	response, err := listNotionAdminSpaceGroupMembers(event.Request.Context(), event.App, secret, identity, workspaceID, groupID, cursor, pageSize)
	if err != nil {
		return notionAdminSpaceGroupError(event, err)
	}
	return event.JSON(http.StatusOK, response)
}

func mutateNotionAdminSpaceGroup(
	event *core.RequestEvent,
	secret string,
	identity notionAdminIdentity,
	workspaceID, groupID string,
	route notionAdminSpaceGroupRouteKind,
) error {
	if route == notionAdminSpaceGroupRouteCollection {
		if len(event.Request.URL.Query()) != 0 {
			return notionAdminSpaceGroupError(event, notionAdminInvalid(errNotionAdminSpaceGroupInvalid, "This request takes no query parameters."))
		}
		request := notionAdminSpaceGroupNameRequest{}
		if err := decodeNotionAdminSpaceGroupBody(event, &request); err != nil {
			return notionAdminSpaceGroupError(event, err)
		}
		name, err := normalizeNotionAdminSpaceGroupName(request.Name)
		if err != nil {
			return notionAdminSpaceGroupError(event, err)
		}
		response, err := createNotionAdminSpaceGroup(event.Request.Context(), event.App, secret, identity, workspaceID, name)
		if err != nil {
			return notionAdminSpaceGroupError(event, err)
		}
		return event.JSON(http.StatusOK, response)
	}
	if event.Request.Method == http.MethodPatch {
		if len(event.Request.URL.Query()) != 0 {
			return notionAdminSpaceGroupError(event, notionAdminInvalid(errNotionAdminSpaceGroupInvalid, "This request takes no query parameters."))
		}
		request := notionAdminSpaceGroupNameRequest{}
		if err := decodeNotionAdminSpaceGroupBody(event, &request); err != nil {
			return notionAdminSpaceGroupError(event, err)
		}
		name, err := normalizeNotionAdminSpaceGroupName(request.Name)
		if err != nil {
			return notionAdminSpaceGroupError(event, err)
		}
		response, err := updateNotionAdminSpaceGroup(event.Request.Context(), event.App, secret, identity, workspaceID, groupID, name)
		if err != nil {
			return notionAdminSpaceGroupError(event, err)
		}
		return event.JSON(http.StatusOK, response)
	}
	transferID, err := notionAdminSpaceGroupTransferQuery(event.Request)
	if err != nil || event.Request.ContentLength > 0 {
		return notionAdminSpaceGroupError(event, notionAdminInvalid(errNotionAdminSpaceGroupInvalid, "Deleting a group takes no body, and transfer_to_owner_id once if given."))
	}
	response, err := deleteNotionAdminSpaceGroup(event.Request.Context(), event.App, secret, identity, workspaceID, groupID, transferID)
	if err != nil {
		return notionAdminSpaceGroupError(event, err)
	}
	return event.JSON(http.StatusOK, response)
}

func mutateNotionAdminSpaceGroupMember(
	event *core.RequestEvent,
	secret string,
	identity notionAdminIdentity,
	workspaceID, groupID, userID string,
	route notionAdminSpaceGroupRouteKind,
) error {
	if len(event.Request.URL.Query()) != 0 {
		return notionAdminSpaceGroupError(event, notionAdminInvalid(errNotionAdminSpaceGroupInvalid, "This request takes no query parameters."))
	}
	if event.Request.Method == http.MethodDelete {
		if event.Request.ContentLength > 0 {
			return notionAdminSpaceGroupError(event, notionAdminInvalid(errNotionAdminSpaceGroupInvalid, "This request takes no body."))
		}
		response, err := deleteNotionAdminSpaceGroupMember(event.Request.Context(), event.App, secret, identity, workspaceID, groupID, userID)
		if err != nil {
			return notionAdminSpaceGroupError(event, err)
		}
		return event.JSON(http.StatusOK, response)
	}
	role := ""
	if route == notionAdminSpaceGroupRouteMembers {
		request := notionAdminSpaceGroupMembershipRequest{}
		if err := decodeNotionAdminSpaceGroupBody(event, &request); err != nil || request.Member.Type != "user" {
			return notionAdminSpaceGroupError(event, notionAdminInvalid(errNotionAdminSpaceGroupInvalid, "The body needs member with type user."))
		}
		var err error
		userID, err = resolveNotionCompatibleUserID(strings.TrimSpace(request.Member.UserID))
		if err != nil {
			return notionAdminSpaceGroupError(event, notionAdminInvalid(errNotionAdminSpaceGroupInvalid, "member.user_id isn't a user ID."))
		}
		role = strings.TrimSpace(request.Role)
	} else {
		request := notionAdminSpaceGroupRoleRequest{}
		if err := decodeNotionAdminSpaceGroupBody(event, &request); err != nil {
			return notionAdminSpaceGroupError(event, err)
		}
		role = strings.TrimSpace(request.Role)
	}
	if role != notionAdminSpaceGroupRoleOwner && role != notionAdminSpaceGroupRoleMember {
		return notionAdminSpaceGroupError(event, notionAdminInvalid(errNotionAdminSpaceGroupInvalid, "role takes owner or member."))
	}
	response, err := upsertNotionAdminSpaceGroupMember(
		event.Request.Context(), event.App, secret, identity, workspaceID, groupID,
		userID, role, route == notionAdminSpaceGroupRouteMembers,
	)
	if err != nil {
		return notionAdminSpaceGroupError(event, err)
	}
	return event.JSON(http.StatusOK, response)
}

func decodeNotionAdminSpaceGroupBody(event *core.RequestEvent, target any) error {
	event.Request.Body = http.MaxBytesReader(event.Response, event.Request.Body, notionAdminSpaceGroupMaxBodyBytes)
	decoder := json.NewDecoder(event.Request.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(target) != nil {
		return notionAdminInvalid(errNotionAdminSpaceGroupInvalid, "The body has a field this request doesn't take, or a value of the wrong kind.")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return notionAdminInvalid(errNotionAdminSpaceGroupInvalid, "The body holds more than one JSON value.")
	}
	return nil
}

func normalizeNotionAdminSpaceGroupName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" || utf8.RuneCountInString(name) > notionAdminSpaceGroupMaxNameRunes {
		return "", notionAdminInvalid(errNotionAdminSpaceGroupInvalid, fmt.Sprintf("name must be non-empty and at most %d characters.", notionAdminSpaceGroupMaxNameRunes))
	}
	return name, nil
}

func notionAdminSpaceGroupTransferQuery(request *http.Request) (string, error) {
	query := request.URL.Query()
	for key, values := range query {
		if key != "transfer_to_owner_id" || len(values) != 1 || values[0] == "" {
			return "", notionAdminInvalid(errNotionAdminSpaceGroupInvalid, "The query takes transfer_to_owner_id once, not empty.")
		}
	}
	raw := query.Get("transfer_to_owner_id")
	if raw == "" {
		return "", nil
	}
	userID, err := resolveNotionCompatibleUserID(strings.TrimSpace(raw))
	if err != nil {
		return "", notionAdminInvalid(errNotionAdminSpaceGroupInvalid, "transfer_to_owner_id isn't a user ID.")
	}
	return userID, nil
}

func notionAdminSpaceGroupError(event *core.RequestEvent, err error) error {
	switch {
	case errors.Is(err, errNotionAdminTokenUnauthorized):
		return notionAdminPublicAPIError(event, http.StatusUnauthorized, notionAdminErrorUnauthorized, notionAdminUnauthorizedMessage, err)
	case errors.Is(err, errNotionAdminSpaceGroupForbidden):
		return notionAdminPublicAPIError(event, http.StatusForbidden, notionErrorRestrictedResource, notionAdminRefusalReason(err, notionAdminForbiddenMessage), err)
	case errors.Is(err, errNotionAdminSpaceGroupInvalid), errors.Is(err, errInvalidNotionAdminSpaceGroupCursor):
		return notionAdminPublicAPIError(event, http.StatusBadRequest, notionAdminErrorValidation, notionAdminValidationReason(err), err)
	case errors.Is(err, errNotionAdminSpaceGroupNotFound):
		return notionAdminPublicAPIError(event, http.StatusNotFound, notionErrorObjectNotFound, notionAdminNotFoundMessage, err)
	case errors.Is(err, errNotionAdminSpaceGroupConflict):
		return notionAdminPublicAPIError(event, http.StatusConflict, "conflict_error", notionAdminRefusalReason(err, "Conflict."), err)
	default:
		return notionAdminPublicAPIError(event, http.StatusInternalServerError, notionAdminErrorInternal, notionAdminInternalErrorMessage, err)
	}
}
