"use client";

import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type MouseEvent as ReactMouseEvent,
  type PointerEvent as ReactPointerEvent,
  type ReactNode,
} from "react";
import type { DbProperty, Page } from "@/lib/types";
import type { PropertyValueReadContext } from "./PropValue";
import type { ConditionalColorMatch } from "./conditionalColorModel";
import { dateKey } from "./dateUtils";
import type { TimelineInteractionMode } from "./timelineInteractionModel";
import type { TimelineDayLabel, TimelineMonthSpan, TimelineNowLabel } from "./timelineViewModel";
import type { TimelineRenderedRow, TimelineRowOpenMode } from "./TimelineRows";
import { TimelineBar, type TimelineBarFrame, type TimelineBarWays } from "./TimelineBar";
import { useLiveWays, useSteadyFrame } from "./memoizedViewItems";
import { useKeyedRefs } from "@/lib/useKeyedRefs";
import styles from "./databaseSheet";
import { TimelineDependencyArrows } from "./TimelineDependencyArrows";
import { barShowsPageIcon } from "./CardTitleIcon";
import { TimelineGroupLanes, type TimelineGroupActions } from "./TimelineGroupLanes";
import { TimelineOffRangeMarkers } from "./TimelineOffRangeMarkers";
import { flushHeldStyleWrites, holdMeasure, holdStyleWrite } from "./heldStyleWrites";
import { timelineMarginBleed, timelineTableCover } from "./timelineShownEdge";
import { dropTargetByPointer } from "@/lib/pointerDrag";
import type { TimelineAddLane, TimelineAwayLane, TimelineBarLane, TimelineGroupLane, TimelineSubitemAddLane, TimelineSubitemElbow, TimelineSubitemLink } from "./timelineLaneProjection";
import { rowOpenModeOfClick } from "./rowOpenClick";
import { useOwnPageViewFill } from "./useOwnPageViewFill";
import { textWidth, useTextWidthsVersion } from "@/lib/textWidth";
import { arrowAlongRow, inlineScrolled, readsRightToLeft } from "@/lib/readingDirection";
import { scrollInlineTo, useTimelineHeldPlace } from "./timelineHeldPlace";
import { TimelineDayHeads } from "./TimelineDayHeads";
import { TimelineCreateLane } from "./TimelineCreateLane"; import { TimelineUndatedLanes, type TimelineUndatedLane } from "./TimelineUndatedLanes";
import { OwnPageViewBar } from "./OwnPageViewBar";
import { useTimelineRangePill } from "./TimelineRangePill";
import { timelineBarValuesWidth } from "./timelineBarValuesWidth";
import { fieldEnter } from "@/lib/fieldKeyBoundary";
import { inlineInto, TimelineSubitemGhostLane } from "./TimelineSubitemGhostLane";
import type { TimelineDropLine } from "./useTimelineRowReorder";

// The time at the middle of the shown days.
function shownCenter(scroller: HTMLElement, days: readonly Date[], dayWidth: number) {
  if (days.length < 2) return null;
  const middle = (inlineScrolled(scroller) + scroller.clientWidth / 2) / dayWidth;
  const at = Math.max(0, Math.min(days.length - 2, Math.floor(middle)));
  const start = days[at].getTime(), step = days[at + 1].getTime() - start;
  return start + (middle - at) * step;
}

// How near an end of its window the shown days come before it is drawn again
// around them: under 1000px, as Notion's (seen 2026-11-13: 900 short of
// either end drawn again, 1000 not), and nearer where its window is short.
const NEAR_END = 1000;

// A bar whose title and values fit shows them inside, 6px in, as Notion's
// one-day bar shows "A" (seen 2026-11-02); one too short for them starts
// them there and runs them on past its end (seen 2026-11-12: "셋째 카드" and
// its chip past a two-day bar's end). `values` is the width the values took
// as last drawn, with their gaps.
function contentFits(title: string, icon: boolean, values: number, barWidth: number) {
  const width = textWidth(title, "500 14px");
  if (width === null) return barWidth >= 76 + values;
  return width + (icon ? 22 : 0) + values + 12 <= barWidth - 3;
}

type BarRect = { left: number; right: number; midY: number };
const NO_AWAY: readonly TimelineAwayLane[] = [];

export type TimelineCanvasPreview = {
  first: number; last: number; lane: number; rowId: string;
  /** "move": the bar goes whole with the pointer (useTimelineRowReorder), these its days where it lands; else stretched by that end over these days. */
  mode: TimelineInteractionMode;
  /** Stretched, the other chosen bars it carries, over the days each would take (timelineStretchAlong). */
  along?: ReadonlyMap<string, { first: number; last: number }>;
};

export type TimelineCanvasProps = {
  anchor: Date;
  arrowMarkerId: string;
  beginInteraction(event: ReactPointerEvent<HTMLElement>, item: TimelineRenderedRow, mode: TimelineInteractionMode): void;
  canonicalDependencyGraphActive: boolean;
  childCountByParent: ReadonlyMap<string, number>;
  createDependency(successorRowId: string, predecessorRowId: string): void;
  /** Takes a link away from its line's ✕, offered where links may be drawn (TimelineDependencyArrows). */
  deleteDependency?(successorRowId: string, predecessorRowId: string): void;
  dayLabel(day: Date): TimelineDayLabel;
  /** The day labels stand centered on their day's start (timelineDayLabelsOnTick); lines run down from every `lineColumns`th (timelineLineColumns). */
  dayLabelOnTick?: boolean; lineColumns?: number;
  days: readonly Date[];
  dayWidth: number;
  dependencyPairs: readonly [string, string][];
  endProperty?: DbProperty;
  earliestDate: Date | null;
  flattenedSubtasks: boolean;
  fullDateLabel(date: Date): string;
  gridTemplate: string;
  hiddenSubtasks: boolean;
  items: readonly TimelineRenderedRow[];
  jumpToEarliest(): void;
  /** The conditional color a bar's page takes, if any. */
  barColor?(row: Page): ConditionalColorMatch | null;
  /** Columns Shift+arrow moves a bar: a day of hours, or a week of days; an arrow alone, `smallStep` (an hour of the day zoom's quarter hours, else one). */
  largeStep: number; smallStep?: number;
  /** The values a bar shows, in the view's order, its title among them. */
  barProperties: readonly DbProperty[];
  monthSpans: readonly TimelineMonthSpan[];
  openRow(rowId: string, mode?: TimelineRowOpenMode): void;
  openRowContextMenu(rowId: string, event: ReactMouseEvent<HTMLElement>): void;
  openRowContextMenuFromElement(rowId: string, element: HTMLElement): void;
  pagesById: Readonly<Record<string, Page>>;
  preview: TimelineCanvasPreview | null;
  propertyValueContext: PropertyValueReadContext;
  rangeDays: number; /** The width its columns cover, the one its anchor was found for (timelineRange). */ windowWidth: number;
  readOnly: boolean;
  /** Its bars placed by a rollup's dates, which open but don't move (Notion's, seen 2026-11-06). */
  datesFixed?: boolean;
  renderSubitemControls(item: TimelineRenderedRow): ReactNode;
  /** The rows whose sub-items show under them. */
  expandedRows?: ReadonlySet<string>;
  resetInteraction(): void;
  rowTitle(row: Page): string;
  rowDateLabels?(row: Page): { start: string; end: string };
  shiftRowDate(item: TimelineRenderedRow, deltaDays: number): void;
  showTable: boolean;
  /** A grouped timeline's heads, "+ 새로 만들기" and "+ 신규 그룹" lanes. */
  lanes?: { groups: readonly TimelineGroupLane[]; addLanes: readonly TimelineAddLane[]; newGroupLane?: number; away?: readonly TimelineAwayLane[];
    subitemAdds: readonly TimelineSubitemAddLane[]; subitemLinks: readonly TimelineSubitemLink[]; subitemElbows: readonly TimelineSubitemElbow[] };
  groupActions?: TimelineGroupActions;
  laneCount?: number; laneHeights?: readonly number[];
  toggleGroup?(key: string): void;
  /** "페이지 아이콘 표시": a page's own icon before its bar's title. */
  pageIcons?: boolean;
  suppressOpenRef: React.MutableRefObject<boolean>;
  t(key: string, values?: Record<string, unknown>): string;
  todayIndex: number;
  /** Where it is now on the canvas, a line across the bars, as Notion's (seen 2026-11-01); under the hour zooms its time over it in the head. */
  nowOffset?: number; nowLabel?: TimelineNowLabel;
  focusIndex?: number;
  /** Where on the canvas to open, to the pixel, where the viewer left it; else the focus column's middle. */
  focusOffset?: number;
  viewportIdentity?: string;
  onViewportStart?(date: Date): void;
  /** The time at the middle of the shown days, as they move. */
  onViewportCenter?(time: number): void;
  /** Near an end of the window, the time at its middle: whether the window is drawn again around it. */
  onNearEnd?(center: number): boolean;
  /** Draws the window again with a row dated past it this far (px) from the shown middle, its end or start. */
  onBringAway?(item: TimelineAwayLane, fromMiddle: number): void;
  /** A row from "+ 새로 만들기", a group's in it, its bar from that column `newBarColumns` on (TimelineCreateLane); a day's label pointed at says `dayTip` (TimelineDayHeads). */
  onCreateRow?(firstColumn: number, durationColumns: number, groupKey?: string): void;
  newBarColumns?: number; dayTip?(day: Date): string; /** Undated rows' lines beside the table and the dates a press there gives (TimelineUndatedLanes). */ undated?: TimelineUndatedLane[]; onDateRow?(row: Page, first: number, columns: number): void;
  /** Adds a sub-item: with a column, a bar from there; without, a row with no date. */
  onCreateSubitem?(parentId: string, first?: number): void;
  subitemGhostColumns?: number;
  /** Where a row carried by its handle or its bar would land (useTimelineRowReorder). */
  dropLine?: TimelineDropLine | null;
  newRowId?: string | null;
  onCommitTitle?(row: Page, title: string, byKey?: boolean): void;
  undatedDrag?: { kind: string; onDrop(rowId: string, column: number): void };
};

export function TimelineCanvas(props: TimelineCanvasProps) {
  // Bars' titles are fitted again once the product's fonts load (textWidth.ts).
  const fontsVersion = useTextWidthsVersion();
  const grouped = Boolean(props.lanes?.groups.length);
  const away = props.lanes?.away ?? NO_AWAY;
  const scrollRef = useRef<HTMLDivElement>(null);
  const held = useTimelineHeldPlace();
  useOwnPageViewFill(scrollRef);
  const lanesRef = useRef<HTMLDivElement>(null);
  const barElements = useRef(new Map<string, HTMLElement>());
  const dependencyDragSourceRef = useRef<string | null>(null);
  const autoScrollKeyRef = useRef("");
  // Its window asked to be drawn again around the shown days (onNearEnd).
  const recentering = useRef(false);
  const [barRects, setBarRects] = useState<Record<string, BarRect>>({});
  const [dependencySourceRowId, setDependencySourceRowId] = useState<string | null>(null);
  const latest = useRef(props); latest.current = props;
  useEffect(() => {
    const element = lanesRef.current;
    if (!element || !props.undatedDrag) return;
    return dropTargetByPointer({ element,
      canDrop: ({ source }) => !latest.current.readOnly && source.data.kind === latest.current.undatedDrag?.kind,
      onDrop: ({ source, location }) => {
        const current = latest.current;
        const column = Math.max(0, Math.min(current.rangeDays - 1, Math.floor(inlineInto(element, location.current.input.clientX) / current.dayWidth)));
        current.undatedDrag?.onDrop(String(source.data.id), column);
      },
    });
  }, [props.undatedDrag?.kind]);

  // The day the heading names is the one where the shown days start: the
  // end edge of a table laid over them, or the edge of the box the timeline
  // stands in — on a database's own page, where its days run on under the
  // page's margin, 96px past the window's edge — as Notion's (seen
  // 2026-11-09 and 2026-11-11: "2026년 11월" once 11월 1일's column reaches
  // that edge, "10월" while half a pixel of 10월 31일 still shows; at every
  // zoom, the table open or not).
  const reportShownDay = useCallback(() => {
    const scroller = scrollRef.current, current = latest.current;
    if (!scroller || !current.onViewportStart) return;
    const covered = Math.max(timelineTableCover(scroller), timelineMarginBleed(scroller));
    const column = Math.floor((inlineScrolled(scroller) + covered) / current.dayWidth);
    const day = current.days[Math.max(0, Math.min(current.days.length - 1, column))];
    if (day) current.onViewportStart(day);
    const center = shownCenter(scroller, current.days, current.dayWidth);
    if (current.onViewportCenter && center !== null) current.onViewportCenter(center);
  }, []);
  // Scrolled near an end of its window once it has been placed, it asks to
  // be drawn again around the time in its middle (onNearEnd).
  const scrolled = useCallback(() => {
    const scroller = scrollRef.current, current = latest.current;
    if (!scroller || held.restore(scroller)) return;
    reportShownDay();
    if (!current.onNearEnd || recentering.current || !autoScrollKeyRef.current) return;
    const before = inlineScrolled(scroller), room = scroller.scrollWidth - scroller.clientWidth;
    const center = shownCenter(scroller, current.days, current.dayWidth);
    if (center !== null && Math.min(before, room - before) < Math.min(NEAR_END, room / 3)) recentering.current = current.onNearEnd(center);
  }, [held, reportShownDay]);
  // Drawn again around its middle, that time stays where it was, before the page is drawn.
  useLayoutEffect(() => {
    const scroller = scrollRef.current;
    if (!recentering.current || !scroller) return;
    recentering.current = false;
    if (latest.current.focusOffset !== undefined) held.put(scroller, latest.current.focusOffset - scroller.clientWidth / 2);
  }, [props.anchor]);
  // The table's width changes the edge without a scroll.
  useEffect(() => {
    const table = props.showTable ? scrollRef.current?.parentElement?.querySelector("[data-timeline-table-pane]") : null;
    const dropFirst = holdMeasure(reportShownDay, reportShownDay);
    if (!table || typeof ResizeObserver === "undefined") return dropFirst;
    const observer = new ResizeObserver(reportShownDay);
    observer.observe(table);
    return () => { dropFirst(); observer.disconnect(); };
  }, [props.showTable, props.dayWidth, props.anchor, reportShownDay]);
  // Opening or shutting the table moves a timeline in a page's text between
  // the page's width and its database's box; its days keep their places on
  // the screen, as Notion's (seen 2026-11-11: today's column at 822 either
  // way), and as much where its page lays it out across itself only after it
  // opened (a page's layout tab, seen 2026-11-12: today at 834 with the table
  // and without). Measured a frame on, once the page-wide width has been laid out.
  const scrollerEdgeRef = useRef<number | null>(null);
  // Opened where its viewer left it (focusOffset), that time stays in its middle as its box settles (useTimelineHeldPlace).
  const keepOpenedCenter = useCallback((scroller: HTMLElement) => {
    if (!held.keepOpened(scroller, !!latest.current.showTable)) return false;
    reportShownDay();
    return true;
  }, [held, reportShownDay]);
  useLayoutEffect(() => {
    let frame = 0;
    const keep = () => {
      window.cancelAnimationFrame(frame);
      frame = window.requestAnimationFrame(() => {
        const scroller = scrollRef.current;
        if (!scroller) return;
        const rect = scroller.getBoundingClientRect(), edge = readsRightToLeft(scroller) ? -rect.right : rect.left;
        const before = scrollerEdgeRef.current;
        scrollerEdgeRef.current = edge;
        if (keepOpenedCenter(scroller) || before === null || Math.abs(edge - before) < 0.5) return;
        held.put(scroller, inlineScrolled(scroller) + edge - before);
        reportShownDay();
      });
    };
    keep();
    // Its page marks it wide or boxed (usePageWideTable) on the timeline's own box.
    const root = scrollRef.current?.closest<HTMLElement>("[data-timeline-root]");
    const marked = root && typeof MutationObserver !== "undefined" ? new MutationObserver(keep) : null;
    if (root) marked?.observe(root, { attributes: true, attributeFilter: ["data-page-wide"] });
    // Its width changing alone (its page's scrollbar) moves its middle: the
    // opened time is put back there, else the time now there is told.
    const scroller = scrollRef.current;
    const resized = scroller && typeof ResizeObserver !== "undefined" ? new ResizeObserver(() => {
      if (!autoScrollKeyRef.current || keepOpenedCenter(scroller)) return;
      reportShownDay();
    }) : null;
    if (scroller) resized?.observe(scroller);
    return () => { window.cancelAnimationFrame(frame); marked?.disconnect(); resized?.disconnect(); };
  }, [props.showTable, reportShownDay, keepOpenedCenter]);
  // The viewer moving it lets its days keep their places from then on.
  const viewerMoves = held.release;
  // The table laid over the days ends where their area does, not over the
  // scrollbar under it nor the room its page keeps below, as Notion's (seen
  // 2026-11-13 at 1440: its edge stops at the bars' area's foot).
  useLayoutEffect(() => {
    const scroller = scrollRef.current, days = scroller?.firstElementChild;
    const pane = scroller?.parentElement?.querySelector<HTMLElement>("[data-timeline-table-pane]");
    if (!props.showTable || !scroller || !(days instanceof HTMLElement) || !pane) return undefined;
    const fit = () => {
      const block = `${Math.min(days.offsetHeight, scroller.clientHeight)}px`;
      if (pane.style.getPropertyValue("--timeline-days-block") !== block) holdStyleWrite(() => pane.style.setProperty("--timeline-days-block", block));
    };
    const dropFirst = holdMeasure(pane, fit);
    const observer = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(fit);
    observer?.observe(scroller); observer?.observe(days);
    return () => { dropFirst(); observer?.disconnect(); };
  }, [props.showTable]);

  // The width each bar's values take once drawn, for its fit (contentFits), measured with the view's other measures
  // (holdMeasure): each bar's fit is written on it as measured (data-narrow) and read from what was measured as it is
  // drawn, before that from their words (timelineBarValuesWidth). Kept as the canvas's state, a bar made drew every
  // bar again once measured (2026-11-15: ~20 ms at CPU ×2 inside a new row's press).
  const valueWidths = useRef(new Map<string, number>());
  const narrowAt = useRef(new Map<string, (valuesWidth: number) => boolean>());
  useLayoutEffect(() => holdMeasure(barElements, () => {
    barElements.current.forEach((bar, rowId) => {
      const width = [...bar.querySelectorAll<HTMLElement>("[data-timeline-bar-values]")]
        .reduce((sum, values) => sum + (values.offsetWidth ? values.offsetWidth + 10 : 0), 0);
      valueWidths.current.set(rowId, width);
      const narrow = narrowAt.current.get(rowId)?.(width);
      if (narrow !== undefined && (bar.dataset.narrow === "true") !== narrow) {
        holdStyleWrite(() => { if (narrow) bar.dataset.narrow = "true"; else delete bar.dataset.narrow; });
      }
    });
  }), [props.items, props.barProperties, props.propertyValueContext, props.dayWidth, props.pageIcons, fontsVersion]);
  // One ref for each bar for as long as it is drawn (its bar not drawn again for a drawing of the canvas).
  const barRef = useKeyedRefs<string>((rowId, element) => {
    barElements.current.set(rowId, element);
    return () => { if (barElements.current.get(rowId) === element) barElements.current.delete(rowId); };
  });
  const measureBars = useCallback(() => {
    const lanes = lanesRef.current;
    if (!lanes) return;
    // Measured along the reading direction (DependencyArrows turns them over
    // where the screen reads from the right): its start, then its end.
    const base = lanes.getBoundingClientRect(), rtl = readsRightToLeft(lanes);
    const next: Record<string, BarRect> = {};
    barElements.current.forEach((element, rowId) => {
      const rect = element.getBoundingClientRect();
      next[rowId] = {
        left: rtl ? base.right - rect.right : rect.left - base.left,
        right: rtl ? base.right - rect.left : rect.right - base.left,
        midY: rect.top - base.top + rect.height / 2,
      };
    });
    setBarRects((current) => JSON.stringify(current) === JSON.stringify(next) ? current : next);
  }, []);
  const layoutSignature = props.items.map((item) => `${item.row.id}:${item.first}:${item.last}:${item.lane}`).join("|");
  useEffect(() => {
    if (props.dependencyPairs.length === 0 || typeof ResizeObserver === "undefined") return;
    const lanes = lanesRef.current;
    if (!lanes) return;
    const observer = new ResizeObserver(measureBars);
    observer.observe(lanes);
    barElements.current.forEach((element) => observer.observe(element));
    return () => observer.disconnect();
  }, [layoutSignature, measureBars, props.dayWidth, props.dependencyPairs.length]);

  // Scrolled with the view's other first measures (holdMeasure).
  useEffect(() => holdMeasure(autoScrollKeyRef, () => {
    const scroller = scrollRef.current;
    const focusIndex = props.focusIndex ?? props.todayIndex;
    if (!scroller || focusIndex < 0 || focusIndex >= props.rangeDays) return;
    const first = props.items.length ? Math.min(...props.items.map((item) => item.first)) : null;
    const key = props.viewportIdentity
      ? `${props.viewportIdentity}:${props.anchor.getTime()}:${props.dayWidth}`
      : `${dateKey(props.anchor)}:${props.dayWidth}:${first ?? "empty"}`;
    if (autoScrollKeyRef.current === key) return;
    autoScrollKeyRef.current = key;
    // Its page's mark (wide or boxed, usePageWideTable), measured with these,
    // is written first: it opens centred in the box it is drawn in.
    flushHeldStyleWrites();
    // Today opens with now in the middle of the box it is drawn in, as
    // Notion's (seen 2026-11-12 at 1440: now at 855 in a database's own page
    // at 270..1440, its table shut or open, and at 841.5 in a page's text at
    // 487.5..1195.5); another day with its column there.
    const target = props.focusOffset !== undefined ? props.focusOffset - scroller.clientWidth / 2
      : focusIndex === props.todayIndex && props.nowOffset !== undefined ? props.nowOffset - scroller.clientWidth / 2
      : focusIndex * props.dayWidth - (scroller.clientWidth - props.dayWidth) / 2;
    scrollInlineTo(scroller, target);
    held.placed(scroller, props.focusOffset !== undefined ? { offset: props.focusOffset, showTable: !!props.showTable } : null);
    // Its days placed where its box stands now, they keep their places should its page widen it after (above).
    const box = scroller.getBoundingClientRect();
    scrollerEdgeRef.current = readsRightToLeft(scroller) ? -box.right : box.left;
    reportShownDay();
  }), [props.anchor, props.dayWidth, props.items, props.rangeDays, props.todayIndex, props.focusIndex, props.focusOffset, props.viewportIdentity]);
  // Weekends a shade darker under the dates, two days in seven, as Notion's;
  // not across hours.
  const firstSaturday = props.days.length > 1 && props.days[1].getTime() - props.days[0].getTime() >= 86_400_000 - 3_600_000
    ? props.days.slice(0, 7).findIndex((day) => day.getDay() === 6) : -1;
  const weekends = firstSaturday >= 0 ? {
    "--timeline-week": `${7 * props.dayWidth}px`, "--timeline-weekend": `${2 * props.dayWidth}px`,
    "--timeline-weekend-at": `${firstSaturday * props.dayWidth}px`,
  } as CSSProperties : undefined;

  const arrows = useMemo(() => props.dependencyPairs.flatMap(([successorId, predecessorId]) => {
    const successor = barRects[successorId];
    const predecessor = barRects[predecessorId];
    return successor && predecessor ? [{
      key: `${predecessorId}->${successorId}`, predecessor: predecessorId, successor: successorId,
      x1: predecessor.right,
      y1: predecessor.midY,
      x2: successor.left,
      y2: successor.midY,
    }] : [];
  }), [barRects, props.dependencyPairs]);

  function openKey(item: TimelineRenderedRow, event: React.KeyboardEvent<HTMLElement>) {
    const e = event;
    if (e.key === "ContextMenu" || (e.shiftKey && e.key === "F10")) {
      event.preventDefault();
      event.stopPropagation();
      props.openRowContextMenuFromElement(item.row.id, e.currentTarget);
      return;
    }
    if (fieldEnter(event) || event.key === " ") {
      event.preventDefault(); props.openRow(item.row.id, event.metaKey || event.ctrlKey ? "new_tab" : "default");
      return;
    }
    // The arrow toward the days' end moves it later, the left one where they
    // run from the right.
    const step = e.shiftKey ? props.largeStep : props.smallStep ?? 1;
    const key = arrowAlongRow(e.key, e.currentTarget);
    if (key === "ArrowLeft") {
      event.preventDefault();
      props.shiftRowDate(item, -step);
    } else if (key === "ArrowRight") {
      event.preventDefault();
      props.shiftRowDate(item, step);
    }
  }

  // What every bar draws alike, and what its presses do by the canvas as last drawn (TimelineBar).
  // The values before the title in the view's order, then those after it; hidden (viewTitleFree), it shows last while no table names the rows.
  const found = props.barProperties.findIndex((property) => property.type === "title"), titleAt = found >= 0 || props.showTable ? found : props.barProperties.length;
  const barFrame = useSteadyFrame<TimelineBarFrame>({
    before: props.barProperties.slice(0, Math.max(0, titleAt)).filter((property) => property.type !== "title"),
    after: props.barProperties.slice(titleAt + 1).filter((property) => property.type !== "title"),
    pageIcons: props.pageIcons !== false, titled: titleAt >= 0, readOnly: props.readOnly, datesFixed: Boolean(props.datesFixed),
    resizes: Boolean(props.endProperty) && !props.readOnly && !props.datesFixed, dependencies: props.canonicalDependencyGraphActive && !props.readOnly, hourColumns: firstSaturday < 0 && props.days.length > 1 ? props.days[1].getTime() - props.days[0].getTime() : null,
    flattenedSubtasks: props.flattenedSubtasks, hiddenSubtasks: props.hiddenSubtasks,
  });
  const barWays = useLiveWays<TimelineBarWays>({
    begin: props.beginInteraction,
    pointerUp: () => { if (!props.suppressOpenRef.current) props.resetInteraction(); },
    key: openKey,
    click: (rowId, event) => { if (!props.suppressOpenRef.current) props.openRow(rowId, rowOpenModeOfClick(event)); },
    open: props.openRow,
    contextMenu: (rowId, event) => { props.resetInteraction(); props.openRowContextMenu(rowId, event); },
    dependencyStart: (rowId) => { dependencyDragSourceRef.current = rowId; setDependencySourceRowId(rowId); },
    dependencyToggle: (rowId) => setDependencySourceRowId((current) => current === rowId ? null : rowId),
    dependencyDrop: (rowId) => {
      const predecessorId = dependencyDragSourceRef.current;
      dependencyDragSourceRef.current = null;
      if (predecessorId) props.createDependency(rowId, predecessorId);
      setDependencySourceRowId(null);
    },
    dependencyPick: (rowId) => {
      if (dependencySourceRowId) props.createDependency(rowId, dependencySourceRowId);
      setDependencySourceRowId(null);
    },
    commitTitle: (row, value, byKey) => props.onCommitTitle?.(row, value, byKey),
    subitemControls: props.renderSubitemControls,
    t: props.t,
    titleOf: (rowId) => props.rowTitle(props.pagesById[rowId]),
    opens: () => !props.suppressOpenRef.current,
  });

  const nowShown = props.nowOffset !== undefined && props.todayIndex >= 0 && props.todayIndex < props.rangeDays;
  const range = useTimelineRangePill({ items: props.items, preview: props.preview, dayWidth: props.dayWidth, days: props.days, onTick: !!props.dayLabelOnTick });
  const newBarColumns = props.newBarColumns ?? 4, newBar = { dayWidth: props.dayWidth, rangeDays: props.rangeDays, columns: newBarColumns, cover: newBarColumns + (barFrame.hourColumns === null ? 1 : 0), label: props.t("timelineView:toolbar.createInCanvas") };
  return (
    <><div ref={scrollRef} className={styles.timelineScroll} onScroll={scrolled} data-timeline-canvas
      onPointerDownCapture={viewerMoves} onWheelCapture={viewerMoves} onKeyDownCapture={viewerMoves} onTouchStartCapture={viewerMoves}>
      <div className={styles.timelineCanvas} data-weekends={weekends ? "true" : undefined} style={{ width: props.windowWidth, minWidth: props.windowWidth, maxWidth: props.windowWidth, overflowX: "clip", ...weekends }}>
        {/* The line on the whole pixel it falls in, as Notion's (seen 2026-11-12 at 1440: at 854 or 855 for now at 855.0). */}
        {nowShown && <div className={styles.timelineToday} style={{ insetInlineStart: Math.floor(props.nowOffset!) }} />}
        {/* The months and days stay under the page's top as it scrolls past them, the dot of now with the days (useStickyViewHeads). */}
        <TimelineDayHeads days={props.days} monthSpans={props.monthSpans} gridTemplate={props.gridTemplate} dayLabel={props.dayLabel}
          dayTip={props.dayTip} dayLabelOnTick={props.dayLabelOnTick} lineColumns={props.lineColumns} todayIndex={props.todayIndex} dayWidth={props.dayWidth}
          nowOffset={nowShown ? props.nowOffset : undefined} nowLabel={nowShown ? props.nowLabel : undefined} wholeDays={firstSaturday >= 0} layer={barFrame.hourColumns === null ? range.layer : undefined} />
        <div
          ref={lanesRef} onPointerOver={range.over} onPointerLeave={range.leave}
          className={styles.timelineLanes} data-timeline-lanes
          data-grouped={grouped ? props.showTable ? "table" : "true" : undefined}
          style={{ gridTemplateColumns: props.gridTemplate, gridTemplateRows: props.laneHeights?.length ? props.laneHeights.map(height => `${height}px`).join(" ") : props.laneCount ? `repeat(${props.laneCount}, minmax(var(--timeline-lane), auto))` : undefined,
            // Only as tall as its lanes, a lone bar one line; empty, room to say so.
            minHeight: props.items.length || away.length || props.laneCount ? props.laneHeights?.reduce((sum, height) => sum + height, 0) ?? `calc(${props.laneCount ?? 0} * var(--timeline-lane))`
              // Nothing yet: "+ 새로 만들기" is its first line, as Notion's (seen 2026-11-03).
              : props.onCreateRow ? 0 : 170 }}
        >
          {/* Beside a table its rows hold the heads; the canvas keeps only each group's new bar lane. */}
          {grouped && props.lanes ? <TimelineGroupLanes groups={props.lanes.groups} addLanes={props.lanes.addLanes} table={props.showTable} newGroupLane={props.lanes.newGroupLane}
            onToggle={props.toggleGroup ?? (() => {})} actions={props.groupActions ?? {}} newBar={props.onCreateRow ? { ...newBar, onCreate: props.onCreateRow, onPoint: range.ghost } : undefined} /> : null}
          {props.lanes?.subitemElbows.map((elbow) => <span key={`elbow:${elbow.key}`} className={styles.timelineSubitemElbow} aria-hidden="true"
            style={{ gridColumn: elbow.first + 1, gridRow: `${elbow.parentLane + 2} / ${elbow.lane + 2}`, width: elbow.columns ? elbow.columns * props.dayWidth : 22 }} />)}
          {props.lanes?.subitemLinks.map((link) => <button key={`link:${link.row.id}`} type="button" className={styles.timelineSubitemLink}
            data-timeline-subitem-link={link.row.id} style={{ gridColumn: `${link.first + 1} / -1`, gridRow: link.lane + 1 }}
            onClick={(event) => props.openRow(link.row.id, rowOpenModeOfClick(event))}>{props.rowTitle(link.row)}</button>)}
          {props.onCreateSubitem ? props.lanes?.subitemAdds.map((add) => <TimelineSubitemGhostLane key={`add:${add.parentId}`} lane={add.lane}
            dayWidth={props.dayWidth} columns={props.subitemGhostColumns ?? 5} onCreate={(first) => props.onCreateSubitem?.(add.parentId, first)} />) : null}
          {props.onDateRow && props.undated?.length ? <TimelineUndatedLanes lanes={props.undated} newBar={newBar} rowTitle={props.rowTitle} onDate={props.onDateRow} onPoint={range.ghost} /> : null}
          <TimelineOffRangeMarkers scrollRef={scrollRef} barElements={barElements} items={props.items} away={away} onBringAway={props.onBringAway} layoutKey={`${layoutSignature}:${props.dayWidth}:${props.showTable}`}
            titleOf={(item) => props.rowTitle(item.row)} dateOf={(item) => {
              // Its dates, start → end, either side, as Notion's (seen 2026-11-13: "2026년 11월 14일 → 2026년 11월 18일").
              const dates = props.rowDateLabels?.(item.row) ?? { start: props.fullDateLabel(item.start), end: props.fullDateLabel(item.end) };
              return dates.end && dates.end !== dates.start ? `${dates.start} → ${dates.end}` : dates.start;
            }} />
          {props.items.map((shownItem) => {
            // A bar moved whole goes faint with the pointer from its own place, which it leaves empty (useTimelineRowReorder; Notion's, seen 2026-11-15 and
            // 2026-11-24); one stretched by an end is drawn as it is over the days it would take, that edge's day beside it (TimelineResizeEdge; seen 2026-11-24), the chosen bars it carries over theirs.
            const preview = props.preview?.rowId === shownItem.row.id ? props.preview : null, edge = preview?.mode === "resize-start" ? "start" : preview?.mode === "resize-end" ? "end" : null, along = props.preview?.along?.get(shownItem.row.id);
            const item = edge ? { ...shownItem, first: preview!.first, last: preview!.last, [edge]: props.days[edge === "start" ? preview!.first : preview!.last] ?? shownItem[edge] } : along ? { ...shownItem, ...along } : shownItem;
            const title = props.rowTitle(item.row);
            const dates = props.rowDateLabels?.(item.row) ?? { start: props.fullDateLabel(item.start), end: props.fullDateLabel(item.end) };
            // A bar with its sub-items' toggle starts with it and runs its
            // title on past its end, as Notion's (seen 2026-11-03).
            const icon = props.pageIcons !== false && barShowsPageIcon(item.row), room = (item.last - item.first + 1) * props.dayWidth;
            const narrowWith = (valuesWidth: number) => !item.hasChildren && !contentFits(titleAt >= 0 ? title : "", icon && titleAt >= 0, valuesWidth, room);
            narrowAt.current.set(item.row.id, narrowWith);
            const parent = props.flattenedSubtasks && item.row.subitemParentId ? props.pagesById[item.row.subitemParentId] : undefined;
            return <TimelineBar key={(item as TimelineBarLane).line ?? item.row.id} item={item} title={title} dateStart={dates.start} dateEnd={dates.end}
              narrow={narrowWith(valueWidths.current.get(item.row.id) ?? timelineBarValuesWidth(item.row, barFrame.before, barFrame.after))} moving={preview?.mode === "move" ? "whole" : edge ?? false}
              tint={props.barColor?.(item.row)?.color ?? undefined} parentTitle={parent ? props.rowTitle(parent) : ""}
              expanded={props.expandedRows?.has(item.row.id) ?? false} editingNew={props.newRowId === item.row.id}
              dependencyFrom={dependencySourceRowId} barRef={barRef(item.row.id)} frame={barFrame} context={props.propertyValueContext} ways={barWays} />;
          })}
          {props.dropLine ? <div className={styles.timelineDropLine} data-side={props.dropLine.side} style={{ gridRow: props.dropLine.lane + 1 }} aria-hidden="true" /> : null}
          <TimelineDependencyArrows arrows={arrows} markerId={props.arrowMarkerId}
            onDelete={props.canonicalDependencyGraphActive && !props.readOnly ? props.deleteDependency : undefined} />
          {/* With no bar at all its lanes stay empty to one who may not add, as Notion's (seen 2026-11-18 as a guest who may only read). */}
          {props.items.length === 0 && !away.length && !props.laneCount && !props.onCreateRow && props.earliestDate && (
            <div className={styles.timelineLanesEmpty}>
              <span>{props.t("timelineView:lanes.noItemsInRange")}</span><button type="button" onClick={props.jumpToEarliest}>{props.t("timelineView:lanes.jumpTo", { date: props.fullDateLabel(props.earliestDate) })}</button>
            </div>
          )}
        </div>
        {props.onCreateRow && !grouped && <TimelineCreateLane {...newBar} labelled={!props.showTable} onCreate={props.onCreateRow} onPoint={range.ghost} />}
      </div>
    </div><OwnPageViewBar scrollRef={scrollRef} onMove={viewerMoves} /></>
  );
}
