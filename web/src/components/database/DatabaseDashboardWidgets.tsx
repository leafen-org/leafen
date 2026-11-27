import { Fragment, useLayoutEffect, useRef, useState, type KeyboardEvent, type PointerEvent, type ReactNode } from "react";
import { combine } from "@atlaskit/pragmatic-drag-and-drop/combine";
import { draggableByPointer, dropTargetByPointer } from "@/lib/pointerDrag";
import { attachClosestEdge, extractClosestEdge } from "@atlaskit/pragmatic-drag-and-drop-hitbox/closest-edge";
import { createPortal } from "react-dom";
import type { GraphView, GraphViewType } from "@/contentGraph";
import { Plus } from "@/icons/leafen";
import { useViewToSidebar } from "@/components/sidebar/useViewToSidebar";
import { popoverRectOf, type PopoverRect } from "@/lib/popoverPlacement";
import {
  DASHBOARD_ROW_FITS_CONTENT,
  DASHBOARD_ROW_HEIGHT,
  DASHBOARD_WIDGETS,
  DASHBOARD_WIDGETS_PER_ROW,
  dashboardLayoutOf,
  dashboardWidgetCount,
  moveDashboardRow,
  placeDashboardWidget,
  moveDashboardWidget,
  dashboardMinColumns,
  dashboardWidgetBox,
  dashboardWidgetLine,
  dashboardWidgetsPerLine,
  placeDashboardDivider,
  resizeDashboardRow,
  type DashboardLayout,
  type DashboardPlacement,
} from "@/lib/dashboardLayout";
import { dashboardWidgetMoves, moveDashboardWidgetBy } from "@/lib/dashboardWidgetMoves";
import { useKeyedRefs } from "@/lib/useKeyedRefs";
import type { DatabaseContainerSource } from "@/lib/databaseContainerPicker";
import { DashboardEmptyMarks, DashboardEmptyPicture } from "./DashboardEmptyPicture";
import { DashboardRowSide } from "./DashboardRowSide";
import { DashboardWidgetMenu, useDashboardSourceLinks } from "./DashboardWidgetMenu";
import { DashboardWidgetPanel } from "./DashboardWidgetPanel";
import { DashboardSourcePicker } from "./DashboardSourcePicker";
import { DASHBOARD_WIDGET_SETTINGS_EVENT, DashboardWidgetHeightContext } from "./dashboardWidgetHeight";
import { ViewIcon } from "./databaseViewShared";
import { takeDashboardMade } from "./dashboardJustMade";
import { databaseViewLabels } from "./databaseViewLabels";
import styles from "./DatabaseDashboardWidgets.module.css";
import { arrowAlongRow, readsRightToLeft } from "@/lib/readingDirection";

// A dashboard lays out its widget views in rows, at most four to a row and
// twelve in all, and draws each widget as its own view under its name, as
// Notion's (seen 2026-11-03). Viewers use the widgets; an editor turns on
// Edit mode with "편집하기" in the toolbar to add widgets, drag them within
// or between rows, resize their widths and row heights, move rows, and
// duplicate, move or delete them from a widget's options under its name.

/** A widget's name line and foot around its card. */
const CARD_INSET = 44 + 6;
const HEIGHT_STEP = 40;
/** The empty widget a new one's source is asked beside, as Notion's "새로운 보기" (seen 2026-11-12). */
const PENDING_WIDGET = "dashboard-widget-pending";

export type DashboardWidgetEditing = {
  /** Its sources, each with the name it goes by (blockDataSourceName). */
  sources: readonly { id: string; title: string; name: string }[];
  /** Whether a new widget asks its source first (dashboardWidgetEditing). */
  asksSource: boolean;
  /** The database's views a new widget can take after: its tabs and widgets. */
  views: readonly GraphView[];
  add(dataSourceId: string, type: GraphViewType, placement: DashboardPlacement): Promise<GraphView | null>;
  /** A chart of the source asked for, which the database links with it where it neither holds nor links it. */
  addFrom(candidate: DatabaseContainerSource, placement: DashboardPlacement): Promise<GraphView | null>;
  /** Turns a widget to another source, as its settings' "데이터베이스" does; true once it shows it. */
  switchSource(widget: GraphView, candidate: DatabaseContainerSource): Promise<boolean>;
  update(widget: GraphView): Promise<void>;
  /** Turns a widget into a new view of another kind. */
  retype(widget: GraphView, type: GraphViewType): Promise<void>;
  duplicate(widget: GraphView): void;
  remove(widgetId: string): Promise<void>;
  arrange(layout: DashboardLayout): Promise<void>;
};

export function dashboardWidgetRows(dashboard: GraphView): string[][] {
  return dashboardLayoutOf(dashboard).map((row) => row.widgets.map(({ id }) => id));
}

export function DatabaseDashboardWidgets({
  dashboard,
  widgets,
  editing,
  barSlot,
  holderPageId,
  renderWidget,
}: {
  dashboard: GraphView;
  /** The page the database sits on, for "사이드바에 보기 추가". */
  holderPageId?: string;
  widgets: readonly GraphView[];
  editing?: DashboardWidgetEditing;
  /** The toolbar's end, where "편집하기" / "완료" goes. */
  barSlot?: HTMLElement | null;
  /** Draws a widget's view, its tools in its head's slot; null when its source can't be shown. */
  renderWidget(widget: GraphView, head?: { slot: HTMLElement; mode: "view" | "edit" } | null): ReactNode;
}) {
  const labels = databaseViewLabels();
  const live = new Map(widgets.filter((widget) => widget.config.dashboardViewId === dashboard.id).map((widget) => [widget.id, widget]));
  const stored = dashboardLayoutOf(dashboard, (id) => live.has(id));
  // A new dashboard opens in Edit mode, ready for its first widget.
  const [editMode, setEditMode] = useState(() => Boolean(editing) && stored.length === 0 && takeDashboardMade(dashboard.id));
  // The widget whose "새 보기" is open, and whether its source was asked first (its "←").
  const [adding, setAdding] = useState<{ id: string; asked: boolean } | null>(null);
  // A source being asked: a new widget's, at its place, or a widget's again from its "←".
  const [picking, setPicking] = useState<{ placement: DashboardPlacement } | { widgetId: string } | null>(null);
  const [draft, setDraft] = useState<DashboardLayout | null>(null);
  const [dragged, setDragged] = useState<string | null>(null);
  const [drop, setDrop] = useState<DashboardPlacement | null>(null);
  const [menu, setMenu] = useState<{ widget: GraphView; anchor: PopoverRect } | null>(null);
  const addToSidebar = useViewToSidebar(holderPageId), sourceLink = useDashboardSourceLinks();
  // A row dragged by its handle, and the gap it would land in.
  const [rowDrag, setRowDrag] = useState<number | null>(null);
  const sectionRef = useRef<HTMLElement>(null);
  // How wide the rows are now: the columns a widget keeps at least and the widgets a line holds.
  const [rowWidth, setRowWidth] = useState(0);
  const minColumns = dashboardMinColumns(rowWidth), perLine = dashboardWidgetsPerLine(rowWidth);
  useLayoutEffect(() => {
    const section = sectionRef.current;
    if (!section) return;
    const measure = () => setRowWidth(section.querySelector("[data-dashboard-row]")?.getBoundingClientRect().width ?? 0);
    const observer = new ResizeObserver(measure);
    observer.observe(section);
    measure();
    return () => observer.disconnect();
  }, []);
  function openWidgetSettings(widgetId: string, menu: "view" | "newView") {
    // Found when sent: a widget given a new layout draws its view again first.
    window.setTimeout(() => sectionRef.current?.querySelector(`[data-dashboard-widget-tools="${CSS.escape(widgetId)}"]`)
      ?.dispatchEvent(new CustomEvent(DASHBOARD_WIDGET_SETTINGS_EVENT, { detail: menu })), 120);
  }
  // Each widget's head slot, where its view's tools go.
  const [heads, setHeads] = useState<Record<string, HTMLElement>>({});
  const headRef = (id: string) => (element: HTMLElement | null) => {
    if (element && heads[id] !== element) setHeads((current) => ({ ...current, [id]: element }));
  };
  const layout = draft ?? stored;
  const total = dashboardWidgetCount(layout);
  const editable = editing && editMode ? editing : undefined;
  // What is drawn: the layout, with the empty widget a source is asked for in its place.
  const pendingAt = editable && picking && "placement" in picking ? picking.placement : null;
  const drawn = pendingAt ? placeDashboardWidget(layout, PENDING_WIDGET, pendingAt) ?? layout : layout;

  // A change builds on the layout shown, a save still on its way included,
  // and the layout shown stays until the last of them is saved.
  function arrange(next: DashboardLayout | null) {
    if (!editable || !next) return;
    setDraft(next);
    void editable.arrange(next).finally(() => setDraft((current) => current === next ? null : current));
  }
  // "+" makes a chart at once, brought into sight, and offers what it can
  // become beside it; where the dashboard asks a widget's source first, an
  // empty widget takes its place while it is asked (Notion's, seen 2026-11-12).
  async function add(placement: DashboardPlacement) {
    if (!editable) return;
    if (editable.asksSource) {
      setAdding(null); setMenu(null); setPicking({ placement });
      window.requestAnimationFrame(() => document.querySelector(`[data-dashboard-widget="${PENDING_WIDGET}"]`)?.scrollIntoView({ block: "nearest" }));
      return;
    }
    const source = editable.sources[0];
    const widget = source && await editable.add(source.id, "chart", placement);
    if (widget) offer(widget.id, false);
  }
  function offer(widgetId: string, asked: boolean) {
    window.requestAnimationFrame(() => {
      document.querySelector(`[data-dashboard-widget="${CSS.escape(widgetId)}"]`)?.scrollIntoView({ block: "nearest" });
      setAdding({ id: widgetId, asked });
    });
  }
  // The source chosen: a new widget of it in the empty one's place, or the widget turned to it.
  async function chooseSource(candidate: DatabaseContainerSource) {
    if (!editable || !picking) return false;
    const widget = "placement" in picking ? await editable.addFrom(candidate, picking.placement) : live.get(picking.widgetId);
    if (!widget || ("widgetId" in picking && !await editable.switchSource(widget, candidate))) return false;
    setPicking(null);
    offer(widget.id, true);
    return true;
  }
  // A drag follows the pointer on the handle and saves when it is released.
  function track(event: PointerEvent<HTMLElement>, resize: (x: number, y: number) => DashboardLayout) {
    if (!editable) return;
    event.preventDefault();
    const handle = event.currentTarget;
    handle.setPointerCapture(event.pointerId);
    const start = layout;
    let next = start;
    const move = (moveEvent: globalThis.PointerEvent) => {
      next = resize(moveEvent.clientX, moveEvent.clientY);
      setDraft(next);
    };
    const end = () => {
      handle.removeEventListener("pointermove", move);
      handle.removeEventListener("pointerup", end);
      handle.removeEventListener("pointercancel", end);
      if (next !== start) arrange(next);
    };
    handle.addEventListener("pointermove", move);
    handle.addEventListener("pointerup", end);
    handle.addEventListener("pointercancel", end);
  }
  // A divider moves by whole columns of the row's twelve, each widget
  // keeping the columns 240px takes (placeDashboardDivider).
  function startWidth(event: PointerEvent<HTMLDivElement>, rowIndex: number, leftIndex: number) {
    const row = event.currentTarget.closest<HTMLElement>("[data-dashboard-row]");
    const box = row?.getBoundingClientRect();
    if (!row || !box?.width) return;
    // The row's columns run from its start: its right where it reads from the right.
    const rtl = readsRightToLeft(row);
    track(event, (x) => placeDashboardDivider(layout, rowIndex, leftIndex,
      (rtl ? box.right - x : x - box.left) / box.width * 12, dashboardMinColumns(box.width), dashboardWidgetsPerLine(box.width)));
  }
  // A row's height is each of its lines' (dashboardWidgetBox): its foot drags its last line's.
  function startHeight(event: PointerEvent<HTMLDivElement>, rowIndex: number) {
    const row = event.currentTarget.parentElement?.querySelector("[data-dashboard-row] > :last-child")?.getBoundingClientRect();
    if (!row) return;
    track(event, (_x, y) => resizeDashboardRow(layout, rowIndex, y - row.top));
  }
  function widthKey(event: KeyboardEvent<HTMLDivElement>, rowIndex: number, leftIndex: number) {
    const key = arrowAlongRow(event.key, event.currentTarget);
    const step = key === "ArrowLeft" ? -1 : key === "ArrowRight" ? 1 : 0;
    const row = layout[rowIndex];
    if (!step || !row) return;
    event.preventDefault();
    const { before, columns } = dashboardWidgetLine(row, leftIndex, perLine, minColumns);
    arrange(placeDashboardDivider(layout, rowIndex, leftIndex, before + columns + step, minColumns, perLine));
  }
  function heightKey(event: KeyboardEvent<HTMLDivElement>, rowIndex: number) {
    const step = event.key === "ArrowUp" ? -HEIGHT_STEP : event.key === "ArrowDown" ? HEIGHT_STEP : 0;
    const height = layout[rowIndex]?.height;
    if (!step || height === undefined) return;
    event.preventDefault();
    // A row fitted to its content steps from the height it shows.
    const shown = height > 0 ? height : height === DASHBOARD_ROW_FITS_CONTENT ? sectionRef.current
      ?.querySelector(`[data-dashboard-row="${rowIndex}"] > :last-child`)?.getBoundingClientRect().height ?? 0 : DASHBOARD_ROW_HEIGHT.standard;
    arrange(resizeDashboardRow(layout, rowIndex, Math.round(shown) + step));
  }
  // A row's handle drags the whole row to a gap between rows, as Notion's
  // "드래그" beside a row in Edit mode (seen 2026-11-05).
  function startRowDrag(event: PointerEvent<HTMLButtonElement>, from: number) {
    if (!editable) return;
    event.preventDefault();
    const handle = event.currentTarget;
    handle.setPointerCapture(event.pointerId);
    const frames = Array.from(sectionRef.current?.querySelectorAll<HTMLElement>("[data-dashboard-row-frame]") ?? []);
    let target = from;
    setRowDrag(from);
    const move = (moveEvent: globalThis.PointerEvent) => {
      target = frames.filter((frame) => { const box = frame.getBoundingClientRect(); return box.top + box.height / 2 < moveEvent.clientY; }).length;
      setDrop({ rowIndex: target, newRow: true });
    };
    const end = () => {
      handle.removeEventListener("pointermove", move);
      handle.removeEventListener("pointerup", end);
      handle.removeEventListener("pointercancel", end);
      setRowDrag(null); setDrop(null);
      if (target === from || target === from + 1) return;
      const rows = [...layout];
      const [moved] = rows.splice(from, 1);
      rows.splice(target > from ? target - 1 : target, 0, moved);
      arrange(rows);
    };
    handle.addEventListener("pointermove", move);
    handle.addEventListener("pointerup", end);
    handle.addEventListener("pointercancel", end);
  }
  // A widget's drag (a mouse's by the pointer's events, pointerDrag); the layout
  // says where it may land: beside a widget in its row, or in a new row between rows.
  const dragKind = `dashboard-widget:${dashboard.id}`;
  const latest = useRef({ layout, editable, arrange });
  latest.current = { layout, editable, arrange };
  const widgetElements = useRef(new Set<HTMLElement>());
  useLayoutEffect(() => {
    for (const element of widgetElements.current) element.draggable = Boolean(editable);
  });
  // A drop target is a widget (keyed by its id) or the gap before a row
  // (keyed by that row's index).
  type DropKey = { widget: string } | { gap: number };
  function placementAt(key: DropKey, data: Record<string | symbol, unknown>): DashboardPlacement | null {
    if ("gap" in key) return { rowIndex: key.gap, newRow: true };
    const rows = latest.current.layout;
    const rowIndex = rows.findIndex((row) => row.widgets.some((widget) => widget.id === key.widget));
    if (rowIndex < 0) return null;
    const index = rows[rowIndex].widgets.findIndex((widget) => widget.id === key.widget);
    // After it on its end side: its left where the dashboard reads from the right.
    return { rowIndex, newRow: false, index: extractClosestEdge(data) === (readsRightToLeft() ? "left" : "right") ? index + 1 : index };
  }
  const dropRef = useKeyedRefs<string>((name, element) => {
    const key: DropKey = name.startsWith("gap:") ? { gap: Number(name.slice(4)) } : { widget: name.slice(7) };
    const widgetId = "widget" in key ? key.widget : undefined;
    const landing = (id: unknown, data: Record<string | symbol, unknown>) => {
      const placement = placementAt(key, data);
      return placement && moveDashboardWidget(latest.current.layout, String(id), placement) ? placement : null;
    };
    const show = (id: unknown, data: Record<string | symbol, unknown>) => {
      const next = landing(id, data);
      setDrop((current) => current?.rowIndex === next?.rowIndex && current?.newRow === next?.newRow
        && current?.index === next?.index ? current : next);
    };
    const target = dropTargetByPointer({
      element,
      canDrop: ({ source }) => source.data.kind === dragKind && source.data.id !== widgetId,
      getData: ({ input, element: self }) => widgetId
        ? attachClosestEdge({}, { input, element: self, allowedEdges: ["left", "right"] }) : {},
      onDragEnter: ({ source, self }) => show(source.data.id, self.data),
      onDrag: ({ source, self }) => show(source.data.id, self.data),
      onDragLeave: () => setDrop(null),
      onDrop: ({ source, self }) => {
        const placement = landing(source.data.id, self.data);
        setDrop(null);
        if (placement) latest.current.arrange(moveDashboardWidget(latest.current.layout, String(source.data.id), placement));
      },
    });
    if (!widgetId) return target;
    widgetElements.current.add(element);
    const registered = combine(target, () => widgetElements.current.delete(element), draggableByPointer({
      // Taken by its name's line, as Notion's: a press in its body uses the view.
      element, dragHandle: element.querySelector<HTMLElement>(":scope > header") ?? undefined,
      canDrag: () => Boolean(latest.current.editable),
      getInitialData: () => ({ kind: dragKind, id: widgetId }),
      onDragStart: () => setDragged(widgetId),
      onDrop: () => { setDragged(null); setDrop(null); },
    }));
    element.draggable = Boolean(latest.current.editable);
    return registered;
  });
  const rowGap = (rowIndex: number) => editable && (dragged || rowDrag !== null) ? (
    <div ref={dropRef(`gap:${rowIndex}`)} className={styles.rowGap}
      data-dashboard-row-gap={rowIndex} aria-label={labels.dashboardNewRow}
      data-drop-target={drop?.newRow && drop.rowIndex === rowIndex ? "" : undefined} />
  ) : null;
  const widgetWidth = (row: DashboardLayout[number], index: number) => dashboardWidgetBox(row, index, perLine, minColumns);
  const nameOf = (widget: GraphView) => widget.name || labels.viewTypes[widget.type as keyof typeof labels.viewTypes];
  // "제목에 아이콘 표시" puts each widget's view icon before its name (Notion's, seen 2026-11-07).
  const titleOf = (widget: GraphView) => <>{dashboard.config.dashboardTitleIcons === true && <ViewIcon view={widget} size="small" />}{nameOf(widget)}</>;
  const addedWidget = adding && !picking ? live.get(adding.id) : undefined;
  const toggle = editing && (
    <button type="button" className={styles.modeButton} data-dashboard-edit-toggle aria-pressed={editMode}
      onClick={() => { setEditMode(!editMode); setAdding(null); setPicking(null); setMenu(null); }}>
      {editMode ? labels.doneEditingDashboard : labels.editDashboard}
    </button>
  );

  return (
    <section ref={sectionRef} className={styles.dashboard} aria-label={labels.dashboardViewAria(dashboard.name)} data-dashboard-widgets
      data-dashboard-mode={editable ? "edit" : "view"}>
      {toggle && (barSlot ? createPortal(toggle, barSlot) : <div className={styles.modeBar}>{toggle}</div>)}
      {drawn.length === 0 && !editable && (
        // Out of Edit mode an empty dashboard says what it holds and how to start it, as Notion's (seen 2026-11-07).
        <div className={styles.emptyView} data-dashboard-empty="view">
          <p>{labels.dashboardEmptyHint}</p>
          {editing && <button type="button" className={styles.modeButton} onClick={() => setEditMode(true)}>{labels.editEmptyDashboard}</button>}
          <DashboardEmptyPicture />
        </div>
      )}
      {drawn.length === 0 && editable && (
        // Notion's empty dashboard: the first widget's place is one "조회수 보기"
        // button, its whole frame, marks and pill alike (seen 2026-11-12: 704×320).
        <button type="button" className={styles.emptyFrame} data-dashboard-empty data-dashboard-widget-add
          aria-label={labels.addFirstDashboardWidget} onClick={() => void add({ rowIndex: 0, newRow: true })}>
          <span className={styles.emptyCard}>
            <DashboardEmptyMarks className={styles.emptyIcons} />
            <span className={styles.emptyAdd} aria-hidden="true"><Plus size="tiny" /><span>{labels.addFirstDashboardWidget}</span></span>
          </span>
        </button>
      )}
      {drawn.map((row, rowIndex) => (
        <Fragment key={row.widgets[0].id}>
          {rowGap(rowIndex)}
          {/* Between two rows, a new widget in a row of its own (Notion's, seen 2026-11-05). */}
          {editable && rowIndex > 0 && !dragged && rowDrag === null && total < DASHBOARD_WIDGETS ? (
            <button type="button" className={styles.rowInsert} aria-label={labels.addDashboardWidget} data-dashboard-row-insert={rowIndex}
              onClick={() => void add({ rowIndex, newRow: true })}>
              <span className={styles.rowInsertLine} aria-hidden="true" /><span className={`${styles.roundButton} ${styles.rowInsertPlus}`} aria-hidden="true"><Plus size="tiny" /></span>
            </button>
          ) : null}
          <div className={styles.rowFrame} data-dashboard-row-frame={rowIndex}>
            {editable && <DashboardRowSide rowIndex={rowIndex} steps={([-1, 1] as const).filter((step) => layout[rowIndex + step])}
              fitted={row.height === DASHBOARD_ROW_FITS_CONTENT} onDragStart={(event) => startRowDrag(event, rowIndex)}
              onMove={(step) => arrange(moveDashboardRow(layout, rowIndex, step))}
              onFit={() => arrange(resizeDashboardRow(layout, rowIndex, DASHBOARD_ROW_FITS_CONTENT))} />}
            <div className={styles.row} data-dashboard-row={rowIndex} data-dashboard-row-fits={row.height === DASHBOARD_ROW_FITS_CONTENT ? "" : undefined}>
              {row.widgets.map((cell, index) => {
                if (cell.id === PENDING_WIDGET) return (
                  <article key={cell.id} className={styles.widget} style={widgetWidth(row, index)} data-dashboard-widget={cell.id}
                    data-dashboard-widget-pending data-selected="">
                    <header className={styles.head}><span className={styles.title}>{labels.newDashboardViewSection}</span></header>
                    <div className={styles.card} />
                  </article>
                );
                const widget = live.get(cell.id) as GraphView;
                const content = renderWidget(widget, heads[cell.id] ? { slot: heads[cell.id], mode: editable ? "edit" : "view" } : null);
                const side = drop && !drop.newRow && drop.rowIndex === rowIndex
                  ? drop.index === index ? "before" : drop.index === index + 1 ? "after" : undefined
                  : undefined;
                return (
                  <Fragment key={cell.id}>
                    {index % perLine > 0 && editable && (
                      <div className={styles.widthHolder}>
                        <div className={styles.widthHandle} role="separator" aria-orientation="vertical" tabIndex={0}
                          aria-label={labels.resizeDashboardWidgets} data-dashboard-widget-resize={`${rowIndex}:${index - 1}`}
                          onPointerDown={(event) => startWidth(event, rowIndex, index - 1)}
                          onKeyDown={(event) => widthKey(event, rowIndex, index - 1)} />
                      </div>
                    )}
                    <article ref={dropRef(`widget:${cell.id}`)}
                      className={styles.widget} style={widgetWidth(row, index)} data-dashboard-widget={cell.id}
                      data-dashboard-widget-type={widget.type} data-dragging={dragged === cell.id ? "" : undefined}
                      data-drop-side={side} data-selected={menu?.widget.id === cell.id || adding?.id === cell.id
                        || (picking && "widgetId" in picking && picking.widgetId === cell.id) ? "" : undefined}>
                      <header className={styles.head}>
                        {editable || addToSidebar || sourceLink(widget) ? (
                          <button type="button" className={styles.title} aria-label={labels.dashboardWidgetMenu(nameOf(widget))}
                            aria-haspopup="menu" aria-expanded={menu?.widget.id === cell.id} data-dashboard-widget-menu-trigger={cell.id}
                            onClick={(event) => setMenu({ widget, anchor: popoverRectOf(event.currentTarget)! })}>
                            {titleOf(widget)}
                          </button>
                        ) : <span className={styles.title}>{titleOf(widget)}</span>}
                        <span ref={headRef(cell.id)} className={styles.headTools} data-dashboard-widget-tools={cell.id} />
                      </header>
                      {/* Its view stays in use in Edit mode, as Notion's (its widgets' views are never disabled there, read
                          2026-11-12): "필터 편집하기" opens the widget's filters; its rows' "+ ⋮⋮" stay hidden (CSS). */}
                      <div className={styles.card} data-dashboard-widget-body>
                        {/* A chart fills its card; in a row fitted to its content, the default card. */}
                        <DashboardWidgetHeightContext.Provider value={(row.height > 0 ? row.height : DASHBOARD_ROW_HEIGHT.standard) - CARD_INSET}>
                          {content ?? <p className={styles.empty}>{labels.dashboardWidgetUnavailable}</p>}
                        </DashboardWidgetHeightContext.Provider>
                      </div>
                    </article>
                  </Fragment>
                );
              })}
            </div>
            {editable && row.widgets.length < DASHBOARD_WIDGETS_PER_ROW && total < DASHBOARD_WIDGETS && (
              <button type="button" className={`${styles.roundButton} ${styles.addBeside}`} aria-label={labels.addDashboardWidgetBeside}
                title={labels.addDashboardWidgetBeside} data-dashboard-widget-add-beside={rowIndex}
                onClick={() => void add({ rowIndex, newRow: false })}>
                <Plus size="tiny" />
              </button>
            )}
            {editable && (
              <div className={styles.heightHandle} role="separator" aria-orientation="horizontal" tabIndex={0}
                aria-label={labels.resizeDashboardRow} data-dashboard-row-resize={rowIndex}
                onPointerDown={(event) => startHeight(event, rowIndex)} onKeyDown={(event) => heightKey(event, rowIndex)} />
            )}
          </div>
        </Fragment>
      ))}
      {rowGap(layout.length)}
      {editable && layout.length > 0 && total < DASHBOARD_WIDGETS && (
        <div className={styles.addRow}>
          <button type="button" className={styles.roundButton} aria-label={labels.addDashboardWidget} title={labels.addDashboardWidget}
            data-dashboard-widget-add onClick={() => void add({ rowIndex: layout.length, newRow: true })}>
            <Plus size="tiny" />
          </button>
        </div>
      )}
      {/* Notion's twelve: told when it comes near, and why adding stops at it. */}
      {editable && total >= DASHBOARD_WIDGETS - 2 && (
        <p className={styles.empty} role="status" data-dashboard-widgets-capacity={total >= DASHBOARD_WIDGETS ? "full" : "near"}>
          {total >= DASHBOARD_WIDGETS ? labels.dashboardWidgetsFull(DASHBOARD_WIDGETS) : labels.dashboardWidgetsNear(total, DASHBOARD_WIDGETS)}
        </p>
      )}
      {menu && !editable && (
        <DashboardWidgetMenu widget={menu.widget} anchor={menu.anchor} onClose={() => setMenu(null)} viewing moves={[]}
          onAddToSidebar={addToSidebar ? () => addToSidebar(menu.widget) : undefined} onMove={() => undefined} onDuplicate={() => undefined} onRemove={() => undefined} />
      )}
      {menu && editable && (
        <DashboardWidgetMenu widget={menu.widget} anchor={menu.anchor} onClose={() => setMenu(null)}
          onEdit={() => openWidgetSettings(menu.widget.id, "view")} onAddToSidebar={addToSidebar ? () => addToSidebar(menu.widget) : undefined}
          moves={dashboardWidgetMoves(layout, menu.widget.id)} onMove={(move) => arrange(moveDashboardWidgetBy(layout, menu.widget.id, move))}
          onDuplicate={() => editable.duplicate(menu.widget)} onRemove={() => void editable.remove(menu.widget.id)} />
      )}
      {/* A layout chosen for it, the widget's "새 보기" settings take the panel's place, as Notion's (seen 2026-11-03). */}
      {addedWidget && editable && <DashboardWidgetPanel key={addedWidget.id} editing={editable} widget={addedWidget}
        onBack={adding?.asked ? () => setPicking({ widgetId: addedWidget.id }) : undefined} onDone={() => setAdding(null)}
        onRetyped={(widgetId, settings) => openWidgetSettings(widgetId, settings)} />}
      {/* The source asked first: "기존 데이터 소스 연결하기" beside the widget, as Notion's (seen 2026-11-12). */}
      {picking && editable && <DashboardSourcePicker key={"placement" in picking ? PENDING_WIDGET : picking.widgetId}
        widgetId={"placement" in picking ? PENDING_WIDGET : picking.widgetId}
        current={"widgetId" in picking ? live.get(picking.widgetId)?.dataSourceId : undefined}
        onChoose={chooseSource} onClose={() => { setPicking(null); setAdding(null); }} />}
    </section>
  );
}
