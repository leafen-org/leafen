package app

import (
	"encoding/base64"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// notion-search searches every workspace the connection was given, as
// Notion's searches its whole workspace, unless what it is given places it:
// a team or a workspace (teamspace_id), a page (page_url), a data source
// (data_source_url), or the workspace_id any tool takes (mcp_tool_arguments.go).
// It took no workspace_id and, on a connection reaching more than one, said
// it needed one (2026-11-13), so such a connection could not search at all.
//
// The workspaces' results come as one list in the search's order: a match's
// tier, or its time for a time sort (workspaceSearchBefore), a workspace's
// own matches in their order. Each workspace's search goes on from its own
// place, which next_cursor keeps for all of them.

const (
	mcpSearchAcrossPrefix    = "a."
	maxMCPSearchAcrossCursor = 64 << 10
)

// mcpSearchPlace is where one workspace's part of a search goes on from.
type mcpSearchPlace struct {
	WorkspaceID string `json:"w"`
	// Cursor is its own search's start_cursor; none starts at the first.
	Cursor string `json:"c,omitempty"`
	Done   bool   `json:"d,omitzero"`
	// Started says it has been searched: a call searches a few workspaces
	// (mcpSearchWorkspacesPerCall), the others once those are done.
	Started bool `json:"s,omitzero"`
}

// notionMCPSearchWorkspaces are the workspaces a notion-search searches.
func notionMCPSearchWorkspaces(app core.App, authority mcpAuthority, notion notionMCPSearchInput) ([]workspaceAuthorization, error) {
	one := func(input mcpWorkspaceInput) ([]workspaceAuthorization, error) {
		authorization, err := selectMCPWorkspace(app, authority, input, permissionWorkspaceRead)
		if err != nil {
			return nil, err
		}
		return []workspaceAuthorization{authorization}, nil
	}
	if id := strings.TrimSpace(notion.TeamspaceID); id != "" || len(authority.WorkspaceIDs) == 1 {
		return one(mcpWorkspaceInput{TeamspaceID: id, AllowImplicit: true})
	}
	if placed := notionMCPSearchPlaced(app, notion); placed != "" && slices.Contains(authority.WorkspaceIDs, placed) {
		return one(mcpWorkspaceInput{WorkspaceID: placed})
	}
	return mcpSearchEveryWorkspace(app, authority)
}

// notionMCPSearchPlaced is the workspace of the page or data source a
// search keeps to, "" for none found.
func notionMCPSearchPlaced(app core.App, notion notionMCPSearchInput) string {
	if raw := strings.TrimSpace(notion.PageURL); raw != "" {
		in, ok, err := mcpSearchPageKeptTo(app, raw)
		if err != nil || !ok {
			return ""
		}
		if record, err := app.FindRecordById(pagesCollection, in[0]); err == nil {
			return record.GetString("workspace")
		}
		// An inline database is kept to by its own ID.
		if location, err := loadMCPDatabaseContainerLocationWith(app, in[0], true); err == nil {
			return location.WorkspaceID
		}
		return ""
	}
	if raw := strings.TrimSpace(notion.DataSourceURL); raw != "" {
		dataSourceID, ok := parseMCPDataSourceIdentifier(raw)
		if !ok {
			return ""
		}
		if location, err := loadMCPDataSourceLocationToRead(app, dataSourceID); err == nil {
			return location.WorkspaceID
		}
	}
	return ""
}

// searchMCPAcross searches workspaces as one, a page of results from where
// each one's search was: the first mcpSearchWorkspacesPerCall not done, in
// the order given, the others once those are.
func searchMCPAcross(
	app core.App, authority mcpAuthority, workspaces []workspaceAuthorization,
	filterOf func(workspaceID string) (workspaceSearchFilter, error), query, startCursor string, limit int,
) (mcpSearchOutput, error) {
	places, err := mcpSearchPlaces(workspaces, startCursor)
	if err != nil {
		return mcpSearchOutput{}, err
	}
	byID := map[string]workspaceAuthorization{}
	for _, authorization := range workspaces {
		byID[authorization.WorkspaceID] = authorization
	}
	// The call's checking time is shared by the workspaces it searches.
	deadline := time.Now().Add(mcpSearchCheckBudget)
	searching := make([]bool, len(places))
	open := 0
	for index, place := range places {
		if !place.Done && open < mcpSearchWorkspacesPerCall {
			searching[index] = true
			open++
		}
	}
	runs := make([]*mcpSearchRun, len(places))
	shown := make([][]storedWorkspaceSearchResult, len(places))
	checking, backlog := false, false
	for index, place := range places {
		if !searching[index] {
			continue
		}
		authorization := byID[place.WorkspaceID]
		filter, err := filterOf(authorization.WorkspaceID)
		if err != nil {
			return mcpSearchOutput{}, err
		}
		budget := max(time.Until(deadline)/time.Duration(open), workspaceSearchCheckBudget)
		open--
		run, err := advanceMCPSearch(app, authority, authorization, query, filter, place.Cursor, limit, budget)
		if err != nil {
			return mcpSearchOutput{}, err
		}
		places[index].Started = true
		if run.step.Indexing {
			// It goes on from where it was checking; the others are
			// searched again from their places once it has its matches.
			if places[index].Cursor, err = run.goesOn(); err != nil {
				return mcpSearchOutput{}, err
			}
			checking = true
			continue
		}
		runs[index] = &run
		if shown[index], err = run.shown(app, authority); err != nil {
			return mcpSearchOutput{}, err
		}
		backlog = backlog || workspaceSearchBacklog(app, authorization.WorkspaceID)
	}
	if checking {
		next, err := encodeMCPSearchPlaces(places)
		if err != nil {
			return mcpSearchOutput{}, err
		}
		searched, asked := mcpSearchPlacesStarted(places)
		return mcpSearchOutput{Results: []mcpSearchResult{}, HasMore: true, NextCursor: &next, Indexing: true, searched: searched, asked: asked}, nil
	}

	taken := mcpSearchTake(runs, shown, limit)
	output := mcpSearchOutput{Results: []mcpSearchResult{}}
	if backlog {
		output.Backlog = mcpSearchBacklogNotice
	}
	reads := make([][]contentRead, len(places))
	for _, at := range taken {
		results, found, err := runs[at.place].results(app, authority, []storedWorkspaceSearchResult{shown[at.place][at.row]})
		if err != nil {
			return mcpSearchOutput{}, err
		}
		output.Results = append(output.Results, results...)
		reads[at.place] = append(reads[at.place], found...)
	}
	for index, found := range reads {
		if len(found) > 0 {
			recordMCPReads(app, authority, places[index].WorkspaceID, found)
		}
	}
	for index, run := range runs {
		if run == nil {
			continue
		}
		if places[index], err = mcpSearchPlaceAfter(app, authority, places[index], *run, shown[index], taken, index, limit); err != nil {
			return mcpSearchOutput{}, err
		}
		places[index].Started = true
	}
	output.searched, output.asked = mcpSearchPlacesStarted(places)
	if slices.ContainsFunc(places, func(place mcpSearchPlace) bool { return !place.Done }) {
		next, err := encodeMCPSearchPlaces(places)
		if err != nil {
			return mcpSearchOutput{}, err
		}
		output.HasMore, output.NextCursor = true, &next
	}
	return output, nil
}

type mcpSearchTaken struct{ place, row int }

// mcpSearchTake merges the workspaces' results in the search's order, up to
// a page. A workspace whose results run out while it has more ends the page
// where its later ones could come.
func mcpSearchTake(runs []*mcpSearchRun, shown [][]storedWorkspaceSearchResult, limit int) []mcpSearchTaken {
	heads := make([]int, len(runs))
	taken := []mcpSearchTaken{}
	for len(taken) < limit {
		best := -1
		for index, run := range runs {
			if run != nil && heads[index] < len(shown[index]) &&
				(best < 0 || shown[index][heads[index]].Score < shown[best][heads[best]].Score) {
				best = index
			}
		}
		if best < 0 {
			break
		}
		// Matches alike (a tier) come in no order between workspaces.
		next := shown[best][heads[best]].Score
		for index, run := range runs {
			if run != nil && heads[index] == len(shown[index]) && run.step.HasMore && next > run.step.Last.Score {
				return taken
			}
		}
		taken = append(taken, mcpSearchTaken{place: best, row: heads[best]})
		heads[best]++
	}
	return taken
}

// mcpSearchPlaceAfter is where a workspace's search goes on from after a
// page took some of its results: after the last taken, found again as the
// last of a page that long; after all it checked once all are taken (those
// an answer doesn't list too); where it was when none are.
func mcpSearchPlaceAfter(
	app core.App, authority mcpAuthority, place mcpSearchPlace, run mcpSearchRun,
	shown []storedWorkspaceSearchResult, taken []mcpSearchTaken, index, limit int,
) (mcpSearchPlace, error) {
	count := 0
	for _, at := range taken {
		if at.place == index {
			count++
		}
	}
	if count == len(shown) {
		if !run.step.HasMore || len(run.step.Rows) == 0 {
			return mcpSearchPlace{WorkspaceID: place.WorkspaceID, Done: true}, nil
		}
		next, err := run.after(run.step.Last)
		return mcpSearchPlace{WorkspaceID: place.WorkspaceID, Cursor: next}, err
	}
	if count == 0 {
		return place, nil
	}
	last := shown[count-1].PageID
	through := slices.IndexFunc(run.step.Rows, func(row storedWorkspaceSearchResult) bool { return row.PageID == last }) + 1
	from := ""
	if run.from.PageID != "" {
		cursor, err := encodeWorkspaceSearchCursor(run.from)
		if err != nil {
			return place, failedBecause(errMCPToolFailed, err)
		}
		from = "r." + cursor
	}
	again, err := advanceMCPSearch(app, authority, run.authorization, run.query, run.filter, from,
		min(max(through, 1), limit), workspaceSearchCheckBudget)
	// Not found again within the moment, it goes on from where it was: the
	// taken ones may come again.
	if err != nil || again.step.Indexing || !again.step.HasMore || len(again.step.Rows) == 0 {
		return place, nil
	}
	next, err := again.after(again.step.Last)
	return mcpSearchPlace{WorkspaceID: place.WorkspaceID, Cursor: next}, err
}

// mcpSearchPlaces reads a search's places from its start_cursor, or starts
// one in each workspace; a workspace the connection no longer searches is
// left out.
func mcpSearchPlaces(workspaces []workspaceAuthorization, startCursor string) ([]mcpSearchPlace, error) {
	if startCursor == "" {
		places := make([]mcpSearchPlace, 0, len(workspaces))
		for _, authorization := range workspaces {
			places = append(places, mcpSearchPlace{WorkspaceID: authorization.WorkspaceID})
		}
		return places, nil
	}
	invalid := mcpInvalid("start_cursor isn't one this search gave, or the search changed since: search again without it")
	raw, across := strings.CutPrefix(startCursor, mcpSearchAcrossPrefix)
	if !across || len(raw) > maxMCPSearchAcrossCursor {
		return nil, invalid
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	places := []mcpSearchPlace{}
	if err != nil || json.Unmarshal(data, &places) != nil || len(places) == 0 {
		return nil, invalid
	}
	return slices.DeleteFunc(places, func(place mcpSearchPlace) bool {
		return !slices.ContainsFunc(workspaces, func(authorization workspaceAuthorization) bool {
			return authorization.WorkspaceID == place.WorkspaceID
		})
	}), nil
}

func encodeMCPSearchPlaces(places []mcpSearchPlace) (string, error) {
	data, err := json.Marshal(places)
	if err != nil {
		return "", failedBecause(errMCPToolFailed, err)
	}
	return mcpSearchAcrossPrefix + base64.RawURLEncoding.EncodeToString(data), nil
}

// mcpSearchPlacesStarted counts a search's workspaces searched so far, and all.
func mcpSearchPlacesStarted(places []mcpSearchPlace) (int, int) {
	started := 0
	for _, place := range places {
		if place.Started || place.Done {
			started++
		}
	}
	return started, len(places)
}
