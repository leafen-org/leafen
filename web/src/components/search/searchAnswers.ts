import { searchContentStateMatches, type WorkspaceSearchContentState } from "@/lib/pageArchive";
import { pagePath } from "@/lib/pagePath";
import { isPageVerified } from "@/lib/pageVerification";
import type { Page } from "../../types";
import type { WorkspaceSearchResult } from "../../workspaceSearchWire.generated";
import { searchFiltersActive, searchFiltersMatchPage, type SearchFilters } from "./searchFilterModel";
import { foldNfcText, readSearchKeywords, searchKeywordsHeld } from "./searchModel";

// What the search shows between a press and the server's answer (the press
// boundary, user decision 2026-11-12 "넓힘"): the words, filters and content
// state changed, the results this tab already has for them stand at once —
// the server's answer for the same search, read before, or what the last
// answer found that the new words and filters still hold by the title, the
// line shown or the place — beside the titles found here, and the server's
// answer replaces them when it comes. The list never empties to wait for it.

/** The server's answer for one search: its results and where reading more goes on. */
export type SearchAnswer = {
  /** Which search it answered (searchAnswerKey). */
  key: string;
  results: WorkspaceSearchResult[];
  nextCursor: string | null;
  backlog: boolean;
};

/** Names a search: its content state, its filters and its words. */
export function searchAnswerKey(contentState: WorkspaceSearchContentState, filterKey: string, query: string) {
  return `${contentState}\u0000${filterKey}\u0000${query}`;
}

// The answers this tab read last, the newest last, for the reader and
// workspace they were read in.
const ANSWERS_KEPT = 24;
const answers = new Map<string, SearchAnswer>();

/** The answer this tab read for a search, if it has one. */
export function keptSearchAnswer(scope: string, key: string): SearchAnswer | null {
  return answers.get(`${scope}\u0000${key}`) ?? null;
}

/** Keeps an answer to show at once when the same search is made again. */
export function keepSearchAnswer(scope: string, answer: SearchAnswer) {
  const id = `${scope}\u0000${answer.key}`;
  answers.delete(id);
  answers.set(id, answer);
  while (answers.size > ANSWERS_KEPT) {
    const oldest = answers.keys().next().value;
    if (oldest === undefined) break;
    answers.delete(oldest);
  }
}

type HeldContext = {
  query: string;
  filters: SearchFilters;
  contentState: WorkspaceSearchContentState;
  pagesById: Readonly<Record<string, Page>>;
  archivedIds: ReadonlySet<string>;
};

/**
 * The results of an answer the reader can still open — its page, or the page
 * holding its row or database, still among theirs (a page whose share was
 * taken away leaves at once) — with their pages as the reader has them now;
 * with `query` and `filters` other than the answer's, only those the new
 * words and filters still hold.
 */
export function heldSearchResults(results: readonly WorkspaceSearchResult[], held: HeldContext, sameSearch: boolean) {
  const { query, filters, contentState, pagesById, archivedIds } = held;
  const keywords = readSearchKeywords(query);
  const words = keywords.phrases.length > 0 || keywords.words.length > 0;
  const filtered = searchFiltersActive(filters);
  const now = new Date();
  return results.flatMap((result) => {
    const listed = pagesById[result.page.id];
    if (!listed && !(result.row && pagesById[result.row.containerPageId]) && !(result.database && pagesById[result.database.pageId])) return [];
    const page = listed ?? result.page;
    if (sameSearch) return [listed ? { ...result, page } : result];
    if (!searchContentStateMatches(contentState, archivedIds.has(page.id), isPageVerified(page))) return [];
    if (filtered && !searchFiltersMatchPage(page, filters, pagesById, now)) return [];
    if (words) {
      const title = searchKeywordsHeld(keywords, foldNfcText(page.title.trim()));
      const line = !filters.titleOnly && Boolean(result.preview) && searchKeywordsHeld(keywords, foldNfcText(result.preview), true);
      const place = !filters.titleOnly && !result.row && listed !== undefined &&
        pagePath(page, pagesById).split(" / ").some((above) => searchKeywordsHeld(keywords, foldNfcText(above)));
      if (!title && !line && !place) return [];
      if (filters.titleOnly && result.match !== "title") return [];
    }
    return [listed ? { ...result, page } : result];
  });
}
