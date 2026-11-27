package app

import "strings"

// The operations listed that no call runs. A call — leafen_call, or a
// program's request with its key — sends the connection's key with a JSON
// body (or bytes) and reads one answer; these take something else: an MCP
// session of their own, a socket kept open, a person's browser and what it
// holds of a sign-in, or another program's own credential (2026-11-26:
// leafen_operations listed POST /setup/restore/mcp as an operation, and
// leafen_call got "Accept must contain both 'application/json' and
// 'text/event-stream'"). Each says why and the way that does it, kept here
// in one place: an operation that describes or lists it says so
// (this_connection: "not a call: …"), a group or a search counts it apart
// from what needs more permission and lists it with all, the map of groups
// leaves it out of its counts, and leafen_call refuses it with the same
// words before sending it. A route that is any of these and not here is a
// call that fails; it belongs here.

// nativeOperationNotCallPrefix begins what this_connection says of one.
const nativeOperationNotCallPrefix = "not a call: "

// nativeOperationNotCalls are those operations by their shape, with why.
var nativeOperationNotCalls = map[string]string{}

// nativeOperationNotCallRules match a filled-in path to its operation.
var nativeOperationNotCallRules = []userKeyRule{}

func init() {
	const (
		restoreMCP = "the restore's own MCP endpoint, for an agent restoring a new server that has no administrator yet: " +
			"add this server's /api/leafen/v1/setup/restore/mcp as an MCP server, with no login, and pass its tools the restore code " +
			"the person pasted"
		formats = "what the formats reader does from the person's browser, so that what it reads never passes through you: " +
			"leafen_notion_migration_formats gives Leafen's page to run it from (reader_page, opened in your browser), and says what came in"
		unsubscribe = "the page an activity email's unsubscribe link opens, by that email's own token; PATCH /account/notifications " +
			"with emailActivity false stops the person's activity emails"
		personSignIn = "a login that runs in the person's browser from Leafen's login screen; the person this connection " +
			"works for is logged in already"
		workspaceMove = "what the server a workspace moves from sends, logged in with the move's code, not a key; " +
			"GET /workspaces/{workspaceId}/move-in follows a move coming in"
		serverMove = "what the server this one moves from sends, logged in with the move's code, not a key; " +
			"GET /admin/server-move/in follows the move coming in"
	)
	for route, why := range map[string]string{
		"GET POST DELETE /setup/restore/mcp": restoreMCP,
		// Sockets kept open, which a call can't hold.
		"GET /collaboration/{room}": "a page's live editing socket, which Leafen's editor keeps open; notion-update-page changes " +
			"the page, and GET /collaboration-status/{room} reads whether it saves",
		"GET /workspaces/{workspaceId}/change-socket": "the socket a logged-in screen keeps open for a workspace's changes as they " +
			"come; a webhook (POST /workspaces/{workspaceId}/webhooks) hears them for a program",
		"GET /app-rooms/{room}": "an app's live room, a socket its pages open with a ticket the app gives them",
		// What a person's browser opens or sends.
		"POST /notion-migration-formats":           formats,
		"GET /notion-migration-formats/reading":    formats,
		"GET /notion-migration-formats/progress":   formats,
		"GET POST /notification-email/unsubscribe": unsubscribe,
		"GET /mcp-services/oauth/callback": "where an external service's login sends the person's browser back; " +
			"POST /mcp-services/{serviceId}/sign-in starts one and answers the authorizeUrl they open",
		"GET /apps/oauth/callback": "where an app's login provider sends the person's browser back; the app starts one " +
			"with its own token",
		"GET /auth/providers/{provider}/start":    personSignIn,
		"GET /auth/providers/{provider}/callback": personSignIn,
		"GET /auth/saml/{providerId}/start":       personSignIn,
		"POST /auth/saml/acs":                     personSignIn,
		"GET /provider-frame":                     "the page Leafen's Mac and iPhone apps frame a video player in",
		// What another program sends with a credential of its own.
		"GET /workspace-move/in/{id}/accounts":             workspaceMove,
		"GET /workspace-move/in/{id}/status":               workspaceMove,
		"PUT /workspace-move/in/{id}/bundle":               workspaceMove,
		"PUT /workspace-move/in/{id}/history":              workspaceMove,
		"PUT /workspace-move/in/{id}/history-files/{file}": workspaceMove,
		"POST /workspace-move/in/{id}/finish":              workspaceMove,
		"POST /workspace-move/in/{id}/public":              workspaceMove,
		"GET /server-move/in/{id}":                         serverMove,
		"POST /server-move/in/{id}/finish":                 serverMove,
		"GET HEAD PUT DELETE OPTIONS PROPFIND PROPPATCH MKCOL MOVE COPY LOCK UNLOCK /server-move/in/{id}/dav/{path...}": serverMove,
		"GET /replica/pair-check": "what the other server of a primary and standby pair asks this one directly",
		"POST /apps/{slug}/hooks/{hook}": "where an external service delivers an app's hook, signed with the hook's secret, " +
			"not a key",
	} {
		methods, path, _ := strings.Cut(route, " /")
		for _, method := range strings.Fields(methods) {
			nativeOperationNotCalls[nativeOperationShape(method+" /"+path)] = why
		}
		rule := userKeyRoute(methods, "/"+path, userKeyScopeClosed, userKeyPlacePerson, "")
		rule.reason = why
		nativeOperationNotCallRules = append(nativeOperationNotCallRules, rule)
	}
}

// nativeOperationNotCall is why a listed operation is no call, or "".
func nativeOperationNotCall(id string) string {
	return nativeOperationNotCalls[nativeOperationShape(id)]
}

// nativeOperationNotCallFor is why the operation a method and a filled-in
// path name is no call, if it isn't one.
func nativeOperationNotCallFor(method, path string) (string, bool) {
	method = strings.ToUpper(strings.TrimSpace(method))
	for _, rule := range nativeOperationNotCallRules {
		if rule.allows(method) && rule.pattern.MatchString(path) {
			return rule.reason, true
		}
	}
	return "", false
}
