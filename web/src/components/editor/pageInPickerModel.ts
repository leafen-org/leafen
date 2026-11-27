import { pageNamesNow } from "@/lib/pageNames";
import { foldNfcText } from "@/shared/database/natural-order.mjs";
import type { Page } from "@/types";
import { outOfTrashIn, pageMapOf } from "@/lib/pageIndex";

// The pages "페이지 위치:" offers beside the turn list (Notion, seen
// 2026-10-01): under "추천", the pages the reader visited last, each a page
// that can open to show its sub-pages (a database can't); a search lists the
// pages whose names hold it, without their sub-pages. Above the visited
// pages, "〈이름〉 개인 페이지" opens to the reader's private pages; it is
// no place itself — pressed, it opens or closes (seen 2026-10-02).

/** How many visited pages "추천" offers. */
export const PAGE_IN_SUGGESTED = 8;
/** How many pages a search offers. */
export const PAGE_IN_FOUND = 20;

export type PageInRow = {
  page: Page;
  depth: number;
  /** A page opens to its sub-pages; a database doesn't. */
  expandable: boolean;
  expanded: boolean;
  /** Why the blocks can't go in it, if they can't. */
  reason?: string;
  /** Listed under "〈이름〉 개인 페이지", which opens its pages apart from "추천". */
  inPrivate?: true;
};

/** "〈이름〉 개인 페이지", which opens to the reader's private pages. */
export type PageInPrivateRow = { private: true; depth: 0; expandable: true; expanded: boolean };

export type PageInListRow = PageInRow | PageInPrivateRow;

export function isPageInPrivateRow(row: PageInListRow | undefined): row is PageInPrivateRow {
  return Boolean(row && "private" in row);
}

type Input = {
  pages: readonly Page[];
  sourcePageId: string;
  unavailable?: (page: Page) => string | undefined;
};

/** The pages a place may be: none in the trash or under a page in it (outOfTrashIn), nor the page the blocks leave. */
function offeredIn(input: Input): (page: Page | undefined) => page is Page {
  const outOfTrash = outOfTrashIn(input.pages);
  return (page): page is Page => Boolean(page && outOfTrash(page) && page.id !== input.sourcePageId);
}

function row(page: Page, depth: number, expanded: ReadonlySet<string>, unavailable: Input["unavailable"]): PageInRow {
  const expandable = page.kind !== "database";
  return { page, depth, expandable, expanded: expandable && expanded.has(page.id), reason: unavailable?.(page) };
}

function byPosition(left: Page, right: Page) {
  return (left.position ?? 0) - (right.position ?? 0) || (left.id < right.id ? -1 : 1);
}

/** The given pages from the given depth, each followed by its sub-pages, in their order, once opened. */
function treeRows(top: readonly Page[], depth: number, input: Input & { expanded: ReadonlySet<string> }): PageInRow[] {
  const children = new Map<string, Page[]>(), offered = offeredIn(input);
  for (const page of input.pages) {
    if (!page.parentId || !offered(page)) continue;
    children.set(page.parentId, [...(children.get(page.parentId) ?? []), page]);
  }
  for (const list of children.values()) list.sort(byPosition);
  const rows: PageInRow[] = [];
  const add = (page: Page, level: number, seen: Set<string>) => {
    if (seen.has(page.id)) return;
    const next = row(page, level, input.expanded, input.unavailable);
    rows.push(next);
    if (!next.expanded) return;
    const within = new Set(seen).add(page.id);
    for (const child of children.get(page.id) ?? []) add(child, level + 1, within);
  };
  for (const page of top) add(page, depth, new Set());
  return rows;
}

/**
 * "추천": the pages visited last, newest first (or the top-level pages
 * before any visit), each followed by its sub-pages, in their order, once
 * opened.
 */
export function pageInSuggestedRows(input: Input & {
  visits: readonly { pageId: string }[];
  expanded: ReadonlySet<string>;
}): PageInRow[] {
  const byId = pageMapOf(input.pages), offered = offeredIn(input);
  const visited = [...new Set(input.visits.map(({ pageId }) => pageId))]
    .map((id) => byId.get(id))
    .filter(offered);
  const top = (visited.length > 0 ? visited : input.pages.filter((page) => !page.parentId && offered(page)))
    .slice(0, PAGE_IN_SUGGESTED);
  return treeRows(top, 0, input);
}

/**
 * "〈이름〉 개인 페이지" and, once opened, the reader's private pages — the
 * top-level pages in no teamspace that weren't shared with them, in the
 * sidebar's order — each opening to its sub-pages.
 */
export function pageInPrivateRows(input: Input & {
  workspaceId: string;
  sharedPageIds: readonly string[];
  expanded: ReadonlySet<string>;
  open: boolean;
}): PageInListRow[] {
  const head: PageInPrivateRow = { private: true, depth: 0, expandable: true, expanded: input.open };
  if (!input.open) return [head];
  const shared = new Set(input.sharedPageIds), offered = offeredIn(input);
  const top = input.pages
    .filter((page) => !page.parentId && !page.teamspaceId && page.workspaceId === input.workspaceId && !shared.has(page.id)
      && offered(page))
    .sort(byPosition);
  return [head, ...treeRows(top, 1, input).map((row): PageInRow => ({ ...row, inPrivate: true }))];
}

/** A search's pages: names starting with it first, then the rest holding it. */
export function pageInFoundRows(input: Input & { query: string }): PageInRow[] {
  const query = foldNfcText(input.query.trim());
  if (!query) return [];
  return input.pages
    .filter(offeredIn(input))
    .map((page) => ({ page, name: foldNfcText(pageNamesNow.title(page)) }))
    .filter(({ name }) => name.includes(query))
    .sort((left, right) => Number(!left.name.startsWith(query)) - Number(!right.name.startsWith(query))
      || left.name.localeCompare(right.name))
    .slice(0, PAGE_IN_FOUND)
    .map(({ page }) => ({ page, depth: 0, expandable: false, expanded: false, reason: input.unavailable?.(page) }));
}

/**
 * Why a page's blocks can't go in a page, as the server allows: pages follow
 * their blocks only within the page's own teamspace or private pages, and
 * never into a wiki, a database or its rows.
 */
export function pageInUnavailable(pages: readonly Page[], sourcePageId: string, reason: string) {
  const byId = pageMapOf(pages);
  const source = byId.get(sourcePageId);
  return (page: Page) => (source && (page.teamspaceId || "") !== (source.teamspaceId || ""))
    || page.isWiki || page.wikiRootId || page.kind === "database" || byId.get(page.parentId)?.kind === "database"
    ? reason : undefined;
}
