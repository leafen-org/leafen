package app

import (
	"encoding/json"
	"slices"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The person's answer to an agent's request to connect
// (mcp_device_connections.go): what allowing it gives, checked before they
// are asked, and their answer kept with its record in the audit.

// mcpDevicePlan is what the person allows: an agent connection as whom, in
// all their workspaces (connection_person_rights.go), as they are when asked.
type mcpDevicePlan struct {
	OwnerID      string
	WorkspaceIDs []string
}

// checkMCPDevicePlan refuses now, where the person hears it, what the
// connection's making would refuse.
func checkMCPDevicePlan(app core.App, request mcpDeviceConnection, plan mcpDevicePlan) error {
	now := time.Now().UnixMilli()
	expiresAt, err := mcpTokenExpiry(app, plan.WorkspaceIDs, now, "")
	if err != nil {
		return err
	}
	if err := authorizeManualMCPCompletion(app, plan.OwnerID, "device:"+request.ID, mcpConnectionInput{
		RequestID: "device:" + request.ID, ClientName: request.ClientName, WorkspaceIDs: plan.WorkspaceIDs,
	}, now, expiresAt); err != nil {
		return err
	}
	connections, err := countActiveMCPConnections(app, plan.OwnerID, now)
	if err != nil {
		return err
	}
	if connections >= maxActiveMCPConnectionsPerOwner {
		return errMCPConnectionCapacity
	}
	return nil
}

// decideMCPDeviceConnection keeps the person's answer to a request: passed
// on from the conversation, or on Leafen's screen; by the connection that
// asked them, if any. Allowed, it waits another ten minutes for its agent's
// poll.
func decideMCPDeviceConnection(app core.App, request mcpDeviceConnection, yes bool, plan mcpDevicePlan, byConnection, in string) error {
	now := time.Now().UnixMilli()
	status, scope, workspaces := "declined", "", []byte("[]")
	if yes {
		status, scope = "approved", personConnectionScopeName
		workspaces, _ = json.Marshal(plan.WorkspaceIDs)
	}
	result, err := app.DB().NewQuery(`UPDATE {{leafen_mcp_device_connections}} SET [[status]] = {:status}, [[owner_id]] = {:owner},
		[[workspace_ids]] = {:workspaces}, [[scope]] = {:scope}, [[decided_by_connection]] = {:by}, [[decided_in]] = {:in},
		[[decided_at]] = {:now}, [[expires_at]] = {:until}
		WHERE [[id]] = {:id} AND [[status]] = 'pending' AND [[expires_at]] > {:now}`).Bind(dbx.Params{
		"id": request.ID, "status": status, "owner": plan.OwnerID, "workspaces": string(workspaces), "scope": scope,
		"by": byConnection, "in": in, "now": now, "until": now + mcpDeviceConnectionLifetime.Milliseconds(),
	}).Execute()
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return errMCPDeviceConnectionMissing
	}
	recordMCPDeviceDecision(app, request, yes, plan, byConnection, in)
	return nil
}

// recordMCPDeviceDecision writes the answer in the audit of the
// organizations of the workspaces it names; the request's row is the record
// on a server without one.
func recordMCPDeviceDecision(app core.App, request mcpDeviceConnection, yes bool, plan mcpDevicePlan, byConnection, in string) {
	organizations := []string{}
	for _, workspaceID := range plan.WorkspaceIDs {
		organization := ""
		if err := app.DB().NewQuery(`SELECT [[organization]] FROM {{leafen_workspaces}} WHERE [[id]] = {:id}`).
			Bind(dbx.Params{"id": workspaceID}).Row(&organization); err == nil && organization != "" && !slices.Contains(organizations, organization) {
			organizations = append(organizations, organization)
		}
	}
	action := "agent.connection.request_declined"
	if yes {
		action = "agent.connection.request_allowed"
	}
	for _, organizationID := range organizations {
		if err := insertOrganizationAuditEvent(app, organizationAuditEvent{
			OrganizationID: organizationID, ActorID: plan.OwnerID, Action: action, TargetType: "mcp_connection_request", TargetID: request.ID,
			Metadata: map[string]any{"client": appOneLine(request.ClientName, 80), "from": request.RequestIP, "askedAt": request.CreatedAt,
				"workspaces": plan.WorkspaceIDs, "decidedIn": in, "connection": byConnection},
		}); err != nil {
			app.Logger().Warn("leafen agent connection request audit failed", "request", request.ID, "error", err)
		}
	}
}
