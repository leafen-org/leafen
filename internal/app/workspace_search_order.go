package app

import (
	"encoding/json"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// workspaceSearchMatch is a search's keywords (workspace_search_keywords.go)
// as the index asks them (workspace_search_chunks.go): Exact over a title,
// Loose over a page's row, whose body words and pairs have no order, and
// Ordered, the terms whose words or pairs must stand in order, checked in the
// title or the chunks of a page Loose found.
type workspaceSearchMatch struct {
	Keywords workspaceSearchKeywords
	Exact    string
	Loose    string
	Ordered  []string
	// Any is Loose's terms each on its own, and the parts of the words of
	// letters and digits, which a result's passage is picked around.
	Any string
	// Some is the pages holding every phrase and some of the other words,
	// with those holding all; none when a page can't match only some. A
	// word before the last typed is held whole there (searchWordWholeTerm).
	// SomeTitle is those whose titles hold them.
	Some      string
	SomeTitle string
	// SomeTitleTerms and SomeLooseTerms are TitleTerms and LooseTerms as the
	// pages matching some hold them, a word before the last typed whole: a
	// page under a title holding one is found by its path (workspace_search_path.go).
	SomeTitleTerms []string
	SomeLooseTerms []string
	// LooseTerms are Loose's terms, a keyword each, which a page whose words
	// are apart is asked one at a time (workspace_search_apart.go).
	LooseTerms []string
	// TitleTerms are Exact's terms, a keyword each in LooseTerms' order, the
	// phrases first: a title holding one, the pages under it are found by
	// their path holding it (workspace_search_path.go).
	TitleTerms []string
	Phrases    int
	// Joined are the keywords' joined runs (workspaceSearchKeywords.Joined),
	// each as a phrase of whole words in order (workspace_search_joined.go).
	Joined []string
	// Every is the pages holding every keyword: Loose, or a title holding
	// them, the last number typed as the start of one of its words
	// (workspaceSearchKeywords.TitleStart), which Loose doesn't find.
	Every string
}

// workspaceSearchTitleColumns is where the index holds a page's title: its
// words (title) and what the index adds for them (grams — a page's row holds
// its title's, its body's being in its chunks: workspace_search_projection.go),
// the pairs of a paired script among them. A title is matched there, so a word
// of such a script inside a longer run of the title ("かな" in "漢字かな") is
// the title's, as the keywords' own check holds it (holdsWord) and as Notion's
// search lists it, above the pages holding it in their bodies (seen 2026-11-16).
const workspaceSearchTitleColumns = "{title grams}"

// workspaceSearchTitleMatch is a match expression asked of titles alone.
func workspaceSearchTitleMatch(expression string) string {
	return workspaceSearchTitleColumns + " : (" + expression + ")"
}

// workspaceSearchMatchFor reads keywords: every word as the start of a word,
// and a phrase in double quotes as exactly those words in that order, as
// Notion's search does. A phrase is ordered, even of one word: it is the
// words of a title and a body, not the parts and pairs the index adds
// (workspace_search_language.go), which a page's row holds with its body's
// words — Notion's finds no "Tarvo4291" by "tarvo" in quotes (seen
// 2026-11-11).
func workspaceSearchMatchFor(query string) workspaceSearchMatch {
	keywords := readWorkspaceSearchKeywords(query)
	match := workspaceSearchMatch{Keywords: keywords}
	exact, phrases, words, ordered := []string{}, []string{}, []string{}, []string{}
	someTitle, someLoose := []string{}, []string{}
	for _, phrase := range keywords.Phrases {
		exact = append(exact, `"`+strings.Join(phrase, " ")+`"`)
		each := make([]string, 0, len(phrase))
		for _, word := range phrase {
			each = append(each, `"`+word+`"`)
		}
		phrases = append(phrases, "{title body} : ("+strings.Join(each, " AND ")+")")
		ordered = append(ordered, `"`+strings.Join(phrase, " ")+`"`)
		someTitle, someLoose = append(someTitle, exact[len(exact)-1]), append(someLoose, phrases[len(phrases)-1])
	}
	for index, word := range keywords.Words {
		term, looseTerm := searchWordTerm(word), searchWordLooseTerm(word)
		exact, words = append(exact, term), append(words, looseTerm)
		if looseTerm != term {
			ordered = append(ordered, term)
		}
		// A title holds the last number typed as the start of a word too.
		if keywords.titleStarts(index) {
			exact[len(exact)-1] = searchWordStartTerm(word)
		}
		if keywords.someWhole(index) {
			term = searchWordWholeTerm(word)
			someTitle, someLoose = append(someTitle, term), append(someLoose, term)
		} else {
			someTitle, someLoose = append(someTitle, exact[len(exact)-1]), append(someLoose, looseTerm)
		}
	}
	parts := []string{}
	for _, part := range keywords.wordParts() {
		parts = append(parts, `"`+part+`"`)
	}
	loose := append(append([]string{}, phrases...), words...)
	anyWord := append(append([]string{}, words...), parts...)
	match.Exact, match.Loose, match.Ordered = strings.Join(exact, " AND "), strings.Join(loose, " AND "), ordered
	match.Every = match.Loose
	if keywords.TitleStart != "" {
		match.Every = "(" + match.Loose + ") OR " + workspaceSearchTitleMatch(match.Exact)
	}
	match.TitleTerms, match.Phrases = exact, len(phrases)
	match.Joined = keywords.joinedPhrases()
	match.Any, match.LooseTerms = strings.Join(append(append([]string{}, phrases...), anyWord...), " OR "), loose
	if keywords.matchesSome() {
		match.SomeTitleTerms, match.SomeLooseTerms = someTitle, someLoose
		match.Some = "(" + strings.Join(append(append([]string{}, someLoose[len(phrases):]...), parts...), " OR ") + ")"
		// A title is matched as the title tier matches it, its words whole
		// or as their start, a word before the last typed whole.
		titleWords := append(append([]string{}, someTitle[len(phrases):]...), parts...)
		match.SomeTitle = "(" + strings.Join(titleWords, " OR ") + ")"
		if len(phrases) > 0 {
			match.Some = strings.Join(phrases, " AND ") + " AND " + match.Some
			match.SomeTitle = strings.Join(exact[:len(phrases)], " AND ") + " AND " + match.SomeTitle
		}
		match.SomeTitle = workspaceSearchTitleMatch(match.SomeTitle)
	}
	return match
}

// workspaceSearchOrderedPhrase is whether an ordered term is a phrase's,
// which a page matching only some keywords holds too; a word's pairs are
// in parentheses (searchWordTerm).
func workspaceSearchOrderedPhrase(term string) bool {
	return strings.HasPrefix(term, `"`)
}

// searchWordLooseTerm is searchWordTerm with a word's pairs in any order.
func searchWordLooseTerm(word string) string {
	term := searchWordTerm(word)
	clusters := searchClusters(word)
	if len(clusters) < 3 || !searchPairedWord(word) {
		return term
	}
	pairs := make([]string, 0, len(clusters)-1)
	for index := 0; index+1 < len(clusters); index++ {
		pairs = append(pairs, `"`+clusters[index]+clusters[index+1]+`"`)
	}
	return `("` + word + `"* OR (` + strings.Join(pairs, " AND ") + `))`
}

// workspaceSearchOrderTogetherRunes is the longest page the order check asks
// with others in one look, about 16 chunks: its matching chunks all come
// back. A longer page is asked alone, the look stopping at its first.
const workspaceSearchOrderTogetherRunes = 32 << 10

// workspaceSearchKeepOrdered keeps, of matches Loose found, the pages whose
// ordered terms stand in order: in the title or the chunks of a page, or in
// the row of one indexed before chunks. A tiered search's title tiers
// already matched the title exactly; a page matching only some keywords
// holds every phrase in order, whichever words it holds.
func workspaceSearchKeepOrdered(
	app core.App, workspaceID string, ordered []string, candidates []workspaceSearchCandidate, tiered bool,
) ([]workspaceSearchCandidate, error) {
	if len(ordered) == 0 || len(candidates) == 0 {
		return candidates, nil
	}
	phrases := 0
	for _, term := range ordered {
		if workspaceSearchOrderedPhrase(term) {
			phrases++
		}
	}
	// A page found by its path holds no keyword of its own to ask.
	asks := func(candidate workspaceSearchCandidate) bool {
		switch tier := candidate.tier(tiered); {
		case tier == workspaceSearchTierPath || tier == workspaceSearchTierSomePath:
			return false
		case workspaceSearchSomeTier(tier):
			return phrases > 0
		default:
			return !tiered || tier == workspaceSearchTierBody || tier == workspaceSearchTierBodyJoined
		}
	}
	asked := []string{}
	for _, candidate := range candidates {
		if asks(candidate) {
			asked = append(asked, candidate.PageID)
		}
	}
	if len(asked) == 0 {
		return candidates, nil
	}
	encoded, err := json.Marshal(asked)
	if err != nil {
		return nil, err
	}
	rows := []struct {
		PageID    string `db:"page_id"`
		Chunked   bool   `db:"chunked"`
		BodyRunes int    `db:"body_runes"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[page_id]], [[chunked]], [[body_runes]] FROM {{leafen_search_pages}} WHERE [[page_id]] IN (SELECT [[value]] FROM json_each({:ids}))`).
		Bind(dbx.Params{"ids": string(encoded)}).All(&rows); err != nil {
		return nil, err
	}
	// A page of a few chunks is asked with the others like it at once; a
	// longer one alone, the look stopping at its first chunk.
	chunkedPages, shortPages, longPages, wholePages := []string{}, []string{}, []string{}, []string{}
	for _, row := range rows {
		switch {
		case !row.Chunked:
			wholePages = append(wholePages, row.PageID)
		case row.BodyRunes > workspaceSearchOrderTogetherRunes:
			chunkedPages, longPages = append(chunkedPages, row.PageID), append(longPages, row.PageID)
		default:
			chunkedPages, shortPages = append(chunkedPages, row.PageID), append(shortPages, row.PageID)
		}
	}
	holds, holdsPhrases := map[string]int{}, map[string]int{}
	for _, term := range ordered {
		found := map[string]bool{}
		read := func(query string, match string) error {
			ids := []string{}
			if err := app.DB().NewQuery(query).Bind(dbx.Params{"match": match}).Column(&ids); err != nil {
				return err
			}
			for _, id := range ids {
				found[id] = true
			}
			return nil
		}
		// A phrase is whole words: the title and the body, not the parts
		// and pairs the index adds beside them (workspace_search_language.go).
		wholeColumns, titleColumns, bodyColumns := "", "{title grams}", "{body grams}"
		if workspaceSearchOrderedPhrase(term) {
			wholeColumns, titleColumns, bodyColumns = "{title body}", "title", "body"
		}
		const pageRows = `SELECT [[projection]].[[page_id]] FROM {{leafen_search_pages_fts}} [[fts]]
			CROSS JOIN {{leafen_search_pages}} [[projection]] INDEXED BY [[idx_leafen_search_pages_fts_row]]
				ON [[projection]].[[fts_row]] = [[fts]].[[rowid]]
			WHERE {{leafen_search_pages_fts}} MATCH {:match}`
		if len(wholePages) > 0 {
			if err := read(pageRows, workspaceSearchScopedMatch(workspaceID, workspaceSearchPagesMatch(searchPageTerms(wholePages, workspaceSearchPageToken), wholeColumns+" : ("+term+")"))); err != nil {
				return nil, err
			}
		}
		if len(chunkedPages) > 0 {
			if err := read(pageRows, workspaceSearchScopedMatch(workspaceID,
				workspaceSearchPagesMatch(searchPageTerms(chunkedPages, workspaceSearchPageToken), titleColumns+" : ("+term+")"))); err != nil {
				return nil, err
			}
			// Whether a page has a chunk holding the term (2026-10-19: one
			// look over every page took back every matching chunk, 468 on a
			// page typed into for hours): the short pages together, a long
			// one alone, stopping at its first. A chunk's row in the index
			// goes with the chunk, so the index alone answers; chunks are
			// indexed in pairs, so the pairs alone ask it, without the start
			// of the word every look would expand.
			chunkScope := workspaceSearchFTSTerm("workspace_token", workspaceSearchChunkScopeToken(workspaceID))
			pairs := searchOrderedPairsTerm(term)
			if len(shortPages) > 0 {
				if err := read(`SELECT [[chunk]].[[page_id]] FROM {{leafen_search_pages_fts}} [[fts]]
					CROSS JOIN {{leafen_search_chunks}} [[chunk]] ON [[chunk]].[[fts_row]] = [[fts]].[[rowid]]
					WHERE {{leafen_search_pages_fts}} MATCH {:match}`,
					chunkScope+" AND ("+workspaceSearchPagesMatch(searchPageTerms(shortPages, workspaceSearchChunkPageToken), bodyColumns+" : ("+pairs+")")+")"); err != nil {
					return nil, err
				}
			}
			for _, pageID := range longPages {
				if found[pageID] {
					continue
				}
				holds := 0
				match := chunkScope + " AND (" + workspaceSearchPagesMatch(searchPageTerms([]string{pageID}, workspaceSearchChunkPageToken), bodyColumns+" : ("+pairs+")") + ")"
				if err := app.DB().NewQuery(`SELECT EXISTS (SELECT 1 FROM {{leafen_search_pages_fts}} WHERE {{leafen_search_pages_fts}} MATCH {:match} LIMIT 1)`).
					Bind(dbx.Params{"match": match}).Row(&holds); err != nil {
					return nil, err
				}
				if holds == 1 {
					found[pageID] = true
				}
			}
		}
		for id := range found {
			holds[id]++
			if workspaceSearchOrderedPhrase(term) {
				holdsPhrases[id]++
			}
		}
	}
	kept := make([]workspaceSearchCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		switch {
		case !asks(candidate):
		case workspaceSearchSomeTier(candidate.tier(tiered)):
			if holdsPhrases[candidate.PageID] != phrases {
				continue
			}
		case holds[candidate.PageID] != len(ordered):
			continue
		}
		kept = append(kept, candidate)
	}
	return kept, nil
}

func searchPageTerms(pageIDs []string, token func(string) string) []string {
	terms := make([]string, 0, len(pageIDs))
	for _, id := range pageIDs {
		terms = append(terms, workspaceSearchFTSTerm("page_token", token(id)))
	}
	return terms
}
