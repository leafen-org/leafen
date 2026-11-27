"use client";

import { AppMenuLines } from "@/components/apps/AppMenuLines";
import { NativeActionMenu, NativeMenuHeader, NativeMenuSection, NativeMenuSeparator } from "@/lib/nativeActionMenu";

import {
  type CSSProperties,
  type PointerEventHandler,
  type ReactNode,
  type RefObject,
  useEffect,
  useRef,
  useState,
} from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { placeAround, placeAtPoint, popoverBackdropStyle, popoverPlacementStyle, popoverRectAt, usePopoverPlacement,
  type PopoverRect, type PopoverRequest, type PopoverViewport } from "@/lib/popoverPlacement";
import { useProductToast } from "@/lib/productToast";
import type { DatabaseRowNotificationLevel } from "@/notificationSettingsApi";
import type { PageSuggestingControl } from "../RowMenu";
import { PageStyleControls, type PageStyle, type PageStyleChange } from "../PageStyleControls";
import { PageActionSearch } from "../PageActionSearch";
import styles from "../RowMenu.module.css";
import rowStyles from "./DatabaseRowMenu.module.css";
import { TouchSheetLayout, useSidebarLayout } from "@/components/shell/AppShell";
import type { DatabaseRowMoveTargetSearch } from "./useDatabaseRowMoveTargets";
import { keepLayerKeys } from "@/lib/overlayLayer";
import { useDatabaseRowPageMenu, type RowPageMenuPage } from "./useDatabaseRowPageMenu";
import { useDatabaseRowViewMenu, type DatabaseRowMenuLines, type DatabaseRowMenuOpenMode } from "./useDatabaseRowViewMenu";
import { RowMenuEditedFoot } from "../RowMenuEditedFoot";
import { RowMenuPhoneSheet } from "../RowMenuPhoneSheet";
import { DatabaseRowPhoneSheet } from "./DatabaseRowPhoneSheet";
import { DatabaseRowMovePhoneList } from "./DatabaseRowMoveSubmenu";
import { pageMenuCap } from "../RowMenuPlaced";
import { requestRowPageShare } from "./rowPageShareRequest";
import { DatabaseRowMovePopover, databaseRowMoveAnchor, type DatabaseRowMoveSubject } from "./DatabaseRowMovePopover";
import type { MovePopoverAnchor } from "@/components/movePopoverPlacement";
import type { RowMenuFoot } from "./rowMenuFoot";
import { untitledPageDisplayTitle } from "../../lib/pageTitle";
import { inlineEndX, inlineStartX } from "@/lib/readingDirection";
import { pageMenuShowsEditedFoot } from "@/lib/pageReaderChrome";

/** A database a row can move to, or a page it can move under as a sub-page. */
export type DatabaseRowMoveTarget = { id: string; title: string; kind?: "database" | "page" };

/** A property whose value the move target cannot hold. */
export type DatabaseRowMoveDrop = { propertyId: string; name: string };

/**
 * The row page's lock, from its own … menu, for someone who can edit it:
 * locking it for everyone, and unlocking it only for themselves while it
 * is locked. A lock settles once the page is read again with it.
 */
export type DatabaseRowPageLockControl = {
  locked: boolean;
  unlockedForMe: boolean;
  onLock(locked: boolean): void | Promise<unknown>;
  onLockForMe(unlocked: boolean): void;
};

/** What a database page's own … adds to its row's menu. */
export type RowPageMenuExtras = {
  suggesting?: PageSuggestingControl;
  onHistory?: () => void;
  /** The page's font, small text and full width, where this person may change them. */
  style?: { value: PageStyle; onChange(change: PageStyleChange): void };
  /** Opens the page's export. */
  onExport?: () => void;
  /** Opens the import screen, which imports files into the page. */
  onImport?: () => void;
  /** The open row page itself, whose own … takes the page menu's rows. */
  page?: RowPageMenuPage;
  /** Opens the database's page layout, where the database may change. */
  onCustomizeLayout?: () => void;
  /** In a peek, opens the page whole as its ⤢ does: its "업데이트와 애널리틱스" opens it so, its updates beside it (usePageSidePanel). */
  openWhole?: () => void;
};

/** Reads and saves how the viewer follows the row page. */
export type DatabaseRowNotifications = {
  read(signal: AbortSignal): Promise<DatabaseRowNotificationLevel>;
  save(level: DatabaseRowNotificationLevel): Promise<DatabaseRowNotificationLevel>;
};

/** Where a row's menu opens: at a point (its top start), under the row or
 * card ⌘/ chose, from `top` (its top) to `y` (its foot), or beside a row's
 * ⋮⋮ handle (`beside`, the handle's box as it was pressed). */
export type RowMenuAnchor = { x: number; y: number; top?: number; pressed?: boolean; beside?: PopoverRect };

type DatabaseRowMenuProps = {
  anchor: RowMenuAnchor;
  archived?: boolean;
  busy?: boolean;
  favorite: boolean;
  moveTargets: DatabaseRowMoveTarget[];
  /** Finds the workspace's other databases a row can move to. */
  searchMoveTargets?: DatabaseRowMoveTargetSearch;
  notifications?: DatabaseRowNotifications;
  readOnly?: boolean;
  rowTitle: string;
  /** Suggesting mode on the row's open page, when the page's own … opens the menu. */
  suggesting?: PageSuggestingControl;
  lock?: DatabaseRowPageLockControl;
  pageStyle?: RowPageMenuExtras["style"];
  /** Opens the row page's version history. */
  onHistory?: () => void;
  onExport?: RowPageMenuExtras["onExport"];
  onImport?: RowPageMenuExtras["onImport"];
  onCustomizeLayout?: RowPageMenuExtras["onCustomizeLayout"];
  /** The open row page, when its own … opens the menu: Notion's page menu, not a row's. */
  page?: RowPageMenuPage;
  variant?: "database-row";
  onArchive?: (archived: boolean) => Promise<void>;
  /** Opens the row page's updates and analytics. */
  onAnalytics?: () => void;
  onClose: () => void;
  onCopyLink: () => Promise<void>;
  /** "링크 복사"'s name in its place, as several rows' "모든 링크 복사". */
  copyLinkLabel?: string;
  /**
   * The row's actions within its database; a database page opened on its
   * own, without its database, offers none of them.
   */
  onDuplicate?: () => Promise<void>;
  /** A page that can be a favorite; a template's can't yet. */
  onFavorite?: (favorite: boolean) => Promise<void>;
  /** Moves the row, dropping the values of the accepted properties. */
  onMove?: (target: DatabaseRowMoveTarget, acceptDroppedPropertyIds: string[]) => Promise<void>;
  /** Lists the values a move to the target would drop. */
  onPreviewMove?: (target: DatabaseRowMoveTarget) => Promise<DatabaseRowMoveDrop[]>;
  /** The row's page and data source, which "옮기기" moves through the pages' move popover. */
  moveSubject?: DatabaseRowMoveSubject;
  onOpenMode?: (mode: DatabaseRowMenuOpenMode) => void;
  /** A computer's app's "새 창에서 열기": the row's page in a new window of its own. */
  onOpenWindow?: () => void;
  onTrash?: () => Promise<void>;
  /** What the row's menu in its view adds by the row: its icon, its values, the view's settings, a comment. */
  lines?: DatabaseRowMenuLines;
  /** The menu's foot, as Notion's: when the page was last edited, then its words and characters (a row's
   * menu in its view) or before them its body's words (an open row page's "…"). */
  foot?: RowMenuFoot;
  /** The row the apps' row lines act on. */
  appTargetId?: string;
  /** Drawn ahead and hidden (RowContextMenuSurface): it is placed, takes a layer and the focus only as it shows. */
  ahead?: boolean;
};

/** Where the menu opens under a database page's … button: 8px under it, ending
 * at its end edge (its left read from the right), as a page's bar "…" and
 * Notion's row peek (seen 2026-11-13: 1172,44 under 1402–1430 at 8–36);
 * `x` is where the menu starts (placeAtPoint). */
export function rowPageActionsAnchor(trigger: HTMLElement) {
  const rect = trigger.getBoundingClientRect();
  return { x: inlineEndX(rect, -256, trigger), y: rect.bottom + 8 };
}

/** A row's menu is at least this wide in its view (an open row page's, 256). */
const ROW_MENU_MIN_WIDTH = 265;

/** How far a row's menu ends before the ⋮⋮ handle that opened it. */
const HANDLE_MENU_GAP = 5;

/** A row's ⋮⋮ handle opens the menu beside it (besideHandle), from its box as pressed. */
export function rowHandleMenuAnchor(handle: HTMLElement): RowMenuAnchor {
  const { top, bottom, left, right } = handle.getBoundingClientRect();
  return { x: inlineStartX({ left, right }, -HANDLE_MENU_GAP - ROW_MENU_MIN_WIDTH, handle), y: top, beside: { top, bottom, left, right } };
}

/**
 * A row's menu from a table or list row's ⋮⋮ handle: on the handle's left,
 * ending 5px before it (on its right read from the right), its middle on the
 * handle's, slid into the window where it would run past it, keeping its
 * fraction, at most 70% of the window high — Notion's (seen 2026-11-16
 * twice, in 723 and 423 high windows: a handle at 433.5..451.5 × 427..451
 * put the 458.9-high menu at 163.5..428.5 × 209.6..668.5, one at 353..377 at
 * 135.6..594.5, one at 616..640 at 252.6..711.5, the window's foot 11.5
 * under it; in the 423 window one at 227..251 at 91..387, 296.1 high).
 */
function besideHandle(handle: PopoverRect, viewport: PopoverViewport): PopoverRequest {
  return { anchor: handle, placement: "left", gap: HANDLE_MENU_GAP, keepSide: true,
    options: { whole: true, cap: Math.floor(viewport.height * 0.7) } };
}

type PlaceMenu = Parameters<typeof usePopoverPlacement>[1];

/**
 * The menu's box and the backdrop under it, placed by their own state:
 * placing the menu, lifting it into its layer and its settling draw these
 * alone, not the rows it holds, which each drew again with React Aria's list
 * of them (some 250 parts after a row's menu was drawn, in the press's own
 * frame, NAS condition, CPU ×2, 2026-11-13). The search takes the keys once
 * the menu shows: a hidden menu takes no focus, which left Escape, the arrows
 * and typing with the "…" that opened it. A row's menu (`headed`) scrolls its
 * rows and foot in a box of their own under its search, which stays in the
 * menu's list for its keys but stands over that box (DatabaseRowMenu.module.css).
 */
function PlacedRowMenu({ menuRef, place, anchor, phone, headed, onClose, closeLabel, variant, label, onPointerMove, children }: {
  menuRef: RefObject<HTMLDivElement | null>;
  place: PlaceMenu;
  anchor: RowMenuAnchor;
  phone: boolean;
  headed: boolean;
  onClose: () => void;
  closeLabel: string;
  variant: "page" | "row";
  label: string;
  onPointerMove?: PointerEventHandler<HTMLDivElement>;
  children: ReactNode;
}) {
  const position = usePopoverPlacement(menuRef, place, [anchor], undefined, { kind: "menu", onEscape: onClose, onOutsidePress: onClose });
  const searchFocused = useRef(false);
  useEffect(() => {
    if (!position || searchFocused.current) return;
    searchFocused.current = true;
    menuRef.current?.querySelector<HTMLElement>("[data-action-search]")?.focus();
  }, [menuRef, position]);
  const style = (phone ? {} : {
    "--menu-x": `${anchor.x}px`,
    "--menu-y": `${anchor.y}px`,
    ...(position ? popoverPlacementStyle(position) : {}),
    visibility: position ? "visible" : "hidden",
  }) as CSSProperties;
  return (
    <>
      <button type="button" className={styles.backdrop} style={popoverBackdropStyle(position)} tabIndex={-1} aria-label={closeLabel}
        data-phone-menu={phone ? "true" : undefined} onClick={onClose} />
      <div ref={menuRef} className={`${styles.menu} ${styles.contextMenu}${headed ? ` ${rowStyles.headed}` : ""}`} style={style}
        data-row-menu-variant={variant} data-phone-menu={phone ? "true" : undefined} aria-label={label}
        onClick={(event) => event.stopPropagation()} onKeyDown={keepLayerKeys} onPointerMove={onPointerMove}>
        {headed ? <div className={rowStyles.rows}>{children}</div> : children}
      </div>
    </>
  );
}

/**
 * A row's menu, or an open row page's "…" (`page`); that one on a touch
 * screen wider than a phone's is the phone's "작업" sheet in the screen's
 * middle, as a page's (RowMenu) and Notion's iPad (seen 2026-11-13 at 834
 * and 1210, a page's and a database page's "⋯" alike; TouchSheetLayout).
 */
export function DatabaseRowMenu(props: DatabaseRowMenuProps) {
  return props.page ? <TouchSheetLayout><RowOrPageMenu {...props} /></TouchSheetLayout> : <RowOrPageMenu {...props} />;
}

function RowOrPageMenu({
  anchor,
  archived = false,
  busy = false,
  favorite,
  moveTargets,
  searchMoveTargets,
  notifications,
  readOnly = false,
  rowTitle,
  suggesting,
  lock,
  pageStyle: givenPageStyle,
  onHistory,
  onExport,
  onImport,
  onCustomizeLayout,
  page,
  onArchive,
  onAnalytics,
  onClose,
  onCopyLink,
  copyLinkLabel,
  onDuplicate,
  onFavorite,
  onMove,
  onPreviewMove,
  moveSubject,
  onOpenMode,
  onOpenWindow,
  onTrash,
  lines,
  foot,
  appTargetId,
  ahead = false,
}: DatabaseRowMenuProps) {
  const { t } = useTranslation(["rowMenu", "topBar", "databaseView"]);
  const menuRef = useRef<HTMLDivElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);
  const [actionQuery, setActionQuery] = useState("");
  // Opened at the pointer, it stays on screen as it grows or its search
  // narrows it; a phone's sits at the screen's bottom, full width, as
  // Notion's sheets.
  const { phone } = useSidebarLayout(true);
  const headed = !page && !phone;
  // "옮기기" puts the move popover where the menu was, in its place.
  const [moveAt, setMoveAt] = useState<MovePopoverAnchor | null | undefined>(undefined);
  const onOpenMove = moveSubject && onMove ? () => setMoveAt(databaseRowMoveAnchor(menuRef.current)) : undefined;
  // A row's menu is at most 70% of the window high, its rows and foot
  // scrolling under its search, their scrollbar beside them alone — Notion's
  // (seen 2026-11-16: a 48-high head over a scroller the menu's width, from
  // the head's foot to the menu's; DatabaseRowMenu.module.css). Shown
  // whole, it opens under its point, or under the row or card ⌘/ chose, over
  // it where only that side holds it, and with neither, under it moved up
  // into the window over it — Notion's (seen 2026-11-16 at 1440 wide in 523,
  // 723 and 873 high windows, 366, 506 and 611 caps: ⌘/ over a table row at
  // 363..400 at 205..711, at 600..637 at 94..600, at 517..554 at 205..711, at
  // 519..556 at 13..519; a right press at 260 at 252..711; the selection
  // bar's "⋯" pressed at 648 at 189..648). An open row page's "…" is a
  // page's, its search scrolling with it, at most 80% of the window high (pageMenuCap). From a row's ⋮⋮ it opens beside
  // the handle, its middle on the handle's (besideHandle).
  const beside = anchor.beside;
  const place: PlaceMenu = ahead || phone || moveAt !== undefined ? null : page
    ? (size, viewport) => placeAtPoint(anchor, { width: Math.max(size.width, 256), height: Math.max(size.height, 180) }, viewport,
      { cap: pageMenuCap(viewport) })
    : beside ? (_size, viewport) => besideHandle(beside, viewport)
      : anchor.pressed ? (size, viewport) => placeAtPoint(anchor, size, viewport, { cap: Math.floor(viewport.height * 0.7) })
      : (size, viewport) => placeAround(popoverRectAt({ top: anchor.top ?? anchor.y, bottom: anchor.y, left: anchor.x }), size, viewport,
        { gap: 0, whole: true, cap: Math.floor(viewport.height * 0.7) });
  // The style as changed while the menu is open, over the page's own as the
  // menu shows it (one drawn ahead is given the page's as it shows).
  const [styleChange, setStyleChange] = useState<PageStyleChange>({});
  const pageStyle = givenPageStyle ? {
    value: { ...givenPageStyle.value, ...styleChange },
    onChange(change: PageStyleChange) {
      setStyleChange((current) => ({ ...current, ...change }));
      givenPageStyle.onChange(change);
    },
  } : givenPageStyle;
  const [notifyLevel, setNotifyLevel] = useState<DatabaseRowNotificationLevel | null>(null);
  const [notifyBusy, setNotifyBusy] = useState(false);
  const { notify } = useProductToast();
  // A row comes named by its kind (useRowDisplayTitle); only a template's or a
  // lone row page's own title, a page's, comes empty.
  const visibleTitle = rowTitle || untitledPageDisplayTitle("page");
  const normalizedQuery = actionQuery.trim().toLocaleLowerCase();

  const [notifyReadFailed, setNotifyReadFailed] = useState(false);
  const [notifyAttempt, setNotifyAttempt] = useState(0);
  useEffect(() => {
    if (!notifications) return;
    const controller = new AbortController();
    setNotifyReadFailed(false);
    void notifications.read(controller.signal).then((level) => {
      if (!controller.signal.aborted) setNotifyLevel(level);
    }).catch(() => {
      // The row says it couldn't load, and reads again when chosen.
      if (!controller.signal.aborted) setNotifyReadFailed(true);
    });
    return () => controller.abort();
  }, [notifications, notifyAttempt]);

  async function changeNotifyLevel(level: DatabaseRowNotificationLevel) {
    if (!notifications || level === notifyLevel || notifyBusy) return;
    setNotifyBusy(true);
    try {
      setNotifyLevel(await notifications.save(level));
    } catch {
      notify(t("toast.couldntUpdateNotifications"), "error");
    } finally {
      setNotifyBusy(false);
    }
  }


  const rowMenu = useDatabaseRowViewMenu({
    query: actionQuery, busy, readOnly, archived, favorite, lines: page ? undefined : lines, lock, suggesting,
    moveTargets, searchMoveTargets, onFavorite, onOpenMode, onOpenWindow, onCopyLink, copyLinkLabel, onDuplicate, onMove, onPreviewMove,
    onOpenMove, onImport, onExport, onHistory, onArchive, onTrash, onClose,
  });
  const pageMenu = useDatabaseRowPageMenu({
    page, query: actionQuery, busy, readOnly, archived, pageStyle, lock, suggesting,
    notifyLevel, notifyBusy, onNotifyLevel: notifications ? (level) => void changeNotifyLevel(level) : undefined,
    onNotifyRetry: notifyReadFailed ? () => setNotifyAttempt((current) => current + 1) : undefined,
    moveTargets, searchMoveTargets, onMove, onPreviewMove, onOpenMove, onCopyLink, onDuplicate, onArchive, onTrash,
    onImport, onExport, onAnalytics, onHistory, onCustomizeLayout, onClose,
  });

  if (moveAt !== undefined && moveSubject) {
    return <DatabaseRowMovePopover subject={moveSubject} anchor={moveAt} onClose={onClose} />;
  }

  const footDrawn = (marker: string) => foot ? <RowMenuEditedFoot name={foot.name ?? ""} editorId={foot.editorId} via={foot.via} at={foot.at} counts={foot.counts}
    words={foot.words} activity={foot.activity} marker={marker} /> : undefined;
  // A row's menu on a phone, its long press's, is Notion's "작업" sheet for a row (seen 2026-11-25 on the A90; DatabaseRowPhoneSheet).
  if (phone && !page) return (
    <DatabaseRowPhoneSheet onClose={onClose} actions={{
      busy, readOnly, lines, copyLinkLabel, onCopyLink: () => void onCopyLink(), foot: footDrawn("row-menu-edited"),
      favorite: onFavorite ? { on: favorite, onToggle: () => void onFavorite(!favorite) } : undefined,
      onDuplicate: onDuplicate ? () => void onDuplicate() : undefined,
      move: onMove ? onOpenMove ? { onPress: onOpenMove } : { sheet: (close) => <DatabaseRowMovePhoneList busy={busy} moveTargets={moveTargets}
        searchMoveTargets={searchMoveTargets} onMove={onMove} onPreviewMove={onPreviewMove} onDone={() => { close(); onClose(); }} /> } : undefined,
      archive: onArchive ? { label: t(archived ? "rowMenu:actions.unarchive" : "rowMenu:actions.archive"), onToggle: () => void onArchive(!archived) } : undefined,
      onTrash: onTrash ? () => void onTrash() : undefined,
    }} />
  );

  const surface = (
    <>
      <PlacedRowMenu menuRef={menuRef} place={place} anchor={anchor} phone={phone} headed={headed} onClose={onClose}
        closeLabel={t("aria.closeDatabaseRowActions")}
        variant={page ? "page" : "row"} label={t("aria.databaseRowActionsFor", { title: visibleTitle })} onPointerMove={page ? pageMenu.onPointerMove : rowMenu.onPointerMove}>
        <NativeActionMenu aria-label={t("aria.databaseRowActionsFor", { title: visibleTitle })}
          renderEmptyState={() => <p className={styles.menuEmpty}>{t("noResults")}</p>}>
        <NativeMenuSection aria-label={t("searchActions")} className={headed ? rowStyles.searchHead : undefined}><NativeMenuHeader>
        {/* An open page's search is a page menu's — its magnifier and "입력 값 지우기" — a row's has neither, as Notion's. */}
        {page ? (
          <PageActionSearch className={styles.actionSearch} label={t("searchPageActions")} value={actionQuery} inputRef={searchRef}
            marker="action-search" onChange={setActionQuery} />
        ) : <input
          className={headed ? `${styles.actionSearch} ${rowStyles.headSearch}` : styles.actionSearch}
          data-action-search data-native-menu-search
          type="search"
          aria-label={t("searchActions")}
          placeholder={t("searchActions")}
          value={actionQuery}
          onChange={(event) => setActionQuery(event.target.value)}
        />}
        </NativeMenuHeader></NativeMenuSection>
        {page ? <>
          {pageMenu.items}
          {appTargetId ? (
            <AppMenuLines menu="row" targetId={appTargetId} query={actionQuery} className={styles.item} dividerClassName={styles.divider}
              hintClassName={styles.itemHint} onDone={onClose} />
          ) : null}
        </> : <>
        {pageStyle && !normalizedQuery ? (
          <><PageStyleControls style={pageStyle.value} disabled={readOnly || busy} onChange={pageStyle.onChange} /><NativeMenuSeparator className={styles.divider} /></>
        ) : null}
        <NativeMenuSection aria-label={t("sections.page")}><NativeMenuHeader>
        <div className={styles.sectionLabel}>{t("sections.page")}</div>
        </NativeMenuHeader>
        {rowMenu.items}
        {appTargetId ? (
          <AppMenuLines menu="row" targetId={appTargetId} query={actionQuery} className={styles.item} dividerClassName={styles.divider}
            hintClassName={styles.itemHint} onDone={onClose} />
        ) : null}
        </NativeMenuSection>
        </>}
        </NativeActionMenu>
        {!normalizedQuery && (!page || pageMenuShowsEditedFoot(!readOnly)) ? footDrawn(page ? "row-page-menu-edited" : "row-menu-edited") : null}
      </PlacedRowMenu>
      {pageMenu.overlay}
      {rowMenu.overlay}
    </>
  );

  // An open database page's "…" on a phone is Notion's "작업" sheet, as a page's (seen 2026-10-27).
  if (phone && page && pageMenu.phone) return (
    <RowMenuPhoneSheet onClose={onClose} actions={{
      ...pageMenu.phone,
      favorite: onFavorite ? { on: favorite, onToggle: () => void onFavorite(!favorite) } : undefined,
      onShare: () => { onClose(); requestRowPageShare(page.id); },
      foot: pageMenuShowsEditedFoot(!readOnly) ? footDrawn("row-page-menu-edited") : undefined,
    }} />
  );
  return typeof document === "undefined" ? surface : createPortal(surface, document.body);
}
