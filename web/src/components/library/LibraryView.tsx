import { Fragment, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useDragReorder } from "@/lib/useDragReorder";
import { usePagesWithEditTimes } from "@/lib/pageEditedHere";
import { useTranslation } from "react-i18next";
import type { GraphView } from "@/contentGraph";
import { advancedFilterParts } from "@/components/database/DatabaseAdvancedFilter";
import { DatabaseFilterMenu } from "@/components/database/DatabaseFilterSettings";
import { PhoneFilterList, phoneFilterCount } from "@/components/database/DatabasePhoneFilterList";
import { DatabaseQuickFilterBar, databaseToolbarProperties } from "@/components/database/DatabaseQuickFilterBar";
import { DatabaseSortSettings, viewSorts } from "@/components/database/DatabaseSortSettings";
import { nativeDatabaseFilterSnapshot } from "@/components/database/databaseFilterSnapshot";
import { FilterIcon, SlidersIcon, SortIcon } from "@/icons/leafen";
import { TopBar } from "@/components/page/TopBar";
import { useSidebarLayout as useShellLayout } from "@/components/shell/AppShell";
import { SIDEBAR_HOME_TAB, setSidebarSectionHidden, sidebarShownSections } from "@/components/sidebar/sidebarLayoutModel";
import { useSidebarLayout } from "@/components/sidebar/useSidebarLayout";
import { useSidebarTeamspaces } from "@/components/sidebar/useSidebarTeamspaces";
import { setDocumentChrome } from "@/lib/documentChrome";
import { useStoredIconFaviconHref } from "@/components/page/pageFavicon";
import { pageHref } from "@/lib/navigation";
import { archivedPageIds } from "@/lib/pageArchive";
import { openPagePeek } from "@/lib/pagePeek";
import { libraryHref, pushRoute, replaceRoute, type LibraryViewId } from "@/lib/router";
import type { FilterGroup, WorkspaceMember } from "@/lib/types";
import type { Page, Session, Workspace } from "@/types";
import { LibraryColumnMenu } from "./LibraryColumnMenu";
import { LibraryEmpty, type LibraryZeroView } from "./LibraryEmpty";
import { LibraryFilterMenu, LibraryToolMenu, libraryMenuEnd } from "./LibraryFilterMenu";
import {
  filterLibraryPages,
  isLibraryFilterColumn,
  libraryFilterProperties,
  libraryFilterView,
  type LibraryQuickFilter,
} from "./libraryFilters";
import { groupLibraryPages, isLibraryGroupColumn } from "./libraryGrouping";
import { LibraryGroupRow, LibraryRows, LibrarySelectBox } from "./LibraryRows";
import { LibrarySearch } from "./LibrarySearch";
import { LibrarySelectionBar, type LibraryPageActions } from "./LibrarySelectionBar";
import { LibrarySettingsPanel } from "./LibrarySettingsPanel";
import { LibrarySettingsSheet } from "./LibrarySettingsSheet";
import { useLibraryStickyHead } from "./useLibraryStickyHead";
import {
  libraryChildPages,
  libraryParentIds,
  NO_LIBRARY_CHILDREN,
  libraryRowHref, libraryRowLink,
  libraryRowPage,
  libraryTreeRows,
  libraryViewPages,
  librarySettingsItems,
  defaultLibrarySort,
  flipSet,
  libraryViews,
  moveLibraryColumn,
  searchLibraryPages,
  sortLibraryPages,
  type LibrarySort,
  libraryColumnsWith,
  libraryHiddenColumns,
  type LibraryColumn,
  type LibraryInput,
} from "./libraryModel";
import { LibraryDocuments } from "./LibraryDocuments";
import { LibraryTeamspaces } from "./LibraryTeamspaces";
import { LibraryViewTabs, type LibraryViewListActions } from "./LibraryViewTabs";
import { useLibraryRowEdits } from "./LibraryRowEdits";
import { libraryFavoriteOrder, useLibraryRowOrder } from "./useLibraryRowOrder";
import { useLibraryCellChoice } from "./useLibraryCellChoice";
import { useLibraryPages, useLibraryViewOrder, usePageVisits } from "./useLibraryData";
import { useLibrarySectionSort } from "./useLibrarySectionSort";
import { lastLibraryView, rememberLibraryView } from "./libraryViewOrder";
import panelStyles from "./LibraryPanel.module.css";
import styles from "./LibraryView.module.css";
import { pageMapOf } from "@/lib/pageIndex";
import { afterNextPaint } from "@/lib/afterPress"; import { addFailedNotice } from "@/components/sidebar/addFailedNotice";

/** The view "보기 편집" opened, whose settings open once it is drawn (each view is its own route). */
let settingsCarried: LibraryViewId | null = null;

// What a view that doesn't read them is given in their place.
const NO_VISITS: LibraryInput["visits"] = [];
const NO_PAGE_IDS: readonly string[] = [];
const NO_PAGES: readonly Page[] = [];
const NO_ROW_PAGES: ReadonlyMap<string, Page> = new Map();

/** Rows shown at first and added by "더 보기". */
const LIBRARY_ROWS = 100;
// The library's first frame draws the rows the window can show, at least this
// tall each, and the rest come in the frame after: a hundred rows drawn at
// once held the press that opened it (2026-11-14).
const FIRST_FRAME_ROW = 32;
const NAME_WIDTH = 420;
const COLUMN_WIDTH = 200;
/** The lines' run past the last column, as Notion's library table. */
const TABLE_END = 24;

/**
 * The library, as Notion's (`/library/…`, seen 2026-10-01 and 10-02): its
 * title with "새 페이지", its views as tabs with "필터", "정렬", "검색" and
 * "설정" at the row's right — a phone's "검색", "필터" and "설정", as
 * Notion's phone (seen 2026-11-12) — and a table of pages — name, creator, where it lives, when
 * it was last edited and when the reader last visited it, and, shown from
 * "설정", when it was made and who edited it last. "필터", a column's
 * "필터" or the settings' "필터" add a filter pill under the tabs, as a
 * database's, and a column's "그룹화" groups the rows. A header dragged
 * moves its column. A row with pages of its own opens them in place. Rows
 * are chosen by their boxes in the gutter, which bring up their bar.
 * Notion's advanced filters are not here.
 */
export function LibraryView({
  session,
  workspace,
  pages: givenPages,
  favoritePageIds,
  sharedPageIds,
  view,
  canCreatePage,
  onCreatePage,
  onCreateSubpage,
  onOpenPage,
  actions: pageActions,
}: {
  session: Session;
  workspace: Workspace;
  /** The workspace's pages the reader has, out of the trash. */
  pages: readonly Page[];
  favoritePageIds: readonly string[];
  sharedPageIds: readonly string[];
  /** None for "/library" alone, which opens on the tab last seen. */
  view?: LibraryViewId;
  canCreatePage: boolean;
  /** Makes a page at the top of the workspace, or of a teamspace. */
  onCreatePage: (teamspaceId?: string) => void;
  /** Makes a page inside another; a database takes a row instead and gives none back. */
  onCreateSubpage: (parentId: string) => Promise<Page | null>;
  onOpenPage: (page: Page, newTab: boolean) => void;
  /** What chosen rows' bar does to their pages. */
  actions: LibraryPageActions;
}) {
  const { t } = useTranslation(["library", "common", "pageTreeItem"]);
  // Each page's edit time as last known, an edit here in a peek with it (lib/pageEditedHere.ts).
  const pages = usePagesWithEditTimes(givenPages);
  const [views, moveView] = useLibraryViewOrder(session.user.id, workspace.id, libraryViews(workspace.guest === true));
  // "/library" alone opens on the tab last seen, as Notion's, and its
  // address names it; a view the reader hasn't, a guest's "최근" say, opens
  // where the library opens.
  const guest = workspace.guest === true;
  const named = useMemo(() => view ?? lastLibraryView(session.user.id, { id: workspace.id, guest }),
    [guest, session.user.id, view, workspace.id]);
  const current = views.includes(named) ? named : guest ? "shared" : views[0] ?? "recents";
  useEffect(() => {
    if (!view) replaceRoute(libraryHref(current));
    rememberLibraryView(session.user.id, workspace.id, current);
  }, [current, session.user.id, view, workspace.id]);
  const sidebar = useSidebarLayout(session, workspace.id);
  const sidebarSections = sidebarShownSections(sidebar.layout, SIDEBAR_HOME_TAB);
  const visits = usePageVisits(session, workspace.id);
  const { joinedTeamspaces } = useSidebarTeamspaces({ session, workspace });
  const [shown, setShown] = useState(LIBRARY_ROWS);
  const [firstFrame, setFirstFrame] = useState(true);
  useEffect(() => firstFrame ? afterNextPaint(() => setFirstFrame(false)) : undefined, [firstFrame]);
  const viewSort = useLibrarySectionSort({ userId: session.user.id, workspaceId: workspace.id, view: current, sidebar, keepShownOrder: () => rowOrder.keepShownOrder() });
  const { settings, change: changeSettings } = viewSort;
  const hiddenColumns = libraryHiddenColumns(current, settings.columns, settings.hidden);
  // A view with "보관된 페이지 포함" lists the archived pages, and those under
  // one, only while it is on, as Notion's (its archive status "NON_ARCHIVED"
  // at first, read 2026-11-14) and as the sidebar leaves them out.
  const settingsItems = librarySettingsItems(current);
  const withArchived = settingsItems.includes("archived") && settings.includeArchived === true;
  // Worked out only for a view that leaves the archived pages out.
  const hidesArchived = settingsItems.includes("archived") && !withArchived;
  const listedPages = useMemo(() => {
    const archived = hidesArchived ? archivedPageIds(pages) : null;
    return archived?.size ? pages.filter((page) => !archived.has(page.id)) : pages;
  }, [hidesArchived, pages]);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [searching, setSearching] = useState(false);
  // A narrow desktop row keeps only "설정", as Notion's; a phone's is never narrow.
  const [narrow, setNarrow] = useState(false);
  const { phone } = useShellLayout(true);
  const [query, setQuery] = useState("");
  const [open, setOpen] = useState<ReadonlySet<string>>(() => new Set());
  const [addFailed, setAddFailed] = useState<string | null>(null); // What adding inside a row last said when it couldn't (addFailedNotice).
  const [collapsedGroups, setCollapsedGroups] = useState<ReadonlySet<string>>(() => new Set());
  const [selected, setSelected] = useState<ReadonlySet<string>>(() => new Set());
  // A row's right click chooses it, unless it is chosen already, and opens the
  // chosen pages' menu where it was pressed (Notion's library, seen 2026-11-05).
  const [rowMenu, setRowMenu] = useState<{ x: number; y: number; at: number; pressed?: boolean; beside?: DOMRect } | null>(null);
  // "필터" asks which column to filter by; a column picked opens its pill.
  const [filterMenuOpen, setFilterMenuOpen] = useState(false);
  const [sortMenuOpen, setSortMenuOpen] = useState(false);
  const [sortRequest, setSortRequest] = useState(0);
  const [menuEnd, setMenuEnd] = useState(0);
  // A view's own sort alone (its section's, say) keeps its bar folded at first, as Notion's.
  const ownSortOnly = () => !settings.filters.length && (viewSort.follows || (!settings.moreSorts?.length && JSON.stringify(settings.sort) === JSON.stringify(defaultLibrarySort(current))));
  const [filterBarHidden, setFilterBarHidden] = useState(ownSortOnly);
  const [filterRequest, setFilterRequest] = useState<{ propertyId: string; at: number } | null>(null);
  const [detailIds, setDetailIds] = useState<string[]>([]);
  const details = useLibraryPages(session, workspace.id, detailIds);
  const visitedAt = new Map(visits.map((visit) => [visit.pageId, visit.visitedAt]));
  const visited = (page: Page) => visitedAt.get(page.id) || details.pages.get(page.id)?.visitedAt || "";
  // The database rows' and templates' pages visited or among favorites aren't
  // in the page list: the server reads what the library shows of them, as
  // Notion's "최근" and favorites list them too.
  const known = pageMapOf(pages);
  const unlistedVisits = (current === "recents" ? visits.map((visit) => visit.pageId)
    : current === "favorites" ? favoritePageIds : []).filter((id) => !known.has(id));
  const unlistedKey = unlistedVisits.join("\u0000");
  const rowPages = useMemo(() => new Map(unlistedVisits.flatMap((id) => {
    const detail = details.pages.get(id);
    const row = detail ? libraryRowPage(detail, workspace.id) : null;
    return row ? [[id, row] as const] : [];
  })), [details.pages, unlistedKey, workspace.id]); // eslint-disable-line react-hooks/exhaustive-deps -- unlistedKey names unlistedVisits
  const rowEdits = useLibraryRowEdits({ workspaceId: workspace.id, actions: pageActions, rowPageIds: rowPages,
    onEnter: (page) => cells.chooseBelow(page.id) });
  // The people the rows name, by what the server read of them (the pages it
  // read, not every page at each drawing).
  const people = new Map<string, string>();
  for (const [id, detail] of details.pages) {
    const page = known.get(id) ?? rowPages.get(id);
    if (!page) continue;
    if (detail.creatorName && page.createdBy) people.set(page.createdBy, detail.creatorName);
    if (detail.lastEditorName && page.lastEditedBy) people.set(page.lastEditedBy, detail.lastEditorName);
  }
  const headRef = useRef<HTMLDivElement>(null);
  useLibraryStickyHead(headRef);
  // "최근" is sorted by its visits at first (defaultLibrarySort), changed on a phone alone.
  const sorts: LibrarySort[] = [...(settings.sort ? [settings.sort] : []), ...(settings.moreSorts ?? [])];
  // The view's pages, worked out as its list changes, not at each drawing,
  // nor as what only another view reads comes: the visits are "최근"'s, the
  // favorites theirs, the rows' pages read for those two theirs (2026-11-15:
  // the private pages of a 44,000-page workspace worked out again, 7 ms at
  // CPU ×2, as the rows' details came in the library's first frame).
  const ownVisits = current === "recents" ? visits : NO_VISITS;
  const ownFavorites = useMemo(() => current === "favorites" ? libraryFavoriteOrder(favoritePageIds, sidebar.layout.favoriteOrder) : NO_PAGE_IDS,
    [current, favoritePageIds, sidebar.layout.favoriteOrder]);
  const ownRowPages = current === "recents" || current === "favorites" ? rowPages : NO_ROW_PAGES;
  const viewPages = useMemo(() => libraryViewPages(current, {
    pages: listedPages, favoritePageIds: ownFavorites, sharedPageIds, visits: ownVisits, rowPages: ownRowPages, withArchived,
  }), [current, listedPages, ownFavorites, ownRowPages, ownVisits, sharedPageIds, withArchived]);
  const listed = filterLibraryPages(searchLibraryPages(sortLibraryPages(
    viewPages, sorts, {
      creator: (page) => details.pages.get(page.id)?.creatorName ?? "",
      lastEditor: (page) => details.pages.get(page.id)?.lastEditorName ?? "",
      visited,
    },
  ), query), settings.filters, {
    visited, personName: (userId) => people.get(userId) ?? "", currentUserId: session.user.id,
    timeZone: Intl.DateTimeFormat().resolvedOptions().timeZone,
  }, settings.filterGroup);
  // Which pages have pages of their own shows on each row; the pages under
  // them are worked out once a row is opened — on opening, the whole list's
  // took the first frame of a 44,000-page workspace's library some 23 ms at CPU ×2 (2026-11-15).
  const parents = useMemo(() => libraryParentIds(listedPages), [listedPages]);
  const anyOpen = open.size > 0;
  const children = useMemo(() => (anyOpen ? libraryChildPages(listedPages) : NO_LIBRARY_CHILDREN), [anyOpen, listedPages]);
  const shownPages = listed.slice(0, shown);
  const groups = settings.groupBy ? groupLibraryPages(shownPages, settings.groupBy, t(`library:columns.${settings.groupBy}`), {
    visited, currentUserId: session.user.id,
    creator: (page) => details.pages.get(page.id)?.creatorName ?? "",
    lastEditor: (page) => details.pages.get(page.id)?.lastEditorName ?? "",
  }).map((group) => ({ ...group, tree: libraryTreeRows(group.pages, children, open) })) : null;
  const tree = groups ? groups.flatMap((group) => group.tree) : libraryTreeRows(shownPages, children, open);
  const shownRows = tree.flatMap((row) => (row.kind === "page" ? [row.page] : []));
  const rowOrder = useLibraryRowOrder({ view: current, sidebar, favoritePageIds, privatePages: current === "private" ? viewPages : NO_PAGES,
    shownTops: tree.flatMap((row) => (row.kind === "page" && row.depth === 0 ? [row.page] : [])), sorted: sorts.length > 0, actions: pageActions,
    onManual: viewSort.setManual });
  const cells = useLibraryCellChoice({ rowIds: shownRows.map((page) => page.id), columns: settings.columns, onChooseRow: (id) => setSelected(new Set([id])),
    onRename: (id, cell) => { const page = shownRows.find((row) => row.id === id); if (page) rowEdits.editName(page, cell); } });
  const rowKey = shownRows.map((page) => page.id).join("\u0000");
  // The chosen rows are those still shown, in their order, a page open
  // under another as well as listed counted once.
  const chosen = [...new Map(shownRows.filter((page) => selected.has(page.id)).map((page) => [page.id, page])).values()];
  const allChosen = shownRows.length > 0 && shownRows.every((page) => selected.has(page.id));
  // The rows shown, and the visited pages the list lacks, are the ones read.
  useEffect(() => setDetailIds([...new Set([
    ...(rowKey ? rowKey.split("\u0000") : []), ...(unlistedKey ? unlistedKey.split("\u0000") : []),
  ])]), [rowKey, unlistedKey]);

  const favicon = useStoredIconFaviconHref(workspace.icon, workspace.id);
  useEffect(() => {
    setDocumentChrome({ title: t("library:title"), iconHref: favicon });
  }, [t, favicon]);
  useEffect(() => {
    setShown(LIBRARY_ROWS);
    setSelected(new Set());
    setFilterBarHidden(ownSortOnly());
  }, [current]); // eslint-disable-line react-hooks/exhaustive-deps -- only as the view changes
  useEffect(() => {
    if (settingsCarried !== current) return;
    settingsCarried = null;
    setSettingsOpen(true);
    // Only as the view is first drawn.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  useEffect(() => {
    if (!narrow) return;
    setQuery("");
    setSearching(false);
    setFilterMenuOpen(false);
  }, [narrow]);

  // The list of views' handles move a view, its "보기 편집" opens the view
  // with its settings, and its section shows or hides in the sidebar.
  const viewList: LibraryViewListActions = {
    onMove: moveView,
    onEdit: (next) => {
      if (next === current) {
        setSettingsOpen(true);
        return;
      }
      settingsCarried = next;
      pushRoute(libraryHref(next));
    },
    sidebarShown: (entry) => sidebarSections.includes(entry),
    onSidebar: (entry, shown) => sidebar.change(setSidebarSectionHidden(sidebar.layout, SIDEBAR_HOME_TAB, entry, !shown)),
  };

  const sourceNames = {
    teamspace: (id: string) => joinedTeamspaces.find((teamspace) => teamspace.id === id)?.name ?? t("library:sources.teamspace"),
    private: t("library:sources.private"),
  };
  const label = (column: LibraryColumn) => t(`library:columns.${column}`);
  const filterProperties = useMemo(() => libraryFilterProperties((column) => t(`library:columns.${column}`)), [t]);
  const filterView = libraryFilterView(current, t(`library:views.${current}`), settings.filters, settings.filterGroup, sorts);
  const peopleKey = JSON.stringify([...people]);
  const filterSnapshot = useMemo(() => ({
    ...nativeDatabaseFilterSnapshot({
      currentUserId: session.user.id, graph: { properties: filterProperties, rows: [] },
      snapshotIdentity: `library:${peopleKey}`, workspaceId: workspace.id,
    }),
    workspaceMembers: (JSON.parse(peopleKey) as Array<[string, string]>).map(([userId, displayName]) => (
      { workspaceId: workspace.id, userId, displayName, role: "member" } as WorkspaceMember
    )),
  }), [filterProperties, peopleKey, session.user.id, workspace.id]);

  // The advanced filter, as a database's toolbar edits it.
  const advanced = advancedFilterParts({
    view: filterView, properties: databaseToolbarProperties(filterProperties), snapshot: filterSnapshot,
    onUpdateView: (next) => changeFilterView(next),
  });
  const filterMenu = (
    <DatabaseFilterMenu properties={databaseToolbarProperties(filterProperties)} writable advancedCount={advanced.count}
      advancedEditor={advanced.editor} onAddAdvanced={advanced.add} onPick={(column) => addFilter(column)} />
  );
  // A phone's filters and sorts are the database's phone lists, in "필터" and in "설정" alike.
  const phoneFilters = (onSort?: () => void) => <PhoneFilterList view={filterView} properties={databaseToolbarProperties(filterProperties)}
    snapshot={filterSnapshot} editable advanced={advanced} onSort={onSort} onUpdateView={changeFilterView} />;
  const phoneSorts = <DatabaseSortSettings view={filterView} properties={databaseToolbarProperties(filterProperties)} onUpdateView={changeFilterView} sheet />;

  function addFilter(column: string) {
    setFilterMenuOpen(false);
    setFilterBarHidden(false);
    setFilterRequest({ propertyId: column, at: Date.now() });
  }

  // The bar and the advanced filter change the view's quick filters and its group.
  function changeFilterView(next: GraphView) {
    const quick = next.config.quickFilters;
    const group = next.config.filterGroup as FilterGroup | undefined;
    const nextSorts = viewSorts(next).map(({ propertyId, direction }) => ({
      column: propertyId as LibrarySort["column"], direction: direction === "desc" ? "descending" as const : "ascending" as const,
    }));
    changeSettings({
      ...settings,
      filters: (Array.isArray(quick) ? quick : []) as LibraryQuickFilter[],
      filterGroup: group && Array.isArray(group.filters) && (group.filters.length > 0 || (group.groups ?? []).length > 0) ? group : null,
      sort: nextSorts[0] ?? null,
      moreSorts: nextSorts.slice(1),
    });
  }

  function closeSearch() {
    setQuery("");
    setSearching(false);
  }

  const toggleGroup = (key: string) => setCollapsedGroups((current) => flipSet(current, key));

  // A header dragged over another lands before or after it, by which half
  // the pointer is over; the page name stays first.
  const headerReorder = useDragReorder("library-header", (dragged, target, side) => changeSettings({
    ...settings,
    columns: moveLibraryColumn(settings.columns, dragged as LibraryColumn, target as LibraryColumn, target === "name" || side === "after"),
  }), { axis: "horizontal" });
  const headerDrag = (column: LibraryColumn) => {
    const { "data-drop-side": side, ...props } = headerReorder.row(column, () => true, column !== "name");
    return { ...props, "data-library-drop": column === "name" && side ? "after" : side };
  };

  const lastChosen = useRef<string | null>(null);
  function choose(pageId: string, next: boolean, range = false) {
    const from = range && lastChosen.current ? shownRows.findIndex((page) => page.id === lastChosen.current) : -1;
    const to = shownRows.findIndex((page) => page.id === pageId);
    const ids = from >= 0 && to >= 0
      ? shownRows.slice(Math.min(from, to), Math.max(from, to) + 1).map((page) => page.id)
      : [pageId];
    lastChosen.current = pageId;
    setSelected((current) => {
      const updated = new Set(current);
      for (const id of ids) {
        if (next) updated.add(id);
        else updated.delete(id);
      }
      return updated;
    });
  }

  const takeRowMenu = useCallback(() => setRowMenu(null), []);
  function openRowMenu(pageId: string, at: { x: number; y: number; pressed?: boolean; beside?: DOMRect }) {
    setSelected((current) => (current.has(pageId) ? current : new Set([pageId])));
    setRowMenu({ ...at, at: Date.now() });
  }

  const toggle = (pageId: string) => setOpen((current) => flipSet(current, pageId));

  // A row's or template's page opens in its database (libraryRowHref); the
  // rows' pages listed are those read, so each has its detail.
  const rowHref = (page: Page) => {
    const detail = details.pages.get(page.id);
    return detail ? libraryRowHref(detail) : pageHref(page.id);
  };
  function openPage(page: Page, newTab: boolean) {
    if (!rowPages.has(page.id)) {
      onOpenPage(page, newTab);
      return;
    }
    const href = rowHref(page);
    if (newTab) window.open(href, "_blank", "noopener,noreferrer");
    else pushRoute(href);
  }

  function addInside(parentId: string) {
    setAddFailed(null);
    void onCreateSubpage(parentId)
      .then((page) => {
        // A database took a row, which opened in it.
        if (page) onOpenPage(page, false);
      })
      .catch((error: unknown) => setAddFailed(addFailedNotice(error, t("pageTreeItem:pageAccessRequired"))[0]));
  }

  const filterTool = (
    <span className={panelStyles.filterWrap}>
      <button type="button" className={panelStyles.tabAction} aria-label={t("library:filter")} title={t("library:filter")}
        aria-expanded={filterMenuOpen} data-active={settings.filters.length > 0 || advanced.count > 0 || phone && sorts.length > 0 ? "true" : undefined}
        data-library-filter-open
        onClick={(event) => {
          // With filters, "필터" shows or hides their bar, as a database's does; a phone's opens its sheet.
          if (settings.filters.length > 0 && !phone) { setFilterBarHidden((value) => !value); return; }
          setMenuEnd(libraryMenuEnd(event.currentTarget));
          setFilterMenuOpen((value) => !value);
        }}>
        <FilterIcon size="small" aria-hidden="true" />
      </button>
      {filterMenuOpen ? (
        <LibraryFilterMenu phone={phone} label={t("library:addFilter")} closeLabel={t("library:closeFilterMenu")}
          filterCount={phoneFilterCount(filterView, advanced.count)} sortCount={sorts.length} menu={filterMenu} phoneList={phoneFilters}
          phoneSorts={phoneSorts} end={menuEnd} onClose={() => setFilterMenuOpen(false)} />
      ) : null}
    </span>
  );
  const sortTool = (
    <span className={panelStyles.filterWrap}>
      {/* "최근" has no sort button, as Notion's desktop (seen 2026-11-05 and 2026-11-16); its phone's filter sheet has its "정렬". */}
      {current === "recents" ? null : <button type="button" className={panelStyles.tabAction} aria-label={t("library:sort")} title={t("library:sort")}
        aria-expanded={sortMenuOpen} data-active={sorts.length ? "true" : undefined} data-library-sort-open
        onClick={(event) => {
          // With sorts, "정렬" hides their bar, or shows it with the first sort's editor open, as Notion's.
          if (sorts.length) { setFilterBarHidden(!filterBarHidden); if (filterBarHidden) setSortRequest((count) => count + 1); return; }
          setMenuEnd(libraryMenuEnd(event.currentTarget));
          setSortMenuOpen((value) => !value);
        }}>
        <SortIcon size="small" aria-hidden="true" />
      </button>}
      {sortMenuOpen && current !== "recents" ? (
        <LibraryToolMenu label={t("library:sort")} closeLabel={t("library:closeFilterMenu")} end={menuEnd} marker="data-library-sort-menu"
          onClose={() => setSortMenuOpen(false)}>
          {/* The first sort picked hands over to its pill's editor, as Notion's (seen 2026-11-05). */}
          <DatabaseSortSettings view={filterView} properties={databaseToolbarProperties(filterProperties)} onUpdateView={changeFilterView}
            onPicked={() => { setSortMenuOpen(false); setFilterBarHidden(false); setSortRequest((count) => count + 1); }} />
        </LibraryToolMenu>
      ) : null}
    </span>
  );
  const searchTool = (
    <LibrarySearch open={searching} value={query} placeholder={t("library:searchPlaceholder")} onOpen={() => setSearching(true)}
      onChange={setQuery} onClose={closeSearch} />
  );
  const settingsTool = (
    <button type="button" className={panelStyles.tabAction} aria-label={t("library:settings")} title={t("library:settings")}
      aria-expanded={settingsOpen} data-library-settings-open onClick={() => setSettingsOpen((value) => !value)}>
      <SlidersIcon size="small" aria-hidden="true" />
    </button>
  );
  // A phone's are a sheet, as Notion's phone (LibrarySettingsSheet).
  const settingsPanel = settingsOpen ? phone ? (
    <LibrarySettingsSheet items={settingsItems} columns={settings.columns} hidden={hiddenColumns} label={label} filterProperties={filterProperties}
      filters={phoneFilters()} filterCount={phoneFilterCount(filterView, advanced.count)}
      sorts={phoneSorts}
      sortCount={sorts.length} sortLabel={sorts[0] ? label(sorts[0].column) : undefined}
      groupBy={settings.groupBy} includeArchived={settings.includeArchived === true}
      onChange={(columns) => changeSettings({ ...settings, ...columns })} onGroup={(groupBy) => changeSettings({ ...settings, groupBy })}
      onIncludeArchived={(includeArchived) => changeSettings({ ...settings, includeArchived })} onClose={() => setSettingsOpen(false)} />
  ) : (
    <LibrarySettingsPanel items={settingsItems} columns={settings.columns} hidden={hiddenColumns} label={label} filterProperties={filterProperties} filterMenu={filterMenu}
      groupBy={settings.groupBy} sortCount={sorts.length} includeArchived={settings.includeArchived === true}
      sortSettings={<DatabaseSortSettings view={filterView} properties={databaseToolbarProperties(filterProperties)} onUpdateView={changeFilterView} />}
      onChange={(columns) => changeSettings({ ...settings, ...columns })} onGroup={(groupBy) => changeSettings({ ...settings, groupBy })}
      onIncludeArchived={(includeArchived) => changeSettings({ ...settings, includeArchived })} onClose={() => setSettingsOpen(false)} />
  ) : null;
  // A phone's row has "검색" first and no "정렬", as Notion's phone (seen 2026-11-12).
  const actions = (
    <div className={panelStyles.tabActions} data-library-tab-actions>
      {phone ? <>{searchTool}{filterTool}</> : narrow ? null : <>{filterTool}{sortTool}{searchTool}</>}
      {settingsTool}
    </div>
  );

  // The documents are a list of their own.
  if (current === "documents") {
    return <LibraryDocuments session={session} workspace={workspace} pages={pages} views={views} list={viewList} canCreatePage={canCreatePage}
      onCreatePage={() => onCreatePage()} />;
  }

  // The teamspaces are a table of their own, as Notion's.
  if (current === "teamspaces") {
    return <LibraryTeamspaces session={session} workspace={workspace} views={views} list={viewList} pages={listedPages} addFailed={addFailed}
      settingsTool={settingsTool} settingsPanel={settingsPanel} onOpenPage={openPage} onAddInside={addInside} />;
  }

  let firstFrameRows = firstFrame ? Math.ceil(window.innerHeight / FIRST_FRAME_ROW) : Infinity;
  return (
    <>
      {/* The bar names nothing; the page heads itself, as Notion's library (seen 2026-10-31). */}
      <TopBar title={t("library:title")} crumbs={<></>} />
      <div className={styles.wrap} data-library-view={current}>
        <header className={styles.header}>
          <h1 className={styles.title}>{t("library:title")}</h1>
          {canCreatePage ? (
            <button type="button" className={styles.newPage} data-library-new-page onClick={() => onCreatePage()}>
              {t("library:newPage")}
            </button>
          ) : null}
        </header>
        <LibraryViewTabs views={views} current={current} onSelect={(next) => pushRoute(libraryHref(next))} actions={actions}
          list={viewList} onNarrow={setNarrow} />
        {/* With no filter or sort it draws nothing: it comes after the first frame, which it held some 13 ms at CPU ×2. */}
        {firstFrame && !settings.filters.length && !sorts.length ? null : (
          <DatabaseQuickFilterBar view={filterView} properties={filterProperties} snapshot={filterSnapshot} writable
            hidden={filterBarHidden} request={filterRequest} onRequestDone={() => setFilterRequest(null)}
            sortRequest={sortRequest} onUpdateView={changeFilterView}
            ownQuery={(settings.filters.length || sorts.length) && !ownSortOnly() ? { onReset: () => {
              changeSettings({ ...settings, filters: [], filterGroup: null, sort: defaultLibrarySort(current), moreSorts: [] }, { reset: true });
            } } : null} />
        )}
        {chosen.length > 0 ? (
          <LibrarySelectionBar session={session} workspaceId={workspace.id} chosen={chosen} recents={current === "recents"}
            favorites={current === "favorites"} toTop={groups ? undefined : rowOrder.top}
            favoritePageIds={favoritePageIds} rowPageIds={new Set(rowPages.keys())} actions={pageActions}
            hrefOf={(page) => { const detail = rowPages.has(page.id) && details.pages.get(page.id); return detail ? libraryRowLink(detail) : pageHref(page.id); }}
            onOpenPage={openPage} onClear={() => setSelected(new Set())} menuRequest={rowMenu} onMenuRequestTaken={takeRowMenu} />
        ) : null}
        {rowEdits.overlays}
        <div className={panelStyles.body}>
          <div className={styles.tableScroll}>
            <div className={styles.table} role="table" aria-label={t("library:table", { view: t(`library:views.${current}`) })}
              style={{ width: NAME_WIDTH + (settings.columns.length - 1) * COLUMN_WIDTH + TABLE_END }}>
              <div ref={headRef} className={styles.headRow} role="row">
                <LibrarySelectBox checked={allChosen} indeterminate={chosen.length > 0 && !allChosen} shown={chosen.length > 0}
                  label={t("library:selection.selectAll")}
                  onChange={() => setSelected(allChosen ? new Set() : new Set(shownRows.map((page) => page.id)))} />
                {settings.columns.map((column) => (
                  <span key={column} className={column === "name" ? styles.nameColumn : styles.column} role="columnheader"
                    {...headerDrag(column)}>
                    <LibraryColumnMenu column={column} label={label(column)} sort={settings.sort}
                      wrapped={settings.wrap.includes(column)}
                      onFilter={isLibraryFilterColumn(column) ? () => addFilter(column) : undefined}
                      onSort={(sort) => changeSettings({ ...settings, sort, moreSorts: (settings.moreSorts ?? []).filter((other) => other.column !== sort?.column) })}
                      grouped={settings.groupBy === column}
                      onGroup={isLibraryGroupColumn(column)
                        ? () => changeSettings({ ...settings, groupBy: settings.groupBy === column ? null : column }) : undefined}
                      onHide={column === "name" ? undefined
                        : () => changeSettings({ ...settings, ...libraryColumnsWith({ columns: settings.columns, hidden: hiddenColumns }, column, false) })}
                      onWrap={(wrapped) => changeSettings({
                        ...settings, wrap: wrapped ? [...settings.wrap, column] : settings.wrap.filter((entry) => entry !== column),
                      })} />
                  </span>
                ))}
              </div>
              {(groups ?? [{ group: null, tree }]).map(({ group, tree: rows }) => {
                const collapsed = group ? collapsedGroups.has(group.key) : false;
                const drawn = rows.slice(0, firstFrameRows);
                if (!collapsed) firstFrameRows -= drawn.length;
                return (
                  <Fragment key={group?.key ?? "rows"}>
                    {group ? (
                      <LibraryGroupRow groupKey={group.key} label={group.label} count={rows.filter((row) => row.kind === "page" && row.depth === 0).length}
                        collapsed={collapsed} onToggle={() => toggleGroup(group.key)} />
                    ) : null}
                    {collapsed ? null : (
                      <LibraryRows tree={drawn} columns={settings.columns} pages={pages} sharedPageIds={sharedPageIds}
                        details={details.pages} visitedAt={visitedAt} sourceNames={sourceNames} wrapped={settings.wrap} open={open}
                        parents={parents} onToggle={toggle} onOpenPage={openPage} onAddInside={addInside}
                        onPeekPage={(page) => openPagePeek(page.id)}
                        selected={selected} onSelect={choose} onRowMenu={openRowMenu}
                        onEditName={rowEdits.editName} onEditIcon={rowEdits.editIcon} order={groups ? undefined : rowOrder.rows} chosenCell={cells.cell} />
                    )}
                  </Fragment>
                );
              })}
            </div>
            {listed.length === 0 ? (
              <LibraryEmpty view={current as LibraryZeroView} searching={query.trim() !== ""} filtered={settings.filters.length > 0}
                onClearSearch={() => setQuery("")} onEditFilters={() => setFilterBarHidden(false)}
                onCreatePage={canCreatePage ? () => onCreatePage() : undefined} />
            ) : null}
            {listed.length > shown ? (
              <button type="button" className={styles.showMore} onClick={() => setShown((value) => value + LIBRARY_ROWS)}>
                {t("library:showMore")}
              </button>
            ) : null}
            {details.failed ? <p className={styles.empty} role="status">{t("library:loadFailed")}</p> : null}
            {addFailed ? <p className={styles.empty} role="status">{addFailed}</p> : null}
          </div>
          {settingsPanel}
        </div>
      </div>
    </>
  );
}
