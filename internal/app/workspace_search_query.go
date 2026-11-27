package app

import (
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A search checks its matches in order, a batch at a time, which the reader
// may see, until it has a page of results: ranked by title, it reads them in
// tiers as it goes (workspace_search_tiers.go); by a time sort, it ranks
// every match first. It used to look through every match in index order
// and check each before answering, so a common word in a big workspace took
// 47 calls and 9 s, and with people working, a minute and more (2026-10-15).
const (
	// workspaceSearchCheckBatch is the most matches checked at once; a
	// batch starts at what is still wanted and doubles while too few of it
	// are the reader's.
	workspaceSearchCheckBatch = 128
	// workspaceSearchCheckBudget is how long one call of the app's search
	// checks before it answers that it goes on, with where it was (the index
	// cursor): matches a reader mostly can't see are many. The app calls
	// again at once; an agent's search checks longer (mcpSearchCheckBudget).
	workspaceSearchCheckBudget = 400 * time.Millisecond
)

var errWorkspaceSearchRevisionChanged = errors.New(errorCodeWorkspaceSearchCursorInvalid)

type workspaceSearchCandidate struct {
	PageID string `db:"page_id"`
	Score  int64  `db:"score"`
	// Row is a title or body tier match's place in the index; a time sort's
	// match keeps there the tier it was found in, its score being its time.
	Row int64 `db:"row"`
}

// tier is the tier a match was found in (workspace_search_tiers.go), as
// tiered, or as a time sort keeps it; one a time sort found by its own words
// has its title's score (workspaceSearchTitleScore), which counts as the
// tiers do up to the body's.
func (candidate workspaceSearchCandidate) tier(tiered bool) int64 {
	if tiered {
		return candidate.Score
	}
	return candidate.Row
}

type workspaceSearchQueryStep struct {
	Rows        []storedWorkspaceSearchResult
	HasMore     bool
	Indexing    bool
	Revision    int
	IndexCursor workspaceSearchIndexCursor
	// Last is the last result's place, where a later page starts.
	Last workspaceSearchCandidate
}

// advanceWorkspaceSearch is one call of the app's search, checking for
// workspaceSearchCheckBudget at most.
func advanceWorkspaceSearch(
	app core.App,
	authorization workspaceAuthorization,
	principalID string,
	queryFolded string,
	match workspaceSearchMatch,
	resultCursor workspaceSearchCursor,
	indexCursor workspaceSearchIndexCursor,
	hasIndexCursor bool,
	limit int,
	filter workspaceSearchFilter,
) (workspaceSearchQueryStep, error) {
	return advanceWorkspaceSearchWithin(workspaceSearchCheckBudget, app, authorization, principalID,
		queryFolded, match, resultCursor, indexCursor, hasIndexCursor, limit, filter)
}

// advanceWorkspaceSearchWithin checks matches for a budget at most before it
// answers that it goes on.
func advanceWorkspaceSearchWithin(
	budget time.Duration,
	app core.App,
	authorization workspaceAuthorization,
	principalID string,
	queryFolded string,
	match workspaceSearchMatch,
	resultCursor workspaceSearchCursor,
	indexCursor workspaceSearchIndexCursor,
	hasIndexCursor bool,
	limit int,
	filter workspaceSearchFilter,
) (workspaceSearchQueryStep, error) {
	// The index as it stands (workspace_search_indexer.go). A few waiting
	// pages — what someone just wrote — are settled first when no one else
	// is settling; otherwise the indexer has them within a moment, and the
	// search doesn't wait.
	settleWorkspaceSearchBriefly(app, authorization.WorkspaceID)
	// The matches' permissions are decided once a call (access_memo.go).
	app = withAccessMemo(app)
	revision, err := workspaceSearchRevision(app, authorization.WorkspaceID)
	if err != nil {
		return workspaceSearchQueryStep{}, err
	}
	if !hasIndexCursor {
		indexCursor = newWorkspaceSearchIndexCursor(
			principalID, authorization.WorkspaceID, resultCursor.QueryHash,
			revision, resultCursor.Score, resultCursor.PageID, resultCursor.Row, limit,
		)
	} else if indexCursor.Limit != limit {
		return workspaceSearchQueryStep{}, errWorkspaceSearchRevisionChanged
	}

	// A later page of results starts after the last result shown; a call
	// that goes on starts after the last match the one before it checked.
	tiered := filter.bestMatch()
	from := workspaceSearchCandidate{Score: indexCursor.AfterScore, PageID: indexCursor.AfterPageID, Row: indexCursor.AfterRow}
	if indexCursor.CheckedPageID != "" {
		from = workspaceSearchCandidate{Score: indexCursor.CheckedScore, PageID: indexCursor.CheckedPageID, Row: indexCursor.CheckedRow}
	}
	// A batch's filter keeps to titles by the title scores a time sort has
	// already given its own scores in place of.
	batchFilter := filter
	var next func(after workspaceSearchCandidate, n int) ([]workspaceSearchCandidate, error)
	if tiered {
		next = newWorkspaceSearchTiers(app, authorization.WorkspaceID, queryFolded, match, filter.TitleOnly).after
	} else {
		// A time sort ranks every match by its time: the pages holding every
		// keyword in their own words, and, beyond a title's, those a search
		// ranked by title finds besides — by their path, and matching some —
		// as Notion's time sorts list them too (seen 2026-11-11: "morvadel"
		// newest first lists "W406 plain child" under "W406 Morvadel parent"
		// and "W406 fd plain row" of "W406 Morvadel fulldb"; "xelvora
		// quintabe", the pages holding one of them).
		batchFilter.TitleOnly = false
		ranked, err := loadWorkspaceSearchCandidates(app, authorization, queryFolded, match, filter.TitleOnly)
		if err != nil {
			return workspaceSearchQueryStep{}, err
		}
		beyond, err := newWorkspaceSearchTiers(app, authorization.WorkspaceID, queryFolded, match, filter.TitleOnly).beyondWords()
		if err != nil {
			return workspaceSearchQueryStep{}, err
		}
		listed := make(map[string]bool, len(ranked))
		for _, candidate := range ranked {
			listed[candidate.PageID] = true
		}
		for _, candidate := range beyond {
			if !listed[candidate.PageID] {
				listed[candidate.PageID] = true
				ranked = append(ranked, candidate)
			}
		}
		for index := range ranked {
			ranked[index].Row = ranked[index].Score
		}
		if ranked, err = workspaceSearchSortScores(app, filter, ranked); err != nil {
			return workspaceSearchQueryStep{}, err
		}
		sort.Slice(ranked, func(left, right int) bool { return workspaceSearchBefore(ranked[left], ranked[right], false) })
		next = func(after workspaceSearchCandidate, n int) ([]workspaceSearchCandidate, error) {
			start := 0
			if after.PageID != "" {
				start = sort.Search(len(ranked), func(index int) bool { return workspaceSearchBefore(after, ranked[index], false) })
			}
			return ranked[start:min(start+n, len(ranked))], nil
		}
	}
	// Matches certainly closed to the reader are left out first; a match
	// is the reader's as they read it (workspace_search_reader.go).
	closed, err := newWorkspaceSearchHiddenReader(app, authorization.WorkspaceID, principalID)
	if err != nil {
		return workspaceSearchQueryStep{}, err
	}
	reading := newWorkspaceSearchReading(personReader(principalID), authorization.WorkspaceID, match, queryFolded, tiered)
	// What an earlier call found comes back in its cursor, which anyone may
	// write: it is checked again before it is shown.
	found := workspaceSearchCursorCandidates(indexCursor)
	if len(found) > 0 {
		ids := make([]string, 0, len(found))
		for _, candidate := range found {
			ids = append(ids, candidate.PageID)
		}
		readable, err := readablePageIDs(app, principalID, ids)
		if err != nil {
			return workspaceSearchQueryStep{}, err
		}
		if found, err = liveWorkspaceSearchCandidates(app, authorization, found); err != nil {
			return workspaceSearchQueryStep{}, err
		}
		found = slices.DeleteFunc(found, func(candidate workspaceSearchCandidate) bool { return !readable[candidate.PageID] })
		if found, err = reading.keep(app, found, len(found)); err != nil {
			return workspaceSearchQueryStep{}, err
		}
	}
	// A page settled again while it is searched keeps its row, but one whose
	// place was taken stands at a later row (workspace_search_projection.go):
	// it is found once, where it was first (2026-10-22: a benchmark's
	// searches got cursors holding a page twice, which the next call refused).
	seen := make(map[string]bool, len(found))
	for _, candidate := range found {
		seen[candidate.PageID] = true
	}
	deadline := time.Now().Add(budget)
	size := 0
	for len(found) <= limit {
		size = min(max(size*2, limit+1-len(found), 16), workspaceSearchCheckBatch)
		batch, err := next(from, size)
		if err != nil {
			return workspaceSearchQueryStep{}, err
		}
		if len(batch) == 0 {
			break
		}
		from = batch[len(batch)-1]
		kept, err := liveWorkspaceSearchCandidates(app, authorization, batch)
		if err != nil {
			return workspaceSearchQueryStep{}, err
		}
		// What is certainly closed goes first, before the index and the
		// filters are asked of it.
		ids := make([]string, 0, len(kept))
		for _, candidate := range kept {
			ids = append(ids, candidate.PageID)
		}
		hidden, err := closed.hidden(app, ids)
		if err != nil {
			return workspaceSearchQueryStep{}, err
		}
		kept = slices.DeleteFunc(kept, func(candidate workspaceSearchCandidate) bool { return hidden[candidate.PageID] })
		// Phrases and a word's pairs in order, where the page's row keeps none.
		if kept, err = workspaceSearchKeepOrdered(app, authorization.WorkspaceID, match.Ordered, kept, tiered); err != nil {
			return workspaceSearchQueryStep{}, err
		}
		if kept, err = applyWorkspaceSearchFilter(app, batchFilter, kept); err != nil {
			return workspaceSearchQueryStep{}, err
		}
		ids = make([]string, 0, len(kept))
		for _, candidate := range kept {
			ids = append(ids, candidate.PageID)
		}
		readable, err := readablePageIDs(app, principalID, ids)
		if err != nil {
			return workspaceSearchQueryStep{}, err
		}
		kept = slices.DeleteFunc(kept, func(candidate workspaceSearchCandidate) bool {
			return !readable[candidate.PageID] || seen[candidate.PageID]
		})
		if kept, err = reading.keep(app, kept, limit+1-len(found)); err != nil {
			return workspaceSearchQueryStep{}, err
		}
		for _, candidate := range kept {
			if !seen[candidate.PageID] && len(found) <= limit {
				seen[candidate.PageID] = true
				found = append(found, candidate)
			}
		}
		if len(found) <= limit && time.Now().After(deadline) {
			indexCursor.CandidatePageIDs, indexCursor.CandidateScores, indexCursor.CandidateRows = []string{}, []int64{}, []int64{}
			for _, candidate := range found {
				indexCursor.CandidatePageIDs = append(indexCursor.CandidatePageIDs, candidate.PageID)
				indexCursor.CandidateScores = append(indexCursor.CandidateScores, candidate.Score)
				indexCursor.CandidateRows = append(indexCursor.CandidateRows, candidate.Row)
			}
			indexCursor.CheckedScore, indexCursor.CheckedPageID, indexCursor.CheckedRow = from.Score, from.PageID, from.Row
			return workspaceSearchQueryStep{
				Rows: []storedWorkspaceSearchResult{}, Indexing: true,
				Revision: revision, IndexCursor: indexCursor,
			}, nil
		}
	}

	hasMore := len(found) > limit
	if hasMore {
		found = found[:limit]
	}
	step := workspaceSearchQueryStep{HasMore: hasMore, Revision: revision}
	if len(found) > 0 {
		step.Last = found[len(found)-1]
	}
	// A page gone or changed since it matched is left out.
	if step.Rows, err = hydrateWorkspaceSearchResults(app, authorization, queryFolded, match, found, tiered); err != nil {
		return workspaceSearchQueryStep{}, err
	}
	reading.show(app, step.Rows)
	return step, nil
}

// workspaceSearchInlineSettle is how many waiting pages a search settles
// itself: one person's last edits, not a busy workspace's backlog. Of them it
// makes only pages whose stored document is short, so a search never waits
// for a long page's whole document to be read; the indexer has those within a
// moment (2026-10-20: a search waited for pages of 750 KB others were typing).
const (
	workspaceSearchInlineSettle        = 8
	workspaceSearchInlineDocumentBytes = 64 << 10
)

// settleWorkspaceSearchBriefly settles a workspace's few waiting pages when
// no one else is settling them; it never waits for the stripe.
func settleWorkspaceSearchBriefly(app core.App, workspaceID string) {
	stripe := workspaceSearchStripe(workspaceID)
	if !stripe.TryLock() {
		return
	}
	defer stripe.Unlock()
	var waiting int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM (SELECT 1 FROM {{leafen_search_dirty}} WHERE [[workspace_id]] = {:workspaceId} LIMIT {:limit})
	`).Bind(dbx.Params{"workspaceId": workspaceID, "limit": workspaceSearchInlineSettle + 1}).Row(&waiting); err != nil ||
		waiting == 0 || waiting > workspaceSearchInlineSettle {
		return
	}
	if _, _, err := drainWorkspaceSearchProjection(app, workspaceID, false, workspaceSearchInlineDocumentBytes); err != nil {
		app.Logger().Warn("search: settling waiting pages failed", "workspaceId", workspaceID, "error", err)
	}
}

func filterWorkspaceSearchCandidates(
	app core.App,
	principalID string,
	candidates []workspaceSearchCandidate,
) ([]workspaceSearchCandidate, error) {
	ids := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.PageID)
	}
	readable, err := readablePageIDs(app, principalID, ids)
	if err != nil {
		return nil, err
	}
	visible := make([]workspaceSearchCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if readable[candidate.PageID] {
			visible = append(visible, candidate)
		}
	}
	return visible, nil
}

func newWorkspaceSearchIndexCursor(
	principalID string,
	workspaceID string,
	queryHash string,
	revision int,
	afterScore int64,
	afterPageID string,
	afterRow int64,
	limit int,
) workspaceSearchIndexCursor {
	return workspaceSearchIndexCursor{
		PrincipalID: principalID, WorkspaceID: workspaceID, QueryHash: queryHash,
		Revision: revision, CandidatePageIDs: []string{}, CandidateScores: []int64{}, CandidateRows: []int64{},
		Limit: limit, AfterScore: afterScore, AfterPageID: afterPageID, AfterRow: afterRow,
	}
}

// liveWorkspaceSearchCandidates keeps, of a batch of matches, the pages of
// the workspace out of trash, the database items whose rows and databases
// are out of trash, and the databases and templates found beside them that stand
// (workspace_search_databases.go); an item's page record is not a workspace
// page and does not carry its trash.
func liveWorkspaceSearchCandidates(
	app core.App,
	authorization workspaceAuthorization,
	candidates []workspaceSearchCandidate,
) ([]workspaceSearchCandidate, error) {
	if len(candidates) == 0 {
		return candidates, nil
	}
	ids := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.PageID)
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	live := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[projection]].[[page_id]] FROM {{leafen_search_pages}} [[projection]]
		WHERE [[projection]].[[page_id]] IN (SELECT [[value]] FROM json_each({:ids}))
			AND [[projection]].[[workspace_id]] = {:workspaceId}
			AND [[projection]].[[owner_id]] = {:ownerId}
			AND (EXISTS (
				SELECT 1 FROM {{leafen_pages}} [[page]]
				WHERE [[page]].[[id]] = [[projection]].[[page_id]] AND [[page]].[[owner]] = {:ownerId}
					AND [[page]].[[workspace]] = {:workspaceId} AND [[page]].[[in_trash]] = 0
					AND [[page]].[[data_source_id]] = ''
			) OR EXISTS (
				SELECT 1 FROM {{leafen_database_row_pages}} [[row_page]]
				WHERE [[row_page]].[[row_id]] = [[projection]].[[page_id]]
					AND [[row_page]].[[owner]] = {:ownerId} AND [[row_page]].[[workspace_id]] = {:workspaceId}
					AND ` + databaseRowNotTrashedSQL("[[row_page]].[[row_id]]") + `
					AND ` + databaseSourceNotTrashedSQL("[[row_page]].[[data_source_id]]") + `
					AND ` + searchSourceNotDeletedSQL("{:workspaceId}", "[[row_page]].[[data_source_id]]") + `
			) OR ` + searchDatabaseEntryLiveSQL("[[projection]]") + `)
	`).Bind(dbx.Params{
		"ids": string(encoded), "workspaceId": authorization.WorkspaceID, "ownerId": authorization.OwnerID,
	}).Column(&live); err != nil {
		return nil, err
	}
	alive := make(map[string]bool, len(live))
	for _, id := range live {
		alive[id] = true
	}
	kept := make([]workspaceSearchCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if alive[candidate.PageID] {
			kept = append(kept, candidate)
		}
	}
	return kept, nil
}

func workspaceSearchCursorCandidates(cursor workspaceSearchIndexCursor) []workspaceSearchCandidate {
	result := make([]workspaceSearchCandidate, 0, len(cursor.CandidatePageIDs))
	for index, pageID := range cursor.CandidatePageIDs {
		result = append(result, workspaceSearchCandidate{
			PageID: pageID, Score: cursor.CandidateScores[index], Row: cursor.CandidateRows[index],
		})
	}
	return result
}

// workspaceSearchScopedMatch keeps a match to one workspace; without
// keywords it matches every page of the workspace.
func workspaceSearchScopedMatch(workspaceID, matchExpression string) string {
	scope := workspaceSearchFTSTerm("workspace_token", workspaceSearchScopeToken(workspaceID))
	if matchExpression == "" {
		return scope
	}
	return scope + " AND (" + matchExpression + ")"
}

// workspaceSearchPagesMatch narrows a match to chosen pages.
func workspaceSearchPagesMatch(pageTerms []string, matchExpression string) string {
	pages := "(" + strings.Join(pageTerms, " OR ") + ")"
	if matchExpression == "" {
		return pages
	}
	return pages + " AND (" + matchExpression + ")"
}
