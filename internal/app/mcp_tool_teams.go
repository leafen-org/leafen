package app

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Teams are teamspaces, as in Notion: notion-get-teams lists the
// teamspaces of the connection's workspaces the person can see, split by
// whether they have joined, and a team's ID chooses its workspace wherever a
// tool takes teamspace_id. The workspaces themselves are listed beside the
// teams, and a workspace ID still chooses that workspace.

const (
	maxNotionMCPTeamsPerStatus = 10
	maxNotionMCPTeamWindows    = 10
)

type notionMCPGetTeamsInput struct {
	Query string `json:"query,omitempty" jsonschema:"Optional case-insensitive team name filter"`
}

type notionMCPTeam struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	MembershipStatus string `json:"membership_status"`
	Role             string `json:"role,omitempty"`
	WorkspaceID      string `json:"workspace_id"`
}

type notionMCPTeamWorkspace struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type notionMCPGetTeamsOutput struct {
	Results    []notionMCPTeam          `json:"results"`
	Joined     []notionMCPTeam          `json:"joined"`
	Available  []notionMCPTeam          `json:"available"`
	HasMore    bool                     `json:"has_more"`
	Workspaces []notionMCPTeamWorkspace `json:"workspaces"`
}

func getMCPTeams(
	ctx context.Context,
	app core.App,
	input notionMCPGetTeamsInput,
) (*mcp.CallToolResult, notionMCPGetTeamsOutput, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return nil, notionMCPGetTeamsOutput{}, err
	}
	query := strings.ToLower(strings.TrimSpace(input.Query))
	if utf8.RuneCountInString(query) > mcpDirectoryQueryMaxRunes {
		return nil, notionMCPGetTeamsOutput{}, mcpInvalid("query is at most %d characters", mcpDirectoryQueryMaxRunes)
	}
	output := notionMCPGetTeamsOutput{
		Results: []notionMCPTeam{}, Joined: []notionMCPTeam{}, Available: []notionMCPTeam{},
		Workspaces: []notionMCPTeamWorkspace{},
	}
	for _, workspace := range authority.ApprovedWorkspaces {
		if _, err := selectMCPWorkspace(app, authority, mcpWorkspaceInput{WorkspaceID: workspace.ID}, permissionWorkspaceRead); err != nil {
			if errors.Is(err, errMCPScopeDenied) || errors.Is(err, errMCPClientNotApproved) {
				continue
			}
			return nil, notionMCPGetTeamsOutput{}, err
		}
		if query == "" || strings.Contains(strings.ToLower(workspace.Name), query) {
			output.Workspaces = append(output.Workspaces, notionMCPTeamWorkspace{ID: workspace.ID, Name: workspace.Name})
		}
		cursor := ""
		for window := 0; window < maxNotionMCPTeamWindows; window++ {
			snapshot, err := readTeamspaceSnapshotState(ctx, app, authority.PrincipalID, workspace.ID, cursor)
			if err != nil {
				return nil, notionMCPGetTeamsOutput{}, failedBecause(errMCPToolFailed, err)
			}
			for index, teamspace := range snapshot.Teamspaces {
				if query != "" && !strings.Contains(strings.ToLower(teamspace.Name), query) {
					continue
				}
				decision := snapshot.Decisions[index]
				team := notionMCPTeam{
					ID: teamspace.ID, Name: teamspace.Name, MembershipStatus: "not_member", WorkspaceID: workspace.ID,
				}
				list := &output.Available
				if decision.Joined {
					team.MembershipStatus, team.Role, list = "member", decision.MembershipRole, &output.Joined
				}
				if len(*list) >= maxNotionMCPTeamsPerStatus {
					output.HasMore = true
					continue
				}
				*list = append(*list, team)
			}
			if !snapshot.HasMore || snapshot.NextCursor == nil {
				break
			}
			if window == maxNotionMCPTeamWindows-1 {
				output.HasMore = true
			}
			cursor = *snapshot.NextCursor
		}
	}
	for _, list := range [][]notionMCPTeam{output.Joined, output.Available} {
		slices.SortStableFunc(list, func(left, right notionMCPTeam) int {
			return strings.Compare(strings.ToLower(left.Name), strings.ToLower(right.Name))
		})
	}
	output.Results = append(append(output.Results, output.Joined...), output.Available...)
	return nil, output, nil
}

// mcpTeamspaceWorkspaceID reads a teamspace_id as the workspace it
// chooses: a connection workspace's own ID, or a teamspace of one of them
// that the person can see. Anything else is returned as given and refused
// by the scope check.
func mcpTeamspaceWorkspaceID(app core.App, authority mcpAuthority, teamspaceID string) (string, error) {
	if teamspaceID == "" || slices.Contains(authority.WorkspaceIDs, teamspaceID) {
		return teamspaceID, nil
	}
	workspaceID, found, err := mcpTeamspaceOwner(app, teamspaceID)
	if err != nil || !found || !slices.Contains(authority.WorkspaceIDs, workspaceID) {
		return teamspaceID, err
	}
	if _, _, _, err := authorizeTeamspace(app, authority.PrincipalID, workspaceID, teamspaceID, false); err != nil {
		if errors.Is(err, errTeamspaceNotFound) {
			return teamspaceID, nil
		}
		return "", err
	}
	return workspaceID, nil
}

// mcpTeamspaceOwner reads the workspace holding a live teamspace.
func mcpTeamspaceOwner(app core.App, teamspaceID string) (string, bool, error) {
	var workspaceID string
	err := app.DB().NewQuery(`
		SELECT [[workspace_id]] FROM {{leafen_teamspaces}}
		WHERE [[id]]={:id} AND [[deleted_at]]=0 AND [[archived_at]]=0
	`).Bind(dbx.Params{"id": teamspaceID}).Row(&workspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return workspaceID, err == nil, err
}
