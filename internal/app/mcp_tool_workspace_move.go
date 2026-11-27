package app

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/pocketbase/core"
)

// leafen_workspace_move lets an agent of a workspace's owner follow moving
// one workspace between Leafen servers (workspace_move_out.go,
// workspace_move_in.go) from either side: the workspace's people and what it
// holds, a move it sends or one it takes in, with the counts to compare. It
// never carries the move code, which the person puts in the screens
// themselves, nor pairs people or starts a move: Leafen copies, the agent
// guides.

type leafenMCPWorkspaceMoveInput struct {
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"The workspace; may be left out when the connection reaches one"`
	List        string `json:"list,omitempty" jsonschema:"people to list the workspace's people, a window at a time; left out, the answer counts them by role"`
	StartCursor string `json:"start_cursor,omitempty" jsonschema:"With list, the next_cursor of the last answer, to go on from there"`
}

// leafenMCPWorkspaceMoveOutput counts the workspace's people by role, and
// lists them only with list people (mcp_answer_lists.go).
type leafenMCPWorkspaceMoveOutput struct {
	WorkspaceID  string                 `json:"workspace_id"`
	Release      string                 `json:"release"`
	PeopleTotal  int                    `json:"people_total"`
	PeopleByRole []mcpAnswerGroup       `json:"people_by_role"`
	People       []workspaceMovePerson  `json:"people,omitempty"`
	NextCursor   string                 `json:"next_cursor,omitempty"`
	Holds        workspaceMoveCounts    `json:"holds"`
	Sending      workspaceMoveOutStatus `json:"sending"`
	Receiving    workspaceMoveInStatus  `json:"receiving"`
	Instructions string                 `json:"instructions"`
}

// leafenMCPWorkspaceMoveInstructions guide a workspace's move, the screens
// named in the person's words (mcp_screen_names.go).
func leafenMCPWorkspaceMoveInstructions(language string) string {
	return "A workspace moves to a Leafen server that people already use, of this version or newer, into an empty workspace there. " +
		"The person makes that empty workspace on the new server, opens its " + mcpScreen(language, "settings", "workspace", "general", "workspaceMove", "workspaceMoveIn") +
		", and pastes the code into this workspace's " + mcpScreen(language, "workspaceMoveOut") + " themselves: never ask for the code in the chat. " +
		"The screen offers each person here the account there with the same email; someone not matched is added by name and email, to be matched under " +
		mcpScreen(language, "settings", "workspace", "import") + " once they have an account there. Left to do: matching the rest with the person, and that to them. " +
		"From the start of the move the workspace accepts no changes here. Its pages, files, comments, mentions, who made what and its pages' edit history go, " +
		"with the IDs they have here; pages only someone else can read stay here. A server the workspace was moved or imported to before refuses it, " +
		"and a code made on this same server is refused when it is checked, since the workspace is already here. sending.report compares the counts on both " +
		"sides once it has moved. On the new server, receiving.state 'importing' with jobStatus 'completed' means the workspace has moved in, with its counts; " +
		"'failed' means it did not. The workspace stays here, read-only; deleting it is the person's own decision."
}

func registerMCPWorkspaceMoveTool(server *mcp.Server, app core.App) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "leafen_workspace_move", Title: "Follow a workspace move",
		// It only reads, whatever its name says (clients may ask before tools named for moving).
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
		Description: "For a workspace's owner: its people, what it holds, and a move to or from another Leafen server, " +
			"with what each side counted, to guide the person through moving the workspace.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input leafenMCPWorkspaceMoveInput) (*mcp.CallToolResult, leafenMCPWorkspaceMoveOutput, error) {
		authority, err := mcpAuthorityFromContext(ctx)
		if err != nil {
			return nil, leafenMCPWorkspaceMoveOutput{}, err
		}
		workspace, err := selectMCPWorkspace(app, authority, mcpWorkspaceInput{WorkspaceID: input.WorkspaceID, AllowImplicit: true}, permissionWorkspaceOwner)
		if err != nil {
			return nil, leafenMCPWorkspaceMoveOutput{}, err
		}
		listing, err := mcpAnswerListingOf(input.List, input.StartCursor, "people")
		if err != nil {
			return nil, leafenMCPWorkspaceMoveOutput{}, err
		}
		people, err := workspaceMovePeopleOf(app, workspace)
		if err != nil {
			return nil, leafenMCPWorkspaceMoveOutput{}, err
		}
		window := mcpAnswerWindowOf(listing, "people", people)
		pages, files, comments := countWorkspaceMoveContents(app, workspace.WorkspaceID)
		output := leafenMCPWorkspaceMoveOutput{
			WorkspaceID: workspace.WorkspaceID, Release: ReleaseVersion(), PeopleTotal: len(people),
			PeopleByRole: mcpAnswerGroupsOf(people, func(person workspaceMovePerson) string { return person.Role }, nil),
			People:       window.Items, NextCursor: window.NextCursor,
			Holds:   workspaceMoveCounts{Pages: pages, Files: files, Comments: comments},
			Sending: readWorkspaceMoveOut(app, workspace.WorkspaceID), Instructions: leafenMCPWorkspaceMoveInstructions(mcpScreenLanguage(app, authority.owner())),
		}
		jobs, ok := app.Store().Get(durableJobRunnerStoreKey).(*durableJobRunner)
		if !ok || jobs == nil {
			return nil, leafenMCPWorkspaceMoveOutput{}, errMCPToolFailed
		}
		in := workspaceMoveInOf(app, jobs)
		in.mu.Lock()
		slot := in.current(workspace.WorkspaceID)
		in.mu.Unlock()
		output.Receiving = in.project(app, workspace.WorkspaceID, slot)
		output.Receiving.Code = ""
		if listing.List != "" {
			output.Instructions = window.said() + " " + output.Instructions
		}
		return nil, output, nil
	})
}
