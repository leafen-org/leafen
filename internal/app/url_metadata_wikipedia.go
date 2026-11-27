package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// A page summary answer is a few kilobytes.
const urlMetadataWikipediaSummaryMaxBytes = 256 * 1024

// urlMetadataWikipediaSummaryURL is the address of the summary Wikipedia
// gives of one of its articles (its REST API's page/summary), or "" for any
// other address: https://<language>.wikipedia.org/wiki/<title>, the
// title whole in one path part.
func urlMetadataWikipediaSummaryURL(pageURL string) string {
	parsed, err := url.Parse(pageURL)
	if err != nil || parsed.Scheme != "https" || !strings.HasSuffix(strings.ToLower(parsed.Hostname()), ".wikipedia.org") {
		return ""
	}
	title, ok := strings.CutPrefix(parsed.Path, "/wiki/")
	if !ok || strings.TrimSpace(title) == "" {
		return ""
	}
	return "https://" + parsed.Host + "/api/rest_v1/page/summary/" + url.PathEscape(title)
}

// readWikipediaSummary is how Wikipedia names and sums up an article: its
// title as Wikipedia writes it, without the site's name after it, and its
// lead's first sentences with the pronunciations and dates in brackets left
// out — what Notion's card and link mention say of an article (seen
// 2026-11-12: the Eiffel Tower's "The Eiffel Tower is a lattice tower on the
// Champ de Mars…", Albert Einstein's "Albert Einstein was a German-born
// theoretical physicist…" and the Korean Eiffel Tower's "에펠탑", not the
// page's "에펠탑 - 위키백과, 우리 모두의 백과사전", word for word that
// summary's, brackets and all where it keeps them). It is read through the
// page's own guarded client, its redirects (a title written another way)
// followed as the page's are; any failure gives nothing, the page's own
// words then standing.
func (fetcher *urlMetadataFetcher) readWikipediaSummary(ctx context.Context, pageURL string) urlMetadataWikipediaSummary {
	none := urlMetadataWikipediaSummary{}
	current, err := normalizeURLMetadataURL(urlMetadataWikipediaSummaryURL(pageURL))
	if err != nil {
		return none
	}
	for redirects := 0; redirects <= urlMetadataMaxRedirects; redirects++ {
		request, requestErr := http.NewRequestWithContext(ctx, http.MethodGet, current, nil)
		if requestErr != nil {
			return none
		}
		request.Header.Set("Accept", "application/json")
		request.Header.Set("User-Agent", "LeafenBot/1.0")
		response, responseErr := fetcher.client.Do(request)
		if responseErr != nil {
			return none
		}
		if response.StatusCode >= http.StatusMultipleChoices && response.StatusCode < http.StatusBadRequest {
			_ = response.Body.Close()
			next, resolveErr := resolveURLMetadataRedirect(current, strings.TrimSpace(response.Header.Get("Location")))
			if resolveErr != nil {
				return none
			}
			current = next
			continue
		}
		raw, readErr := io.ReadAll(io.LimitReader(response.Body, urlMetadataWikipediaSummaryMaxBytes+1))
		_ = response.Body.Close()
		if response.StatusCode != http.StatusOK || readErr != nil || len(raw) > urlMetadataWikipediaSummaryMaxBytes {
			return none
		}
		var summary struct {
			Title  string `json:"title"`
			Titles struct {
				Normalized string `json:"normalized"`
			} `json:"titles"`
			Extract string `json:"extract"`
		}
		if json.Unmarshal(raw, &summary) != nil {
			return none
		}
		return urlMetadataWikipediaSummary{
			Title:   strings.TrimSpace(firstNonempty(summary.Titles.Normalized, summary.Title)),
			Extract: strings.TrimSpace(summary.Extract),
		}
	}
	return none
}

type urlMetadataWikipediaSummary struct {
	Title, Extract string
}
