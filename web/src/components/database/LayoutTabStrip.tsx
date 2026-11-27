import { useLayoutEffect, useRef, useState, type HTMLAttributes, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { CheckIcon, ChevronDown, DotsHorizontal, DragHandleIcon, Plus } from "@/icons/leafen";
import { NativeMenuItem, useNativeMenu } from "@/lib/nativeMenu";
import { placeAround, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import { textWidth, useTextWidthsVersion } from "@/lib/textWidth";
import { useDragReorder } from "@/lib/useDragReorder";
import { DatabaseViewActionChoices, DatabaseViewActionsBeside, type DatabaseViewAction } from "./DatabaseTabMenus";
import { databaseViewLabels } from "./databaseViewLabels";
import { visibleDatabaseViewTabs } from "./databaseViewModel";
import sheet from "./databaseSheet";
import listStyles from "./DatabaseViewList.module.css";

/** A page layout's tab as its row draws it: the name it shows and its icon. */
export type LayoutStripTab = { id: string; name: string; icon: ReactNode };

const ADD = "__add-tab";

/**
 * A page layout's row of tabs, on a row page and in its layout builder, as
 * Notion's (seen 2026-11-24 at 1440, a row page twice in two tabs and its
 * builder twice): one line, the tabs that fit in the room before the tools
 * — or the whole line where none stand beside them — with the shown tab
 * among them, the rest under "N개 더 보기", which lists every tab, the shown
 * one ticked, under a search; the room is reckoned as a views' row's
 * (visibleDatabaseViewTabs: 80 kept past the tabs, each tab its name at
 * 14px/500 and 50 more, 1 apart). The builder's tabs stand 5 apart and its
 * "+" goes into the list as "새 보기" while some are folded. Notion's builder
 * reckons its tabs against its whole row and cuts its "N개 더 보기" under its
 * filter (seen twice); here the tools' room is never the tabs'. The
 * builder's list is its views' list (DatabaseViewList): each row with its
 * handle, which moves the tab — the content tab too — and its "⋯" (seen
 * 2026-11-24 in two tabs at 640 and 1440); a page's has neither.
 */
export function LayoutTabStrip({ tabs, activeId, renderTab, gap = 1, maxTab, after, onPick, onAdd, onMove, rowActions, listOpen, onListOpen, className, attributes }: {
  tabs: LayoutStripTab[];
  activeId: string;
  renderTab(tab: LayoutStripTab): ReactNode;
  /** The room between two tabs. */
  gap?: number;
  /** The widest a tab is drawn. */
  maxTab?: number;
  /** What follows the tabs while every one shows: the builder's "+". */
  after?: ReactNode;
  /** A tab chosen from the list. */
  onPick(id: string): void;
  /** The list's "새 보기", given "N개 더 보기" to open its menu by. */
  onAdd?(anchor: HTMLButtonElement): void;
  /** The builder's: a row's handle drops its tab before or after another's. */
  onMove?(source: string, target: string, side: "before" | "after"): void;
  /** The builder's: a row's "⋯" — its actions beside it, or a press of its own; `close` puts the list away. */
  rowActions?(id: string, close: () => void): DatabaseViewAction[] | (() => void);
  /**
   * Whether "N개 더 보기" is open, kept by the row's owner: the builder draws
   * its row anew as another tab shows, and a tab deleted from the list leaves
   * it open (Notion's, seen 2026-11-24).
   */
  listOpen?: boolean;
  onListOpen?(open: boolean): void;
  className: string;
  attributes?: HTMLAttributes<HTMLDivElement> & Record<`data-${string}`, string | boolean | undefined>;
}) {
  const { t } = useTranslation("databaseView"), labels = databaseViewLabels();
  const ref = useRef<HTMLDivElement>(null), trigger = useRef<HTMLButtonElement>(null), menuRef = useRef<HTMLDivElement>(null);
  const search = useRef<HTMLInputElement>(null), itemsRef = useRef<HTMLDivElement>(null);
  const [room, setRoom] = useState(Number.POSITIVE_INFINITY);
  const [moreWidth, setMoreWidth] = useState(0);
  const [family, setFamily] = useState<string | null>(null);
  const [ownOpen, setOwnOpen] = useState(false);
  const open = listOpen ?? ownOpen, setOpen = (next: boolean) => { setOwnOpen(next); onListOpen?.(next); };
  const [query, setQuery] = useState("");
  const [beside, setBeside] = useState<{ id: string; anchor: HTMLElement; actions: DatabaseViewAction[] } | null>(null);
  // The list put away puts away what its "⋯" opened beside it.
  const close = () => { setBeside(null); setOpen(false); };
  const reorder = useDragReorder("layout-tab-list", (source, target, side) => onMove?.(source, target, side), { live: true });
  useTextWidthsVersion();
  // The room runs from the row's start to the tools' first button, a hidden
  // "»" not counted (its showing moves no tab), or to the row's end with no tools.
  const measure = () => {
    const strip = ref.current;
    if (!strip) return;
    const style = getComputedStyle(strip), rtl = style.direction === "rtl", sign = rtl ? -1 : 1;
    const edge = (rect: DOMRect, side: "start" | "end") => (side === "start") !== rtl ? rect.left : rect.right;
    const own = strip.getBoundingClientRect();
    const start = edge(own, "start") + sign * (parseFloat(style.paddingInlineStart) || 0);
    const tools = strip.parentElement?.querySelector<HTMLElement>(":scope > [data-database-toolbar]");
    const first = tools?.firstElementChild;
    const fold = tools?.querySelector<HTMLElement>("[data-toolbar-fold]")?.getBoundingClientRect().width ?? 0;
    const end = first ? edge(first.getBoundingClientRect(), "start") + sign * fold : edge(own, "end") - sign * (parseFloat(style.paddingInlineEnd) || 0);
    const next = Math.max(0, (end - start) * sign);
    setRoom((current) => Math.abs(current - next) > 0.5 ? next : current);
    setFamily(style.fontFamily);
    const more = trigger.current?.getBoundingClientRect().width ?? 0;
    if (more > 0) setMoreWidth((current) => Math.abs(current - more) > 0.5 ? more : current);
  };
  useLayoutEffect(() => {
    const strip = ref.current;
    measure();
    if (!strip || typeof ResizeObserver === "undefined") return undefined;
    const watch = new ResizeObserver(() => measure());
    watch.observe(strip);
    if (strip.parentElement) watch.observe(strip.parentElement);
    const tools = strip.parentElement?.querySelector<HTMLElement>(":scope > [data-database-toolbar]");
    if (tools) watch.observe(tools);
    return () => watch.disconnect();
  // Measured as it comes and as it, its row or its tools resize.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  // "N개 더 보기" measured once drawn; its room past 80 is its own.
  useLayoutEffect(() => {
    const more = trigger.current?.getBoundingClientRect().width ?? 0;
    if (more > 0 && Math.abs(more - moreWidth) > 0.5) setMoreWidth(more);
  });
  const widthOf = (tab: LayoutStripTab) => {
    const text = family ? textWidth(tab.name, "500 14px", family) : null;
    return text === null ? undefined : Math.min(maxTab ?? Number.POSITIVE_INFINITY, 50 + text) + gap - 1;
  };
  const views = tabs.map((tab) => ({ id: tab.id, name: tab.name, type: "table" }));
  const projected = Number.isFinite(room)
    ? visibleDatabaseViewTabs(views, activeId, room, Math.max(0, moreWidth + 1 - 80), "both", (view) => widthOf(tabs.find((tab) => tab.id === view.id)!))
    : { visible: tabs.map(({ id }) => id), overflow: [], collapsed: false };
  const shown = tabs.filter((tab) => projected.visible.includes(tab.id));
  // Kept open while no tab is folded any more, it closes.
  const folds = projected.overflow.length > 0 || projected.collapsed;
  useLayoutEffect(() => { if (open && Number.isFinite(room) && !folds) close(); });
  const active = tabs.find((tab) => tab.id === activeId) ?? tabs[0];
  const place = usePopoverPlacement(menuRef, open ? (size, viewport) => placeAround(
    popoverRectOf(trigger.current) ?? { top: 0, bottom: 0, left: 0, right: 0 }, size, viewport, { gap: 4 },
  ) : null, [open], undefined, { kind: "menu", onEscape: () => close(), onOutsidePress: () => close(),
    inside: () => [trigger.current] });
  const found = tabs.filter((tab) => tab.name.toLocaleLowerCase().includes(query.trim().toLocaleLowerCase()));
  // A row's "⋯" opens its actions beside it, its row lit while they show, or does what the row's own press is.
  const more = (tab: LayoutStripTab, button: HTMLElement) => {
    const asked = rowActions?.(tab.id, close);
    if (typeof asked === "function") asked();
    else if (asked) setBeside((current) => current?.id === tab.id ? null : { id: tab.id, anchor: button, actions: asked });
  };
  const row = (tab: LayoutStripTab, item: ReactNode) => {
    const at = found.indexOf(tab);
    return (
      <div key={tab.id} {...reorder.row(tab.id, () => Boolean(onMove))} className={listStyles.row} data-layout-tabs-row-box={tab.id}>
        {onMove ? <button type="button" {...reorder.handle(tab.id, true, { above: found[at - 1]?.id, below: found[at + 1]?.id })}
          className={`${listStyles.tool} ${listStyles.handle}`} aria-label={t("reorderView", { name: tab.name })}
          onClick={(event) => event.stopPropagation()}><DragHandleIcon size="small" aria-hidden="true" /></button> : null}
        {item}
        <button type="button" className={`${listStyles.tool} ${listStyles.more}`} aria-label={labels.openActions(tab.name)}
          aria-haspopup="dialog" aria-expanded={beside?.id === tab.id} data-layout-tabs-row-more={tab.id}
          onKeyDown={(event) => event.stopPropagation()} onClick={(event) => { event.stopPropagation(); more(tab, event.currentTarget); }}>
          <DotsHorizontal size="small" aria-hidden="true" /></button>
      </div>
    );
  };
  const items: Array<LayoutStripTab | typeof ADD> = [...found, ...(onAdd ? [ADD as typeof ADD] : [])];
  const label = projected.collapsed ? (active?.name ?? "") : labels.moreViews(projected.overflow.length);
  const menu = useNativeMenu({ items, open, menuRef: itemsRef, label, quietOnPointer: true, firstKey: activeId,
    focusOwnerRef: tabs.length > 1 ? search : undefined, searchLights: (key) => key !== ADD,
    keyOf: (item) => item === ADD ? ADD : item.id, textOf: (item) => item === ADD ? labels.newView : item.name,
    onAction: (item) => {
      close();
      if (item === ADD) { if (trigger.current) onAdd?.(trigger.current); } else onPick(item.id);
    } });
  const choice = (tab: LayoutStripTab) => (
    <NativeMenuItem key={tab.id} itemKey={tab.id} state={menu.state} className={listStyles.item}
      aria-current={tab.id === activeId ? "true" : undefined} data-layout-tabs-row={tab.id}>
      {() => <>{tab.icon}<span className={listStyles.itemName}>{tab.name}</span>
        {tab.id === activeId ? <CheckIcon size="small" aria-hidden="true" className={listStyles.current} /> : null}</>}
    </NativeMenuItem>
  );
  const opener = (body: ReactNode, picker: boolean) => (
    <button ref={trigger} type="button" className={`${sheet.viewTab} ${sheet.viewOverflowButton}`} aria-haspopup="menu" aria-expanded={open}
      data-layout-tabs-more={picker ? "picker" : "more"} data-press-keeps-focus
      onClick={() => { setQuery(""); setBeside(null); setOpen(!open); }}>{body}</button>
  );
  return (
    <div ref={ref} className={className} {...attributes} data-layout-tab-strip>
      {projected.collapsed && active ? opener(<>{active.icon}<span>{active.name}</span><ChevronDown size="small" aria-hidden="true" /></>, true) : (
        <>
          {shown.map((tab) => renderTab(tab))}
          {projected.overflow.length > 0 ? opener(<span>{labels.moreViews(projected.overflow.length)}</span>, false) : after}
        </>
      )}
      {open && folds && typeof document !== "undefined" ? createPortal(
        <div ref={menuRef} className={sheet.viewTabMenu} data-view-overflow-menu="true" data-layout-tabs-menu
          style={{ position: "fixed", ...popoverPlacementStyle(place) }}>
          <div className={listStyles.list}>
            {tabs.length > 1 ? <div className={listStyles.search}><input {...menu.ownerProps} ref={search} value={query}
              aria-label={t("searchViews")} placeholder={t("searchViews")} onChange={(event) => setQuery(event.currentTarget.value)} /></div> : null}
            <div {...menu.menuProps} ref={itemsRef} className={listStyles.items}>
              {query.trim() && found.length === 0 ? <div className={listStyles.noResults}>{t("viewSearchNoResults")}</div> : null}
              {items.map((item) => item === ADD ? (
                <div key={ADD}>
                  <div role="separator" className={listStyles.divider} />
                  <NativeMenuItem itemKey={ADD} state={menu.state} className={`${listStyles.item} ${listStyles.action}`} data-layout-tabs-add>
                    {() => <><Plus size="regular" aria-hidden="true" /><span className={listStyles.itemName}>{labels.newView}</span></>}
                  </NativeMenuItem>
                </div>
              ) : rowActions ? row(item, choice(item)) : choice(item))}
            </div>
          </div>
          {beside ? <div className={listStyles.rowActions}><DatabaseViewActionsBeside key={beside.id} anchor={beside.anchor} gap={0} onClose={() => setBeside(null)}>
            {(placed) => <DatabaseViewActionChoices open={placed} label={labels.openActions(tabs.find(({ id }) => id === beside.id)?.name ?? "")}
              actions={beside.actions.map((action) => ({ ...action, act: () => { setBeside(null); action.act(); } }))} />}
          </DatabaseViewActionsBeside></div> : null}
        </div>,
        document.body,
      ) : null}
    </div>
  );
}
