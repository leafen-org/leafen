package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// The settings' "연결" item, as Notion's (seen 2026-10-02): the connections
// added to a workspace — here the apps that reached it through the
// Notion-compatible OAuth — each with who added it and how many pages it may
// reach, disconnected by whoever added it or by the workspace's owner. For
// the owner it also counts the connections and the working personal access
// tokens, as Notion's "관리" does; the owner lists every token through the
// personal access token routes (personal_access_tokens.go), and every agent
// key with ?kind=agent-keys (workspace_agent_keys.go).

const (
	workspaceConnectionsPath          = "/workspaces/{workspaceId}/connections"
	workspaceConnectionDisconnectPath = workspaceConnectionsPath + "/{grantId}/disconnect"
	workspaceConnectionsListWindow    = 200
	workspaceConnectionBodyBytes      = 4 << 10

	errorCodeWorkspaceConnectionNotFound = "LEAFEN_WORKSPACE_CONNECTION_NOT_FOUND"
	errorCodeWorkspaceConnectionFailed   = "LEAFEN_WORKSPACE_CONNECTION_FAILED"
)

var errWorkspaceConnectionNotFound = errors.New(errorCodeWorkspaceConnectionNotFound)

type workspaceConnectionPerson struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type workspaceConnectionResponse struct {
	ID          string                    `json:"id"`
	Name        string                    `json:"name"`
	InstalledBy workspaceConnectionPerson `json:"installedBy"`
	CreatedAt   int64                     `json:"createdAt"`
	// The pages chosen when it was added; 0 means every page its person reaches.
	Pages         int  `json:"pages"`
	CanDisconnect bool `json:"canDisconnect"`
}

type workspaceConnectionsManagement struct {
	Connections                int `json:"connections"`
	ActivePersonalAccessTokens int `json:"activePersonalAccessTokens"`
}

type workspaceConnectionsResponse struct {
	Items []workspaceConnectionResponse `json:"items"`
	// With it, more follow from ?cursor=.
	NextCursor string `json:"nextCursor,omitempty"`
	// Only for the workspace's owner.
	Manage *workspaceConnectionsManagement `json:"manage,omitempty"`
}

type storedWorkspaceConnection struct {
	ID         string `db:"id"`
	ClientName string `db:"client_name"`
	OwnerID    string `db:"owner_id"`
	OwnerName  string `db:"owner_name"`
	CreatedAt  int64  `db:"created_at"`
	Pages      int    `db:"pages"`
}

func registerWorkspaceConnectionRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+workspaceConnectionsPath, listWorkspaceConnections)
	event.Router.POST(apiRoot+workspaceConnectionDisconnectPath, disconnectWorkspaceConnection).
		Bind(apis.BodyLimit(workspaceConnectionBodyBytes))
}

func listWorkspaceConnections(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	authorization, err := authorizeWorkspace(event.App, user.Id, workspaceID, permissionWorkspaceRead)
	if err != nil {
		return workspaceConnectionError(event, err, "list")
	}
	// Its agent keys, for its owner (workspace_agent_keys.go).
	if event.Request.URL.Query().Get("kind") == workspaceAgentKeysKind {
		return listWorkspaceAgentKeys(event, authorization)
	}
	owner := authorization.Role == workspaceRoleOwner
	after, err := decodeTimeIDCursor(event.Request.URL.Query().Get("cursor"))
	if err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeWorkspaceConnectionInvalid, "Give the nextCursor of the last list, or none to start.")
	}
	rows := []storedWorkspaceConnection{}
	if err := event.App.DB().NewQuery(`
		SELECT [[grant]].[[id]], [[grant]].[[client_name]], [[grant]].[[owner_id]],
			COALESCE([[person]].[[name]], '') AS [[owner_name]], [[grant]].[[created_at]],
			(SELECT COUNT(*) FROM {{leafen_notion_oauth_grant_resources}} [[resource]]
				WHERE [[resource]].[[grant_id]] = [[grant]].[[id]]) AS [[pages]]
		FROM {{leafen_notion_oauth_grants}} [[grant]]
		LEFT JOIN {{users}} [[person]] ON [[person]].[[id]] = [[grant]].[[owner_id]]
		WHERE [[grant]].[[workspace_id]] = {:workspaceId} AND [[grant]].[[status]] = 'active'
			AND ({:afterId} = '' OR [[grant]].[[created_at]] < {:afterAt}
				OR ([[grant]].[[created_at]] = {:afterAt} AND [[grant]].[[id]] < {:afterId}))
		ORDER BY [[grant]].[[created_at]] DESC, [[grant]].[[id]] DESC
		LIMIT {:limit}
	`).Bind(dbx.Params{"workspaceId": workspaceID, "afterAt": after.At, "afterId": after.ID,
		"limit": workspaceConnectionsListWindow + 1}).All(&rows); err != nil {
		return workspaceConnectionError(event, err, "list")
	}
	response := workspaceConnectionsResponse{Items: make([]workspaceConnectionResponse, 0, len(rows))}
	if len(rows) > workspaceConnectionsListWindow {
		rows = rows[:workspaceConnectionsListWindow]
		response.NextCursor = encodeTimeIDCursor(rows[len(rows)-1].CreatedAt, rows[len(rows)-1].ID)
	}
	for _, row := range rows {
		response.Items = append(response.Items, workspaceConnectionResponse{
			ID: row.ID, Name: row.ClientName, CreatedAt: row.CreatedAt, Pages: row.Pages,
			InstalledBy:   workspaceConnectionPerson{ID: row.OwnerID, Name: row.OwnerName},
			CanDisconnect: owner || row.OwnerID == user.Id,
		})
	}
	if owner {
		manage := workspaceConnectionsManagement{}
		if err := event.App.DB().NewQuery(`
			SELECT
				(SELECT COUNT(*) FROM {{leafen_notion_oauth_grants}}
					WHERE [[workspace_id]] = {:workspaceId} AND [[status]] = 'active') AS [[connections]],
				(SELECT COUNT(*) FROM {{leafen_notion_personal_access_tokens}}
					WHERE [[workspace_id]] = {:workspaceId} AND [[status]] = 'active' AND [[expires_at]] > {:now})
					AS [[tokens]]
		`).Bind(dbx.Params{"workspaceId": workspaceID, "now": time.Now().UTC().UnixMilli()}).
			Row(&manage.Connections, &manage.ActivePersonalAccessTokens); err != nil {
			return workspaceConnectionError(event, err, "list")
		}
		response.Manage = &manage
	}
	return event.JSON(http.StatusOK, response)
}

func disconnectWorkspaceConnection(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	if event.Request.URL.Query().Get("kind") == workspaceAgentKeyKind {
		return disconnectWorkspaceAgentKey(event, user.Id)
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	grantID := strings.TrimSpace(event.Request.PathValue("grantId"))
	err := event.App.RunInTransaction(func(txApp core.App) error {
		authorization, err := authorizeWorkspace(txApp, user.Id, workspaceID, permissionWorkspaceRead)
		if err != nil {
			return err
		}
		grant, found, err := findNotionOAuthGrantByID(txApp, grantID)
		if err != nil {
			return err
		}
		if !found || grant.WorkspaceID != workspaceID || grant.Status != "active" ||
			(grant.OwnerID != user.Id && authorization.Role != workspaceRoleOwner) {
			return errWorkspaceConnectionNotFound
		}
		now := time.Now().UTC().UnixMilli()
		if err := revokeNotionOAuthGrant(txApp, grant.ID, now); err != nil {
			return err
		}
		return insertOrganizationAuditEvent(txApp, organizationAuditEvent{
			OrganizationID: authorization.Organization.OrganizationID, WorkspaceID: workspaceID,
			ActorID: user.Id, Action: "connection.disconnect", TargetType: "connection", TargetID: grant.ID,
			Metadata: map[string]any{"clientId": grant.ClientID}, OccurredAt: now,
		})
	})
	if err != nil {
		return workspaceConnectionError(event, err, "disconnect")
	}
	return event.NoContent(http.StatusNoContent)
}

func workspaceConnectionError(event *core.RequestEvent, err error, stage string) error {
	status, code, message := http.StatusInternalServerError,
		errorCodeWorkspaceConnectionFailed, "Couldn't complete the connection request."
	if errors.Is(err, errWorkspaceConnectionNotFound) || isPermissionDenied(err) || errors.Is(err, sql.ErrNoRows) {
		status, code, message = http.StatusNotFound,
			errorCodeWorkspaceConnectionNotFound, "Connection not found."
	}
	return writeAPIError(
		event, status, code, message, "workspace_connection", stage, code, err,
		diagnosticSourcePosition(1),
	)
}
