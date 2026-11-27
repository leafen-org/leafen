//go:build !leafen_noapps

package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// What a connection's person chose for it (docs/app-platform.md 13, T11):
// every app they may use, or only chosen ones — then the chosen apps' tools
// are also listed as tools of their own, named after the app. Apps' prompts
// and resources come to the connection as MCP prompts and resources, marked
// as the app's own; reading a resource runs the action the app named for it.

const appAgentResourceScheme = "leafen-app://"

var appPromptArgument = regexp.MustCompile(`\{\{\s*([a-zA-Z][a-zA-Z0-9_]{0,40})\s*\}\}`)

// appConnectionChosenApps is the apps a connection was held to, if it was.
func appConnectionChosenApps(app core.App, connectionID string) (map[string]bool, bool, error) {
	choice := ""
	err := app.DB().NewQuery(`SELECT [[app_choice]] FROM {{leafen_mcp_connections}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": connectionID}).Row(&choice)
	if errors.Is(err, sql.ErrNoRows) || err == nil && choice != "chosen" {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	ids := []string{}
	if err := app.DB().NewQuery(`SELECT [[install_id]] FROM {{leafen_mcp_connection_apps}} WHERE [[connection_id]] = {:id}`).
		Bind(dbx.Params{"id": connectionID}).Column(&ids); err != nil {
		return nil, false, err
	}
	chosen := map[string]bool{}
	for _, id := range ids {
		chosen[id] = true
	}
	return chosen, true, nil
}

// appAgentOwnName is an app's tool, prompt or resource named for the
// connection's own list.
func appAgentOwnName(usable usableApp, key string) string {
	return "app_" + strings.ReplaceAll(usable.Install.Slug, "-", "_") + "__" + key
}

type appAgentListed struct {
	usable usableApp
	action appManifestAction
}

// appAgentOwnTools are the chosen apps' tools, each by its own name; an app
// the person reaches in more than one organization stays behind
// leafen_app_call, which names the organization.
func appAgentOwnTools(app core.App, authority mcpAuthority) (map[string]appAgentListed, []*mcp.Tool, error) {
	if _, limited, err := appConnectionChosenApps(app, authority.ConnectionID); err != nil || !limited {
		return nil, nil, err
	}
	apps, err := appAgentApps(app, authority)
	if err != nil {
		return nil, nil, err
	}
	seen := map[string]int{}
	for _, usable := range apps {
		seen[usable.Install.ID]++
	}
	byName, tools := map[string]appAgentListed{}, []*mcp.Tool{}
	for _, usable := range apps {
		if seen[usable.Install.ID] > 1 {
			continue
		}
		for _, action := range usable.Manifest.Actions {
			if !action.offeredToAgents() {
				continue
			}
			name := appAgentOwnName(usable, action.Key)
			schema := any(map[string]any{"type": "object"})
			if len(action.Input) > 0 {
				_ = json.Unmarshal(action.Input, &schema)
			}
			description := appAgentOwnToolDescription(usable.Manifest, action)
			tools = append(tools, &mcp.Tool{Name: name, Title: appDisplayName(usable.Manifest) + ": " + appEnglish(action.Name), Description: description,
				InputSchema: schema, Annotations: &mcp.ToolAnnotations{DestructiveHint: appBool(action.Risk == "high"), OpenWorldHint: appBool(action.ExternalEffect)}})
			byName[name] = appAgentListed{usable: usable, action: action}
		}
	}
	return byName, tools, nil
}

// appAgentOwnToolDescription is an app's tool's description: the app, its
// maker, the action and its maker's words, and that it asks the person
// first where it does.
func appAgentOwnToolDescription(manifest appManifest, action appManifestAction) string {
	description := appDisplayName(manifest) + " app (by " + manifest.Developer.Name + "): " + appEnglish(action.Name)
	if text := appEnglish(action.Description); text != "" {
		description += " — " + appOneLine(text, appDescriptionMaxRunes) + " (the app's own words)"
	}
	if action.RequiresApproval {
		description += ". Asks the person first: answers approval_pending with the question to ask them in the conversation; " +
			"once they say yes, run it with leafen_app_call and the approvalId."
	}
	return description
}

// appAgentToolDescriptionsRoom says whether an app's tool's description,
// at the longest its manifest allows — each name and its maker's words as
// long as they may be, in characters that take two UTF-16 units —, stays
// within what an agent reads (mcp_tool_description_limit.go).
func appAgentToolDescriptionsRoom() error {
	wide := func(runes int) map[string]string { return map[string]string{"en": strings.Repeat("𝕎", runes)} }
	manifest := appManifest{Name: wide(appNameMaxRunes)}
	manifest.Developer.Name = strings.Repeat("𝕎", appNameMaxRunes)
	action := appManifestAction{Name: wide(appNameMaxRunes), Description: wide(appDescriptionMaxRunes + 1), RequiresApproval: true}
	if length := utf16Length(appAgentOwnToolDescription(manifest, action)); length > mcpToolDescriptionLimit {
		return fmt.Errorf("an app's tool at the longest: %d of %d", length, mcpToolDescriptionLimit)
	}
	return nil
}

func appBool(value bool) *bool { return &value }

// appAgentPrompts are the apps' prompts for the connection, by their own names.
func appAgentPrompts(apps []usableApp) (map[string]appAgentPromptListed, []*mcp.Prompt) {
	byName, prompts := map[string]appAgentPromptListed{}, []*mcp.Prompt{}
	for _, usable := range apps {
		if usable.Manifest.Agent == nil {
			continue
		}
		for _, prompt := range usable.Manifest.Agent.Prompts {
			name := appAgentOwnName(usable, prompt.Key)
			if _, taken := byName[name]; taken {
				continue
			}
			listed := &mcp.Prompt{Name: name, Title: appDisplayName(usable.Manifest) + ": " + string(prompt.Name),
				Description: appDisplayName(usable.Manifest) + " app's prompt (by " + usable.Manifest.Developer.Name + "; the app's own words)"}
			for _, match := range appPromptArgument.FindAllStringSubmatch(prompt.Template, -1) {
				if !slices.ContainsFunc(listed.Arguments, func(argument *mcp.PromptArgument) bool { return argument.Name == match[1] }) {
					listed.Arguments = append(listed.Arguments, &mcp.PromptArgument{Name: match[1], Required: true})
				}
			}
			prompts = append(prompts, listed)
			byName[name] = appAgentPromptListed{prompt: prompt, listed: listed}
		}
	}
	return byName, prompts
}

type appAgentPromptListed struct {
	prompt appManifestPrompt
	listed *mcp.Prompt
}

// appAgentResources are the apps' resources for the connection.
func appAgentResources(apps []usableApp) (map[string]appAgentListed, []*mcp.Resource) {
	byURI, resources := map[string]appAgentListed{}, []*mcp.Resource{}
	for _, usable := range apps {
		if usable.Manifest.Agent == nil {
			continue
		}
		for _, resource := range usable.Manifest.Agent.Resources {
			action, found := appManifestActionByKey(usable.Manifest, resource.Action)
			uri := appAgentResourceScheme + usable.Install.Slug + "/" + resource.Key
			if _, taken := byURI[uri]; taken || !found || action.RequiresApproval {
				continue
			}
			resources = append(resources, &mcp.Resource{URI: uri, Name: resource.Key, Title: appDisplayName(usable.Manifest) + ": " + string(resource.Name),
				Description: appDisplayName(usable.Manifest) + " app's data (by " + usable.Manifest.Developer.Name + "; read by running its action " + action.Key + ")",
				MIMEType:    "application/json"})
			byURI[uri] = appAgentListed{usable: usable, action: action}
		}
	}
	return byURI, resources
}

// appAgentMiddleware lists and answers the apps' own tools, prompts and
// resources for the connection, and names the apps in its first
// instructions.
func appAgentMiddleware(app core.App) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, request mcp.Request) (mcp.Result, error) {
			authority, authorityErr := mcpAuthorityFromContext(ctx)
			if authorityErr != nil {
				return next(ctx, method, request)
			}
			switch method {
			case "tools/call":
				call, ok := request.(*mcp.CallToolRequest)
				if !ok || call.Params == nil || !strings.HasPrefix(call.Params.Name, "app_") {
					break
				}
				byName, _, err := appAgentOwnTools(app, authority)
				if err != nil {
					return nil, err
				}
				listed, found := byName[call.Params.Name]
				if !found {
					break
				}
				input := map[string]any{}
				if len(call.Params.Arguments) > 0 && json.Unmarshal(call.Params.Arguments, &input) != nil {
					return appAgentToolError("give the tool's input as a JSON object"), nil
				}
				// It runs as leafen_app_call, through the same checks and
				// the same asking of the person.
				arguments, _ := json.Marshal(appAgentCallInput{App: listed.usable.Install.Slug, Tool: listed.action.Key, Input: input,
					Organization: listed.usable.TenancyKey})
				call.Params.Name, call.Params.Arguments = "leafen_app_call", arguments
				return next(ctx, method, request)
			case "resources/list":
				// Leafen's own resources, such as its guides, come first.
				own, err := next(ctx, method, request)
				if err != nil {
					return own, err
				}
				answer, err := appAgentAnswer(ctx, app, authority, method, request)
				if err != nil {
					return nil, err
				}
				listed := answer.(*mcp.ListResourcesResult)
				if typed, ok := own.(*mcp.ListResourcesResult); ok {
					resources := slices.DeleteFunc(typed.Resources, func(resource *mcp.Resource) bool {
						return !leafenOwnResource(resource.URI)
					})
					listed.Resources = append(resources, listed.Resources...)
				}
				return listed, nil
			case "resources/read":
				if read, ok := request.(*mcp.ReadResourceRequest); ok && read.Params != nil && leafenOwnResource(read.Params.URI) {
					return next(ctx, method, request)
				}
				return appAgentAnswer(ctx, app, authority, method, request)
			case "prompts/list", "prompts/get", "resources/templates/list":
				return appAgentAnswer(ctx, app, authority, method, request)
			}
			result, err := next(ctx, method, request)
			if err != nil {
				return result, err
			}
			switch typed := result.(type) {
			case *mcp.InitializeResult:
				// The apps' tools are named in the instructions with the rest
				// (mcp_session_instructions.go).
				if apps, err := appAgentApps(app, authority); err == nil && typed.Capabilities != nil {
					if _, prompts := appAgentPrompts(apps); len(prompts) > 0 {
						typed.Capabilities.Prompts = &mcp.PromptCapabilities{ListChanged: true}
					}
					if _, resources := appAgentResources(apps); len(resources) > 0 {
						typed.Capabilities.Resources = &mcp.ResourceCapabilities{ListChanged: true}
					}
				}
			case *mcp.ListToolsResult:
				typed.Tools = slices.DeleteFunc(typed.Tools, func(tool *mcp.Tool) bool { return tool.Name == mcpAppsChangedMarker })
				if typed.NextCursor == "" {
					if _, tools, err := appAgentOwnTools(app, authority); err == nil {
						typed.Tools = append(typed.Tools, tools...)
					}
				}
			}
			return result, nil
		}
	}
}

// leafenOwnResourceScheme names the resources Leafen itself gives, beside
// the apps'.
const leafenOwnResourceScheme = "leafen://"

// leafenOwnResource says whether a resource is Leafen's own: one under its
// scheme, or the page Markdown's specification at the address Notion's MCP
// gives its own (notion_page_markdown_spec.go).
func leafenOwnResource(uri string) bool {
	return strings.HasPrefix(uri, leafenOwnResourceScheme) || uri == notionPageMarkdownSpecURI || uri == mcpViewDSLSpecURI
}

func appAgentToolError(message string) *mcp.CallToolResult {
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: message}}}
}

func appAgentAnswer(ctx context.Context, app core.App, authority mcpAuthority, method string, request mcp.Request) (mcp.Result, error) {
	apps, err := appAgentApps(app, authority)
	if err != nil {
		return nil, err
	}
	switch method {
	case "prompts/list":
		_, prompts := appAgentPrompts(apps)
		return &mcp.ListPromptsResult{Prompts: prompts}, nil
	case "prompts/get":
		get, ok := request.(*mcp.GetPromptRequest)
		if !ok || get.Params == nil {
			return nil, mcpInvalidParams("name a prompt")
		}
		byName, _ := appAgentPrompts(apps)
		listed, found := byName[get.Params.Name]
		if !found {
			return nil, mcpInvalidParams("no such prompt for this connection")
		}
		missing := ""
		text := appPromptArgument.ReplaceAllStringFunc(listed.prompt.Template, func(placeholder string) string {
			name := appPromptArgument.FindStringSubmatch(placeholder)[1]
			value, given := get.Params.Arguments[name]
			if !given && missing == "" {
				missing = name
			}
			return value
		})
		if missing != "" {
			return nil, mcpInvalidParams("the prompt needs " + missing)
		}
		return &mcp.GetPromptResult{Description: listed.listed.Description,
			Messages: []*mcp.PromptMessage{{Role: "user", Content: &mcp.TextContent{Text: text}}}}, nil
	case "resources/list":
		_, resources := appAgentResources(apps)
		return &mcp.ListResourcesResult{Resources: resources}, nil
	case "resources/templates/list":
		return &mcp.ListResourceTemplatesResult{ResourceTemplates: []*mcp.ResourceTemplate{}}, nil
	}
	read, ok := request.(*mcp.ReadResourceRequest)
	byURI, _ := appAgentResources(apps)
	if !ok || read.Params == nil {
		return nil, mcpInvalidParams("name a resource")
	}
	listed, found := byURI[read.Params.URI]
	if !found {
		return nil, mcp.ResourceNotFoundError(read.Params.URI)
	}
	if authority.Scope < userKeyScopeContent {
		return nil, mcpInvalidParams("this connection reaches read here; reading an app's resource runs its action and needs content. " + mcpConnectionWiden(authority))
	}
	output, err := runAppAgentAction(ctx, app, authority, listed.usable, listed.action, map[string]any{}, "")
	if err != nil {
		return nil, err
	}
	encoded, _ := json.Marshal(output.Output)
	return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: read.Params.URI, MIMEType: "application/json", Text: string(encoded)}}}, nil
}

func registerConnectionAppRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+appPlatformConnectionAppsReadPath, func(event *core.RequestEvent) error { return connectionAppsRoute(event, false) })
	event.Router.PUT(apiRoot+appPlatformConnectionAppsSetPath, func(event *core.RequestEvent) error { return connectionAppsRoute(event, true) })
}

// connectionAppsRoute shows or changes, for the connection's owner with
// their own sign-in, which apps' tools the connection reaches.
func connectionAppsRoute(event *core.RequestEvent, change bool) error {
	// Its person, signed in or through a key of theirs (user_key_account_routes.go).
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	personID := user.Id
	connectionID := event.Request.PathValue("connectionId")
	owned := struct {
		Choice string `db:"app_choice"`
	}{}
	if err := event.App.DB().NewQuery(`SELECT [[app_choice]] FROM {{leafen_mcp_connections}}
		WHERE [[id]] = {:id} AND [[owner_id]] = {:owner} AND [[revoked_at]] = 0 AND [[app_install_id]] = ''`).
		Bind(dbx.Params{"id": connectionID, "owner": personID}).One(&owned); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return apiError(event, http.StatusNotFound, errorCodeAppPlatformInputInvalid, "No such connection.")
		}
		return appPlatformFailure(event, err)
	}
	workspaceIDs := []string{}
	if err := event.App.DB().NewQuery(`SELECT [[workspace_id]] FROM {{leafen_mcp_connection_workspaces}} WHERE [[connection_id]] = {:id}`).
		Bind(dbx.Params{"id": connectionID}).Column(&workspaceIDs); err != nil {
		return appPlatformFailure(event, err)
	}
	reachable, err := appAgentReachableApps(event.App, personID, workspaceIDs)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	if change {
		input := appConnectionAppsInput{}
		if err := json.NewDecoder(event.Request.Body).Decode(&input); err != nil {
			return apiError(event, http.StatusBadRequest, errorCodeAppPlatformInputInvalid,
				refusalMessage("Choose all apps or the chosen ones.", bodyRefusalReason("choice of apps", err)))
		}
		if input.Mode != "all" && input.Mode != "chosen" {
			return apiError(event, http.StatusBadRequest, errorCodeAppPlatformInputInvalid,
				refusalMessage("Choose all apps or the chosen ones.", fmt.Sprintf("mode is all or chosen, not %q", input.Mode)))
		}
		for position, id := range input.InstallIDs {
			if !slices.ContainsFunc(reachable, func(usable usableApp) bool { return usable.Install.ID == id }) {
				return apiError(event, http.StatusBadRequest, errorCodeAppPlatformInputInvalid,
					refusalMessage("Choose among the apps listed.", fmt.Sprintf("installIds[%d] is %q, which isn't an app this connection's workspaces use", position, id)))
			}
		}
		if err := event.App.RunInTransaction(func(tx core.App) error {
			if _, err := tx.DB().Update("leafen_mcp_connections", dbx.Params{"app_choice": input.Mode}, dbx.HashExp{"id": connectionID}).Execute(); err != nil {
				return err
			}
			if _, err := tx.DB().Delete("leafen_mcp_connection_apps", dbx.HashExp{"connection_id": connectionID}).Execute(); err != nil {
				return err
			}
			for _, id := range input.InstallIDs {
				if _, err := tx.DB().NewQuery(`INSERT OR IGNORE INTO {{leafen_mcp_connection_apps}} ([[connection_id]], [[install_id]], [[created]])
					VALUES ({:connection}, {:install}, {:now})`).Bind(dbx.Params{"connection": connectionID, "install": id, "now": time.Now().UnixMilli()}).Execute(); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return appPlatformFailure(event, err)
		}
		owned.Choice = input.Mode
		mcpAppsChanged()
	}
	chosen, _, err := appConnectionChosenApps(event.App, connectionID)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	wire := appConnectionAppsWire{Mode: "all", Apps: []appConnectionAppWire{}}
	if owned.Choice == "chosen" {
		wire.Mode = "chosen"
	}
	for _, usable := range reachable {
		// Only apps that offer agents something: tools, prompts or resources.
		summary := appAgentSummary(event.App, "", usable)
		if agent := usable.Manifest.Agent; summary.Tools == 0 && (agent == nil || len(agent.Prompts) == 0 && len(agent.Resources) == 0) {
			continue
		}
		wire.Apps = append(wire.Apps, appConnectionAppWire{InstallID: usable.Install.ID, Slug: usable.Install.Slug, Name: appTextOrEmpty(usable.Manifest.Name),
			Developer: usable.Manifest.Developer.Name, Builtin: appInstallBuiltin(event.App, usable.Install), Organization: usable.TenancyName, Tools: summary.Tools, Chosen: chosen[usable.Install.ID]})
	}
	return event.JSON(http.StatusOK, wire)
}
