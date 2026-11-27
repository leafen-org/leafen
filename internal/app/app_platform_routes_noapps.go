//go:build leafen_noapps

package app

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/pocketbase/core"
)

// A certified build leaves the app platform out: no app routes, no workers,
// and the settings window draws no app section.

const appPlatformAvailable = false

// A note about outside sign-ins opens the sign-in settings: there are no
// apps' settings in this build (auth_provider_redirects.go).
const appNoticeAdministratorTarget = "/admin/settings?section=providers"

func registerAppPlatformRoutes(*core.ServeEvent) {}

// There are no app frames in this build (local_owner.go).
func originlessFrameRoute(string) bool { return false }

func startAppPlatformUpkeep(core.App) func(context.Context) error {
	return func(context.Context) error { return nil }
}

func registerMCPAppTools(*mcp.Server, core.App) {}

// No app has tools whose descriptions could run long (mcp_tool_description_limit.go).
func appAgentToolDescriptionsRoom() error { return nil }

// No app has tools for an agent's instructions to name.
func appAgentInstructions(core.App, mcpAuthority) (string, string) { return "", "" }
