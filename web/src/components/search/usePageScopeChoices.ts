import { useMemo } from "react";
import { usePageVisits } from "@/components/library/useLibraryData";
import { outOfTrashIn } from "@/lib/pageIndex";
import { usePageNames } from "@/lib/pageNames";
import { routeInfoFromPath, usePathname } from "@/lib/router";
import type { Page, Session } from "@/types";
import { readSearchKeywords, searchKeywordsHeld, searchKeywordWord, type SearchKeywords } from "./searchModel";

/** As many pages as a picker lists before it is searched, about Notion's (44, seen 2026-11-16). */
const SCOPE_RESULTS = 50;

/**
 * The pages a "검색 범위" picker lists, the search window's and the trash's
 * alike, as Notion's (seen 2026-11-16 at 1440, its two pickers listing the
 * same pages in the same order, the search window's "오늘 · 어제 …" list's
 * order): the page on screen first, then the pages the reader visited,
 * newest first, then the rest, edited last first. Typed, the pages whose
 * title the search window finds by its words (searchKeywordsHeld, folded as
 * it folds: "strasse" lists "Straße", "aero" "Ærø", "иод" "йод"; a word's
 * front, not its inside: "morv" lists "Morvenalq", "venal" nothing), those
 * holding every word before those holding some ("strasse zzqv" lists
 * "Straße") — Notion's, seen 2026-11-16 — each in the same order. None in
 * the trash or under a page in it (outOfTrashIn). The visits are read as the
 * picker opens.
 */
export function usePageScopeChoices({ session, workspaceId, pages, query, open }: {
  session: Session; workspaceId: string; pages: readonly Page[]; query: string; open: boolean;
}): Page[] {
  const pageNames = usePageNames();
  const visits = usePageVisits(session, workspaceId, open ? "scope" : null);
  const pathname = usePathname();
  const route = routeInfoFromPath(pathname);
  const currentPageId = route.kind === "page" ? route.pageId : null;
  return useMemo(() => {
    if (!open) return [];
    const keywords = readSearchKeywords(query);
    const typed = keywords.phrases.length > 0 || keywords.words.length > 0;
    const rank = new Map<string, number>();
    visits.forEach((visit, index) => { if (!rank.has(visit.pageId)) rank.set(visit.pageId, index); });
    if (currentPageId) rank.set(currentPageId, -1);
    const found = new Map<string, number>(), outOfTrash = outOfTrashIn(pages);
    for (const page of pages) {
      if (!outOfTrash(page) || page.workspaceId !== workspaceId) continue;
      const held = typed ? titleHeld(keywords, pageNames.title(page)) : 0;
      if (held >= 0) found.set(page.id, held);
    }
    return pages.filter((page) => found.has(page.id)).sort((left, right) => {
      const a = rank.get(left.id) ?? Number.POSITIVE_INFINITY, b = rank.get(right.id) ?? Number.POSITIVE_INFINITY;
      return found.get(left.id)! - found.get(right.id)! || (a !== b ? a - b : (right.updatedAt ?? "").localeCompare(left.updatedAt ?? ""));
    }).slice(0, SCOPE_RESULTS);
  }, [currentPageId, open, pages, query, visits, workspaceId, pageNames]);
}

/** 0 when a title holds every word, 1 when it holds every phrase and some of the other words, -1 otherwise. */
function titleHeld(keywords: SearchKeywords, title: string) {
  if (searchKeywordsHeld(keywords, title)) return 0;
  const phrases = keywords.phrases.every((phrase) => searchKeywordsHeld({ phrases: [phrase], words: [] }, title));
  return phrases && keywords.words.some((_, index) => searchKeywordsHeld(searchKeywordWord(keywords, index), title)) ? 1 : -1;
}
