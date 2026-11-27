import type { LibraryViewId } from "@/lib/router";
import { pageNamesNow } from "@/lib/pageNames";
import type { Page } from "@/types";
import type { LibraryPage, PageVisit } from "@/workspaceMembershipWire.generated";
import { rootTreePages, sharedTreePages } from "@/components/sidebar/pageTreeModel";
import { sortSidebarPages } from "@/components/sidebar/sidebarLayoutModel";
import { pageHref } from "@/lib/navigation";
import { isPeopleDatabasePage } from "@/lib/peopleDatabasePage";
import { PEOPLE_DIRECTORY_HREF, personPageHref, personProfileHref } from "@/lib/peopleProfiles";
import { TEMPLATE_PAGE_PARAM, templateAddress } from "@/components/database/useTemplateAddress";
import { pageMapOf, trashedPageIdsOf } from "@/lib/pageIndex";

/**
 * The library's lists, as Notion's (seen 2026-10-01): the pages the reader
 * visited last first, their favorites in the favorites' order, and the
 * shared and private pages heading those sections, the last edited first.
 */

export type LibraryInput = {
  /** The workspace's pages the reader has, out of the trash. */
  pages: readonly Page[];
  favoritePageIds: readonly string[];
  sharedPageIds: readonly string[];
  visits: readonly PageVisit[];
  /** The database rows' and templates' pages visited or kept among favorites, which the page list doesn't hold. */
  rowPages?: ReadonlyMap<string, Page>;
  /** The archived pages listed too ("보관된 페이지 포함"), which the sidebar leaves out. */
  withArchived?: boolean;
};

/** A row of a view's "설정 보기". */
export type LibrarySettingsItem = "properties" | "filter" | "sort" | "group" | "archived";

/**
 * What a view's "설정" lists, as Notion's library (its code, read
 * 2026-11-14; seen the same day at 1440): the teamspaces "보관된 페이지
 * 포함" alone, "최근" the columns shown and "필터", the favorites, shared and
 * private pages "정렬", "그룹화" and "보관된 페이지 포함" too. The documents
 * have their own tools.
 */
const SETTINGS_ITEMS: Record<LibraryViewId, readonly LibrarySettingsItem[]> = {
  teamspaces: ["archived"],
  recents: ["properties", "filter"],
  documents: [],
  favorites: ["properties", "filter", "sort", "group", "archived"],
  shared: ["properties", "filter", "sort", "group", "archived"],
  private: ["properties", "filter", "sort", "group", "archived"],
};
export function librarySettingsItems(view: LibraryViewId): readonly LibrarySettingsItem[] {
  return SETTINGS_ITEMS[view];
}

/**
 * The views a reader has. A guest's are the favorites and the pages shared
 * with them, as Notion's guest library (seen 2026-11-12) — no teamspaces,
 * recents or private pages. The documents follow the favorites.
 */
export function libraryViews(guest: boolean): LibraryViewId[] {
  return guest ? ["favorites", "documents", "shared"] : ["teamspaces", "recents", "favorites", "documents", "shared", "private"];
}

// The pages of a list out of the trash, by id: worked out once for a list, as
// the library draws again (a 44,000-page workspace's walked every page up to
// its top on each drawing, 2026-11-14).
const livePagesOf = new WeakMap<readonly Page[], readonly Page[]>();

function livePages(pages: readonly Page[]) {
  let live = livePagesOf.get(pages);
  if (!live) {
    // A page under a page in the trash is in the trash too.
    const trashed = trashedPageIdsOf(pages);
    live = trashed.size ? pages.filter((page) => !trashed.has(page.id)) : pages;
    livePagesOf.set(pages, live);
  }
  return live;
}

export function libraryViewPages(view: LibraryViewId, input: LibraryInput): Page[] {
  const pages = livePages(input.pages);
  const live = pageMapOf(pages);
  if (view === "recents") return input.visits.flatMap((visit) => live.get(visit.pageId) ?? input.rowPages?.get(visit.pageId) ?? []);
  if (view === "favorites") return input.favoritePageIds.flatMap((id) => live.get(id) ?? input.rowPages?.get(id) ?? []);
  const shared = new Set(input.sharedPageIds);
  if (view === "shared") return sortSidebarPages(sharedTreePages(pages, shared, input.withArchived), "edited");
  // The private pages in their order by hand, the sidebar's "수동", which their own sort (by last edit) shows otherwise.
  return rootTreePages(pages, input.withArchived).filter((page) => !page.teamspaceId && !shared.has(page.id))
    .sort((left, right) => left.position - right.position);
}

/** The set with the key taken out, or put in when it wasn't there. */
export function flipSet(current: ReadonlySet<string>, key: string): ReadonlySet<string> {
  const next = new Set(current);
  if (!next.delete(key)) next.add(key);
  return next;
}

export type LibrarySource =
  | { kind: "page"; page: Page }
  | { kind: "teamspace"; teamspaceId: string }
  | { kind: "shared" }
  | { kind: "private" };

/**
 * Where a page lives, as the library's "출처" says: the page above it, or,
 * at the top, its teamspace, the reader's shared pages or their private
 * pages. A shared page's says nothing, as Notion's library leaves it empty
 * for whoever shares it and whoever it is shared with, guest or member (seen
 * 2026-11-12 and 11-13).
 */
export function librarySource(page: Page, input: Pick<LibraryInput, "pages" | "sharedPageIds">): LibrarySource {
  // People's own database is under nothing, its "출처" empty, as Notion's (seen 2026-11-16).
  if (isPeopleDatabasePage(page)) return { kind: "shared" };
  // By the list's own index: a row looked through the whole list for its parent.
  const parent = page.parentId ? pageMapOf(input.pages).get(page.parentId) : undefined;
  if (parent) return { kind: "page", page: parent };
  if (page.teamspaceId) return { kind: "teamspace", teamspaceId: page.teamspaceId };
  if (input.sharedPageIds.includes(page.id)) return { kind: "shared" };
  return { kind: "private" };
}

export function librarySourceTitle(source: LibrarySource, names: LibrarySourceNames) {
  if (source.kind === "page") return pageNamesNow.title(source.page);
  if (source.kind === "teamspace") return names.teamspace(source.teamspaceId);
  return source.kind === "shared" ? "" : names.private;
}

/** What the "출처" column calls a teamspace and the private pages. */
export type LibrarySourceNames = { teamspace(id: string): string; private: string };

/** The library's columns, as Notion's table names them. */
export type LibraryColumn = "name" | "creator" | "source" | "edited" | "visited" | "created" | "lastEditor";

/** Every column, "최근"'s at first (Notion's, seen 2026-10-02 and 2026-11-14). */
export const LIBRARY_COLUMNS: readonly LibraryColumn[] = ["name", "creator", "source", "edited", "visited", "created", "lastEditor"];
export const DEFAULT_LIBRARY_COLUMNS: readonly LibraryColumn[] = ["name", "creator", "source", "edited", "visited"];

/**
 * Each view's columns at first, as Notion's "속성 표시 여부" lists them (seen
 * 2026-10-31 and 2026-11-14 at 1440): those shown, then those hidden, in one
 * order of the view's own. "최근" shows five and hides "생성 시간" and
 * "마지막으로 편집한 사람"; "즐겨찾기" and "공유된 페이지" show four and hide
 * those two and then "마지막 방문 시간"; "개인 페이지" shows the name and
 * "마지막 수정 시간" and hides "마지막 방문 시간", "생성 시간", "생성자",
 * "마지막으로 편집한 사람" and "출처".
 */
const VIEW_COLUMNS: Record<string, { order: readonly LibraryColumn[]; shown: number }> = {
  recents: { order: LIBRARY_COLUMNS, shown: 5 },
  favorites: { order: ["name", "creator", "source", "edited", "created", "lastEditor", "visited"], shown: 4 },
  shared: { order: ["name", "creator", "source", "edited", "created", "lastEditor", "visited"], shown: 4 },
  private: { order: ["name", "edited", "visited", "created", "creator", "lastEditor", "source"], shown: 2 },
};
const viewColumns = (view: string) => VIEW_COLUMNS[view] ?? VIEW_COLUMNS.recents;

/** The columns a view shows at first. */
export function defaultLibraryColumns(view: string): LibraryColumn[] {
  const { order, shown } = viewColumns(view);
  return order.slice(0, shown);
}

/**
 * The sort a view has at first: "개인 페이지" its pages last edited first,
 * as Notion's (seen 2026-11-15 and 2026-11-16 at 1440: its "정렬" blue and,
 * opened, "마지막 수정 시간: 새로운 항목 → 오래된 항목"; its phone's filter
 * sheet "정렬 · 마지막 수정 시간"), and "최근" those visited last first (seen
 * 2026-11-16 in iPhone Safari 402: its filter sheet "정렬 · 마지막 방문 시간",
 * opening "마지막 방문 시간 · 새로운 항목 → 오래된 항목으로 정렬 · 삭제 |
 * 정렬 추가 · 정렬 제거"; its desktop row has no "정렬"). The others have none.
 */
export function defaultLibrarySort(view: string): LibrarySort | null {
  return view === "private" ? { column: "edited", direction: "descending" }
    : view === "recents" ? { column: "visited", direction: "descending" } : null;
}

/** A view's columns: those shown and those hidden, each in its order. */
export type LibraryColumns = { columns: LibraryColumn[]; hidden: LibraryColumn[] };

/**
 * The columns a view leaves out, in its order: those hidden since it opened
 * first, the last one hidden first, then the rest in the view's order at
 * first (libraryColumnsWith).
 */
export function libraryHiddenColumns(view: string, shown: readonly LibraryColumn[], hidden: readonly LibraryColumn[] = []): LibraryColumn[] {
  const left = (column: LibraryColumn) => !shown.includes(column) && LIBRARY_COLUMNS.includes(column);
  return [...new Set([...hidden.filter(left), ...viewColumns(view).order.filter(left)])];
}

/**
 * The columns with one shown or hidden, as Notion's eye does (seen
 * 2026-11-14): a column shown goes after the others shown, one hidden
 * before the others hidden. The page name always shows.
 */
export function libraryColumnsWith({ columns, hidden }: LibraryColumns, column: LibraryColumn, visible: boolean): LibraryColumns {
  const rest = hidden.filter((entry) => entry !== column);
  if (column === "name") return { columns: [...columns], hidden: [...hidden] };
  if (!visible) return { columns: columns.filter((entry) => entry !== column), hidden: [column, ...rest] };
  return { columns: columns.includes(column) ? [...columns] : [...columns, column], hidden: rest };
}

/** "모두 표시하기" puts the hidden after those shown, in their order; "모두 숨기기" all but the name before the hidden. */
export function libraryColumnsAll({ columns, hidden }: LibraryColumns, visible: boolean): LibraryColumns {
  if (visible) return { columns: [...new Set([...columns, ...hidden])], hidden: [] };
  return { columns: ["name"], hidden: [...new Set([...columns.filter((column) => column !== "name"), ...hidden])] };
}

/**
 * The shown columns with one moved before or after another, as dragging a
 * header or a "속성 표시 여부" row does; the page name stays first.
 */
export function moveLibraryColumn(
  shown: readonly LibraryColumn[], column: LibraryColumn, target: LibraryColumn, after: boolean,
): LibraryColumn[] {
  if (column === "name" || column === target || !shown.includes(column) || !shown.includes(target)) return [...shown];
  const rest = shown.filter((entry) => entry !== column);
  const index = rest.indexOf(target) + (after || target === "name" ? 1 : 0);
  return [...rest.slice(0, index), column, ...rest.slice(index)];
}

export function libraryTimeColumn(column: LibraryColumn) {
  return column === "edited" || column === "visited" || column === "created";
}

/** A column sorted from its header's "정렬": text A → Z or back, times old → new or back. */
export type LibrarySort = { column: Exclude<LibraryColumn, "source">; direction: "ascending" | "descending" };

/**
 * The view's pages in its sort: names and people as text, times as times,
 * with what has no value last either way.
 */
export function sortLibraryPages(
  pages: readonly Page[],
  sorts: readonly LibrarySort[],
  keys: { creator(page: Page): string; lastEditor(page: Page): string; visited(page: Page): string },
): Page[] {
  const value = (page: Page, sort: LibrarySort): string | number => {
    if (sort.column === "name") return pageNamesNow.title(page);
    if (sort.column === "creator") return keys.creator(page);
    if (sort.column === "lastEditor") return keys.lastEditor(page);
    const time = sort.column === "edited" ? page.updatedAt : sort.column === "created" ? page.createdAt : keys.visited(page);
    return time ? Date.parse(time) || 0 : 0;
  };
  if (sorts.length === 0) return [...pages];
  const empty = (key: string | number) => key === "" || key === 0;
  const collator = new Intl.Collator(undefined, { sensitivity: "base", numeric: true });
  const compare = (a: string | number, b: string | number, sort: LibrarySort) => {
    if (empty(a) || empty(b)) return empty(a) === empty(b) ? 0 : empty(a) ? 1 : -1;
    const order = typeof a === "number" && typeof b === "number" ? a - b : collator.compare(String(a), String(b));
    return order * (sort.direction === "ascending" ? 1 : -1);
  };
  // Each page's values read once, not at each comparison (thousands of pages).
  const rows = pages.map((page) => ({ page, values: sorts.map((sort) => value(page, sort)) }));
  // Each sort settles what the one before it leaves even.
  rows.sort((left, right) => {
    for (let at = 0; at < sorts.length; at += 1) {
      const order = compare(left.values[at], right.values[at], sorts[at]);
      if (order) return order;
    }
    return 0;
  });
  return rows.map((row) => row.page);
}

/** A row of the library's table: a page at its depth, or the "새 페이지 추가하기" row ending an open page's pages. */
export type LibraryTreeRow =
  | { kind: "page"; page: Page; depth: number }
  | { kind: "add"; parentId: string; depth: number };

const childrenOfList = new WeakMap<readonly Page[], ReadonlyMap<string, readonly Page[]>>();
const parentsOfList = new WeakMap<readonly Page[], ReadonlySet<string>>();

/**
 * The pages with pages of their own out of the trash, worked out once for a
 * list: a row's toggle needs no more until it is opened (libraryChildPages).
 */
export function libraryParentIds(pages: readonly Page[]): ReadonlySet<string> {
  let parents = parentsOfList.get(pages);
  if (!parents) {
    const ids = new Set<string>();
    for (const page of pages) if (page.parentId && !page.inTrash) ids.add(page.parentId);
    parents = ids;
    parentsOfList.set(pages, parents);
  }
  return parents;
}

/** No page's pages: the rows' tree while no row is open. */
export const NO_LIBRARY_CHILDREN: ReadonlyMap<string, readonly Page[]> = new Map();

/** Each page's own pages out of the trash, in their order, worked out once for a list. */
export function libraryChildPages(pages: readonly Page[]): ReadonlyMap<string, readonly Page[]> {
  const kept = childrenOfList.get(pages);
  if (kept) return kept;
  const children = new Map<string, Page[]>();
  for (const page of pages) {
    if (!page.parentId || page.inTrash) continue;
    const list = children.get(page.parentId);
    if (list) list.push(page);
    else children.set(page.parentId, [page]);
  }
  for (const list of children.values()) list.sort((left, right) => left.position - right.position);
  childrenOfList.set(pages, children);
  return children;
}

/**
 * The table's rows, as Notion's "열기" opens a row (seen 2026-10-02): an
 * open page's own pages follow it one step in, each openable too, then a
 * "새 페이지 추가하기" row. A page opens once on its own branch.
 */
export function libraryTreeRows(
  rows: readonly Page[],
  children: ReadonlyMap<string, readonly Page[]>,
  open: ReadonlySet<string>,
): LibraryTreeRow[] {
  const tree: LibraryTreeRow[] = [];
  const visit = (page: Page, depth: number, branch: ReadonlySet<string>) => {
    tree.push({ kind: "page", page, depth });
    if (!open.has(page.id) || branch.has(page.id)) return;
    const next = new Set(branch).add(page.id);
    for (const child of children.get(page.id) ?? []) visit(child, depth + 1, next);
    tree.push({ kind: "add", parentId: page.id, depth: depth + 1 });
  };
  for (const page of rows) visit(page, 0, new Set());
  return tree;
}

/** The pages whose title holds the search, ignoring case. */
export function searchLibraryPages(pages: readonly Page[], query: string): Page[] {
  const needle = query.trim().toLocaleLowerCase();
  if (!needle) return [...pages];
  return pages.filter((page) => pageNamesNow.title(page).toLocaleLowerCase().includes(needle));
}

/**
 * A database row's page as the library lists it, from what the server
 * read of it: its title, icon, times and people, under the page holding
 * its database, which is its "출처".
 */
export function libraryRowPage(detail: LibraryPage, workspaceId: string): Page | null {
  // People's own database page lists as a row's page does, a database by Leafen that opens as People (isPeopleDatabasePage).
  const people = !detail.row && Boolean(detail.peopleDatabase);
  const row = detail.row ?? detail.peopleDatabase;
  if (!row) return null;
  return {
    id: detail.pageId, workspaceId, teamspaceId: "", title: row.title, untitledName: row.untitledName, content: "", revision: 0, cursor: 0,
    ...(row.titleSpans?.length ? { titleSpans: row.titleSpans } : {}),
    // An empty one is the empty document, as the sidebar's pages are (PageIcon).
    ...(row.hasContent === undefined ? {} : { hasContent: row.hasContent }),
    parentId: row.containerPageId, position: 0, isLocked: false, inTrash: false,
    icon: row.icon, iconType: row.iconType || "none", cover: "", coverPosition: 50,
    kind: people ? "database" : "page", createdAt: row.createdAt, updatedAt: row.updatedAt, createdBy: row.createdBy, lastEditedBy: row.lastEditedBy,
  } as Page;
}

/**
 * Where a database row's or template's page listed among visits or favorites
 * opens: a row's full in its database, as Notion's library opens it; a
 * template's in its window there (useTemplateAddress), or whole at its own
 * address where it is opened as its page — Home's card, as Notion's (seen 2026-11-11). A People
 * row opens as its person's profile from the first frame: through People's
 * own page, which no reader's page list holds, the app first asked whether
 * that page was a row (two "not found" answers) and showed a loading frame
 * till the row named its person (usePersonRowPage).
 */
export function libraryRowHref(detail: LibraryPage, templateFull = false): string {
  if (detail.peopleDatabase) return PEOPLE_DIRECTORY_HREF;
  const row = detail.row;
  if (row?.person && !row.template) return personProfileHref(row.person);
  if (row?.template && templateFull) return templateAddress(detail.pageId);
  return pageHref(row?.containerPageId ?? "", new URLSearchParams(!row?.template ? { p: detail.pageId, pm: "f" } : { [TEMPLATE_PAGE_PARAM]: detail.pageId }));
}

/**
 * The address a row's or template's page listed among visits or favorites is
 * copied as: a People row's its person's page (personPageHref), as Notion's
 * favourite person copies; People's own its page's, which opens People, as
 * People's bar copies it; a template's its own, as its link copies it (templateAddress); any other where it opens (libraryRowHref).
 */
export function libraryRowLink(detail: LibraryPage): string {
  if (detail.peopleDatabase) return pageHref(detail.pageId);
  if (detail.row?.template) return templateAddress(detail.pageId);
  return detail.row?.person && !detail.row.template ? personPageHref(detail.pageId) : libraryRowHref(detail);
}
