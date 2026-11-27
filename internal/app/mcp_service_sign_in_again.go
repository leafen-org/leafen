package app

// How an agent signs one of the person's outside services in again
// (2026-11-22, the user: everything a person can do, an agent does to the
// end): it makes the sign-in address itself with the key's own route
// (POST /mcp-services/{id}/sign-in), opens it in a browser it works in, so
// that the service's own sign-in there is the person's one step, and reads
// the result through MCP. A service signed in with a token has no sign-in
// page: the agent connects it again at its address with the new token put
// in by `leafen mcp secret` from where it is kept, never reading it
// (mcp_secret_command.go; decision corrected 2026-11-22 10:2x); only an
// agent not allowed to handle secrets leaves the token to its person, who
// types it on their own screen (secretsFromPerson, person_steps.go).

// mcpServicesNone is what leafen_services says with no service: an agent
// connection connects one itself as the settings' form does (POST
// /mcp-services), the service's own sign-in or its token being all that is
// the person's (2026-11-22: it said the person connects one in Leafen's
// settings). A connection that uses only the services chosen for it sees
// the new one once it is chosen for it, which the agent connection does
// itself (PUT /mcp/connections/{id}/services, user_key_account_routes.go;
// 2026-11-26: no way is its person's screen's alone). An MCP token, kept to
// its one workspace, hears that connecting one takes an agent connection,
// as its POST /mcp-services is refused (connection_kind_ways.go).
func mcpServicesNone(kind connectionKind) string {
	if kind == connectionKindToken {
		return "This MCP token has no external services yet. " + mcpTokenNotAgent("Connecting one")
	}
	return "This connection has no external services yet. Connect one yourself, as Leafen's settings do: leafen_call POST /mcp-services " +
		"with {\"url\": \"<the service's MCP address>\"} (a \"name\" too, if you like). A service with a login of its own answers with its " +
		"authorizeUrl: open it in a tab of your own in a browser you work in (else in their browser yourself); logging in to the service " +
		"there is the one step that is theirs, so say just that, marked. One that takes a token gets it by " +
		jsonCommandWords("leafen mcp secret POST /mcp-services", `{"url": "<its address>"}`, " --from-file token=<path>", true) +
		" (or --from-env, --from-keychain) on your computer, never " +
		"reading it; only an agent not allowed to handle secrets sends \"secretsFromPerson\": [\"token\"] instead. Then call leafen_services " +
		"again; if it doesn't show the new one, this connection uses only the services chosen for it: choose it yourself with leafen_call " +
		"PUT /mcp/connections/{this connection's id, as GET /mcp/connections lists it}/services with {\"mode\": \"chosen\", \"serviceId\": \"<the new service's id>\", " +
		"\"chosen\": true}."
}

// mcpServiceSignInAgain is the agent's way to sign a service in again; an MCP
// token's is an agent connection, as signing it in crosses its person's
// workspaces.
func mcpServiceSignInAgain(kind connectionKind, serviceID string) string {
	if kind == connectionKindToken {
		return mcpTokenNeedsAgent("logging it in again (leafen_call POST /mcp-services/" + serviceID + "/sign-in)")
	}
	return "log it in again yourself: leafen_call POST /mcp-services/" + serviceID + "/sign-in answers its authorizeUrl, which you open " +
		"in a tab of your own in a browser you work in (else in their browser yourself); logging in to the service there is the one step " +
		"that is theirs, so say just that, marked; leafen_services then shows it connected"
}

// mcpServiceNotConnected is the instructions' word on a service not
// connected, after its line.
func mcpServiceNotConnected(kind connectionKind, service MCPService) string {
	switch {
	case service.Status == mcpServiceStatusFailed && kind == connectionKindToken:
		return "; unreachable when Leafen last tried; " + mcpTokenNeedsAgent("trying it again (POST /mcp-services/"+service.ID+"/check)")
	case service.Status == mcpServiceStatusFailed:
		return "; unreachable when Leafen last tried (leafen_call POST /mcp-services/" + service.ID + "/check tries it again)"
	case service.Auth == mcpServiceAuthOAuth:
		return "; logged out — " + mcpServiceSignInAgain(kind, service.ID)
	}
	return "; its token was refused — " + mcpServiceTokenAgain(kind, service.URL)
}

// mcpServiceTokenAgain is the agent's way to connect a token's service again.
func mcpServiceTokenAgain(kind connectionKind, address string) string {
	if kind == connectionKindToken {
		return mcpTokenNeedsAgent("connecting it again with its new token")
	}
	body := `{"url": "` + address + `"}`
	return "connect it again yourself with its new token, never reading it: on your computer, " +
		jsonCommandWords("leafen mcp secret POST /mcp-services", body, " --from-file token=<path>", true) +
		" (or --from-env token=<NAME>, --from-keychain token=<service>/<account>) where the new token is kept; " +
		"only an agent not allowed to handle secrets sends that body with \"secretsFromPerson\": [\"token\"] through leafen_call instead, " +
		"and its person types the token on their own screen"
}
