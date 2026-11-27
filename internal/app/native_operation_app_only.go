package app

// The operations no person's key runs, whatever its scope: an app's own
// token or account, or an app guest's session, is what their handlers take.
// The operations list says so beside the scope (2026-11-16: on a server with
// no apps, an agent found /app-data and /app-groups listed as "read" and got
// 403 from each).
var nativeOperationsOfAppsOnly = map[string]string{}

func init() {
	const appOnly = " (an app's own token only: a person's key is refused)"
	const guestOnly = " (an app guest's session only: a key is refused)"
	for id, mark := range map[string]string{
		// Where the app's own data went, and its people (app_platform_data_map.go).
		"GET " + appPlatformDataMapPath: appOnly, "GET " + appPlatformPeoplePath: appOnly,
		"GET " + appPlatformGroupsPath: appOnly, "GET " + appPlatformWorkspacesPath: appOnly,
		// Its relay and outside sign-ins, which a key reaches only as the app (appRelayCaller).
		"POST " + appPlatformRelayPath: appOnly, "GET " + appPlatformOAuthAccountsPath: appOnly,
		"POST " + appPlatformOAuthStartPath: appOnly, "DELETE " + appPlatformOAuthDisconnectPath: appOnly,
		// What the app does as itself (appTokenInstall).
		"POST " + appPlatformActionRunPath: appOnly, "POST " + appPlatformKVPath: appOnly, "POST " + appPlatformStoragePath: appOnly,
		"POST " + appPlatformRoomTicketPath: appOnly, "POST " + appPlatformGuestLinkMakePath: appOnly, "POST " + appPlatformGuestReportPath: appOnly,
		// A guest's own, by the guest session's token (appGuestFromRequest).
		"POST " + appPlatformGuestKVPath: guestOnly, "POST " + appPlatformGuestRoomTicketPath: guestOnly,
	} {
		nativeOperationsOfAppsOnly[nativeOperationShape(id)] = mark
	}
}

// nativeOperationAppOnly is what an operation's scope adds when no person's
// key runs it.
func nativeOperationAppOnly(id string) string {
	return nativeOperationsOfAppsOnly[nativeOperationShape(id)]
}
