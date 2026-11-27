import { useEffect, useLayoutEffect, useRef, useState, type MouseEvent, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { NativeTab, useNativeTabs } from "@/lib/nativeTabs";
import { ChevronDown, ClockIcon, DotsHorizontal, DragHandleIcon, EyeSlashIcon, FileText, LockIcon, MenuIcon, SharePeopleIcon, SlidersIcon, Star, TableIcon, TeamspaceIcon } from "@/icons/leafen";
import { placeAround, popoverAnchorOf, popoverPlacementStyle, usePopoverPlacement, type PopoverAnchor } from "@/lib/popoverPlacement";
import type { LibraryViewId } from "@/lib/router";
import { useDragReorder } from "@/lib/useDragReorder";
import { PhoneViewListRow } from "@/components/database/DatabaseViewPhoneSheets";
import { PhoneSheet, PhoneSheetGroup, PhoneSheetRow } from "@/components/shell/PhoneSheet";
import styles from "./LibraryViewTabs.module.css";
import { keysFocusHolder } from "@/lib/lastPress";

// A view's row the keys' focus is in takes its ground (data-keys-within).
keysFocusHolder(styles.viewRow);

let focusCarried = false;
// The tabs' widths and room as the library last measured them: a view opened
// afresh draws the tabs that fit at once, not a first guess drawn again after
// its measure (each view is its own route, drawn anew).
let lastMeasure: { labelKey: string; layout: { widths: number[]; room: number } } | null = null;

/** The tabs shown before the row is measured, as Notion's narrow window shows them. */
const SHOWN_TABS = 2;
/** Room kept for "N개 더 보기" and the gaps between tabs. */
const MORE_ROOM = 88;
const TAB_GAP = 1;
/**
 * Below this the row keeps only its settings, as Notion's (seen 2026-10-07: a
 * 447px row did, a 472px one didn't), measured as the row stands between the
 * page's sides; so narrow, it runs on to 16px from the page's edge, its
 * settings 24px in (seen 2026-11-14 beside a side peek: 681 in a page ending
 * at 705 at 1440, 521 in one ending at 545 at 1280; 96px sides at 739 and
 * 753). Its own margin at either end.
 */
const NARROW_ROW = 460;
const ROW_INSET = 8;

/** A view's mark on its tab and in the list of views, as Notion's (seen 2026-10-07). */
function LibraryViewIcon({ view }: { view: LibraryViewId }) {
  if (view === "teamspaces") return <TeamspaceIcon size="regular" aria-hidden="true" />;
  if (view === "recents") return <ClockIcon size="regular" aria-hidden="true" />;
  if (view === "favorites") return <Star size="regular" aria-hidden="true" />;
  if (view === "shared") return <SharePeopleIcon size="regular" aria-hidden="true" />;
  if (view === "documents") return <FileText size="regular" aria-hidden="true" />;
  return <LockIcon size="regular" aria-hidden="true" />;
}

/** What a view's handle and "…" in the list of views do. */
export type LibraryViewListActions = {
  /** Moves a view before or after another. */
  onMove: (view: LibraryViewId, target: LibraryViewId, after: boolean) => void;
  /** Opens a view with its settings; the teamspaces have none. */
  onEdit: (view: LibraryViewId) => void;
  /** Whether the view's section shows in the sidebar. */
  sidebarShown: (view: LibraryViewId) => boolean;
  onSidebar: (view: LibraryViewId, shown: boolean) => void;
};

/**
 * The library's views as Notion's (seen 2026-10-07): a tab each, its mark
 * before its name, the open one a gray pill — as many as the row has room
 * for beside its buttons, then "N개 더 보기". With room for fewer than two
 * the row is one pill, the open view's name with ⌄. Both open the list of
 * every view (290px, at the button's right edge) under a search field, each
 * with its handle to move it and "…" with "보기 편집" and the sidebar's
 * section shown or hidden. The view open from the list shows as a tab too.
 * A phone's row is always that pill and keeps its tools, as Notion's phone
 * (seen 2026-11-12 in iPhone Safari: "▦ 즐겨찾기 ⌄" then 🔍, 필터 and 설정),
 * where a narrow desktop window keeps only "설정"; its pill opens the views
 * as a sheet (LibraryViewSheet).
 */
export function LibraryViewTabs({
  views,
  current,
  onSelect,
  actions,
  list,
  onNarrow,
}: {
  views: readonly LibraryViewId[];
  current: LibraryViewId;
  onSelect: (view: LibraryViewId) => void;
  /** The row's right: the view's search and settings. */
  actions?: ReactNode;
  list?: LibraryViewListActions;
  /** Whether the row is too narrow for its filter and search, as it changes; a phone's never is. */
  onNarrow?: (narrow: boolean) => void;
}) {
  const { t } = useTranslation("library");
  const { phone } = useSidebarLayout(true);
  // The list of views opens under the button that opened it.
  const [anchor, setAnchor] = useState<PopoverAnchor | null>(null);
  const open = anchor !== null;
  const narrowChanged = useRef(onNarrow);
  narrowChanged.current = onNarrow;
  const [narrow, setNarrow] = useState(false);
  // The tabs' widths and the room beside the row's buttons, as last measured.
  const rowRef = useRef<HTMLDivElement>(null);
  const measureRef = useRef<HTMLDivElement>(null);
  const labelKey = views.map((view) => t(`views.${view}`)).join("\u0000");
  const [layout, setLayout] = useState<{ widths: number[]; room: number } | null>(() => lastMeasure?.labelKey === labelKey ? lastMeasure.layout : null);

  // The tabs that fit beside the row's buttons, measured from hidden copies
  // whenever the row or its buttons (the search opening) change width.
  useLayoutEffect(() => {
    const row = rowRef.current;
    const measure = measureRef.current;
    if (!row || !measure || typeof ResizeObserver === "undefined") return undefined;
    const actions = row.querySelector<HTMLElement>("[data-library-tab-actions]");
    const update = () => {
      // The page's width between its sides, which the narrow row's own reach
      // past its side leaves as it is.
      const page = row.parentElement;
      const sides = page ? getComputedStyle(page) : null;
      const between = page && sides
        ? page.clientWidth - parseFloat(sides.paddingInlineStart) - parseFloat(sides.paddingInlineEnd) - 2 * ROW_INSET
        : row.clientWidth;
      setNarrow(between < NARROW_ROW);
      const widths = Array.from(measure.children, (child) => (child as HTMLElement).offsetWidth + TAB_GAP);
      const room = row.clientWidth - (actions?.offsetWidth ?? 0);
      setLayout((previous) => {
        const next = previous && previous.room === room && previous.widths.join() === widths.join() ? previous : { widths, room };
        lastMeasure = { labelKey, layout: next };
        return next;
      });
    };
    update();
    const observer = new ResizeObserver(update);
    observer.observe(row);
    if (actions) observer.observe(actions);
    return () => observer.disconnect();
  }, [labelKey]);

  // Told before the row is painted, so its tools never show a frame too many.
  const narrowRow = narrow && !phone;
  useLayoutEffect(() => {
    narrowChanged.current?.(narrowRow);
  }, [narrowRow]);

  // The tabs that fit, the open view always among them: its room is kept
  // first, so it never runs under "N개 더 보기" (2026-10-16: an open view past
  // the fitting ones overlapped the button by 56px).
  const tabs = ((): LibraryViewId[] => {
    if (!layout) {
      const first = views.slice(0, SHOWN_TABS);
      return first.includes(current) ? first : [...first, current];
    }
    const { widths, room } = layout;
    if (widths.reduce((sum, width) => sum + width, 0) <= room) return [...views];
    const at = views.indexOf(current);
    let used = widths[at] ?? 0;
    const shown = new Set<LibraryViewId>([current]);
    for (const [index, view] of views.entries()) {
      if (index === at) continue;
      if (used + widths[index] > room - MORE_ROOM) break;
      used += widths[index];
      shown.add(view);
    }
    return views.filter((view) => shown.has(view));
  })();
  const compact = phone || tabs.length < Math.min(2, views.length);
  const hidden = views.length - tabs.length;
  const tabListRef = useRef<HTMLDivElement>(null);
  const tabList = useNativeTabs<LibraryViewId>({
    tabs: tabs.map((view) => ({ key: view, text: t(`views.${view}`) })), selected: current, onSelect,
    label: t("viewsLabel"), listRef: tabListRef,
  });
  // Each view is its own route, drawn afresh; a tab moved to from the keys
  // keeps the focus across that.
  useLayoutEffect(() => {
    if (focusCarried) tabListRef.current?.querySelector<HTMLElement>('[aria-selected="true"]')?.focus({ preventScroll: true });
    focusCarried = false;
    return () => {
      focusCarried = Boolean(tabListRef.current?.contains(document.activeElement));
    };
  }, []);

  const toggle = (event: MouseEvent<HTMLButtonElement>) => {
    const button = event.currentTarget;
    setAnchor((value) => (value ? null : popoverAnchorOf(button)));
  };
  const choose = (view: LibraryViewId) => { setAnchor(null); onSelect(view); };
  const menu = !anchor ? null : phone ? (
    <LibraryViewSheet views={views} current={current} list={list} onClose={() => setAnchor(null)} onChoose={choose} />
  ) : (
    <LibraryViewList views={views} current={current} list={list} anchor={anchor} onClose={() => setAnchor(null)} onChoose={choose} />
  );

  return (
    <div ref={rowRef} className={styles.tabRow} data-compact={compact ? "true" : undefined} data-narrow={narrowRow ? "true" : undefined}>
      <div ref={measureRef} className={styles.tabMeasure} aria-hidden="true">
        {views.map((view) => (
          <span key={view} className={styles.tab}><LibraryViewIcon view={view} />{t(`views.${view}`)}</span>
        ))}
      </div>
      {compact ? (
        <div className={styles.moreViews}>
          <button type="button" className={styles.viewPill} data-library-view-pill={current} aria-haspopup="dialog" aria-expanded={open}
            onClick={toggle}>
            {/* Notion's pill marks a list of pages as a table and the teamspaces as theirs. */}
            {current === "teamspaces" ? <TeamspaceIcon size="regular" aria-hidden="true" /> : <TableIcon size="regular" aria-hidden="true" />}
            <span>{t(`views.${current}`)}</span>
            <ChevronDown size="small" aria-hidden="true" />
          </button>
          {menu}
        </div>
      ) : (
        <>
          <div {...tabList.tabListProps} ref={tabListRef} className={styles.tabs}>
            {tabs.map((view) => (
              <NativeTab key={view} state={tabList.state} tabKey={view} className={styles.tab} data-library-view={view}>
                <LibraryViewIcon view={view} />
                {t(`views.${view}`)}
              </NativeTab>
            ))}
          </div>
          {hidden > 0 ? (
            <div className={styles.moreViews}>
              <button type="button" className={styles.moreViewsButton} data-library-more-views aria-haspopup="dialog" aria-expanded={open}
                onClick={toggle}>
                {t("moreViews", { count: hidden })}
              </button>
              {menu}
            </div>
          ) : null}
        </>
      )}
      {actions}
    </div>
  );
}

/**
 * The list of every view under its search field, as "N개 더 보기" and the
 * pill open it, over the window (the sidebar too) at the button's right edge.
 */
function LibraryViewList({ views, current, list, anchor, onChoose, onClose }: {
  views: readonly LibraryViewId[];
  current: LibraryViewId;
  list?: LibraryViewListActions;
  anchor: PopoverAnchor;
  onChoose: (view: LibraryViewId) => void;
  onClose: () => void;
}) {
  const { t } = useTranslation("library");
  const [query, setQuery] = useState("");
  // A view's "…" opens over the list, so Escape and a press outside close it first.
  const [options, setOptions] = useState<{ view: LibraryViewId; anchor: PopoverAnchor } | null>(null);
  const search = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const place = usePopoverPlacement(listRef, (size, viewport) => placeAround(anchor, size, viewport, { gap: 4, align: "end" }),
    [anchor], undefined, { kind: "menu", inside: () => [anchor.contextElement], onEscape: onClose, onOutsidePress: onClose });
  const listed = views.filter((view) => t(`views.${view}`).toLowerCase().includes(query.trim().toLowerCase()));
  const reorder = useDragReorder("library-view", (dragged, target, side) => {
    list?.onMove(dragged as LibraryViewId, target as LibraryViewId, side === "after");
  }, { live: true });

  useEffect(() => search.current?.focus({ preventScroll: true }), []);

  return createPortal(
    <div ref={listRef} className={styles.viewList} role="dialog" aria-label={t("viewsLabel")}
      style={{ position: "fixed", ...popoverPlacementStyle(place) }}>
      <label className={styles.viewSearch}>
        <input ref={search} value={query} placeholder={t("searchViews")} aria-label={t("searchViews")}
          onChange={(event) => setQuery(event.target.value)} />
      </label>
      <div className={styles.viewRows}>
        {listed.map((view, index) => {
          const name = t(`views.${view}`);
          return (
            <div key={view} className={styles.viewRow} data-library-view-row={view} data-current={view === current ? "true" : undefined}
              {...(list ? reorder.row(view) : {})}>
              {list ? (
                <button type="button" className={styles.viewHandle} aria-label={t("moveView", { name })}
                  {...reorder.handle(view, true, { above: listed[index - 1], below: listed[index + 1] })}>
                  <DragHandleIcon size="small" aria-hidden="true" />
                </button>
              ) : null}
              <button type="button" className={styles.viewOption} aria-current={view === current ? "true" : undefined}
                data-library-view-option={view} onClick={() => onChoose(view)}>
                <LibraryViewIcon view={view} />
                <span className={styles.viewName}>{name}</span>
              </button>
              {/* The documents have neither settings nor a sidebar section. */}
              {list && view !== "documents" ? (
                <button type="button" className={styles.viewMore} aria-label={t("viewOptions", { name })} aria-haspopup="menu"
                  aria-expanded={options?.view === view} data-library-view-more={view}
                  onClick={(event) => {
                    const button = event.currentTarget;
                    setOptions((value) => (value?.view === view ? null : { view, anchor: popoverAnchorOf(button) }));
                  }}>
                  <DotsHorizontal size="small" aria-hidden="true" />
                </button>
              ) : null}
            </div>
          );
        })}
      </div>
      {list && options ? (
        <LibraryViewOptions view={options.view} anchor={options.anchor} list={list} onClose={() => setOptions(null)}
          onDone={() => { setOptions(null); onClose(); }} />
      ) : null}
    </div>,
    document.body,
  );
}

/**
 * The views on a phone, as Notion's sheet (seen 2026-11-14 in iPhone Safari
 * at 402): "N개 보기" with "완료", a row per view — its mark, its name, the
 * open one ticked, and "⋯" — with neither search nor handles, the database's
 * own rows (PhoneViewListRow). A view's "⋯" opens "보기 옵션" over it:
 * "보기 편집" (not for the teamspaces) and the sidebar's section; either
 * closes both.
 */
function LibraryViewSheet({ views, current, list, onChoose, onClose }: {
  views: readonly LibraryViewId[];
  current: LibraryViewId;
  list?: LibraryViewListActions;
  onChoose: (view: LibraryViewId) => void;
  onClose: () => void;
}) {
  const { t } = useTranslation("library");
  const [options, setOptions] = useState<LibraryViewId | null>(null);
  const shown = options && list ? list.sidebarShown(options) : false;
  const done = (act: () => void) => { setOptions(null); onClose(); act(); };
  return (
    <PhoneSheet title={t("databaseView:viewsCount", { count: views.length })} onClose={onClose}
      action={{ label: t("common:actions.done"), onPress: onClose }} data={{ "data-library-view-sheet": "" }}>
      <PhoneSheetGroup>
        {views.map((view) => {
          const name = t(`views.${view}`);
          return (
            <PhoneViewListRow key={view} icon={<LibraryViewIcon view={view} />} name={name} current={view === current}
              onPick={() => onChoose(view)} data={{ "data-library-view-row": view }}
              // The documents have neither settings nor a sidebar section.
              more={list && view !== "documents" ? { label: t("viewOptions", { name }), onPress: () => setOptions(view) } : undefined} />
          );
        })}
      </PhoneSheetGroup>
      {list && options ? (
        <PhoneSheet title={t("databaseView:viewOptions")} onClose={() => setOptions(null)} data={{ "data-library-view-options": options }}>
          <PhoneSheetGroup>
            {options === "teamspaces" ? null : (
              <PhoneSheetRow icon={<SlidersIcon size="regular" />} label={t("editView")} more={false}
                onPress={() => done(() => list.onEdit(options))} data={{ "data-library-view-edit": options }} />
            )}
            <PhoneSheetRow icon={shown ? <EyeSlashIcon size="regular" weight="regular" /> : <MenuIcon size="regular" />}
              label={t(shown ? "hideSidebarSection" : "showSidebarSection")} more={false}
              onPress={() => done(() => list.onSidebar(options, !shown))} data={{ "data-library-view-sidebar": shown ? "hide" : "show" }} />
          </PhoneSheetGroup>
        </PhoneSheet>
      ) : null}
    </PhoneSheet>
  );
}

/**
 * A view's "…", as Notion's (seen 2026-10-07): a 220px menu under the button
 * with "보기 편집" (not for the teamspaces, which have no settings) and its
 * sidebar section hidden or shown; either closes the list too.
 */
function LibraryViewOptions({ view, anchor, list, onClose, onDone }: {
  view: LibraryViewId;
  anchor: PopoverAnchor;
  list: LibraryViewListActions;
  onClose: () => void;
  onDone: () => void;
}) {
  const { t } = useTranslation("library");
  const ref = useRef<HTMLDivElement>(null);
  const place = usePopoverPlacement(ref, (size, viewport) => placeAround(anchor, size, viewport, { gap: 0 }), [anchor], undefined,
    { kind: "menu", inside: () => [anchor.contextElement], onEscape: onClose, onOutsidePress: onClose });
  const shown = list.sidebarShown(view);
  return createPortal(
    <div ref={ref} className={styles.viewOptions} role="menu" aria-label={t("viewOptions", { name: t(`views.${view}`) })}
      style={{ position: "fixed", ...popoverPlacementStyle(place) }}>
      {view === "teamspaces" ? null : (
        <button type="button" role="menuitem" className={styles.viewOptionsItem} data-library-view-edit={view}
          onClick={() => { onDone(); list.onEdit(view); }}>
          <SlidersIcon size="regular" aria-hidden="true" />
          <span>{t("editView")}</span>
        </button>
      )}
      <button type="button" role="menuitem" className={styles.viewOptionsItem} data-library-view-sidebar={shown ? "hide" : "show"}
        onClick={() => { onDone(); list.onSidebar(view, !shown); }}>
        {shown ? <EyeSlashIcon size="regular" weight="regular" aria-hidden="true" /> : <MenuIcon size="regular" aria-hidden="true" />}
        <span>{t(shown ? "hideSidebarSection" : "showSidebarSection")}</span>
      </button>
    </div>,
    document.body,
  );
}
