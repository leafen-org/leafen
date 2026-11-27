package app

import "slices"

// The person's own routes, each by its own path: what no key does, what
// takes only its own code, and what is the person's own across their
// workspaces (user_key_routes.go has the broader rules these come before).

// userKeyKeylessRules are routes whose handlers refuse every key: the
// person decides there, signed in to Leafen itself. The key hears why
// before the handler runs, and the operations list says so.
var userKeyKeylessRules = slices.Concat(
	// An app's request on its screen waits for its person: a key that
	// could answer it would approve itself.
	userKeyEach(func(methods, path string) userKeyRule {
		return userKeyRoute(methods, path, userKeyScopeClosed, userKeyPlacePerson, "").
			closedBecause("Only the person decides, logged in to Leafen itself: a key never answers an approval.")
	},
		"POST /apps/{installId}/approvals/{approvalId}/approve", "POST /apps/{installId}/approvals/{approvalId}/decline"),
	// A sign-in handed to another device or address, a password set or
	// forgotten, and the desktop app's first owner settled go from the device
	// that is signed in (their handlers refuse a key).
	userKeyEach(func(methods, path string) userKeyRule {
		return userKeyRoute(methods, path, userKeyScopeClosed, userKeyPlacePerson, "").
			closedBecause("A key can't do this; it's done logged in.")
	},
		"POST /account/device-codes", "DELETE /account/device-codes/{codeId}", "POST /admin/accounts/{accountId}/device-codes",
		"POST /account/address-handoff", "POST /account/app-sign-in",
		"POST /local-owner/settled"),
	// What acts on a browser's own sign-in — a passkey made on it or taken
	// away, its 2-step code shown for it — answers only that browser's
	// session, and a key has none.
	userKeyEach(func(methods, path string) userKeyRule {
		return userKeyRoute(methods, path, userKeyScopeClosed, userKeyPlacePerson, "").
			closedBecause("Only a browser logged in to Leafen does this: it acts on that browser's own login, which a key doesn't have.")
	},
		"POST /account/passkeys/begin", "POST /account/passkeys/finish", "DELETE /account/passkeys",
		"POST /account/sessions/current/second-factor"),
	// A token's key shown again goes to its person's own screen alone, as
	// its value would pass through the conversation: an agent gets a key of
	// its own by a connection code. Its pages — Notion's content access —
	// its person chooses there (user decision of 2026-11-10, 3, under
	// review with them).
	userKeyEach(func(methods, path string) userKeyRule {
		return userKeyRoute(methods, path, userKeyScopeClosed, userKeyPlacePerson, "").
			closedBecause("A token's page is changed by its person, logged in.")
	},
		"POST /mcp/connections/{id}/token/reveal", "PUT /mcp/connections/{id}/pages"),
	// What only the person gives an agent's step — a secret typed, a code
	// kept or shown to them alone — goes in on their own screen, signed in
	// (person_step_apply.go); no key types or sees it.
	userKeyEach(func(methods, path string) userKeyRule {
		return userKeyRoute(methods, path, userKeyScopeClosed, userKeyPlacePerson, "").
			closedBecause("Only the person types or sees this, logged in to Leafen itself. Left to do: where, to them — GET /person-steps/{stepId} says.")
	},
		"POST /person-steps/{stepId}/reveal", "POST /person-steps/{stepId}/enter"),
	// Another agent's connection asked for by its code is allowed on the
	// screen here, or in the conversation by leafen_connection_requests,
	// which asks the person there.
	userKeyEach(func(methods, path string) userKeyRule {
		return userKeyRoute(methods, path, userKeyScopeClosed, userKeyPlacePerson, "").
			closedBecause("Leafen's screen takes the other agent's code here; in a conversation, leafen_connection_requests allows it, asking the person.")
	},
		"POST /mcp/device-connections/find", "POST /mcp/device-connections/decide"),
)

// userKeyCodeRules take only their own code, token, ticket or secret — a
// move's code, a guest link, a hook's secret, a sign-in's state — or serve
// what anyone may fetch: a key adds nothing, and the route's own check
// answers it.
var userKeyCodeRules = userKeyEach(func(methods, path string) userKeyRule {
	return userKeyRoute(methods, path, userKeyScopeRead, userKeyPlacePerson, "")
},
	// A workspace or server moving here: only the server given its code calls.
	"GET /workspace-move/in/{id}/accounts", "GET /workspace-move/in/{id}/status",
	"PUT /workspace-move/in/{id}/bundle", "PUT /workspace-move/in/{id}/history",
	"PUT /workspace-move/in/{id}/history-files/{file}", "POST /workspace-move/in/{id}/finish",
	"POST /workspace-move/in/{id}/public",
	"GET /server-move/in/{id}", "POST /server-move/in/{id}/finish",
	"GET HEAD PUT DELETE OPTIONS PROPFIND PROPPATCH MKCOL MOVE COPY LOCK UNLOCK /server-move/in/{id}/dav/{path...}",
	"GET /replica/pair-check",
	// An app's guests, rooms, hooks and outside sign-ins.
	"POST /app-guests/sessions", "POST /app-guests/kv", "POST /app-guests/rooms/ticket", "GET /app-rooms/{room}",
	"POST /apps/{slug}/hooks/{hook}", "GET /apps/oauth/callback",
	"GET /app-icon/{install}", "GET /app-home-screen/{slug}/icon", "GET /app-home-screen/{slug}/manifest",
	// A service's sign-in coming back, and what the service is told of Leafen.
	"GET /mcp-services/oauth/callback", "GET /mcp-services/oauth/callback-address", "GET /mcp-services/oauth/client-metadata",
	// Pictures and frames anyone may fetch, and the name a login wall shows.
	"GET /avatars/{file}", "GET /avatars/photos/{file}", "GET /document-preview/{path...}", "GET /provider-frame",
	"GET /page-login-wall/{pageId}", "GET /form-login-wall/{token}",
	// The desktop app's earlier owner signed in with the code its window holds.
	"POST /local-owner/session",
	// What a Notion migration's reader read in the person's Notion, by its key,
	// and how far it is while it reads.
	"POST /notion-migration-formats", "GET /notion-migration-formats/progress", "GET /notion-migration-formats/reading",
)

// userKeyOwnRules are the person's own across their workspaces — signing in,
// credentials, keys and connections, the first run, joining, their outside
// services and apps — done with a key for all of them (agent full-permission
// decision of 2026-10-16, 1–2); an app's token never does them.
var userKeyOwnRules = slices.Concat(
	// The person's steps an agent made, read or cancelled by any of their
	// keys: what was typed or shown is never in them (person_steps.go).
	[]userKeyRule{userKeyRoute("GET", "/person-steps", userKeyScopeRead, userKeyPlacePerson, "").persons(),
		userKeyRoute("GET DELETE", "/person-steps/{stepId}", userKeyScopeRead, userKeyPlacePerson, "").persons()},
	// A key checking or disconnecting its own connection, whatever its
	// workspaces: the check reads only what it reaches, and disconnecting
	// only ends it (mcp_connection_self_disconnect.go). Before the rule for
	// any connection.
	[]userKeyRule{userKeyRoute("GET DELETE", mcpOwnConnectionPath, userKeyScopeRead, userKeyPlacePerson, "").persons()},
	// An agent's request to connect and its poll, which a key's request is
	// told it needs no more (mcp_device_connection_widen.go).
	userKeyEach(func(methods, path string) userKeyRule {
		return userKeyRoute(methods, path, userKeyScopeRead, userKeyPlacePerson, "").persons()
	}, "POST "+mcpDeviceConnectionPath, "POST "+mcpDeviceConnectionTokenPath),
	// A connection's page and what it reaches beside Leafen — its person's
	// outside services and apps, each service's tools and how they run —
	// are its person's to set through any of their connections that reach
	// all they do, as on their screen (user decision of 2026-11-26: no way
	// is the person's screen's alone). What turns off a question to them —
	// a token's "모두 맡기기", a service's tool run without asking or all of
	// its tools always allowed — asks them once in the conversation (the
	// 2026-11-13 decision on "모두 맡기기"); a key made again ends the one
	// before, which can't be undone.
	userKeyEach(func(methods, path string) userKeyRule {
		return userKeyRoute(methods, path, userKeyScopeRead, userKeyPlacePerson, "").crossing()
	}, "GET /mcp/connections/{id}/settings", "GET /mcp/connections/{connectionId}/services", "GET /mcp/connections/{connectionId}/apps"),
	userKeyEach(func(methods, path string) userKeyRule {
		return userKeyRoute(methods, path, userKeyScopeAdmin, userKeyPlacePerson, "").crossing()
	}, "PUT /mcp/connections/{connectionId}/services", "PUT /mcp/connections/{connectionId}/apps"),
	[]userKeyRule{
		userKeyRoute("PATCH", "/mcp/connections/{id}/settings", userKeyScopeAdmin, userKeyPlacePerson, "").crossing().
			askedWhen(mcpTokenSettingsStopAsking).askedWhereItActs(),
		userKeyRoute("PATCH", "/mcp-services/tools/{serviceId}/{tool}", userKeyScopeAdmin, userKeyPlacePerson, "").crossing().
			askedWhen(mcpServiceToolStopsAsking).askedWhereItActs(),
		userKeyRoute("PATCH", "/mcp-services/{serviceId}", userKeyScopeAdmin, userKeyPlacePerson, "").crossing().
			askedWhen(mcpServiceStopsAsking).askedWhereItActs(),
		userKeyRoute("POST", "/mcp/connections/{id}/token", userKeyScopeAdmin, userKeyPlacePerson, "").crossing().askedWhereItActs(),
	},
	// Agents' approval of a browser's login, as its person's account sets it:
	// turned back on with their yes in the conversation, off at once
	// (agent_sign_in_requests.go).
	[]userKeyRule{
		userKeyRoute("GET", agentSignInSettingPath, userKeyScopeRead, userKeyPlacePerson, "").persons(),
		userKeyRoute("PUT", agentSignInSettingPath, userKeyScopeAdmin, userKeyPlacePerson, "").crossing().
			askedWhen(agentSignInTurnsOn).askedWhereItActs(),
	},
	userKeyEach(func(methods, path string) userKeyRule {
		return userKeyRoute(methods, path, userKeyScopeAdmin, userKeyPlacePerson, "").crossing()
	},
		"POST /account/app-passwords", "DELETE /account/app-passwords/{passwordId}",
		"PUT /account/app-schedules/{installId}/{schedule}",
		"POST /account/emails/confirm", "PUT /account/emails/primary",
		"POST /account/mfa/totp/confirm", "POST /account/mfa/totp/enroll",
		"POST /account/sessions/without-second-factor/revoke",
		"POST /account/switcher-keys", "DELETE /account/switcher-keys/{keyId}",
		"POST /auth/address-handoff/exchange", "POST /auth/app-sign-in/exchange", "POST /auth/device", "POST /auth/administrator-code",
		"POST /auth/agent-sign-in", "POST /auth/agent-sign-in/poll", "POST /auth/agent-sign-in/cancel",
		"POST /auth/invitation/join", "POST /auth/invitation/take", "POST /auth/login", "POST /auth/register",
		"POST /auth/mfa/recovery", "POST /auth/mfa/totp",
		"POST /auth/passkeys/app/exchange", "POST /auth/passkeys/begin", "POST /auth/passkeys/finish",
		"POST /auth/password-reset/check", "POST /auth/password-reset/confirm", "POST /auth/password-reset/request",
		"POST /auth/providers/exchange", "POST /auth/saml/acs", "POST /auth/saml/exchange", "POST /auth/saml/find",
		"POST /auth/signup-invitation", "POST /auth/verification/confirm", "POST /auth/verification/request",
		"POST /setup/agent-start", "POST /setup/claim", "POST /setup/restore", "POST /setup/restore/place",
		"POST /setup/restore/sign-in", "POST /setup/restore/progress", "POST /setup/restore/agent", "POST /setup/restore/agent/state",
		"POST /setup/restore/agent/secrets", "POST /setup/restore/agent/end", "POST /setup/restore/agent/status",
		"POST /setup/restore/agent/find", "POST /setup/restore/agent/run", "GET POST DELETE /setup/restore/mcp",
		"POST /first-run/steps", "POST /first-run/finish", "POST /first-run/reopen",
		"POST /workspace-invites/read", "POST /workspace-invites/join",
		"POST /mcp/connection-codes", "POST /mcp/connection-codes/exchange", "POST /mcp/connections",
		"DELETE /mcp/connections/{id}",
		"POST /mcp/oauth/requests/{request}/approve", "POST /mcp/oauth/requests/{request}/deny",
		"POST /mcp-services", "POST /mcp-services/probe",
		"DELETE /mcp-services/{serviceId}", "POST /mcp-services/{serviceId}/check",
		"POST /mcp-services/{serviceId}/return", "POST /mcp-services/{serviceId}/sign-in",
		"POST /apps", "POST /app-requests", "POST /app-self-installs",
		"POST /installed-apps/{installId}/consent", "POST /installed-apps/{installId}/sessions",
		"GET /apps/{installId}/credentials", "PUT DELETE /apps/{installId}/credentials/{key}",
	),
	userKeyEach(func(methods, path string) userKeyRule {
		return userKeyRoute(methods, path, userKeyScopeRead, userKeyPlacePerson, "").crossing()
	},
		"GET /first-run", "GET /switcher/list",
		"GET /mcp-services", "GET /mcp-services/{serviceId}", "GET /mcp-services/tools/{serviceId}",
		"GET /app-availability", "GET /app-catalog", "GET /installed-apps",
		"GET /installed-apps/{installId}/reads", "GET /installed-apps/{installId}/runs",
		"GET /installed-apps/{installId}/runs/{runId}/changes",
	),
	// What loosens the person's own sign-in — 2-step verification turned
	// off, its recovery codes made again (the saved ones stop working and the
	// agent gets the new ones), a sign-in email taken away — and their
	// account deleted, with every workspace they own, ask them every time,
	// whatever the token's switch and with no hour (decided 2026-11-13),
	// once their handlers checked the request (user_key_acting_ask.go); no
	// key adds or takes away a passkey (above). Adding the others, making a
	// primary email of one already theirs and ending sessions go ahead as
	// they did.
	userKeyEach(func(methods, path string) userKeyRule {
		return userKeyRoute(methods, path, userKeyScopeAdmin, userKeyPlacePerson, "").crossing().askedEveryTime().askedWhereItActs()
	},
		"DELETE /account/mfa/totp", "POST /account/mfa/recovery-codes", "DELETE /account/emails", "POST /account/deletion"),
	// A key's own token's "모두 맡기기", whatever its workspaces: turning it
	// on waits for its person's yes, asked once the request reads
	// (mcp_token_hand_over.go).
	[]userKeyRule{
		userKeyRoute("GET", mcpHandsAllOverPath, userKeyScopeRead, userKeyPlacePerson, "").persons(),
		userKeyRoute("PUT", mcpHandsAllOverPath, userKeyScopeRead, userKeyPlacePerson, "").persons().askedWhen(mcpHandsAllOverTurnsOn).askedWhereItActs(),
		// Notion's tools on its own list, asking no one (mcp_token_notion_tools.go).
		userKeyRoute("GET PUT", mcpNotionToolsPath, userKeyScopeRead, userKeyPlacePerson, "").persons(),
		// Kept in place of a connection text's code, nothing is traded (mcp_connection_kept_code.go).
		userKeyRoute("POST", mcpOwnCodePath, userKeyScopeRead, userKeyPlacePerson, "").persons(),
	},
	// The person's picture, as their profile; the browser they hear on.
	[]userKeyRule{
		userKeyRoute("POST", "/account/avatar-photo", userKeyScopeContent, userKeyPlacePerson, ""),
		userKeyRoute("PUT DELETE", "/account/web-push/subscription", userKeyScopeAdmin, userKeyPlacePerson, "").persons(),
	},
)
