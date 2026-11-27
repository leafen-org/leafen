package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// One argument, one name (2026-11-12: an agent importing from Notion made
// calls that missed, one tool requiring workspace_id and another refusing it,
// one taking the search words as query and another as filter). Leafen's own
// tools name what they share as Notion's tools do:
//
//   - workspace_id, the workspace: never required — left out, the connection's
//     one workspace, or the one the IDs given are in; workspace_ids, several,
//     only on a tool that works in several at once (leafen_search, 2026-11-23),
//     and read as workspace_id on the others;
//   - query, the words to find;
//   - start_cursor, the next_cursor an answer gave, to go on from there;
//   - page_size, how many an answer gives at most.
//
// Notion's tools keep Notion's own schemas. A tool that takes no workspace_id
// still takes one of the connection's workspaces given as it, and works in
// that workspace alone, as if the connection reached it alone; one the
// connection wasn't given is refused with why (2026-11-13: on a connection
// reaching several, a tool that needed a workspace said to give workspace_id,
// and refused the one given). Only a creation with no parent chooses its
// workspace by it — a page or database with none, an upload — and, on a
// connection reaching several, tools/list lists it on those tools
// (mcpWorkspaceChoosingTools), so a client that sends only what a schema has
// can choose (user 2026-11-24: notion-create-pages' description named
// workspace_id and its schema didn't have it, and such a client's pages went
// to the default workspace; anything with a target takes its target's, and
// no call needs the workspace). A call to Leafen's tools under another name for
// one of these (filter, cursor, limit…) is read by its name. The server checks
// its tools against this as it makes them, and doesn't start with one that
// breaks it.

// Notion's tools all take two fields for Notion's own tracking (seen
// 2026-11-13), which an agent following Notion's setup may send with any
// call: instructions_version, its setup instructions' version, and skill, the
// skill it follows. Leafen's tools take them too and keep neither: Notion's
// list them, described in Leafen's words, and a call to any tool may carry
// them.
var mcpTrackingArguments = map[string]string{
	"instructions_version": "Which version of Notion's setup instructions the agent goes by, as 0.0.2, or left out when not known; Leafen doesn't store it.",
	"skill":                "The name of the agent skill this task goes by, or left out without one; it is for tracking alone, and Leafen doesn't store it.",
}

// mcpArgumentRenames are the names Leafen's tools don't use for an argument,
// with the one they use.
var mcpArgumentRenames = map[string]string{
	"workspace": "workspace_id", "workspaceId": "workspace_id", "workspace_ids": "workspace_id",
	"q": "query", "search": "query", "words": "query", "filter": "query",
	"cursor": "start_cursor", "after": "start_cursor", "startCursor": "start_cursor", "next_cursor": "start_cursor",
	"limit": "page_size", "max_results": "page_size", "count": "page_size", "pageSize": "page_size",
}

// mcpSeveralWorkspacesTools are the tools that take workspace_ids, several
// workspaces, as a name of its own.
var mcpSeveralWorkspacesTools = map[string]bool{"leafen_search workspace_ids": true}

// checkMCPToolArguments says where Leafen's tools break the one-name rule.
func checkMCPToolArguments(tools []*mcp.Tool) error {
	problems := []string{}
	for _, tool := range tools {
		if !strings.HasPrefix(tool.Name, "leafen_") {
			continue
		}
		properties, required := mcpToolSchemaArguments(tool)
		for name := range properties {
			if want, renamed := mcpArgumentRenames[name]; renamed && !mcpSeveralWorkspacesTools[tool.Name+" "+name] {
				problems = append(problems, fmt.Sprintf("%s takes %s, which Leafen's tools call %s", tool.Name, name, want))
			}
		}
		if slices.Contains(required, "workspace_id") {
			problems = append(problems, tool.Name+" requires workspace_id, which Leafen's tools let be left out")
		}
	}
	if len(problems) == 0 {
		return nil
	}
	slices.Sort(problems)
	return fmt.Errorf("MCP tool arguments break the one-name rule (mcp_tool_arguments.go): %s", strings.Join(problems, "; "))
}

// mcpToolSchemaArguments reads a tool's input schema's argument names and the
// ones it requires.
func mcpToolSchemaArguments(tool *mcp.Tool) (map[string]json.RawMessage, []string) {
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if data, err := json.Marshal(tool.InputSchema); err == nil {
		_ = json.Unmarshal(data, &schema)
	}
	return schema.Properties, schema.Required
}

// mcpCheckedToolArguments holds each endpoint's tools with the arguments
// they take, read once: its tools are the same for every server made.
var mcpCheckedToolArguments [2]struct {
	once  sync.Once
	takes map[string]map[string]bool
}

// checkedMCPToolArguments checks a server's own tools against the rule —
// their answers, the instructions' room, the descriptions' length and the
// operations' map's (native_operation_map_room.go) too —,
// panicking on one that breaks it, and names the arguments each takes; an
// endpoint's first server is read, the rest take its answer.
func checkedMCPToolArguments(server *mcp.Server, surface mcpToolSurface) map[string]map[string]bool {
	checked := &mcpCheckedToolArguments[surface]
	checked.once.Do(func() {
		tools := listRegisteredMCPTools(server)
		if err := errors.Join(checkMCPToolArguments(tools), checkMCPAnswerRule(tools), checkMCPInstructionsRoom(),
			checkMCPToolDescriptions(tools), checkNativeOperationsMapRoom()); err != nil {
			panic(err)
		}
		checked.takes = make(map[string]map[string]bool, len(tools))
		for _, tool := range tools {
			properties, _ := mcpToolSchemaArguments(tool)
			names := make(map[string]bool, len(properties))
			for name := range properties {
				names[name] = true
			}
			checked.takes[tool.Name] = names
		}
	})
	return checked.takes
}

// mcpSharedArguments reads a call to one of the server's own tools by the
// rule, before the call itself is read: on Leafen's tools an argument under
// another name the rule gives (filter, cursor, limit…) is read as its name
// there; a tool that takes no workspace_id, given one of the connection's
// workspaces as it, works in that one alone, and another is refused with
// why; Notion's tracking fields are taken and dropped, and listed on Notion's
// tools. Tools it doesn't know, apps' among them, are left as they are.
func mcpSharedArguments(takes map[string]map[string]bool) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, request mcp.Request) (mcp.Result, error) {
			if method == "tools/list" {
				result, err := next(ctx, method, request)
				return mcpListedArguments(ctx, result, takes), err
			}
			call, ok := request.(*mcp.CallToolRequest)
			if method != "tools/call" || !ok || call.Params == nil {
				return next(ctx, method, request)
			}
			names, known := takes[call.Params.Name]
			arguments := map[string]json.RawMessage{}
			if !known || json.Unmarshal(call.Params.Arguments, &arguments) != nil {
				return next(ctx, method, request)
			}
			changed := false
			for name := range mcpTrackingArguments {
				if _, given := arguments[name]; given && !names[name] {
					delete(arguments, name)
					changed = true
				}
			}
			if strings.HasPrefix(call.Params.Name, "leafen_") {
				for given, raw := range arguments {
					want, renamed := mcpArgumentRenames[given]
					if _, taken := arguments[want]; renamed && !names[given] && names[want] && !taken {
						arguments[want] = raw
						delete(arguments, given)
						changed = true
					}
				}
			}
			held := ""
			if raw, given := arguments["workspace_id"]; given && !names["workspace_id"] {
				workspaceID := ""
				_ = json.Unmarshal(raw, &workspaceID)
				authority, err := mcpAuthorityFromContext(ctx)
				if err != nil {
					return next(ctx, method, request)
				}
				if workspaceID = strings.TrimSpace(workspaceID); workspaceID != "" {
					if !slices.Contains(authority.WorkspaceIDs, workspaceID) {
						return mcpToolText(errorCodeMCPScopeDenied+": workspace "+workspaceID+" isn't one this connection was given: "+
							mcpWorkspacesListedOn(authority)+".", true), nil
					}
					ctx, held = context.WithValue(ctx, mcpAuthorityContextKey{}, authority.within(workspaceID)), workspaceID
				}
				delete(arguments, "workspace_id")
				changed = true
			}
			if !changed {
				return next(ctx, method, request)
			}
			data, err := json.Marshal(arguments)
			if err != nil {
				return next(ctx, method, request)
			}
			call.Params.Arguments = data
			result, err := next(ctx, method, call)
			if held != "" {
				result = mcpHeldWorkspaceRefusal(ctx, result, held)
			}
			return result, err
		}
	}
}

// mcpWorkspacesListed is where an agent finds the workspaces a connection
// reaches, said for either endpoint where the call's isn't known
// (mcpWorkspacesListedOn says it for the call's).
const mcpWorkspacesListed = "leafen_list_workspaces lists them (on the Notion-compatible endpoint, notion-get-teams under workspaces)"

// mcpWorkspaceNeeded is a call that needs a workspace and names nothing in
// one, on a connection reaching several (connection_person_rights.go): it
// goes to none of them by itself, and says, in one line, which it reaches
// and how to choose — workspace_id, which every tool takes by the rule
// above, or what else names one (orElse).
func mcpWorkspaceNeeded(authority mcpAuthority, orElse string) error {
	listed := make([]string, 0, min(len(authority.ApprovedWorkspaces), mcpWorkspacesNamed))
	for index, workspace := range authority.ApprovedWorkspaces {
		if index == mcpWorkspacesNamed {
			listed = append(listed, fmt.Sprintf("%d more (%s)", len(authority.ApprovedWorkspaces)-index, mcpWorkspacesListedOn(authority)))
			break
		}
		listed = append(listed, workspace.ID+" “"+appOneLine(workspace.Name, 60)+"”")
	}
	return fmt.Errorf("%w: nothing in this call says which of this connection's workspaces it is for, and it goes to none by itself: "+
		"give workspace_id%s — %s. Choose by the page you are working on, a search, or what you already know (your folder's rules, "+
		"your memory); only if you still can't tell, ask your person", errMCPWorkspaceRequired, orElse, strings.Join(listed, ", "))
}

// mcpWorkspacesNamed is how many workspaces mcpWorkspaceNeeded names.
const mcpWorkspacesNamed = 20

// within is the connection's authority as if it reached one of its
// workspaces alone.
func (authority mcpAuthority) within(workspaceID string) mcpAuthority {
	in := func(workspace mcpGovernanceWorkspace) bool { return workspace.ID != workspaceID }
	narrowed := authority
	if narrowed.whole == nil {
		whole := authority
		narrowed.whole = &whole
	}
	narrowed.WorkspaceIDs = []string{workspaceID}
	narrowed.AllWorkspaces = false
	if scoped := slices.DeleteFunc(slices.Clone(authority.ScopedWorkspaces), in); len(scoped) > 0 {
		narrowed.ScopedWorkspaces = scoped
	}
	narrowed.ApprovedWorkspaces = slices.DeleteFunc(slices.Clone(authority.ApprovedWorkspaces), in)
	return narrowed
}

// mcpWorkspaceChoosingTools are the creations with no target, which list
// workspace_id on a connection reaching several workspaces.
var mcpWorkspaceChoosingTools = map[string]bool{
	"notion-create-pages": true, "notion-create-database": true, "notion-create-file-upload": true, "notion-create-attachment": true,
}

// mcpListedWorkspaceArgument is workspace_id as those tools list it.
const mcpListedWorkspaceArgument = "The workspace, of those this connection reaches (leafen_list_workspaces or notion-get-teams' workspaces list them), " +
	"that a page, database or upload made without a parent goes to; left out, the call is refused with the list. One with a parent goes to its parent's workspace."

// mcpListedArguments lists Notion's tools with the tracking fields their
// Notion schemas have, and, for a connection reaching several workspaces,
// the creations with no target with workspace_id and every other tool that
// takes none with the line saying it may go with any call
// (mcpHeldWorkspaceLine), on copies of the tools.
func mcpListedArguments(ctx context.Context, result mcp.Result, takes map[string]map[string]bool) mcp.Result {
	listed, ok := result.(*mcp.ListToolsResult)
	if !ok || listed == nil {
		return result
	}
	authority, err := mcpAuthorityFromContext(ctx)
	several := err == nil && len(authority.WorkspaceIDs) > 1
	tracked := *listed
	tracked.Tools = make([]*mcp.Tool, len(listed.Tools))
	for index, tool := range listed.Tools {
		tracked.Tools[index] = tool
		names, known := takes[tool.Name]
		workspace := several && known && !names["workspace_id"] && mcpWorkspaceChoosingTools[tool.Name]
		held := several && known && !names["workspace_id"] && !workspace
		if !strings.HasPrefix(tool.Name, "notion-") && !workspace && !held {
			continue
		}
		each := *tool
		if held {
			each.Description += mcpHeldWorkspaceLine
		}
		tracked.Tools[index] = &each
		if !strings.HasPrefix(tool.Name, "notion-") && !workspace {
			continue
		}
		schema := map[string]any{}
		if data, err := json.Marshal(tool.InputSchema); err != nil || json.Unmarshal(data, &schema) != nil {
			continue
		}
		properties, _ := schema["properties"].(map[string]any)
		if properties == nil {
			properties = map[string]any{}
		}
		for name, description := range mcpTrackingArguments {
			if _, has := properties[name]; !has && strings.HasPrefix(tool.Name, "notion-") {
				properties[name] = map[string]any{"type": "string", "description": description}
			}
		}
		if workspace {
			properties["workspace_id"] = map[string]any{"type": "string", "description": mcpListedWorkspaceArgument}
		}
		schema["properties"] = properties
		each.InputSchema = schema
	}
	return &tracked
}
