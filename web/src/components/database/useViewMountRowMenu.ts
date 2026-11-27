import { useContext, useEffect, useMemo, type RefObject } from "react";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import type { DatabaseRowPageSnapshot } from "@/databaseRowPageWire.generated";
import type { GraphDataSource, GraphProperty, GraphRow, GraphView } from "@/contentGraph";
import { nextPaint } from "@/lib/afterPress";
import { copyText } from "@/lib/copyText";
import { createDatabaseRowPageService, type DatabaseRowPageService } from "@/lib/databaseRowPageService";
import { dataSourceInTrash } from "@/lib/dataSourcePage";
import type { NativeTableProductAdapter } from "@/lib/nativeTableProductAdapter";
import { pageHref } from "@/lib/navigation";
import type { PageAnalyticsPanelService } from "@/lib/pageAnalyticsView";
import { useProductToast } from "@/lib/productToast";
import { createRelationDatabaseSearch } from "@/lib/relationDatabaseSearch";
import { serverUrl } from "@/lib/serverOrigin";
import { searchWorkspacePages } from "@/lib/workspacePageSearch";
import type { Session } from "@/types";
import type { DatabaseViewOpenMode } from "./DatabaseView";
import { DatabasePagelessRowsContext } from "./databasePagelessRows";
import type { DatabaseToolbarCommands } from "./databaseToolbarModel";
import { galleryPresentationPage } from "./galleryViewModel";
import type { PropertyCellEnvironment } from "./propertyCellTypes";
import { useDatabaseRowActions } from "./useDatabaseRowActions";
import { useRowPageDockedPanel } from "./useRowPageDockedPanel";
import { useRowContextMenu, type RowContextMenuOptions } from "./useRowContextMenu";
import { useRowPageUnderPageLock } from "./rowPageUnderPageLock";
import { useRowPageReads } from "./useRowPageReads";

/**
 * A database view's rows' menu and what it does to their pages: open,
 * archive, move — to another database, or out of it as a page under another
 * — duplicate, trash, link, favorite, its icon and cells, and a board's or
 * gallery's settings, by the row (NativeDatabaseViewMount).
 */
const SHUT = { open: false, close: () => {} };

export function useViewMountRowMenu({
  session, service, reread, rowPageSnapshot, menuRowPage, rowPageOf, tableAdapter, closeRow, closeRowAtPress, analyticsService, pathDatabase,
  workspaceId, pageId, source, dataSources, readOnly, rowsReadOnly, structureLocked, listOnly, selectedRowId, rowTitle,
  rows, properties, titleProperty, activeView, cellEnvironment, toolbarCommands, openRow, rowPageFull = false,
  holderPageId, readOnlyHere = readOnly, rowPageHeld = false, rowPageAll = SHUT,
}: {
  session: Session | null;
  service: DatabaseRowPageService | null;
  reread: () => Promise<unknown> | undefined;
  /** The opened row's page; `menuRowPage` the latest read, `rowPageOf` each row's last (useRowPageReads). */
  rowPageSnapshot: DatabaseRowPageSnapshot | null;
  menuRowPage?: DatabaseRowPageSnapshot | null;
  rowPageOf?: (rowId: string) => DatabaseRowPageSnapshot | null;
  tableAdapter: NativeTableProductAdapter | undefined;
  closeRow(): void;
  closeRowAtPress(): void;
  analyticsService: PageAnalyticsPanelService | null;
  /** The open row fills the page (⤢): its updates and its "모든 토론" (`rowPageAll`) dock beside it under its bar, as a page's do. */
  rowPageFull?: boolean;
  rowPageAll?: { open: boolean; close: () => void };
  pathDatabase: NonNullable<RowContextMenuOptions["analytics"]>["pathDatabase"];
  workspaceId: string;
  pageId: string;
  source: GraphDataSource;
  dataSources: readonly GraphDataSource[];
  readOnly: boolean;
  rowsReadOnly: boolean;
  /** The page whose body holds the database, and what it gave the view as read only: a locked one leaves its rows' pages to their own lock (rowPageUnderPageLock.ts). */
  holderPageId?: string;
  readOnlyHere?: boolean;
  /** A reason the rows' pages stay read only with the view: a preview, the trash, no one signed in. */
  rowPageHeld?: boolean;
  structureLocked: boolean;
  listOnly: boolean | "tasks";
  selectedRowId: string;
  rowTitle: string;
  rows: readonly GraphRow[];
  properties: GraphProperty[];
  titleProperty: GraphProperty | undefined;
  activeView: GraphView;
  cellEnvironment: PropertyCellEnvironment | undefined;
  toolbarCommands: RefObject<DatabaseToolbarCommands | null>;
  openRow(rowId: string, mode: DatabaseViewOpenMode, fromMenu?: boolean): void;
}) {
  const { t } = useTranslation(["rowMenu", "pageTreeItem"]);
  const { notify } = useProductToast();
  // A row moves to another database, or out of its database to the top level.
  const moveTargets = useMemo(() => [
    // A source in the trash takes no rows (dataSourceInTrash), as no list offers it.
    ...dataSources.filter((other) => other.id !== source.id && !dataSourceInTrash(other)).map(({ id, title }) => ({ id, title })),
    { id: "", title: t("moveTopLevel"), kind: "page" as const },
  ], [dataSources, source.id, t]);
  // Databases on other pages of the workspace, and pages a row can move
  // under as a sub-page, are found by name.
  const searchMoveTargets = useMemo(() => {
    if (!session || readOnly) return undefined;
    const search = createRelationDatabaseSearch(session, workspaceId, { addsRows: true });
    return async (query: string, signal: AbortSignal) => {
      const [databases, pages] = await Promise.all([
        search(query, signal),
        query ? searchWorkspacePages(session, query, 20, workspaceId, { signal, places: true }) : Promise.resolve({ pages: [] }),
      ]);
      return [
        ...databases.filter(({ id }) => id !== source.id).map(({ id, title }) => ({ id, title, kind: "database" as const })),
        ...pages.pages.map(({ id, title }) => ({ id, title, kind: "page" as const })),
      ];
    };
  }, [readOnly, session, source.id, workspaceId]);
  // "내 작업" opens its rows itself and reads no row page here (no
  // `service`), yet each is a page of its own database: its menu acts on it
  // as any row's, as Notion's (seen 2026-11-15: 즐겨찾기 … 휴지통, 최종 편집),
  // and the list reads its tasks again after.
  const tasks = listOnly === "tasks";
  // People's rows are pages too, opened as profiles (databasePagelessRows.ts).
  const pageless = useContext(DatabasePagelessRowsContext);
  const ownRows = tasks || pageless !== null;
  const { latest: taskSnapshot, set: setTaskSnapshot, of: taskRowPageOf } = useRowPageReads();
  const taskService = useMemo(() => ownRows && !service && session ? createDatabaseRowPageService(session, setTaskSnapshot) : null,
    [ownRows, service, session]);
  useEffect(() => () => taskService?.dispose(), [taskService]);
  const menuService = service ?? taskService;
  const menuSnapshot = rowPageSnapshot ?? menuRowPage ?? taskSnapshot;
  const snapshotOf = (rowId: string) => rowPageOf?.(rowId) ?? taskRowPageOf(rowId);
  const rowsReread = () => { tableAdapter?.rowsChanged?.(); return reread(); };
  const rowActions = useDatabaseRowActions({ session, service: menuService, reread: rowsReread, rowPageSnapshot: menuSnapshot, openedRowPage: rowPageSnapshot, rowPageOf: snapshotOf,
    rowsTrashed: tableAdapter?.rowsTrashed, closeRow, pageId: source.pageId ?? holderPageId ?? pageId });
  const { run: runRowAction, setArchived: setRowArchived, changePresentation: changeRowPresentation, changeTrash: changeRowTrash,
    copyLink: copyRowLink, setFavorite: setRowFavorite } = rowActions;
  const rowWritable = !rowsReadOnly && Boolean(service);
  // The open row's page under a locked page takes its own changes, its "…" menu with it.
  const underLock = useRowPageUnderPageLock({ holderPageId: holderPageId ?? pageId, readOnlyHere, held: rowPageHeld, snapshot: rowPageSnapshot, service });
  // People's rows have a menu of their own for whoever writes their values (databasePagelessRows.ts).
  const menuWritable = !rowsReadOnly && (Boolean(menuService) || pageless !== null);
  const pageMenuWritable = menuWritable || underLock;
  const copyLinks = async (rowIds: readonly string[], href: (rowId: string) => string) => {
    if (await copyText(rowIds.map((rowId) => serverUrl(href(rowId))).join("\n"))) notify(t("toast.copiedLink"), "success");
    else notify(t("toast.couldntCopyLink"), "error");
  };
  const viewRowPage = (rowId: string) => {
    const position = rows.findIndex(({ id }) => id === rowId);
    return position < 0 ? undefined : galleryPresentationPage({ row: rows[position], titleProperty, workspaceId, position });
  };
  const cells: RowContextMenuOptions["cells"] = !rowsReadOnly && tableAdapter && cellEnvironment
    ? { properties, environment: cellEnvironment, row: viewRowPage } : undefined;
  const viewSettings: RowContextMenuOptions["viewSettings"] = !rowsReadOnly && !structureLocked && ["board", "gallery"].includes(activeView.type)
    ? (screen) => toolbarCommands.current?.openViewSettings(screen) : undefined;
  const rowMenuOptions: RowContextMenuOptions = pageless ? {
    busy: rowActions.busy, moveTargets: [], readOnly: !menuWritable, rowId: selectedRowId, rowTitle, snapshot: menuSnapshot, snapshotOf, viewRowPage,
    onWarm: (rowId) => warmRowPage(menuService, rowId),
    onFavorite: menuService ? setRowFavorite : undefined,
    onPresentation: menuWritable && menuService ? changeRowPresentation : undefined,
    onOpenMode: (rowId, mode) => openRow(rowId, mode, true),
    onCopyLink: (rowId) => copyLinks([rowId], pageless.link),
    onCopyLinks: (rowIds) => copyLinks(rowIds, pageless.link),
    foot: pageless.foot,
    // "속성 편집 ›" lists the values its reader writes on the rows it opened for.
    cells: cells ? { ...cells, properties: properties.filter(({ id }) => rows.some((row) => pageless.writable(row.id).has(id))),
      editable: (rowIds) => properties.filter(({ id }) => rowIds.every((rowId) => pageless.writable(rowId).has(id))) } : undefined,
    viewSettings,
  } : {
    analytics: analyticsService ? { workspaceId, pageId, service: analyticsService, session: session ?? undefined, pathDatabase, docked: rowPageFull } : undefined,
    busy: rowActions.busy,
    moveTargets,
    searchMoveTargets,
    readOnly: !menuWritable,
    pageReadOnly: !pageMenuWritable,
    rowId: selectedRowId,
    rowTitle,
    snapshot: menuSnapshot, snapshotOf,
    onWarm: (rowId) => warmRowPage(menuService, rowId),
    viewRow: (rowId) => rows.find(({ id }) => id === rowId),
    viewRowPage,
    // Its own source's rows are where that source is; rows another source lent the list wait for their page's read.
    rowPlace: (row) => row.dataSourceId === source.id ? { workspaceId, containerPageId: source.pageId ?? pageId } : undefined,
    // "내 작업"'s menu has no 보관, as Notion's (seen 2026-11-15).
    onArchive: pageMenuWritable && !tasks ? async (rowId, archived) => { await setRowArchived(rowId, archived); } : undefined,
    notifications: session ? (rowId) => ({
      read: async (signal) => (await api.readDatabaseRowNotificationPreference(session, rowId, signal)).level,
      save: async (level) => (await api.saveDatabaseRowNotificationPreference(session, rowId, level)).level,
    }) : undefined,
    onOpenMode: (rowId, mode) => openRow(rowId, mode, true),
    onCopyLink: copyRowLink,
    onCopyLinks: (rowIds) => copyLinks(rowIds, pageHref),
    onFavorite: setRowFavorite,
    // A row of the view is copied as its chosen rows are, right after it (viewRowOrder.ts).
    onDuplicate: async (rowId) => { await runRowAction(
      async () => {
        if (tableAdapter && !tasks && rows.some(({ id }) => id === rowId)) await tableAdapter.duplicateRows([rowId], () => true, activeView);
        else {
          await menuService?.duplicate(rowId);
          if (tasks) tableAdapter?.rowsChanged?.();
        }
      }, null, t("toast.couldntDuplicatePage"),
    ); },
    onMove: async (rowId, target, accepted) => { await runRowAction(
      async () => {
        await menuService?.move(rowId, { kind: target.kind ?? "database", id: target.id }, accepted);
        // A row moved out as a page leaves the views.
        if (target.kind === "page" || tasks) await rowsReread();
      },
      t("pageTreeItem:movedPage"), t("pageTreeItem:couldntMovePage"),
    ); },
    onPreviewMove: menuService
      ? (rowId, target) => menuService.previewMove(rowId, { kind: target.kind ?? "database", id: target.id })
      : undefined,
    // Its page slides out at the press and the row leaves the views right after that frame; one the server refuses comes back.
    // A task leaves "내 작업" as its own Delete takes it (MyTasksDatabaseView).
    onTrash: async (rowId) => {
      closeRowAtPress();
      await nextPaint();
      if (tasks && tableAdapter) await tableAdapter.deleteRows([rowId], () => true);
      else await changeRowTrash(rowId, true, true);
    },
    // The menu's icon, values, a board's or gallery's settings and a comment, by the row.
    onPresentation: pageMenuWritable ? changeRowPresentation : undefined,
    cells,
    viewSettings,
    comments: session && (!listOnly || tasks) ? { session, workspaceId, pageId } : undefined,
  };
  const rowMenu = useRowContextMenu(rowMenuOptions);
  // A full row page makes room for them, as StandaloneRowPage's does (useRowPageDockedPanel); opened from the trash or a template, it keeps none.
  const sidePanel = useRowPageDockedPanel({ rowId: selectedRowId, whole: rowPageFull, read: Boolean(rowPageSnapshot), keep: false,
    updates: { open: rowMenu.analyticsShown, close: rowMenu.closeAnalytics }, all: rowPageAll });
  return { rowActions, rowMenu, rowWritable, analyticsDocked: sidePanel.docked, sidePanel, rowPageWritable: rowWritable || underLock, rowPageReadOnly: readOnly && !underLock };
}

// A row's page read for its menu, a comment on it or its updates, beside the
// opened row's read rather than in its place (databaseRowPageService.ts).
function warmRowPage(service: DatabaseRowPageService | null, rowId: string) {
  void service?.refresh(rowId).catch(() => {});
}
