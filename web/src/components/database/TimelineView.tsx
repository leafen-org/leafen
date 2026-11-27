"use client";

import { useCommittedOperationScope } from "../product/useCommittedOperationScope";

import {
  useCallback,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type MouseEvent as ReactMouseEvent,
  type PointerEvent as ReactPointerEvent,
  type ReactNode,
} from "react";
import { useTranslation } from "react-i18next";
import type { GraphDataSource, GraphProperty, GraphRow, GraphView } from "../../contentGraph";
import { activeDateLocale } from "@/lib/i18n";
import type { DbProperty, Page, ViewConfig } from "@/lib/types";
import { firstViewDateSource, isDateRollup, useDateRollupReads, viewDateSources, viewDateValue } from "./viewDateSource";
import type { DatabaseDependencyGraphEdge } from "@/databaseQueryWire.generated";
import { timelineDependencyEdits } from "./timelineDependencyEdits";
import type { PropertyValueReadContext } from "./PropValue";
import type { PropertyCellEnvironment } from "./propertyCellTypes";
import { createListOperationCoordinator, projectListHierarchy } from "./listViewModel";
import { galleryPresentationProperty, type GalleryPagePresentation } from "./galleryViewModel";
import { pagesOver, presentedPagesById } from "./presentedPages";
import { subitemFilterScope } from "./subitemFilterScope";
import { projectRows, useDeferredPlacing } from "./viewRowProjection";
import { tableRowWindow, visibleViewProperties, type QueryBounds, type ViewFilterSeedValues } from "./query";
import { dateKey, shownEndDate } from "./dateUtils";
import { timelineDatedValues, timelinePlacedValues } from "./timelineDatedValues";
import { timelineAlongColumns, timelineStretchedAlong, timelineStretchValues } from "./timelineStretchAlong";
import { projectTimelineDependencyPairs, timelineInteractionPreview, type TimelineInteractionMode } from "./timelineInteractionModel";
import { rememberedTimelineCenter, rememberTimelineCenter } from "./timelineRememberedCenter";
import { projectTimelineRows, TIMELINE_ZOOMS, timelineAnchorFor, timelineGridTemplate, timelineColumn, timelineColumnStart, timelineDateText, timelineDayLabel, timelineDayLabelsOnTick, timelineHeadingDay, timelineHeadingLabel, timelineHourHead, timelineHourSpan, timelineLineColumns, timelineNewBarColumns, timelineRange, timelineShiftedAnchor, timelineTimeAt, timelineTopSpans, timelineZoomOf, type TimelineZoom } from "./timelineViewModel";
import { resizeTimelineTableProperty, timelineTableColumns } from "./timelineTableModel";
import { conditionalColorResolver } from "./conditionalColorModel";
import { TimelineCanvas, type TimelineCanvasPreview } from "./TimelineCanvas";
import { TimelineSubitemControls, TimelineTableRows, type TimelineRenderedRow, type TimelineRowOpenMode } from "./TimelineRows";
import { ChevronLeft, ChevronRight, DoubleChevronLeft, DoubleChevronRight } from "../icons";
import { usePageWideTable } from "./usePageWideTable";
import { useTimelineNow } from "./useTimelineNow";
import { useStickyViewHeads } from "./useStickyViewHeads";
import styles from "./databaseSheet";
import { useDatabaseStructureLocked } from "./databaseStructureLock";
import { useColumnHeadsReadOnly } from "./databaseRowsPreview";
import { viewShowsPageIcons } from "./CardTitleIcon";
import { NotionSelect } from "./NotionSelect";
import { DatabaseViewControls } from "./DatabaseViewControls";
import { UndatedRowsMenu } from "./UndatedRowsMenu";
import { TableLoadMoreRow } from "./TableLoadMoreRow"; import { useListGroupLoads } from "./ListGroupLoads";
import { dateTimeValue, endMoment, moment, shownDay } from "./timelineDateValues";
import { useUndatedTimelineDrag } from "./useUndatedTimelineDrag";
import { listenTimelineBarDrag } from "./timelineBarDrag";
import { projectTimelineLanes, timelineGroupLines } from "./timelineLaneProjection";
import { useTimelineLaneHeights } from "./useTimelineLaneHeights";
import { useTimelineFirstLanes } from "./useTimelineFirstLanes";
import { timelineTableCover } from "./timelineShownEdge";
import { timelineGroupAddKind, useTimelineGroupActions } from "./useTimelineGroupActions";
import { groupMoveValues } from "./viewGroupMove";
import { timelinePresentationView, timelineRelationEdges, timelineRowLanes } from "./timelineViewValues";
import { carriedAlongValues, useTimelineRowReorder } from "./useTimelineRowReorder"; import { useTimelineRowsMade } from "./useTimelineRowsMade"; import { timelineTableCells } from "./useTimelineTableRange";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { useViewOffersNewRows } from "./databaseArchivedRowsContext";
import { useRowDisplayTitle } from "./untitledRowName";
import { useCollapsedGroups } from "./useCollapsedGroups";
import { DateViewMissing } from "./DateViewMissing";
import { inlineSign } from "@/lib/readingDirection";
import { CardSelectionProvider, useCardSelection } from "./cardSelection";
import { useTimelineNewBarChoice } from "./useTimelineNewBarChoice";
import { copiesPlace, type CopiesPlace } from "./viewRowOrder";
import { TableSummaryRow } from "./TableSummaryRow";
import { tableConfigAfterCalculation } from "./tablePropertyMenuModel";

export interface TimelineViewSnapshot {
  /** Whether the database has its sub-items on. */
  subitemsOn?: boolean;
  snapshotIdentity: string;
  dependencySnapshotIdentity: string;
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
  /** Whether its rows are every row the view has read, its bars' pages made from them alone (presentedPages.ts). */
  rowsAreAll?: boolean;
  pagePresentationById: Readonly<Record<string, GalleryPagePresentation>>;
  propertyValueContext: PropertyValueReadContext;
  /** Edits table panel cells in place when present. */
  propertyCellEnvironment?: PropertyCellEnvironment;
  queryBounds: QueryBounds;
  dependencyEdges: readonly DatabaseDependencyGraphEdge[];
  dependencyPropertyId?: string;
  dependencyRevision: number;
  dependencyNextCursor: string | null;
  dependencyHasMore: boolean;
  /** An inline timeline's first rows: how many it shows now, of the first `limit`, with those made here. */
  rowWindow?: { limit: number; count: number; made: ReadonlySet<string>; /** Grouped: each group shows its own first rows (useListGroupLoads). */ perGroup?: boolean };
}

export interface TimelineViewOperationContext {
  contextIdentity: string;
  dataSourceId: string;
  dependencySnapshotIdentity: string;
  dependencyVisibleIdentity: string;
  requestGeneration: number;
  requestKey: string;
  signal: AbortSignal;
  snapshotIdentity: string;
  viewId: string;
}

export interface TimelineDependencyPage {
  dependencySnapshotIdentity: string;
  edges: readonly DatabaseDependencyGraphEdge[];
  hasMore: boolean;
  nextCursor: string | null;
}

export interface TimelineViewOperations {
  /** alongside: what the press changes with the row, run as it shows so both draw together. */
  createDatedRow(dayKey: string, seeds: ViewFilterSeedValues, context: TimelineViewOperationContext, alongside?: (row: GraphRow) => void): Promise<GraphRow | null>;
  createSubitem?(parentId: string, values: Readonly<Record<string, unknown>>, context: TimelineViewOperationContext): Promise<GraphRow | null>;
  /** A row with no date beside another (a table row's "+"), made as a table's; `discardRow` takes one left unnamed away; `asking`, a sorted view asks whether its sort goes. */
  createRowBeside?(beside: { id: string; side: "before" | "after" }, seeds: ViewFilterSeedValues, context: TimelineViewOperationContext, alongside?: (row: GraphRow, asking?: boolean) => void): Promise<GraphRow | null>;
  discardRow?(rowId: string): void; /** A paste's rows its table fills and those it makes past them after a row, no date, one step of ⌘Z (TableViewOperations pasteRows). */
  pasteRows?(changed: readonly GraphRow[], made: { after: string; seeds: Readonly<Record<string, unknown>>; rows: readonly GraphRow["values"][] }, context: TimelineViewOperationContext): Promise<void>;
  createDatePropertyAndSelect(name: string, context: TimelineViewOperationContext): Promise<void>;
  /** A person's change to the view (`step`) is a step of the page's ⌘Z; one written for another change is not. */
  updateView(view: GraphView, context: TimelineViewOperationContext, step?: boolean): Promise<void>;
  moveOrResizeRow(rowId: string, values: Readonly<Record<string, unknown>>, context: TimelineViewOperationContext, alongside?: Readonly<Record<string, Readonly<Record<string, unknown>>>>): Promise<void>;
  createDependency(successorRowId: string, predecessorRowId: string, revision: number, context: TimelineViewOperationContext, id?: string): Promise<void>;
  /** Takes links away by their IDs, saying whether the server took it; without it no line offers its ✕. */
  deleteDependency?(edgeIds: readonly string[], revision: number, context: TimelineViewOperationContext): Promise<boolean>;
  loadMoreDependencies(cursor: string | null, visibleRowIds: readonly string[], context: TimelineViewOperationContext): Promise<TimelineDependencyPage>;
  isCurrent(context: TimelineViewOperationContext): boolean;
  onError(error: unknown, context: TimelineViewOperationContext): void;
  openRow(rowId: string, context: TimelineViewOperationContext): void;
  openRowIn(rowId: string, mode: TimelineRowOpenMode, context: TimelineViewOperationContext): void; /** Where a row opens by default; the side peek's row closed, by its table row's "닫기". */ openPageIn?: "side" | "center" | "full"; closeRow?(): void;
  openRowContextMenu(rowId: string, event: ReactMouseEvent<HTMLElement>, duplicate?: () => void): void;
  openRowContextMenuFromElement(rowId: string, element: HTMLElement, duplicate?: () => void): void;
  rowContextMenu: ReactNode;
  /** A grouped timeline's group colors and "+ 신규 그룹", as a table's. */
  commitProperty?(property: GraphProperty, context: TimelineViewOperationContext): Promise<void>;
  trashRows?(rowIds: readonly string[]): Promise<void>;
  /** Copies the rows, beside them in the view's order, and gives back their copies. */
  duplicateRows?(rowIds: readonly string[], place?: CopiesPlace): Promise<readonly string[] | void>;
  openGroupSettings?(trigger: HTMLElement): void;
  /** "더 불러오기" and its "⋯", where the timeline shows its first rows. */
  loadMoreRows?(): void; setRowLoadLimit?(limit: number): void;
  /** Moves rows beside another among the rows as shown (`shownIds`), with the values they take there by row; without it its rows stay where they are. */
  moveRows?(rowIds: readonly string[], target: { id: string; side: "before" | "after" }, shownIds: readonly string[], context: TimelineViewOperationContext, values?: Readonly<Record<string, Readonly<Record<string, unknown>>>>, withDates?: boolean): Promise<void>;
}

type Drag = {
  baseEndOffset: number;
  baseStartOffset: number;
  durationDays: number;
  lane: number;
  mode: TimelineInteractionMode;
  pointerStartX: number;
  /** 1 where the days run from the left, -1 from the right: a step along the screen as one along the days. */
  pointerSign: 1 | -1;
  rowId: string;
  /** The other chosen bars a stretched one carries (timelineStretchAlong). */
  along: readonly StretchedAlong[];
};
type StretchedAlong = { row: Page; start: Date; end: Date; first?: number; last?: number };

const NO_FADED: ReadonlySet<string> = new Set();
// A timeline's bars read their rows' pages as they are.
const TIMELINE_PAGES = {};

export function TimelineView({ snapshot, operations, readOnly = false }: { snapshot: TimelineViewSnapshot; operations: TimelineViewOperations; readOnly?: boolean }) {
  const { t } = useTranslation(["timelineView", "tableView", "common"]);
  const rowDisplayTitle = useRowDisplayTitle();
  // A locked database keeps its rows' dates editable but not its view and properties.
  const structureLocked = useDatabaseStructureLocked();
  // A layout builder's preview reads its pages but sets its view, its table and zoom too (databaseRowsPreview).
  const settingsReadOnly = useColumnHeadsReadOnly(readOnly);
  const offersNewRows = useViewOffersNewRows();
  const translate = (key: string, values?: Record<string, unknown>) => String(t(key, values));
  const markerId = `timeline-dep-arrow-${useId().replace(/[^a-zA-Z0-9_-]/g, "")}`;
  const coordinator = useMemo(() => createListOperationCoordinator(), []);
  const controller = useCommittedOperationScope([snapshot.contextIdentity, snapshot.requestGeneration, snapshot.snapshotIdentity]);
  // Left by its viewer, it is first drawn where it opens again (below), at its own zoom: drawn on now, it was drawn whole twice as it opened.
  const [openedAt] = useState(() => rememberedTimelineCenter(snapshot.view.id));
  const [focusDate, setFocusDate] = useState(() => openedAt === null ? new Date() : new Date(openedAt));
  // The time this viewer left the timeline's middle at, while it is the focus.
  const [focusAt, setFocusAt] = useState<number | null>(openedAt);
  const [anchor, setAnchor] = useState(() => timelineAnchorFor(openedAt === null ? new Date() : new Date(openedAt), timelineZoomOf(snapshot.view.config)));
  const [expandedRows, setExpandedRows] = useState<ReadonlySet<string>>(new Set());
  const [collapsedGroups, setCollapsedGroups] = useCollapsedGroups(snapshot.view.id);
  const [interaction, setInteraction] = useState<Drag | null>(null);
  const [preview, setPreview] = useState<TimelineCanvasPreview | null>(null);
  const [newRowId, setNewRowId] = useState<string | null>(null), [newTableRowId, setNewTableRowId] = useState<string | null>(null);
  // A row made here, its bar's title open as the bar is first drawn (below).
  const newRowMade = useRef<string | null>(null);
  const [visibleDate, setVisibleDate] = useState(() => new Date());
  useEffect(() => setNewRowId(null), [snapshot.contextIdentity]);
  // The edges shown, from the snapshot's (`from`, with what it said of more) and those read on since.
  const [dependencyWindow, setDependencyWindow] = useState<{ identity: string; from?: readonly DatabaseDependencyGraphEdge[]; startedMore?: boolean;
    startedAt?: string | null; edges: DatabaseDependencyGraphEdge[]; hasMore: boolean; nextCursor: string | null }>(() => ({
    identity: snapshot.dependencySnapshotIdentity,
    edges: [...snapshot.dependencyEdges],
    hasMore: snapshot.dependencyHasMore,
    nextCursor: snapshot.dependencyNextCursor,
  }));
  const suppressOpenRef = useRef(false);
  // In a page's text its dates keep the database's box while its table is
  // shut and run from the page's left edge to its right one with it open, as
  // Notion's (seen 2026-11-03).
  const timelineRef = useRef<HTMLDivElement>(null);
  const initialAnchorIdentity = useRef("");
  // Where its viewer moves it is kept, an empty timeline's too (Notion's,
  // seen 2026-11-12); an empty one left untouched has its first rows to
  // arrive decide where it opens (below). One opened where it was left keeps
  // that, not written again till its viewer moves it, so it opens there each time.
  const viewerMoved = useRef("");
  const openedWhereLeft = useRef("");
  const dependencyVisibleIdentityRef = useRef(snapshot.dependencySnapshotIdentity);
  const dependencySignalRef = useRef(controller.signal);

  const baseContext = useMemo<TimelineViewOperationContext>(() => ({
    contextIdentity: snapshot.contextIdentity,
    dataSourceId: snapshot.dataSource.id,
    dependencySnapshotIdentity: snapshot.dependencySnapshotIdentity,
    dependencyVisibleIdentity: dependencyVisibleIdentityRef.current,
    requestGeneration: snapshot.requestGeneration,
    requestKey: "timeline",
    signal: controller.signal,
    snapshotIdentity: snapshot.snapshotIdentity,
    viewId: snapshot.view.id,
  }), [controller, snapshot]);
  const contextFor = (requestKey: string): TimelineViewOperationContext => ({ ...baseContext, dependencyVisibleIdentity: dependencyVisibleIdentityRef.current, requestKey, signal: requestKey.startsWith("dependency:") ? dependencySignalRef.current : baseContext.signal });
  // A read or a press that makes something runs once; a write (`write`) is never refused for another of its key still saving (listViewModel.ts).
  async function runOperation<Result>(requestKey: string, perform: (context: TimelineViewOperationContext) => Promise<Result>, publish: (result: Result, context: TimelineViewOperationContext) => void = () => {}, write = false) {
    const handlers = { perform, publish, onError: (error: unknown, context: TimelineViewOperationContext) => operations.onError(error, context),
      isCurrent: (owner: TimelineViewOperationContext) => operations.isCurrent(owner) && (!owner.requestKey.startsWith("dependency:") || owner.dependencyVisibleIdentity === dependencyVisibleIdentityRef.current) };
    await (write ? coordinator.write(contextFor(requestKey), handlers) : coordinator.run(contextFor(requestKey), handlers));
  }

  const properties = useMemo(() => snapshot.properties.map(galleryPresentationProperty), [snapshot.properties]);
  const titleProperty = snapshot.properties.find((property) => property.type === "title");
  // Its rows' pages, made again for the rows changed alone (presentedPages.ts).
  const pagesById = useMemo(() => presentedPagesById({ pagesById: snapshot.rowsAreAll ? undefined : snapshot.pagesById, rowsAreAll: snapshot.rowsAreAll, rows: snapshot.rows, workspaceId: snapshot.workspaceId,
    pagePresentationById: snapshot.pagePresentationById }, titleProperty, TIMELINE_PAGES),
  [snapshot.pagePresentationById, snapshot.rowsAreAll || snapshot.pagesById, snapshot.rows, snapshot.workspaceId, titleProperty]);
  const groupingPages = useMemo(() => pagesOver(snapshot.propertyValueContext.pagesById, pagesById), [pagesById, snapshot.propertyValueContext.pagesById]);
  const view = useMemo(() => timelinePresentationView(snapshot.view), [snapshot.view]);
  // Its rows its filters keep, in its sorts' order; with sub-items, those around them its sub-item option
  // shows, faded, as Notion's. An edit that arrives places the rows it changed alone (viewRowProjection.ts);
  // a value picked shows at once, each row as it is now, the order worked out behind the press (useDeferredPlacing).
  const placing = useDeferredPlacing(snapshot.rows, snapshot.view);
  const { rows: scoped, faded } = useMemo(() => {
    const subitems = placing.rows.some((row) => !!row.subitemParentId) ? { faded: NO_FADED } : undefined;
    const placed = projectRows({ currentUserId: snapshot.currentUserId, pageId: snapshot.contextPageId ?? "", properties: snapshot.properties,
      rows: placing.rows, view: placing.view, workspaceId: snapshot.workspaceId, subitems });
    return { rows: placed.flatMap((row) => pagesById[row.id] ?? []), faded: subitems?.faded ?? NO_FADED };
  }, [pagesById, placing, snapshot.contextPageId, snapshot.currentUserId, snapshot.properties, snapshot.workspaceId]);
  // An inline timeline shows its first rows and "더 불러오기", as a table does (Notion's, seen 2026-10-11).
  const rowWindow = snapshot.rowWindow;
  const { rows: shown, hasMore } = useMemo(() => rowWindow && !rowWindow.perGroup ? tableRowWindow(scoped, rowWindow.count, rowWindow.made) : { rows: scoped, hasMore: false }, [rowWindow, scoped]);
  useEffect(() => { if (faded.size && subitemFilterScope(view.config) !== "parents") setExpandedRows((current) => new Set([...current, ...faded])); }, [faded, view.config]);
  const barColor = useMemo(() => conditionalColorResolver(view.config as Record<string, unknown>, properties, {
    pagesById, currentPageId: snapshot.contextPageId, currentUserId: snapshot.currentUserId,
  }, snapshot.queryBounds), [pagesById, properties, snapshot.contextPageId, snapshot.currentUserId, snapshot.queryBounds, view.config]);
  const subtask = view.config?.subtasks;
  // With sub-items on every row and bar starts with its toggle, whether it holds any or not, and an open
  // one ends in the line that adds one (Notion's, seen 2026-11-05).
  const subitemsOn = snapshot.subitemsOn === true;
  const hierarchy = useMemo(() => {
    const projected = projectListHierarchy({
      displayMode: subtask?.displayMode ?? "show",
      expandedRowIds: expandedRows,
      rows: shown,
      subitemsEnabled: subitemsOn || shown.some((row) => !!row.subitemParentId),
      subitemAdds: subitemsOn,
    });
    return subitemsOn && projected.expandable ? { ...projected, items: projected.items.map((item) => ({ ...item, hasChildren: true })) } : projected;
  }, [expandedRows, shown, subitemsOn, subtask?.displayMode, subtask?.filterScope]);
  const subitemAddsAfter = useMemo(() => {
    const after = new Map<string, Array<{ parentId: string; depth: number }>>();
    let last: string | null = null;
    for (const entry of hierarchy.entries) {
      if (entry.kind === "row") last = entry.item.row.id;
      else if (last && !readOnly) after.set(last, [...(after.get(last) ?? []), { parentId: entry.parentId, depth: entry.depth }]);
    }
    return after;
  }, [hierarchy.entries, readOnly]);
  // Its rows join the pages the shared context already reads, which hold
  // the pages its relations link to, as a board's cards do.
  const propertyValueContext = useMemo(() => ({ ...snapshot.propertyValueContext, contextIdentity: snapshot.contextIdentity, pagesById: pagesOver(snapshot.propertyValueContext.pagesById, pagesById), propsByDatabaseId: { ...snapshot.propertyValueContext.propsByDatabaseId, [snapshot.dataSource.id]: properties }, snapshotIdentity: snapshot.snapshotIdentity }), [pagesById, properties, snapshot]);
  const dateProperties = viewDateSources(properties);
  const startProperty = dateProperties.find((property) => property.id === view.config?.timelineBy) ?? dateProperties.find((property) => property.id === view.config?.calendarBy) ?? firstViewDateSource(dateProperties);
  // Without an end date of its own, a bar runs over its date's range, as
  // Notion's timeline by one date property does (seen 2026-10-06).
  const endProperty = dateProperties.find((property) => property.id === view.config?.timelineEndBy && property.id !== startProperty?.id);
  // A rollup's dates are its related pages': its bars stay where those put
  // them and its days make no page (see viewDateSource.ts).
  const datesFixed = isDateRollup(startProperty) || isDateRollup(endProperty);
  useDateRollupReads(startProperty, propertyValueContext);
  useDateRollupReads(endProperty, propertyValueContext);
  const dateContext = useMemo(() => ({
    properties, pagesById: propertyValueContext.pagesById as Record<string, Page>,
    propertiesByDatabaseId: propertyValueContext.propsByDatabaseId as Record<string, DbProperty[]>,
    bounds: { formula: snapshot.queryBounds.formula, projection: snapshot.queryBounds.formula.rollup },
  }), [properties, propertyValueContext, snapshot.queryBounds]);
  const dateOf = useCallback((row: Page, property: DbProperty) => viewDateValue(row, property, dateContext), [dateContext]);
  const zoom = timelineZoomOf(view.config);
  const range = useMemo(() => timelineRange(anchor, zoom), [anchor, zoom]); // Its window as wide as the browser was as its anchor was found (timelineWindowWidth).
  const columnStep = range.stepMinutes ?? false;
  const columnsPerDay = range.stepMinutes ? 1440 / range.stepMinutes : 1;
  // Hours/day zooms use clock intervals; the others keep calendar-day spans.
  const hourSpan = (row: Page) => {
    const start = startProperty ? moment(dateOf(row, startProperty)) : null;
    return start ? timelineHourSpan(start, endProperty ? moment(dateOf(row, endProperty)) : endMoment(dateOf(row, startProperty!)), range.stepMinutes) : null;
  };
  const projected = useMemo(() => projectTimelineRows({
    anchor,
    column: (date, from) => timelineColumn(date, from, columnStep),
    end: (item) => range.hourly ? hourSpan(item.row)?.end ?? null : endProperty ? shownDay(dateOf(item.row, endProperty))
      : startProperty ? shownEndDate(dateOf(item.row, startProperty))?.date ?? null : null,
    moments: range.hourly, rangeDays: range.rangeDays,
    rows: hierarchy.items,
    start: (item) => range.hourly ? hourSpan(item.row)?.start ?? null : startProperty ? shownDay(dateOf(item.row, startProperty)) : null,
  }), [anchor, dateOf, endProperty, hierarchy, columnStep, range.hourly, range.rangeDays, startProperty]);
  const datedItems = useMemo(() => projected.items.map((item) => ({ ...item, ...item.row, row: item.row.row })) as TimelineRenderedRow[], [projected]);
  const awayItems = useMemo(() => projected.away.map((item) => ({ ...item, ...item.row, row: item.row.row })), [projected]);
  // A phone keeps no table beside its timeline.
  const { phone } = useSidebarLayout(true);
  const tableShown = !!view.config?.timelineShowTable && !phone;
  usePageWideTable(timelineRef, Number.POSITIVE_INFINITY, { wide: tableShown });
  // Its controls, dates and table's head stay at the page's top as it scrolls (useStickyViewHeads).
  useStickyViewHeads(timelineRef, { own: true, shape: `${tableShown}:${String(snapshot.view.config.groupBy ?? "")}` });
  const groupProperty = snapshot.properties.find(property => property.id === snapshot.view.config.groupBy), groupLoads = useListGroupLoads(snapshot.view, rowWindow?.made);
  const creates = Boolean(startProperty) && !readOnly && offersNewRows;
  const viewWritable = !readOnly && !structureLocked;
  const lanes = useMemo(() => projectTimelineLanes({ dated: datedItems, away: awayItems, undated: projected.undated, hierarchy: hierarchy.items,
    groupProperty, properties: snapshot.properties,
    graphRows: snapshot.rows, pagesById: groupingPages, propsByDatabaseId: propertyValueContext.propsByDatabaseId,
    config: snapshot.view.config, currentUserId: snapshot.currentUserId,
    showTable: tableShown, collapsed: collapsedGroups, adds: creates, subitemAddsAfter,
    newGroup: Boolean(timelineGroupAddKind(groupProperty, snapshot.view.config, viewWritable, Boolean(operations.commitProperty))), groupCut: rowWindow?.perGroup ? groupLoads.cut : undefined,
  }), [subitemAddsAfter, collapsedGroups, creates, groupLoads.cut, rowWindow?.perGroup, datedItems, awayItems, groupProperty, groupingPages, hierarchy.items, operations.commitProperty, projected.undated, propertyValueContext.propsByDatabaseId, snapshot.currentUserId, snapshot.properties, snapshot.rows, snapshot.view.config, tableShown, viewWritable]);
  const items = lanes.bars;
  // Its bars are chosen as a board's cards are (cardSelection.tsx), a drag from the canvas's empty room choosing those it touches; a chosen bar's row beside it in the table
  // wears the halo too (Notion's, seen 2026-11-12). A bar let go after a move, or brought back, stays chosen, its days marked over the bars (seen 2026-11-15 at 1440: "가 막대"
  // dragged to 12–16, "12 … 16" marked); one pressed by an end to stretch is chosen at the press, alone unless already among the chosen, which stay (seen 2026-11-24).
  const selection = useCardSelection({ keep: snapshot.view.id, onMenu: operations.openRowContextMenuFromElement, onOpen: openRow, marqueeIn: "[data-timeline-canvas]",
    onTrash: readOnly || !operations.trashRows ? undefined : (ids) => void operations.trashRows?.(ids),
    onDuplicate: readOnly || !operations.duplicateRows ? undefined : (ids) => {
      // Placed in the order its lanes show, a grouped timeline's groups one after another.
      const groups = timelineGroupLines(lanes.groups, hierarchy.items.map((item) => item.row.id));
      const shown = lanes.groups.length ? lanes.groups.flatMap((group) => group.rowIds) : groups[0]!, { rowIds, place } = copiesPlace(groups, ids, shown);
      return operations.duplicateRows!(rowIds, place).catch((error) => { operations.onError(error, contextFor("duplicate")); });
    } }), chooseDragged = useRef(selection.chooseMany); chooseDragged.current = selection.chooseMany;
  const { laneHeights, measure: measureLanes } = useTimelineLaneHeights(lanes, tableShown, phone);
  // Its rows move by their table's ⠿ or their bars carried up and down, the one dropped chosen, into another group taking its value (useTimelineRowReorder).
  const rowReorder = useTimelineRowReorder({ viewId: snapshot.view.id, rootRef: timelineRef, rows: useMemo(() => timelineRowLanes(lanes, tableShown), [lanes, tableShown]), enabled: viewWritable && Boolean(operations.moveRows), chosen: () => selection.value.chosen,
    move: (rowIds, target, group, groupOf, withDates, landed) => { chooseDragged.current(landed ?? rowIds); const values = groupMoveValues(groupProperty, rowIds, group, groupOf, snapshot.view.config, snapshot.rows);
      void runOperation(`row:order:${rowIds[0]}`, (context) => operations.moveRows!(rowIds, target, scoped.map(({ id }) => id), context, values, withDates), undefined, true); } });
  const drawn = useTimelineFirstLanes(timelineRef, lanes.laneCount, laneHeights, items, lanes.table);
  const undated = lanes.undated;
  const undatedDrag = useUndatedTimelineDrag(undated.map(item => item.row), readOnly || datesFixed, (row, column) => {
    if (!startProperty) return;
    const date = timelineColumnStart(anchor, column, columnStep);
    void runOperation(`row:date:${row.id}`, context => operations.moveOrResizeRow(row.id,
      { [startProperty.id]: range.hourly ? dateTimeValue(date) : dateKey(date) }, context), undefined, true);
  });
  const childCountByParent = new Map(hierarchy.items.map((item) => [item.row.id, item.childCount]));
  const earliestDate = hierarchy.items.reduce<Date | null>((current, item) => {
    const date = startProperty ? shownDay(dateOf(item.row, startProperty)) : null;
    return date && (!current || date < current) ? date : current;
  }, null);
  const anchorIdentity = `${snapshot.dataSource.id}:${snapshot.view.id}:${startProperty?.id ?? ""}`;
  useEffect(() => {
    if (initialAnchorIdentity.current === anchorIdentity) return;
    // Where this viewer left it, it opens again, as Notion's (seen 2026-11-12).
    const remembered = rememberedTimelineCenter(snapshot.view.id);
    if (remembered !== null) {
      initialAnchorIdentity.current = anchorIdentity; openedWhereLeft.current = anchorIdentity;
      const at = new Date(remembered), next = timelineAnchorFor(at, zoom);
      setFocusAt(remembered); setFocusDate((shown) => shown.getTime() === remembered ? shown : at); setAnchor((shown) => shown.getTime() === next.getTime() ? shown : next);
      return;
    }
    // With no rows yet it opens on today, in the middle, as Notion's empty
    // timeline (seen 2026-11-04); the first rows it reads decide again, unless
    // its viewer has moved it since (viewerMoved) and it opens there.
    if (snapshot.rows.length === 0) {
      const today = timelineAnchorFor(new Date(), zoom);
      setAnchor((shown) => shown.getTime() === today.getTime() ? shown : today);
      return;
    }
    const opening = initialAnchorIdentity.current === "";
    initialAnchorIdentity.current = anchorIdentity;
    const current = timelineAnchorFor(new Date(), zoom);
    const end = timelineColumnStart(current, range.rangeDays - 1, columnStep);
    const hasCurrent = hierarchy.items.some((item) => {
      const start = startProperty ? shownDay(dateOf(item.row, startProperty)) : null;
      return !!start && start <= end;
    });
    const today = hasCurrent || !earliestDate, nextFocus = today || !earliestDate ? new Date() : earliestDate;
    const nextAnchor = timelineAnchorFor(nextFocus, zoom);
    // Opened on today it keeps the now it was drawn with while in now's column, an anchor at the same time the one
    // drawn: made anew, either drew it whole again, its year of days too, in the press that opened it (2026-11-15).
    setFocusDate((shown) => opening && today && timelineColumn(shown, nextAnchor, columnStep) === timelineColumn(nextFocus, nextAnchor, columnStep) ? shown : nextFocus);
    setAnchor((shown) => shown.getTime() === nextAnchor.getTime() ? shown : nextAnchor);
  }, [anchorIdentity, dateOf, earliestDate, hierarchy.items, columnStep, range.hourly, range.rangeDays, snapshot.rows.length, snapshot.view.id, startProperty, zoom]);
  // Each zoom owns its date window, including the year and five-year ranges.
  const shownZoom = useRef(zoom);
  useEffect(() => {
    if (shownZoom.current === zoom) return;
    shownZoom.current = zoom;
    setAnchor(timelineAnchorFor(focusDate, zoom));
  }, [zoom]);

  const navigate = (direction: 1 | -1) => { const next = timelineShiftedAnchor(focusDate, zoom, direction); setFocusDate(next); setAnchor(timelineAnchorFor(next, zoom)); };
  // Scrolled near an end of its window, it is drawn again around the time in
  // its middle, which stays where it is (TimelineCanvas), as Notion's — not
  // while a bar is being dragged on it.
  const recenter = (center: number) => {
    const at = new Date(center), next = timelineAnchorFor(at, zoom);
    if (interaction || next.getTime() === anchor.getTime()) return false;
    setFocusAt(center); setFocusDate(at); setAnchor(next);
    return true;
  };
  // A row dated past its window comes in by its arrow as a near bar does:
  // drawn again around the time that stands its end (or start) this far from
  // the middle (TimelineOffRangeMarkers), as Notion's (seen 2026-11-13).
  const bringAway = (item: { start: Date; end: Date; side: "start" | "end" }, fromMiddle: number) => {
    const edge = item.side === "end" ? timelineColumn(item.end, anchor, columnStep) + 1 : timelineColumn(item.start, anchor, columnStep);
    const center = timelineTimeAt(anchor, edge - fromMiddle / range.dayWidth, columnStep), at = new Date(center);
    setFocusAt(center); setFocusDate(at); setAnchor(timelineAnchorFor(at, zoom));
  };
  // "오늘" puts now in the middle of the days shown, past a table laid over
  // them, as Notion's (seen 2026-11-13 at 1440, month and week: now at 1028.5
  // past a table ending at 640 in a page's text, at 841 without it).
  const showToday = () => {
    const today = new Date(), scroller = timelineRef.current?.querySelector<HTMLElement>("[data-timeline-canvas]");
    const cover = scroller ? timelineTableCover(scroller) : 0;
    if (!cover) { setFocusDate(today); setAnchor(timelineAnchorFor(today, zoom)); return; }
    const column = timelineColumn(today, anchor, columnStep), start = timelineColumnStart(anchor, column, columnStep).getTime();
    const at = column + (today.getTime() - start) / (timelineColumnStart(anchor, column + 1, columnStep).getTime() - start);
    // The time at the box's middle, half the table before now.
    const center = timelineTimeAt(anchor, at - cover / 2 / range.dayWidth, columnStep), date = new Date(center);
    setFocusAt(center); setFocusDate(date); setAnchor(timelineAnchorFor(date, zoom));
  };
  // Only a relation back to this database links its rows as earlier and
  // later work; one to another database's pages can't.
  const relationProperties = properties.filter((property) => property.type === "relation" && property.config?.relationDatabaseId === snapshot.dataSource.id);
  const dependencyProperty = relationProperties.find((property) => property.id === (snapshot.dependencyPropertyId ?? view.config?.dependencyProperty));
  // A view may hide its dependencies, as Notion's "보기에서 종속성 숨기기" (seen 2026-10-11).
  const dependenciesShown = view.config?.timelineHideDependencies !== true;
  const canonicalDependencyGraphActive = !!dependencyProperty && snapshot.dependencyRevision > 0;
  const positionedIds = useMemo(() => items.map((item) => item.row.id), [items]);
  const newBars = useTimelineNewBarChoice({ titlePropertyId: titleProperty?.id, choose: selection.chooseMany, reopen: setNewRowId });
  const dependencyVisibleIdentity = `${snapshot.dependencySnapshotIdentity}:${positionedIds.join("\u0000")}`;
  const dependencyController = useCommittedOperationScope([dependencyVisibleIdentity]);
  dependencyVisibleIdentityRef.current = dependencyVisibleIdentity;
  dependencySignalRef.current = dependencyController.signal;
  // A row made here opens its bar's title as the bar is first drawn, in that
  // drawing: set at the press, the timeline was drawn once without the bar
  // and again with it (2026-11-15: ~20 ms at CPU ×2 inside the press).
  if (newRowMade.current && newRowMade.current !== newRowId && positionedIds.includes(newRowMade.current)) {
    setNewRowId(newRowMade.current);
    newRowMade.current = null;
  }
  // Its dependencies start again from the snapshot's as the bars or the
  // snapshot change, set as it is drawn: set after, it drew the whole
  // timeline a second time — twice inside a new row's press (2026-11-15:
  // ~20 ms each at CPU ×2).
  const dependencyStart = { identity: dependencyVisibleIdentity, from: snapshot.dependencyEdges,
    hasMore: snapshot.dependencyHasMore && positionedIds.length > 0, nextCursor: snapshot.dependencyNextCursor };
  if (dependencyWindow.identity !== dependencyStart.identity || dependencyWindow.from !== dependencyStart.from
    || dependencyWindow.startedMore !== dependencyStart.hasMore || dependencyWindow.startedAt !== dependencyStart.nextCursor) {
    setDependencyWindow({ identity: dependencyStart.identity, from: dependencyStart.from, startedMore: dependencyStart.hasMore,
      startedAt: dependencyStart.nextCursor, edges: [...dependencyStart.from], hasMore: dependencyStart.hasMore, nextCursor: dependencyStart.nextCursor });
  }
  useEffect(() => {
    if (!dependencyWindow.hasMore || dependencyWindow.identity !== dependencyVisibleIdentity) return;
    void runOperation("dependency:continuation", (context) => operations.loadMoreDependencies(dependencyWindow.nextCursor, positionedIds, context), (page) => {
      if (page.dependencySnapshotIdentity !== dependencyVisibleIdentity) return;
      setDependencyWindow((current) => ({ ...current, edges: [...current.edges, ...page.edges], hasMore: page.hasMore, nextCursor: page.nextCursor }));
    });
  }, [dependencyVisibleIdentity, dependencyWindow.hasMore, dependencyWindow.identity, dependencyWindow.nextCursor, operations, positionedIds]);
  const fallbackEdges = canonicalDependencyGraphActive || !dependencyProperty ? [] : timelineRelationEdges(items, dependencyProperty);
  const dependencyPairs = dependenciesShown
    ? projectTimelineDependencyPairs({ edges: canonicalDependencyGraphActive ? dependencyWindow.edges : fallbackEdges, positionedRowIds: positionedIds }) : [];
  // Its dates show beside the bars too while the view shows them, as Notion's (seen 2026-11-06).
  // A bar shows its values in the view's order, its title among them where that order puts it (TimelineCanvas).
  const barProperties = visibleViewProperties(properties, view, snapshot.queryBounds);

  // A setting changed here is a step of ⌘Z, as Notion's (seen 2026-11-12: the table shown or hidden), its zoom not.
  function updateView(config: ViewConfig, key: string, step = true) {
    if (settingsReadOnly || structureLocked) return;
    const next: GraphView = { ...snapshot.view, config: config as unknown as Record<string, unknown> };
    void runOperation(key, (context) => operations.updateView(next, context, step), undefined, true);
  }
  function openRow(rowId: string, mode: TimelineRowOpenMode = "default") {
    const context = contextFor(`open:${rowId}`);
    if (!operations.isCurrent(context) || context.signal.aborted) return;
    if (mode === "default") operations.openRow(rowId, context);
    else operations.openRowIn(rowId, mode, context);
  }
  // The values a bar moved or stretched to columns first..last writes (timelineDatedValues).
  const dateFrame = startProperty ? { anchor, columnStep, startProperty, endProperty, dateOf } : undefined;
  const datedValues = (item: TimelineRenderedRow, startOffset: number, endOffset: number, mode: TimelineInteractionMode) => dateFrame
    ? timelineDatedValues(dateFrame, item, startOffset, endOffset, mode) : {};
  // The chosen bars a stretched one carries, drawn over the columns they would take as it lands on `target`.
  const alongShown = (active: Drag, target: { first: number; last: number }) => new Map(active.along.flatMap((bar) => bar.first === undefined || bar.last === undefined ? []
    : [[bar.row.id, timelineAlongColumns({ first: bar.first, last: bar.last }, active.mode, target, range.hourly)] as const]));
  const shiftedValues = (item: TimelineRenderedRow, delta: number) => datedValues(item, timelineColumn(item.start, anchor, columnStep) + delta, timelineColumn(item.end, anchor, columnStep) + delta, "move");
  // An undated row's line beside the table gives it the dates of the bar shown where pressed (TimelineUndatedLanes).
  const undatedIds = new Set(lanes.undated.map((item) => item.row.id)), linkedIds = new Set(lanes.subitemLinks.map((link) => link.row.id));
  const undatedLanes = tableShown && !readOnly && !datesFixed && startProperty ? lanes.table.filter((lane) => undatedIds.has(lane.row.id) && !linkedIds.has(lane.row.id) && !lane.row.isLocked) : [];
  const dateRow = (row: Page, first: number, columns: number) => { if (dateFrame) void runOperation(`row:date:${row.id}`, (context) => operations.moveOrResizeRow(row.id, timelinePlacedValues(dateFrame, first, columns), context), undefined, true); };
  function shiftRowDate(item: TimelineRenderedRow, delta: number) {
    if (readOnly || datesFixed || item.row.isLocked || !startProperty) return;
    void runOperation(`row:move:${item.row.id}`, (context) => operations.moveOrResizeRow(item.row.id, shiftedValues(item, delta), context), undefined, true);
  }
  // Its links drawn and taken away (timelineDependencyEdits).
  const dependencyEdits = timelineDependencyEdits({ readOnly, pairs: dependencyPairs, edges: dependencyWindow.edges, revision: snapshot.dependencyRevision,
    setWindow: setDependencyWindow, run: runOperation, operations });
  function beginInteraction(event: ReactPointerEvent<HTMLElement>, item: TimelineRenderedRow, mode: TimelineInteractionMode) {
    // A row whose database page is locked keeps its dates.
    if (readOnly || datesFixed || item.row.isLocked || event.button !== 0) return;
    if (mode !== "move") event.preventDefault();
    event.stopPropagation();
    const baseStartOffset = timelineColumn(item.start, anchor, columnStep);
    const baseEndOffset = timelineColumn(item.end, anchor, columnStep);
    const along: StretchedAlong[] = mode === "move" ? [] : timelineStretchedAlong<StretchedAlong>([...items, ...lanes.away], selection.value.chosenRows, item.row.id);
    const next = { mode, rowId: item.row.id, lane: item.lane, pointerStartX: event.clientX, pointerSign: inlineSign(event.currentTarget), baseStartOffset, baseEndOffset, durationDays: Math.max(range.hourly ? -1 : 0, baseEndOffset - baseStartOffset), along };
    setInteraction(next); if (mode === "move") rowReorder.barBegin(item.row.id, event, item.lane); else if (!selection.value.chosenRows.has(item.row.id)) chooseDragged.current([item.row.id]);
    setPreview({ rowId: next.rowId, lane: next.lane, first: Math.max(0, next.baseStartOffset), last: Math.min(range.rangeDays - 1, next.baseEndOffset), mode, along: alongShown(next, { first: item.first, last: item.last }) });
  }
  useEffect(() => {
    if (!interaction || !startProperty || readOnly) return;
    const active = interaction;
    const move = (clientX: number) => {
      if (Math.abs(clientX - active.pointerStartX) > 4) suppressOpenRef.current = true;
      const deltaDays = Math.round((clientX - active.pointerStartX) * active.pointerSign / range.dayWidth), next = timelineInteractionPreview({ ...active, deltaDays, maxIndex: range.rangeDays - 1, moments: range.hourly });
      setPreview({ rowId: active.rowId, lane: active.lane, first: next.first, last: next.last, mode: active.mode, along: alongShown(active, next) });
      return { ...next, deltaDays };
    };
    // Carried up or down too, its row and the chosen rows it carries land where the line shows; their days move as far as its own, written only where they moved (useTimelineRowReorder).
    const onPointerMove = (event: PointerEvent) => { move(event.clientX); if (rowReorder.barFollow(event)) suppressOpenRef.current = true; };
    const onPointerUp = (event: PointerEvent) => {
      const next = move(event.clientX);
      const item = items.find((candidate) => candidate.row.id === active.rowId);
      if (item && suppressOpenRef.current) {
        // Stretched, it and the chosen bars it carries take its pressed side's new edge (timelineStretchAlong), it where the pointer took it: past its other end, stopped there as a range of the two alike (Notion's, seen 2026-11-24: 14:00–15:00's end taken to 12:00 is 14:00–14:00).
        const stretched = (bar: StretchedAlong, edge = active.mode === "resize-end" ? next.endOffset : next.startOffset) => active.mode === "move" || !dateFrame ? {} : timelineStretchValues(dateFrame, bar, active.mode, edge);
        const values = active.mode === "move" ? datedValues(item, next.startOffset, next.endOffset, active.mode) : stretched(item, (active.mode === "resize-end" ? active.baseEndOffset : active.baseStartOffset) + next.deltaDays), along = rowReorder.barCarriedAlong();
        const alongside = active.mode === "move" ? carriedAlongValues(items, along, (other) => shiftedValues(other, next.deltaDays)) : active.along.length ? Object.fromEntries(active.along.map((bar) => [bar.row.id, stretched(bar)])) : undefined;
        if (next.deltaDays) void runOperation(`row:drag:${active.rowId}`, (context) => operations.moveOrResizeRow(active.rowId, values, context, alongside), undefined, true); if (active.mode === "move") chooseDragged.current([active.rowId, ...along]);
      }
      rowReorder.barEnd(event.type === "pointerup", Boolean(item && suppressOpenRef.current && next.deltaDays)); setInteraction(null); setPreview(null); window.setTimeout(() => { suppressOpenRef.current = false; }, 0);
    };
    // Escape gives it up: its bars back, nothing written, none chosen (timelineBarDrag.ts).
    return listenTimelineBarDrag({ moved: onPointerMove, released: onPointerUp, suppressOpen: suppressOpenRef, escaped: () => { rowReorder.barEnd(false); setInteraction(null); setPreview(null); } });
  }, [anchor, columnStep, columnsPerDay, dateOf, endProperty, interaction, items, operations, range.dayWidth, range.hourly, range.rangeDays, readOnly, startProperty]);

  const rowProps = { childCountByParent, expandedRows, flattenedSubtasks: subtask?.displayMode === "flattened", hiddenSubtasks: subtask?.displayMode === "hidden", openRow, openRowContextMenu: (rowId: string, event: ReactMouseEvent<HTMLElement>) => operations.openRowContextMenu(rowId, event, selection.copyHere(event.currentTarget, rowId)), openRowContextMenuFromElement: (rowId: string, element: HTMLElement) => operations.openRowContextMenuFromElement(rowId, element, selection.copyHere(element, rowId)), pagesById, rowTitle: rowDisplayTitle, t: translate, toggleExpanded: (rowId: string) => setExpandedRows((current) => { const next = new Set(current); if (next.has(rowId)) next.delete(rowId); else next.add(rowId); return next; }) };
  const fullDateLabel = (date: Date) => timelineDateText(date, "date", activeDateLocale());
  const columnLabel = (date: Date) => range.hourly ? timelineDateText(date, "dateTime", activeDateLocale()) : fullDateLabel(date);
  const rangeLabel = timelineHeadingLabel(visibleDate, zoom, activeDateLocale());
  const rowDateLabels = (row: Page) => {
    const start = startProperty ? moment(dateOf(row, startProperty)) : null;
    const end = endProperty ? moment(dateOf(row, endProperty)) : startProperty ? endMoment(dateOf(row, startProperty)) : null;
    const label = (value: typeof start) => value ? (range.hourly && value.timed ? columnLabel(value.date) : fullDateLabel(value.date)) : "";
    return { start: label(start), end: label(end ?? start) };
  };
  // Rows made by the canvas, its group heads, its table's "+ 새로 만들기", sub-item "+" and row "+" (useTimelineRowsMade).
  const { createAt, createSubitem, addBeside, pasteRows, leftUnnamed } = useTimelineRowsMade({ operations, run: runOperation, readOnly, offersNewRows, snapshot, properties, view, startProperty, endProperty, groupProperty,
    anchor, columnStep, columnsPerDay, hourly: range.hourly, newRowMade, setNewRowId, setNewTableRowId, groups: lanes.groups, table: lanes.table, choose: selection.choose });
  const tableCells = timelineTableCells({ snapshot, readOnly, operations, run: runOperation, makeRows: pasteRows }); // Its table's range's writes (useTimelineTableRange).
  const commitNewTitle = (row: Page, title: string, byKey = false) => {
    if (!readOnly && !row.isLocked && titleProperty) void runOperation(`row:title:${row.id}`, context => operations.moveOrResizeRow(row.id, { [titleProperty.id]: title }, context), undefined, true);
    setNewRowId(null); setNewTableRowId(null); newBars.closed(row.id, title, byKey);
  };
  // A group head's "+" makes a row from today four days on (seen 2026-11-01); its table's "+ 새로 만들기" one with no date and its canvas line one where pressed (2026-11-15).
  const group = useTimelineGroupActions({ groupProperty, config: snapshot.view.config, writable: viewWritable, groups: lanes.groups, environment: snapshot.propertyCellEnvironment,
    createInGroup: creates ? key => createAt(timelineColumn(new Date(), anchor, columnStep), 4 * columnsPerDay, key) : undefined, createInTable: creates ? key => createAt(null, 0, key) : undefined,
    updateConfig: next => updateView(next as ViewConfig, "view:group"),
    commitProperty: operations.commitProperty ? property => runOperation(`property:${property.id}`, context => operations.commitProperty!(property, context), undefined, true) : undefined,
    trashRows: operations.trashRows ? ids => { if (ids.length) void operations.trashRows?.(ids); } : undefined,
    openGroupSettings: operations.openGroupSettings, loadMore: groupLoads.loadMore, loadLimit: { limit: groupLoads.step, onLimit: operations.setRowLoadLimit } });
  const monthSpans = timelineTopSpans(range.days, zoom, activeDateLocale());
  const showTable = !!view.config?.timelineShowTable;
  const toggleGroup = (key: string) => setCollapsedGroups(current => { const next = new Set(current); if (next.has(key)) next.delete(key); else next.add(key); return next; });
  const tableColumns = showTable ? timelineTableColumns(properties, view.config as Record<string, unknown>) : [];
  // The shown columns' widths, the title's 280px among them, as Notion's.
  const tableWidth = tableColumns.reduce((total, column) => total + column.width, 0);
  // Its calculations, under its columns as a table's foot (TableSummaryRow; Notion's, seen 2026-11-15).
  const tableSummary = (columns: string) => <TableSummaryRow columns={columns} widths={[]} rows={snapshot.rows} allProperties={snapshot.properties}
    properties={tableColumns.flatMap((column) => snapshot.properties.find((property) => property.id === column.property.id) ?? [])}
    calculations={(snapshot.view.config.calculations && typeof snapshot.view.config.calculations === "object" ? snapshot.view.config.calculations : {}) as Record<string, unknown>}
    limitLargeCounts={snapshot.view.config.limitLargeCounts === true} pagesById={snapshot.propertyValueContext.pagesById}
    propertiesByDatabaseId={snapshot.propertyCellEnvironment?.snapshot.propertiesByDataSourceId} workspaceId={snapshot.workspaceId}
    readOnly={settingsReadOnly || structureLocked} onChange={(id, value) => updateView(tableConfigAfterCalculation(snapshot.view.config, id, value) as unknown as ViewConfig, `view-calculation:${id}`)}
    onLimitLargeCounts={(limit) => updateView({ ...snapshot.view.config, limitLargeCounts: limit || undefined } as unknown as ViewConfig, "view-limit-large-counts")} />;
  // Where it is now on the canvas, to the minute and kept so as minutes pass
  // (useTimelineNow): the line by that time to the pixel at month and week
  // zoom, not by its hour (Notion's, seen 2026-11-12 at 1440).
  // The remembered time, to the pixel, while the focus is still there.
  const focusColumn = timelineColumn(focusDate, anchor, columnStep);
  const focusColumnStart = timelineColumnStart(anchor, focusColumn, columnStep).getTime();
  // A time kept to a part of a millisecond is its date's still (a Date keeps whole ones): else it opened on its column's middle.
  const focusOffset = focusAt === null || Math.abs(focusAt - focusDate.getTime()) >= 1 ? undefined : (focusColumn + (focusAt - focusColumnStart)
    / (timelineColumnStart(anchor, focusColumn + 1, columnStep).getTime() - focusColumnStart)) * range.dayWidth;
  const { column: nowColumn, offset: nowOffset, label: nowLabel } = useTimelineNow(anchor, range, activeDateLocale());
  if (!startProperty) {
    return <DateViewMissing view="timelineView" onAdd={viewWritable ? () => void runOperation(
      "create-date-property",
      (context) => operations.createDatePropertyAndSelect(t("calendarView:property.dateDefaultName"), context),
    ) : undefined} />;
  }
  const viewerMoves = () => { viewerMoved.current = anchorIdentity; };
  const keepsCenter = () => viewerMoved.current === anchorIdentity
    || (initialAnchorIdentity.current === anchorIdentity && openedWhereLeft.current !== anchorIdentity);
  return <div ref={timelineRef} className={styles.timeline} data-timeline-root="" data-show-table={tableShown ? "true" : undefined}
    onPointerDownCapture={viewerMoves} onWheelCapture={viewerMoves} onKeyDownCapture={viewerMoves}
    style={tableShown ? { "--timeline-table-width": `${tableWidth}px` } as CSSProperties : undefined}>
    <div className={styles.timelineToolbar}>
      <div className={styles.timelineHeading} data-sticky-head="">
        <button type="button" className={styles.timelineTableToggle} aria-label={t(showTable ? "timelineView:toolbar.hideTable" : "timelineView:toolbar.showTable")}
          aria-pressed={showTable} disabled={settingsReadOnly || structureLocked} onClick={() => updateView({ ...view.config, timelineShowTable: !showTable }, "view:table")}>
          {showTable ? <DoubleChevronLeft size="small" aria-hidden="true" /> : <DoubleChevronRight size="small" aria-hidden="true" />}</button>
        <div className={styles.timelineTitle}>{rangeLabel}</div>
      </div>
      <div className={styles.timelineControls} data-sticky-head="">
        <DatabaseViewControls><UndatedRowsMenu key={anchorIdentity} rows={undated.map(item => item.row)} readOnly={readOnly || datesFixed} timeline
          onOpen={openRow} rowRef={undatedDrag.rowRef} onAssignToday={row => { if (!row.isLocked && startProperty && !datesFixed) void runOperation(`row:date:${row.id}`,
            context => operations.moveOrResizeRow(row.id, { [startProperty.id]: dateKey(new Date()) }, context), undefined, true); }} /></DatabaseViewControls>
        {/* As Notion's (seen 2026-11-03): 180 wide 8px under its button, the step shown marked by its ground alone. */}
        <NotionSelect ariaLabel={t("timelineView:zoom.ariaLabel")} className={styles.timelineZoomSelect} buttonClassName={styles.timelineZoomButton}
          menuClassName={styles.timelineZoomMenu} optionClassName={styles.timelineZoomOption} menuSize={{ width: 180, maxHeight: 300, gap: 8 }} showCheck={false} value={zoom} options={TIMELINE_ZOOMS.map(value => ({ value, label: t(`timelineView:zoom.${value}`) }))}
          disabled={settingsReadOnly || structureLocked} onChange={value => updateView({ ...view.config, timelineZoom: value as TimelineZoom }, "view:zoom", false)} />
        {/* Its dates run in the reading direction (DatabaseTimelineCore .timelineBody): earlier at the start, its arrows turned over with the screen. */}
        <button type="button" aria-label={t("timelineView:toolbar.previousRange")} onClick={() => navigate(-1)}><ChevronLeft size="small" aria-hidden="true" /></button>
        <button type="button" onClick={showToday}>{t("timelineView:toolbar.today")}</button>
        <button type="button" aria-label={t("timelineView:toolbar.nextRange")} onClick={() => navigate(1)}><ChevronRight size="small" aria-hidden="true" /></button>
      </div>
    </div>
    <CardSelectionProvider value={selection.value}><div className={styles.timelineBody} data-show-table={tableShown ? "true" : undefined}>{tableShown && <TimelineTableRows onLaneHeightsChange={measureLanes} onCreateSubitem={createSubitem} onAddBeside={addBeside} newTableRowId={newTableRowId} onCommitTitle={(row, title, escaped) => { if (!leftUnnamed(row.id, title, escaped === true)) commitNewTitle(row, title); }}
      onCreate={startProperty && !readOnly && offersNewRows ? () => createAt(null, 0) : undefined} nameHeader={titleProperty?.name} summary={tableSummary} items={drawn.rows} lanes={lanes} groupActions={group.actions} toggleGroup={toggleGroup} columns={tableColumns} propertyValueContext={propertyValueContext} propertyCellEnvironment={snapshot.propertyCellEnvironment} cellsReadOnly={readOnly} cells={tableCells} reorder={rowReorder} openRowIn={operations.openPageIn} onCloseRow={operations.closeRow} onResizeColumn={(propertyId, width) => updateView({ ...view.config, timelineTableProperties: resizeTimelineTableProperty(properties, view.config as Record<string, unknown>, propertyId, width) }, `view:table-width:${propertyId}`)} {...rowProps} />}<TimelineCanvas onCreateSubitem={createSubitem} subitemGhostColumns={4 * columnsPerDay + 1} undatedDrag={undatedDrag.target} laneHeights={laneHeights} lanes={lanes} groupActions={group.actions} laneCount={lanes.laneCount} toggleGroup={toggleGroup} anchor={anchor} arrowMarkerId={markerId} barColor={barColor} beginInteraction={beginInteraction} canonicalDependencyGraphActive={canonicalDependencyGraphActive && dependenciesShown} childCountByParent={childCountByParent} createDependency={dependencyEdits.create} deleteDependency={dependencyEdits.remove} dayLabel={(day) => range.hourly ? timelineHourHead(day, activeDateLocale(), range.labelMinutes) : timelineDayLabel(day, zoom, activeDateLocale())} dayLabelOnTick={timelineDayLabelsOnTick(zoom)} lineColumns={timelineLineColumns(zoom)} days={range.days} dayWidth={range.dayWidth} dependencyPairs={dependencyPairs} endProperty={endProperty ?? startProperty} earliestDate={earliestDate} flattenedSubtasks={rowProps.flattenedSubtasks} fullDateLabel={columnLabel} dayTip={range.hourly || zoom === "5_years" ? undefined : (day) => timelineDateText(day, "longWeekday", activeDateLocale())} gridTemplate={timelineGridTemplate(range)} hiddenSubtasks={rowProps.hiddenSubtasks} items={drawn.bars} jumpToEarliest={() => { if (earliestDate) { setFocusDate(earliestDate); setAnchor(timelineAnchorFor(earliestDate, zoom)); } }} barProperties={barProperties} monthSpans={monthSpans} openRow={openRow} openRowContextMenu={rowProps.openRowContextMenu} openRowContextMenuFromElement={rowProps.openRowContextMenuFromElement} pagesById={pagesById} preview={preview} propertyValueContext={propertyValueContext} largeStep={range.hourly ? columnsPerDay : 7} smallStep={(range.labelMinutes ?? 1) / (range.stepMinutes ?? 1)} rangeDays={range.rangeDays} windowWidth={range.windowWidth} readOnly={readOnly} datesFixed={datesFixed} renderSubitemControls={(item) => <TimelineSubitemControls {...rowProps} item={item} />} expandedRows={expandedRows} resetInteraction={() => { rowReorder.barEnd(false); setInteraction(null); setPreview(null); }} dropLine={rowReorder.line} rowTitle={rowDisplayTitle} rowDateLabels={rowDateLabels} shiftRowDate={shiftRowDate} showTable={tableShown} pageIcons={viewShowsPageIcons(view.config ?? {})} suppressOpenRef={suppressOpenRef} t={translate} todayIndex={nowColumn} nowOffset={nowOffset} nowLabel={nowLabel} onViewportStart={date => setVisibleDate(current => timelineHeadingDay(current, date, zoom, activeDateLocale()))} focusIndex={focusColumn} focusOffset={focusOffset} onViewportCenter={(time) => { if (keepsCenter()) rememberTimelineCenter(snapshot.view.id, time); }} onNearEnd={recenter} onBringAway={bringAway} viewportIdentity={`${snapshot.contextIdentity}:${zoom}:${focusDate.getTime()}`} onCreateRow={startProperty && !readOnly && offersNewRows ? createAt : undefined} newBarColumns={timelineNewBarColumns(zoom)} newRowId={newRowId} undated={undatedLanes} onDateRow={dateRow}
      onCommitTitle={commitNewTitle} /></div></CardSelectionProvider>
    {hasMore && operations.loadMoreRows ? <TableLoadMoreRow loading={false} limit={rowWindow?.limit} onLoadMore={operations.loadMoreRows} onLimit={operations.setRowLoadLimit} /> : null}
    {operations.rowContextMenu}{group.menu}
  </div>;
}
