package app

import (
	"encoding/json"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// hydrateWorkspaceSearchResults reads the chosen pages, the index first: the
// joins are CROSS JOINs so SQLite keeps that order. Without statistics it
// took the database items as the outer loop and ran the match once for each
// — on 100,000 items a broad search's last page took minutes (2026-10-15).
// workspaceSearchSnippetBodyRunes is the longest body FTS5 picks a result's
// passage in: its snippet slows with the square of a body's matches (100,000
// characters of one repeated word take about 0.1 s, 2,000,000 a minute), so a
// longer body — a page holding long documents — shows the passage around its
// first match instead.
const workspaceSearchSnippetBodyRunes = 100_000

// A page indexed in chunks (workspace_search_chunks.go) shows its passage
// from the first chunk holding a keyword, picked as the whole body's was,
// reading no more of the page; how long the page is comes from its index row
// (2026-10-19: summing its chunks read its whole text for every result). A
// short one's passage the results pick together after (PassageLater).
const workspaceSearchPreviewSQL = `CASE WHEN [[projection]].[[chunked]] THEN COALESCE(CASE
				WHEN [[projection]].[[body_runes]] <= {:snippetBodyRunes} THEN ''
				ELSE (SELECT '…' || substr([[chunk]].[[body]], max(instr([[chunk]].[[body_folded]], {:firstWord}) - 60, 1), 200) || '…'
					FROM {{leafen_search_chunks}} [[chunk]]
					WHERE [[chunk]].[[page_id]] = [[projection]].[[page_id]] AND instr([[chunk]].[[body_folded]], {:firstWord}) > 0
					ORDER BY [[chunk]].[[chunk_no]] LIMIT 1)
			END, '')
			WHEN length([[projection]].[[body]]) <= {:snippetBodyRunes}
				THEN snippet([[leafen_search_pages_fts]], 5, '', '', '…', 32)
			ELSE '…' || substr([[projection]].[[body]], max(instr([[projection]].[[body_folded]], {:firstWord}) - 60, 1), 200) || '…'
			END`

// workspaceSearchPassageLaterSQL says a result is a short page indexed in
// chunks, whose passage workspaceSearchChunkPassages picks.
const workspaceSearchPassageLaterSQL = `([[projection]].[[chunked]] AND [[projection]].[[body_runes]] <= {:snippetBodyRunes})`

// workspaceSearchChunkPassages picks the passages of short pages indexed in
// chunks as a look for each page did — FTS5's snippet of its first chunk
// holding a keyword — in two looks for all of them: their matching chunks,
// then the snippets of each page's first (2026-10-19: a look for each result
// expanded the start of a word over the whole index each time, so a search's
// short pages took twice its long ones). The rows are kept after the match,
// not asked by the index, so the match is read once.
func workspaceSearchChunkPassages(app core.App, chunkScope, anyTerm string, pageIDs []string) (map[string]string, error) {
	passages := map[string]string{}
	// A search without keywords browses: no passage holds them (2026-10-22: a
	// teamspace's pages asked so failed on the empty term).
	if len(pageIDs) == 0 || strings.TrimSpace(anyTerm) == "" {
		return passages, nil
	}
	match := chunkScope + " AND " + workspaceSearchPagesMatch(searchPageTerms(pageIDs, workspaceSearchChunkPageToken), "{body grams} : ("+anyTerm+")")
	matched := []struct {
		PageID string `db:"page_id"`
		No     int    `db:"chunk_no"`
		Row    int64  `db:"fts_row"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[chunk]].[[page_id]], [[chunk]].[[chunk_no]], [[chunk]].[[fts_row]]
		FROM {{leafen_search_pages_fts}} [[fts]]
		CROSS JOIN {{leafen_search_chunks}} [[chunk]] ON [[chunk]].[[fts_row]] = [[fts]].[[rowid]]
		WHERE {{leafen_search_pages_fts}} MATCH {:match}`).Bind(dbx.Params{"match": match}).All(&matched); err != nil {
		return nil, err
	}
	type first struct {
		no  int
		row int64
	}
	firsts := map[string]first{}
	for _, chunk := range matched {
		if seen, found := firsts[chunk.PageID]; !found || chunk.No < seen.no {
			firsts[chunk.PageID] = first{no: chunk.No, row: chunk.Row}
		}
	}
	if len(firsts) == 0 {
		return passages, nil
	}
	pageOf := make(map[int64]string, len(firsts))
	chosen := make([]int64, 0, len(firsts))
	for pageID, chunk := range firsts {
		pageOf[chunk.row] = pageID
		chosen = append(chosen, chunk.row)
	}
	encoded, err := json.Marshal(chosen)
	if err != nil {
		return nil, err
	}
	snippets := []struct {
		Row     int64  `db:"fts_row"`
		Passage string `db:"passage"`
	}{}
	if err := app.DB().NewQuery(`SELECT {{leafen_search_pages_fts}}.[[rowid]] AS [[fts_row]], snippet([[leafen_search_pages_fts]], 5, '', '', '…', 32) AS [[passage]]
		FROM {{leafen_search_pages_fts}}
		WHERE {{leafen_search_pages_fts}} MATCH {:match} AND +{{leafen_search_pages_fts}}.[[rowid]] IN (SELECT [[value]] FROM json_each({:rows}))`).
		Bind(dbx.Params{"match": match, "rows": string(encoded)}).All(&snippets); err != nil {
		return nil, err
	}
	for _, snippet := range snippets {
		if pageID, found := pageOf[snippet.Row]; found {
			passages[pageID] = snippet.Passage
		}
	}
	return passages, nil
}

func hydrateWorkspaceSearchResults(
	app core.App,
	authorization workspaceAuthorization,
	queryFolded string,
	match workspaceSearchMatch,
	candidates []workspaceSearchCandidate,
	tiered bool,
) ([]storedWorkspaceSearchResult, error) {
	// The candidates were chosen already; any keyword finds them again, as a
	// page matching only some keywords is one too (workspace_search_tiers.go).
	matchExpression := match.Any
	chunkScope := workspaceSearchFTSTerm("workspace_token", workspaceSearchChunkScopeToken(authorization.WorkspaceID))
	firstWord := ""
	if words := workspaceSearchPlainWords(queryFolded); len(words) > 0 {
		// An English word's long page shows its passage around a word of its stem too.
		firstWord = searchEnglishStemRoot(words[0])
	}
	if len(candidates) == 0 {
		return []storedWorkspaceSearchResult{}, nil
	}
	// Database items and pages read their results apart, each asking only
	// for its own: a search that found no item asks no item query
	// (2026-10-17: the two queries were half of a search's time).
	candidateIDs := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		candidateIDs = append(candidateIDs, candidate.PageID)
	}
	items, err := loadWorkspaceSearchRowSources(app, candidateIDs)
	if err != nil {
		return nil, err
	}
	// A page whose words are apart is read by its row alone: its row holds
	// its title, its body is in its chunks (workspace_search_apart.go).
	apart, err := workspaceSearchApartPageIDs(app, candidateIDs)
	if err != nil {
		return nil, err
	}
	// So is one found by its title starting a word with the last number typed alone.
	titleStart, err := workspaceSearchTitleStartOnly(app, authorization.WorkspaceID, match, candidateIDs)
	if err != nil {
		return nil, err
	}
	// So do the databases and templates found beside them
	// (workspace_search_databases.go).
	entries, err := loadSearchDatabaseEntries(app, candidateIDs)
	if err != nil {
		return nil, err
	}
	pageTerms, apartPageTerms := make([]string, 0, len(candidates)), []string{}
	itemTerms, apartItemTerms := make([]string, 0, len(items)), []string{}
	entryTerms := map[bool][2][]string{}
	for _, candidate := range candidates {
		term := workspaceSearchFTSTerm("page_token", workspaceSearchPageToken(candidate.PageID))
		_, isItem := items[candidate.PageID]
		// A page found by its path holds no keyword of its own to find it by.
		tier := candidate.tier(tiered)
		byRow := apart[candidate.PageID] || titleStart[candidate.PageID] || tier == workspaceSearchTierPath || tier == workspaceSearchTierSomePath
		if entry, isEntry := entries[candidate.PageID]; isEntry {
			terms, at := entryTerms[entry.Template], 0
			if byRow {
				at = 1
			}
			terms[at] = append(terms[at], term)
			entryTerms[entry.Template] = terms
			continue
		}
		switch {
		case isItem && byRow:
			apartItemTerms = append(apartItemTerms, term)
		case isItem:
			itemTerms = append(itemTerms, term)
		case byRow:
			apartPageTerms = append(apartPageTerms, term)
		default:
			pageTerms = append(pageTerms, term)
		}
	}
	rows := []storedWorkspaceSearchResult{}
	readPages := func(pageTerms []string, matchExpression string) error {
		if len(pageTerms) == 0 {
			return nil
		}
		found := []storedWorkspaceSearchResult{}
		if err := app.DB().NewQuery(`
		SELECT [[page]].[[id]] AS [[page_id]], [[page]].[[workspace]] AS [[workspace_id]],
			[[page]].[[teamspace_id]],
			[[page]].[[title]], [[page]].[[content]], [[page]].[[revision]], [[page]].[[cursor]],
			[[page]].[[parent_id]], [[page]].[[position]], [[page]].[[is_locked]], [[page]].[[locked_by]],
			[[page]].[[in_trash]], [[page]].[[trashed_at]], [[page]].[[archived_at]], [[page]].[[archived_by]],
			[[page]].[[icon]],
			[[page]].[[icon_type]], [[page]].[[cover]], [[page]].[[cover_position]], [[page]].[[font]],
			[[page]].[[small_text]], [[page]].[[full_width]],
			[[page]].[[backlinks_display]], [[page]].[[page_comments_display]], [[page]].[[table_of_contents_display]], [[page]].[[inline_comments_display]], [[page]].[[kind]], [[page]].[[updated]],
			[[page]].[[created]], [[page]].[[created_by]], [[page]].[[last_edited_by]],
			[[page]].[[verified_at]], [[page]].[[verified_by]], [[page]].[[verification_expires_at]],
			` + workspaceSearchPreviewSQL + ` AS [[preview]], [[projection]].[[partial]],
			` + workspaceSearchPassageLaterSQL + ` AS [[passage_later]]
		FROM {{leafen_search_pages_fts}} [[fts]]
		CROSS JOIN {{leafen_search_pages}} [[projection]] INDEXED BY [[idx_leafen_search_pages_fts_row]]
			ON [[projection]].[[fts_row]] = [[fts]].[[rowid]]
		CROSS JOIN {{leafen_pages}} [[page]] ON [[page]].[[id]] = [[projection]].[[page_id]]
		WHERE {{leafen_search_pages_fts}} MATCH {:match}
			AND [[projection]].[[workspace_id]] = {:workspaceId}
			AND [[projection]].[[owner_id]] = {:ownerId}
			AND [[page]].[[owner]] = {:ownerId}
			AND [[page]].[[workspace]] = {:workspaceId}
			AND [[page]].[[in_trash]] = 0
			AND [[page]].[[data_source_id]] = ''
	`).Bind(dbx.Params{
			"match": workspaceSearchScopedMatch(
				authorization.WorkspaceID,
				workspaceSearchPagesMatch(pageTerms, matchExpression),
			),
			"workspaceId": authorization.WorkspaceID, "ownerId": authorization.OwnerID,
			"snippetBodyRunes": workspaceSearchSnippetBodyRunes, "firstWord": firstWord,
			"chunkScope": chunkScope, "anyTerm": match.Any,
		}).All(&found); err != nil {
			return err
		}
		rows = append(rows, found...)
		return nil
	}
	if err := readPages(pageTerms, matchExpression); err != nil {
		return nil, err
	}
	if err := readPages(apartPageTerms, ""); err != nil {
		return nil, err
	}
	// Database items read their title from the index and show their icon.
	itemRows := []storedWorkspaceSearchResult{}
	readItems := func(itemTerms []string, matchExpression string) error {
		if len(itemTerms) == 0 {
			return nil
		}
		found := []storedWorkspaceSearchResult{}
		if err := app.DB().NewQuery(`
		SELECT [[projection]].[[page_id]], [[row_page]].[[workspace_id]], [[projection]].[[title]], [[projection]].[[title_folded]],
			[[row_page]].[[container_page_id]] AS [[parent_id]],
			[[row_page]].[[container_page_id]] AS [[row_container_page_id]],
			[[row_page]].[[data_source_id]] AS [[row_data_source_id]],
			[[row_record]].[[icon]], [[row_record]].[[icon_type]],
			strftime('%Y-%m-%d %H:%M:%fZ', [[row_page]].[[updated]] / 1000.0, 'unixepoch') AS [[updated]],
			strftime('%Y-%m-%d %H:%M:%fZ', [[row_page]].[[created]] / 1000.0, 'unixepoch') AS [[created]],
			[[row_record]].[[created_by]], [[row_record]].[[last_edited_by]],
			` + workspaceSearchPreviewSQL + ` AS [[preview]], [[projection]].[[partial]],
			` + workspaceSearchPassageLaterSQL + ` AS [[passage_later]]
		FROM {{leafen_search_pages_fts}} [[fts]]
		CROSS JOIN {{leafen_search_pages}} [[projection]] INDEXED BY [[idx_leafen_search_pages_fts_row]]
			ON [[projection]].[[fts_row]] = [[fts]].[[rowid]]
		CROSS JOIN {{leafen_database_row_pages}} [[row_page]] ON [[row_page]].[[row_id]] = [[projection]].[[page_id]]
		CROSS JOIN {{leafen_pages}} [[row_record]] ON [[row_record]].[[id]] = [[row_page]].[[row_id]]
		WHERE {{leafen_search_pages_fts}} MATCH {:match}
			AND [[projection]].[[workspace_id]] = {:workspaceId}
			AND [[projection]].[[owner_id]] = {:ownerId}
			AND [[row_page]].[[owner]] = {:ownerId}
			AND [[row_page]].[[workspace_id]] = {:workspaceId}
			AND ` + databaseRowNotTrashedSQL("[[row_page]].[[row_id]]") + `
	`).Bind(dbx.Params{
			"match": workspaceSearchScopedMatch(
				authorization.WorkspaceID,
				workspaceSearchPagesMatch(itemTerms, matchExpression),
			),
			"workspaceId": authorization.WorkspaceID, "ownerId": authorization.OwnerID,
			"snippetBodyRunes": workspaceSearchSnippetBodyRunes, "firstWord": firstWord,
			"chunkScope": chunkScope, "anyTerm": match.Any,
		}).All(&found); err != nil {
			return err
		}
		itemRows = append(itemRows, found...)
		return nil
	}
	if err := readItems(itemTerms, matchExpression); err != nil {
		return nil, err
	}
	if err := readItems(apartItemTerms, ""); err != nil {
		return nil, err
	}
	readEntries := func(query string, terms []string, matchExpression string) error {
		if len(terms) == 0 {
			return nil
		}
		found := []storedWorkspaceSearchResult{}
		if err := app.DB().NewQuery(query).Bind(dbx.Params{
			"match": workspaceSearchScopedMatch(
				authorization.WorkspaceID,
				workspaceSearchPagesMatch(terms, matchExpression),
			),
			"workspaceId": authorization.WorkspaceID, "ownerId": authorization.OwnerID,
			"snippetBodyRunes": workspaceSearchSnippetBodyRunes, "firstWord": firstWord,
			"chunkScope": chunkScope, "anyTerm": match.Any,
		}).All(&found); err != nil {
			return err
		}
		itemRows = append(itemRows, found...)
		return nil
	}
	for template, query := range map[bool]string{false: workspaceSearchInlineDatabaseResultsSQL, true: workspaceSearchTemplateResultsSQL} {
		if err := readEntries(query, entryTerms[template][0], matchExpression); err != nil {
			return nil, err
		}
		if err := readEntries(query, entryTerms[template][1], ""); err != nil {
			return nil, err
		}
	}
	rows = append(rows, itemRows...)
	later := []string{}
	for _, row := range rows {
		if row.PassageLater {
			later = append(later, row.PageID)
		}
	}
	passages, err := workspaceSearchChunkPassages(app, chunkScope, match.Any, later)
	if err != nil {
		return nil, err
	}
	for index := range rows {
		if passage, found := passages[rows[index].PageID]; found && rows[index].PassageLater {
			rows[index].Preview = passage
		}
	}
	byPage := make(map[string]storedWorkspaceSearchResult, len(rows))
	for _, row := range rows {
		byPage[row.PageID] = row
	}
	ordered := make([]storedWorkspaceSearchResult, 0, len(candidates))
	for _, candidate := range candidates {
		if row, ok := byPage[candidate.PageID]; ok {
			row.Score, row.found = candidate.Score, candidate.tier(tiered)
			ordered = append(ordered, row)
		}
	}
	return ordered, nil
}

// workspaceSearchTitleStartOnly is which chosen pages hold no keyword whole,
// found by a title starting a word with the last number typed alone
// (workspaceSearchKeywords.TitleStart): they are read by their rows, as
// asking the index for the start of the word again reads every word of that
// start (2026-11-24: for one digit, most of a search on a NAS-class server).
func workspaceSearchTitleStartOnly(app core.App, workspaceID string, match workspaceSearchMatch, pageIDs []string) (map[string]bool, error) {
	only := map[string]bool{}
	if match.Keywords.TitleStart == "" || match.Any == "" || len(pageIDs) == 0 {
		return only, nil
	}
	held := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[projection]].[[page_id]] FROM {{leafen_search_pages_fts}} [[fts]]
		CROSS JOIN {{leafen_search_pages}} [[projection]] INDEXED BY [[idx_leafen_search_pages_fts_row]]
			ON [[projection]].[[fts_row]] = [[fts]].[[rowid]]
		WHERE {{leafen_search_pages_fts}} MATCH {:match}
	`).Bind(dbx.Params{
		"match": workspaceSearchScopedMatch(workspaceID, workspaceSearchPagesMatch(searchPageTerms(pageIDs, workspaceSearchPageToken), match.Any)),
	}).Column(&held); err != nil {
		return nil, err
	}
	holds := make(map[string]bool, len(held))
	for _, id := range held {
		holds[id] = true
	}
	for _, id := range pageIDs {
		if !holds[id] {
			only[id] = true
		}
	}
	return only, nil
}
