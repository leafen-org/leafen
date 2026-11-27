package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/pocketbase/core"
)

// A Notion token handed over to Leafen from the agent (user decisions
// 2026-11-13: setting up, moving from Notion and using Leafen can all go
// through the conversation alone; 2026-11-19 22:1x: an agent with a browser
// gets the token itself and hands it over here, never sending the person to
// the import screen; 2026-11-20 04:5x: the key comes through the
// conversation whoever made it — the agent in its browser, theirs or its
// own, or the person, who pastes it here). Leafen keeps it as the import
// screen's hidden field does — sealed with the server's key, never shown
// again — and the agent goes on with the migration's tools. The conversation
// keeps the token, so once the move is done the agent tells the person to
// delete that connection in Notion or make it a new token. A token kept in a
// file, an environment variable or the keychain goes in without passing
// through the conversation, by `leafen mcp secret` and the import screen's
// own operation (mcp_secret_command.go, user decision 2026-11-22). The import
// screen's own field is only for a person who'd rather not paste it into the
// conversation (notion_import_handoff.go).

type leafenMCPNotionMigrationConnectInput struct {
	Token       string `json:"token" jsonschema:"The Notion internal integration token (ntn_…): the one you read in Notion in your browser, or the one they pasted into this conversation"`
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"Workspace to migrate into, when the person has more than one"`
}

type leafenMCPNotionMigrationConnectOutput struct {
	Connection leafenMCPNotionConnection `json:"connection"`
	Next       string                    `json:"next"`
}

func registerMCPNotionMigrationConnectTool(server *mcp.Server, app core.App) {
	idempotent := true
	mcp.AddTool(server, &mcp.Tool{
		Name: "leafen_notion_migration_connect", Title: "Hand Leafen a Notion token",
		Description: "Keeps a Notion internal integration token as the workspace's Notion connection, encrypted in Leafen as its import screen keeps one, " +
			"once Notion takes it; answers the connection to migrate from. Hand it over at once, without repeating it: the token you read in Notion in your " +
			"browser — theirs or your own — after their yes, or one they pasted into this conversation (leafen_notion_migration_guide section 1); " +
			"this is the token's way, never Leafen's import screen. This conversation keeps the token, so once the migration is done, left to do is a line to them that " +
			"deleting that connection in Notion, or making it a new token, ends it. A token in a file, an environment variable or the keychain — or one you'd rather keep " +
			"out of this conversation — goes in without its value passing through you: " +
			jsonCommandWords("leafen mcp secret POST /workspaces/<workspace_id>/notion-import/connections",
				`{"requestId": "<any new id>", "label": "Notion"}`, " --from-file token=<path>", true) +
			" (or --from-env, --from-keychain) on your computer; " +
			"leafen_notion_migration_sources then lists the connection.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: idempotent, DestructiveHint: new(bool), OpenWorldHint: &idempotent},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input leafenMCPNotionMigrationConnectInput) (*mcp.CallToolResult, leafenMCPNotionMigrationConnectOutput, error) {
		return connectMCPNotionMigration(ctx, app, input)
	})
}

func connectMCPNotionMigration(ctx context.Context, app core.App, input leafenMCPNotionMigrationConnectInput) (*mcp.CallToolResult, leafenMCPNotionMigrationConnectOutput, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return nil, leafenMCPNotionMigrationConnectOutput{}, err
	}
	token := strings.TrimSpace(input.Token)
	if token == "" || len(token) > 4096 || strings.ContainsAny(token, " \n\t") {
		return nil, leafenMCPNotionMigrationConnectOutput{}, mcpInvalid("token is the Notion token as Notion shows it, one word (ntn_…)")
	}
	workspace, err := mcpNotionMigrationWorkspace(app, authority, input.WorkspaceID)
	if err != nil {
		return nil, leafenMCPNotionMigrationConnectOutput{}, err
	}
	// The same token handed over again is the same connection.
	digest := sha256.Sum256([]byte(workspace.WorkspaceID + "\x00" + token))
	connection, _, err := saveNotionImportConnection(ctx, app, authority.owner(), workspace.WorkspaceID, notionImportConnectionInput{
		RequestID: "agent-" + hex.EncodeToString(digest[:12]), Token: token, Label: "Notion",
	})
	switch {
	case err == nil:
	case notionProviderTokenRejected(err):
		return nil, leafenMCPNotionMigrationConnectOutput{}, mcpInvalid("Notion didn't take this token: copy the connection's API token again (ntn_…) — yourself in your tab, or ask the person to — " +
			"as leafen_notion_migration_guide section 1 says, and hand it over again")
	case isPermissionDenied(err):
		return nil, leafenMCPNotionMigrationConnectOutput{}, mcpInvalid("the person can't add pages to this workspace; name another with workspace_id")
	case errors.Is(err, errNotionConnectionInputInvalid), errors.Is(err, errNotionConnectionRequestReused),
		errors.Is(err, errNotionConnectionReadFailed), errors.Is(err, errNotionConnectionProtectFailed), errors.Is(err, errNotionConnectionSaveFailed):
		return nil, leafenMCPNotionMigrationConnectOutput{}, fmt.Errorf("%w: %s", errMCPToolFailed, err.Error())
	default:
		return nil, leafenMCPNotionMigrationConnectOutput{}, fmt.Errorf("%w: %s", errMCPToolFailed, notionProviderFailureReason(err, false))
	}
	return nil, leafenMCPNotionMigrationConnectOutput{
		Connection: leafenMCPNotionConnection{ID: connection.ID, Label: connection.Label},
		Next: "Leafen keeps the token encrypted now. Next: leafen_notion_migration_sources with this connection_id, to choose the roots with the person, " +
			"then start and scan. This conversation keeps their token, so once the migration is done, left to do is a line to the person that deleting that connection " +
			"in Notion's developer connections (app.notion.com/developers/connections), or making it a new token there, ends it.",
	}, nil
}
