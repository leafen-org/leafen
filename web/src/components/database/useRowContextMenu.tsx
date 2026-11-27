"use client";
import { pagesByIdOf } from "@/lib/pageIndex";

import { type MouseEvent, type ReactNode, Suspense, useCallback, useRef, useState } from "react";
import { lazyScreen } from "@/lib/lazyScreen";
import type { DatabaseRowPageSnapshot } from "@/databaseRowPageWire.generated";
import type { GraphProperty, GraphRow } from "@/contentGraph";
import type { PageAnalyticsPanelService } from "@/lib/pageAnalyticsView";
import { popoverRectOf } from "@/lib/popoverPlacement";
import { useProductPagesSelect } from "@/components/product/productPagesStore";
import { pagePathLinks } from "@/lib/pagePath";
import { knownPathAboveId } from "@/lib/pagePathPieces";
import { usePagePathsAbove } from "@/lib/usePagePathsAbove";
import type { Page, Session } from "@/types";
import type { Page as RowValuesPage, TextSpan } from "@/lib/types";
import type { PagePresentationChange } from "../page/pagePresentation";
import type { DatabaseRowPageLockControl, RowMenuAnchor, RowPageMenuExtras } from "./DatabaseRowMenu";
import type { DatabaseRowPlace } from "./DatabaseRowMovePopover";
import type { PropertyCellEnvironment } from "./propertyCellTypes";
import type { RowMenuFoot } from "./rowMenuFoot";
import type { RowPageTopBarActions } from "./RowPageTopBarItems";
import type { DatabaseRowMenuOpenMode } from "./useDatabaseRowViewMenu";
import type { DatabaseRowMoveTargetSearch } from "./useDatabaseRowMoveTargets";
import {
  rowHandleMenuAnchor,
  rowPageActionsAnchor,
  type DatabaseRowMoveDrop,
  type DatabaseRowMoveTarget,
  type DatabaseRowNotifications,
} from "./DatabaseRowMenu";
import { focusOpenerAfterPaint } from "@/lib/overlayLayer";
import { inlineStartX } from "@/lib/readingDirection";
import { useSuggestingMode } from "../editor/suggestingMode";
import { longPressKeptForWords, rowMenuOpenedByKey } from "./rowMenuByKey";
import { createRowMenuStore, RowContextMenuSurface, rowElementOf, rowPageReadOf, underItem } from "./RowContextMenuSurface";
import { focusedOpener } from "@/lib/lastPress";
import { pressedPoint } from "@/lib/pressedPoint";

const PageAnalyticsPanel = lazyScreen(() => (
  import("../PageAnalyticsPanel").then(({ PageAnalyticsPanel: Panel }) => ({ default: Panel }))
));
const PageHistoryDialog = lazyScreen(() => (
  import("../PageHistoryDialog").then(({ PageHistoryDialog: Dialog }) => ({ default: Dialog }))
));

/**
 * What a table's selected rows hand their "⋯", as Notion's menu for several
 * pages (seen 2026-11-02): their icon and a property set for all of them,
 * all their links, and duplicating, moving or trashing them together.
 */
export type SelectedRowsMenu = {
  rowIds: readonly string[];
  /** Their titles, which the menu's foot counts. */
  titles: readonly string[];
  onDuplicate(): void;
  onTrash(): void;
  /** Sets a property's value on every selected row. */
  onValue?(property: GraphProperty, value: unknown, spans?: TextSpan[]): void;
  /** Opened by ⌘/, under the first of them: no "Del" and no foot, as Notion's (seen 2026-11-07; its bar's "⋯" has both). */
  byKey?: boolean;
};

export type RowContextMenuOptions = {
  /** Row pages' updates and analytics, read through the page holding the rows, which is where they are. */
  analytics?: { workspaceId: string; pageId: string; service: PageAnalyticsPanelService; session?: Session;
    pathDatabase?: { blockId: string; title: string; href: string };
    /** A row open as a page shows them docked beside it under the top bar, as a page's (seen 2026-11-06). */
    docked?: boolean };
  busy?: boolean;
  moveTargets: DatabaseRowMoveTarget[];
  searchMoveTargets?: DatabaseRowMoveTargetSearch;
  notifications?: (rowId: string) => DatabaseRowNotifications;
  readOnly?: boolean;
  /** The open row's page's own "…" menu, where it differs from the rows' (rowPageUnderPageLock.ts). */
  pageReadOnly?: boolean;
  rowId: string;
  rowTitle: string;
  snapshot: DatabaseRowPageSnapshot | null;
  /** Each row's page as last read, the menu's own row's (useRowPageReads); `snapshot` the open row's. */
  snapshotOf?: (rowId: string) => DatabaseRowPageSnapshot | null;
  onArchive?: (rowId: string, archived: boolean) => Promise<void>;
  onCopyLink: (rowId: string) => Promise<void>;
  /** Copies several rows' links at once, a line each. */
  onCopyLinks?: (rowIds: readonly string[]) => Promise<void>;
  /** What acts on a row's page: none where rows are no pages (databasePagelessRows.ts). */
  onDuplicate?: (rowId: string) => Promise<void>;
  onFavorite?: (rowId: string, favorite: boolean) => Promise<void>;
  onMove?: (rowId: string, target: DatabaseRowMoveTarget, acceptDroppedPropertyIds: string[]) => Promise<void>;
  onPreviewMove?: (rowId: string, target: DatabaseRowMoveTarget) => Promise<DatabaseRowMoveDrop[]>;
  onOpenMode: (rowId: string, mode: DatabaseRowMenuOpenMode) => void;
  onTrash?: (rowId: string) => Promise<void>;
  onWarm?: (rowId: string) => void;
  /** The foot of a row with no page read (databasePagelessRows.ts). */
  foot?: (rowId: string) => RowMenuFoot | undefined;
  /** The row as its view holds it, whose last edit the menu's foot names until its page is read. */
  viewRow?: (rowId: string) => GraphRow | undefined;
  /** A row of the view as a page, whose name its menu is called by — the pressed row's, not the open one's (rowTitle). */
  viewRowPage?: (rowId: string) => RowValuesPage | undefined;
  /** Where a view's row's page is, which its "옮기기" takes until the page is read (databaseViewRowMoveSubject). */
  rowPlace?: (row: GraphRow) => DatabaseRowPlace | undefined;
  /** Changes the row page's icon, from the menu's "아이콘 편집". */
  onPresentation?: (rowId: string, change: PagePresentationChange) => Promise<boolean>;
  /** The view's cells, whose editors the menu's "속성 편집 ›" opens by the row. */
  cells?: { properties: readonly GraphProperty[]; environment: PropertyCellEnvironment; row(rowId: string): RowValuesPage | undefined;
    /** Of them, those its reader writes on every one of these rows, where that goes by the row (databasePagelessRows.ts). */
    editable?(rowIds: readonly string[]): readonly GraphProperty[] };
  /** A board's or gallery's view settings, at the layout or the properties. */
  viewSettings?: (screen: "layout" | "properties") => void;
  /** Where a row's new comment goes, from the menu's "댓글", and where its icon's uploads go. */
  comments?: { session: Session; workspaceId: string; pageId: string };
};

export function useRowContextMenu(options: RowContextMenuOptions): {
  openRowContextMenu: (rowId: string, event: MouseEvent<HTMLElement>, duplicate?: () => void) => void;
  openRowContextMenuAt: (rowId: string, anchor: RowMenuAnchor, returnTo?: HTMLElement | null) => void;
  openRowContextMenuFromElement: (rowId: string, returnTo?: HTMLElement | null, duplicate?: () => void) => void;
  /** Opens the menu for several selected rows, from their bar's "⋯". */
  openRowsContextMenuAt: (rows: SelectedRowsMenu, anchor: RowMenuAnchor, returnTo: HTMLElement) => void;
  /** Opens the menu from the row's open page's … button. */
  openRowPageActions: (
    rowId: string, trigger: HTMLElement, extras: RowPageMenuExtras, lock?: DatabaseRowPageLockControl,
  ) => void;
  rowContextMenu: ReactNode;
  /** The menu opened from the row's open page, shown with that page. */
  rowPageActionsMenu: ReactNode;
  /** Opens a row page's updates, where the rows have updates and analytics. */
  openRowUpdates?: (rowId: string) => void;
  /**
   * The open row page's top bar's part of them, the one way every row page
   * takes — beside its view and at its own address alike: its last edit's
   * time opens its updates, as Notion's (seen 2026-11-17), and "패널 닫기"
   * closes them where they dock beside it.
   */
  topBarUpdates: (rowId: string, docked: boolean) => Pick<RowPageTopBarActions, "onOpenUpdates" | "onClosePanel">;
  /** Whether updates and analytics are open, and how to close them: a docked panel's page makes room. */
  analyticsShown: boolean;
  closeAnalytics: () => void;
  /** Opens a new comment's field by a row or card, as its menu's "댓글" does — for ⌘⇧M. */
  commentOnRow?: (rowId: string, element: HTMLElement) => void;
} {
  // The menu open and what its lines opened by the row, which only the menu draws (RowContextMenuSurface).
  const [store] = useState(createRowMenuStore);
  // The row page's updates and analytics, on the tab it opened at.
  const [analyticsOpen, setAnalyticsOpen] = useState<{ rowId: string; tab: "updates" | "analytics" } | null>(null);
  const [historyVersion, setHistoryVersion] = useState<{ rowId: string; versionId: string; title: string } | null>(null);
  const returnToRef = useRef<HTMLElement | null>(null);

  // The control the closing menu came from takes the focus back once the closing is painted, unless another layer
  // opened meanwhile (focusOpenerAfterPaint): a right press at once opens the next menu, which keeps it.
  const closeRowContextMenu = useCallback(() => {
    store.set({ menu: null });
    const back = returnToRef.current;
    returnToRef.current = null;
    focusOpenerAfterPaint(back);
  }, [store]);

  const openRowContextMenuAt = useCallback((
    rowId: string,
    anchor: RowMenuAnchor,
    returnTo?: HTMLElement | null,
    duplicate?: () => void,
  ) => {
    // Someone who can only comment or read has no row menu, as in Notion (seen 2026-10-11).
    if (options.readOnly) return;
    // ⌘/ over a chosen row gives the focus back to the row's own control it was on — the box a press ticked — as
    // Notion's (seen 2026-11-24 in two tabs: Escape, the box ringed); else the row's (chosenItemFocus.ts).
    const focused = focusedOpener();
    returnToRef.current = returnTo && rowMenuOpenedByKey() && focused && focused !== returnTo && returnTo.contains(focused) ? focused : returnTo ?? focused;
    store.set({ menu: { rowId, anchor, element: rowElementOf(returnToRef.current), duplicate, pageItem: rowMenuOpenedByKey() } });
    options.onWarm?.(rowId);
  }, [options, store]);

  const openRowsContextMenuAt = useCallback((rows: SelectedRowsMenu, anchor: RowMenuAnchor, returnTo: HTMLElement) => {
    if (options.readOnly || rows.rowIds.length === 0) return;
    returnToRef.current = returnTo;
    store.set({ menu: { rowId: rows.rowIds[0], rows, anchor, element: returnTo } });
  }, [options.readOnly, store]);

  const openRowContextMenuFromElement = useCallback((rowId: string, returnTo?: HTMLElement | null, duplicate?: () => void) => {
    const target = returnTo ?? focusedOpener();
    const rect = target?.getBoundingClientRect();
    const under = rowMenuOpenedByKey() && target ? underItem(target) : null;
    openRowContextMenuAt(
      rowId,
      under ? { x: inlineStartX(under, 0, target), y: under.bottom, top: under.top }
        : target?.matches("[data-row-menu-handle]") ? rowHandleMenuAnchor(target)
          : rect && target ? { x: inlineStartX(rect, 16, target), y: rect.top + Math.min(rect.height, 32) } : { x: 16, y: 16 },
      target,
      duplicate,
    );
  }, [openRowContextMenuAt]);

  const openRowPageActions = useCallback((
    rowId: string, trigger: HTMLElement, extras: RowPageMenuExtras, lock?: DatabaseRowPageLockControl,
  ) => {
    returnToRef.current = trigger;
    store.set({ menu: { rowId, anchor: rowPageActionsAnchor(trigger), rowPage: { ...extras, lock } } });
    options.onWarm?.(rowId);
  }, [options, store]);

  const openRowContextMenu = useCallback((rowId: string, event: MouseEvent<HTMLElement>, duplicate?: () => void) => {
    // The browser's own menu stays for someone with no row menu, and for a long press on words being written.
    if (options.readOnly || longPressKeptForWords(event)) return;
    event.preventDefault();
    event.stopPropagation();
    const rect = event.currentTarget.getBoundingClientRect();
    const keyboardFallback = event.clientX === 0 && event.clientY === 0;
    openRowContextMenuAt(
      rowId,
      keyboardFallback ? { x: inlineStartX(rect, 16, event.currentTarget), y: rect.top + Math.min(rect.height, 32) } : pressedPoint(event),
      event.currentTarget,
      duplicate,
    );
  }, [openRowContextMenuAt, options.readOnly]);

  const analytics = options.analytics;
  // The page's path, as its bar's once the server named it (pagePathPieces.ts), with its source.
  usePagePathsAbove(analytics && analyticsOpen ? [{ id: analyticsOpen.rowId, workspaceId: analytics.workspaceId }] : []);
  const namedAbove = analyticsOpen ? knownPathAboveId(analyticsOpen.rowId) : undefined;
  // Read from the page list while its dialog is open: a list read again draws
  // the database under it again only then (productPagesStore.tsx).
  const pathRead = useProductPagesSelect((productPages) => analytics && analyticsOpen ? JSON.stringify(pagePathLinks(namedAbove || analytics.pageId,
    pagesByIdOf(productPages?.pages ?? []))) : "");
  const pathLinks: Array<{ id: string; title: string; href?: string }> = pathRead ? JSON.parse(pathRead) : [];
  if (analytics?.pathDatabase && !namedAbove) pathLinks.push({ id: analytics.pathDatabase.blockId, title: analytics.pathDatabase.title, href: analytics.pathDatabase.href });
  const updateSnapshot = rowPageReadOf(options, analyticsOpen?.rowId ?? historyVersion?.rowId);
  const titleProperty = updateSnapshot?.properties.find((property) => property.type === "title");
  const snapshotTitle = titleProperty ? updateSnapshot?.row.values[titleProperty.id]?.value : undefined;
  const updateTitle = analyticsOpen?.rowId === options.rowId ? options.rowTitle : typeof snapshotTitle === "string" ? snapshotTitle : "";
  const analyticsPanel = analytics && (analyticsOpen || historyVersion) ? (
    <Suspense fallback={null}>
      {analyticsOpen ? <PageAnalyticsPanel key={`${analyticsOpen.rowId}:${analyticsOpen.tab}`} initialTab={analyticsOpen.tab} docked={analytics.docked}
        pageId={analytics.pageId} rowId={analyticsOpen.rowId} workspaceId={analytics.workspaceId}
        service={analytics.service} onClose={() => setAnalyticsOpen(null)}
        onOpenPageHistory={analytics.session ? (versionId) => {
          setHistoryVersion({ rowId: analyticsOpen.rowId, versionId: versionId ?? "", title: updateTitle });
          setAnalyticsOpen(null);
        } : undefined}
        updates={analytics.session ? {
          pageTitle: updateTitle,
          path: pathLinks.map(({ title }) => title).join(" / "), pathLinks, session: analytics.session,
          titleLabel: (titleProperty ?? options.cells?.properties.find((property) => property.type === "title"))?.name,
          properties: updateSnapshot?.properties ?? options.cells?.properties,
          canEdit: !options.readOnly,
        } : undefined} /> : null}
      {historyVersion && analytics.session ? <PageHistoryDialog overPage session={analytics.session}
        page={{ id: historyVersion.rowId, title: historyVersion.title, workspaceId: analytics.workspaceId, databaseRow: true,
          isLocked: updateSnapshot?.row.locked && !updateSnapshot.row.unlockedForMe } as Page}
        properties={updateSnapshot?.properties} initialVersionId={historyVersion.versionId}
        canEdit={!options.readOnly && (updateSnapshot?.accessRole === "edit" || updateSnapshot?.accessRole === "full_access")}
        onClose={() => setHistoryVersion(null)} onPageChange={() => options.onWarm?.(historyVersion.rowId)} /> : null}
    </Suspense>
  ) : null;
  const suggesting = useSuggestingMode();
  const openRowUpdates = analytics ? (rowId: string) => { options.onWarm?.(rowId); setAnalyticsOpen({ rowId, tab: "updates" }); } : undefined;
  const closeAnalytics = () => setAnalyticsOpen(null);
  const surface = { store, options, close: closeRowContextMenu, suggesting, returnToRef,
    onAnalytics: analytics ? (rowId: string) => setAnalyticsOpen({ rowId, tab: "updates" }) : undefined };
  return {
    openRowContextMenu,
    openRowContextMenuAt,
    openRowContextMenuFromElement,
    openRowsContextMenuAt,
    openRowPageActions,
    rowContextMenu: <RowContextMenuSurface {...surface} analyticsPanel={analyticsPanel} />,
    rowPageActionsMenu: <RowContextMenuSurface {...surface} page />,
    openRowUpdates,
    topBarUpdates: (rowId, docked) => ({ onOpenUpdates: openRowUpdates && (() => openRowUpdates(rowId)), onClosePanel: docked ? closeAnalytics : undefined }),
    analyticsShown: Boolean(analyticsOpen),
    closeAnalytics,
    commentOnRow: options.comments && !options.readOnly ? (rowId, element) => {
      const row = rowElementOf(element), anchor = popoverRectOf(row);
      if (!row || !anchor) return;
      options.onWarm?.(rowId);
      store.set({ menu: null, byRow: { kind: "comment", rowId, element: row, anchor } });
    } : undefined,
  };
}
