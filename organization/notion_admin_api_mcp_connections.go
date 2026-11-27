package organization

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/pocketbase/core"
)

const notionAdminMCPConnectionsMaxBodyBytes = 64 << 10

var (
	errNotionAdminMCPConnectionsInvalid   = errors.New("invalid Notion Admin MCP client request")
	errNotionAdminMCPConnectionsNotFound  = errors.New("Notion Admin MCP client target not found")
	errNotionAdminMCPConnectionsForbidden = leafen.NotionAdminInvalid(errors.New("Notion Admin MCP client request forbidden"), leafen.NotionAdminScopeChanged)
)

type notionAdminMCPEnterpriseAccessRequest struct {
	Access      string `json:"access"`
	UserID      string `json:"user_id"`
	WorkspaceID string `json:"workspace_id"`
}

type notionAdminMCPRevokeRequest struct {
	ClientKey   string `json:"client_key"`
	UserID      string `json:"user_id"`
	WorkspaceID string `json:"workspace_id"`
}

func serveNotionAdminMCPConnections(event *core.RequestEvent) (bool, error) {
	path := event.Request.URL.Path
	switch {
	case path == leafen.NotionAdminAPIMCPConnectionsPath && event.Request.Method == http.MethodGet:
		return true, serveNotionAdminMCPConnectionsList(event)
	case path == leafen.NotionAdminAPIMCPConnectionsPath+"/enterprise_managed_access" &&
		event.Request.Method == http.MethodPut:
		return true, updateNotionAdminMCPEnterpriseAccess(event)
	case path == leafen.NotionAdminAPIMCPConnectionsPath+"/revoke" &&
		event.Request.Method == http.MethodPost:
		return true, revokeNotionAdminMCPConnections(event)
	default:
		return false, nil
	}
}

func serveNotionAdminMCPConnectionsList(event *core.RequestEvent) error {
	secret := leafen.BearerToken(event.Request)
	release := leafen.AcquireNotionAdminTokenLifecycleLock(leafen.NotionAdminTokenHash(secret))
	defer release()
	identity, authorized := leafen.AuthorizeNotionAdminAPI(
		event, secret, leafen.NotionAdminCapabilityMCPClientConnectionRead,
	)
	if !authorized {
		return nil
	}
	if event.Request.ContentLength > 0 {
		return notionAdminMCPConnectionsError(event, leafen.NotionAdminInvalid(errNotionAdminMCPConnectionsInvalid, "This request takes no body."))
	}
	query, err := parseNotionAdminMCPConnectionsQuery(event.Request, identity)
	if err != nil {
		return notionAdminMCPConnectionsError(event, err)
	}
	cursor, err := decodeNotionAdminMCPConnectionsCursor(query.Cursor, secret, identity, query)
	if err != nil {
		return notionAdminMCPConnectionsError(event, err)
	}
	page, err := listNotionAdminMCPConnections(
		event.Request.Context(), event.App, identity, query, cursor,
	)
	if err != nil {
		return notionAdminMCPConnectionsError(event, err)
	}
	response := map[string]any{"has_more": page.HasMore, "results": page.Results}
	if page.HasMore {
		nextCursor, err := encodeNotionAdminMCPConnectionsCursor(
			secret, identity, query, page.AuthorityRevision, page.LastConnectionID,
		)
		if err != nil {
			return notionAdminMCPConnectionsError(event, err)
		}
		response["next_cursor"] = nextCursor
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, response)
}

func updateNotionAdminMCPEnterpriseAccess(event *core.RequestEvent) error {
	secret := leafen.BearerToken(event.Request)
	release := leafen.AcquireNotionAdminTokenLifecycleLock(leafen.NotionAdminTokenHash(secret))
	defer release()
	identity, authorized := leafen.AuthorizeNotionAdminAPI(
		event, secret, leafen.NotionAdminCapabilityMCPClientConnectionWriteHighImpact,
	)
	if !authorized {
		return nil
	}
	if len(event.Request.URL.Query()) != 0 {
		return notionAdminMCPConnectionsError(event, leafen.NotionAdminInvalid(errNotionAdminMCPConnectionsInvalid, "This request takes no query parameters."))
	}
	request := notionAdminMCPEnterpriseAccessRequest{}
	if err := decodeNotionAdminMCPConnectionsBody(event, &request); err != nil {
		return notionAdminMCPConnectionsError(event, err)
	}
	request.Access = strings.TrimSpace(request.Access)
	userID, workspaceID, err := normalizeNotionAdminMCPGovernanceTarget(request.UserID, request.WorkspaceID)
	if err != nil || request.Access != mcpEnterpriseAccessAllowed && request.Access != mcpEnterpriseAccessDenied {
		return notionAdminMCPConnectionsError(event, leafen.NotionAdminInvalid(errNotionAdminMCPConnectionsInvalid, "user_id and workspace_id must be valid IDs, and access takes allowed or denied."))
	}
	revoked, err := updateNotionAdminMCPEnterpriseAccessInTransaction(
		event.Request.Context(), event.App, secret, identity,
		request.Access, userID, workspaceID,
	)
	if err != nil {
		return notionAdminMCPConnectionsError(event, err)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, map[string]any{
		"access": request.Access, "revoked_token_count": revoked,
	})
}

func revokeNotionAdminMCPConnections(event *core.RequestEvent) error {
	secret := leafen.BearerToken(event.Request)
	release := leafen.AcquireNotionAdminTokenLifecycleLock(leafen.NotionAdminTokenHash(secret))
	defer release()
	identity, authorized := leafen.AuthorizeNotionAdminAPI(
		event, secret, leafen.NotionAdminCapabilityMCPClientConnectionWriteHighImpact,
	)
	if !authorized {
		return nil
	}
	if len(event.Request.URL.Query()) != 0 {
		return notionAdminMCPConnectionsError(event, leafen.NotionAdminInvalid(errNotionAdminMCPConnectionsInvalid, "This request takes no query parameters."))
	}
	request := notionAdminMCPRevokeRequest{}
	if err := decodeNotionAdminMCPConnectionsBody(event, &request); err != nil {
		return notionAdminMCPConnectionsError(event, err)
	}
	clientKey := strings.TrimSpace(request.ClientKey)
	userID, workspaceID, err := normalizeNotionAdminMCPGovernanceTarget(request.UserID, request.WorkspaceID)
	if err != nil || clientKey == "" || len(clientKey) > leafen.MCPGovernanceMaxClientIDRunes {
		return notionAdminMCPConnectionsError(event, leafen.NotionAdminInvalid(errNotionAdminMCPConnectionsInvalid, fmt.Sprintf("user_id and workspace_id must be valid IDs, and client_key non-empty, at most %d characters.", leafen.MCPGovernanceMaxClientIDRunes)))
	}
	revoked, err := revokeNotionAdminMCPConnectionsInTransaction(
		event.Request.Context(), event.App, secret, identity, clientKey, userID, workspaceID,
	)
	if err != nil {
		return notionAdminMCPConnectionsError(event, err)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, map[string]any{"revoked_token_count": revoked})
}

func decodeNotionAdminMCPConnectionsBody(event *core.RequestEvent, target any) error {
	event.Request.Body = http.MaxBytesReader(
		event.Response, event.Request.Body, notionAdminMCPConnectionsMaxBodyBytes,
	)
	decoder := json.NewDecoder(event.Request.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(target) != nil {
		return leafen.NotionAdminInvalid(errNotionAdminMCPConnectionsInvalid, "The body has a field this request doesn't take, or a value of the wrong kind.")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return leafen.NotionAdminInvalid(errNotionAdminMCPConnectionsInvalid, "The body holds more than one JSON value.")
	}
	return nil
}

func normalizeNotionAdminMCPGovernanceTarget(rawUserID, rawWorkspaceID string) (string, string, error) {
	userID, err := leafen.ResolveNotionCompatibleUserID(strings.TrimSpace(rawUserID))
	if err != nil {
		return "", "", leafen.NotionAdminInvalid(errNotionAdminMCPConnectionsInvalid, "user_id isn't a user ID.")
	}
	workspaceID, err := leafen.ResolveNotionCompatibleWorkspaceID(strings.TrimSpace(rawWorkspaceID))
	if err != nil {
		return "", "", leafen.NotionAdminInvalid(errNotionAdminMCPConnectionsInvalid, "workspace_id should be a UUID.")
	}
	return userID, workspaceID, nil
}

func revokeNotionAdminMCPConnectionSet(
	app core.App,
	filter leafen.MCPConnectionRevocationFilter,
	now int64,
) (int, bool, error) {
	return leafen.RevokeMCPConnectionsInTransaction(app, filter, now)
}

func notionAdminMCPConnectionsError(event *core.RequestEvent, err error) error {
	switch {
	case errors.Is(err, leafen.ErrNotionAdminTokenUnauthorized):
		return leafen.NotionAdminAPIError(event, http.StatusUnauthorized, leafen.NotionAdminErrorUnauthorized, leafen.NotionAdminUnauthorizedMessage, err)
	case errors.Is(err, errNotionAdminMCPConnectionsForbidden):
		return leafen.NotionAdminAPIError(event, http.StatusForbidden, leafen.NotionAdminErrorForbidden, leafen.NotionAdminRefusalReason(err, leafen.NotionAdminForbiddenMessage), err)
	case errors.Is(err, errNotionAdminMCPConnectionsNotFound):
		return leafen.NotionAdminAPIError(event, http.StatusNotFound, leafen.NotionAdminErrorNotFound, leafen.NotionAdminNotFoundMessage, err)
	case errors.Is(err, errNotionAdminMCPConnectionsInvalid), errors.Is(err, errInvalidNotionAdminMCPConnectionsCursor):
		return leafen.NotionAdminAPIError(event, http.StatusBadRequest, leafen.NotionAdminErrorValidation, leafen.NotionAdminValidationReason(err), err)
	default:
		return leafen.NotionAdminAPIError(event, http.StatusInternalServerError, leafen.NotionAdminErrorInternal, leafen.NotionAdminInternalErrorMessage, err)
	}
}
