package app

import (
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Where an agent's search looks, on a connection reaching several
// workspaces, is the agent's to choose each time (2026-11-23, the user: "어디를
// 검색할지는 그때그때 에이전트가 정하게"; "진행해. 이건 노션하고 달라지는거긴하잖아" —
// Notion's connection reaches one workspace): leafen_search takes one
// workspace (workspace_id), several (workspace_ids) or, naming none, every
// one the connection reaches — so an agent that doesn't know where a page
// is doesn't answer that it's nowhere because it looked in the default one
// alone. Leafen keeps no choice between calls: the tool's description tells
// the agent, as it takes the tool up, to name the workspaces it knows from
// the conversation or its memory, to search them all only when it doesn't,
// and to narrow to where results came from after (the instructions say only
// to search Leafen: what applies as one tool is called is that tool's,
// mcp_instructions.go).
//
// Every workspace isn't searched at once: a call searches
// mcpSearchWorkspacesPerCall of them, those its person used last first
// (their page visits), and next_cursor goes on to the others once those run
// out, as a search reads its matches a few at a time
// (workspace_search_tiers.go) — tens of workspaces each searched for one
// common word would be one call's whole load. The answer says how many of
// the workspaces asked have been searched, of how many the connection
// reaches, and its results by workspace. notion-search, which keeps
// Notion's answer, searches the same way and says in a notice what is left
// and that leafen_search chooses.

// mcpSearchWorkspacesPerCall is how many workspaces one call searches.
const mcpSearchWorkspacesPerCall = 5

// leafenMCPSearchWorkspaces are the workspaces a leafen_search asks for:
// those named, else every one the connection reaches that it may search
// (mcpSearchEveryWorkspace). A teamspace or a workspace named alone is
// searched as before.
func leafenMCPSearchWorkspaces(app core.App, authority mcpAuthority, input mcpSearchInput) ([]workspaceAuthorization, error) {
	named := []string{}
	for _, id := range input.WorkspaceIDs {
		if id = strings.TrimSpace(id); id != "" && !slices.Contains(named, id) {
			named = append(named, id)
		}
	}
	if len(named) > 0 && (strings.TrimSpace(input.WorkspaceID) != "" || strings.TrimSpace(input.TeamspaceID) != "") {
		return nil, mcpInvalid("give workspace_id for one workspace or workspace_ids for several, not both")
	}
	if len(named) == 0 && (strings.TrimSpace(input.WorkspaceID) != "" || strings.TrimSpace(input.TeamspaceID) != "" || len(authority.WorkspaceIDs) <= 1) {
		authorization, err := selectMCPWorkspace(app, authority, mcpWorkspaceInput{
			WorkspaceID: input.WorkspaceID, TeamspaceID: input.TeamspaceID, AllowImplicit: input.AllowImplicitScope,
		}, permissionWorkspaceRead)
		if err != nil {
			return nil, err
		}
		return []workspaceAuthorization{authorization}, nil
	}
	if len(named) == 0 {
		return mcpSearchEveryWorkspace(app, authority)
	}
	asked := make([]workspaceAuthorization, 0, len(named))
	for _, id := range named {
		authorization, err := selectMCPWorkspace(app, authority, mcpWorkspaceInput{WorkspaceID: id}, permissionWorkspaceRead)
		if err != nil {
			return nil, err
		}
		asked = append(asked, authorization)
	}
	return asked, nil
}

// mcpSearchEveryWorkspace is every workspace the connection reaches that it
// may search, those its person used last first: one its person no longer
// reads, or whose administrators haven't approved this kind of connection,
// is left out.
func mcpSearchEveryWorkspace(app core.App, authority mcpAuthority) ([]workspaceAuthorization, error) {
	every := []workspaceAuthorization{}
	for _, id := range authority.WorkspaceIDs {
		authorization, err := selectMCPWorkspace(app, authority, mcpWorkspaceInput{WorkspaceID: id}, permissionWorkspaceRead)
		if errors.Is(err, errMCPScopeDenied) || errors.Is(err, errMCPClientNotApproved) {
			continue
		}
		if err != nil {
			return nil, err
		}
		every = append(every, authorization)
	}
	if len(every) == 0 {
		return nil, mcpDenied("none of the workspaces this connection was given can be searched by it: %s", mcpWorkspacesListedOn(authority))
	}
	return mcpWorkspacesLastUsedFirst(app, authority.PrincipalID, every)
}

// mcpWorkspacesLastUsedFirst orders workspaces as their person last opened a
// page in each, the latest first; one never opened after, as given.
func mcpWorkspacesLastUsedFirst(app core.App, principalID string, workspaces []workspaceAuthorization) ([]workspaceAuthorization, error) {
	rows := []struct {
		WorkspaceID string `db:"workspace_id"`
		Last        int64  `db:"last"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[workspace_id]], MAX([[visited_at]]) AS [[last]] FROM {{leafen_page_visits}} INDEXED BY [[idx_leafen_page_visits_recent]]
		WHERE [[owner_id]] = {:owner} GROUP BY [[workspace_id]]
	`).Bind(dbx.Params{"owner": principalID}).All(&rows); err != nil {
		return nil, failedBecause(errMCPToolFailed, err)
	}
	last := make(map[string]int64, len(rows))
	for _, row := range rows {
		last[row.WorkspaceID] = row.Last
	}
	ordered := slices.Clone(workspaces)
	slices.SortStableFunc(ordered, func(left, right workspaceAuthorization) int {
		switch {
		case last[left.WorkspaceID] > last[right.WorkspaceID]:
			return -1
		case last[left.WorkspaceID] < last[right.WorkspaceID]:
			return 1
		}
		return 0
	})
	return ordered, nil
}

// mcpSearchWorkspaceSummary is where a leafen_search looked.
type mcpSearchWorkspaceSummary struct {
	Searched  int                         `json:"searched" jsonschema:"How many of the workspaces asked this search has searched so far"`
	Asked     int                         `json:"asked" jsonschema:"How many workspaces this search covers: those named, or every one the connection reaches"`
	Reachable int                         `json:"reachable" jsonschema:"How many workspaces this connection reaches"`
	Results   []mcpSearchWorkspaceResults `json:"results" jsonschema:"This answer's results by workspace"`
	Note      string                      `json:"note,omitempty"`
}

type mcpSearchWorkspaceResults struct {
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name"`
	Count       int    `json:"count"`
}

// summarizeMCPSearchWorkspaces names each result's workspace and says where
// the search looked: the workspaces asked, those searched so far, those
// the connection reaches, and the answer's results by workspace.
func summarizeMCPSearchWorkspaces(authority mcpAuthority, output *mcpSearchOutput, asked []workspaceAuthorization) {
	names := map[string]string{}
	for _, workspace := range authority.ApprovedWorkspaces {
		names[workspace.ID] = workspace.Name
	}
	summary := mcpSearchWorkspaceSummary{
		Searched: output.searched, Asked: output.asked, Reachable: len(authority.WorkspaceIDs), Results: []mcpSearchWorkspaceResults{},
	}
	counts := map[string]int{}
	for index := range output.Results {
		id := output.Results[index].WorkspaceID
		output.Results[index].Workspace = names[id]
		counts[id]++
	}
	for _, workspace := range asked {
		if counts[workspace.WorkspaceID] > 0 {
			summary.Results = append(summary.Results, mcpSearchWorkspaceResults{
				WorkspaceID: workspace.WorkspaceID, Name: names[workspace.WorkspaceID], Count: counts[workspace.WorkspaceID],
			})
		}
	}
	switch {
	case summary.Searched < summary.Asked:
		summary.Note = "Searched " + strconv.Itoa(summary.Searched) + " of " + strconv.Itoa(summary.Asked) +
			" workspaces so far, those used last first: next_cursor goes on to the others once these run out. " +
			"Name workspace_ids where you know the pages are."
	case summary.Asked < summary.Reachable && len(output.Results) == 0 && !output.HasMore:
		// Not found in the workspaces named isn't not found in the others.
		summary.Note = "Nothing found in the workspaces searched; this connection reaches " + strconv.Itoa(summary.Reachable) +
			": leave workspace_id and workspace_ids out to search them all."
	}
	output.Workspaces = &summary
}

// notionMCPSearchLeft is notion-search's notice of the workspaces it hasn't
// searched yet, "" when none.
func notionMCPSearchLeft(output mcpSearchOutput, notion notionMCPSearchInput) string {
	if output.searched >= output.asked {
		return ""
	}
	left := "Searched " + strconv.Itoa(output.searched) + " of the " + strconv.Itoa(output.asked) +
		" workspaces this connection reaches so far, those used last first"
	if !notion.once {
		left += "; next_cursor goes on to the others"
	}
	if notion.native {
		left += ". leafen_search searches only the workspaces you name (workspace_ids)"
	}
	return left + "."
}
