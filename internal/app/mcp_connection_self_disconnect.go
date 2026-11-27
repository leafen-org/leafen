package app

import (
	"net/http"
	"slices"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// An agent disconnects its own earlier connection to this Leafen with that
// connection's own key (2026-11-20, the user: when a new connection text
// puts Leafen beside an earlier one the agent no longer uses, it asks the
// person once whether to remove it and, on their yes, removes it from its
// settings and disconnects it here, instead of telling them to do it in
// Settings; never on its own — the 2026-11-19 rule that no text removes or
// overwrites a connection unasked stands). A key reaches only its own
// connection here, whatever its scope and workspaces: it can only narrow
// what an agent reaches. Its person disconnects any connection, signed in,
// in Settings › Features › Agents › Connected agents (DELETE
// /mcp/connections/{id}).

const mcpOwnConnectionPath = "/mcp/connections/current"

func registerMCPOwnConnectionRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+mcpOwnConnectionPath, checkOwnMCPConnection)
	event.Router.DELETE(apiRoot+mcpOwnConnectionPath, disconnectOwnMCPConnection)
}

// checkOwnMCPConnection is the light check a connect command makes, before
// it takes a new key, of a connection an agent holds to this Leafen already
// (agent_connect_reuse.go, user 2026-11-20: a pasted text added a third
// connection beside one that worked): sent with that connection's key, it
// answers 204 when the key is a live connection that reaches all its
// person's workspaces (?all=1, as an agent connection does), every
// workspace named (?workspace=, again for each, as a check before 2026-11-26
// asked) and at least the scope named (?scope=), and works as the person
// named (?person=, the one connecting now: 2026-11-27, another person's
// entry in the same agent settings was kept, and the agent worked as them);
// 403 when it reaches less or is another person's, which the command goes in
// beside and never asks to remove; and, as every call with a key Leafen
// refuses, 401. It changes nothing, and names nothing the key's agent
// couldn't read already.
func checkOwnMCPConnection(event *core.RequestEvent) error {
	key, keyed := userKeyOf(event)
	if !keyed || key.AppInstallID != "" || key.ConnectionID == "" {
		return apiError(event, http.StatusNotFound, errorCodeMCPConnectionMissing,
			"No MCP token sends this: an agent sends it with its own token to check that connection.")
	}
	query := event.Request.URL.Query()
	if query.Get("all") != "" && !key.AllWorkspaces {
		return apiError(event, http.StatusForbidden, errorCodeMCPScopeDenied, "This connection doesn't reach all your person's workspaces.")
	}
	if person := query.Get("person"); person != "" && person != key.OwnerID {
		return apiError(event, http.StatusForbidden, errorCodeMCPScopeDenied,
			"This connection works as another person: leave it as it is, and go in beside it.")
	}
	if named := query.Get("scope"); named != "" {
		scope, known := parseUserKeyScope(named)
		if !known {
			return apiError(event, http.StatusBadRequest, errorCodeMCPConnectionInvalid, "scope is read, content, structure or admin.")
		}
		if key.Scope < scope {
			return apiError(event, http.StatusForbidden, errorCodeMCPScopeDenied, "This connection may do less than that.")
		}
	}
	for _, workspaceID := range query["workspace"] {
		if !key.AllWorkspaces && !slices.Contains(key.WorkspaceIDs, workspaceID) {
			return apiError(event, http.StatusForbidden, errorCodeMCPScopeDenied, "This connection doesn't reach that workspace.")
		}
	}
	event.Response.WriteHeader(http.StatusNoContent)
	return nil
}

func disconnectOwnMCPConnection(event *core.RequestEvent) error {
	key, keyed := userKeyOf(event)
	if !keyed || key.AppInstallID != "" || key.ConnectionID == "" {
		return apiError(event, http.StatusNotFound, errorCodeMCPConnectionMissing,
			"No MCP token sends this: an agent sends it with its own token to disconnect that connection. Logged in, its person disconnects one in Settings.")
	}
	found := false
	err := event.App.RunInTransaction(func(txApp core.App) error {
		var err error
		_, found, err = revokeMCPConnectionsInTransaction(txApp,
			mcpConnectionRevocationFilter{ConnectionID: key.ConnectionID, OwnerID: key.OwnerID}, time.Now().UnixMilli())
		return err
	})
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeMCPConnectionFailed, "Couldn't disconnect the MCP connection.", err)
	}
	if !found {
		return apiError(event, http.StatusNotFound, errorCodeMCPConnectionMissing, "MCP connection not found.")
	}
	event.Response.WriteHeader(http.StatusNoContent)
	return nil
}
