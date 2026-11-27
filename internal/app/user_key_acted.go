package app

import (
	"net/http"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// What a call made with a key leaves once it went through: a change is in
// the audit log of the organizations it touched, and a call an agent made
// through Leafen's MCP names, in the tool's answer, the workspaces it worked
// in (mcp_answer_workspaces.go).

// recordUserKeyChange puts a change made with a key in the audit log of the
// organizations of the workspaces it touched: the person, through the key's
// connection, with the method, path and outcome.
func recordUserKeyChange(event *core.RequestEvent, key userKeyAuthority, workspaceIDs []string) {
	for _, workspaceID := range workspaceIDs {
		var organizationID string
		if err := event.App.DB().NewQuery(`SELECT [[organization]] FROM {{leafen_workspaces}} WHERE [[id]] = {:id}`).
			Bind(dbx.Params{"id": workspaceID}).Row(&organizationID); err != nil || organizationID == "" {
			continue
		}
		if err := insertOrganizationAuditEvent(event.App, organizationAuditEvent{
			OrganizationID: organizationID, WorkspaceID: workspaceID, ActorID: key.OwnerID,
			Action: "connection.request", TargetType: "connection", TargetID: key.ConnectionID,
			Metadata: map[string]any{
				"connection": key.Name, "method": event.Request.Method,
				"path": event.Request.URL.Path, "status": event.Status(),
			},
		}); err != nil {
			event.App.Logger().Warn("leafen key change audit failed", "error", err)
		}
	}
}

// noteUserKeyWorkedIn gives an MCP call, every Leafen operation it ran
// that went through, the workspaces the operation worked in: where it acts
// and what else it reaches — a copy's or a move's destination, a source it
// reads. leafen_call and every tool made of operations name them so, by the
// key's own check of where a call acts, which no operation goes around.
func noteUserKeyWorkedIn(event *core.RequestEvent, key userKeyAuthority, workspaceIDs ...[]string) {
	if key.MCP == nil || event.Status() >= http.StatusBadRequest {
		return
	}
	for _, ids := range workspaceIDs {
		for _, workspaceID := range ids {
			key.MCP.Authority.reaches(workspaceID)
		}
	}
}
