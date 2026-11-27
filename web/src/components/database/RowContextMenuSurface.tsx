"use client";

import { Activity, type ComponentProps, memo, type ReactNode, type RefObject, useMemo, useState, useSyncExternalStore } from "react";
import { useTranslation } from "react-i18next";
import { popoverRectOf, type PopoverRect } from "@/lib/popoverPlacement";
import { useProductToast } from "@/lib/productToast";
import type { TextSpan } from "@/lib/types";
import type { DatabaseRowPageSnapshot } from "@/databaseRowPageWire.generated";
import { rowMenuDocumentStats } from "@/lib/rowMenuDocumentStats";
import { usePageLastEditVia } from "@/lib/pageEditedActivity";
import { canPresentPage, requestPresentation } from "@/lib/pageSlides";
import { untitledPageDisplayTitle } from "@/lib/pageTitle";
import { hasDesktopTabs } from "@/lib/desktopMenuCommands";
import { pageHref } from "@/lib/navigation";
import { openPageInNewWindow } from "@/lib/newWindowRequests";
import { CommentsPanel } from "../CommentsPanel";
import { AnchoredIconPicker, PICKER_WIDTH as SELECTED_ROWS_PICKER_WIDTH } from "../page/AnchoredEmojiPicker";
import type { PagePresentationChange } from "../page/pagePresentation";
import { PageRenamePopover, renamePopoverUnder } from "../page/PageRenamePopover";
import type { SuggestingMode } from "../editor/suggestingMode";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { usePageMenuWarm } from "../page/TopBarPageMenu";
import { openWholeWithUpdates } from "../page/usePageSidePanel";
import { WikiPageMenu } from "../product/WikiPageMenu";
import { wikiPageMenuHost } from "../product/wikiPageMenuHost";
import { DatabaseRowMenu, type DatabaseRowMoveTarget, type DatabaseRowPageLockControl, type RowMenuAnchor, type RowPageMenuExtras } from "./DatabaseRowMenu";
import { databaseRowMoveSubject, databaseViewRowMoveSubject } from "./DatabaseRowMovePopover";
import { DatabaseRowPropertyPopover } from "./DatabaseRowPropertyPopover";
import { galleryPresentationProperty } from "./galleryViewModel";
import { rowMenuFoot, rowPageMenuFoot, viewRowMenuFoot } from "./rowMenuFoot";
import { SelectedRowsPropertyEditor, selectionEditableProperties } from "./TableSelectionProperties";
import type { RowContextMenuOptions, SelectedRowsMenu } from "./useRowContextMenu";
import { useRowDisplayTitle } from "./untitledRowName";

/** A row's menu as opened: by its row or card, by several rows, or by its open page's "…". */
export type RowMenuState = {
  rowId: string;
  /** Several rows a table's selection opened it for. */
  rows?: SelectedRowsMenu;
  anchor: RowMenuAnchor;
  /** The row or card the menu was opened from, where its icon picker, value editor and comment field open. */
  element?: HTMLElement | null;
  /** The row's open page asked for it from its …, with its suggesting mode and lock. */
  rowPage?: RowPageMenuExtras & { lock?: DatabaseRowPageLockControl };
  /** The view's own duplicate, from the line the menu was opened on, which leaves the copy chosen (TableView, cardSelection copyHere). */
  duplicate?: () => void;
  /** Opened by ⌘/ over the row or card alone: Notion's page item menu (rowMenuByKey.ts). */
  pageItem?: boolean;
};

/** What a menu line opened by its row: the icon picker, a value's editor, a new comment's field or the rename. */
export type ByRow = { kind: "icon" | "property" | "comment" | "rename"; rowId: string; element: HTMLElement; anchor: PopoverRect; propertyId?: string;
  /** Several selected rows the icon or value goes to, opened where their menu was. */
  rows?: SelectedRowsMenu; at?: { x: number; y: number } };

type Shown = { menu: RowMenuState | null; byRow: ByRow | null };

/**
 * Which row menu is open, kept apart from the view that opens it: a press
 * that opens or closes one draws the menu alone, not the view, its open row
 * page or the toolbar holding the hook (user decision 2026-11-12, a press
 * redraws only what changed: a row's ⋮⋮ drew 146 parts of the view in its
 * press frame, an open row page's "…" 285 with its page, 2026-11-13).
 */
export type RowMenuStore = {
  get(): Shown;
  set(change: Partial<Shown>): void;
  subscribe(listener: () => void): () => void;
};

export function createRowMenuStore(): RowMenuStore {
  let shown: Shown = { menu: null, byRow: null };
  const listeners = new Set<() => void>();
  return {
    get: () => shown,
    set(change) {
      shown = { ...shown, ...change };
      for (const listener of listeners) listener();
    },
    subscribe(listener) {
      listeners.add(listener);
      return () => { listeners.delete(listener); };
    },
  };
}

/**
 * The open page's lock as the page now holds it: its "…" stays open as its
 * lock switch turns (Notion's, seen 2026-11-16), so it shows the lock the
 * page was read again with, not the one it opened with.
 */
function liveLock(lock: DatabaseRowPageLockControl | undefined, snapshot: DatabaseRowPageSnapshot | null) {
  return lock && snapshot ? { ...lock, locked: snapshot.row.locked, unlockedForMe: snapshot.row.unlockedForMe } : lock;
}

const ROW_ITEMS = "[data-row-card], [data-table-row-id], [data-list-row-id], [data-timeline-row-id], [data-feed-post]";

/**
 * Where ⌘/'s page item menu stands under the row or card, and its "이름
 * 바꾸기" opens: from a card's start, or 8 before a row's first cell as
 * several rows' (TableView) — Notion's (seen 2026-11-12: a row with its first
 * cell at 496 at 488 under it, a board's card at 504 at 504 under it); the
 * menu turns over to its `top` (DatabaseRowMenu).
 */
export function underItem(element: HTMLElement) {
  const item = element.closest<HTMLElement>(`${ROW_ITEMS}, [data-card-choice]`) ?? element;
  const start = item.querySelector<HTMLElement>("[data-table-cell]");
  const from = (start ?? item).getBoundingClientRect(), before = start ? 8 : 0;
  const { top, bottom } = item.getBoundingClientRect();
  return { left: from.left - before, right: from.right + before, top, bottom };
}

/** The row, card or feed post a menu opened from — a table row's title cell, as Notion's opens there — else the element itself. */
export function rowElementOf(element: HTMLElement | null | undefined) {
  const row = element?.closest<HTMLElement>(ROW_ITEMS) ?? element ?? null;
  return row?.querySelector<HTMLElement>("[data-table-cell][data-title='true']") ?? row;
}

// Put away, a menu drawn ahead keeps what it was drawn with: each new
// drawing of the view or the page doesn't draw it again too.
const HeldRowMenu = memo(function HeldRowMenu({ shown, ...props }: ComponentProps<typeof DatabaseRowMenu> & { shown: boolean }) {
  return <DatabaseRowMenu {...props} ahead={!shown} />;
}, (_before, next) => !next.shown);

const AHEAD_AT = { x: 0, y: 0 };

/**
 * The row menu and what its lines open by the row (in its view), or the
 * menu an open row page's "…" opened (`page`), drawn from the store alone.
 */
export function RowContextMenuSurface({ store, options, close, onAnalytics, suggesting, returnToRef, analyticsPanel, page = false }: {
  store: RowMenuStore;
  options: RowContextMenuOptions;
  close: () => void;
  onAnalytics?: (rowId: string) => void;
  suggesting: SuggestingMode;
  returnToRef: RefObject<HTMLElement | null>;
  /** The view's updates and analytics, shown while no row's own menu is. */
  analyticsPanel?: ReactNode;
  page?: boolean;
}) {
  const { t } = useTranslation(["blockHandle", "rowMenu"]);
  const { notify } = useProductToast();
  const shown = useSyncExternalStore(store.subscribe, store.get, store.get);
  // Each place draws its own: the view a row's, the open page's bar its "…".
  const open = page ? (shown.menu?.rowPage ? shown.menu : null) : (shown.menu?.rowPage ? null : shown.menu);
  const byRow = page ? null : shown.byRow;
  // A wiki's page's "…" is the page menu, the one it has opened whole, in a
  // peek too — the sidebar's and its wiki's view's alike (WikiPageMenu).
  const wikiHost = page && options.snapshot?.wiki && !options.snapshot.template ? wikiPageMenuHost() : null;
  const wikiPage = wikiHost?.pages.find(({ id }) => id === options.snapshot?.row.id);
  const footSession = options.comments?.session ?? options.analytics?.session;
  const workspaceId = options.comments?.workspaceId ?? options.analytics?.workspaceId;
  // Once the start has settled the menu is drawn ahead, hidden (Activity), as
  // the page's "…" is (TopBarPageMenu): its press shows what is there and
  // draws what is the row's, the open page's or the selection's (2026-11-13,
  // NAS condition, CPU ×2). Each opening is a new menu, the one drawn ahead
  // after the last one closed. Not on a phone, whose menus are sheets.
  const { phone } = useSidebarLayout(true);
  const [warm] = usePageMenuWarm(page ? options.snapshot?.row.id : "rows");
  const ahead: RowMenuState | null = open || !warm || phone || wikiPage || (page ? options.pageReadOnly ?? options.readOnly : options.readOnly) ? null : !page ? { rowId: options.rowId, anchor: AHEAD_AT }
    : options.snapshot && footSession && workspaceId ? { rowId: options.snapshot.row.id, anchor: AHEAD_AT, rowPage: { page: {
      id: options.snapshot.row.id, title: options.rowTitle, workspaceId, session: footSession, dataSourceId: options.snapshot.row.dataSourceId,
    } } } : null;
  const menu = open ?? ahead;
  const [round, setRound] = useState(0);
  const [wasOpen, setWasOpen] = useState(false);
  if (wasOpen !== Boolean(open)) {
    setWasOpen(Boolean(open));
    if (!open) setRound((count) => count + 1);
  }
  const rowSnapshot = rowPageReadOf(options, menu?.rowId);
  // A row's menu is called by the row it was opened on, as its view names it
  // ("새 페이지", a task tracker's "새 작업" when untitled), not by the row
  // open beside the view: every row's read "새 페이지의 데이터베이스 행 작업"
  // (2026-11-18). Notion's has no name at all (seen 2026-11-18: an unnamed
  // dialog and list); the open row's page menu keeps the page's own title.
  const pressedRow = menu && !menu.rows && !menu.rowPage ? options.viewRowPage?.(menu.rowId) : undefined;
  const rowName = useRowDisplayTitle();
  const menuTitle = pressedRow ? rowName(pressedRow) : options.rowTitle;
  // Until the row's page is read (onWarm), its view's row names its last edit.
  const viewRow = !rowSnapshot && menu && !menu.rows ? options.viewRow?.(menu.rowId) : undefined;
  const footRow = rowSnapshot?.row ?? viewRow;
  const rowVia = usePageLastEditVia(footSession, open?.rowId ?? "",
    footRow ? footRow.lastEditedBy || footRow.createdBy || null : null, Boolean(open && footRow));
  const onArchive = options.onArchive;
  const notificationsFor = options.notifications;
  const notifications = useMemo(
    () => (open && notificationsFor ? notificationsFor(open.rowId) : undefined),
    // Each opened row reads its own level once.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [open?.rowId],
  );
  // The lines a row's menu in its view adds, each opening by the row it was opened from.
  const openByRow = (kind: ByRow["kind"], propertyId?: string) => {
    const element = menu?.element, anchor = popoverRectOf(element);
    if (!menu || !element?.isConnected || !anchor) return;
    // Several rows' icon picker and value editor open where their menu
    // opened, as Notion's (seen 2026-11-02): the picker centred 4px under
    // that point, the editor at it.
    store.set({ byRow: menu.rows
      ? { kind, rowId: menu.rowId, element, propertyId, rows: menu.rows, at: menu.anchor,
        anchor: { top: menu.anchor.y, bottom: menu.anchor.y, left: menu.anchor.x - SELECTED_ROWS_PICKER_WIDTH / 2,
          right: menu.anchor.x - SELECTED_ROWS_PICKER_WIDTH / 2 } }
      : { kind, rowId: menu.rowId, element, anchor, propertyId } });
  };
  const cells = options.cells;
  // A row's name, as its title cell writes it, from ⌘/'s "이름 바꾸기".
  const titleAt = cells ? cells.properties.findIndex(({ type }) => type === "title") : -1;
  const renameRow = cells && titleAt >= 0 && !options.readOnly ? (rowId: string, title: string, spans?: TextSpan[]) => void cells.environment.operations
    .setTextValue(rowId, galleryPresentationProperty(cells.properties[titleAt], titleAt), title, cells.environment.context, spans) : undefined;
  // "여기에서 발표" presents the page the view stands in from its block, as the block's own menu does.
  const presentFrom = (element: HTMLElement) => {
    const pageId = element.closest<HTMLElement>("[data-editor-page]")?.dataset.editorPage;
    const blockId = element.closest<HTMLElement>("[data-block-id]")?.dataset.blockId;
    return pageId && canPresentPage(pageId) ? () => requestPresentation({ pageId, blockId }) : undefined;
  };
  // A timeline's bars and rows open as Notion's timeline bar menu does.
  const timelineRow = Boolean(menu?.element?.closest("[data-timeline-row-id], [data-timeline-table-row-id]"));
  const selectedRows = menu?.rows;
  const rowLines = selectedRows ? {
    byKey: selectedRows.byKey,
    onEditIcon: options.onPresentation ? () => openByRow("icon") : undefined,
    editProperties: cells && selectedRows.onValue && !options.readOnly
      ? { properties: selectionEditableProperties(cells.editable?.(selectedRows.rowIds) ?? cells.properties), onEdit: (propertyId: string) => openByRow("property", propertyId) } : undefined,
  } : menu?.element && !menu.rowPage ? {
    onEditIcon: options.onPresentation ? () => openByRow("icon") : undefined,
    editProperties: cells?.row(menu.rowId) && !options.readOnly
      ? { properties: cells.editable?.([menu.rowId]) ?? cells.properties, onEdit: (propertyId: string) => openByRow("property", propertyId) } : undefined,
    onViewSettings: menu.pageItem ? undefined : options.viewSettings,
    onComment: options.comments && (!timelineRow || menu.pageItem) ? () => openByRow("comment") : undefined,
    openInline: timelineRow || menu.pageItem,
    byKey: menu.pageItem,
    pageItem: menu.pageItem ? {
      onRename: renameRow && cells?.row(menu.rowId) ? () => openByRow("rename") : undefined,
      onSuggest: suggesting.onChange ? () => suggesting.onChange?.(!suggesting.active) : undefined,
      onPresent: presentFrom(menu.element),
    } : undefined,
  } : undefined;
  const closeByRow = () => store.set({ byRow: null });
  const byRowSnapshot = rowPageReadOf(options, byRow?.rowId);
  const presentation = options.onPresentation;
  const changeIcon = (change: PagePresentationChange) => {
    // Several rows take the icon one after another, as each row's page saves it.
    const rowIds = byRow?.rows?.rowIds ?? (byRow ? [byRow.rowId] : []);
    void (async () => { for (const rowId of rowIds) await presentation?.(rowId, change); })();
    closeByRow();
  };
  const cellIndex = byRow?.kind === "property" && cells ? cells.properties.findIndex(({ id }) => id === byRow.propertyId) : -1;
  const cellRow = cellIndex >= 0 && byRow ? cells?.row(byRow.rowId) : undefined;
  const byRowsProperty = byRow?.kind === "property" && byRow.rows?.onValue && byRow.at && cells
    ? cells.properties.find(({ id }) => id === byRow.propertyId) : undefined;
  const renamed = byRow?.kind === "rename" && renameRow ? cells?.row(byRow.rowId) : undefined;
  const byRowSurface = !byRow ? null : renamed && byRow.kind === "rename" ? (
    // Under the row or card, from its start, as Notion's (seen 2026-11-12: under a card at 504,247–287, at 504,291).
    <PageRenamePopover page={{ ...renamed, icon: renamed.icon ?? "", iconType: renamed.iconType ?? "none" }} title={renamed.title}
      titleSpans={renamed.titleSpans} untitled={untitledPageDisplayTitle(renamed)} at={renamePopoverUnder(byRow.element, underItem(byRow.element))}
      onTitleChange={(title, spans) => renameRow?.(byRow.rowId, title, spans)} onClose={closeByRow}
      onPresentationChange={presentation ? (change) => void presentation(byRow.rowId, change) : undefined} />
  ) : byRowsProperty && byRow.rows?.onValue && byRow.at && cells && cellRow ? (
    <SelectedRowsPropertyEditor at={byRow.at} environment={cells.environment} page={cellRow} property={byRowsProperty}
      onValue={byRow.rows.onValue} onClose={closeByRow} />
  ) : byRow.kind === "icon" && presentation ? (
    // Several rows' picker is centred under the point by its own left (no `right`).
    <AnchoredIconPicker anchor={byRow.rows ? { top: byRow.anchor.top, bottom: byRow.anchor.bottom, left: byRow.anchor.left } : byRow.anchor}
      marker="row-icon-picker" onClose={closeByRow} picker={{
      // Several rows' picker has no upload tab, as Notion's (seen 2026-11-02: 이모지 · 아이콘).
      current: byRow.rows ? undefined : byRowSnapshot?.row,
      uploadTarget: options.comments && !byRow.rows ? { workspaceId: options.comments.workspaceId, pageId: byRow.rowId } : undefined,
      onPick: (emoji) => changeIcon({ icon: emoji, iconType: "emoji" }),
      onPickImage: byRow.rows ? undefined : (url) => changeIcon({ icon: url, iconType: "image" }),
      onPickCustomEmoji: (emoji) => changeIcon({ icon: emoji.id, iconType: "custom_emoji" }),
      onPickBuiltInIcon: (value) => changeIcon({ icon: value, iconType: "icon" }),
      onRemove: byRowSnapshot?.row.icon && !byRow.rows ? () => changeIcon({ icon: "", iconType: "none" }) : undefined,
    }} />
  ) : byRow.kind === "property" && cells && cellRow ? (
    <DatabaseRowPropertyPopover anchor={byRow.anchor} row={cellRow} property={galleryPresentationProperty(cells.properties[cellIndex], cellIndex)}
      environment={cells.environment} onClose={closeByRow} />
  ) : byRow.kind === "comment" && options.comments ? (
    <CommentsPanel key={`row-comment:${byRow.rowId}`} compose anchorElement={byRow.element} rowId={byRow.rowId}
      pageId={byRowSnapshot?.row.containerPageId ?? options.comments.pageId} workspaceId={options.comments.workspaceId}
      session={options.comments.session} canComment={byRowSnapshot?.accessRole !== "view"}
      canEditPage={byRowSnapshot?.accessRole === "edit" || byRowSnapshot?.accessRole === "full_access"}
      onClose={closeByRow} onStatus={(message) => notify(message, "error")} />
  ) : null;
  // Several rows' menu, as Notion's for several pages (seen 2026-11-02): no
  // favorite, opening or page lines; "모든 링크 복사", duplicating, moving and
  // trashing all of them; its foot counts their titles' words and letters.
  const rows = menu?.rows;
  const rowsCounts = rows ? rowMenuDocumentStats([...rows.titles], 0) : null;
  // The menu closes at the press; what it set off goes on behind it (user decision 2026-11-12).
  const onMove = options.onMove;
  const moveRows = rows && onMove ? async (target: DatabaseRowMoveTarget, accepted: string[]) => {
    close();
    for (const rowId of rows.rowIds) await onMove(rowId, target, accepted);
  } : undefined;
  const copyRowsLinks = options.onCopyLinks;
  // Rows that are no pages are neither copied, moved nor trashed, nor an app's target (databasePagelessRows.ts).
  const pages = Boolean(options.onTrash);
  const drawn = menu && rows && rowsCounts ? (
    <HeldRowMenu key={round} shown={Boolean(open)}
      anchor={menu.anchor}
      busy={options.busy}
      favorite={false}
      moveTargets={options.moveTargets}
      searchMoveTargets={options.searchMoveTargets}
      readOnly={options.readOnly}
      rowTitle={rows.titles.join(", ")}
      copyLinkLabel={t("rowMenu:actions.copyAllLinks")}
      onClose={close}
      onCopyLink={async () => { close(); await (copyRowsLinks ? copyRowsLinks(rows.rowIds) : options.onCopyLink(menu.rowId)); }}
      onDuplicate={pages ? async () => { rows.onDuplicate(); close(); } : undefined}
      onMove={moveRows}
      onPreviewMove={options.onPreviewMove ? (target) => options.onPreviewMove!(menu.rowId, target) : undefined}
      onTrash={pages ? async () => { rows.onTrash(); close(); } : undefined}
      lines={rowLines}
      foot={rows.byKey ? undefined : { at: "", counts: t("blockHandle:footer.counts", { words: rowsCounts.words, chars: rowsCounts.characters }) }}
    />
  ) : menu ? (
    <HeldRowMenu key={round} shown={Boolean(open)}
      anchor={menu.anchor}
      archived={Boolean(rowSnapshot?.row.archivedAt)}
      busy={options.busy}
      favorite={rowSnapshot?.row.favorite ?? false}
      moveTargets={options.moveTargets}
      searchMoveTargets={options.searchMoveTargets}
      notifications={notifications}
      readOnly={menu.rowPage ? options.pageReadOnly ?? options.readOnly : options.readOnly}
      rowTitle={menuTitle}
      suggesting={menu.rowPage?.suggesting}
      lock={liveLock(menu.rowPage?.lock, rowSnapshot)}
      pageStyle={menu.rowPage?.style}
      onHistory={menu.rowPage?.onHistory}
      onExport={menu.rowPage?.onExport}
      onImport={menu.rowPage?.onImport}
      onCustomizeLayout={menu.rowPage?.onCustomizeLayout}
      page={menu.rowPage?.page}
      variant="database-row"
      onArchive={onArchive
        ? async (archived) => { close(); await onArchive(menu.rowId, archived); }
        : undefined}
      // A peek's page opens whole for its updates, docked beside it, as Notion's (usePageSidePanel); elsewhere they open here.
      onAnalytics={onAnalytics ? () => {
        const whole = menu.rowPage?.openWhole;
        if (whole) { close(); openWholeWithUpdates(menu.rowId, whole); } else { onAnalytics(menu.rowId); close(); }
      } : undefined}
      onClose={close}
      onCopyLink={async () => { close(); await options.onCopyLink(menu.rowId); }}
      onDuplicate={options.onDuplicate ? async () => {
        // The view's duplicate chooses the copy; the cell or item the menu came from isn't focused again.
        if (menu.duplicate) { returnToRef.current = null; close(); menu.duplicate(); return; }
        close(); await options.onDuplicate?.(menu.rowId);
      } : undefined}
      onFavorite={options.onFavorite ? (favorite) => options.onFavorite!(menu.rowId, favorite) : undefined}
      onMove={onMove ? async (target, accepted) => { close(); await onMove(menu.rowId, target, accepted); } : undefined}
      onPreviewMove={options.onPreviewMove ? (target) => options.onPreviewMove!(menu.rowId, target) : undefined}
      // Read or not, "옮기기" opens the pages' move: until the row's page is read, with the row as its view holds it.
      moveSubject={rowSnapshot ? databaseRowMoveSubject(rowSnapshot, pressedRow ? pressedRow.title : options.rowTitle)
        : viewRow ? databaseViewRowMoveSubject(viewRow, options.rowPlace?.(viewRow)) : undefined}
      onOpenMode={(mode) => { close(); options.onOpenMode(menu.rowId, mode); }}
      onOpenWindow={hasDesktopTabs() ? () => { close(); openPageInNewWindow(pageHref(menu.rowId)); } : undefined}
      onTrash={options.onTrash ? async () => { close(); await options.onTrash?.(menu.rowId); } : undefined}
      appTargetId={pages ? menu.rowId : undefined}
      lines={rowLines}
      foot={!rowSnapshot ? options.foot?.(menu.rowId) ?? (viewRow ? { ...viewRowMenuFoot(viewRow, menu.pageItem || menu.rowPage ? null
        : (words, chars) => t("blockHandle:footer.counts", { words, chars }), footSession?.user.id ?? ""), via: rowVia } : undefined) : menu.pageItem
        // ⌘/'s names when and by whom it was last edited, with no counts (Notion's, seen 2026-11-12).
        ? { ...rowMenuFoot(rowSnapshot, () => "", footSession?.user.id ?? ""), counts: undefined, via: rowVia } : menu.rowPage ? { ...rowPageMenuFoot(rowSnapshot, (count) => t("rowMenu:meta.wordCount", { count }),
          footSession?.user.id ?? ""), via: rowVia }
        : { ...rowMenuFoot(rowSnapshot, (words, chars) => t("blockHandle:footer.counts", { words, chars }), footSession?.user.id ?? ""), via: rowVia }}
    />
  ) : null;
  const menuSurface = drawn ? <Activity mode={open ? "visible" : "hidden"}>{drawn}</Activity> : null;
  if (page && wikiHost && wikiPage) {
    const whole = open?.rowPage?.openWhole;
    return open ? (
      <WikiPageMenu page={wikiPage} pages={wikiHost.pages} session={wikiHost.session} favorite={wikiHost.favorite(wikiPage.id)}
        menu={wikiHost.actions(wikiPage)} anchor={open.anchor} canEdit={!(options.pageReadOnly ?? options.readOnly)}
        suggesting={open.rowPage?.suggesting} style={open.rowPage?.style} onClose={close}
        // Its updates open with the page whole, docked beside it, as a peek's do (usePageSidePanel).
        onOpenUpdatesPanel={whole ? () => openWholeWithUpdates(open.rowId, whole) : onAnalytics && (() => onAnalytics(open.rowId))} />
    ) : null;
  }
  if (page) return menuSurface;
  // The view's updates and analytics give way to a row's own menu.
  return <>{menuSurface}{open ? null : analyticsPanel}{byRowSurface}</>;
}

/** A row's page as last read, for its menu, its comment or its updates: its own read, else the open row's if it is that row. */
export function rowPageReadOf(options: Pick<RowContextMenuOptions, "snapshot" | "snapshotOf">, rowId: string | undefined): DatabaseRowPageSnapshot | null {
  if (!rowId) return null;
  return options.snapshotOf?.(rowId) ?? (options.snapshot?.row.id === rowId ? options.snapshot : null);
}
