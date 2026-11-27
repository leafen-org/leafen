package app

import (
	"context"
	"slices"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Every tool answer, on a connection reaching several workspaces, names the
// workspaces the call worked in, by ID and name, in its field
// workspaces_worked_in (user 2026-11-24: an agent that put an upload, a copy
// or a page in another workspace than it meant should see it at once, not a
// day later), inside the answer as its notes go (mcp_answer_notes.go). A call
// works in a workspace where the connection's reach lets it in: its
// target's, or, for a creation with no parent, the one workspace_id names or
// the connection's default. Every way in notes it — the note is written once,
// here, for every tool's answer:
//
//   - selectMCPWorkspace, where a tool picks the workspace it works in;
//   - mcpAuthority.reaches, where a tool checks a page or file it works on
//     against the connection's workspaces itself;
//   - the key's check of where a Leafen operation acts (noteUserKeyWorkedIn),
//     for every operation leafen_call or a tool runs: its place, and what
//     else it reaches, a copy's or a move's destination among them.
//
// A source read to work elsewhere — an upload brought into a page's
// workspace — isn't worked in. A refusal names none, and a connection
// reaching one workspace says nothing.

type mcpWorkspacesUsed struct {
	mu         sync.Mutex
	workspaces []mcpGovernanceWorkspace
	// missed are the connection's workspaces a call held to another by
	// workspace_id was asked into (mcpHeldWorkspaceRefusal).
	missed []string
}

// miss keeps a workspace a held call was asked into; nil keeps none.
func (used *mcpWorkspacesUsed) miss(workspaceID string) {
	if used == nil {
		return
	}
	used.mu.Lock()
	defer used.mu.Unlock()
	used.missed = append(used.missed, workspaceID)
}

// note keeps a workspace a call worked in; nil keeps none.
func (used *mcpWorkspacesUsed) note(workspace mcpGovernanceWorkspace) {
	if used == nil {
		return
	}
	used.mu.Lock()
	defer used.mu.Unlock()
	for _, seen := range used.workspaces {
		if seen.ID == workspace.ID {
			return
		}
	}
	used.workspaces = append(used.workspaces, workspace)
}

// reaches says whether the connection reaches a workspace a call works in,
// and notes it as one the call worked in.
func (authority mcpAuthority) reaches(workspaceID string) bool {
	if workspaceID == "" || !slices.Contains(authority.WorkspaceIDs, workspaceID) {
		return false
	}
	for _, workspace := range authority.ApprovedWorkspaces {
		if workspace.ID == workspaceID {
			authority.used.note(workspace)
		}
	}
	return true
}

// mcpWorkedIn is a workspace a call worked in, as its answer names it.
type mcpWorkedIn struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// named are the workspaces, in the order the call came to them, and them in
// words, for an answer that isn't JSON; none for a call that worked in none.
func (used *mcpWorkspacesUsed) named() ([]mcpWorkedIn, string) {
	used.mu.Lock()
	defer used.mu.Unlock()
	listed := make([]mcpWorkedIn, 0, len(used.workspaces))
	said := make([]string, 0, len(used.workspaces))
	for _, workspace := range used.workspaces {
		listed = append(listed, mcpWorkedIn{ID: workspace.ID, Name: workspace.Name})
		said = append(said, "“"+workspace.Name+"” ("+workspace.ID+")")
	}
	if len(listed) == 0 {
		return nil, ""
	}
	return listed, "Worked in: " + strings.Join(said, ", ")
}

func mcpAnswerWorkspaces() mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, request mcp.Request) (mcp.Result, error) {
			authority, err := mcpAuthorityFromContext(ctx)
			if method != "tools/call" || err != nil || len(authority.WorkspaceIDs) <= 1 {
				return next(ctx, method, request)
			}
			authority.used = &mcpWorkspacesUsed{}
			result, err := next(context.WithValue(ctx, mcpAuthorityContextKey{}, authority), method, request)
			called, ok := result.(*mcp.CallToolResult)
			if err != nil || !ok || called == nil || called.IsError {
				return result, err
			}
			if listed, said := authority.used.named(); len(listed) > 0 {
				mcpAnswerNote(called, mcpAnswerWorkedInField, listed, said)
			}
			return result, err
		}
	}
}

// Every tool takes workspace_id, one of the connection's workspaces, though
// only the creations with no parent list it (mcp_tool_arguments.go): given
// to another, it changes nothing and refuses nothing but what it names in
// another workspace, whose refusal names both, by name and ID, whatever the
// tool's own refusal said (command session 2026-11-24: an agent told "it is
// outside the workspaces this connection was given" of a page its connection
// reaches couldn't tell it had named another workspace).

// mcpHeldWorkspaceLine says it at the end of every other tool's description,
// on a connection reaching several workspaces (mcpListedArguments).
const mcpHeldWorkspaceLine = " Any call may also give workspace_id, one of this connection's workspaces: " +
	"it only checks, and what the call names in another is refused, naming both."

// mcpHeldWorkspaceRefusal is the answer of a call workspace_id held to one
// workspace: a refusal whose target was in another of the connection's names
// both; any other answer is as it was.
func mcpHeldWorkspaceRefusal(ctx context.Context, result mcp.Result, held string) mcp.Result {
	called, ok := result.(*mcp.CallToolResult)
	authority, err := mcpAuthorityFromContext(ctx)
	if !ok || called == nil || !called.IsError || err != nil || authority.used == nil || authority.whole == nil {
		return result
	}
	authority.used.mu.Lock()
	missed := append([]string(nil), authority.used.missed...)
	authority.used.mu.Unlock()
	for _, workspaceID := range missed {
		if workspaceID == held {
			continue
		}
		called.Content = []mcp.Content{&mcp.TextContent{Text: mcpHeldWorkspaceText(*authority.whole, held, workspaceID)}}
		return called
	}
	return result
}

// mcpHeldWorkspaceText words that refusal.
func mcpHeldWorkspaceText(whole mcpAuthority, held, target string) string {
	named := func(workspaceID string) string {
		for _, workspace := range whole.ApprovedWorkspaces {
			if workspace.ID == workspaceID {
				return "“" + workspace.Name + "” (" + workspaceID + ")"
			}
		}
		return workspaceID
	}
	return errorCodeMCPToolInvalid + ": workspace_id names " + named(held) + ", and what this call names is in " + named(target) +
		": give that workspace's ID, or leave workspace_id out — what a call names decides its workspace."
}
