package app

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// What a connection reaches whose title matches search words. Notion's
// search gives far more than titles that match, in an order of its own: a
// synthetic page's exact title came third of 138 results, the others mostly
// holding none of its words (2026-11-16, two pages of 100). So the scope
// reads a few of Notion's pages and keeps the titles that match, the ones
// the words name first: the title itself, then a title they begin, then one
// holding them, then one holding each word, each kind in Notion's order —
// as Notion's own search puts a page whose title is the words first. Words
// and titles are compared composed (NFC) and without case. When no title
// matches, Notion's own results stay, so a misspelt word still finds
// something.

// notionImportScopeQueryPages is how many of Notion's pages one search
// reads at most before it answers, saying where to go on.
const notionImportScopeQueryPages = 5

// searchRootsMatching is Notion's search for query at any depth, from
// cursor, in the data source version: a database of several data sources is
// one item, named as Notion names it.
func (client notionProviderClient) searchRootsMatching(ctx context.Context, cursor, query string) (notionImportScope, error) {
	items := []notionImportRoot{}
	sources := map[string]int{}
	scope := notionImportScope{}
	for read := 0; read < notionImportScopeQueryPages; read++ {
		payload := map[string]any{"page_size": notionImportProviderPageItems, "query": query}
		if cursor != "" {
			payload["start_cursor"] = cursor
		}
		var page struct {
			Results    []notionScopeSearchResult `json:"results"`
			NextCursor *string                   `json:"next_cursor"`
			HasMore    bool                      `json:"has_more"`
		}
		err := client.requestVersion(ctx, notionProviderDataSourceAPIVersion, http.MethodPost, "/v1/search", payload, &page)
		if err != nil && read > 0 && ctx.Err() != nil {
			// Out of time after a page or more: answer with what was read.
			break
		}
		if err != nil {
			return notionImportScope{}, err
		}
		if len(page.Results) > notionImportProviderPageItems {
			return notionImportScope{}, errors.New(errorCodeNotionImportProviderFailed)
		}
		for _, result := range page.Results {
			if result.inTrash() {
				continue
			}
			switch result.Object {
			case "page":
				items = append(items, notionImportRoot{ID: result.ID, Title: result.title(), Kind: "page"})
			case "data_source":
				key := notionScopeID(result.Parent.DatabaseID)
				if result.Parent.Type != "database_id" || key == "" {
					continue
				}
				if sources[key]++; sources[key] == 1 {
					result.Object = "database"
					items = append(items, notionImportRoot{ID: result.Parent.DatabaseID, Title: result.title(), Kind: "database"})
				}
			}
		}
		scope.HasMore, scope.NextCursor = page.HasMore && page.NextCursor != nil, ""
		if !scope.HasMore {
			break
		}
		cursor = *page.NextCursor
		scope.NextCursor = cursor
	}
	client.nameScopeDatabases(ctx, items, sources)
	scope.Items = notionScopeRanked(items, query)
	return scope, nil
}

// notionScopeRanked keeps the roots whose title matches query, the closest
// first, or all of them in Notion's order when none does.
func notionScopeRanked(items []notionImportRoot, query string) []notionImportRoot {
	fold := func(text string) string { return strings.ToLower(norm.NFC.String(strings.TrimSpace(text))) }
	words := fold(query)
	parts := strings.Fields(words)
	tiers := make([][]notionImportRoot, 4)
	for _, item := range items {
		title := fold(item.Title)
		switch {
		case words == "":
			continue
		case title == words:
			tiers[0] = append(tiers[0], item)
		case strings.HasPrefix(title, words):
			tiers[1] = append(tiers[1], item)
		case strings.Contains(title, words):
			tiers[2] = append(tiers[2], item)
		case notionScopeHoldsEach(title, parts):
			tiers[3] = append(tiers[3], item)
		}
	}
	ranked := []notionImportRoot{}
	for _, tier := range tiers {
		ranked = append(ranked, tier...)
	}
	if len(ranked) == 0 {
		return items
	}
	return ranked
}

func notionScopeHoldsEach(title string, words []string) bool {
	if len(words) < 2 {
		return false
	}
	for _, word := range words {
		if !strings.Contains(title, word) {
			return false
		}
	}
	return true
}
