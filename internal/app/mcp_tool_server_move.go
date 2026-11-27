package app

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/pocketbase/core"
)

// leafen_server_move lets an agent of the server's administrator follow a
// whole-server move (server_move.go) from either side: what the server
// holds and its version, a move it sends or one it took in, with the counts
// to compare. It never carries the move code, which the person puts in the
// screens themselves, nor starts a move: Leafen copies, the agent guides.

type leafenMCPServerMoveInput struct{}

type leafenMCPServerMoveOutput struct {
	Release       string                        `json:"release"`
	SchemaVersion int                           `json:"schemaVersion"`
	Holds         administratorServerMoveCounts `json:"holds"`
	Sending       administratorServerMoveOut    `json:"sending"`
	Receiving     administratorServerMoveIn     `json:"receiving"`
	Instructions  string                        `json:"instructions"`
}

// leafenMCPServerMoveInstructions guide a whole server's move, the screens
// named in the person's words (mcp_screen_names.go).
func leafenMCPServerMoveInstructions(language string) string {
	return "A whole server moves only to a Leafen server no one else uses yet, of this version or newer; a server with other people takes a workspace instead. " +
		"The person makes a code on the new server (" + mcpScreen(language, "settings", "server", "serverRestore", "serverMoveIn") + ") and pastes it into " +
		"the old server's " + mcpScreen(language, "serverMove") + " themselves: never ask for the code in the chat. From then the old server takes no changes " +
		"and gives its address to the new server, which starts again with everything. On the new server, receiving.state \"moved\" with expected and actual " +
		"counts tells whether everything arrived. Afterwards the person switches their apps and phones to the new server (the old server's band opens it), " +
		"gives outside agents the new connection text (" + mcpScreen(language, "settings", "features", "agents") + ") and checks the new server's backup place. " +
		"The old server stays, reading only; deleting it is the person's own decision."
}

func registerMCPServerMoveTool(server *mcp.Server, app core.App) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "leafen_server_move", Title: "Follow a server move",
		// It only reads, whatever its name says (clients may ask before tools named for moving).
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
		Description: "For the server's administrator, on an agent connection as all the server's administration is (not an MCP token): what " +
			"this server holds and its version, and a whole-server move it sends or took in, " +
			"with what each side counted, to guide the person through moving to another Leafen server.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ leafenMCPServerMoveInput) (*mcp.CallToolResult, leafenMCPServerMoveOutput, error) {
		authority, err := mcpAuthorityFromContext(ctx)
		if err != nil {
			return nil, leafenMCPServerMoveOutput{}, err
		}
		administrator, err := isAdministrator(app, authority.owner())
		if err != nil || !administrator || authority.Scope < userKeyScopeAdmin {
			return nil, leafenMCPServerMoveOutput{}, errors.New("a server move is for the server's administrator, on a connection whose tools and organizations leave it everything")
		}
		output := leafenMCPServerMoveOutput{Release: ReleaseVersion(), Holds: countServerContents(app), Instructions: leafenMCPServerMoveInstructions(mcpScreenLanguage(app, authority.owner()))}
		_ = app.DB().NewQuery(`SELECT COALESCE(MAX([[version]]), 0) FROM {{leafen_schema_migrations}}`).Row(&output.SchemaVersion)
		out := serverMoveOutOf(app)
		out.mu.Lock()
		output.Sending = out.status
		out.mu.Unlock()
		if fence := readReplicaFence(app.DataDir()); fence != nil && output.Sending.State == serverMoveOutNone {
			output.Sending.State, output.Sending.To = serverMoveOutMoved, fence.MovedTo
		}
		output.Receiving = serverMoveInOf(app).project()
		output.Receiving.Code = ""
		return nil, output, nil
	})
}
