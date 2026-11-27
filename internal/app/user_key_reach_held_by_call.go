package app

import "github.com/pocketbase/pocketbase/core"

// A call an agent holds to one workspace itself — leafen_call or
// leafen_operations given workspace_id, which works in that workspace alone
// as if its connection reached it alone (mcp_tool_arguments.go) — reaches
// only that one; its connection may reach all its person's workspaces. On
// an operation across them it hears that its workspace_id held it, and to
// call again without it, not to connect again (2026-11-26, a real agent: GET
// /account/developer-ids with workspace_id heard "connect again" and told
// its person to); a connection held to some of them hears its kind's way
// (connection_kind_ways.go). What
// the connection can do, and whether it has all a widening would give it,
// is the connection's, as a whole.

// userKeyHeldByCallMessage is what such a call hears.
const userKeyHeldByCallMessage = "This call names one workspace by workspace_id, so it works in that workspace alone, " +
	"and this is your person's own, across all their workspaces: call it again without workspace_id. " +
	"This connection reaches all of them, so there is nothing to connect again."

// userKeyWholeOf is the key of the connection a call is made through, as a
// whole: without the workspace its call's workspace_id held it to.
func userKeyWholeOf(authority userKeyAuthority) userKeyAuthority {
	if authority.MCP == nil || authority.MCP.Authority.whole == nil {
		return authority
	}
	return mcpNativeKey(*authority.MCP.Authority.whole)
}

// userKeyReachesSomeRefusal is what a key reaching only some of its
// person's workspaces hears on an operation across them: a call held to one
// by its workspace_id, to leave it out; otherwise its connection's kind
// says why and the way on (connection_kind_ways.go) — an MCP token, kept to
// its one workspace, that this takes an agent connection, never to choose
// all their workspaces on a connection screen none has now.
func userKeyReachesSomeRefusal(app core.App, authority userKeyAuthority) string {
	whole := userKeyWholeOf(authority)
	if whole.MCP != authority.MCP {
		if reachesAll, err := userKeyReachesAllWorkspaces(app, whole); err == nil && reachesAll {
			return userKeyHeldByCallMessage
		}
	}
	if refusal := connectionCrossingRefusal(connectionKindOf(app, whole), "This works across your person's workspaces, so it"); refusal != "" {
		return refusal
	}
	return userKeyReachesSomeMessage
}
