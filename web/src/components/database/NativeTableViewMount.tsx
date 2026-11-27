"use client";

import { useCallback, useContext, useMemo, useRef, useState, type MouseEvent as ReactMouseEvent, type ReactNode } from "react";
import { RELATION_CREATION_BOUNDS, relationDatabaseOptions, type RelationDatabaseOption } from "@/lib/relationPropertyCreationModel";
import type { GraphDataSource, GraphProperty, GraphRow, GraphView } from "@/contentGraph";
import type { NativeTableProductAdapter } from "@/lib/nativeTableProductAdapter";
import type { DbProperty, Page, ViewConfig } from "@/lib/types";
import {
  TableView,
  type TableRowOpenMode,
  type TableSubitemWindow,
  type TableViewOperationContext,
  type TableViewOperations,
  type TableViewSnapshot,
} from "./TableView";
import { nativeBoardQueryBounds } from "./boardViewModel";
import { graphRowsIdentity, nativeDatabaseFilterSnapshot, type DatabaseFilterGraph } from "./databaseFilterSnapshot";
import { useDatabaseOwnPage } from "./databaseFullPageLink";
import { GROUPED_TABLE_INITIAL_ROWS, PHONE_GROUP_ROWS, TABLE_LOAD_MORE_ROWS, chosenInitialLoadLimit, firstLoadLimit, tableRowWindow, topRowCount } from "./queryProjection";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { useNativePropertyCells } from "./useNativePropertyCells";
import { projectRows, rowsAsNow, useDeferredPlacing } from "./viewRowProjection";
import { viewQuery, type ViewNoResultsCommands } from "./DatabaseNoResults";
import { DashboardFilteredContext } from "./dashboardWidgetHeight";
import { galleryPresentationPage } from "./galleryViewModel";
import { subitemDisplayMode, subitemFilterScope } from "./subitemFilterScope";
import { markFreshRow, watchFreshTableRow } from "./freshRows";
import type { SelectedRowsMenu } from "./useRowContextMenu";
import { useRowDisplayTitle } from "./untitledRowName";
import { rowsUnderParents } from "./tableViewModel";
import { HeldRowSourceContext } from "./heldRowSource";

type NativeTableViewMountProps = {
  adapter?: NativeTableProductAdapter;
  /** Makes a property of the table's data source; a relation's header "+" makes it through the relation dialog. */
  onAddProperty?: (type: GraphProperty["type"], name: string, config?: Record<string, unknown>) => Promise<GraphProperty | null>;
  searchRelationDatabases?: (query: string, signal: AbortSignal) => Promise<RelationDatabaseOption[]>;
  /** Opens the view's properties menu beside the header's "⋯". */
  onOpenProperties?: (trigger: HTMLElement) => void;
  /** Filters the view by a property in its filter bar. */
  onFilterProperty?: (propertyId: string) => void;
  /** Opens the grouping settings, for a group's "그룹 편집하기". */
  onOpenGroupSettings?: (trigger: HTMLElement) => void;
  currentUserId?: string;
  dataSource: GraphDataSource;
  dataSources: readonly GraphDataSource[];
  filterGraph: DatabaseFilterGraph;
  onError(error?: unknown): void;
  onOpenRow(rowId: string, mode: TableRowOpenMode): void;
  /** Closes the row its side peek shows. */
  onCloseRow?(): void;
  openRowContextMenu(rowId: string, event: ReactMouseEvent<HTMLElement>, duplicate?: () => void): void;
  openRowContextMenuFromElement(rowId: string, element: HTMLElement, duplicate?: () => void): void;
  /** Several selected rows' menu, from their bar's "⋯". */
  openRowsContextMenuAt?(rows: SelectedRowsMenu, anchor: { x: number; y: number; top?: number }, returnTo: HTMLElement): void;
  /** Moves rows to the trash, as their menus do; without it they are deleted. */
  trashRows?(rowIds: readonly string[]): Promise<void>;
  pageId: string;
  /** The page the view is on, a linked view's own: "current page" in its filters. */
  contextPageId?: string;
  properties: readonly GraphProperty[];
  readOnly: boolean;
  rowContextMenu: ReactNode;
  rows: readonly GraphRow[];
  staticReadOnlySemantics?: boolean;
  /** Whether the source's sub-items are on. */
  subitemsEnabled?: boolean;
  /** A task database's: its last row reads "+ 새 작업". */
  taskDatabase?: boolean;
  /** The sub-item relations a row made under another shows at once, and whether their toggles stand on the title
   * ("제목에 중첩 토글 표시"; off, in the table's first column). */
  subitemPair?: { parentPropertyId: string; childrenPropertyId: string; onTitle?: boolean };
  view: GraphView;
  workspaceId: string;
  /** The toolbar's filter menu and search, for a table its query leaves empty. */
  noResults?: ViewNoResultsCommands;
};

function subitemWindows(rows: readonly GraphRow[], snapshotIdentity: string) {
  const children = new Map<string, string[]>();
  for (const row of rowsUnderParents(rows)) {
    if (!row.subitemParentId) continue;
    const ids = children.get(row.subitemParentId) ?? [];
    ids.push(row.id);
    children.set(row.subitemParentId, ids);
  }
  return Object.fromEntries([...children].map(([parentId, rowIds]) => [parentId, {
    hasMore: false,
    nextCursor: null,
    rowIds,
    snapshotIdentity,
  } satisfies TableSubitemWindow]));
}

export function NativeTableViewMount({
  noResults,
  adapter,
  onAddProperty,
  searchRelationDatabases,
  onOpenProperties,
  onFilterProperty,
  onOpenGroupSettings,
  currentUserId,
  dataSource,
  dataSources,
  filterGraph,
  onError,
  onOpenRow,
  onCloseRow,
  openRowContextMenu,
  openRowContextMenuFromElement,
  openRowsContextMenuAt,
  trashRows,
  pageId,
  contextPageId,
  properties,
  readOnly,
  rowContextMenu,
  rows,
  staticReadOnlySemantics,
  subitemsEnabled,
  taskDatabase,
  subitemPair,
  view,
  workspaceId,
}: NativeTableViewMountProps) {
  const snapshotIdentity = useMemo(
    () => JSON.stringify([dataSource, view, properties]) + graphRowsIdentity(rows),
    [dataSource, properties, rows, view],
  );
  const contextIdentity = `${currentUserId ?? "anonymous"}:${workspaceId}:${pageId}:${dataSource.id}:${view.id}`;
  const generation = useRef({ identity: "", value: 0 });
  if (generation.current.identity !== snapshotIdentity) {
    generation.current = { identity: snapshotIdentity, value: generation.current.value + 1 };
  }
  // The view's order is worked out behind a press (useDeferredValue: in
  // pieces, dropped for a newer press), from the rows and the view as last
  // placed: a value picked shows in its cell at once and its row moves, goes
  // or comes as soon as the order is worked out, and a filter or sort picked
  // lights its pill at once, the rows following (user decision 2026-11-12;
  // Notion's moves the row some 40 ms after the pick, seen 2026-11-12).
  const placing = useDeferredPlacing(rows, view);
  // With sub-items, the rows shown around those the filter keeps fade.
  const { placedRows, fadedRowIds } = useMemo(() => {
    const scope = subitemsEnabled ? { faded: new Set<string>() as ReadonlySet<string> } : undefined;
    // A filter or sort on a formula or a rollup reads through the related databases.
    const related = properties.some(({ type }) => type === "formula" || type === "rollup")
      ? nativeDatabaseFilterSnapshot({ currentUserId: currentUserId ?? "", graph: filterGraph, snapshotIdentity: "", workspaceId }) : undefined;
    const projected = projectRows({ currentUserId, pageId: contextPageId ?? pageId, properties, rows: placing.rows, view: placing.view, workspaceId, subitems: scope,
      related: related && { pagesById: related.pagesById as Record<string, Page | undefined>, propsByDb: related.propertiesByDatabase } });
    return { placedRows: projected, fadedRowIds: scope?.faded };
  }, [contextPageId, currentUserId, filterGraph, pageId, placing, properties, subitemsEnabled, workspaceId]);
  // Each placed row as it is now: values changed since show in their places.
  const projectedRows = useMemo(() => rowsAsNow(placedRows, placing.rows, rows), [placedRows, placing.rows, rows]);
  // An inline table shows its first rows, 50 unless its view says, and as
  // many more at each "더 불러오기", as Notion's "처음 불러오기" (seen
  // 2026-10-01, 11-03);
  // a row made here shows at once. A grouped one shows each group's first
  // 25 and 50 more at its own "더 불러오기". A database's own page shows
  // every row, but on a touch screen, a phone's or a tablet's, its groups'
  // first 10 and 10 more at a time (PHONE_GROUP_ROWS; an iPad's at 834 as a
  // phone's, seen 2026-11-16: a 14-row group 10 and "↓ 더 불러오기").
  const ownPage = useDatabaseOwnPage(), touchGroups = useSidebarLayout(true).touch && ownPage;
  const grouped = properties.some((property) => property.id === view.config.groupBy);
  const loadLimit = ownPage || grouped ? undefined : firstLoadLimit(view);
  const windowKey = `${view.id}:${loadLimit ?? "all"}`;
  const [rowWindow, setRowWindow] = useState<{ key: string; count: number; made: ReadonlySet<string> }>({ key: "", count: 0, made: new Set() });
  const currentWindow = rowWindow.key === windowKey ? rowWindow : { key: windowKey, count: loadLimit ?? 0, made: new Set<string>() };
  const windowRef = useRef(currentWindow);
  windowRef.current = currentWindow;
  const projectedRowsRef = useRef(projectedRows);
  projectedRowsRef.current = projectedRows;
  const shownRows = useMemo(
    () => loadLimit === undefined ? { rows: projectedRows, hasMore: false } : tableRowWindow(projectedRows, currentWindow.count, currentWindow.made),
    [currentWindow.count, currentWindow.made, loadLimit, projectedRows],
  );
  // A flattened list's parents count their sub-items and its sub-items name their parent, as Notion's (seen 2026-10-11).
  const rowTitle = useRowDisplayTitle();
  const flatSubitems = useMemo(() => {
    if (subitemsEnabled !== true || subitemDisplayMode(view.config) !== "flattened") return undefined;
    const counts = new Map<string, number>();
    for (const row of rows) if (row.subitemParentId) counts.set(row.subitemParentId, (counts.get(row.subitemParentId) ?? 0) + 1);
    const titleProperty = properties.find((property) => property.type === "title");
    const titles = new Map(rows.map((row, position) => [row.id, rowTitle(galleryPresentationPage({ row, titleProperty, workspaceId, position }))]));
    return { scope: subitemFilterScope(view.config), counts, titles };
  }, [properties, rowTitle, rows, subitemsEnabled, view.config, workspaceId]);
  const dashboardFiltered = useContext(DashboardFilteredContext);
  const snapshot = useMemo<TableViewSnapshot>(() => ({
    snapshotIdentity,
    contextIdentity,
    requestGeneration: generation.current.value,
    workspaceId,
    currentUserId,
    contextPageId: contextPageId ?? pageId,
    dataSource,
    view,
    properties,
    rows: shownRows.rows,
    sourceRows: rows,
    query: viewQuery({ config: view.config as ViewConfig }, properties as unknown as readonly DbProperty[], nativeBoardQueryBounds, dashboardFiltered),
    subitemsFlat: subitemsEnabled === true && ["flattened", "disabled"].includes(subitemDisplayMode(view.config)),
    flatSubitems,
    viewRows: loadLimit === undefined ? undefined : projectedRows,
    rowLoadLimit: loadLimit,
    groupRowLimit: grouped && (!ownPage || touchGroups) ? chosenInitialLoadLimit(view) ?? (touchGroups ? PHONE_GROUP_ROWS : GROUPED_TABLE_INITIAL_ROWS) : undefined,
    groupRowStep: chosenInitialLoadLimit(view) ?? (touchGroups ? PHONE_GROUP_ROWS : TABLE_LOAD_MORE_ROWS),
    madeRowIds: currentWindow.made,
    rowHasMore: shownRows.hasMore,
    rowNextCursor: null,
    subitemWindows: subitemWindows(projectedRows, snapshotIdentity),
    subitemsEnabled: subitemsEnabled && subitemPair?.onTitle !== true ? "first" : subitemsEnabled,
    taskDatabase,
    fadedRowIds,
    formulaBounds: nativeBoardQueryBounds.formula,
    // The header's "+" makes a relation as the toolbar's properties menu
    // does: to a database this page shows or one found by name.
    relationCreation: onAddProperty && !readOnly ? {
      snapshot: {
        databases: relationDatabaseOptions(dataSources),
        snapshotIdentity,
      },
      context: {
        bounds: RELATION_CREATION_BOUNDS,
        identity: contextIdentity,
        notifyFailure: onError,
        // A relation picked for a property turned into one updates it; any
        // other makes a new property.
        commit: async (command) => {
          const existing = command.existingPropertyId
            ? properties.find((candidate) => candidate.id === command.existingPropertyId)
            : undefined;
          if (!existing) return onAddProperty("relation", command.name, command.propertyConfig);
          const updated = { ...existing, type: "relation" as const, name: command.name, config: command.propertyConfig };
          await adapter?.commitProperties([updated], () => true);
          return updated;
        },
        searchDatabases: searchRelationDatabases,
      },
    } : undefined,
  }), [adapter, contextIdentity, contextPageId, currentUserId, dashboardFiltered, dataSource, dataSources, fadedRowIds, flatSubitems, loadLimit, onAddProperty, onError, pageId, touchGroups, projectedRows, properties, readOnly, rows,
    searchRelationDatabases, shownRows, snapshotIdentity, subitemsEnabled, subitemPair?.onTitle, taskDatabase, view, workspaceId]);
  const { propertyCellEnvironment, propertyValueContext } = useNativePropertyCells({
    // A locked page's table keeps its cells' ways: they only show their values (cellReadOnly.ts).
    adapter, readOnly, contextIdentity, currentUserId, dataSource, dataSources, filterGraph, pageId, properties,
    requestGeneration: snapshot.requestGeneration, rows: projectedRows, workspaceId,
  });
  const currentSnapshot = useRef(snapshot);
  currentSnapshot.current = snapshot;
  snapshot.propertyCellEnvironment = propertyCellEnvironment;
  snapshot.propertyValueContext = propertyValueContext;
  // Read when a row is made, so the operations stay put while the sub-items' settings load.
  const subitemPairRef = useRef(subitemPair);
  subitemPairRef.current = subitemPair;
  const operations = useMemo<TableViewOperations>(() => {
    const isCurrent = (context: TableViewOperationContext) => {
      const current = currentSnapshot.current;
      return !context.signal.aborted
        && context.contextIdentity === current.contextIdentity
        && context.snapshotIdentity === current.snapshotIdentity
        && context.requestGeneration === current.requestGeneration
        && context.dataSourceId === current.dataSource.id
        && context.viewId === current.view.id;
    };
    return {
      // The rows shown take it in as it shows, with what the table changes alongside (TableView).
      createRow: async (parentId, afterRowId, context, seeds, beforeRowId, atOnce, alongside) => {
        let shown = false;
        const along = (row: GraphRow) => {
          shown = true;
          // Named among the rows made here only where the rows shown would leave it out, a
          // group's first rows or the table's: the table isn't drawn again for it, ahead of its rows.
          if (!parentId && (grouped || (loadLimit !== undefined && topRowCount(projectedRowsRef.current) >= windowRef.current.count))) {
            setRowWindow({ ...windowRef.current, made: new Set([...windowRef.current.made, row.id]) });
          }
          alongside?.(row);
        };
        const row = await (adapter?.createRow(dataSource, parentId, afterRowId, () => isCurrent(context), seeds, beforeRowId, subitemPairRef.current, atOnce, !atOnce, along)
          ?? Promise.resolve(null));
        if (row && !shown) along(row);
        return row;
      },
      commitRows: (changed, context, typed) => adapter?.commitRows(changed, () => isCurrent(context), typed) ?? Promise.resolve(),
      // A paste's rows made past those it fills are named among the rows made here, as createRow's are.
      pasteRows: adapter?.pasteRows ? async (changed, made, context) => {
        const ids = await adapter.pasteRows!(dataSource, changed, made, () => isCurrent(context), subitemPairRef.current);
        if (ids.length && !made.parentId && (grouped || loadLimit !== undefined)) setRowWindow({ ...windowRef.current, made: new Set([...windowRef.current.made, ...ids]) });
        return ids;
      } : undefined,
      moveRows: adapter?.moveRows ? (moved, next, rowIds, context, values) => adapter.moveRows!(moved, next, rowIds, () => isCurrent(context), values) : undefined,
      rowMadeBeside: adapter?.rowMadeBeside ? (placed) => adapter.rowMadeBeside!(placed) : undefined,
      commitProperties: (changed, context, shownIn) => adapter?.commitProperties(changed, () => isCurrent(context), shownIn, context.viewId) ?? Promise.resolve(),
      deleteProperties: (ids, context) => adapter?.deleteProperties(ids, () => isCurrent(context)) ?? Promise.resolve(),
      updateView: (next, context, step) => adapter?.updateView(next, () => isCurrent(context), step) ?? Promise.resolve(),
      deleteRows: (ids, context) => (trashRows ? trashRows(ids) : adapter?.deleteRows(ids, () => isCurrent(context))) ?? Promise.resolve(),
      freshRow: (rowId, made) => { markFreshRow(rowId, pageId, made); watchFreshTableRow(rowId, (id) => void adapter?.discardRow?.(id)); },
      duplicateRows: (ids, context, place) => adapter?.duplicateRows(ids, () => isCurrent(context), view, place) ?? Promise.resolve(),
      loadMoreRows: (_cursor, context) => {
        // As many more as the view first loads, as Notion's "더 불러오기" does (seen 2026-11-03: 10 → 20).
        setRowWindow({ ...windowRef.current, count: windowRef.current.count + (loadLimit ?? TABLE_LOAD_MORE_ROWS) });
        return Promise.resolve({ hasMore: false, nextCursor: null, rows: [], snapshotIdentity: context.snapshotIdentity });
      },
      loadMoreSubitems: (_parentId, _cursor, context) => Promise.resolve({ hasMore: false, nextCursor: null, rows: [], snapshotIdentity: context.snapshotIdentity }),
      isCurrent,
      onError,
      openRow: (rowId) => onOpenRow(rowId, "default"),
      openRowIn: onOpenRow,
      closeRow: onCloseRow,
      openRowContextMenu,
      openRowContextMenuFromElement,
      openRowsContextMenuAt,
      rowContextMenu,
      openProperties: onOpenProperties,
      filterProperty: onFilterProperty,
      openGroupSettings: onOpenGroupSettings,
      ...noResults,
    };
  }, [adapter, dataSource, grouped, loadLimit, noResults, onError, onFilterProperty, onCloseRow, onOpenGroupSettings, onOpenProperties, onOpenRow, openRowContextMenu, openRowContextMenuFromElement, openRowsContextMenuAt, pageId, rowContextMenu, trashRows]);
  // A row whose value's editor is open, gone from these rows, closes it (useHeldCellEditor).
  const drawnFrom = useRef(rows);
  drawnFrom.current = rows;
  const inSource = useCallback((rowId: string) => drawnFrom.current.some((row) => row.id === rowId), []);
  return <HeldRowSourceContext.Provider value={inSource}>
    <TableView snapshot={snapshot} operations={operations} readOnly={readOnly || !adapter} staticReadOnlySemantics={staticReadOnlySemantics} />
  </HeldRowSourceContext.Provider>;
}
