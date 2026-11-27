package app

// Where an operation that sets a state reads it back, when that read is
// another operation's — in another group, or under a name that doesn't say
// so (2026-11-26, the wider scorecard: turning strict app mode on, an agent
// looked for GET /admin/apps/strict and GET /admin/apps/settings, which
// aren't operations; strict mode is read in GET /admin/apps). The operations
// list gives the note beside the operation, after any it has
// (agent_secret_notes.go, backup_progress.go — earlier files by name).

func init() {
	const appsRead = "Read it back in GET /admin/apps"
	const backupsRead = "Read it back in GET /admin/backups/schedule"
	for id, note := range map[string]string{
		"GET /admin/apps": "Beside the installed apps it reads the server's app settings an administrator sets: " +
			"tenancies[] (each organization's key, with strict — strict app mode — and overseen — oversight), " +
			"settings (strictNetwork: strict mode for every app on the server, installPolicy, homeScreenAppsOff, unsignedAppsOff, maxPackageBytes) " +
			"and allowlist; PUT /admin/apps/strict, /oversight, /settings and /allowlist change them.",
		"PUT /admin/apps/strict": appsRead + ": tenancies[].strict, one organization's strict mode, by its key (tenancies[].key, org: and its ID). " +
			"Strict mode for every app on the server at once is settings.strictNetwork, changed with PUT /admin/apps/settings.",
		"PUT /admin/apps/oversight": appsRead + ": tenancies[].overseen, by the organization's key (tenancies[].key).",
		"PUT /admin/apps/settings": appsRead + " (settings). It takes the settings whole: send all of them as read there, " +
			"with what you change — strictNetwork true puts every app on the server under strict mode.",
		"PUT /admin/apps/allowlist":              appsRead + " (allowlist).",
		"PUT /admin/backups/replication":         backupsRead + " (replication: enabled, state, lastSyncAt).",
		"PUT /admin/backups/protection/password": backupsRead + " (protection: password, code, opened).",
		"PUT /admin/backups/standby/auto":        backupsRead + " (standby.autoTakeover), or with the MCP tool leafen_standby.",
		"PUT /admin/backups/standby/join":        backupsRead + " (standby: role, peerOrigin), or with the MCP tool leafen_standby.",
		"PUT /organizations/{organizationId}/storage-policy": "Read it back in GET /organizations: each organization's storageLimitBytes " +
			"(null for none) and policyVersion, which expectedPolicyVersion here names.",
	} {
		shape := nativeOperationShape(id)
		if held := nativeOperationNotes[shape]; held != "" {
			note = held + " " + note
		}
		nativeOperationNotes[shape] = note
	}
}
