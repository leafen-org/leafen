package app

import "slices"

// The server's administration and an organization's sign-in and display
// policies, each by its own path. They aren't work in one workspace: they
// reach every workspace on the server or in the organization, so only a
// key for all of its person's workspaces does them, with the whole scope,
// and never an app's token; what can't be taken back is confirmed, and an
// administrator's safeguards are asked every time, once each handler
// checked the change (user_key_acting_ask.go).
var userKeyAdminRules = slices.Concat(
	userKeyEach(func(methods, path string) userKeyRule {
		return userKeyRoute(methods, path, userKeyScopeAdmin, userKeyPlacePerson, "").crossing()
	},
		// Apps on this server: what is installed and its settings.
		"POST /admin/apps/catalogs", "POST /admin/apps/catalogs/upload", "DELETE /admin/apps/catalogs/{catalogId}",
		"POST /admin/apps/catalogs/{catalogId}/apps/{appId}/fetch", "POST /admin/apps/catalogs/{catalogId}/refresh",
		"POST /admin/apps/packages", "POST /admin/apps/requests/{requestId}/decide",
		"POST /admin/apps/installs", "DELETE /admin/apps/installs/{installId}", "POST /admin/apps/installs/{installId}/restore",
		"PUT /admin/apps/installs/{installId}/endpoint-limits",
		"GET /admin/apps/installs/{installId}/guests/links", "GET /admin/apps/installs/{installId}/guests/reports",
		"POST /admin/apps/installs/{installId}/guests/links/{linkId}/revoke",
		"POST /admin/apps/installs/{installId}/hooks/{hook}/secret",
		"DELETE /admin/apps/installs/{installId}/hooks/{hook}/orgs/{organizationId}",
		"GET /admin/apps/installs/{installId}/jobs/approvals", "GET /admin/apps/installs/{installId}/jobs/runs",
		"POST /admin/apps/installs/{installId}/oauth/{provider}/return",
		"PUT /admin/apps/installs/{installId}/schedules/{schedule}",
		"PUT /admin/apps/installs/{installId}/secrets/{name}",
		"PUT /admin/apps/installs/{installId}/settings/{key}",
		// Sign-in providers, accounts' sessions.
		"POST /admin/auth-providers/{provider}/return",
		// Backups: their protection, the copy kept elsewhere and the standby.
		"POST /admin/backups/protection/code", "POST /admin/backups/protection/open",
		"PUT /admin/backups/protection/password", "POST /admin/backups/protection/write-here",
		"PUT /admin/backups/replication", "POST /admin/backups/replication/apart",
		"POST /admin/backups/schedule/suggestion/dismiss",
		"DELETE /admin/backups/standby", "PUT /admin/backups/standby/auto", "POST /admin/backups/standby/code",
		"DELETE /admin/backups/standby/join",
		// The server's address, proxy and request log, and a server moving here.
		"PUT /admin/network-proxy", "PUT /admin/request-log",
		"DELETE /admin/server-move/in",
		"PUT /admin/tunnel", "POST /admin/tunnel/certificate-alert/clear", "POST /admin/tunnel/tailscale/logout",
		// A workspace's content as the server's administrator sees it.
		"GET /admin/workspaces/{workspaceId}/files/{fileId}/download",
		"POST /admin/workspaces/{workspaceId}/pages/{pageId}/trash",
		"POST /admin/workspaces/{workspaceId}/public/{kind}/{itemId}/disable",
		// An organization's single sign-on address and views.
		"POST /organizations/{organizationId}/saml/return", "PUT /organizations/{organizationId}/view-display-policy",
	),
	// What can't be taken back — an app's secret or the credentials people
	// gave it gone, a way backups are protected taken away, this server
	// opened to be replaced by another server moving here — asked, as the
	// token's switch says, once its handler checked it (agent key decision
	// of 2026-11-16: every irreversible administrator's act asks its person).
	userKeyEach(func(methods, path string) userKeyRule {
		return userKeyRoute(methods, path, userKeyScopeAdmin, userKeyPlacePerson, "").crossing().askedWhereItActs()
	},
		"DELETE /admin/apps/installs/{installId}/credentials", "DELETE /admin/apps/installs/{installId}/secrets/{name}",
		"DELETE /admin/backups/protection/{way}", "POST /admin/server-move/in",
	),
	// An administrator's safeguards — over what apps, agents and keys may do,
	// how people sign in and share, and what is kept for oversight: an agent
	// changing one, either way, asks its person every time, whatever its
	// token's switch ("모두 맡기기" too) and with no hour (decided
	// 2026-11-13), each once its handler checked the change
	// (user_key_acting_ask.go). Left to the
	// token's switch: installing apps and deciding requests (within these
	// policies), an app's limits, settings, secrets and hooks, the server's
	// mail, map, updates and name, address, proxy, request log and backups,
	// and an organization's members, groups, domains, storage and SCIM
	// tokens.
	userKeyEach(func(methods, path string) userKeyRule {
		return userKeyRoute(methods, path, userKeyScopeAdmin, userKeyPlacePerson, "").crossing().
			askedEveryTime().askedWhereItActs()
	},
		// Apps: which apps and how they install, how they connect, and each
		// app's people, guests, open addresses, running and people's
		// credentials.
		"PUT /admin/apps/allowlist", "PUT /admin/apps/settings",
		"PATCH /admin/apps/installs/{installId}/status", "PUT /admin/apps/installs/{installId}/grants",
		"PUT /admin/apps/installs/{installId}/guests", "PUT /admin/apps/installs/{installId}/endpoint-public",
		"PUT /admin/apps/installs/{installId}/credentials",
		// An organization's sign-in, guests' 2-step and account history.
		"PUT /organizations/{organizationId}/sign-in-policy", "PUT /organizations/{organizationId}/guest-mfa",
		"PUT /organizations/{organizationId}/account-history-policy",
		// Apps' oversight and strictness.
		"PUT /admin/apps/oversight", "PUT /admin/apps/strict",
	),
	// The server's settings, saved in one request: a change to who may sign
	// up or be added to a workspace, sign-up's email verification or the
	// sign-in providers is asked so; one to the rest goes ahead
	// (administrator_settings_safeguards.go).
	[]userKeyRule{
		userKeyRoute("PATCH", "/admin/settings", userKeyScopeAdmin, userKeyPlacePerson, "").crossing().
			askedEveryTime().askedWhen(administratorSettingsChangeSafeguards).askedWhereItActs(),
	},
	userKeyEach(func(methods, path string) userKeyRule {
		return userKeyRoute(methods, path, userKeyScopeAdmin, userKeyPlaceOrganizationParam, "organizationId").crossing().
			askedEveryTime().askedWhereItActs()
	},
		// An organization's keys and the agents' programs it allows, its
		// sharing, managed people, single sign-on and provisioning, data
		// loss prevention, and its audit log and legal holds, asked once
		// their handlers checked the change (user_key_acting_ask.go).
		"PUT /organizations/{organizationId}/user-key-policy",
		"PUT /organizations/{organizationId}/mcp-governance/workspaces/{workspaceId}",
		"POST /organizations/{organizationId}/mcp-governance/workspaces/{workspaceId}/clients",
		"DELETE /organizations/{organizationId}/mcp-governance/workspaces/{workspaceId}/clients/{clientId}",
		"PUT /organizations/{organizationId}/sharing-security-policy", "PATCH /organizations/{organizationId}/managed-user-policies",
		"PUT /organizations/{organizationId}/saml", "PUT /organizations/{organizationId}/scim", "PUT /organizations/{organizationId}/dlp",
		"PUT /organizations/{organizationId}/audit-retention",
		"POST /organizations/{organizationId}/legal-holds/{legalHoldId}/release",
	),
	// What can't be taken back — the server's data replaced or sent away, an
	// account's session ended, a workspace's page or file gone for good, a
	// private page opened to the workspace's owner — asked once its handler
	// found what it names and the change would go through
	// (user_key_acting_ask.go).
	userKeyEach(func(methods, path string) userKeyRule {
		return userKeyRoute(methods, path, userKeyScopeAdmin, userKeyPlacePerson, "").crossing().askedWhereItActs()
	},
		"POST /admin/backups/replica/restore", "PUT /admin/backups/standby/join", "POST /admin/backups/standby/takeover",
		"POST /admin/server-move/out",
		"POST /admin/accounts/{accountId}/sessions/{sessionId}/revoke",
		"POST /admin/workspaces/{workspaceId}/files/{fileId}/delete",
		"POST /admin/workspaces/{workspaceId}/pages/{pageId}/delete-permanently",
		"POST /admin/workspaces/{workspaceId}/pages/{pageId}/take-over",
	),
)
