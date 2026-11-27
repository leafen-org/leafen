"use client";

import { useMemo, useRef, useState, type MouseEvent as ReactMouseEvent, type ReactNode } from "react";
import type { GraphDataSource, GraphProperty, GraphRow, GraphView } from "@/contentGraph";
import type { DatabaseSubitemsFeature } from "@/lib/types";
import type { NativeTableProductAdapter } from "@/lib/nativeTableProductAdapter";
import type { NativeTimelineProductAdapter } from "@/lib/nativeTimelineProductAdapter";
import type { TimelineRowOpenMode } from "./TimelineRows";
import {
  TimelineView,
  type TimelineViewOperationContext,
  type TimelineViewOperations,
  type TimelineViewSnapshot,
} from "./TimelineView";
import { nativeBoardQueryBounds } from "./boardViewModel";
import { useDatabaseOwnPage } from "./databaseFullPageLink";
import { effectiveOpenPageIn } from "./databaseViewShared";
import { TABLE_LOAD_MORE_ROWS, firstLoadLimit, topRowCount } from "./queryProjection";
import { graphRowsIdentity, type DatabaseFilterGraph } from "./databaseFilterSnapshot";
import { useNativePropertyCells } from "./useNativePropertyCells";
import { rowsInViewOrder, viewKeepsOwnRowOrder, viewRowOrderPlacing, type CopiesPlace } from "./viewRowOrder";
import { timelineDependencySettings } from "./timelineDependencySettings";
import { rowsById } from "./presentedPages";
import { moveShownRows, movedRowsValues } from "./viewRowMove";

const NO_ROWS: ReadonlySet<string> = new Set();

type NativeTimelineViewMountProps = {
  adapter?: NativeTimelineProductAdapter;
  /** Saves cells edited in the table panel, as a table saves its cells. */
  cellAdapter?: NativeTableProductAdapter;
  currentUserId?: string;
  dataSource: GraphDataSource;
  dataSources: readonly GraphDataSource[];
  filterGraph: DatabaseFilterGraph;
  onError(error?: unknown): void;
  onOpenRow(rowId: string, mode: TimelineRowOpenMode): void;
  /** Closes the row open in the side peek, by a table row's "닫기". */
  onCloseRow?(): void;
  /** A group menu's "그룹 편집": the view's group settings. */
  onOpenGroupSettings?(trigger: HTMLElement): void;
  /** A group menu's "휴지통으로 이동" for the group's rows. */
  trashRows?(rowIds: readonly string[]): Promise<void>;
  /** ⌘D on chosen bars: copies them and gives back their copies. */
  duplicateRows?(rowIds: readonly string[], place?: CopiesPlace): Promise<readonly string[] | void>;
  openRowContextMenu(rowId: string, event: ReactMouseEvent<HTMLElement>, duplicate?: () => void): void;
  openRowContextMenuFromElement(rowId: string, element: HTMLElement, duplicate?: () => void): void;
  pageId: string;
  /** The page the view is on, a linked view's own: "current page" in its filters. */
  contextPageId?: string;
  properties: readonly GraphProperty[];
  readOnly: boolean;
  rowContextMenu: ReactNode;
  rows: readonly GraphRow[];
  subitems?: DatabaseSubitemsFeature | null;
  view: GraphView;
  workspaceId: string;
};

export function NativeTimelineViewMount({
  adapter,
  cellAdapter,
  currentUserId,
  dataSource,
  dataSources,
  filterGraph,
  onError,
  onOpenRow,
  onCloseRow,
  onOpenGroupSettings,
  trashRows,
  duplicateRows,
  openRowContextMenu,
  openRowContextMenuFromElement,
  pageId,
  contextPageId,
  properties,
  readOnly,
  rowContextMenu,
  rows,
  subitems = null,
  view,
  workspaceId,
}: NativeTimelineViewMountProps) {
  const snapshotIdentity = useMemo(
    () => JSON.stringify([dataSource, view, properties]) + graphRowsIdentity(rows),
    [dataSource, properties, rows, view],
  );
  const contextIdentity = `${currentUserId ?? "anonymous"}:${workspaceId}:${pageId}:${dataSource.id}:${view.id}`;
  const generation = useRef({ identity: "", value: 0 });
  if (generation.current.identity !== snapshotIdentity) {
    generation.current = { identity: snapshotIdentity, value: generation.current.value + 1 };
  }
  const { propertyCellEnvironment, propertyValueContext } = useNativePropertyCells({
    // A locked page's timeline keeps its table's cells' ways: they only show their values (cellReadOnly.ts).
    adapter: cellAdapter, readOnly, contextIdentity, currentUserId, dataSource, dataSources, filterGraph,
    pageId, properties, requestGeneration: generation.current.value, rows, workspaceId,
  });
  const dependency = timelineDependencySettings(view, properties);
  // An inline timeline shows its first rows, 50 unless its view says, and as many more at each "더 불러오기",
  // a row made here at once; a database's own page shows every row (Notion's, seen 2026-10-11).
  const ownPage = useDatabaseOwnPage();
  const loadLimit = ownPage ? undefined : firstLoadLimit(view);
  const windowKey = `${view.id}:${loadLimit ?? "all"}`;
  const [shownRows, setShownRows] = useState<{ key: string; count: number; made: ReadonlySet<string> }>({ key: "", count: 0, made: new Set() });
  const rowWindow = useMemo(() => loadLimit === undefined ? undefined
    : shownRows.key === windowKey ? { limit: loadLimit, count: shownRows.count, made: shownRows.made } : { limit: loadLimit, count: loadLimit, made: NO_ROWS },
  [loadLimit, shownRows, windowKey]);
  const windowRef = useRef(rowWindow);
  windowRef.current = rowWindow;
  const widen = (change: (current: NonNullable<typeof rowWindow>) => { count: number; made: ReadonlySet<string> }) => {
    const current = windowRef.current;
    if (current) setShownRows({ key: windowKey, ...change(current) });
  };
  // Public presentation draws the relation values it can read; authenticated
  // pages use the bounded edge API and its stale-configuration guard.
  const revision = adapter ? dependency.revision : 0;
  const dependencySnapshotIdentity = `${contextIdentity}:${revision}:${snapshotIdentity}`;
  const snapshot = useMemo<TimelineViewSnapshot>(() => ({
    snapshotIdentity,
    dependencySnapshotIdentity,
    contextIdentity,
    requestGeneration: generation.current.value,
    workspaceId,
    subitemsOn: Boolean(subitems),
    contextPageId: contextPageId ?? pageId,
    currentUserId,
    dataSource,
    view,
    properties,
    rows,
    // Made when it is read: the timeline's bars are made from its rows alone.
    get pagesById() { return rowsById(rows); },
    rowsAreAll: true,
    pagePresentationById: {},
    propertyCellEnvironment,
    propertyValueContext,
    queryBounds: nativeBoardQueryBounds,
    dependencyEdges: [],
    dependencyPropertyId: dependency.propertyId,
    dependencyRevision: revision,
    dependencyNextCursor: null,
    dependencyHasMore: revision > 0,
    rowWindow,
  }), [subitems, rowWindow, contextIdentity, contextPageId, currentUserId, dataSource, dependency.propertyId, dependencySnapshotIdentity, pageId, properties, propertyCellEnvironment, propertyValueContext, revision, rows, snapshotIdentity, view, workspaceId]);
  const currentSnapshot = useRef(snapshot);
  currentSnapshot.current = snapshot;
  const operations = useMemo<TimelineViewOperations>(() => {
    const isCurrent = (context: TimelineViewOperationContext) => {
      const current = currentSnapshot.current;
      return !context.signal.aborted
        && context.contextIdentity === current.contextIdentity
        && context.snapshotIdentity === current.snapshotIdentity
        && context.dependencySnapshotIdentity === current.dependencySnapshotIdentity
        && context.requestGeneration === current.requestGeneration
        && context.dataSourceId === current.dataSource.id
        && context.viewId === current.view.id;
    };
    return {
      // The rows shown take it in as it shows, with its title's editor (TimelineView): drawn together.
      createDatedRow: async (dayKey, seeds, context, alongside) => {
        let shown = false;
        const along = (row: GraphRow) => {
          shown = true;
          // Named among the rows made here only where the rows shown would leave it out: the
          // timeline isn't drawn again for it, ahead of its rows (2026-11-15: a timeline of 31
          // rows drew its every bar again for it inside the press, ~20 ms at CPU ×2).
          const current = windowRef.current;
          if (current && topRowCount(currentSnapshot.current.rows) >= current.count) widen(() => ({ count: current.count, made: new Set([...current.made, row.id]) }));
          // In a view keeping its own order it goes after what the view shows, in the same drawing, as
          // Notion's timeline puts it under its other bars by "+ 새로 만들기" (seen 2026-11-15 at 768 and 1440).
          const now = currentSnapshot.current;
          if (viewKeepsOwnRowOrder(now.view.config)) void adapter?.updateView({ ...now.view, config: { ...now.view.config,
            rowOrder: viewRowOrderPlacing(now.view.config, rowsInViewOrder(now.rows, now.view.config).map(({ id }) => id), [row.id]) } }, () => isCurrent(context)).catch(() => undefined);
          alongside?.(row);
        };
        const row = await adapter?.createDatedRow(dataSource, view, dayKey, seeds, () => isCurrent(context), along) ?? null;
        if (row && !shown) along(row);
        return row;
      },
      createSubitem: async (parentId, values, context) => {
        const row = await cellAdapter?.createRow(dataSource, parentId, null, () => isCurrent(context), values, undefined,
          subitems ? { parentPropertyId: subitems.parentPropertyId, childrenPropertyId: subitems.childrenPropertyId } : undefined) ?? null;
        if (row) widen((current) => ({ count: current.count, made: new Set([...current.made, row.id]) }));
        // In a view keeping its own order it goes after what the view shows, as a table's does.
        if (row && viewKeepsOwnRowOrder(view.config)) {
          void adapter?.updateView({ ...view, config: { ...view.config, rowOrder: viewRowOrderPlacing(view.config, rowsInViewOrder(rows, view.config).map(({ id }) => id), [row.id]) } }, () => isCurrent(context)).catch(() => undefined);
        }
        return row;
      },
      // A table row's "+": made as a table's row "+" makes one, by its writer beside the row, among its parent's rows,
      // where the view keeps its own order put beside it there too, in the same drawing (TableView's addRow).
      createRowBeside: cellAdapter && !readOnly ? async (beside, seeds, context, alongside) => {
        const now = currentSnapshot.current, parentId = now.rows.find((row) => row.id === beside.id)?.subitemParentId ?? null;
        const title = now.properties.find((property) => property.dataSourceId === dataSource.id && property.type === "title");
        const values = { ...seeds.properties, ...(title && seeds.title != null ? { [title.id]: seeds.title } : {}) };
        let shown = false;
        const along = (row: GraphRow) => {
          shown = true;
          widen((current) => ({ count: current.count, made: new Set([...current.made, row.id]) }));
          // In a sorted view it asks whether the sort goes, as a table's row "+" does (rowMadeBeside).
          const placed = viewKeepsOwnRowOrder(now.view.config) ? { ...now.view, config: { ...now.view.config,
            rowOrder: viewRowOrderPlacing(now.view.config, rowsInViewOrder(now.rows, now.view.config).map(({ id }) => id), [row.id], beside) } } : null;
          if (placed) void adapter?.updateView(placed, () => isCurrent(context)).catch(() => undefined);
          alongside?.(row, Boolean(placed && !parentId && cellAdapter.rowMadeBeside?.(placed)));
        };
        const row = await cellAdapter.createRow(dataSource, parentId, beside.side === "after" ? beside.id : null, () => isCurrent(context), values,
          beside.side === "before" ? beside.id : undefined, subitems ? { parentPropertyId: subitems.parentPropertyId, childrenPropertyId: subitems.childrenPropertyId } : undefined,
          false, false, along) ?? null;
        if (row && !shown) along(row);
        return row;
      } : undefined,
      // A paste's rows, by its table's writer as a table's (nativeTableProductAdapter pasteRows): made among the parent's rows of
      // the row they go after, and after it where the view keeps its own order, in the same step.
      pasteRows: cellAdapter?.pasteRows && !readOnly ? async (changed, made, context) => {
        const now = currentSnapshot.current, parentId = now.rows.find((row) => row.id === made.after)?.subitemParentId ?? null;
        const ids = await cellAdapter.pasteRows!(dataSource, changed, { parentId, afterRowId: made.after, seeds: made.seeds, rows: made.rows }, () => isCurrent(context),
          subitems ? { parentPropertyId: subitems.parentPropertyId, childrenPropertyId: subitems.childrenPropertyId } : undefined);
        if (ids.length) widen((current) => ({ count: current.count, made: new Set([...current.made, ...ids]) }));
        if (ids.length && viewKeepsOwnRowOrder(now.view.config)) void adapter?.updateView({ ...now.view, config: { ...now.view.config, rowOrder: viewRowOrderPlacing(now.view.config,
          rowsInViewOrder(now.rows, now.view.config).map(({ id }) => id), ids, { id: made.after, side: "after" }) } }, () => isCurrent(context)).catch(() => undefined);
      } : undefined,
      discardRow: cellAdapter?.discardRow && !readOnly ? (rowId) => void cellAdapter.discardRow!(rowId) : undefined,
      createDatePropertyAndSelect: (name, context) => adapter?.createDatePropertyAndSelect(dataSource, view, name, () => isCurrent(context)) ?? Promise.resolve(),
      updateView: (next, context, step) => adapter?.updateView(next, () => isCurrent(context), step) ?? Promise.resolve(),
      moveOrResizeRow: (rowId, values, context, alongside) => adapter?.moveOrResizeRow(rowId, values, () => isCurrent(context), alongside) ?? Promise.resolve(),
      // Rows moved in its table or by a bar, as a list's rows move (moveShownRows): among the rows as the timeline shows them,
      // another group's value in the same step of ⌘Z, and the dates its bar was carried along the days to (`withDates`).
      moveRows: cellAdapter && adapter && !readOnly ? (rowIds, target, shownIds, context, values, withDates) => {
        const now = currentSnapshot.current, byId = rowsById(now.rows);
        return moveShownRows(cellAdapter, now.view, shownIds.flatMap((id) => byId[id] ?? []), rowIds, target, () => isCurrent(context), movedRowsValues(now.properties, values), withDates);
      } : undefined,
      createDependency: (successor, predecessor, expectedRevision, context, id) => adapter?.createDependency(dataSource, successor, predecessor, expectedRevision, () => isCurrent(context), id) ?? Promise.resolve(),
      deleteDependency: (edgeIds, expectedRevision, context) => adapter?.deleteDependency(dataSource, edgeIds, expectedRevision, () => isCurrent(context)) ?? Promise.resolve(false),
      async loadMoreDependencies(cursor, visibleRowIds, context) {
        if (!adapter || revision < 1 || visibleRowIds.length === 0) {
          return { dependencySnapshotIdentity: context.dependencyVisibleIdentity, edges: [], hasMore: false, nextCursor: null };
        }
        const response = await adapter.loadDependencies({
          cursor,
          dataSourceId: dataSource.id,
          expectedRevision: revision,
          signal: context.signal,
          visibleRowIds,
        });
        return {
          dependencySnapshotIdentity: context.dependencyVisibleIdentity,
          edges: response.edges,
          hasMore: response.hasMore,
          nextCursor: response.nextCursor,
        };
      },
      isCurrent,
      onError,
      openRow: (rowId) => onOpenRow(rowId, "default"),
      openRowIn: onOpenRow,
      openPageIn: effectiveOpenPageIn(view),
      closeRow: onCloseRow,
      openRowContextMenu,
      openRowContextMenuFromElement,
      rowContextMenu,
      commitProperty: cellAdapter && !readOnly ? (property, context) => cellAdapter.commitProperties([property], () => isCurrent(context)) : undefined,
      trashRows: readOnly ? undefined : trashRows,
      duplicateRows: readOnly ? undefined : duplicateRows,
      openGroupSettings: onOpenGroupSettings,
      // As many more as it first loads, as Notion's (seen 2026-11-03).
      loadMoreRows: () => widen((current) => ({ count: current.count + (loadLimit ?? TABLE_LOAD_MORE_ROWS), made: current.made })),
      setRowLoadLimit: readOnly || !adapter ? undefined
        : (limit) => void adapter.updateView({ ...view, config: { ...view.config, initialLoadLimit: limit } }, () => true, true).catch(onError),
    };
  }, [subitems, readOnly, windowKey, adapter, cellAdapter, dataSource, dependencySnapshotIdentity, onError, onOpenGroupSettings, onOpenRow, onCloseRow, openRowContextMenu, openRowContextMenuFromElement, revision, rowContextMenu, trashRows, duplicateRows, view]);
  return <TimelineView snapshot={snapshot} operations={operations} readOnly={readOnly || !adapter} />;
}
