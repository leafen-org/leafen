package app

import (
	"encoding/hex"
	"encoding/json"
	"sort"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A page with many words keeps its title alone in its index row, its body's
// words in its chunks only (2026-10-19: a page's row held every word of the
// page and was written whole again for every change — 273 KB for a page
// with many one-off words). A search's terms are its keywords each, and a
// keyword's word or pairs always stand in one chunk, so such a page matches
// when each term is in its title or one of its chunks: asked a term at a
// time of these few pages, which a search then ranks with the others.

// searchPageWordsApartBytes is the most of a page's body words its row
// holds; a page with more keeps them in its chunks alone.
const searchPageWordsApartBytes = 32 << 10

// workspaceSearchApartBatch is how many such pages one question names.
const workspaceSearchApartBatch = 200

type workspaceSearchApartPage struct {
	PageID string `db:"page_id"`
	Row    int64  `db:"fts_row"`
	Title  string `db:"title_folded"`
}

// workspaceSearchApartMatches is, of a workspace's pages whose words are
// apart, those whose title or chunks hold every term, in their rows' order;
// with notTitle, less those whose row matches it.
func workspaceSearchApartMatches(app core.App, workspaceID string, terms []string, notTitle string) ([]workspaceSearchApartPage, error) {
	if len(terms) == 0 {
		return nil, nil
	}
	pages := []workspaceSearchApartPage{}
	if err := app.DB().NewQuery(`SELECT [[page_id]], [[fts_row]], [[title_folded]] FROM {{leafen_search_pages}}
		INDEXED BY [[idx_leafen_search_pages_words_apart]]
		WHERE [[workspace_id]] = {:workspaceId} AND [[words_apart]] = 1`).Bind(dbx.Params{"workspaceId": workspaceID}).All(&pages); err != nil {
		return nil, err
	}
	// A row's page is in its token, a page's row ("p") or a chunk's ("c"),
	// each its page's own: one question a term, naming the pages' tokens.
	pagesOf := func(match string) (map[string]bool, error) {
		tokens := []string{}
		if err := app.DB().NewQuery(`SELECT [[page_token]] FROM {{leafen_search_pages_fts}} WHERE {{leafen_search_pages_fts}} MATCH {:match}`).
			Bind(dbx.Params{"match": match}).Column(&tokens); err != nil {
			return nil, err
		}
		found := make(map[string]bool, len(tokens))
		for _, token := range tokens {
			if len(token) < 2 {
				continue
			}
			if id, err := hex.DecodeString(token[1:]); err == nil {
				found[string(id)] = true
			}
		}
		return found, nil
	}
	matched := []workspaceSearchApartPage{}
	for start := 0; start < len(pages); start += workspaceSearchApartBatch {
		batch := pages[start:min(start+workspaceSearchApartBatch, len(pages))]
		batchIDs := make([]string, 0, len(batch))
		alive := map[string]bool{}
		for _, page := range batch {
			batchIDs = append(batchIDs, page.PageID)
			alive[page.PageID] = true
		}
		rows := append(searchPageTerms(batchIDs, workspaceSearchPageToken), searchPageTerms(batchIDs, workspaceSearchChunkPageToken)...)
		for _, term := range terms {
			found, err := pagesOf(workspaceSearchPagesMatch(rows, term))
			if err != nil {
				return nil, err
			}
			for id := range alive {
				if !found[id] {
					delete(alive, id)
				}
			}
			if len(alive) == 0 {
				break
			}
		}
		if len(alive) > 0 && notTitle != "" {
			titled, err := pagesOf(workspaceSearchPagesMatch(searchPageTerms(batchIDs, workspaceSearchPageToken), notTitle))
			if err != nil {
				return nil, err
			}
			for id := range titled {
				delete(alive, id)
			}
		}
		for _, page := range batch {
			if alive[page.PageID] {
				matched = append(matched, page)
			}
		}
	}
	sort.Slice(matched, func(left, right int) bool { return matched[left].Row < matched[right].Row })
	return matched, nil
}

// workspaceSearchApartPageIDs is which of some pages keep their words apart.
func workspaceSearchApartPageIDs(app core.App, pageIDs []string) (map[string]bool, error) {
	apart := map[string]bool{}
	if len(pageIDs) == 0 {
		return apart, nil
	}
	encoded, err := json.Marshal(pageIDs)
	if err != nil {
		return nil, err
	}
	found := []string{}
	if err := app.DB().NewQuery(`SELECT [[page_id]] FROM {{leafen_search_pages}}
		WHERE [[words_apart]] = 1 AND [[page_id]] IN (SELECT [[value]] FROM json_each({:ids}))`).
		Bind(dbx.Params{"ids": string(encoded)}).Column(&found); err != nil {
		return nil, err
	}
	for _, id := range found {
		apart[id] = true
	}
	return apart, nil
}
