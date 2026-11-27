package app

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/pocketbase/dbx"
)

// Words written joined, with no space between them — "11/23", "2026-11-23",
// "11.23", "257-97" — are words of their own to the index, each found as any
// keyword is (workspace_search_keywords.go), but the pages holding such a run
// as written come first: Notion's search lists for "11/23" the pages holding
// 11 and 23 side by side in that order, a mark or a space between
// ("2026/11/23", "11-23", "11.23", "11 23", "11/23/2026"), before those
// holding them otherwise ("23/11", "청구항 11 에서 청구항 23") and, last, those
// holding one of them, a title holding the run before a title holding its
// words apart (seen 2026-11-24 in a test workspace on synthetic pages, for
// "11/23", "11-23", "11.23", "2026-11-23" and "257-97"). After the titles
// equal to or starting with the keywords, the titles holding every run, then
// any page holding them, come before the other titles and bodies. A
// number's words are whole (searchNumberWord), so for "11/23" no "110/230",
// "1123" or "114-67 2383" is found, as Notion lists none. Notion orders the
// rest by a weight it learns; Leafen keeps the index's order (decision
// 2026-10-31).

// workspaceSearchJoinedRuns are the runs of words a part of the keywords
// writes joined, each of two words or more.
func workspaceSearchJoinedRuns(text string) [][]string {
	runs := [][]string{}
	for _, field := range strings.Fields(text) {
		if words := workspaceSearchWords(field); len(words) > 1 {
			runs = append(runs, words)
		}
	}
	return runs
}

// joinedPhrases are the joined runs, each a phrase of whole words in order.
func (keywords workspaceSearchKeywords) joinedPhrases() []string {
	phrases := make([]string, 0, len(keywords.Joined))
	for _, run := range keywords.Joined {
		phrases = append(phrases, `"`+strings.Join(run, " ")+`"`)
	}
	return phrases
}

// workspaceSearchPlainWords are the words of a search's plain keywords
// (workspaceSearchPlainText) a page's lines are looked through for, as the
// keywords read them: "11/23" is 11 and 23, which a line holds as "11-23" or
// "청구항 11 … 23" too, as Notion's result lines show (seen 2026-11-24).
func workspaceSearchPlainWords(plainQuery string) []string {
	return workspaceSearchWords(plainQuery)
}

// workspaceSearchJoinedPages bounds the pages one search reads holding a
// joined run; past them, a page holding one stands with the others.
const workspaceSearchJoinedPages = 2000

type workspaceSearchJoinedRead struct {
	read bool
	// titles are the pages holding every joined run as written in titles the
	// title tiers find, and bodies those, holding the runs in a title or a
	// body, whose titles don't hold every keyword.
	titles, bodies map[string]bool
	// rows are the pages in the index's order.
	rows []workspaceSearchTierRow
}

// joinedPages reads once the pages holding every joined run as written: in
// a title, in a chunk of a body (whose words stand in order, with the end of
// the chunk before: workspace_search_chunks.go), or in the body of a page's
// row indexed before chunks; a chunked page's row holds its body's words
// without their order. Every run is whole words, so the index reads no
// word's start, which a common one makes long (2026-10-19).
func (tiers workspaceSearchTiers) joinedPages() (*workspaceSearchJoinedRead, error) {
	joined := tiers.joined
	if joined.read {
		return joined, nil
	}
	joined.read, joined.titles, joined.bodies = true, map[string]bool{}, map[string]bool{}
	runs := tiers.match.Keywords.Joined
	var held map[string]bool
	for index, phrase := range tiers.match.Joined {
		found := map[string]bool{}
		rows := []struct {
			PageID  string `db:"page_id"`
			Chunked bool   `db:"chunked"`
			Title   string `db:"title"`
		}{}
		if err := tiers.app.DB().NewQuery(`
			SELECT [[projection]].[[page_id]], [[projection]].[[chunked]], [[projection]].[[title_folded]] AS [[title]]
			FROM {{leafen_search_pages_fts}} [[fts]]
			CROSS JOIN {{leafen_search_pages}} [[projection]] INDEXED BY [[idx_leafen_search_pages_fts_row]]
				ON [[projection]].[[fts_row]] = [[fts]].[[rowid]]
			WHERE {{leafen_search_pages_fts}} MATCH {:match} LIMIT {:bound}
		`).Bind(dbx.Params{
			"match": workspaceSearchScopedMatch(tiers.workspaceID, "{title body} : "+phrase), "bound": workspaceSearchJoinedPages,
		}).All(&rows); err != nil {
			return nil, err
		}
		for _, row := range rows {
			if !row.Chunked || readWorkspaceSearchText(row.Title, true).holdsPhrase(runs[index]) {
				found[row.PageID] = true
			}
		}
		chunked := []string{}
		chunkScope := workspaceSearchFTSTerm("workspace_token", workspaceSearchChunkScopeToken(tiers.workspaceID))
		if err := tiers.app.DB().NewQuery(`
			SELECT DISTINCT [[chunk]].[[page_id]] FROM {{leafen_search_pages_fts}} [[fts]]
			CROSS JOIN {{leafen_search_chunks}} [[chunk]] ON [[chunk]].[[fts_row]] = [[fts]].[[rowid]]
			WHERE {{leafen_search_pages_fts}} MATCH {:match} LIMIT {:bound}
		`).Bind(dbx.Params{"match": chunkScope + " AND body : " + phrase, "bound": workspaceSearchJoinedPages}).Column(&chunked); err != nil {
			return nil, err
		}
		for _, id := range chunked {
			found[id] = true
		}
		if held == nil {
			held = found
			continue
		}
		for id := range held {
			if !found[id] {
				delete(held, id)
			}
		}
	}
	if len(held) == 0 {
		return joined, nil
	}
	ids := make([]string, 0, len(held))
	for id := range held {
		ids = append(ids, id)
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	if err := tiers.app.DB().NewQuery(`
		SELECT [[fts_row]] AS [[row]], [[page_id]], [[title_folded]] AS [[title]] FROM {{leafen_search_pages}}
		WHERE [[page_id]] IN (SELECT [[value]] FROM json_each({:ids})) AND [[workspace_id]] = {:workspaceId}
		ORDER BY [[fts_row]]
	`).Bind(dbx.Params{"ids": string(encoded), "workspaceId": tiers.workspaceID}).All(&joined.rows); err != nil {
		return nil, err
	}
	// A title holding every keyword is the title tiers' (the keywords' one
	// rule: workspace_search_keywords.go), one starting with them the
	// Starts tier's.
	for _, row := range joined.rows {
		title := readWorkspaceSearchText(row.Title, true)
		holds := true
		for _, run := range runs {
			holds = holds && title.holdsPhrase(run)
		}
		all, _ := tiers.match.Keywords.match(row.Title)
		switch {
		case !all:
			joined.bodies[row.PageID] = true
		case holds && !tiers.titleStarts(row.Title):
			joined.titles[row.PageID] = true
		}
	}
	return joined, nil
}

// readJoined reads up to n of a joined tier's pages after a position, in
// the index's order: the titles holding the runs, or the body tier's pages
// holding them.
func (tiers workspaceSearchTiers) readJoined(tier int64, from workspaceSearchCandidate, n int) ([]workspaceSearchCandidate, error) {
	if len(tiers.match.Joined) == 0 {
		return nil, nil
	}
	joined, err := tiers.joinedPages()
	if err != nil {
		return nil, err
	}
	of := joined.bodies
	if tier == workspaceSearchTierTitleJoined {
		of = joined.titles
	}
	at := sort.Search(len(joined.rows), func(index int) bool { return joined.rows[index].Row > from.Row })
	for at < len(joined.rows) {
		batch := []workspaceSearchTierRow{}
		for ; at < len(joined.rows) && len(batch) < n; at++ {
			if row := joined.rows[at]; of[row.PageID] {
				batch = append(batch, row)
			}
		}
		var held map[string]bool
		if tier == workspaceSearchTierBodyJoined {
			if held, err = tiers.bodyHolds(batch); err != nil {
				return nil, err
			}
		}
		matches := []workspaceSearchCandidate{}
		for _, row := range batch {
			if held == nil || held[row.PageID] {
				matches = append(matches, workspaceSearchCandidate{PageID: row.PageID, Score: tier, Row: row.Row})
			}
		}
		if len(matches) > 0 {
			return matches, nil
		}
	}
	return nil, nil
}

// bodyHolds is which of the pages holding the joined runs, their titles not
// holding every keyword, the body tier finds: each, when the keywords are the
// runs alone, which such a page holds whole; else those whose row matches
// the body tier's keywords, or, their words apart, whose chunks do
// (workspace_search_apart.go).
func (tiers workspaceSearchTiers) bodyHolds(rows []workspaceSearchTierRow) (map[string]bool, error) {
	held := make(map[string]bool, len(rows))
	words := 0
	for _, run := range tiers.match.Keywords.Joined {
		words += len(run)
	}
	for _, row := range rows {
		held[row.PageID] = true
	}
	if len(tiers.match.Keywords.Phrases) == 0 && words == len(tiers.match.Keywords.Words) || len(held) == 0 {
		return held, nil
	}
	ids := make([]string, 0, len(held))
	for id := range held {
		ids = append(ids, id)
	}
	apart, err := tiers.apartPages()
	if err != nil {
		return nil, err
	}
	inBody := []string{}
	if err := tiers.app.DB().NewQuery(`
		SELECT [[projection]].[[page_id]] FROM {{leafen_search_pages_fts}} [[fts]]
		CROSS JOIN {{leafen_search_pages}} [[projection]] INDEXED BY [[idx_leafen_search_pages_fts_row]]
			ON [[projection]].[[fts_row]] = [[fts]].[[rowid]]
		WHERE {{leafen_search_pages_fts}} MATCH {:match}
	`).Bind(dbx.Params{
		"match": workspaceSearchScopedMatch(tiers.workspaceID, workspaceSearchPagesMatch(searchPageTerms(ids, workspaceSearchPageToken),
			"("+tiers.match.Loose+") NOT "+workspaceSearchTitleMatch(tiers.match.Exact))),
	}).Column(&inBody); err != nil {
		return nil, err
	}
	body := make(map[string]bool, len(inBody)+len(apart))
	for _, id := range inBody {
		body[id] = true
	}
	for _, page := range apart {
		body[page.PageID] = true
	}
	for id := range held {
		if !body[id] {
			delete(held, id)
		}
	}
	return held, nil
}
