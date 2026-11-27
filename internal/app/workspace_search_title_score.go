package app

import (
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// loadWorkspaceSearchCandidates reads a time sort's matches from the index
// alone, each page once with its title's score (workspaceSearchTitleScore).
// Whether a match is a live page of the workspace is asked of the batches
// checked (liveWorkspaceSearchCandidates), not of every match: on a common
// word in a big workspace that was most of a search's time.
func loadWorkspaceSearchCandidates(
	app core.App,
	authorization workspaceAuthorization,
	queryFolded string,
	match workspaceSearchMatch,
	titleOnly bool,
) ([]workspaceSearchCandidate, error) {
	matches := []struct {
		PageID string `db:"page_id"`
		Title  string `db:"title"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[projection]].[[page_id]], [[projection]].[[title_folded]] AS [[title]]
		FROM {{leafen_search_pages_fts}} [[fts]]
		CROSS JOIN {{leafen_search_pages}} [[projection]] INDEXED BY [[idx_leafen_search_pages_fts_row]]
			ON [[projection]].[[fts_row]] = [[fts]].[[rowid]]
		WHERE {{leafen_search_pages_fts}} MATCH {:match}
	`).Bind(dbx.Params{
		"match": workspaceSearchScopedMatch(authorization.WorkspaceID, match.Every),
	}).All(&matches); err != nil {
		return nil, err
	}
	// A page has one row in the index; were one settled twice, its best stays.
	best := make(map[string]int, len(matches))
	candidates := make([]workspaceSearchCandidate, 0, len(matches))
	for _, found := range matches {
		score := workspaceSearchTitleScore(found.Title, queryFolded, match.Keywords, titleOnly)
		if at, seen := best[found.PageID]; seen {
			candidates[at].Score = min(candidates[at].Score, score)
			continue
		}
		best[found.PageID] = len(candidates)
		candidates = append(candidates, workspaceSearchCandidate{PageID: found.PageID, Score: score})
	}
	// And the pages whose words are apart, found by their chunks.
	apart, err := workspaceSearchApartMatches(app, authorization.WorkspaceID, match.LooseTerms, "")
	if err != nil {
		return nil, err
	}
	for _, page := range apart {
		if _, seen := best[page.PageID]; !seen {
			best[page.PageID] = len(candidates)
			candidates = append(candidates, workspaceSearchCandidate{
				PageID: page.PageID, Score: workspaceSearchTitleScore(page.Title, queryFolded, match.Keywords, titleOnly),
			})
		}
	}
	return candidates, nil
}

// workspaceSearchTitleScore is a folded title's tier as a search ranked by
// title finds it (workspace_search_tiers.go): the keywords themselves, a
// title starting with them and holding them as they are asked, one holding
// them by the keywords' one rule (workspace_search_keywords.go), or none,
// a page then found by its body — or, keeping to titles, one holding some
// (workspaceSearchTitleOnlyTier). So keeping to titles keeps the same pages
// whatever the sort: newest first and titles only, "11/23" lists the titles
// "23 … 11" and "11 … 23" too, as the best matches do and as Notion's search
// does ("19/37" kept "w 37 apart 19" and "w 19 apart 37" newest first, seen
// 2026-11-24 in a test workspace, two tabs).
func workspaceSearchTitleScore(title, query string, keywords workspaceSearchKeywords, titleOnly bool) int64 {
	if title == query {
		return workspaceSearchTierExact
	}
	if all, some := keywords.match(title); !all {
		if titleOnly && some {
			return workspaceSearchTierSomeTitle
		}
		return workspaceSearchTierBody
	}
	if strings.HasPrefix(title, query) {
		return workspaceSearchTierStarts
	}
	return workspaceSearchTierTitle
}
