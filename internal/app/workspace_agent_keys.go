package app

import (
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A workspace's owner sees every agent key that reaches the workspace —
// whose it is, what it may do, when it was made and last used, and when it
// ends or that it renews itself — and ends any of them, as Notion's "모든
// 개인 액세스 토큰" (agent key decision of 2026-11-16). A key's value is
// never shown here. The list is the settings' "연결" route's
// (?kind=agent-keys), and ending one its disconnect's (?kind=agent-key).
// Another person's key is ended by the owner signed in, never by a key.

const (
	workspaceAgentKeysKind = "agent-keys"
	workspaceAgentKeyKind  = "agent-key"
	workspaceAgentKeysMost = 500
)

type workspaceAgentKeyResponse struct {
	ID     string                    `json:"id"`
	Name   string                    `json:"name"`
	Person workspaceConnectionPerson `json:"person"`
	// An agent connection, its person's in all their workspaces, rather than
	// an MCP token (connection_person_rights.go).
	Agent bool `json:"agent"`
	// An OAuth connection renews itself while its agent uses it, and has no end said.
	OAuth      bool  `json:"oauth"`
	CreatedAt  int64 `json:"createdAt"`
	LastUsedAt int64 `json:"lastUsedAt,omitempty"`
	ExpiresAt  int64 `json:"expiresAt,omitempty"`
	// "active", or "expired" for a key past its end.
	Status string `json:"status"`
}

type workspaceAgentKeysResponse struct {
	Items []workspaceAgentKeyResponse `json:"items"`
}

func listWorkspaceAgentKeys(event *core.RequestEvent, authorization workspaceAuthorization) error {
	if authorization.Role != workspaceRoleOwner {
		return workspaceConnectionError(event, errWorkspaceConnectionNotFound, "agent-keys")
	}
	rows := []struct {
		ID         string `db:"id"`
		Name       string `db:"client_name"`
		OwnerID    string `db:"owner_id"`
		OwnerName  string `db:"owner_name"`
		Agent      bool   `db:"agent_connection"`
		OAuth      bool   `db:"oauth"`
		Created    int64  `db:"created"`
		LastUsedAt int64  `db:"last_used_at"`
		ExpiresAt  int64  `db:"token_expires_at"`
	}{}
	now := time.Now().UnixMilli()
	if err := event.App.DB().NewQuery(`
		SELECT [[connection]].[[id]], [[connection]].[[client_name]], [[connection]].[[owner_id]],
			COALESCE([[person]].[[name]], '') AS [[owner_name]], [[connection]].[[agent_connection]],
			[[connection]].[[oauth_client_id]] <> '' AS [[oauth]], [[connection]].[[created]],
			[[connection]].[[last_used_at]], [[connection]].[[token_expires_at]]
		FROM {{leafen_mcp_connections}} [[connection]]
		JOIN {{leafen_mcp_connection_workspaces}} [[scope]] ON [[scope]].[[connection_id]] = [[connection]].[[id]]
		LEFT JOIN {{users}} [[person]] ON [[person]].[[id]] = [[connection]].[[owner_id]]
		WHERE [[scope]].[[workspace_id]] = {:workspaceId} AND [[connection]].[[revoked_at]] = 0
			AND [[connection]].[[app_install_id]] = ''
			AND ([[connection]].[[oauth_client_id]] = '' OR [[connection]].[[refresh_expires_at]] > {:now})
		ORDER BY [[connection]].[[created]] DESC, [[connection]].[[id]] DESC
		LIMIT {:limit}
	`).Bind(dbx.Params{"workspaceId": authorization.WorkspaceID, "now": now, "limit": workspaceAgentKeysMost}).All(&rows); err != nil {
		return workspaceConnectionError(event, err, "agent-keys")
	}
	response := workspaceAgentKeysResponse{Items: make([]workspaceAgentKeyResponse, 0, len(rows))}
	for _, row := range rows {
		item := workspaceAgentKeyResponse{
			ID: row.ID, Name: row.Name, Person: workspaceConnectionPerson{ID: row.OwnerID, Name: row.OwnerName},
			Agent: row.Agent, OAuth: row.OAuth, CreatedAt: row.Created, LastUsedAt: row.LastUsedAt, Status: "active",
		}
		if !row.OAuth {
			item.ExpiresAt = row.ExpiresAt
			if row.ExpiresAt > 0 && row.ExpiresAt <= now {
				item.Status = "expired"
			}
		}
		response.Items = append(response.Items, item)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, response)
}

// disconnectWorkspaceAgentKey ends a key that reaches the workspace: its
// person's own, or any, by the workspace's owner.
func disconnectWorkspaceAgentKey(event *core.RequestEvent, userID string) error {
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	keyID := strings.TrimSpace(event.Request.PathValue("grantId"))
	err := event.App.RunInTransaction(func(txApp core.App) error {
		authorization, err := authorizeWorkspace(txApp, userID, workspaceID, permissionWorkspaceRead)
		if err != nil {
			return err
		}
		var ownerID string
		if err := txApp.DB().NewQuery(`SELECT [[connection]].[[owner_id]] FROM {{leafen_mcp_connections}} [[connection]]
			JOIN {{leafen_mcp_connection_workspaces}} [[scope]] ON [[scope]].[[connection_id]] = [[connection]].[[id]]
			WHERE [[connection]].[[id]] = {:id} AND [[scope]].[[workspace_id]] = {:workspaceId}
				AND [[connection]].[[revoked_at]] = 0 AND [[connection]].[[app_install_id]] = ''`).
			Bind(dbx.Params{"id": keyID, "workspaceId": workspaceID}).Row(&ownerID); err != nil {
			return err
		}
		_, keyed := userKeyOf(event)
		if ownerID != userID && (authorization.Role != workspaceRoleOwner || keyed) {
			return errWorkspaceConnectionNotFound
		}
		now := time.Now().UnixMilli()
		if _, found, err := revokeMCPConnectionsInTransaction(txApp,
			mcpConnectionRevocationFilter{OwnerID: ownerID, ClientKey: keyID, WorkspaceID: workspaceID}, now); err != nil {
			return err
		} else if !found {
			return errWorkspaceConnectionNotFound
		}
		return insertOrganizationAuditEvent(txApp, organizationAuditEvent{
			OrganizationID: authorization.Organization.OrganizationID, WorkspaceID: workspaceID,
			ActorID: userID, Action: "connection.disconnect", TargetType: "connection", TargetID: keyID,
			Metadata: map[string]any{"kind": workspaceAgentKeyKind, "person": ownerID}, OccurredAt: now,
		})
	})
	if err != nil {
		return workspaceConnectionError(event, err, "disconnect")
	}
	return event.NoContent(http.StatusNoContent)
}
