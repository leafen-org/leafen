import { pagePath } from "@/lib/pagePath";
import type { Page } from "../../types";
import type { WorkspaceSearchDatabaseSource, WorkspaceSearchResult } from "../../workspaceSearchWire.generated";
import { parseStored, personValue, storePersonValue } from "@/lib/browserStorage";
import { serverScopedName } from "@/lib/serverOrigin";
import { pagesByIdOf } from "@/lib/pageIndex";
import { foldSearchText, SEARCH_PAIRED } from "@/lib/textMatch";
import { englishStemEqual } from "@/lib/englishStem";

export const SEARCH_RESULT_LIMIT = 12;
// The results a search's head counts, as Notion's: past them it reads
// "검색 결과(100개 이상)" (seen 2026-10-31, 2026-11-14); a later page of
// results reads this many at once, the most the server gives.
export const SEARCH_COUNT_LIMIT = 100;
export const SEARCH_MORE_LIMIT = 50;
// The server is asked from the first letter, as Notion's search asks: "행"
// finds a database's row titled "행 @…" (seen 2026-11-16), which only the
// server reads, a row being outside the page list the titles here are
// found in.
export const REMOTE_SEARCH_MIN_LENGTH = 1;
export const REMOTE_SEARCH_DEBOUNCE_MS = 220;
const RECENT_SEARCH_LIMIT = 6;
const RECENT_SEARCH_KEY = "leafen:v2:quick-find:recent-searches";

export type SearchHit = WorkspaceSearchResult & {
  score: number;
  /** The one of a database's several sources its row lists (searchHitRows). */
  source?: WorkspaceSearchDatabaseSource;
  /** One of the person's recently opened pages leading the results (searchLead.ts). */
  lead?: true;
};

/**
 * A database of several sources found is listed once for each source, as
 * Notion's search lists it (seen 2026-11-11: "A 및 B / 🗄 A", "A 및 B / 🗄
 * B"), then by its own name once it has one ("X 데이터베이스" after "X / 🗄
 * A" and "X / 🗄 B"); any other result is one row. A search by keywords
 * lists only the names they find (searchResultRows).
 */
export function searchHitRows(hit: SearchHit): SearchHit[] {
  const sources = hit.database?.sources ?? [];
  if (sources.length < 2) return [hit];
  const rows: SearchHit[] = sources.map((source) => ({ ...hit, source }));
  return hit.database?.named ? [...rows, hit] : rows;
}

export type SearchSection = {
  label: string;
  detail?: string;
  hits: SearchHit[];
  /** One line a row — the title, then "— its place" — as the visited pages Notion lists before a search. */
  compact?: boolean;
};

export type VisitSectionKey = "today" | "yesterday" | "week" | "month" | "older";

/** Which of Notion's empty-search groups a visit falls in, by calendar day. */
export function visitSectionKey(visitedAt: string, now: Date): VisitSectionKey {
  const at = new Date(visitedAt);
  const startOf = (date: Date) => new Date(date.getFullYear(), date.getMonth(), date.getDate()).getTime();
  const days = Math.round((startOf(now) - startOf(at)) / 86_400_000);
  if (days <= 0) return "today";
  if (days === 1) return "yesterday";
  if (days < 7) return "week";
  if (days < 30) return "month";
  return "older";
}

export function foldNfcText(value: string) {
  return value.normalize("NFC").toLowerCase().normalize("NFC");
}

export function normalizedSearchQuery(value: string) {
  return value.trim().replace(/\s+/g, " ");
}

/** The keywords without the quotes around exact phrases, as a title holds them. */
export function searchTextQuery(value: string) {
  return normalizedSearchQuery(value.replace(/["\u201c\u201d]/gu, " "));
}

/**
 * Keywords as the server reads them (workspace_search_keywords.go): quoted
 * phrases and the other words; titleStart the last word typed when it is a
 * number's, which a title holds as the start of a word too (searchNumberWord);
 * endsInWord whether the last of the words is the last typed, no phrase
 * after it; whole that the words are held whole (searchKeywordWord).
 */
export type SearchKeywords = { phrases: string[][]; words: string[]; titleStart?: string; endsInWord?: boolean; whole?: boolean };

export const SEARCH_WORD = /[\p{L}\p{N}\p{M}]+/gu;
// Scripts written without spaces, a word of which the server finds anywhere in a run of it.
export { SEARCH_PAIRED };

// A text's words as search compares them, accents aside (foldSearchText).
function searchWords(text: string) {
  return foldSearchText(text).match(SEARCH_WORD) ?? [];
}

/** A word of letters and digits cut where they meet ("tarvo4291": tarvo, 4291); none for a word of either alone. */
export function searchWordParts(word: string) {
  const parts = word.match(/\p{N}+|[\p{L}\p{M}]+/gu) ?? [];
  return parts.length > 1 ? parts : [];
}

/** Reads keywords: a phrase in double quotes, and the other words; a quote left open quotes nothing. */
export function readSearchKeywords(query: string): SearchKeywords {
  const keywords: SearchKeywords = { phrases: [], words: [] };
  const parts = query.split(/["\u201c\u201d]/u);
  parts.forEach((part, index) => {
    const words = searchWords(part);
    if (!words.length) return;
    keywords.titleStart = undefined;
    keywords.endsInWord = !(index % 2 === 1 && index < parts.length - 1);
    if (!keywords.endsInWord) keywords.phrases.push(words);
    else {
      keywords.words.push(...words);
      const last = words[words.length - 1];
      if (searchNumberWord(last)) keywords.titleStart = last;
    }
  });
  return keywords;
}

/**
 * Whether a keyword is digits alone, a number's word, which search finds
 * whole ("11/23" no "110/230", "8123" no "81234"), the last typed in a title
 * as the start of a word too ("5123" the title "w nm 51234 mid"), as Notion's
 * search finds them (seen 2026-11-24, two tabs; the server's searchNumberWord).
 */
export function searchNumberWord(word: string) {
  return /^\p{Nd}+$/u.test(word);
}

/**
 * Whether a text holds every keyword as the server's search judges a title,
 * or, a body's line, a body: each phrase's words whole and in order, each
 * other word the start of a word or of a part of one or, an English one, a
 * word of its stem ("runs" holds "running"; englishStemEqual), a word of a
 * script written without spaces anywhere in a word, a number's word or part
 * whole — the title start, in a title, its start (searchNumberWord). What the
 * server finds besides comes with its results.
 */
export function searchKeywordsHeld(keywords: SearchKeywords, text: string, inBody = false) {
  if (!keywords.phrases.length && !keywords.words.length) return false;
  const words = searchWords(text);
  const starts = words.flatMap((word) => [word, ...searchWordParts(word)]);
  const holdsPhrase = (phrase: string[]) => words.some((_, start) =>
    phrase.every((word, offset) => words[start + offset] === word));
  const holdsWord = (word: string, index: number) => {
    if (keywords.whole) return starts.includes(word) || words.some((held) => englishStemEqual(held, word));
    if (searchNumberWord(word) && (inBody || word !== keywords.titleStart || index !== keywords.words.length - 1)) {
      return starts.includes(word);
    }
    return SEARCH_PAIRED.test(word)
      ? words.some((held) => held.includes(word))
      : starts.some((held) => held.startsWith(word)) || words.some((held) => englishStemEqual(held, word));
  };
  return keywords.phrases.every(holdsPhrase) && keywords.words.every(holdsWord);
}

/**
 * One of the keywords' words alone, as a text holding some of them holds it:
 * the title start the last, and a word before the last typed whole, as the
 * server's search judges it (someWhole: "vorpalt zzqk" holds no "vorpalton";
 * a word of a script written without spaces anywhere in a run, as ever).
 */
export function searchKeywordWord(keywords: SearchKeywords, index: number): SearchKeywords {
  const last = index === keywords.words.length - 1, word = keywords.words[index];
  const whole = !(last && keywords.endsInWord !== false) && !SEARCH_PAIRED.test(word);
  return { phrases: [], words: [word], titleStart: last ? keywords.titleStart : undefined, ...(whole ? { whole } : {}) };
}

// A person's own recent searches, as Notion keeps them per person: another
// account on the same browser has its own.
export function readRecentSearches() {
  try {
    const parsed = parseStored(personValue(RECENT_SEARCH_KEY, { previous: [serverScopedName(RECENT_SEARCH_KEY)] }));
    if (!Array.isArray(parsed)) return [];
    const seen = new Set<string>();
    const searches: string[] = [];
    for (const item of parsed) {
      const query = normalizedSearchQuery(String(item));
      const key = foldNfcText(query);
      if (!query || seen.has(key)) continue;
      seen.add(key);
      searches.push(query);
      if (searches.length >= RECENT_SEARCH_LIMIT) break;
    }
    return searches;
  } catch {
    return [];
  }
}

export function rememberRecentSearch(current: string[], value: string) {
  const query = normalizedSearchQuery(value);
  if (!query) return current;
  const key = foldNfcText(query);
  const searches = [query, ...current.filter((item) => foldNfcText(item) !== key)]
    .slice(0, RECENT_SEARCH_LIMIT);
  storePersonValue(RECENT_SEARCH_KEY, JSON.stringify(searches));
  return searches;
}

function scorePage(page: Page, above: () => string[], query: string, keywords: SearchKeywords, favorite: boolean) {
  if (!query) return favorite ? 0 : 10;
  // A page's own title, not the "새 페이지" an untitled one shows: Notion
  // doesn't find untitled pages by that word (seen 2026-11-07). Its words
  // as the server judges them: "lowix" finds no "Brendlowix", as Notion's
  // search doesn't (seen 2026-11-11). A page under one whose title holds
  // them is found by its path.
  const title = foldSearchText(page.title.trim());
  if (searchKeywordsHeld(keywords, title)) return titleAnswerScore(title, query);
  if (above().some((ancestor) => searchKeywordsHeld(keywords, ancestor))) return 3;
  return Number.POSITIVE_INFINITY;
}

/** How well a folded name holding the keywords answers the folded query: 0 the query itself, 1 starting with it, else 2. */
export function titleAnswerScore(title: string, query: string) {
  if (title === query) return 0;
  return title.startsWith(query) ? 1 : 2;
}

function timeValue(page: Page) {
  return Date.parse(page.updatedAt) || 0;
}

/**
 * The pages found by their titles, of those the search may show — out of the
 * trash and from under any page in it (outOfTrashIn) — the first SEARCH_RESULT_LIMIT of them:
 * kept as they come rather than all sorted, a page's path read only when its
 * title doesn't hold the words. Sorting a 44,000-page workspace, its times
 * read at each comparison, took much of the frame search's rows came in
 * (2026-11-14).
 */
export function localTitleHits(
  pages: Page[],
  workspaceId: string,
  query: string,
  recentOrder: ReadonlyMap<string, number> = new Map(),
  favoriteIds: ReadonlySet<string> = new Set(),
) {
  const folded = foldSearchText(searchTextQuery(query));
  const keywords = readSearchKeywords(query);
  let pagesById: Record<string, Page> | null = null;
  const above = (page: Page) => () => pagePath(page, pagesById ??= pagesByIdOf(pages)).split(" / ");
  type Found = { hit: SearchHit; recent: number; favorite: number; time: number };
  const before = (left: Found, right: Found) => {
    if (!folded && (left.recent !== Number.POSITIVE_INFINITY || right.recent !== Number.POSITIVE_INFINITY)) {
      return left.recent - right.recent;
    }
    return left.hit.score - right.hit.score
      || right.favorite - left.favorite
      || right.time - left.time
      || left.hit.page.position - right.hit.page.position
      || left.hit.page.id.localeCompare(right.hit.page.id);
  };
  const kept: Found[] = [];
  for (const page of pages) {
    if (page.inTrash || page.workspaceId !== workspaceId) continue;
    const favorite = favoriteIds.has(page.id);
    const score = scorePage(page, folded ? above(page) : () => [], folded, keywords, favorite);
    if (score === Number.POSITIVE_INFINITY) continue;
    const found: Found = { hit: { match: "title", page, preview: "", score }, recent: recentOrder.get(page.id) ?? Number.POSITIVE_INFINITY,
      favorite: Number(favorite), time: 0 };
    if (kept.length === SEARCH_RESULT_LIMIT && found.recent === Number.POSITIVE_INFINITY && kept.at(-1)!.recent === Number.POSITIVE_INFINITY
      && score > kept.at(-1)!.hit.score) continue;
    found.time = timeValue(page);
    let at = kept.length;
    while (at > 0 && before(found, kept[at - 1]) < 0) at -= 1;
    if (at >= SEARCH_RESULT_LIMIT) continue;
    kept.splice(at, 0, found);
    if (kept.length > SEARCH_RESULT_LIMIT) kept.pop();
  }
  return kept.map((found) => found.hit);
}

/**
 * The results: a page shows once, as in Notion's, found by its title if it
 * was, with the line of its body holding the keywords. What the server found
 * stands where it put it, all it gave, those read after with them: one list
 * in one order, as Notion's quick find and its MCP search give it (seen
 * 2026-11-04), every page matching all the keywords before those matching
 * some (user decision 2026-10-31, A), a page matching some by its title too
 * (2026-11-11: "W398 Qorlith only" stood before "W398 plain bodyfull",
 * holding both in its body). A page found here alone — before the server
 * answers, or not among what it gave yet — follows them in its own order,
 * so that what "더 검색" reads takes its place without moving the rest
 * (2026-11-11: reading more for "w398" left 9 of 21 results, cut to eight
 * found by title before the one found by its body, and 12 in all).
 */
export function mergeSearchHits(local: SearchHit[], remote: WorkspaceSearchResult[]) {
  const merged = new Map<string, SearchHit>();
  const add = (hit: SearchHit, found = false) => {
    const current = merged.get(hit.page.id);
    merged.set(hit.page.id, current ? {
      ...hit,
      match: current.match === "title" || hit.match === "title" ? "title" : "body",
      preview: hit.preview || current.preview,
      score: found ? hit.score : Math.min(current.score, hit.score),
    } : hit);
  };
  for (const hit of local) add(hit);
  // Before every score found here (0 on).
  for (const [index, hit] of remote.entries()) add({ ...hit, score: index - remote.length }, true);
  return [...merged.values()]
    .sort((left, right) => left.score - right.score || left.page.id.localeCompare(right.page.id));
}
