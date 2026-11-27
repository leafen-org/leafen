"use client";

import { useCommittedOperationScope } from "../product/useCommittedOperationScope";

import {
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent as ReactKeyboardEvent,
  type MouseEvent as ReactMouseEvent,
  type ReactNode,
} from "react";
import { useTranslation } from "react-i18next";
import type { GraphDataSource, GraphProperty, GraphRow, GraphView } from "../../contentGraph";
import { activeDateLocale } from "@/lib/i18n";
import { dataSourcePage } from "@/lib/dataSourcePage";
import { useKeyedRefs } from "@/lib/useKeyedRefs";
import { draggableByPointer, dropTargetByPointer } from "@/lib/pointerDrag";
import type { DbProperty, DbView, Page, ViewConfig } from "@/lib/types";
import { firstViewDateSource, isDateRollup, useDateRollupReads, viewDateSources, viewDateValue } from "./viewDateSource";
import { DatabaseViewControls } from "./DatabaseViewControls";
import { UndatedRowsMenu } from "./UndatedRowsMenu";
import type { PropertyValueReadContext } from "./PropValue";
import {
  projectCardViewSubitems,
  visibleViewProperties,
  viewFilterSeedValues,
  type QueryBounds,
  type ViewFilterSeedValues,
} from "./query";
import { addDays, addMonths, startOfMonth, dateKey, formatDateForProperty, formatTimeDraft, shiftDateValueToDay, shownDate, shownDateKey, shownEndDate } from "./dateUtils";
import {
  galleryPresentationProperty,
  type GalleryPagePresentation,
} from "./galleryViewModel";
import { pagesOver, presentedPagesById, rowPagesInOrder, type PagePresenting } from "./presentedPages";
import { conditionalColorResolver } from "./conditionalColorModel";
import { createListOperationCoordinator } from "./listViewModel";
import { useWeekStartDay, weekOrder } from "@/lib/displayPreferences";
import { calendarAnchorForLayout, calendarTitle, layoutCalendarWeek, projectCalendarRows } from "./calendarViewModel";
import { ChevronLeft, ChevronRight } from "../icons";
import styles from "./databaseSheet";
import { viewShowsPageIcons } from "./CardTitleIcon";
import { useDatabaseStructureLocked } from "./databaseStructureLock";
import { makeRowThatOpens } from "./newViewRow";
import { useViewOffersNewRows } from "./databaseArchivedRowsContext";
import { useRowDisplayTitle } from "./untitledRowName";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { PhoneCalendarMonth } from "./PhoneCalendarMonth";
import { useStickyViewHeads } from "./useStickyViewHeads";
import { DateViewMissing } from "./DateViewMissing";
import { useCalendarCells, useKeptCalendarAnchor } from "./useCalendarCells";
import { CalendarWeek, fullDateLabel, shortDateLabel, type CalendarCardLook, type CalendarWeekActions } from "./CalendarWeek";
import { arrowAlongRow } from "@/lib/readingDirection";
import { CardSelectionProvider, useCardSelection } from "./cardSelection";
import { copiesPlace, type CopiesPlace } from "./viewRowOrder";

const WEEKDAYS = ["sun", "mon", "tue", "wed", "thu", "fri", "sat"];
// A card is locked as its presentation says, else as its page is.
const CALENDAR_PAGES: PagePresenting = { finish: (page, presentation) => { page.isLocked = presentation?.isLocked ?? page.isLocked; } };

export type CalendarRowOpenMode = "default" | "new_tab" | "side" | "full";
export type CalendarPagePresentation = GalleryPagePresentation & { isLocked?: boolean };

export interface CalendarViewSnapshot {
  snapshotIdentity: string;
  contextIdentity: string;
  requestGeneration: number;
  workspaceId: string;
  contextPageId?: string;
  currentUserId?: string;
  dataSource: GraphDataSource;
  view: GraphView;
  properties: readonly GraphProperty[];
  rows: readonly GraphRow[];
  pagesById: Readonly<Record<string, GraphRow>>;
  pagePresentationById: Readonly<Record<string, CalendarPagePresentation>>;
  databaseFeatures?: Page["databaseFeatures"];
  propertyValueContext: PropertyValueReadContext;
  queryBounds: QueryBounds;
}

export interface CalendarViewOperationContext {
  contextIdentity: string;
  dataSourceId: string;
  requestGeneration: number;
  requestKey: string;
  signal: AbortSignal;
  snapshotIdentity: string;
  viewId: string;
}

export interface CalendarViewOperations {
  createDatePropertyAndSelect(name: string, context: CalendarViewOperationContext): Promise<void>;
  createDatedRow(dayKey: string, seeds: ViewFilterSeedValues, context: CalendarViewOperationContext): Promise<GraphRow | null>;
  isCurrent(context: CalendarViewOperationContext): boolean;
  moveRowDate(rowId: string, propertyId: string, value: unknown, context: CalendarViewOperationContext): Promise<void>;
  onError(error: unknown, context: CalendarViewOperationContext): void;
  openRow(rowId: string, context: CalendarViewOperationContext): void;
  openRowIn(rowId: string, mode: CalendarRowOpenMode, context: CalendarViewOperationContext): void;
  openRowContextMenu(rowId: string, event: ReactMouseEvent<HTMLElement>, duplicate?: () => void): void;
  openRowContextMenuFromElement(rowId: string, element: HTMLElement, duplicate?: () => void): void;
  selectDateProperty(propertyId: string, context: CalendarViewOperationContext): Promise<void>;
  rowContextMenu: ReactNode;
  /** Delete on chosen cards. */
  trashRows?(rowIds: readonly string[]): Promise<void>;
  /** ⌘D on chosen cards: copies them and gives back their copies. */
  duplicateRows?(rowIds: readonly string[], place?: CopiesPlace): Promise<readonly string[] | void>;
}

export function CalendarView({
  snapshot,
  operations,
  readOnly = false,
}: {
  snapshot: CalendarViewSnapshot;
  operations: CalendarViewOperations;
  readOnly?: boolean;
}) {
  const { t } = useTranslation(["calendarView", "common"]);
  const displayTitle = useRowDisplayTitle();
  const { phone } = useSidebarLayout(true);
  // A locked database keeps its rows' dates editable but not its view and properties.
  const structureLocked = useDatabaseStructureLocked();
  // A view showing its archived rows makes no page.
  const creates = !readOnly && useViewOffersNewRows();
  // It opens on the day it last showed in this browser, else on today: its month, or the week it falls in (Notion's, seen 2026-11-11).
  const [anchor, setAnchor] = useKeptCalendarAnchor(snapshot.currentUserId, snapshot.view.id);
  const [draggingRowId, setDraggingRowId] = useState<string | null>(null);
  const [dragOverDay, setDragOverDay] = useState<string | null>(null);
  const anchorIdentityRef = useRef("");
  const coordinator = useMemo(() => createListOperationCoordinator(), []);
  const lifecycleController = useCommittedOperationScope([
    snapshot.contextIdentity,
    snapshot.requestGeneration,
    snapshot.snapshotIdentity,
  ]);
  const baseContext = useMemo<CalendarViewOperationContext>(() => ({
    contextIdentity: snapshot.contextIdentity,
    dataSourceId: snapshot.dataSource.id,
    requestGeneration: snapshot.requestGeneration,
    requestKey: "calendar",
    signal: lifecycleController.signal,
    snapshotIdentity: snapshot.snapshotIdentity,
    viewId: snapshot.view.id,
  }), [lifecycleController, snapshot]);
  const operationContext = (requestKey: string): CalendarViewOperationContext => ({
    ...baseContext,
    requestKey,
  });

  const props = useMemo(() => snapshot.properties.map(galleryPresentationProperty), [snapshot.properties]);
  const titleProperty = snapshot.properties.find((property) => property.type === "title");
  // Its rows' pages, made again for the rows changed alone (presentedPages.ts).
  const pagesById = useMemo(() => presentedPagesById({ pagesById: snapshot.pagesById, rows: snapshot.rows, workspaceId: snapshot.workspaceId,
    pagePresentationById: snapshot.pagePresentationById }, titleProperty, CALENDAR_PAGES),
  [snapshot.pagePresentationById, snapshot.pagesById, snapshot.rows, snapshot.workspaceId, titleProperty]);
  const rows = useMemo(
    () => rowPagesInOrder(snapshot.rows, pagesById) as Page[] | undefined ?? snapshot.rows.map((row) => pagesById[row.id]).filter(Boolean),
    [pagesById, snapshot.rows],
  );
  const view = useMemo<DbView>(() => ({
    id: snapshot.view.id,
    databaseId: snapshot.view.dataSourceId,
    name: snapshot.view.name,
    type: snapshot.view.type as DbView["type"],
    config: snapshot.view.config as ViewConfig,
    position: 0,
  }), [snapshot.view]);
  const cardColor = useMemo(() => conditionalColorResolver(view.config as Record<string, unknown>, props, {
    pagesById, currentPageId: snapshot.contextPageId, currentUserId: snapshot.currentUserId,
  }, snapshot.queryBounds), [pagesById, props, snapshot.contextPageId, snapshot.currentUserId, snapshot.queryBounds, view.config]);
  const database = useMemo(() => dataSourcePage(snapshot.dataSource, snapshot.workspaceId, 0, { databaseFeatures: snapshot.databaseFeatures }),
    [snapshot.dataSource, snapshot.databaseFeatures, snapshot.workspaceId]);
  const presentation = useMemo(
    () => projectCardViewSubitems(database, view, rows, snapshot.queryBounds),
    [database, rows, snapshot.queryBounds, view],
  );
  const propertyValueContext = useMemo<PropertyValueReadContext>(() => ({
    ...snapshot.propertyValueContext,
    contextIdentity: snapshot.contextIdentity,
    // The related pages the view has read stay beside its own cards.
    pagesById: pagesOver(snapshot.propertyValueContext.pagesById, pagesById),
    propsByDatabaseId: {
      ...snapshot.propertyValueContext.propsByDatabaseId,
      [snapshot.dataSource.id]: props,
    },
    snapshotIdentity: snapshot.snapshotIdentity,
  }), [pagesById, props, snapshot.contextIdentity, snapshot.dataSource.id, snapshot.propertyValueContext, snapshot.snapshotIdentity]);
  const dateProps = viewDateSources(props);
  const dateProp = dateProps.find((property) => property.id === view.config?.calendarBy) ?? firstViewDateSource(dateProps);
  // A rollup's dates are its related pages': its cards stay where those put
  // them and its days make no page (see viewDateSource.ts).
  const datesFixed = isDateRollup(dateProp);
  useDateRollupReads(dateProp, propertyValueContext);
  const dateContext = useMemo(() => ({
    properties: props, pagesById: propertyValueContext.pagesById as Record<string, Page>,
    propertiesByDatabaseId: propertyValueContext.propsByDatabaseId as Record<string, DbProperty[]>,
    bounds: { formula: snapshot.queryBounds.formula, projection: snapshot.queryBounds.formula.rollup },
  }), [propertyValueContext, props, snapshot.queryBounds]);
  const dateOf = useCallback((row: Page) => dateProp ? viewDateValue(row, dateProp, dateContext) : undefined, [dateContext, dateProp]);
  // A timed date's time at the end of its card's name line, in the date's
  // own time format: its start's on the card that starts it, and its end's
  // after "→" where that card also ends it on another day; a week's card that
  // goes on from the week before shows none (Notion's, seen 2026-11-11:
  // "오전 9:00", "오전 9:00 → 오후 6:30", over two weeks "오전 9:00" and none).
  const timeFormat = dateProp?.config?.timeFormat;
  const cardTime = useCallback((row: Page, continuesBefore: boolean, continuesAfter: boolean) => {
    const value = dateOf(row), start = shownDate(value), end = shownEndDate(value);
    if (continuesBefore || !start?.time || timeFormat === "hidden") return "";
    const clock = (time: string) => formatTimeDraft(time, timeFormat);
    return !continuesAfter && end?.time && dateKey(end.date) !== dateKey(start.date) ? `${clock(start.time)} → ${clock(end.time)}` : clock(start.time);
  }, [dateOf, timeFormat]);
  // Weekends show unless the view hides them.
  const showWeekends = view.config?.showWeekends !== false;
  const calendarLayout = view.config?.calendarLayout ?? "month";
  // "페이지 아이콘 표시" and "페이지 제목 줄바꿈", as Notion's calendar (seen 2026-10-05).
  const pageIcons = viewShowsPageIcons(view.config ?? {});
  const wrapTitles = view.config?.calendarWrapTitles === true;
  const anchorIdentity = `${snapshot.dataSource.id}:${snapshot.view.id}:${dateProp?.id ?? ""}`;
  useEffect(() => {
    if (anchorIdentityRef.current === anchorIdentity) return;
    // Another source, view or date goes back to today; the first keeps the day it opened on (useKeptCalendarAnchor).
    if (anchorIdentityRef.current) setAnchor(new Date());
    anchorIdentityRef.current = anchorIdentity;
  }, [anchorIdentity]);
  const weekStart = useWeekStartDay();
  // A changed unit goes where Notion's goes, in the same frame (calendarAnchorForLayout).
  const [anchorLayout, setAnchorLayout] = useState(calendarLayout);
  if (anchorLayout !== calendarLayout) {
    setAnchorLayout(calendarLayout);
    setAnchor((current) => calendarAnchorForLayout(current, calendarLayout, weekStart));
  }
  const gridRef = useRef<HTMLDivElement>(null), { cells, ownPage } = useCalendarCells(anchor, calendarLayout, weekStart, gridRef);
  // Its date shows on the cards too while the view shows it, as Notion's (seen 2026-11-06).
  const metaProps = useMemo(() => visibleViewProperties(props, view, snapshot.queryBounds).filter((property) => property.type !== "title"),
    [props, snapshot.queryBounds, view]);
  const dateAdapters = useMemo(() => ({
    // A date sits on the day and time it shows for the viewer, as Notion's calendar places it.
    dateKey: (row: Page) => dateProp ? shownDateKey(dateOf(row)) : "",
    endKey: (row: Page) => { const end = dateProp ? shownEndDate(dateOf(row)) : null; return end ? dateKey(end.date) : ""; },
    time: (row: Page) => dateProp ? shownDate(dateOf(row))?.time ?? "" : "",
    title: (row: Page) => displayTitle(row),
  }), [dateOf, dateProp, displayTitle]);
  const projected = useMemo(() => projectCalendarRows<Page>(presentation.rows, dateAdapters), [dateAdapters, presentation.rows]);
  const weeks = useMemo(() => Array.from({ length: Math.ceil(cells.length / 7) }, (_, index) => {
    const days = cells.slice(index * 7, index * 7 + 7).filter((day) => showWeekends || (day.getDay() !== 0 && day.getDay() !== 6));
    return { days, cards: layoutCalendarWeek(days.map(dateKey), presentation.rows, dateAdapters) };
  }), [cells, dateAdapters, presentation.rows, showWeekends]);
  // Each week's lines as tall as their tallest card, so a range's bar and the
  // days' own cards line up across the week; measured again as cards resize.
  const [laneHeights, setLaneHeights] = useState<Record<string, number[]>>({});
  // A resize draws it again, and its lines are measured after (below).
  const [, setResized] = useState(0);
  useEffect(() => {
    const grid = gridRef.current;
    if (!grid || typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(() => setResized((count) => count + 1));
    observer.observe(grid);
    return () => observer.disconnect();
  }, []);
  // Its month row and weekdays stay at the page's top as it scrolls (useStickyViewHeads).
  useStickyViewHeads(gridRef, { shape: phone });
  useLayoutEffect(() => {
    const next: Record<string, number[]> = {};
    for (const card of gridRef.current?.querySelectorAll<HTMLElement>("[data-calendar-lane]") ?? []) {
      const lanes = (next[card.dataset.calendarWeek ?? ""] ??= []), lane = Number(card.dataset.calendarLane);
      // Its own content and 2px over and under, not the line's height it was stretched to.
      // A card is 28px at least, a name alone 21px on it (Notion's, seen 2026-11-06).
      // A chosen card's halo lies over it and takes no room of its own.
      const content = Math.max(28, [...card.children].reduce((sum, child) => sum
        + ((child as HTMLElement).dataset.cardChosen === undefined ? (child as HTMLElement).offsetHeight : 0), 4));
      lanes[lane] = Math.max(lanes[lane] ?? 0, content);
    }
    setLaneHeights((current) => JSON.stringify(current) === JSON.stringify(next) ? current : next);
  // After every drawing, as a card's lines may change with what it is drawn with (a value, a name, the language).
  });
  // A line not measured yet is a card's height: its name and a line for each
  // shown value. A week keeps one line however few its cards, so a week with
  // none is as tall as one with a card (Notion's, seen 2026-11-11: 140px with
  // three values shown, 124 at least).
  const cardLine = 28 + 24 * metaProps.length;
  // Its cards are chosen as a board's are (cardSelection.tsx): a drag from its
  // days' empty room chooses those it touches, and a right press chooses the
  // card it opens the menu of (Notion's, seen 2026-11-12).
  const selection = useCardSelection({ keep: snapshot.view.id, onMenu: operations.openRowContextMenuFromElement, onOpen: (rowId) => openRow(rowId),
    room: '[role="gridcell"], [role="row"]', marqueeIn: '[role="grid"]',
    onTrash: readOnly || !operations.trashRows ? undefined : (ids) => void operations.trashRows?.(ids).catch((error) => operations.onError(error, operationContext("trash"))),
    onDuplicate: readOnly || !operations.duplicateRows ? undefined : (ids) => {
      const shown = presentation.rows.map(({ id }) => id), { rowIds, place } = copiesPlace([shown], ids, shown);
      return operations.duplicateRows!(rowIds, place).catch((error) => { operations.onError(error, operationContext("duplicate")); });
    } });
  // What its weeks draw their cards with, and what their days and cards do: the
  // same functions for its life, reading its latest state (CalendarWeek).
  const look = useMemo<CalendarCardLook>(() => ({ metaProps, presentation, pagesById, propertyValueContext, cardColor, pageIcons, cardTime }),
    [cardColor, cardTime, metaProps, pageIcons, pagesById, presentation, propertyValueContext]);
  const acting = useRef<CalendarWeekActions | null>(null);
  const act = useMemo<CalendarWeekActions>(() => ({
    dayRef: (key) => acting.current!.dayRef(key), cardRef: (rowId) => acting.current!.cardRef(rowId),
    addOn: (day) => acting.current!.addOn(day), openRow: (rowId, mode) => acting.current!.openRow(rowId, mode),
    cardKey: (row, event) => acting.current!.cardKey(row, event), cardMenu: (rowId, event) => acting.current!.cardMenu(rowId, event),
  }), []);

  // A press that makes something runs once; a write (`write`) is never refused for another of its key still saving (listViewModel.ts).
  async function runOperation<Result>(requestKey: string, perform: (context: CalendarViewOperationContext) => Promise<Result>,
    publish: (result: Result, context: CalendarViewOperationContext) => void = () => {}, write = false) {
    const handlers = { perform, publish, isCurrent: (owner: CalendarViewOperationContext) => operations.isCurrent(owner),
      onError: (error: unknown, context: CalendarViewOperationContext) => operations.onError(error, context) };
    await (write ? coordinator.write(operationContext(requestKey), handlers) : coordinator.run(operationContext(requestKey), handlers));
  }

  function openRow(rowId: string, mode: CalendarRowOpenMode = "default") {
    const context = operationContext(`open:${rowId}`);
    if (!operations.isCurrent(context) || context.signal.aborted) return;
    if (mode === "default") operations.openRow(rowId, context);
    else operations.openRowIn(rowId, mode, context);
  }

  function openRowKey(rowId: string, e: ReactKeyboardEvent<HTMLElement>) {
    if (e.key === "ContextMenu" || (e.shiftKey && e.key === "F10")) {
      e.preventDefault();
      e.stopPropagation();
      operations.openRowContextMenuFromElement(rowId, e.currentTarget, selection.copyHere(e.currentTarget, rowId));
    }
  }

  function moveRowToDay(row: Page, day: Date) {
    if (readOnly || datesFixed || row.isLocked || !dateProp) return;
    const value = shiftDateValueToDay(dateOf(row), day);
    void runOperation(`move:${row.id}`, (context) => operations.moveRowDate(row.id, dateProp.id, value, context), undefined, true);
    setDraggingRowId(null);
    setDragOverDay(null);
  }

  function cardKey(row: Page, e: ReactKeyboardEvent<HTMLElement>) {
    openRowKey(row.id, e);
    const key = arrowAlongRow(e.key, e.currentTarget);
    const delta = key === "ArrowRight" || key === "ArrowDown" ? 1 : key === "ArrowLeft" || key === "ArrowUp" ? -1 : 0;
    if (!delta || readOnly || datesFixed || row.isLocked) return;
    const start = shownDate(dateOf(row))?.date;
    if (!start) return;
    e.preventDefault();
    moveRowToDay(row, addDays(start, delta));
  }

  // A card's drag within this calendar (a mouse's by the pointer's events,
  // pointerDrag); the day it is dropped on takes the row's date, and the card dropped is chosen and stays chosen through ⌘Z, as a board's (Notion's, seen 2026-11-17).
  const calendarId = useId();
  const dragKind = `calendar-card:${calendarId}`;
  const latest = useRef({ readOnly: readOnly || datesFixed, pagesById, cells, moveRowToDay, choose: selection.choose });
  latest.current = { readOnly: readOnly || datesFixed, pagesById, cells, moveRowToDay, choose: selection.choose };
  const movable = (rowId: string) => !latest.current.readOnly && !latest.current.pagesById[rowId]?.isLocked;
  const cardElements = useRef(new Map<string, HTMLElement>());
  useLayoutEffect(() => {
    for (const [rowId, element] of cardElements.current) element.draggable = movable(rowId);
  });
  const cardRef = useKeyedRefs<string>((rowId, element) => {
    cardElements.current.set(rowId, element);
    const cleanup = draggableByPointer({
      element,
      canDrag: () => movable(rowId),
      getInitialData: () => ({ kind: dragKind, id: rowId }),
      onDragStart: () => setDraggingRowId(rowId),
      onDrop: () => { setDraggingRowId(null); setDragOverDay(null); },
    });
    element.draggable = movable(rowId);
    return () => { cleanup(); if (cardElements.current.get(rowId) === element) cardElements.current.delete(rowId); };
  });
  const dayRef = useKeyedRefs<string>((key, element) => dropTargetByPointer({
    element,
    canDrop: ({ source }) => source.data.kind === dragKind && !latest.current.readOnly,
    onDragEnter: () => setDragOverDay(key),
    onDragLeave: () => setDragOverDay((current) => current === key ? null : current),
    onDrop: ({ source }) => {
      setDragOverDay(null);
      const row = latest.current.pagesById[String(source.data.id)];
      const day = latest.current.cells.find((cell) => dateKey(cell) === key);
      if (row && day) { latest.current.choose(row.id); latest.current.moveRowToDay(row, day); }
    },
  }));

  acting.current = { dayRef, cardRef, addOn, openRow, cardKey,
    cardMenu: (rowId, event) => { selection.choose(rowId); operations.openRowContextMenu(rowId, event, selection.copyHere(event.currentTarget, rowId)); } };

  function addOn(day: Date) {
    if (readOnly || datesFixed || !dateProp) return;
    const seeds = viewFilterSeedValues(
      props,
      view,
      [dateProp.id],
      { currentPageId: snapshot.contextPageId, currentUserId: snapshot.currentUserId },
      snapshot.queryBounds,
    );
    // Its page opens however the view changed as the row was made (newViewRow.ts: in a template's body it leaves the view).
    const dayValue = dateKey(day), made: { row: GraphRow | null } = { row: null };
    void makeRowThatOpens(() => runOperation(`create:${dayValue}`, async (context) => { made.row = await operations.createDatedRow(dayValue, seeds, context); }).then(() => made.row),
      (rowId) => operations.openRow(rowId, operationContext(`open:${rowId}`)));
  }

  // On its database's own page a month shown by ‹, › or 오늘 is shown from the
  // page's top, title and toolbar over its first week; inline, the page stays
  // where it was (Notion's, seen 2026-11-12 and 2026-11-15 from 300 and 872
  // down; inline seen 2026-11-15 under thirty lines).
  function showMonth(next: (current: Date) => Date) {
    setAnchor(next);
    if (ownPage && calendarLayout === "month") gridRef.current?.closest("[data-page-scroll]")?.scrollTo({ top: 0 });
  }
  function moveAnchor(delta: number) {
    showMonth((current) => calendarLayout === "week"
      ? addDays(current, delta * 7)
      : startOfMonth(addMonths(current, delta)));
  }

  if (!dateProp) {
    return <DateViewMissing view="calendarView" onAdd={!readOnly && !structureLocked ? () => void runOperation(
      "create-date-property",
      (context) => operations.createDatePropertyAndSelect(t("calendarView:property.dateDefaultName"), context),
    ) : undefined} />;
  }

  const title = calendarTitle(anchor, calendarLayout, weekStart, weeks[0]?.days ?? [], activeDateLocale());
  const shownMonth = calendarLayout === "month" ? `${anchor.getFullYear()}-${anchor.getMonth()}` : null;
  const todayKey = dateKey(new Date());
  const weekdayKeys = weekOrder(WEEKDAYS, weekStart).filter((day) => showWeekends || (day !== "sun" && day !== "sat"));

  // A phone draws Notion's small month of dots, its days opening their items
  // (PhoneCalendarMonth), a week under its month's name too; its toolbar has
  // no "날짜 없음", as Notion's (seen 2026-11-04). A day's items go in the
  // order they start, one without a time at its day's start, in the view's
  // order where they start together; only a timed one shows its date: its
  // time alone, "start-end" within the day, the whole range over days
  // (Notion's, seen 2026-11-12 at 402pt).
  if (phone) {
    const startOf = (row: Page) => { const start = shownDate(dateOf(row)); return start ? `${dateKey(start.date)} ${start.time || "00:00"}` : ""; };
    const rowsOn = (day: Date) => {
      const key = dateKey(day);
      return presentation.rows.filter((row) => {
        const start = dateAdapters.dateKey(row);
        return Boolean(start) && start <= key && key <= (dateAdapters.endKey(row) || start);
      }).sort((a, b) => startOf(a).localeCompare(startOf(b)));
    };
    const rowDate = (row: Page) => {
      const value = dateOf(row), start = shownDate(value), end = shownEndDate(value), format = dateProp?.config?.timeFormat;
      if (!start?.time) return null;
      if (end && dateKey(end.date) !== dateKey(start.date)) return formatDateForProperty(value, dateProp as never);
      if (format === "hidden") return null;
      const clock = (time: string) => formatTimeDraft(time, format);
      return end?.time ? `${clock(start.time)}-${clock(end.time)}` : clock(start.time);
    };
    return (
      <div className={styles.calendar} data-layout={calendarLayout}>
        <PhoneCalendarMonth title={title} weekdays={weekdayKeys.map((day) => t(`calendarView:weekdays.${day}`))} weeks={weeks}
          month={calendarLayout === "month" ? anchor.getMonth() : null}
          previousLabel={t(calendarLayout === "week" ? "calendarView:toolbar.previousWeek" : "calendarView:toolbar.previousMonth")}
          nextLabel={t(calendarLayout === "week" ? "calendarView:toolbar.nextWeek" : "calendarView:toolbar.nextMonth")}
          rowsOn={rowsOn} rowTitle={displayTitle} dayLabel={shortDateLabel} fullDayLabel={fullDateLabel}
          rowDate={rowDate}
          onMove={moveAnchor} onToday={() => setAnchor(new Date())} onOpenRow={(rowId) => openRow(rowId)}
          onAdd={creates && !datesFixed ? addOn : undefined} />
        {operations.rowContextMenu}
      </div>
    );
  }

  return (
    <CardSelectionProvider value={selection.value}><div className={styles.calendar} data-layout={calendarLayout} data-wrap-titles={wrapTitles ? "true" : undefined}
      data-card-dragging={draggingRowId ? "true" : undefined}>
      <div className={styles.calendarToolbar} data-sticky-head="">
        <div className={styles.calendarTitle}>{title}</div>
        <div className={styles.calendarControls}>
          <DatabaseViewControls><UndatedRowsMenu key={anchorIdentity} rows={projected.undated} readOnly={readOnly || datesFixed}
            onAssignToday={row => moveRowToDay(row, new Date())} onOpen={openRow} rowRef={cardRef} /></DatabaseViewControls>
          {/* ‹ 오늘 ›, as Notion's (seen 2026-10-07), its arrows 20px. */}
          <button type="button" className={styles.calendarNavButton} aria-label={t(calendarLayout === "week" ? "calendarView:toolbar.previousWeek" : "calendarView:toolbar.previousMonth")} onClick={() => moveAnchor(-1)}>
            <ChevronLeft size="regular" />
          </button>
          <button type="button" className={styles.calendarTodayButton} onClick={() => showMonth(() => new Date())}>
            {t("calendarView:toolbar.today")}
          </button>
          <button type="button" className={styles.calendarNavButton} aria-label={t(calendarLayout === "week" ? "calendarView:toolbar.nextWeek" : "calendarView:toolbar.nextMonth")} onClick={() => moveAnchor(1)}>
            <ChevronRight size="regular" />
          </button>
        </div>
      </div>
      <div ref={gridRef} className={styles.calendarGrid} role="grid" aria-label={t("calendarView:gridAriaLabel", { title })} aria-colcount={showWeekends ? 7 : 5} aria-rowcount={Math.ceil(cells.length / 7) + 1}
        data-weekends={showWeekends ? undefined : "hidden"}>
        <div className={styles.calendarGridWeek} role="row">
          {weekdayKeys.map((day) => <div key={day} className={styles.calendarWeekday} role="columnheader" data-sticky-head="">{t(`calendarView:weekdays.${day}`)}</div>)}
        </div>
        {weeks.map(({ days, cards }, weekIndex) => (
          <CalendarWeek key={`week-${weekIndex}`} weekIndex={weekIndex} days={days} cards={cards} heights={laneHeights[weekIndex]} cardLine={cardLine}
            shownMonth={shownMonth} todayKey={todayKey} adds={creates && !datesFixed} look={look} act={act}
            dragOverKey={dragOverDay && days.some((day) => dateKey(day) === dragOverDay) ? dragOverDay : null}
            draggingRowId={draggingRowId && cards.some((card) => card.row.id === draggingRowId) ? draggingRowId : null} />
        ))}
      </div>
      {operations.rowContextMenu}
    </div></CardSelectionProvider>
  );
}
