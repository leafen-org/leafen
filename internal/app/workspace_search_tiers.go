package app

import (
	"database/sql"
	"errors"
	"sort"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A search ranked by title reads its matches in tiers, each a few at a
// time as they are checked, never all of them first: the pages titled with
// the keywords (by page), then those whose titles start with them (by
// title, then page; both from the folded titles' index), then the others
// whose titles match, then the rest (each in the index's order; the pages
// holding the keywords' joined runs as written, by their titles then by any
// words, come before the others: workspace_search_joined.go), then the
// pages whose path and words hold them, as Notion's search finds them by
// their path (seen 2026-11-04, 2026-11-11; workspace_search_path.go), then,
// for several keywords, the pages that match only some of them, as Notion's
// search lists them after (seen 2026-10-31): those whose titles hold some
// before those whose bodies alone do, as Notion's search lists "W406
// Xelvora titleonly" and "W406 Quintabe titleonly" before "W406 plain
// bodytwo" for "xelvora quintabe" (seen 2026-11-11), and those holding some
// by their path alone last (seen 2026-11-11). On a common word in a big
// workspace, reading every match to rank it took most of a search
// (2026-10-15).
//
// The index's order is the order the pages came into it, older first: a
// page indexed again keeps its place (workspace_search_projection.go), so an
// edit, a rename, a new icon or sub-page doesn't move it within its tier,
// nor does who reads or when. Notion orders a tier by a learned reranker of
// visits, times and meaning, which moves with the person and time (seen
// 2026-11-11); Leafen keeps a fixed order (decision 2026-10-31).
//
// A match's score is its tier; its row, in the body tier and among the
// pages matching some by their words, its place in the index, and among the
// pages found by their path, 0 for one whose title holds a keyword. A place
// among titles that start with the keywords is its page, whose title is
// read again to go on after it.

const (
	workspaceSearchTierExact = iota
	workspaceSearchTierStarts
	workspaceSearchTierTitleJoined
	workspaceSearchTierBodyJoined
	workspaceSearchTierTitle
	workspaceSearchTierBody
	workspaceSearchTierPath
	workspaceSearchTierSomeTitle
	workspaceSearchTierSome
	workspaceSearchTierSomePath
)

// workspaceSearchTitleTier is whether a tier is of the pages whose titles
// hold every keyword.
func workspaceSearchTitleTier(score int64) bool {
	return score <= workspaceSearchTierTitleJoined || score == workspaceSearchTierTitle
}

// workspaceSearchTitleOnlyTier is whether a tier is one keeping to titles
// keeps: the pages whose titles hold every keyword, then those whose titles
// hold some, as Notion's "titles only" lists them in either sort (seen
// 2026-11-24 in a test workspace: "longer zzzq" kept the title "w nm
// longer", "37/19" the title "AB02b s192 대시보드" besides the titles holding
// both).
func workspaceSearchTitleOnlyTier(score int64) bool {
	return workspaceSearchTitleTier(score) || score == workspaceSearchTierSomeTitle
}

// workspaceSearchSomeTier is whether a tier is of the pages matching some
// keywords by their own words.
func workspaceSearchSomeTier(score int64) bool {
	return score == workspaceSearchTierSomeTitle || score == workspaceSearchTierSome
}

// workspaceSearchPathMatches bounds the pages one search reads whose titles
// hold a keyword, and the places it reads under them.
const workspaceSearchPathMatches = 2000

// workspaceSearchWholeTier is a read's bound that takes a tier whole.
const workspaceSearchWholeTier = 1 << 30

// workspaceSearchBefore orders matches: by tier, or by a time sort's score;
// then, in a title or body tier, by the index's order; then by page.
func workspaceSearchBefore(left, right workspaceSearchCandidate, tiered bool) bool {
	if left.Score != right.Score {
		return left.Score < right.Score
	}
	if tiered && left.Score >= workspaceSearchTierTitleJoined && left.Row != right.Row {
		return left.Row < right.Row
	}
	return left.PageID < right.PageID
}

// workspaceSearchInOrder is whether matches found one after another may
// stand in that order: a title tier's by its title, which a place doesn't
// keep, is not asked.
func workspaceSearchInOrder(left, right workspaceSearchCandidate, tiered bool) bool {
	if tiered && left.Score == workspaceSearchTierStarts && right.Score == workspaceSearchTierStarts {
		return left.PageID != right.PageID
	}
	return workspaceSearchBefore(left, right, tiered)
}

type workspaceSearchTiers struct {
	app         core.App
	workspaceID string
	query       string
	match       workspaceSearchMatch
	titleOnly   bool
	// apart is the body tier's pages whose words are apart, read once.
	apart *workspaceSearchApartRead
	// path is what the search finds by pages' paths, read once.
	path *workspaceSearchPathRead
	// joined is the pages holding the keywords' joined runs, read once.
	joined *workspaceSearchJoinedRead
}

type workspaceSearchApartRead struct {
	read  bool
	pages []workspaceSearchApartPage
}

func newWorkspaceSearchTiers(app core.App, workspaceID, queryFolded string, match workspaceSearchMatch, titleOnly bool) workspaceSearchTiers {
	return workspaceSearchTiers{
		app: app, workspaceID: workspaceID, query: queryFolded, titleOnly: titleOnly,
		match: match, apart: &workspaceSearchApartRead{}, path: &workspaceSearchPathRead{},
		joined: &workspaceSearchJoinedRead{},
	}
}

// after reads up to n matches after a position, in order.
func (tiers workspaceSearchTiers) after(position workspaceSearchCandidate, n int) ([]workspaceSearchCandidate, error) {
	last := int64(workspaceSearchTierPath)
	if tiers.match.Some != "" {
		last = workspaceSearchTierSomePath
	}
	for tier := position.Score; tier <= last; tier++ {
		if tiers.titleOnly && !workspaceSearchTitleOnlyTier(tier) {
			continue
		}
		from := position
		if tier != position.Score {
			from = workspaceSearchCandidate{Score: tier}
		}
		matches, err := tiers.read(tier, from, n)
		if err != nil || len(matches) > 0 {
			return matches, err
		}
	}
	return nil, nil
}

// beyondWords reads every match after the pages holding every keyword in
// their own words: those found by their path, then, for several keywords,
// those matching some, for a time sort, which ranks them all. A tier is
// read whole, in one look: the index reads a start of a word's every match
// again for each look, and read a batch at a time, the pages matching some
// were looked through once for each batch, most of what was left of a time
// sort on a NAS (2026-11-13). Keeping to titles, only the titles holding
// some keywords are read.
func (tiers workspaceSearchTiers) beyondWords() ([]workspaceSearchCandidate, error) {
	last := int64(workspaceSearchTierPath)
	if tiers.match.Some != "" {
		last = workspaceSearchTierSomePath
	}
	found := []workspaceSearchCandidate{}
	for tier := int64(workspaceSearchTierPath); tier <= last; tier++ {
		if tiers.titleOnly && !workspaceSearchTitleOnlyTier(tier) {
			continue
		}
		whole, err := tiers.read(tier, workspaceSearchCandidate{Score: tier}, workspaceSearchWholeTier)
		if err != nil {
			return nil, err
		}
		found = append(found, whole...)
	}
	return found, nil
}

func (tiers workspaceSearchTiers) read(tier int64, from workspaceSearchCandidate, n int) ([]workspaceSearchCandidate, error) {
	switch tier {
	case workspaceSearchTierExact:
		ids := []string{}
		if err := tiers.app.DB().NewQuery(`
			SELECT [[page_id]] FROM {{leafen_search_pages}} INDEXED BY [[idx_leafen_search_pages_workspace_title]]
			WHERE [[workspace_id]] = {:workspaceId} AND [[title_folded]] = {:query} AND [[page_id]] > {:after}
			ORDER BY [[page_id]] LIMIT {:limit}
		`).Bind(dbx.Params{
			"workspaceId": tiers.workspaceID, "query": tiers.query, "after": from.PageID, "limit": n,
		}).Column(&ids); err != nil {
			return nil, err
		}
		return workspaceSearchTierMatches(ids, tier), nil
	case workspaceSearchTierStarts:
		// After a page, by its title as it stands; gone, from the start again.
		afterTitle, afterPage := tiers.query, ""
		if from.PageID != "" {
			var title string
			err := tiers.app.DB().NewQuery(`SELECT [[title_folded]] FROM {{leafen_search_pages}} WHERE [[page_id]] = {:pageId}`).
				Bind(dbx.Params{"pageId": from.PageID}).Row(&title)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return nil, err
			}
			if err == nil {
				afterTitle, afterPage = title, from.PageID
			}
		}
		for {
			rows := []workspaceSearchTierRow{}
			if err := tiers.app.DB().NewQuery(`
				SELECT [[page_id]], [[title_folded]] AS [[title]] FROM {{leafen_search_pages}} INDEXED BY [[idx_leafen_search_pages_workspace_title]]
				WHERE [[workspace_id]] = {:workspaceId} AND [[title_folded]] > {:query} AND [[title_folded]] < {:queryEnd}
					AND [[title_folded]] >= {:afterTitle} AND ([[title_folded]], [[page_id]]) > ({:afterTitle}, {:afterPage})
				ORDER BY [[title_folded]], [[page_id]] LIMIT {:limit}
			`).Bind(dbx.Params{
				"workspaceId": tiers.workspaceID, "query": tiers.query, "queryEnd": tiers.query + "\U0010FFFF",
				"afterTitle": afterTitle, "afterPage": afterPage, "limit": n,
			}).All(&rows); err != nil {
				return nil, err
			}
			if len(rows) == 0 {
				return nil, nil
			}
			ids := []string{}
			for _, row := range rows {
				if tiers.titleStarts(row.Title) {
					ids = append(ids, row.PageID)
				}
			}
			if len(ids) > 0 {
				return workspaceSearchTierMatches(ids, tier), nil
			}
			afterTitle, afterPage = rows[len(rows)-1].Title, rows[len(rows)-1].PageID
		}
	case workspaceSearchTierTitleJoined, workspaceSearchTierBodyJoined:
		return tiers.readJoined(tier, from, n)
	case workspaceSearchTierPath, workspaceSearchTierSomePath:
		path, err := tiers.pathMatches()
		if err != nil {
			return nil, err
		}
		found := path.all
		if tier == workspaceSearchTierSomePath {
			found = path.some
		}
		// After a place, or from the start of the tier.
		at := 0
		if from.PageID != "" {
			at = sort.Search(len(found), func(index int) bool { return workspaceSearchBefore(from, found[index], true) })
		}
		return found[at:min(len(found), at+n)], nil
	}
	// A page matching some keywords by its words whose path holds the rest
	// was found by its path; keeping to titles, which reads no path, by its title.
	var complete map[string]bool
	if workspaceSearchSomeTier(tier) && !tiers.titleOnly {
		path, err := tiers.pathMatches()
		if err != nil {
			return nil, err
		}
		complete = path.complete
	}
	// A page holding the joined runs was read before the others of its tier.
	if (tier == workspaceSearchTierTitle || tier == workspaceSearchTierBody) && len(tiers.match.Joined) > 0 {
		joined, err := tiers.joinedPages()
		if err != nil {
			return nil, err
		}
		complete = joined.bodies
		if tier == workspaceSearchTierTitle {
			complete = joined.titles
		}
	}
	// Titles that match the keywords as words, then everything else; a title
	// starting with the keywords was read in its own tier. A title is kept
	// whole in the index, with its pairs (workspaceSearchTitleMatch), so it is
	// matched exactly; the rest is the page's words, whose ordered terms are
	// checked after (workspace_search_order.go).
	// Of the pages matching some, those whose titles hold some, then the rest;
	// a title holds the last number typed as its start (SomeTitle) besides.
	match := workspaceSearchTitleMatch(tiers.match.Exact)
	switch tier {
	case workspaceSearchTierSomeTitle:
		// Keeping to titles, a title holding some keywords is one, whatever
		// its body holds.
		every := tiers.match.Every
		if tiers.titleOnly {
			every = match
		}
		match = "(" + tiers.match.SomeTitle + ") NOT (" + every + ")"
	case workspaceSearchTierSome:
		match = "((" + tiers.match.Some + ") NOT (" + tiers.match.Every + ")) NOT " + tiers.match.SomeTitle
	case workspaceSearchTierTitle:
	default:
		match = "(" + tiers.match.Loose + ") NOT " + match
	}
	// The body tier's pages whose words are apart stand among the others by
	// their rows (workspace_search_apart.go); their titles didn't match.
	var apart []workspaceSearchApartPage
	if tier == workspaceSearchTierBody {
		var err error
		if apart, err = tiers.apartPages(); err != nil {
			return nil, err
		}
	}
	row := from.Row
	for {
		rows := []workspaceSearchTierRow{}
		// The index gives each match's row; its page and title are read beside
		// it, not from the index's copy of the page (schema_baseline.json).
		if err := tiers.app.DB().NewQuery(`
			SELECT [[fts]].[[rowid]] AS [[row]], [[projection]].[[page_id]], [[projection]].[[title_folded]] AS [[title]]
			FROM {{leafen_search_pages_fts}} [[fts]]
			CROSS JOIN {{leafen_search_pages}} [[projection]] INDEXED BY [[idx_leafen_search_pages_fts_row]]
				ON [[projection]].[[fts_row]] = [[fts]].[[rowid]]
			WHERE {{leafen_search_pages_fts}} MATCH {:match} AND [[fts]].[[rowid]] > {:after}
			ORDER BY [[fts]].[[rowid]] LIMIT {:limit}
		`).Bind(dbx.Params{
			"match": workspaceSearchScopedMatch(tiers.workspaceID, match), "after": row, "limit": n,
		}).All(&rows); err != nil {
			return nil, err
		}
		if tier == workspaceSearchTierBody {
			rows = mergeWorkspaceSearchApart(rows, apart, row, n)
		}
		if len(rows) == 0 {
			return nil, nil
		}
		matches := make([]workspaceSearchCandidate, 0, len(rows))
		for _, found := range rows {
			if !tiers.titleStarts(found.Title) && !complete[found.PageID] {
				matches = append(matches, workspaceSearchCandidate{PageID: found.PageID, Score: tier, Row: found.Row})
			}
		}
		if len(matches) > 0 {
			return matches, nil
		}
		row = rows[len(rows)-1].Row
	}
}

// apartPages reads once the body tier's pages whose words are apart.
func (tiers workspaceSearchTiers) apartPages() ([]workspaceSearchApartPage, error) {
	if !tiers.apart.read {
		apart, err := workspaceSearchApartMatches(tiers.app, tiers.workspaceID, tiers.match.LooseTerms, workspaceSearchTitleMatch(tiers.match.Exact))
		if err != nil {
			return nil, err
		}
		tiers.apart.read, tiers.apart.pages = true, apart
	}
	return tiers.apart.pages, nil
}

// titleStarts is whether a title starts with the keywords and holds them as
// they are asked: a phrase's last word whole ("zorvek kravnid" doesn't
// start with "zorvek krav" in quotes). Such a page is read in its own tier.
func (tiers workspaceSearchTiers) titleStarts(title string) bool {
	if !strings.HasPrefix(title, tiers.query) {
		return false
	}
	all, _ := tiers.match.Keywords.match(title)
	return all
}

type workspaceSearchTierRow struct {
	Row    int64  `db:"row"`
	PageID string `db:"page_id"`
	Title  string `db:"title"`
}

// mergeWorkspaceSearchApart puts the pages whose words are apart, after a
// row, among a batch of the index's matches, keeping the first n by row.
func mergeWorkspaceSearchApart(rows []workspaceSearchTierRow, apart []workspaceSearchApartPage, after int64, n int) []workspaceSearchTierRow {
	merged := append([]workspaceSearchTierRow{}, rows...)
	for _, page := range apart {
		if page.Row > after {
			merged = append(merged, workspaceSearchTierRow{Row: page.Row, PageID: page.PageID, Title: page.Title})
		}
	}
	sort.Slice(merged, func(left, right int) bool { return merged[left].Row < merged[right].Row })
	return merged[:min(len(merged), n)]
}

func workspaceSearchTierMatches(ids []string, tier int64) []workspaceSearchCandidate {
	matches := make([]workspaceSearchCandidate, 0, len(ids))
	for _, id := range ids {
		matches = append(matches, workspaceSearchCandidate{PageID: id, Score: tier})
	}
	return matches
}
