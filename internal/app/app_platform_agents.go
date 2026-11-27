//go:build !leafen_noapps

package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/pocketbase/core"
)

// Apps' tools for agents (docs/app-platform.md 13, T11): on the native MCP
// surface, leafen_app_tools lists the apps a connection's person may use in
// the connection's organizations, or one app's tools — its actions marked as
// tools, with their input schemas — and the usage notes its maker wrote;
// leafen_app_call runs one through the same checks and worker as the app's
// screen. What it may reach is the connection's scope and workspaces within
// what the person reaches through the app. The connection's first
// instructions name the apps in a line each; the app's own words stay one
// line there and are marked as the app's in the tool list.

const appAgentInstructionApps = 20

type appAgentToolsInput struct {
	App string `json:"app,omitempty" jsonschema:"An app's name from the list, to see its tools with their input schemas and the notes its maker wrote."`
}

type appAgentApp struct {
	App          string   `json:"app"`
	Name         string   `json:"name"`
	Summary      string   `json:"summary,omitempty"`
	Developer    string   `json:"developer"`
	Organization string   `json:"organization"`
	Tools        int      `json:"tools"`
	NeedApproval []string `json:"needApproval,omitempty"`
	// URL is the app's screen, for a link the agent gives its person: in the
	// desktop app its window's, else where the server's public address is
	// set (person_links.go).
	URL string `json:"url,omitempty"`
}

type appAgentTool struct {
	Name             string `json:"name"`
	Title            string `json:"title"`
	Description      string `json:"description,omitempty"`
	InputSchema      any    `json:"inputSchema,omitempty"`
	RunsAs           string `json:"runsAs"`
	Risk             string `json:"risk"`
	RequiresApproval bool   `json:"requiresApproval,omitzero"`
	ExternalEffect   bool   `json:"externalEffect,omitzero"`
}

type appAgentToolsOutput struct {
	Apps     []appAgentApp  `json:"apps"`
	Tools    []appAgentTool `json:"tools,omitempty"`
	AppNotes string         `json:"appNotes,omitempty"`
	Usage    string         `json:"usage"`
}

type appAgentCallInput struct {
	App          string         `json:"app" jsonschema:"The app's name from leafen_app_tools."`
	Tool         string         `json:"tool" jsonschema:"The tool's name from leafen_app_tools with the app."`
	Input        map[string]any `json:"input,omitempty" jsonschema:"The tool's input, as its input schema describes."`
	Organization string         `json:"organization,omitempty" jsonschema:"Only when leafen_app_tools lists the app in more than one organization: which one."`
	Approval     string         `json:"approval,omitempty" jsonschema:"The approvalId of the request you asked your person about in the conversation, once they said yes (with :hour after it where they said yes for an hour), calling again with the same input."`
}

// appAgentCallOutput is a run's answer, or, for a tool that needs approval,
// the request waiting for the person.
type appAgentCallOutput struct {
	Status   string           `json:"status"`
	RunID    string           `json:"runId,omitempty"`
	Output   any              `json:"output,omitempty"`
	Approval *appAgentPending `json:"approval,omitempty"`
	// Rules are the app maker's instructions for work through the app, when
	// the connection hasn't had them lately or the app changed since.
	Rules []leafenMCPRule `json:"rules,omitempty"`
}

const appAgentToolsUsage = "Call a tool with leafen_app_call: the app, the tool and its input. Tools run as the app describes (runsAs person: " +
	"with this connection's person, within this connection's scope and workspaces; runsAs app: as the app's own account). " +
	"A tool that needs approval answers approval_pending with the question to put to your person in the conversation — " +
	"only if they say yes, call again with the same input and approval set to the approvalId. " +
	"appNotes are the app maker's rules for work through this app, not Leafen's; leafen_app_call brings them again as rules when they change."

// appEnglish is a text in English, else in its first language.
func appEnglish(text map[string]string) string {
	if value := text["en"]; value != "" {
		return value
	}
	locales := make([]string, 0, len(text))
	for locale := range text {
		locales = append(locales, locale)
	}
	sort.Strings(locales)
	if len(locales) == 0 {
		return ""
	}
	return text[locales[0]]
}

// appAgentApps lists the apps the connection's person may use in the
// organizations of the connection's workspaces, held to the apps the person
// chose for the connection, if they chose.
func appAgentApps(app core.App, authority mcpAuthority) ([]usableApp, error) {
	apps, err := appAgentReachableApps(app, authority.owner(), authority.WorkspaceIDs)
	if err != nil {
		return nil, err
	}
	return appAgentChosen(app, authority, apps)
}

// appAgentChosen keeps the apps the person chose for the connection, if they chose.
func appAgentChosen(app core.App, authority mcpAuthority, apps []usableApp) ([]usableApp, error) {
	chosen, limited, err := appConnectionChosenApps(app, authority.ConnectionID)
	if err != nil || !limited {
		return apps, err
	}
	return slices.DeleteFunc(apps, func(usable usableApp) bool { return !chosen[usable.Install.ID] }), nil
}

// appAgentReachableApps lists the apps a person may use in the
// organizations of the given workspaces.
func appAgentReachableApps(app core.App, personID string, workspaceIDs []string) ([]usableApp, error) {
	apps, err := usableApps(app, personID)
	if err != nil {
		return nil, err
	}
	return appAgentInWorkspaces(app, apps, personID, workspaceIDs)
}

// appAgentInWorkspaces keeps the apps in the organizations of the given workspaces.
func appAgentInWorkspaces(app core.App, apps []usableApp, personID string, workspaceIDs []string) ([]usableApp, error) {
	reached := map[string]bool{}
	result := []usableApp{}
	for _, usable := range apps {
		reach, known := reached[usable.TenancyKey]
		if !known {
			workspaces, err := appPersonWorkspaces(app, personID, usable.TenancyKey)
			if err != nil {
				return nil, err
			}
			reach = slices.ContainsFunc(workspaces, func(id string) bool { return slices.Contains(workspaceIDs, id) })
			reached[usable.TenancyKey] = reach
		}
		if reach {
			result = append(result, usable)
		}
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].Install.Slug < result[j].Install.Slug })
	return result, nil
}

func appAgentSummary(app core.App, person string, usable usableApp) appAgentApp {
	summary := appAgentApp{App: usable.Install.Slug, Name: appDisplayName(usable.Manifest), Developer: usable.Manifest.Developer.Name,
		Organization: usable.TenancyName, URL: personPublicLink(app, person, "/apps/"+usable.Install.Slug)}
	if usable.Manifest.Agent != nil {
		summary.Summary = appOneLine(appEnglish(usable.Manifest.Agent.Summary), 120)
	}
	// An app that says nothing to agents is told by its description, so no
	// app hands out tools under its name alone (2026-11-09: the vault).
	if summary.Summary == "" {
		summary.Summary = appOneLine(appEnglish(usable.Manifest.Description), 120)
	}
	for _, action := range usable.Manifest.Actions {
		if action.offeredToAgents() {
			summary.Tools++
			if action.RequiresApproval || action.Risk == "high" {
				summary.NeedApproval = append(summary.NeedApproval, action.Key)
			}
		}
	}
	return summary
}

func listAppAgentTools(app core.App, authority mcpAuthority, input appAgentToolsInput) (appAgentToolsOutput, error) {
	apps, err := appAgentApps(app, authority)
	if err != nil {
		return appAgentToolsOutput{}, err
	}
	output := appAgentToolsOutput{Apps: []appAgentApp{}, Usage: appAgentToolsUsage}
	name := strings.TrimSpace(input.App)
	var chosen *usableApp
	for index, usable := range apps {
		if name != "" && usable.Install.Slug != name {
			continue
		}
		output.Apps = append(output.Apps, appAgentSummary(app, authority.owner(), usable))
		chosen = &apps[index]
	}
	if name == "" {
		return output, nil
	}
	if chosen == nil {
		return appAgentToolsOutput{}, appAgentNoApp(app, authority, name, "call leafen_app_tools without an app to list them")
	}
	output.Tools = []appAgentTool{}
	for _, action := range chosen.Manifest.Actions {
		if !action.offeredToAgents() {
			continue
		}
		tool := appAgentTool{Name: action.Key, Title: appEnglish(action.Name), Description: appEnglish(action.Description), RunsAs: action.RunAs,
			Risk: action.Risk, RequiresApproval: action.RequiresApproval || action.Risk == "high", ExternalEffect: action.ExternalEffect}
		if len(action.Input) > 0 {
			_ = json.Unmarshal(action.Input, &tool.InputSchema)
		}
		output.Tools = append(output.Tools, tool)
	}
	if rules := appAgentRules(app, authority, *chosen, false); len(rules) > 0 {
		output.AppNotes = rules[0].Body
	}
	return output, nil
}

// appAgentRules are an app's agent instructions as the rule of work through
// that app (app_platform_agent_notes.go): with onlyNew, only when the
// connection hasn't had them lately or the app's package changed since.
func appAgentRules(app core.App, authority mcpAuthority, usable usableApp, onlyNew bool) []leafenMCPRule {
	if usable.Manifest.Agent == nil {
		return nil
	}
	notes := strings.TrimSpace(appEnglish(usable.Manifest.Agent.Instructions))
	key := authority.ConnectionID + "\x00app:" + usable.Install.ID
	if notes == "" || onlyNew && !mcpRuleDue(key, usable.Install.PackageHash) {
		return nil
	}
	mcpRuleMarkSent(key, usable.Install.PackageHash)
	rule := leafenMCPRule{Title: appDisplayName(usable.Manifest), Scope: "app", Body: notes,
		URL: personPublicLink(app, authority.owner(), "/apps/"+usable.Install.Slug)}
	return []leafenMCPRule{rule}
}

// callAppAgentTool runs one of an app's tools for the connection's person.
func callAppAgentTool(ctx context.Context, app core.App, authority mcpAuthority, input appAgentCallInput) (appAgentCallOutput, error) {
	apps, err := appAgentApps(app, authority)
	if err != nil {
		return appAgentCallOutput{}, err
	}
	matches := []usableApp{}
	for _, usable := range apps {
		if usable.Install.Slug == strings.TrimSpace(input.App) &&
			(input.Organization == "" || input.Organization == usable.TenancyName || input.Organization == usable.TenancyKey) {
			matches = append(matches, usable)
		}
	}
	switch {
	case len(matches) == 0:
		return appAgentCallOutput{}, appAgentNoApp(app, authority, strings.TrimSpace(input.App), "call leafen_app_tools to list them")
	case len(matches) > 1:
		names := []string{}
		for _, usable := range matches {
			names = append(names, usable.TenancyName)
		}
		return appAgentCallOutput{}, fmt.Errorf("the app is in more than one organization; name one: %s", strings.Join(names, ", "))
	}
	usable := matches[0]
	action, found := appManifestActionByKey(usable.Manifest, input.Tool)
	if !found || !action.offeredToAgents() {
		return appAgentCallOutput{}, fmt.Errorf("the app has no tool %q; call leafen_app_tools with the app to list them", input.Tool)
	}
	output, err := runAppAgentAction(ctx, app, authority, usable, action, input.Input, input.Approval)
	if err == nil {
		output.Rules = appAgentRules(app, authority, usable, true)
	}
	return output, err
}

// runAppAgentAction runs one of an app's actions for the connection's
// person — as a tool, a listed tool of its own or a resource's reading.
func runAppAgentAction(ctx context.Context, app core.App, authority mcpAuthority, usable usableApp, action appManifestAction,
	input map[string]any, approval string) (appAgentCallOutput, error) {
	if input == nil {
		input = map[string]any{}
	}
	// A high-risk action a person confirms on the app's screen an agent asks
	// them for, as with an approval (agent full-permission decision of
	// 2026-10-16, 2).
	if action.Risk == "high" {
		action.RequiresApproval = true
	}
	if err := validateAppActionInput(action, input); err != nil {
		return appAgentCallOutput{}, fmt.Errorf("the input doesn't fit the tool: %w", err)
	}
	required, err := appApprovalRequired(app, usable.Install, action, authority.owner(), authority.ConnectionID, usable.TenancyKey)
	if err != nil {
		return appAgentCallOutput{}, err
	}
	canonical, _ := json.Marshal(input)
	digest := sha256.Sum256(canonical)
	inputHash := hex.EncodeToString(digest[:])
	if required {
		// Asked by the agent inside the conversation (app_platform_agent_ask.go).
		ask := appAgentAsk(app, usable, action, authority, canonical)
		outcome, err := askPerson(appApprovalRequests{app: app, usable: usable, action: action, authority: authority, inputHash: inputHash,
			input: canonical}, personAskCall{PassedOn: approval}, ask)
		switch {
		case err != nil:
			return appAgentCallOutput{}, err
		case outcome.Declined:
			return appAgentCallOutput{Status: "declined"}, nil
		case !outcome.Proceed:
			message := personAskConversation(ask, "call leafen_app_call again with the same input and approval set to "+outcome.ID+".")
			return appAgentCallOutput{Status: "approval_pending",
				Approval: &appAgentPending{ApprovalID: outcome.ID, ExpiresAt: outcome.ExpiresAt, Ask: ask, Message: message}}, nil
		}
	}
	personID := authority.owner()
	if _, err := prepareAppForPerson(app, usable, personID); err != nil {
		return appAgentCallOutput{}, err
	}
	// The person's own data, as their first opening of the app makes it.
	readyAppPersonData(app, usable, personID)
	workerInput, _ := json.Marshal(withAppActionCaller(app, map[string]any{"action": action.Key, "input": input, "person": personID,
		"tenancy": usable.TenancyKey, "via": map[string]string{"kind": "agent", "connection": authority.ConnectionID, "name": authority.ClientName}},
		personID, usable.TenancyKey))
	run := appRun{Trigger: "tool", Ref: action.Key, Subject: personID, Worker: action.Worker, Function: action.Function, Input: workerInput,
		Tenancy: usable.TenancyKey}
	if action.RunAs == "person" {
		person, err := appAgentPersonAuthority(app, usable, authority)
		if err != nil {
			return appAgentCallOutput{}, err
		}
		run.Person = &person
	}
	output, runID, runErr := runAppWorkerFunction(ctx, app, usable.Install, usable.Manifest, run)
	status := "ok"
	if runErr != nil {
		status = "failed"
	}
	if err := recordAppAudit(app, usable.Install, "agent-tool", personID, map[string]any{
		"connection": authority.ConnectionID, "client": authority.ClientName, "tool": action.Key, "run": runID, "status": status,
	}); err != nil {
		return appAgentCallOutput{}, err
	}
	if errors.Is(runErr, errAppWorkerBusy) {
		return appAgentCallOutput{}, errors.New("the app is busy; try again in a moment")
	}
	if errors.Is(runErr, errAppWorkerHalted) {
		return appAgentCallOutput{}, errors.New("the app is stopped on this server; only an administrator can bring it back")
	}
	if errors.As(runErr, new(appWorkerPastLimit)) {
		return appAgentCallOutput{}, fmt.Errorf("the tool ran past its time limit (run %s)", runID)
	}
	// The app's own reason for refusing reaches the agent.
	if reason, refused := appWorkerRefusalText(runErr); refused {
		return appAgentCallOutput{}, fmt.Errorf("the app refused: %s (run %s)", reason, runID)
	}
	if runErr != nil {
		return appAgentCallOutput{}, fmt.Errorf("the tool failed (run %s)", runID)
	}
	result := appAgentCallOutput{Status: "done", RunID: runID, Output: string(output)}
	if decoded := any(nil); json.Valid(output) && json.Unmarshal(output, &decoded) == nil {
		result.Output = decoded
	}
	return result, nil
}

// appAgentNoApp says why a name finds no app: one this connection would reach
// but an administrator stopped is the agent's to report, not to look for under
// another name. To any other connection it is no app at all.
func appAgentNoApp(app core.App, authority mcpAuthority, name, next string) error {
	noApp := fmt.Errorf("no app %q for this connection; %s", name, next)
	stopped, err := grantedApps(app, authority.owner(), "[[status]] != 'active' AND [[removed_at]] = 0")
	if err != nil {
		return noApp
	}
	if stopped, err = appAgentInWorkspaces(app, stopped, authority.owner(), authority.WorkspaceIDs); err != nil {
		return noApp
	}
	if stopped, err = appAgentChosen(app, authority, stopped); err != nil {
		return noApp
	}
	for _, usable := range stopped {
		if usable.Install.Slug == name {
			return fmt.Errorf("the app %q is stopped on this server; only an administrator can bring it back", name)
		}
	}
	return noApp
}

// appAgentPersonAuthority is what a tool run as the person reaches: the
// person's own app connection, held to the agent connection's scope and
// workspaces.
func appAgentPersonAuthority(app core.App, usable usableApp, authority mcpAuthority) (userKeyAuthority, error) {
	scope, known := parseUserKeyScope(usable.Manifest.PersonAccess)
	if !known {
		return userKeyAuthority{}, errors.New("the app has no access to data as a person")
	}
	own, err := appWorkspaceForPerson(app, usable, authority.owner())
	if err != nil {
		return userKeyAuthority{}, err
	}
	workspaces, err := appPersonWorkspaces(app, authority.owner(), usable.TenancyKey)
	if err != nil {
		return userKeyAuthority{}, err
	}
	connectionID, err := appConnection(app, usable.Install, usable.Manifest, usable.TenancyKey, authority.owner(), workspaces)
	if err != nil {
		return userKeyAuthority{}, err
	}
	now := time.Now()
	return userKeyAuthority{
		ConnectionID: connectionID, Name: appDisplayName(usable.Manifest), OwnerID: authority.owner(), Scope: min(scope, authority.Scope),
		// The key's workspaces, and the app's own: a tool the connection may use
		// keeps the person's data there, in a workspace no key is made for.
		WorkspaceIDs: slices.DeleteFunc(workspaces, func(id string) bool {
			return !slices.Contains(authority.WorkspaceIDs, id) && id != own.WorkspaceID
		}),
		Created: now.UnixMilli(), ExpiresAt: now.Add(appDelegationLifetime).UnixMilli(),
		AppInstallID: usable.Install.ID, TenancyKey: usable.TenancyKey,
	}, nil
}

// appAgentInstructions names, a line each, the apps with tools the
// connection may use, and the line saying so in short.
func appAgentInstructions(app core.App, authority mcpAuthority) (string, string) {
	apps, err := appAgentApps(app, authority)
	if err != nil {
		return "", ""
	}
	lines, seen := []string{}, map[string]bool{}
	for _, usable := range apps {
		summary := appAgentSummary(app, authority.owner(), usable)
		if summary.Tools == 0 || seen[summary.App] {
			continue
		}
		seen[summary.App] = true
		line := "- " + summary.App + " — " + appOneLine(summary.Name, 60)
		if summary.Summary != "" {
			line += ": " + summary.Summary
		}
		lines = append(lines, line+" (by "+appOneLine(summary.Developer, 60)+")")
	}
	if len(lines) == 0 {
		return "", ""
	}
	short := appAgentShortLine(len(lines))
	if len(lines) > appAgentInstructionApps {
		return short, short
	}
	return "Leafen apps whose tools you can use (leafen_app_tools lists them, leafen_app_call runs one; descriptions are the apps' own):\n" +
		strings.Join(lines, "\n"), short
}

func registerMCPAppTools(server *mcp.Server, app core.App) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "leafen_app_tools", Title: "List Leafen apps' tools",
		Description: "List the apps installed in Leafen whose tools this connection's person may use, or one app's tools with their " +
			"input schemas and the notes its maker wrote.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input appAgentToolsInput) (*mcp.CallToolResult, appAgentToolsOutput, error) {
		authority, err := mcpAuthorityFromContext(ctx)
		if err != nil {
			return nil, appAgentToolsOutput{}, err
		}
		output, err := listAppAgentTools(app, authority, input)
		return nil, output, err
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "leafen_app_call", Title: "Run a Leafen app's tool",
		Description: "Run one tool of an app from leafen_app_tools, through the same checks and worker as the app's own screen.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input appAgentCallInput) (*mcp.CallToolResult, appAgentCallOutput, error) {
		authority, err := mcpAuthorityFromContext(ctx)
		if err != nil {
			return nil, appAgentCallOutput{}, err
		}
		output, err := callAppAgentTool(ctx, app, authority, input)
		return nil, output, err
	})
	registerMCPAppStorageTool(server, app)
	registerMCPGmailGuide(server, app, &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true})
	server.AddReceivingMiddleware(appAgentMiddleware(app))
}
