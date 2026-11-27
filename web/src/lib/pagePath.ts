import { untitledDatabaseSourcesName, type DatabaseSourceTitle } from "@/components/editor/databaseTitles";
import { isSyntheticNotionImportRootPage } from "./importedNotionUi";
import { listedPageNameKind, untitledPageDisplayTitle } from "./pageTitle";
import { pageNamesNow } from "./pageNames";
import { knownPathAboveId, knownPathHref, knownPathPage, pathParent } from "./pagePathPieces";
import { knownTeamspaceName } from "./teamspaceNames";
import type { Page } from "@/types";

/** A page of the reader's, else a piece of a path the server named (pagePathPieces.ts). */
function pathPage(id: string, pagesById: Record<string, Page>): Page | undefined {
  return pagesById[id] ?? knownPathPage(id);
}

/**
 * The titles of a page and the pages above it, from its top-level page down,
 * through the database pieces and database pages the server named above it;
 * a piece that opens elsewhere than a page's address says where.
 */
export function pagePathLinks(pageId: string | null | undefined, pagesById: Record<string, Page>) {
  const links: Array<{ id: string; title: string; href?: string }> = [];
  const seen = new Set<string>();
  let current = pageId ? pathPage(pageId, pagesById) : undefined;
  while (current && !seen.has(current.id)) {
    seen.add(current.id);
    if (!isSyntheticNotionImportRootPage(current)) {
      const href = knownPathHref(current.id);
      links.unshift({ id: current.id, title: pageNamesNow.title(current), ...(href ? { href } : {}) });
    }
    current = pathParent(current, (id) => pagesById[id]);
  }
  return links;
}

/** The titles from a page's top down, its teamspace's name first when the top page is in one. */
function titlesFrom(pageId: string | null | undefined, pagesById: Record<string, Page>) {
  const links = pagePathLinks(pageId, pagesById);
  const team = knownTeamspaceName(links.length > 0 ? pathPage(links[0].id, pagesById)?.teamspaceId : undefined);
  return [...(team ? [team] : []), ...links.map(({ title }) => title)];
}

/** The titles of the pages above a page, from its top-level page (or its teamspace) to its parent. */
function pageAncestorTitles(page: Page, pagesById: Record<string, Page>) {
  const parent = pathParent(page, (id) => pagesById[id]);
  if (parent) return titlesFrom(parent.id, pagesById);
  // A teamspace's top-level page sits in its teamspace, as Notion's path names it.
  return page.parentId ? [] : [knownTeamspaceName(page.teamspaceId)].filter(Boolean);
}

/**
 * Where a page sits as Notion's lists show it under a title, one title a
 * piece and "..." (null here) for those folded away, drawn on one line with
 * "/" between and each title cut on its own (PlaceLine).
 */
export type Place = readonly (string | null)[];

/** Titles as a page's place: the first and the last, with "..." for those between, as Notion's path (seen 2026-10-31). */
function shortPlace(labels: string[]): Place {
  return labels.length > 2 ? [labels[0], null, labels[labels.length - 1]] : labels;
}

/** A place as words, "Top / ... / Page", for finding it and naming it where it isn't drawn. */
export function placeText(place: Place) {
  return place.map((piece) => piece ?? "...").join(" / ");
}

/** Every page above a page, as "Top / Parent / Child", for finding it by where it sits. */
export function pagePath(page: Page, pagesById: Record<string, Page>) {
  return pageAncestorTitles(page, pagesById).join(" / ");
}

/**
 * Where a page sits, as Notion's lists show it under its title (seen
 * 2026-11-12 alike in the search and its scope list, the @ menu, "옮기기",
 * the trash and a relation's "+ 신규 그룹"): its top-level page and its
 * parent, with "..." for the pages between them. A top-level page has none.
 */
export function pagePlace(page: Page, pagesById: Record<string, Page>): Place {
  return shortPlace(pageAncestorTitles(page, pagesById));
}

/**
 * Where a page directly under the given page sits, as pagePlace reads it,
 * with any last titles the page list lacks: for a database page, the page
 * holding its database and then the database's name.
 */
export function placeUnder(parentId: string, pagesById: Record<string, Page>, ...below: string[]): Place {
  return shortPlace([...titlesFrom(parentId, pagesById), ...below]);
}

/** A database page's inline database, as a search or the @ menu names it: none for a full-page database. */
export type InlineDatabaseName = { inlineDatabase?: boolean; databaseTitle?: string };

/**
 * Where a database page sits, as Notion's paths show it: the page holding
 * its database and, for an inline database, the database — "Page /
 * Database". A full-page database is that page itself. Given the page,
 * the path the server read for it, once read, which names its source too.
 */
export function databasePagePlace(containerPageId: string, pagesById: Record<string, Page>, database?: InlineDatabaseName, pageId?: string): Place {
  // Once the server named its path, with its source while its database holds several (pagePathPieces.ts).
  const named = pageId ? knownPathAboveId(pageId) : undefined;
  if (named) return shortPlace(titlesFrom(named, pagesById));
  return database?.inlineDatabase
    ? placeUnder(containerPageId, pagesById, database.databaseTitle?.trim() || untitledPageDisplayTitle("database"))
    : placeUnder(containerPageId, pagesById);
}

/**
 * Where a database a database page holds in its body sits, as Notion's @
 * menu shows one (seen 2026-11-15, "Top / ... / Row"): the page's own place
 * (databasePagePlace) and then the page.
 */
export function rowBodyDatabasePlace(
  containerPageId: string, pagesById: Record<string, Page>, database: InlineDatabaseName, rowId: string, rowTitle: string,
): Place {
  const named = knownPathAboveId(rowId);
  const above = named ? titlesFrom(named, pagesById) : [...titlesFrom(containerPageId, pagesById),
    ...(database.inlineDatabase ? [database.databaseTitle?.trim() || untitledPageDisplayTitle("database")] : [])];
  return shortPlace([...above, rowTitle]);
}

/**
 * Where a database sits, as Notion's lists of data sources show it under its
 * name — to relate to, for a new view or a linked one, a layout tab, in the
 * new page window (seen 2026-11-02, 2026-11-12): the pages above a full-page
 * database, or the page holding an inline one and those above it — "Top /
 * Page", each title its own piece (PlaceLine). Nothing while the page
 * isn't among the reader's.
 */
export function databaseSourcePlace(
  pageId: string | null | undefined, pagesById: Record<string, Page>, sourceTitles?: readonly DatabaseSourceTitle[], databaseTitle?: string,
  fold: (labels: string[]) => Place = sourcePlace,
): Place | undefined {
  const page = pageId ? pagesById[pageId] : undefined;
  if (!page) return undefined;
  const database = severalSourcesDatabaseName(sourceTitles, page, databaseTitle);
  return fold([...(page.kind === "database" ? pageAncestorTitles(page, pagesById) : titlesFrom(page.id, pagesById)), ...database]);
}

/**
 * A place as Notion's lists of data sources show it (seen 2026-11-12 in the
 * layout builder, the relation window, the linked view's list and the new
 * database gallery alike): up to four titles all shown, more as the first two,
 * "..." and the last two.
 */
function sourcePlace(labels: string[]): Place {
  return labels.length > 4 ? [labels[0], labels[1], null, ...labels.slice(-2)] : labels;
}

/**
 * A database holding several sources ends its sources' paths, as Notion's
 * (seen 2026-11-02, "Top / ... / 새 데이터 소스, 새 데이터 소스, 새 데이터
 * 소스"): a full-page one by its page's title, an inline one by its own name,
 * or its sources' names while it has none (inlineDatabaseName). None for one
 * source.
 */
export function severalSourcesDatabaseName(sourceTitles: readonly DatabaseSourceTitle[] | undefined, page?: Page, databaseTitle?: string): string[] {
  if (!sourceTitles || sourceTitles.length < 2) return [];
  if (page?.kind === "database") return [pageNamesNow.title(page)];
  return [databaseTitle?.trim() || untitledDatabaseSourcesName(sourceTitles)];
}

/**
 * databaseSourcePlace for a source a search found: while its page isn't
 * among the reader's, the page holding it, unless that is the database's own
 * — an untitled one by its kind's name, as Notion's lists of sources say "새
 * 페이지" under an untitled page's database (seen 2026-11-14).
 */
export function foundDatabaseSourcePlace(
  source: {
    pageId: string; pageTitle: string; title: string; sourceTitles?: readonly DatabaseSourceTitle[]; databaseTitle?: string;
    pageKind?: string; viewBlock?: boolean;
  },
  pagesById: Record<string, Page>,
  fold: (labels: string[]) => Place = sourcePlace,
): Place {
  // A full-page database's page is the database itself; one made here has no kind yet.
  const ownPage = source.pageKind ? source.pageKind === "database" && !source.viewBlock : source.pageTitle === source.title;
  const holder = ownPage ? "" : source.pageTitle || (source.pageKind ? untitledPageDisplayTitle(listedPageNameKind(source)) : "");
  return databaseSourcePlace(source.pageId, pagesById, source.sourceTitles, source.databaseTitle, fold)
    ?? fold([holder, ...severalSourcesDatabaseName(source.sourceTitles, undefined, source.databaseTitle)].filter(Boolean));
}

/**
 * Where a database sits as the sidebar's "데이터베이스 보기" and "출처" lists
 * show it under each source (Notion's, seen 2026-11-18): the titles
 * foundDatabaseSourcePlace reads, past two the first and the last with
 * "..." between, as a page's path — "Top / ... / Page", "Top / ... / A 및 B"
 * for a database of several.
 */
export function sidebarDatabasePlace(source: Parameters<typeof foundDatabaseSourcePlace>[0], pagesById: Record<string, Page>): Place {
  return foundDatabaseSourcePlace(source, pagesById, shortPlace);
}
