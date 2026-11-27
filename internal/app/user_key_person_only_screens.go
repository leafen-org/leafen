package app

import "github.com/pocketbase/pocketbase/core"

// Where the person does what no key does (user_key_account_routes.go: a
// press their own device or sign-in must make, or a choice of what an agent
// may reach, which no agent makes for itself — user decision 2026-11-22, 1
// and 3), said in the refusal so the agent leads them to just that one step
// (2026-11-22: the refusals said only "signed in", and an agent handed the
// whole task to its person): the screen as the person's screen names it
// (mcp_screen_names.go) and the step there, kept here, in one place.

type userKeyPersonOnlyStep struct {
	// The way to the screen, as mcpScreen names it; none where no fixed
	// screen of Leafen's is the place (an app's window, another device).
	screen []string
	step   string
}

var userKeyPersonOnlySteps = map[string]userKeyPersonOnlyStep{
	"/apps/{installId}/approvals/{approvalId}/approve": {step: "they answer it where the app asks them — its window, or the notice in their inbox; ask them and wait"},
	"/apps/{installId}/approvals/{approvalId}/decline": {step: "they answer it where the app asks them — its window, or the notice in their inbox; ask them and wait"},
	"/account/device-codes": {screen: []string{"addDevice"}, step: "they make a code for a device of theirs in the workspace menu there; " +
		"a browser you work in logs in by its own “Log in with AI” and leafen_sign_in_requests instead"},
	"/account/device-codes/{codeId}":           {screen: []string{"addDevice"}, step: "they end it in the workspace menu there"},
	"/admin/accounts/{accountId}/device-codes": {screen: []string{"settings", "server", "serverUsers"}, step: "they make it at that account's row"},
	"/account/address-handoff":                 {step: "they open it from the browser that is logged in, where Leafen offers the other address"},
	"/account/app-sign-in":                     {step: "they sign the Leafen app on their device in from a browser that is logged in"},
	"/local-owner/settled":                     {step: "they finish it in the Leafen desktop app's window on that computer"},
	"/account/passkeys/begin":                  {screen: []string{"settings", "accountSecurity"}, step: "they add a passkey there, on their own device"},
	"/account/passkeys/finish":                 {screen: []string{"settings", "accountSecurity"}, step: "they add a passkey there, on their own device"},
	"/account/passkeys":                        {screen: []string{"settings", "accountSecurity"}, step: "they remove a passkey there, on their own device"},
	"/account/sessions/current/second-factor":  {step: "they enter it in the browser that is logging in"},
	"/mcp/connections/{id}/token/reveal":       {screen: []string{"settings", "agents", "agentsConnected"}, step: "they see its key on this connection's page there"},
	"/mcp/connections/{id}/pages":              {screen: []string{"settings", "agents", "agentsConnected"}, step: "they choose an MCP token's pages on its page there (an agent connection reaches every page, with none to choose)"},
	"/mcp/device-connections/find":             {screen: []string{"settings", "agents", "agentsOtherComputer"}, step: "they type the other agent's code there"},
	"/mcp/device-connections/decide":           {screen: []string{"settings", "agents", "agentsOtherComputer"}, step: "they type the other agent's code there"},
}

// userKeyClosedWhere is a closed route's refusal with where its person does
// it and the one step there, in the words of their screen.
func userKeyClosedWhere(app core.App, ownerID, path, reason string) string {
	step, listed := userKeyPersonOnlySteps[path]
	if !listed {
		return reason
	}
	return reason + " The person does it" + step.where(mcpScreenLanguage(app, ownerID)) + ": " + step.step + "."
}

// where is the screen the step is on, as " at <screen>", or none.
func (step userKeyPersonOnlyStep) where(language string) string {
	if len(step.screen) == 0 {
		return ""
	}
	return " at " + mcpScreen(language, step.screen...)
}

// userKeyPersonOnlyPlace is where a closed route's person does it and the
// step there, after the words that say it is theirs (", at <screen>: <step>"),
// as leafen_operations lists it beside the operation (native_operation_grant.go);
// none for a route with no place kept here.
func userKeyPersonOnlyPlace(language, path string) string {
	step, listed := userKeyPersonOnlySteps[path]
	if !listed {
		return ""
	}
	return step.where(language) + " — " + step.step
}
