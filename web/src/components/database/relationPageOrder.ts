import { useEffect, useMemo, useState } from "react";
import { api } from "@/api";
import { useProductPagesSelect } from "@/components/product/productPagesStore";
import type { ProductPages } from "@/components/product/productPagesContext";
import type { Page } from "@/lib/types";

/**
 * The reader's page visits, newest first, read each time a list of a related
 * database's pages opens: a page's place among them, 0 the latest.
 */
export function useRelationPageVisits(open: boolean, workspaceId: string): ReadonlyMap<string, number> {
  // The session alone, not the page list: each relation cell takes this, and
  // a page's settings changing drew every one again (relationSession).
  const session = useProductPagesSelect(relationSession);
  const [visited, setVisited] = useState<ReadonlyMap<string, number>>(() => new Map());
  useEffect(() => {
    if (!open || !session) return undefined;
    const controller = new AbortController();
    api.readPageVisits(session, workspaceId, controller.signal)
      .then((read) => setVisited(new Map(read.visits.map((visit, index) => [visit.pageId, index]))))
      .catch(() => undefined);
    return () => controller.abort();
  }, [open, session, workspaceId]);
  return visited;
}

/**
 * A relation filter's pages, the related database's, in the relation
 * picker's order (relationPagesInOrder), the reader's visits read once as
 * it shows while `wanted`: Notion's filter value lists are the same search
 * of the database (seen 2026-11-17 in Chrome at 1440 and on a Galaxy A90 at
 * 411: a page just visited first, then one just edited).
 */
export function useRelationRowsInOrder(rows: readonly Page[] | undefined, workspaceId: string | undefined, wanted = true): readonly Page[] {
  const visited = useRelationPageVisits(wanted && Boolean(workspaceId), workspaceId ?? "");
  return useMemo(() => wanted ? relationPagesInOrder(rows ?? [], visited) : [], [rows, visited, wanted]);
}

/**
 * The reader's session, as a relation cell reads it from the product pages:
 * one answer of them (useProductPagesSelect), so a page locked, its text made
 * small or renamed, which reads the page list anew, doesn't draw the page's
 * relation cells again (2026-11-13: 150 of them, with their chips, drawn for
 * each such press on a page of three tables).
 */
export function relationSession(pages: ProductPages | null) {
  return pages?.session;
}

/**
 * A related database's pages in the order Notion's lists of them give,
 * the relation picker's and a relation grouping's "+ 신규 그룹" alike (both
 * one search of the database, seen 2026-11-12): the pages the reader
 * visited first, the latest first — a page opened whole or in a peek alike —
 * then the rest by when they were last edited, the latest first; the table's
 * order where those tie. The row choosing is no first of its own: in a
 * database relating to itself Notion's picker in a table cell keeps that
 * row in its place, and on the row's page it is first only as the page
 * just visited (seen 2026-11-12). Each page's place is read once to sort
 * them, not again at each comparison.
 */
export function relationPagesInOrder(pages: readonly Page[], visited: ReadonlyMap<string, number>): Page[] {
  return pages
    .map((page) => ({ page, visit: visited.get(page.id) ?? Number.MAX_SAFE_INTEGER,
      edited: page.updatedAt ? Date.parse(page.updatedAt) || 0 : 0 }))
    .sort((left, right) => left.visit - right.visit || right.edited - left.edited || left.page.position - right.page.position)
    .map(({ page }) => page);
}

// Folded for a search: lower case, accents dropped ("uber" finds "über"),
// a Korean syllable kept whole.
function searchFolded(text: string) {
  return text.normalize("NFD").replace(/\p{M}/gu, "").normalize("NFC").toLowerCase();
}

// A name's words are its runs of letters and digits: "Bob-cat", "x_under",
// "file.txt" and "(paren)" break where those end, "CamelCase" is one word.
// Korean, and the scripts written without spaces between words, are found
// anywhere instead (relationSearchRank).
const WORD_BREAK = /[^\p{L}\p{N}]+/u;
const ANYWHERE = /[\p{Script=Hangul}\p{Script=Han}\p{Script=Hiragana}\p{Script=Katakana}\p{Script=Thai}\p{Script=Lao}\p{Script=Khmer}\p{Script=Myanmar}]/u;
let lastSearch: { query: string; rank: (name: string) => number } | undefined;

/**
 * How well a page's name answers a relation search, 0 for not at all, as
 * Notion's one page search does wherever its pages are found — the relation
 * picker, a relation filter's value on the desktop and the phone (seen
 * 2026-11-17 in Chrome at 1440 and on a Galaxy A90 at 411, the same pages
 * each way): each of the search's words is found where a word of the name
 * starts ("z" finds "Zac", "ac" nothing; "2024" finds "mail 2024", "024"
 * nothing), any one of them enough and more of the search's letters found
 * first ("tmpl b": Tmpl B, Tmpl D, then Bob-cat — seen once, in Chrome), a
 * name holding the whole search as typed before the rest ("le.t": file.txt
 * first). A word with Korean in it is found
 * anywhere in the name, its spaces left out: Notion breaks Korean by a
 * dictionary ("사이트" and "웹사이트카피" find "웹사이트 카피", "북" finds
 * "노트북"; "이" and "트" alone found nothing there, which this does not
 * follow); Chinese, Japanese and Thai, not seen, are found anywhere too, as
 * before. The search's own reading is kept for the next name.
 */
export function relationSearchRank(name: string, query: string): number {
  if (lastSearch?.query !== query) {
    const search = searchFolded(query.trim()), words = search.split(WORD_BREAK).filter(Boolean);
    lastSearch = { query, rank: (title) => {
      const folded = searchFolded(title), own = folded.split(WORD_BREAK), joined = own.join("");
      const found = words.reduce((sum, word) => sum
        + ((ANYWHERE.test(word) ? joined.includes(word) : own.some((part) => part.startsWith(word))) ? word.length : 0), 0);
      return found > 0 || (!words.length && search) ? found * 2 + Number(folded.includes(search)) : 0;
    } };
  }
  return lastSearch.rank(name);
}

/**
 * The pages a search finds (relationSearchRank), the best first, else in
 * the order they came; all of them, as they came, without a search.
 */
export function relationPagesFound<T>(pages: T[], query: string, titleOf: (page: T) => string): T[] {
  if (!query.trim()) return pages;
  return pages
    .map((page, index) => ({ page, index, rank: relationSearchRank(titleOf(page), query) }))
    .filter(({ rank }) => rank > 0)
    .sort((left, right) => right.rank - left.rank || left.index - right.index)
    .map(({ page }) => page);
}
