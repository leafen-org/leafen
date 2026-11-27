package app

import (
	"net/http"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// What a connection hears of what it can do, and of the way to do more, is
// by its kind (connection_person_rights.go), decided here for every text that
// tells a key a way on — the refusal of what crosses its person's
// workspaces, a refusal of a tool or an operation its scope doesn't reach,
// leafen_operations' connection, a key's request to connect, leafen_services
// and the instructions' services, leafen_standby's steps (2026-11-27, the
// gate's lap 13: an MCP token, kept to one workspace since 2026-11-26, was
// refused what crosses them with "this key reaches only some of your
// workspaces … your person chooses all their workspaces on Leafen's
// connection screen", a step no screen has now, and leafen_services told it
// to connect a service itself, which it is refused):
//
//   - an agent connection reaches all its person's workspaces and does what
//     crosses them itself; one held out of some by their administrators
//     (mcp_client_policy.go) hears that, as connecting again changes nothing;
//   - an MCP token, as Notion's, is kept to its one workspace and the tools
//     its page leaves on: what crosses them — its page's tools, outside
//     services, the server's administration among them — it hears takes an
//     agent connection, by a connection code or `leafen connect`
//     (mcpTokenNotAgent), never that it does it itself; the tools that do
//     the server's administration refuse it as their routes do
//     (mcpServerAdministrationTools);
//   - an app — an installed app's token, an app registered with Leafen
//     (lapp_) or one granted the native API over OAuth — keeps the words it
//     had: what more it may do is what it was granted.

// connectionKind is which of those a key's connection is, as
// leafen_operations names it.
type connectionKind string

const (
	connectionKindAgent connectionKind = "agent connection"
	connectionKindToken connectionKind = "MCP token"
	connectionKindApp   connectionKind = "app"
)

// storedConnectionKind is the kind of a connection as it is kept: an agent
// connection by its mark, else by the OAuth client it was granted to, as
// mcpConnectionKind reads a call's.
func storedConnectionKind(agent bool, oauthClientID string) connectionKind {
	if agent {
		return connectionKindAgent
	}
	return mcpClientKind(oauthClientID)
}

// connectionKindListed is the word the connection list gives a kind, so its
// person's screen and an agent reading the list tell an agent connection
// from an MCP token (2026-11-27: an agent read one reaching four workspaces
// and one reaching one as the same kind, one of them wrong).
func connectionKindListed(kind connectionKind) string {
	switch kind {
	case connectionKindAgent:
		return "agent"
	case connectionKindToken:
		return "token"
	}
	return "app"
}

// connectionKindOf is the kind of the connection a key is of.
func connectionKindOf(app core.App, key userKeyAuthority) connectionKind {
	switch {
	case key.AppInstallID != "":
		return connectionKindApp
	case key.Agent:
		return connectionKindAgent
	case key.MCP != nil:
		return mcpConnectionKind(key.MCP.Authority)
	}
	// A key sent to the API itself: its connection says whether an OAuth
	// client was granted it.
	client := ""
	if err := app.DB().NewQuery(`SELECT [[oauth_client_id]] FROM {{leafen_mcp_connections}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": key.ConnectionID}).Row(&client); err != nil {
		client = ""
	}
	return mcpClientKind(client)
}

// mcpConnectionKind is the kind of an MCP tool's connection.
func mcpConnectionKind(authority mcpAuthority) connectionKind {
	if authority.Agent {
		return connectionKindAgent
	}
	return mcpClientKind(authority.ClientID)
}

// mcpClientKind is the kind of a connection other than an agent's or an
// installed app's, by the OAuth client it was granted to: none (a token's
// page makes it) is an MCP token's.
func mcpClientKind(clientID string) connectionKind {
	if clientID != "" && clientID != mcpGovernanceManualClientID {
		return connectionKindApp
	}
	return connectionKindToken
}

// mcpTokenAgentWay is how an agent on an MCP token comes to do what only an
// agent connection does.
const mcpTokenAgentWay = "your person connects you as an agent by a connection code or `leafen connect`"

// mcpTokenNeedsAgent says, within a sentence, that doing something takes an
// agent connection, for an MCP token.
func mcpTokenNeedsAgent(doing string) string {
	return doing + " takes an agent connection (this MCP token is kept to its workspace, as Notion's tokens are): " + mcpTokenAgentWay
}

// connectionCrossingRefusal is what a connection reaching only some of its
// person's workspaces hears on doing what crosses them; "" for an app, which
// keeps its own words.
func connectionCrossingRefusal(kind connectionKind, doing string) string {
	switch kind {
	case connectionKindToken:
		return mcpTokenNotAgent(doing)
	case connectionKindAgent:
		return doing + " takes a connection reaching all its person's workspaces, and this agent connection is held out of some by " +
			"their administrators, who haven't approved this kind of connection there: ask them (connecting again doesn't change it)."
	}
	return ""
}

// mcpServerAdministrationTools are the MCP tools that do the server's
// administration, each by the /admin/ route whose work it does and whose key
// rule it follows (user decision of 2026-11-27, "응 맞춰": as those routes,
// only an agent connection does it; an MCP token is refused with the way to
// one). A tool that reads only what any connection may — the server's
// version (leafen_server_version) — isn't one.
var mcpServerAdministrationTools = map[string]struct{ method, path, doing string }{
	"leafen_standby":       {http.MethodGet, "/admin/backups/schedule", "Setting up a standby server"},
	"leafen_server_move":   {http.MethodGet, "/admin/server-move/out", "Following a server move"},
	"leafen_remote_access": {http.MethodGet, serverTunnelPath, "The server's remote access"},
}

// mcpToolRouteRefusal is what a connection hears calling such a tool, by its
// route's rule as the key's check judges a call (user_key_auth.go): on a
// route across its person's workspaces, one that doesn't reach all of them
// is refused in its kind's words, an app's as the route answers it; "" when
// it may call it. The scope guard leaves the tool off its list then
// (mcp_tool_scope.go), so leafen_operations doesn't list it either, and
// says this when asked for it by name (native_operation_lookup.go).
func mcpToolRouteRefusal(app core.App, authority mcpAuthority, name string) (string, error) {
	if _, listed := mcpServerAdministrationTools[name]; !listed {
		return "", nil
	}
	key := userKeyWholeOf(mcpNativeKey(authority))
	reachesAll, err := userKeyReachesAllWorkspaces(app, key)
	if err != nil {
		return "", err
	}
	return serverAdministrationToolRefusal(connectionKindOf(app, key), reachesAll, name), nil
}

// serverAdministrationToolRefusal is mcpToolRouteRefusal for a connection of
// this kind that reaches all its person's workspaces or doesn't.
func serverAdministrationToolRefusal(kind connectionKind, reachesAll bool, name string) string {
	route, listed := mcpServerAdministrationTools[name]
	if !listed || reachesAll {
		return ""
	}
	if rule, _ := userKeyRuleOfRoute(route.method, route.path); !rule.across {
		return ""
	}
	if refusal := connectionCrossingRefusal(kind, route.doing); refusal != "" {
		return refusal
	}
	return userKeyReachesSomeMessage
}

// connectionKindsGroup is the operations' group of the person's connections,
// whose own answer opens with connectionKindsToSuggest, and whose line in
// the map, which keeps its budget, says only that it does (2026-11-27, the
// gate's lap 13: the three kinds in the map took it past its 2,600 tokens;
// native_operation_map_room.go).
const connectionKindsGroup = "agent-connections"

// connectionKindsInMap is that line in the map.
const connectionKindsInMap = "The person's connections; opened, which to suggest when they ask for one (a folder, a shared one, a tool)."

// connectionKindsAbout is the connections' group's line: opened, the kinds
// to suggest; in the map, that it says them.
func connectionKindsAbout(about string, opened bool) string {
	if opened {
		return connectionKindsToSuggest + " " + about
	}
	return connectionKindsInMap + " " + about
}

// connectionKindsToSuggest is which connection an agent suggests when its
// person asks for one — "a connection just for this folder", "just for this
// tool" — by what it is for (user 2026-11-27, "그건 당연히 해야하는데"): an
// agent working for them is an agent connection, a narrowed key handed to
// another program an MCP token, an external service on the Notion-compatible
// API an internal integration. An agent connection belongs to one computer
// (user, the same day: "기본적으로 그림자는 컴퓨터 단위"): a folder several
// computers open keeps only a narrowed MCP token in its settings. The
// instructions send such a request to the operations' groups
// (mcp_instructions.go), whose map sends it on to this group.
const connectionKindsToSuggest = "The person's connections, and which to suggest when they ask for one, for a folder or a tool too: " +
	"an agent that works for them → an agent connection (as them, everything they may in all their workspaces; a connection code or " +
	"`leafen connect`); a narrowed key to hand another program, or the key in the settings of a folder several computers open (a NAS's " +
	"shared or a synced folder) → an MCP token (one workspace, the tools and pages chosen, as Notion's; an agent connection's key " +
	"stays in each computer's own settings, never in such a folder's, as a folder copied or passed on whole takes its keys along); " +
	"an external service on the Notion-compatible API → an internal integration (a bot of its own, which isn't taken on Leafen's MCP). " +
	"Apps have theirs."

// connectionWiden is how a connection comes to do more, by its kind.
func connectionWiden(kind connectionKind) string {
	switch kind {
	case connectionKindAgent:
		return nativeOperationWidenAgent
	case connectionKindToken:
		return nativeOperationWidenToken
	}
	return nativeOperationWiden
}

// mcpConnectionWiden is connectionWiden for an MCP tool's connection.
func mcpConnectionWiden(authority mcpAuthority) string {
	return connectionWiden(mcpConnectionKind(authority))
}

// nativeOperationWidenAgent is an agent connection's way to do more: none
// but its organizations' key policy, as nothing on its page narrows it
// (connection_person_rights.go).
const nativeOperationWidenAgent = "This agent connection does what its person may in all their workspaces, with every tool and page — " +
	"nothing on its page narrows it —, held only to what its workspaces' organizations let keys do, which their administrators set."

// nativeOperationWidenToken is an MCP token's: its page and every other
// workspace are an agent connection's to reach.
const nativeOperationWidenToken = "This MCP token stays in its one workspace with the tools its page leaves on, as Notion's tokens do, held " +
	"to what its organization lets keys do: an agent connection, by a connection code or `leafen connect`, reaches all its person's " +
	"workspaces and turns this token's tools on (leafen_call PATCH " + apiRoot + "/mcp/connections/{this token's id}/settings with its " +
	"disabledTools); what keys may do in an organization its administrators set."
