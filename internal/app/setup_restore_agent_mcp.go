package app

import (
	"context"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/pocketbase/core"
)

// The agent's side of a first-setup restore (setup_restore_agent.go) over
// MCP: an endpoint of its own with three tools — status, find, run — and
// nothing else, which a chat app adds as a custom connector with no sign-in
// and an agent with commands adds as any MCP server. The address holds no
// secret: every call carries the restore code as its argument, so the code
// stays out of addresses and the logs that keep them. Without a good code a
// call is refused; on a server set up already, every call says so. Its
// fields are described in the words Settings › Backups and the restore
// screen name them with (web/src/lib/backupPlaceFields.ts).

const setupRestoreAgentMCPPath = apiRoot + "/setup/restore/mcp"

const setupRestoreAgentMCPInstructions = "This is Leafen's restore endpoint: a new Leafen server with no administrator yet, " +
	"waiting to be filled from the backups of the person's old one. The person gave you a one-time restore code in the " +
	"sentence they pasted; pass it as restore_code to every tool. Start with leafen_restore_status. Then ask the person " +
	"where the old Leafen backed up to and name it with leafen_restore_find. Never ask for passwords, recovery codes or " +
	"storage keys in the conversation: the person types them themselves in fields on Leafen's setup screen. Left to do: which " +
	"fields, to them. Once they choose what to restore, run it with leafen_restore_run and follow it with leafen_restore_status. " +
	"The code works for this one restore only, ends once the restore is done, and ends after about 30 minutes."

type setupRestoreMCPCodeInput struct {
	RestoreCode string `json:"restore_code" jsonschema:"The one-time restore code from the sentence the person pasted"`
}

type setupRestoreMCPFindInput struct {
	RestoreCode    string `json:"restore_code" jsonschema:"The one-time restore code from the sentence the person pasted"`
	Target         string `json:"target" jsonschema:"Where the backups are: folder (a folder the server's computer reads), webdav (a WebDAV server) or s3 (S3-compatible storage)"`
	Folder         string `json:"folder,omitempty" jsonschema:"For folder: the full path on the server's computer of the folder holding 'Leafen backups', or of that folder itself (in a container, a path inside it such as /backups)"`
	WebDAVURL      string `json:"webdav_url,omitempty" jsonschema:"For webdav: the WebDAV address"`
	WebDAVUsername string `json:"webdav_username,omitempty" jsonschema:"For webdav: the user name (the WebDAV password the person types on the screen)"`
	S3Endpoint     string `json:"s3_endpoint,omitempty" jsonschema:"For s3: the endpoint"`
	S3Region       string `json:"s3_region,omitempty" jsonschema:"For s3: the region (us-east-1 when left out)"`
	S3Bucket       string `json:"s3_bucket,omitempty" jsonschema:"For s3: the bucket"`
	S3Prefix       string `json:"s3_prefix,omitempty" jsonschema:"For s3: the prefix inside the bucket (optional)"`
	S3PathStyle    bool   `json:"s3_path_style,omitzero" jsonschema:"For s3: true to use path-style addresses (storage such as MinIO that puts the bucket in the path)"`
}

type setupRestoreMCPRunInput struct {
	RestoreCode string `json:"restore_code" jsonschema:"The one-time restore code from the sentence the person pasted"`
	Choice      string `json:"choice" jsonschema:"What to restore: replica (the continuous replica, the latest changes) or a snapshot's name from leafen_restore_find"`
}

type setupRestoreMCPFindOutput struct {
	Replica   administratorReplica                   `json:"replica"`
	Snapshots []administratorScheduledBackupSnapshot `json:"snapshots"`
	Status    setupRestoreAgentStatus                `json:"status"`
}

func newSetupRestoreAgentMCPServer(app core.App) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "leafen-restore", Version: "1"},
		&mcp.ServerOptions{Instructions: setupRestoreAgentMCPInstructions})
	keeper := setupRestoreAgentOf(app)
	mcp.AddTool(server, &mcp.Tool{
		Name: "leafen_restore_status", Title: "Where the restore is",
		Description: "Where this restore is: the place named, what was found there, which fields the person typed on the setup screen (never their values), and what to do next.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input setupRestoreMCPCodeInput) (*mcp.CallToolResult, setupRestoreAgentStatus, error) {
		status, err := keeper.statusFor(ctx, app, input.RestoreCode)
		return nil, status, err
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "leafen_restore_find", Title: "Find the old Leafen's backups",
		Description: "Look for the old Leafen's backups in a place — a folder the server's computer reads, a WebDAV server or S3-compatible storage — and list its continuous replica and snapshots, newest first. Passwords and keys are not arguments: the person types them on the setup screen, and this uses them.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input setupRestoreMCPFindInput) (*mcp.CallToolResult, setupRestoreMCPFindOutput, error) {
		session, err := keeper.admit(ctx, app, input.RestoreCode, false)
		if err != nil {
			return nil, setupRestoreMCPFindOutput{}, err
		}
		place, status, err := keeper.find(ctx, app, session, setupRestoreAgentPlace{
			Target: input.Target, Folder: input.Folder, WebDAVURL: input.WebDAVURL, WebDAVUsername: input.WebDAVUsername,
			S3Endpoint: input.S3Endpoint, S3Region: input.S3Region, S3Bucket: input.S3Bucket, S3Prefix: input.S3Prefix, S3PathStyle: input.S3PathStyle,
		})
		if err != nil {
			return nil, setupRestoreMCPFindOutput{}, err
		}
		return nil, setupRestoreMCPFindOutput{Replica: place.Replica, Snapshots: place.Snapshots, Status: status}, nil
	})
	destructive := true
	mcp.AddTool(server, &mcp.Tool{
		Name: "leafen_restore_run", Title: "Restore the chosen backup",
		Description: "Restore what the person chose from the place last named with leafen_restore_find: this new server is filled from it and starts again on it. Ask the person first. It runs on in the background; follow it with leafen_restore_status.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &destructive},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input setupRestoreMCPRunInput) (*mcp.CallToolResult, setupRestoreAgentStatus, error) {
		session, err := keeper.admit(ctx, app, input.RestoreCode, false)
		if err != nil {
			return nil, setupRestoreAgentStatus{}, err
		}
		status, err := keeper.run(app, session, input.Choice)
		return nil, status, err
	})
	// Listed as the main endpoints list theirs: open to an answer's new fields
	// (mcp_tool_schemas.go).
	server.AddReceivingMiddleware(mcpPlainToolSchemas())
	return server
}

func registerSetupRestoreAgentMCP(event *core.ServeEvent) {
	server := newSetupRestoreAgentMCPServer(event.App)
	streamable := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server },
		// The DNS rebinding guard is kept below, which knows the restore's outside way too.
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true, DisableLocalhostProtection: true})
	serve := func(e *core.RequestEvent) error {
		if !mcpHostAllowed(e.Request) {
			http.Error(e.Response, "Forbidden: invalid Host header", http.StatusForbidden)
			return nil
		}
		if !sameOriginOrNoOrigin(e.Request) {
			writeMCPHTTPError(e.Response, http.StatusForbidden, errorCodeMCPOriginDenied)
			return nil
		}
		e.Response.Header().Set("Cache-Control", "no-store")
		streamable.ServeHTTP(e.Response, e.Request)
		return nil
	}
	event.Router.POST(setupRestoreAgentMCPPath, serve)
	event.Router.GET(setupRestoreAgentMCPPath, serve)
	event.Router.DELETE(setupRestoreAgentMCPPath, serve)
}
