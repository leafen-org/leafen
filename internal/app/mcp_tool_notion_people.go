package app

import (
	"context"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/pocketbase/core"
)

// Notion's get-self and get-groups (seen 2026-11-13): who the connection
// works as and in which workspace — every workspace it was given, where
// Notion's has one — and the workspace's groups by name, as its settings'
// 그룹 tab lists them.

const mcpGroupsMaximumResults = 25

type notionMCPGetSelfInput struct{}

type notionMCPSelfWorkspace struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type notionMCPSelfUser struct {
	Type  string `json:"type"`
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
}

type notionMCPGetSelfOutput struct {
	Workspace  *notionMCPSelfWorkspace  `json:"workspace,omitempty" jsonschema:"The workspace this connection works in, when it was given one"`
	Workspaces []notionMCPSelfWorkspace `json:"workspaces,omitempty" jsonschema:"The workspaces this connection was given, when it was given more than one"`
	User       notionMCPSelfUser        `json:"user"`
}

type notionMCPGetGroupsInput struct {
	Query string `json:"query,omitempty" jsonschema:"A group's name, or part of it, to search for (case doesn't matter), or a group's ID to look it up"`
}

type notionMCPGroup struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	WorkspaceID string `json:"workspace_id"`
}

type notionMCPGetGroupsOutput struct {
	Results []notionMCPGroup `json:"results"`
	HasMore bool             `json:"hasMore"`
}

func registerMCPPeopleTools(server *mcp.Server, app core.App, readOnly *mcp.ToolAnnotations) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "notion-get-self", Title: "Get this Leafen connection",
		Description: "Who this connection works as — the person's ID as notion-get-users gives it, their name and email — and the workspace it works in, or the workspaces when it was given more than one. Call with {}.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ notionMCPGetSelfInput) (*mcp.CallToolResult, notionMCPGetSelfOutput, error) {
		return getMCPSelf(ctx, app)
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "notion-get-groups", Title: "Get Leafen groups",
		Description: "List the groups of the workspaces this connection was given, by name, at most 25 — the closest to query first when one is given, or the one a group ID names. Groups give people access to teamspaces and pages; a people property takes people, not groups.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input notionMCPGetGroupsInput) (*mcp.CallToolResult, notionMCPGetGroupsOutput, error) {
		return getMCPGroups(ctx, app, input)
	})
}

func getMCPSelf(ctx context.Context, app core.App) (*mcp.CallToolResult, notionMCPGetSelfOutput, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return nil, notionMCPGetSelfOutput{}, err
	}
	output := notionMCPGetSelfOutput{}
	for _, workspace := range authority.ApprovedWorkspaces {
		output.Workspaces = append(output.Workspaces, notionMCPSelfWorkspace{ID: workspace.ID, Name: workspace.Name})
	}
	if len(output.Workspaces) == 0 {
		return nil, notionMCPGetSelfOutput{}, errMCPAuthorityMissing
	}
	user, found, err := loadNotionUserDirectoryUser(app, authority.PrincipalID, output.Workspaces[0].ID, authority.PrincipalID)
	if err != nil || !found {
		return nil, notionMCPGetSelfOutput{}, failedBecause(errMCPToolFailed, err)
	}
	publicID, err := notionCompatibleUserID(user.ID)
	if err != nil {
		return nil, notionMCPGetSelfOutput{}, failedBecause(errMCPToolFailed, err)
	}
	output.User = notionMCPSelfUser{Type: "person", ID: publicID, Name: user.Name, Email: user.Email}
	if len(output.Workspaces) == 1 {
		output.Workspace, output.Workspaces = &output.Workspaces[0], nil
	}
	return nil, output, nil
}

func getMCPGroups(ctx context.Context, app core.App, input notionMCPGetGroupsInput) (*mcp.CallToolResult, notionMCPGetGroupsOutput, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return nil, notionMCPGetGroupsOutput{}, err
	}
	query := strings.ToLower(strings.TrimSpace(input.Query))
	if len([]rune(query)) > 100 {
		return nil, notionMCPGetGroupsOutput{}, mcpInvalid("query is at most 100 characters")
	}
	// A people value's group reference names it as Notion's does.
	groupID := strings.TrimPrefix(query, "space_permission_group-")
	type ranked struct {
		group notionMCPGroup
		rank  int
	}
	found := []ranked{}
	for _, workspaceID := range authority.WorkspaceIDs {
		access, err := selectMCPWorkspace(app, authority, mcpWorkspaceInput{WorkspaceID: workspaceID}, permissionWorkspaceRead)
		if err != nil {
			continue
		}
		groups, err := loadWorkspacePermissionGroups(app, access)
		if err != nil {
			return nil, notionMCPGetGroupsOutput{}, failedBecause(errMCPToolFailed, err)
		}
		for _, item := range groups {
			name := strings.ToLower(item.Name)
			rank := 3
			switch {
			case query == "":
				rank = 0
			case strings.ToLower(item.ID) == groupID:
				rank = 0
			case name == query:
				rank = 1
			case strings.HasPrefix(name, query):
				rank = 2
			case !strings.Contains(name, query):
				continue
			}
			found = append(found, ranked{group: notionMCPGroup{ID: item.ID, Name: item.Name, WorkspaceID: workspaceID}, rank: rank})
		}
	}
	slices.SortStableFunc(found, func(a, b ranked) int {
		if a.rank != b.rank {
			return a.rank - b.rank
		}
		return strings.Compare(strings.ToLower(a.group.Name), strings.ToLower(b.group.Name))
	})
	output := notionMCPGetGroupsOutput{Results: []notionMCPGroup{}, HasMore: len(found) > mcpGroupsMaximumResults}
	for index, item := range found {
		if index == mcpGroupsMaximumResults {
			break
		}
		output.Results = append(output.Results, item.group)
	}
	return nil, output, nil
}
