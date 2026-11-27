import { useMemo } from "react";
import { pageMapOf } from "@/lib/pageIndex";
import type { Page, Session } from "../../types";
import { libraryRowPage } from "../library/libraryModel";
import { useLibraryPages, usePageVisits } from "../library/useLibraryData";
import { searchLeadHits } from "./searchLead";
import { visitSectionKey, type SearchHit, type SearchSection, type VisitSectionKey } from "./searchModel";

/** A page the reader visited, as a result: when, and whether it is one of the workspace's listed pages the search may show. */
export type SearchVisit = { hit: SearchHit; visitedAt: string; listed: boolean };

/**
 * The pages the reader visited, newest first, as the server keeps them for
 * that person (page_visits.go): a listed page the search may show, or a
 * database row's page by what the library read of it (useLibraryPages), the
 * pages read again each time the window opens. None in the trash or under a
 * page in it (outOfTrash), as the server's search finds none.
 */
export function useSearchVisits({ session, workspaceId, open, listShown, pagesById, searchable, outOfTrash }: {
  session: Session;
  workspaceId: string;
  open: boolean;
  listShown: boolean;
  pagesById: Readonly<Record<string, Page>>;
  searchable: (page: Page) => boolean;
  outOfTrash: (page: Page) => boolean;
}): SearchVisit[] {
  const visits = usePageVisits(session, workspaceId, open ? "open" : null);
  const unlistedVisitIds = listShown ? visits.map((visit) => visit.pageId).filter((id) => !pagesById[id]) : [];
  const unlistedVisits = useLibraryPages(session, workspaceId, unlistedVisitIds).pages;
  return useMemo(() => visits.flatMap((visit) => {
    const detail = unlistedVisits.get(visit.pageId);
    const listed = pagesById[visit.pageId];
    const shown = listed && searchable(listed) ? listed : null;
    const page = shown ?? (detail ? libraryRowPage(detail, workspaceId) : null);
    if (!page || !outOfTrash(page)) return [];
    const hit: SearchHit = { match: "title", page, preview: "", score: 0,
      ...(detail?.row && !shown ? { row: { rowId: page.id, containerPageId: detail.row.containerPageId, dataSourceId: detail.row.dataSourceId,
        ...(detail.row.template ? { template: true } : {}) } } : {}) };
    return [{ hit, visitedAt: visit.visitedAt, listed: Boolean(shown) }];
  }), [outOfTrash, pagesById, searchable, unlistedVisits, visits, workspaceId]);
}

/**
 * Before a search, the pages the reader visited, newest first, under
 * 오늘·어제·지난 7일·지난 30일·이전, one line each, as Notion's (seen
 * 2026-10-07): not the page on screen.
 */
export function searchVisitSections(visited: readonly SearchVisit[], currentPageId: string | undefined,
  label: (key: VisitSectionKey) => string): SearchSection[] | null {
  const now = new Date();
  const grouped = new Map<VisitSectionKey, SearchHit[]>();
  for (const { hit, visitedAt } of visited) {
    if (hit.page.id === currentPageId) continue;
    const key = visitSectionKey(visitedAt, now);
    grouped.set(key, [...(grouped.get(key) ?? []), hit]);
  }
  const order: VisitSectionKey[] = ["today", "yesterday", "week", "month", "older"];
  const sections = order.filter((key) => grouped.has(key)).map((key) => ({ label: label(key), hits: grouped.get(key)!, compact: true }));
  return sections.length ? sections : null;
}

/**
 * The visited pages that lead a search (searchLead.ts), the page on screen
 * among them as in Notion's (seen 2026-11-24 in two tabs, "현재 페이지"):
 * listed ones the search may show — out of the trash and from under any page
 * in it, as every visit (useSearchVisits) — and, while nothing narrows the
 * search, database rows whose databases' pages are listed.
 */
export function useSearchLead(visited: readonly SearchVisit[], query: string, { on, pages, rows }: {
  /** Whether results are sorted by how well they answer, the lead's only order. */
  on: boolean;
  pages: readonly Page[];
  /** Whether rows not in the page list may lead: no filter or content choice narrows the search. */
  rows: boolean;
}) {
  const candidates = useMemo(() => {
    if (!on) return [];
    const byId = pageMapOf(pages);
    return visited.filter(({ hit, listed }) => listed || (rows && Boolean(hit.row) && byId.has(hit.row!.containerPageId)))
      .map(({ hit }) => hit);
  }, [on, pages, rows, visited]);
  return useMemo(() => (query ? searchLeadHits(candidates, query) : []), [candidates, query]);
}

/** The results with the lead over them, a page leading shown there alone. */
export function withSearchLead(lead: readonly SearchHit[], rows: readonly SearchHit[]): SearchHit[] {
  if (!lead.length) return rows as SearchHit[];
  const leading = new Set(lead.map((hit) => hit.page.id));
  return [...lead, ...rows.filter((hit) => !leading.has(hit.page.id))];
}

/**
 * How many results the head counts: those past the lead, as Notion's head
 * counts what its server found (seen 2026-11-24 in two tabs: "검색 결과(2개)"
 * over three leading rows and two more), or the leading ones when nothing
 * else is found ("검색 결과(1개)" over "plumbert [Stra]ße" alone).
 */
export function searchResultsCount(hits: readonly SearchHit[]) {
  return hits.filter((hit) => !hit.lead).length || hits.length;
}
