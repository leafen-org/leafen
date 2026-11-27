import { useFormFavorites } from "@/lib/formFavorites";
import { SidebarFavoriteFormRow, SidebarFavoriteMyTasksRow } from "./SidebarFavoriteFormRow";
import { ArrowUpRightIcon, DotsHorizontal, Plus, SidebarChevronDown, SlidersIcon, type LeafenIconSize } from "@/icons/leafen";
import { SidebarSectionGlyph } from "./SidebarSectionGlyph";
import { Fragment, useCallback, useMemo, useRef, useState, type ReactNode } from "react";
import { sidebarListWithDrops } from "./sidebarDropMap";
import { useTranslation } from "react-i18next";
import type { ContentGraph } from "@/contentGraph";
import { type DialogTeamspace } from "@/components/teamspace/teamspaceDialogModel";
import { teamspaceGlyph } from "@/components/teamspace/KeptIconGlyph";
import { usePageNames } from "@/lib/pageNames";
import { useEditTimesChange } from "@/lib/pageEditedHere";
import type { Page, Session } from "@/types";
import { workspaceMembershipBounds, type DatabaseFavoriteProjection, type SidebarLayout, type SidebarLayoutShortcut } from "@/workspaceMembershipWire.generated";
import { placeNextFavorite } from "./useSidebarLayout";
import { PageTreeItem } from "./PageTreeItem";
import { usePageTreeRowContext } from "./pageTreeContext";
import { usePageSectionDrop } from "./usePageTreeRowDrag";
import { SectionOrderProvider, useKeepShownOrder, useSectionHeadDrop } from "./sidebarSectionOrder";
import { privateRootsOf, rootTreePages, sharedSectionPages } from "./pageTreeModel";
import {
  addSidebarShortcut,
  addSidebarTeamspace,
  addSidebarView,
  canAddSidebarShortcut,
  canAddSidebarView,
  changeSidebarSection,
  changeSidebarShortcut,
  changeSidebarTeamspace,
  changeSidebarView,
  knownSidebarSection,
  newSidebarTabId,
  type KnownSidebarSectionId,
  moveSidebarSection,
  moveSidebarSectionBefore,
  moveSidebarShortcut,
  moveSidebarShortcutBefore,
  removeSidebarShortcut,
  setSidebarSectionHidden,
  sidebarAddableSections,
  sidebarSection,
  sidebarSectionTeamspace,
  sidebarShownSections,
  sidebarTabShortcuts,
  sortSidebarPages,
  favoriteOrderKey,
  movedFavoriteOrder,
  orderFavorites,
  teamspaceSectionKey,
  type SidebarShowAllKey,
} from "./sidebarLayoutModel";
import { SidebarCustomizeList } from "./SidebarCustomizeList";
import { SidebarDatabaseViewSection, SidebarViewSectionName } from "./SidebarDatabaseViewSection";
import { SidebarRecentsSection } from "./SidebarRecentsSection";
import { SidebarDeveloperSection } from "./SidebarDeveloperSection";
import { SidebarSectionMenu } from "./SidebarSectionMenu";
import { SidebarDatabaseShortcutMenu } from "./SidebarDatabaseShortcut";
import { SidebarShortcutMenu } from "./SidebarShortcutMenu";
import { SidebarViewSectionMenu } from "./SidebarViewSectionMenu";
import { SidebarShortcuts, SidebarShortcutsEditor, type SidebarShortcutMenuRender } from "./SidebarShortcuts";
import { SidebarTeamspacesSection } from "./SidebarTeamspacesSection";
import { SidebarOneTeamspaceSection } from "./SidebarOneTeamspaceSection";
import { SidebarTeamspaceSectionMenu } from "./SidebarTeamspaceSectionMenu";
import { SidebarViewPicker } from "./SidebarViewPicker";
import { readCollapsedSections, writeCollapsedSections, type CollapsibleSection } from "./sidebarSectionCollapse";
import { styles } from "./sidebarStyles";
import type { SidebarTeamspaceAction } from "./useSidebarTeamspaceActions";
import { SidebarFavoriteDatabaseRow } from "./SidebarFavoriteDatabaseRow";
import { SidebarFavoriteRowPage, useFavoriteRowPages } from "./SidebarFavoriteRowPage";
import type { DeveloperToolsTab } from "@/lib/router";
import { pageMapOf } from "@/lib/pageIndex";

const noTrashedPages: readonly Page[] = [];

/**
 * The reader's teamspace order with those the section shows in the order a
 * pull left them, each other one — in a section of its own or past the
 * count — where it was, the order the library's tab keeps too.
 */
function reorderedTeamspaces(all: readonly { id: string }[], shownOrder: readonly string[]) {
  const shown = new Set(shownOrder);
  let next = 0;
  return all.map(({ id }) => (shown.has(id) ? shownOrder[next++] : id));
}

/**
 * The sidebar's sections in the reader's order (the one-sidebar decision of
 * 2026-10-29), Notion's since its 2026-09 redesign — favorites, teamspaces,
 * private pages, shared pages and, once added, the recent pages —, each
 * sorting and counting its pages as
 * its "…" menu says, with "모두 보기" past the count, and "사이드바 사용자
 * 지정" at the end, which turns the list into the customize mode's.
 */
export function SidebarPageSections({
  session,
  guest,
  guestPrivate = false, privateAdds = true,
  pages,
  trashedPages = noTrashedPages,
  sharedPageIds,
  workspaceWidePageIds,
  favoritePageIds,
  favoriteDatabases = [],
  layout,
  tab,
  onLayoutChange,
  onOpenShortcut,
  onOpenLibrary,
  onOpenDeveloperTools,
  workspaceId,
  presets = [],
  onAddPreset,
  customizing,
  onCustomize,
  onCustomizeDone,
  creatingPage,
  joinedTeamspaces,
  teamspaceBrowseCount,
  canCreateTeamspace,
  onCreatePage,
  onAddPrivatePage,
  onCreateTeamspacePage,
  onAddTeamspaceRowPage,
  onOpenTeamspaceCreate,
  onOpenTeamspaceManage,
  onTeamspaceAction,
  phone = false,
  footer,
}: {
  session: Session;
  /** A guest has only the Shared section: the pages shared with them. */
  guest: boolean;
  /** A guest waiting to be a member keeps private pages meanwhile, as Notion's security setting lets them; a restricted member kept from making them has no "+" there (privateAdds false). */
  guestPrivate?: boolean; privateAdds?: boolean;
  pages: Page[];
  /** Pages in the trash with the page above them: one keeps its favorite, as Notion's does. */
  trashedPages?: readonly Page[];
  /** The pages of the reader's Shared section and the pages under them. */
  sharedPageIds: readonly string[];
  /** Open to the whole workspace, not shared with this person by name: no line in shared pages, as Notion's (seen 2026-11-05). */
  workspaceWidePageIds: readonly string[];
  favoritePageIds: readonly string[];
  /** The inline databases this person keeps among favorites. */
  favoriteDatabases?: readonly DatabaseFavoriteProjection[];
  layout: SidebarLayout;
  /** The open tab. */
  tab: string;
  onLayoutChange: (layout: SidebarLayout) => void;
  /** Does what a shortcut names. */
  onOpenShortcut: (shortcut: SidebarLayoutShortcut) => void;
  /** Opens the library at the section's own view. */
  onOpenLibrary: (view: "favorites" | "private" | "shared" | "teamspaces" | "recents") => void;
  /** Opens a tab of the developer tools: the developer section's links. */
  onOpenDeveloperTools: (tab: DeveloperToolsTab) => void;
  workspaceId: string | null;
  /** The optional presets "섹션 추가" offers, and adding one. */
  presets?: readonly { id: string; name: string }[];
  onAddPreset?: (id: string) => void;
  /** The sidebar is in its customize mode. */
  customizing: boolean;
  onCustomize: () => void;
  onCustomizeDone: () => void;
  creatingPage: boolean;
  joinedTeamspaces: DialogTeamspace[];
  teamspaceBrowseCount: number;
  canCreateTeamspace: boolean;
  /** The private list's last row: the add window, on a phone the page at once. */
  onCreatePage: () => void;
  /**
   * A private page made at once, opening at its title — the section's "+",
   * as Notion's (seen 2026-10-29: its private pages' "+" opens no window).
   */
  onAddPrivatePage: () => void;
  onCreateTeamspacePage: (teamspaceId: string) => void;
  /** A teamspace's last row, as the private list's. */
  onAddTeamspaceRowPage: (teamspaceId: string) => void;
  onOpenTeamspaceCreate: () => void;
  onOpenTeamspaceManage: (teamspaceId: string, invite: boolean) => void;
  onTeamspaceAction: (teamspace: DialogTeamspace, action: SidebarTeamspaceAction) => void;
  phone?: boolean;
  footer?: ReactNode;
}) {
  const { t } = useTranslation("sidebar"), pageNames = usePageNames();
  const [collapsedSections, setCollapsedSections] = useState(readCollapsedSections);
  // A drag held near the list's top or bottom scrolls it, it fades where it scrolls on, as Notion's, and a page lands where its drop map says.
  const listEdges = useCallback((element: HTMLElement | null) =>
    element ? sidebarListWithDrops(element, !phone) : undefined, [phone]);
  const shared = useMemo(() => new Set(sharedPageIds), [sharedPageIds]);
  // Worked out as the page list changes, not at each drawing: each walked every
  // page of a 44,000-page workspace as the sidebar drew again (2026-11-14).
  const roots = useMemo(() => rootTreePages(pages), [pages]);
  const sharedRoots = useMemo(() => sharedSectionPages(pages, shared, workspaceWidePageIds), [pages, shared, workspaceWidePageIds]);
  const privateRoots = useMemo(() => privateRootsOf(roots, shared), [roots, shared]);
  // A section in edit order sorts again as an edit time changes, one made here among them (lib/pageEditedHere.ts).
  const editTimes = useEditTimesChange(sidebarShownSections(layout, tab).some((key) => sidebarSection(layout, tab, key).sort === "edited"));
  const favoriteDatabasesHere = favoriteDatabases.filter((database) => database.favorite && database.workspaceId === workspaceId);
  const favoriteRowPages = useFavoriteRowPages(session, workspaceId, favoritePageIds, pages);
  const { favorites: favoriteForms, myTasks: favoriteMyTasks } = useFormFavorites(session, workspaceId);
  // A page sent to the trash leaves favorites on the server; one in the trash
  // with the page above it stays and opens on its trash banner, as Notion's
  // does (seen 2026-11-08).
  const favoriteRoots = useMemo(() => {
    const live = pageMapOf(pages), trashed = pageMapOf(trashedPages);
    return favoritePageIds.flatMap((id) => { const page = live.get(id) ?? trashed.get(id); return page && !page.archivedAt ? [page] : []; });
  }, [favoritePageIds, pages, trashedPages]);
  const privateCollapsed = collapsedSections.has("private");
  const sharedCollapsed = collapsedSections.has("shared");
  const favoritesCollapsed = collapsedSections.has("favorites");
  // A guest has the favorites and shared pages only, and no customizing: Notion's
  // guest sidebar has no "사이드바 사용자 지정" (seen 2026-11-12). A teamspace's
  // own section shows while the reader is in it.
  const teamspaceOf = (key: string) => {
    const id = sidebarSectionTeamspace(key);
    return id ? joinedTeamspaces.find((teamspace) => teamspace.id === id) : undefined;
  };
  const forReader = (key: string) => {
    const known = knownSidebarSection(key);
    if (sidebarSectionTeamspace(key)) return !guest && Boolean(teamspaceOf(key));
    return !guest || known === "favorites" || known === "shared" || guestPrivate && known === "private";
  };
  // A phone's drawer holds the same list (the one-sidebar decision's 10).
  const onSidebar = sidebarShownSections(layout, tab).filter(forReader);
  // A teamspace with a section of its own leaves the teamspaces section, as Notion's (seen 2026-11-11).
  const teamspacesWithoutSection = joinedTeamspaces.filter((teamspace) => !onSidebar.includes(teamspaceSectionKey(teamspace.id)));
  const shownSections = onSidebar.filter((id) => (
    id === "favorites" ? favoriteMyTasks || favoriteRoots.length + favoriteDatabasesHere.length + favoriteRowPages.length + favoriteForms.length > 0
      // With none joined, the section still holds the row to browse the open ones.
      : id === "teamspaces" ? joinedTeamspaces.length > 0 || teamspaceBrowseCount > 0
        : id === "shared" ? sharedRoots.length > 0
          : id === "recents" || id === "private" ? workspaceId !== null
            // Notion's phone sidebar leaves the developer links out (seen 2026-10-30).
            : id === "developer" ? !phone
              : true
  ));
  const sectionName = (id: string) => (knownSidebarSection(id) ? t(id === "private" ? "privatePages" : id)
    : teamspaceOf(id)?.name ?? (sidebarSection(layout, tab, id).view?.name.trim() || t("databaseView")));
  // In customize mode a view reads "〈보기〉 · 〈데이터베이스〉", as its heading does,
  // and every section has its icon, as Notion's rows do (seen 2026-10-08).
  const sectionLabel = (id: string): ReactNode => {
    const view = knownSidebarSection(id) ? undefined : sidebarSection(layout, tab, id).view;
    return view ? <SidebarViewSectionName view={view} session={session} fallback={t("databaseView")} /> : sectionName(id);
  };
  const sectionIcon = (id: string, size: LeafenIconSize): ReactNode => {
    const teamspace = teamspaceOf(id);
    if (teamspace) return <span className={styles.customizeTeamspaceIcon}>{teamspaceGlyph(teamspace)}</span>;
    return <SidebarSectionGlyph id={id} view={knownSidebarSection(id) ? undefined : sidebarSection(layout, tab, id).view} session={session} size={size} />;
  };

  // A section's "…": a database view's has its own rows (SidebarViewSectionMenu).
  // A guest's heads have none, only "라이브러리에서 열기", as Notion's (seen 2026-11-12).
  function sectionMenu(id: string, name: string, graph: ContentGraph | null = null) {
    if (phone || guest) return null;
    const order = customizing ? onSidebar : shownSections;
    const at = order.indexOf(id);
    const section = sidebarSection(layout, tab, id);
    const teamspace = teamspaceOf(id);
    if (teamspace) {
      return (
        <SidebarTeamspaceSectionMenu teamspace={teamspace} choices={[teamspace, ...teamspacesWithoutSection]}
          canCreate={canCreateTeamspace}
          canMoveUp={at > 0} canMoveDown={at >= 0 && at < order.length - 1}
          onChoose={(teamspaceId) => onLayoutChange(changeSidebarTeamspace(layout, tab, id, teamspaceId))}
          onMove={(step) => onLayoutChange(moveSidebarSection(layout, tab, id, step, order))}
          onRemove={() => onLayoutChange(setSidebarSectionHidden(layout, tab, id, true))}
          onCustomize={customizing ? undefined : onCustomize}
          onManage={onOpenTeamspaceManage} onAct={onTeamspaceAction} />
      );
    }
    if (section.view && workspaceId) {
      return (
        <SidebarViewSectionMenu name={name}
          view={section.view} limit={section.limit} graph={graph} session={session}
          workspaceId={workspaceId}
          onChange={(change) => onLayoutChange(changeSidebarView(layout, tab, id, change))}
          onLimit={(limit) => onLayoutChange(changeSidebarSection(layout, tab, id, { limit }))}
          canMoveUp={at > 0} canMoveDown={at >= 0 && at < order.length - 1}
          onMove={(step) => onLayoutChange(moveSidebarSection(layout, tab, id, step, order))}
          onRemove={() => onLayoutChange(setSidebarSectionHidden(layout, tab, id, true))}
          onCustomize={customizing ? undefined : onCustomize} />
      );
    }
    return (
      <SidebarSectionMenu name={name} section={section}
        onCreateTeamspace={id === "teamspaces" && canCreateTeamspace ? onOpenTeamspaceCreate : undefined}
        canMoveUp={at > 0} canMoveDown={at >= 0 && at < order.length - 1}
        onChange={(change) => {
          if ((id === "private" || id === "shared") && change.sort === "manual" && section.sort === "edited") keepShownOrder(shownSorted(id)());
          onLayoutChange(changeSidebarSection(layout, tab, id, change));
        }}
        onMove={(step) => onLayoutChange(moveSidebarSection(layout, tab, id, step, order))}
        onRemove={() => onLayoutChange(setSidebarSectionHidden(layout, tab, id, true))}
        onCustomize={customizing ? undefined : onCustomize} />
    );
  }

  // A shortcut's "…" menu: its name, where it opens and its page; a database's new page's, its view and template too.
  const shortcutMenu: SidebarShortcutMenuRender = (shortcut, label, rename, databaseTitle) => {
    if (shortcut.kind === "new_database_page") return (
      <SidebarDatabaseShortcutMenu label={label} shortcut={shortcut} databaseTitle={databaseTitle ?? "…"} session={session}
        holder={pages.find((candidate) => candidate.id === shortcut.pageId)} onRename={rename}
        onChange={(change) => onLayoutChange(changeSidebarShortcut(layout, tab, shortcut.key, change))}
        onMove={shortcutMoves(shortcut.key)}
        onRemove={() => onLayoutChange(removeSidebarShortcut(layout, tab, shortcut.key))}
        onCustomize={customizing ? undefined : onCustomize} />
    );
    const place = shortcut.kind === "new_page" && shortcut.pageId
      ? pages.find((candidate) => candidate.id === shortcut.pageId && !candidate.inTrash) : undefined;
    return (
      <SidebarShortcutMenu label={label} shortcut={shortcut}
        placeTitle={place ? pageNames.title(place) : undefined} onRename={rename}
        onOpenIn={(openIn) => onLayoutChange(changeSidebarShortcut(layout, tab, shortcut.key, { openIn }))}
        onOpenPlace={place ? () => onOpenShortcut({ key: shortcut.key, kind: "page", pageId: place.id, name: "" }) : undefined}
        onMove={shortcutMoves(shortcut.key)}
        onRemove={() => onLayoutChange(removeSidebarShortcut(layout, tab, shortcut.key))}
        onCustomize={customizing ? undefined : onCustomize} />
    );
  };
  const renameShortcut = (key: string, name: string) => onLayoutChange(changeSidebarShortcut(layout, tab, key, { name }));
  function shortcutMoves(key: string) {
    const all = sidebarTabShortcuts(layout, tab), at = all.findIndex((shortcut) => shortcut.key === key);
    const move = (step: -1 | 1) => () => onLayoutChange(moveSidebarShortcut(layout, tab, key, step));
    return { up: at > 0 ? move(-1) : undefined, down: at >= 0 && at < all.length - 1 ? move(1) : undefined };
  }

  function openInLibrary(view: "favorites" | "private" | "shared" | "teamspaces") {
    return (
      <button type="button" className={styles.sectionAction} data-sidebar-open-library={view}
        aria-label={t("openInLibrary")} title={t("openInLibrary")} onClick={() => onOpenLibrary(view)}>
        <ArrowUpRightIcon size="small" aria-hidden="true" />
      </button>
    );
  }

  // A list in its section's order, up to its count; a list sorted once for its order.
  const sortedLists = useRef(new WeakMap<readonly Page[], Map<string, Page[]>>()).current;
  function listed(id: KnownSidebarSectionId, list: readonly Page[], sectionKey: string = id) {
    const section = sidebarSection(layout, tab, sectionKey);
    let byOrder = sortedLists.get(list);
    if (!byOrder) sortedLists.set(list, byOrder = new Map());
    const edited = section.sort === "edited";
    const order = edited ? `edited:${editTimes}` : section.sort;
    let sorted = byOrder.get(order);
    if (!sorted) {
      if (edited) for (const kept of byOrder.keys()) if (kept.startsWith("edited:")) byOrder.delete(kept);
      byOrder.set(order, sorted = sortSidebarPages(list, section.sort));
    }
    const all = sorted.length <= section.limit;
    return { pages: all ? sorted : sorted.slice(0, section.limit), more: !all };
  }

  // "··· 모두 보기", a 20px ⋯ before it, as Notion's (seen 2026-10-31): the
  // whole list is the section's tab of the library, the sidebar staying as it
  // is, as Notion's (seen 2026-11-03 private pages, 2026-11-13 favorites; a
  // teamspace's, the teamspaces tab).
  function showAll(key: SidebarShowAllKey): ReactNode {
    return (
      <button type="button" className={styles.showAll} data-sidebar-show-all={key}
        onClick={() => onOpenLibrary(sidebarSectionTeamspace(key) ? "teamspaces" : key as "favorites" | "private" | "shared")}>
        <DotsHorizontal size="regular" aria-hidden="true" />
        <span>{t("showAll")}</span>
      </button>
    );
  }

  function toggleSection(section: CollapsibleSection) {
    setCollapsedSections((current) => {
      const next = new Set(current);
      if (next.has(section)) next.delete(section);
      else next.add(section);
      writeCollapsedSections(next);
      return next;
    });
  }

  const sharedListed = listed("shared", sharedRoots);
  const privateListed = listed("private", privateRoots);
  // The favorites as one list in the reader's order: a new one first and a
  // drag among them moving only it, as Notion's (seen 2026-11-06), whose
  // favorites have no sort (seen 2026-11-13) — one set before is not kept.
  const favoritesSection = sidebarSection(layout, tab, "favorites");
  type FavoriteEntry = { key: string; page?: Page; database?: DatabaseFavoriteProjection;
    rowPage?: (typeof favoriteRowPages)[number]; form?: (typeof favoriteForms)[number] };
  const groupedFavorites: FavoriteEntry[] = [
    ...favoriteForms.map((form) => ({ key: favoriteOrderKey("form", form.token), form })),
    ...sortSidebarPages(favoriteRoots, "manual").map((page) => ({ key: favoriteOrderKey("page", page.id), page })),
    ...favoriteDatabasesHere.map((database) => ({ key: favoriteOrderKey("database", database.containerId), database })),
    ...favoriteRowPages.map((rowPage) => ({ key: favoriteOrderKey("page", rowPage.page.id), rowPage })),
  ];
  const favoriteList = orderFavorites(groupedFavorites, ({ key }) => key, layout.favoriteOrder);
  const allFavorites = favoriteList.length <= favoritesSection.limit;
  const shownFavorites = allFavorites ? favoriteList : favoriteList.slice(0, favoritesSection.limit);
  // One drop function across drawings (a row redraws for its own changes only): it moves the favorites as shown, or a page joins them there.
  const favoriteDropAt = useRef<(target: string, draggedId: string, edge: "before" | "after") => void>(() => undefined);
  favoriteDropAt.current = (target, draggedId, edge) => {
    const order = movedFavoriteOrder(favoriteList.map(({ key }) => key), draggedId, target, edge);
    onLayoutChange({ ...layout, favoriteOrder: order });
    if (!favoritePageIds.includes(draggedId)) { placeNextFavorite(draggedId, order); void onFavoriteChange(draggedId, true); }
  };
  const favoriteDrops = useRef(new Map<string, (draggedId: string, edge: "before" | "after") => void>());
  const dropFavorite = (target: string) => {
    let drop = favoriteDrops.current.get(target);
    if (!drop) {
      drop = (draggedId, edge) => favoriteDropAt.current(target, draggedId, edge);
      favoriteDrops.current.set(target, drop);
    }
    return drop;
  };
  // A page no favorite's row takes, dropped on the section — its heading, or
  // a row taking nothing —, joins them (first, as any new favorite).
  const { onFavoriteChange } = usePageTreeRowContext();
  const [favoritesDropArea, setFavoritesDropArea] = useState<HTMLElement | null>(null);
  const [favoriteDropOver, setFavoriteDropOver] = useState(false);
  usePageSectionDrop(favoritesDropArea, {
    accepts: (pageId) => !favoritePageIds.includes(pageId) && pages.some((page) => page.id === pageId),
    drop: (pageId) => void onFavoriteChange(pageId, true),
  }, setFavoriteDropOver, true);
  // The private and shared pages take a page on their headings, and put by hand a sorted section first asks to be "수동".
  // Turned "수동" by a drop or its menu, a sorted section keeps the order it showed (useKeepShownOrder).
  const toManual = (id: "private" | "shared") => () => onLayoutChange(changeSidebarSection(layout, tab, id, { sort: "manual" }));
  const shownSorted = (id: "private" | "shared") => () => sortSidebarPages(id === "private" ? privateRoots : sharedRoots, "edited");
  const keepShownOrder = useKeepShownOrder();
  const privateDrop = useSectionHeadDrop("private", t("privatePages"), sidebarSection(layout, tab, "private").sort, toManual("private"), shared,
    shownSorted("private"));
  const sharedDrop = useSectionHeadDrop("shared", t("shared"), sidebarSection(layout, tab, "shared").sort, toManual("shared"), shared,
    shownSorted("shared"));
  if (customizing && !phone && !guest) {
    // The list scrolls in the sidebar's body, its "완료" under it (SidebarCustomizeList).
    return (
      <SidebarCustomizeList scrollRef={listEdges} sections={onSidebar} name={sectionName} label={sectionLabel} icon={sectionIcon}
        addable={sidebarAddableSections(layout, tab, !guest && teamspacesWithoutSection.length > 0).filter(forReader)}
        teamspaces={guest ? undefined : {
          choices: teamspacesWithoutSection.map((teamspace) => ({ id: teamspace.id, name: teamspace.name,
            icon: <span className={styles.sectionMenuTeamspaceIcon} aria-hidden="true">{teamspaceGlyph(teamspace)}</span> })),
          onAdd: (teamspaceId) => onLayoutChange(addSidebarTeamspace(layout, tab, teamspaceId)),
        }}
        menu={(id) => sectionMenu(id, sectionName(id))}
        presets={presets} onAddPreset={onAddPreset}
        shortcuts={workspaceId ? (
          <SidebarShortcutsEditor shortcuts={sidebarTabShortcuts(layout, tab)} pages={pages} session={session}
            workspaceId={workspaceId} canAdd={canAddSidebarShortcut(layout, tab)}
            onAdd={(kind, pageId, name) => onLayoutChange(addSidebarShortcut(layout, tab, { key: newSidebarTabId(), kind, pageId, name }))}
            onRemove={(key) => onLayoutChange(removeSidebarShortcut(layout, tab, key))}
            onMove={(key, before) => onLayoutChange(moveSidebarShortcutBefore(layout, tab, key, before))}
            menu={shortcutMenu} onRename={renameShortcut} />
        ) : null}
        viewsFull={workspaceId && !canAddSidebarView(layout, tab)
          ? t("viewsFull", { limit: workspaceMembershipBounds.sidebarViewsPerTab }) : undefined}
        viewPicker={workspaceId && canAddSidebarView(layout, tab) ? (close, anchor, back, byKeys) => (
          <SidebarViewPicker anchor={anchor} session={session} workspaceId={workspaceId} searchFirst={byKeys} onClose={back} onChoose={(source) => {
            close();
            onLayoutChange(addSidebarView(layout, tab, {
              key: newSidebarTabId(), pageId: source.pageId, dataSourceId: source.dataSourceId, viewId: source.viewId,
            }));
          }} />
        ) : undefined}
        onMove={(id, before) => onLayoutChange(moveSidebarSectionBefore(layout, tab, id, before))}
        onRemove={(id) => onLayoutChange(setSidebarSectionHidden(layout, tab, id, true))}
        onAdd={(id) => onLayoutChange(setSidebarSectionHidden(layout, tab, id, false))}
        onDone={onCustomizeDone} />
    );
  }

  // A teamspace's own section: its pages as the section's top rows.
  function teamspaceSection(id: string, teamspace: DialogTeamspace) {
    const all = roots.filter((page) => page.teamspaceId === teamspace.id), shown = listed("teamspaces", all, id);
    return (
      <SidebarOneTeamspaceSection sectionKey={id} teamspace={teamspace} pages={{ all, listed: shown.pages }} more={shown.more}
        showAll={showAll} menu={sectionMenu(id, teamspace.name)} collapsed={collapsedSections.has(teamspaceSectionKey(teamspace.id))}
        onToggle={() => toggleSection(teamspaceSectionKey(teamspace.id))} creatingPage={creatingPage}
        onCreatePage={onCreateTeamspacePage} onAddRowPage={onAddTeamspaceRowPage} />
    );
  }

  const sections: Record<KnownSidebarSectionId, () => ReactNode> = {
    developer: () => <SidebarDeveloperSection menu={sectionMenu("developer", t("developer"))} onOpen={onOpenDeveloperTools} />,
    recents: () => (workspaceId ? (
      <SidebarRecentsSection workspaceId={workspaceId} section={sidebarSection(layout, tab, "recents")}
        menu={sectionMenu("recents", t("recents"))} />
    ) : null),
    favorites: () => (
      <section ref={setFavoritesDropArea} className={styles.section} data-sidebar-favorites-section="true"
        data-favorite-drop={favoriteDropOver || undefined} data-section-folded={favoritesCollapsed || undefined}>
        <div className={styles.sectionLabel}>
          <button
            type="button"
            className={`${styles.sectionToggle} ${styles.privateSectionToggle}`}
            aria-expanded={!favoritesCollapsed}
            aria-controls="sidebar-favorites-section"
            onClick={() => toggleSection("favorites")}
          >
            <span className={styles.sectionTitle}>{t("favorites")}</span>
            <SidebarChevronDown className={styles.sectionChevron} size="micro" aria-hidden="true" />
          </button>
          <div className={styles.sectionActions} data-section-actions="favorites">
            {openInLibrary("favorites")}
            {sectionMenu("favorites", t("favorites"))}
          </div>
        </div>
        {!favoritesCollapsed && (
          <div
            id="sidebar-favorites-section"
            className={styles.tree}
            role="tree"
            aria-label={t("favoritePages")}
          >
            {favoriteMyTasks ? <SidebarFavoriteMyTasksRow /> : null}
            {shownFavorites.map(({ key, page, database, rowPage, form }, index) => (
              page ? <PageTreeItem key={key} pageId={page.id} depth={0} index={index} setSize={shownFavorites.length}
                listCopy="favorite" onFavoriteDrop={dropFavorite(key)} />
                // An inline database among favorites opens alone as a page; its row lists its views and has its menu (Notion's, seen 2026-10-11).
                : database ? <SidebarFavoriteDatabaseRow key={key} database={database} session={session} />
                  : rowPage ? <SidebarFavoriteRowPage key={key} {...rowPage} session={session} />
                    : form ? <SidebarFavoriteFormRow key={key} favorite={form} /> : null
            ))}
            {allFavorites ? null : showAll("favorites")}
          </div>
        )}
      </section>
    ),
    teamspaces: () => (
      <SidebarTeamspacesSection teamspaces={teamspacesWithoutSection} roots={roots} menu={sectionMenu("teamspaces", t("teamspaces"))}
        libraryButton={openInLibrary("teamspaces")} collapsed={collapsedSections.has("teamspaces")}
        onToggle={() => toggleSection("teamspaces")}
        canCreateTeamspace={canCreateTeamspace} creatingPage={creatingPage}
        browseCount={teamspaceBrowseCount}
        limit={sidebarSection(layout, tab, "teamspaces").limit}
        listed={(list) => listed("teamspaces", list)} showAll={showAll}
        onCreateTeamspacePage={onCreateTeamspacePage} onAddRowPage={onAddTeamspaceRowPage}
        onOpenTeamspaceManage={onOpenTeamspaceManage}
        onOpenTeamspacesLibrary={() => onOpenLibrary("teamspaces")}
        onTeamspaceAction={onTeamspaceAction}
        onReorder={(order) => onLayoutChange({ ...layout, teamspaceOrder: reorderedTeamspaces(joinedTeamspaces, order) })} />
    ),
    shared: () => (
      <section ref={sharedDrop.ref} className={styles.section} data-sidebar-shared-section="true"
        data-section-drop={sharedDrop.over || undefined} data-section-folded={sharedCollapsed || undefined}>
        <div className={styles.sectionLabel}>
          <button
            type="button"
            className={`${styles.sectionToggle} ${styles.privateSectionToggle}`}
            aria-expanded={!sharedCollapsed}
            aria-controls="sidebar-shared-section"
            onClick={() => toggleSection("shared")}
          >
            <span className={styles.sectionTitle}>{t("shared")}</span>
            <SidebarChevronDown className={styles.sectionChevron} size="micro" aria-hidden="true" />
          </button>
          <div className={styles.sectionActions} data-section-actions="shared">
            {openInLibrary("shared")}
            {sectionMenu("shared", t("shared"))}
          </div>
        </div>
        {!sharedCollapsed && (
          <div
            id="sidebar-shared-section"
            className={styles.tree}
            role="tree"
            aria-label={t("sharedPages")}
          >
            <SectionOrderProvider value={sharedDrop.order}>
              {sharedListed.pages.map((page, index) => (
                <PageTreeItem
                  key={page.id}
                  pageId={page.id}
                  depth={0}
                  index={index}
                  setSize={sharedListed.pages.length}
                  sharedRoot
                />
              ))}
            </SectionOrderProvider>
            {sharedListed.more ? showAll("shared") : null}
          </div>
        )}
      </section>
    ),
    private: () => (
      <section ref={privateDrop.ref} className={styles.section} data-sidebar-private-section data-section-drop={privateDrop.over || undefined}
        data-section-folded={privateCollapsed || undefined}>
        <div className={styles.sectionLabel}>
          <button
            type="button"
            className={`${styles.sectionToggle} ${styles.privateSectionToggle}`}
            aria-expanded={!privateCollapsed}
            aria-controls="sidebar-private-section" onClick={() => toggleSection("private")}
          >
            <span className={styles.sectionTitle}>{t("privatePages")}</span>
            <SidebarChevronDown className={styles.sectionChevron} size="micro" aria-hidden="true" />
          </button>
          <div className={styles.sectionActions} data-section-actions="private">
            {openInLibrary("private")}
            {privateAdds ? <button
              type="button"
              className={`${styles.sectionAction} ${styles.sectionAdd}`}
              aria-label={t("addPrivatePage")}
              disabled={creatingPage}
              onClick={onAddPrivatePage}
            >
              <Plus size="small" aria-hidden="true" />
            </button> : null}
            {sectionMenu("private", t("privatePages"))}
          </div>
        </div>
        {!privateCollapsed && (
          <div className={styles.rootDropArea}>
            <div
              id="sidebar-private-section"
              className={styles.tree}
              role="tree"
              aria-label={t("privatePages")}
            >
              <SectionOrderProvider value={privateDrop.order}>
                {privateListed.pages.map((page, index) => (
                  <PageTreeItem
                    key={page.id}
                    pageId={page.id}
                    depth={0}
                    index={index}
                    setSize={privateListed.pages.length}
                  />
                ))}
              </SectionOrderProvider>
              {privateListed.more && !phone ? showAll("private") : null}
            </div>
            {/* Notion's rule (its sidebar code, read 2026-10-31): "+ 새 페이지 추가"
                ends the list while it shows two pages or fewer, and always on a
                phone, where "모두 보기" comes after it. */}
            {(privateListed.pages.length <= 2 || phone) && privateAdds ? <button
              type="button"
              className={styles.emptyTreeAction}
              disabled={creatingPage}
              onClick={onCreatePage}
              data-sidebar-add-private-page
            >
              <Plus size="small" aria-hidden="true" />
              <span>{creatingPage ? t("creating") : t("addPageRow")}</span>
            </button> : null}
            {privateListed.more && phone ? showAll("private") : null}
          </div>
        )}
      </section>
    ),
  };

  return (
    <div ref={listEdges} className={`${styles.scroll} nscroll`}>
      {/* A phone's drawer lists the shortcuts too: its list is the sidebar's, but for search and home (baseline 사이드바 8). */}
      {guest ? null : <SidebarShortcuts shortcuts={sidebarTabShortcuts(layout, tab)} pages={pages} session={session}
        onOpen={onOpenShortcut} menu={shortcutMenu} onRename={renameShortcut} />}
      {shownSections.map((id) => {
        const known = knownSidebarSection(id), teamspace = teamspaceOf(id);
        const view = known ? undefined : sidebarSection(layout, tab, id);
        return (
          <Fragment key={id}>
            {known ? sections[known]() : teamspace ? teamspaceSection(id, teamspace) : view?.view && workspaceId ? (
              <SidebarDatabaseViewSection view={view.view} limit={view.limit} session={session} workspaceId={workspaceId}
                currentUserId={session.user.id} holder={pages.find((page) => page.id === view.view?.pageId)}
                menu={(graph) => sectionMenu(id, sectionName(id), graph)} />
            ) : null}
          </Fragment>
        );
      })}
      {phone ? footer : guest ? null : <div className={styles.customizeFoot}><button type="button" className={styles.customizeEntry}
        data-sidebar-customize-open onClick={onCustomize}><SlidersIcon size="wide" aria-hidden="true" />
        <span>{t("customizeSidebar")}</span>
      </button></div>}
    </div>
  );
}
