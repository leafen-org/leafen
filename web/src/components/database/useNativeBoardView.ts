import { type MutableRefObject, useMemo, useRef } from "react";
import type { GraphBlock, GraphDataSource, GraphProperty, GraphRow, GraphView } from "@/contentGraph";
import { useWorkspaceFile } from "@/lib/fileUrls";
import type { NativeBoardProductAdapter } from "@/lib/nativeBoardProductAdapter";
import type { NativeTableProductAdapter } from "@/lib/nativeTableProductAdapter";
import type { DatabaseSubitemsFeature } from "@/lib/types";
import type { BoardRowOpenMode, BoardViewOperationContext, BoardViewOperations, BoardViewSnapshot } from "./BoardView";
import { nativeBoardGroupingBounds, nativeBoardQueryBounds } from "./boardViewModel";
import type { ViewNoResultsCommands } from "./DatabaseNoResults";
import type { DatabaseViewOpenMode } from "./DatabaseView";
import type { DatabaseToolbarCommands } from "./databaseToolbarModel";
import { effectiveOpenPageIn } from "./databaseViewShared";
import type { PropertyValueReadContext } from "./PropValue";
import type { PropertyCellEnvironment } from "./propertyCellTypes";
import type { useRowContextMenu } from "./useRowContextMenu";
import type { DatabaseFilterSnapshot } from "./databaseFilterSnapshot";
import { rowsById } from "./presentedPages";

/**
 * A database's board view: what it reads (its snapshot) and what it writes
 * (its operations), from the mount's own view, rows and adapters
 * (NativeDatabaseViewMount).
 */
export function useNativeBoardView(input: {
  block: GraphBlock;
  source: GraphDataSource;
  activeView: GraphView;
  view: GraphView;
  properties: GraphProperty[];
  rows: GraphRow[];
  snapshotIdentity: string;
  contextIdentity: string;
  requestGeneration: number;
  workspaceId: string;
  containerPageId?: string;
  currentUserId?: string;
  subitems: DatabaseSubitemsFeature | null;
  propertyValueContext: PropertyValueReadContext;
  filterSnapshot: DatabaseFilterSnapshot;
  cellEnvironment?: PropertyCellEnvironment;
  readOnly: boolean;
  boardAdapter?: NativeBoardProductAdapter;
  tableAdapter?: NativeTableProductAdapter;
  noResults: ViewNoResultsCommands;
  rowMenu: ReturnType<typeof useRowContextMenu>;
  toolbarCommands: MutableRefObject<DatabaseToolbarCommands | null>;
  openRow(rowId: string, mode?: DatabaseViewOpenMode): void;
  changeRowTrash(rowIds: string | readonly string[], trash: boolean): Promise<boolean>;
  onError(error?: unknown): void;
}) {
  const { block, source, activeView, view, properties, rows, snapshotIdentity, contextIdentity, requestGeneration, workspaceId,
    containerPageId, currentUserId, subitems, propertyValueContext, filterSnapshot, cellEnvironment, readOnly, boardAdapter, tableAdapter,
    noResults, rowMenu, toolbarCommands } = input;
  const snapshot = useMemo<BoardViewSnapshot>(() => ({
    snapshotIdentity,
    contextIdentity,
    requestGeneration,
    workspaceId,
    dataSource: source,
    view,
    properties,
    rows,
    // Made when a board reads it: a table on the same rows never does
    // (2026-11-11: 100,000 rows paired by ID for each edit that arrived).
    get pagesById() { return rowsById(rows); },
    rowsAreAll: true,
    pagePresentationById: {},
    pageContentPreviews: {},
    // Cards show relation titles and rollups as the table does, from the
    // related pages the view has read.
    propertyValueContext,
    queryBounds: nativeBoardQueryBounds,
    groupingBounds: nativeBoardGroupingBounds,
    useWorkspaceFile,
    currentUserId,
    // "Current page" is the page the view is on, a linked view's own.
    contextPageId: containerPageId,
    databaseFeatures: subitems ? { subitems } : undefined,
    filterSnapshot,
    sourceContainerId: typeof block.content.sourceContainerId === "string" ? block.content.sourceContainerId : block.id,
    propertyCellEnvironment: readOnly ? undefined : cellEnvironment,
  }), [block.content.sourceContainerId, block.id, containerPageId, contextIdentity, properties, filterSnapshot, readOnly, cellEnvironment, propertyValueContext, rows, currentUserId, requestGeneration, snapshotIdentity, source, subitems, view, workspaceId]);
  const current = useRef(snapshot);
  current.current = snapshot;
  // The latest of what changes every render, read when an operation runs.
  const latest = useRef(input);
  latest.current = input;
  const operations = useMemo<BoardViewOperations>(() => {
    const isCurrent = (context: BoardViewOperationContext) => {
      const now = current.current;
      return !context.signal.aborted && context.contextIdentity === now.contextIdentity
        && context.snapshotIdentity === now.snapshotIdentity && context.requestGeneration === now.requestGeneration
        && context.dataSourceId === now.dataSource.id && context.viewId === now.view.id;
    };
    const openRow = (rowId: string, mode?: DatabaseViewOpenMode) => latest.current.openRow(rowId, mode);
    const openRowIn = (rowId: string, mode: BoardRowOpenMode) => openRow(rowId, mode === "default" ? effectiveOpenPageIn(view) : mode);
    return {
      // A group's settings are the view settings' "그룹화", a sub-group's their "하위 그룹화".
      openGroupSettings: (_trigger, subgroup) => toolbarCommands.current?.openViewSettings(subgroup ? "subGroup" : "group"),
      ...noResults,
      trashRows: async (ids, context) => { if (isCurrent(context) && !readOnly) await latest.current.changeRowTrash(ids, true); },
      duplicateRows: async (ids, context, place) => isCurrent(context) && !readOnly ? tableAdapter?.duplicateRows(ids, () => isCurrent(context), activeView, place) : undefined,
      setRowProperty: (rowId, propertyId, value, context) => {
        const row = rows.find(candidate => candidate.id === rowId), property = properties.find(candidate => candidate.id === propertyId);
        if (!row || !property || !isCurrent(context)) return Promise.resolve();
        return tableAdapter?.commitRows([{ ...row, values: { ...row.values, [propertyId]: { type: property.type, value } } }], () => isCurrent(context)) ?? Promise.resolve();
      },
      createGroupProperty: (type, context) => boardAdapter?.addProperty(source, activeView, type, () => isCurrent(context)) ?? Promise.resolve(null),
      createRowInGroup: (propertyId, value, seeds, context) => boardAdapter?.createRow(source, propertyId, value, seeds, () => isCurrent(context)) ?? Promise.resolve(null),
      updateGroupOptions: (propertyId, options, context, groupView) => boardAdapter?.updateOptions(propertyId, options, () => isCurrent(context), groupView) ?? Promise.resolve(),
      moveRow: (rowId, change, context) => boardAdapter?.moveRow(rowId, change, () => isCurrent(context)) ?? Promise.resolve(),
      moveCards: boardAdapter?.moveCards ? (moves, context) => boardAdapter.moveCards!(moves, () => isCurrent(context)) : undefined,
      updateView: (next, context, step) => boardAdapter?.updateView(next, () => isCurrent(context), step) ?? Promise.resolve(),
      isCurrent,
      onError: (error) => latest.current.onError(error),
      openRow: (rowId) => openRow(rowId, effectiveOpenPageIn(view)),
      openRowIn,
      openRowContextMenu: rowMenu.openRowContextMenu,
      openRowContextMenuFromElement: rowMenu.openRowContextMenuFromElement,
      rowContextMenu: rowMenu.rowContextMenu,
    };
  }, [activeView, boardAdapter, noResults, properties, readOnly, rowMenu, rows, source, tableAdapter, toolbarCommands, view]);
  return { snapshot, operations };
}
