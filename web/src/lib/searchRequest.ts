// Search opened from somewhere other than its own entries: a wiki's "검색"
// opens it searching within the wiki, as Notion's does (seen 2026-10-31:
// "검색 범위: 〈wiki〉"). The product opens the window; the window takes the
// pages to search within.

export const SEARCH_REQUEST = "leafen:search-request";

export type SearchRequest = { inPages: string[] };

export function requestSearch(request: SearchRequest) {
  window.dispatchEvent(new CustomEvent<SearchRequest>(SEARCH_REQUEST, { detail: request }));
}
