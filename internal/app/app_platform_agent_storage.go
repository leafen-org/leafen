//go:build !leafen_noapps

package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/pocketbase/core"
)

// An app's screen keeps the person's own settings for it in their store
// (app_platform_storage.go); their agent reads and changes that store too,
// by the app's name (agent full-permission decision of 2026-10-16, 6).

type appAgentStorageInput struct {
	App     string `json:"app" jsonschema:"The app's name from leafen_app_tools."`
	Tenancy string `json:"tenancy,omitempty" jsonschema:"The organization, for a person who uses the app in more than one (leafen_app_tools lists them)."`
	Command string `json:"command" jsonschema:"get, set, remove or keys."`
	Key     string `json:"key,omitempty" jsonschema:"The key, for get, set and remove."`
	Value   any    `json:"value,omitempty" jsonschema:"The JSON value to set."`
}

func registerMCPAppStorageTool(server *mcp.Server, app core.App) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "leafen_app_storage", Title: "Read or change your store for a Leafen app",
		Description: "The person's own store for an app — the settings its screen keeps for them: get a key, set or remove one, or list the keys.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input appAgentStorageInput) (*mcp.CallToolResult, appStorageResult, error) {
		authority, err := mcpAuthorityFromContext(ctx)
		if err != nil {
			return nil, appStorageResult{}, err
		}
		command := strings.TrimSpace(input.Command)
		if (command == "set" || command == "remove") && authority.Scope < userKeyScopeContent {
			return nil, appStorageResult{}, fmt.Errorf("this connection reaches %s here; changing an app's store needs content. %s", userKeyScopeNames[authority.Scope], mcpConnectionWiden(authority))
		}
		apps, err := appAgentApps(app, authority)
		if err != nil {
			return nil, appStorageResult{}, err
		}
		found := []usableApp{}
		for _, usable := range apps {
			if usable.Install.Slug == strings.TrimSpace(input.App) && (input.Tenancy == "" || usable.TenancyKey == input.Tenancy) {
				found = append(found, usable)
			}
		}
		if len(found) != 1 {
			return nil, appStorageResult{}, fmt.Errorf("%w: name one app you may use (leafen_app_tools), and its organization if you use it in more than one", errMCPToolInvalid)
		}
		result, err := runAppStorage(app, found[0].Install.ID, found[0].TenancyKey, authority.owner(),
			appStorageRequest{Command: command, Key: input.Key, Value: input.Value}, time.Now())
		if errors.Is(err, errAppStorageRefused) || errors.Is(err, errAppStorageFull) {
			return nil, appStorageResult{}, fmt.Errorf("%w: %s", errMCPToolInvalid, err.Error())
		}
		return nil, result, err
	})
}
