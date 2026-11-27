package app

import "fmt"

// A connection's key once asked its person for more with a request to
// connect (2026-11-23); since 2026-11-26 there is nothing more to ask for
// with a key (connection_person_rights.go): an agent connection has its
// person's rights in all their workspaces and turns its page's tools on
// itself (PATCH /mcp/connections/{id}/settings), and an MCP token, as
// Notion's, keeps its one workspace and the tools its page leaves on, which
// an agent connection reaches past — the request sent without the key asks
// for one. A request sent with a key hears so, in its kind's words
// (connection_kind_ways.go), and no code is made.

// mcpDeviceAskedWithKey is what a connection's key asking to connect hears.
func mcpDeviceAskedWithKey(kind connectionKind) string {
	switch kind {
	case connectionKindAgent:
		return "This connection already works as your person: an agent connection does everything they may in all their workspaces, " +
			"so there is nothing more to ask for. To turn a tool on, leafen_call PATCH " + apiRoot + "/mcp/connections/{its id}/settings " +
			"with its disabledTools (Leafen asks your person in the conversation)."
	case connectionKindToken:
		return "This key is an MCP token: as Notion's, it keeps its one workspace and the tools its page leaves on, so there is nothing " +
			"more to ask for with it. An agent connection reaches all your person's workspaces and turns this token's tools on: this same " +
			"request sent without the key asks for one, its code your person's to allow, or " + mcpTokenAgentWay + "."
	}
	return mcpDeviceAskedWithAppKey
}

// mcpDeviceAskedWithAppKey is what an app's key asking to connect hears, as it
// always has.
const mcpDeviceAskedWithAppKey = "This connection already works as your person: an agent connection does everything they may in all " +
	"their workspaces, so there is nothing more to ask for. An MCP token, as Notion's, keeps its one workspace and the tools its page " +
	"leaves on: to turn a tool on, leafen_call PATCH " + apiRoot + "/mcp/connections/{its id}/settings with its disabledTools " +
	"(Leafen asks your person in the conversation); for another workspace, your person makes a token there or connects you by a " +
	"connection code."

// mcpDeviceAskAnswer is what a request's answer tells the agent: to give its
// person the code, where they allow it, and what to poll.
func mcpDeviceAskAnswer(shown, name, tokenURL string, interval int) string {
	return fmt.Sprintf("The code is %s, for “%s”, and allowing it is your person's: in their conversation with an agent already connected to "+
		"this Leafen, by asking it to allow the Leafen connection %s — it asks them there — or, with no agent connected yet, "+
		"by typing it in Leafen's phone app ("+mcpScreen("", "settings", "features", "agents", "agentsMore", "agentsOtherComputer")+"). "+
		"Don't send them to a page. Meanwhile poll POST %s with {\"device_code\": \"<device_code>\"} "+
		"every %d seconds: it answers authorization_pending until they decide, then gives your key. Claude Code polls with "+
		"\"check\": true as well, so that once allowed the answer says how to hand the key to Claude Code without it passing "+
		"through you. The code lasts ten minutes; keep device_code to yourself. Left to do: the code to your person, with where they "+
		"allow it.", shown, name, shown, tokenURL, interval)
}
