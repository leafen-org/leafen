"use client";

import { useCommittedOperationScope } from "../product/useCommittedOperationScope";
import { viewKeepsOwnRowOrder, viewRowOrderPlacing, type CopiesPlace } from "./viewRowOrder";
import { moveShownRows, movedRowsValues } from "./viewRowMove";
import { firstViewDateSource } from "./viewDateSource";

import { useMemo, useRef, useState, type MouseEvent as ReactMouseEvent, type ReactNode } from "react";
import type { GraphProperty, GraphRow, GraphView } from "@/contentGraph";
import { createFeedPostService } from "@/lib/feedPostService";
import { useWorkspaceFile } from "@/lib/fileUrls";
import { dataSourcePage } from "@/lib/dataSourcePage";
import type { NativeTableProductAdapter } from "@/lib/nativeTableProductAdapter";
import type { PropertyValueSchemaReadOwner } from "@/lib/propertyValueReadModel";
import type { DatabaseSubitemsFeature, DbView, Page, ViewConfig } from "@/lib/types";
import type { Session } from "@/types";
import { api } from "@/api";
import { CalendarView, type CalendarViewOperationContext, type CalendarViewOperations, type CalendarViewSnapshot } from "./CalendarView";
import { ChartView } from "./ChartView";
import { FeedView } from "./CurrentDatabaseViews";
import type { ViewNoResultsCommands } from "./DatabaseNoResults";
import { MapView } from "./DatabaseMapView";
import type { PropertyCellEnvironment } from "./propertyCellTypes";
import { GalleryView, type GalleryViewOperationContext, type GalleryViewOperations, type GalleryViewSnapshot } from "./GalleryView";
import { ListView, type ListSubitemWindow, type ListViewOperationContext, type ListViewOperations, type ListViewSnapshot } from "./ListView";
import { nativeBoardQueryBounds } from "./boardViewModel";
import { effectiveOpenPageIn } from "./databaseViewShared";
import { graphViewFilterSeeds } from "./graphViewFilterSeeds";
import { projectRows, rowsAsNow, useDeferredPlacing } from "./viewRowProjection";
import { galleryPresentationProperty } from "./galleryViewModel";
import { idsWith, pagesOver, presentedRowPages, rowIds, rowsById } from "./presentedPages";
import { useDatabaseOwnPage } from "./databaseFullPageLink";
import { FEED_INITIAL_LOAD_LIMIT, firstLoadLimit, tableRowWindow, topRowCount } from "./queryProjection";
import type { CardsLoadMoreControl } from "./CardsLoadMore";
import type { PropertyValueReadContext } from "./PropValue";
import type { DatabaseViewOpenMode, DatabaseViewSnapshot } from "./DatabaseView";
import { useDatabaseStructureLocked } from "./databaseStructureLock";
import { makeRowThatOpens } from "./newViewRow";
import type { DatabaseFilterSnapshot } from "./databaseFilterSnapshot";

const schemaReadOwner: PropertyValueSchemaReadOwner = Object.freeze({ request: () => false, retire: () => {} });
// An imported list view's settings over however many properties and
// settings the database holds.
const importedViewBounds = Object.freeze({
  maxProperties: Number.MAX_SAFE_INTEGER,
  maxPropertySettings: Number.MAX_SAFE_INTEGER,
});

type Props = {
  snapshot: DatabaseViewSnapshot;
  session?: Session | null;
  currentUserId?: string;
  workspaceId: string;
  pageId: string;
  /** The page the view is on, a linked view's own: "current page" in its filters. */
  contextPageId?: string;
  tableAdapter?: NativeTableProductAdapter;
  /** The related pages and schemas the view has read, for relation titles and rollups on cards. */
  relationValueContext?: PropertyValueReadContext;
  /** The database's sub-items, when on: a list nests them, cards follow their view's option. */
  subitems?: DatabaseSubitemsFeature | null;
  onAddProperty?: (type: GraphProperty["type"], name: string, config?: Record<string, unknown>) => Promise<GraphProperty | null>;
  onError(error?: unknown): void;
  onOpenRow(rowId: string, mode: DatabaseViewOpenMode): void;
  openRowContextMenu(rowId: string, event: ReactMouseEvent<HTMLElement>, duplicate?: () => void): void;
  openRowContextMenuFromElement(rowId: string, element: HTMLElement, duplicate?: () => void): void;
  rowContextMenu: ReactNode;
  /** The toolbar's filter menu and search, for a list its query leaves empty. */
  noResults?: ViewNoResultsCommands;
  /** The toolbar's grouping settings, for a list group's "그룹 편집하기". */
  onOpenGroupSettings?(trigger: HTMLElement): void;
  /** Moves rows to the trash, for a list group's "휴지통으로 이동". */
  trashRows?(rowIds: readonly string[]): Promise<void>;
  duplicateRows?(rowIds: readonly string[], place?: CopiesPlace): Promise<readonly string[] | void>;
  /** Edits a value in place: a gallery card's property slots while its name is typed. */
  propertyCellEnvironment?: PropertyCellEnvironment;
  /** What a chart's group's rows window filters with, and where a view saved from it links. */
  chartRows?: { filterSnapshot: DatabaseFilterSnapshot; sourceContainerId: string };
};

const NO_PAGES: PropertyValueReadContext["pagesById"] = Object.freeze({});
const NO_PROPERTIES: PropertyValueReadContext["propsByDatabaseId"] = Object.freeze({});
const NO_PRESENTATION = Object.freeze({});
const NO_PREVIEWS = Object.freeze({});

function rowWindows(rows: readonly GraphRow[], identity: string) {
  const grouped = new Map<string, string[]>();
  for (const row of rows) {
    if (!row.subitemParentId) continue;
    const values = grouped.get(row.subitemParentId) ?? [];
    values.push(row.id);
    grouped.set(row.subitemParentId, values);
  }
  return Object.fromEntries([...grouped].map(([parentId, rowIds]) => [parentId, {
    queryIdentity: identity,
    rowIds,
    nextCursor: null,
    hasMore: false,
  } satisfies ListSubitemWindow]));
}

export function NativeCardDatabaseViewMount({
  snapshot,
  session,
  currentUserId,
  workspaceId,
  pageId,
  contextPageId,
  tableAdapter,
  relationValueContext,
  subitems = null,
  onAddProperty,
  onError,
  onOpenRow,
  openRowContextMenu,
  openRowContextMenuFromElement,
  rowContextMenu,
  noResults,
  onOpenGroupSettings,
  trashRows,
  duplicateRows,
  propertyCellEnvironment,
  chartRows,
}: Props) {
  const { dataSource, activeView: view, properties, rows: sourceRows } = snapshot;
  const subitemsEnabled = subitems !== null;
  // A card view shows the rows its filters and search keep, in its sorts'
  // order, as the table does; a list with sub-items shows those around
  // them its sub-item option says, faded. A chart and a dashboard read their own.
  // A value picked shows on its card or row at once, the order worked out
  // behind the press (useDeferredPlacing: in pieces a newer press drops; a
  // row made, moved or gone is placed at once), as a table's.
  const placing = useDeferredPlacing(sourceRows, view);
  const { placedRows, fadedRowIds } = useMemo(() => {
    if (placing.view.type === "chart" || placing.view.type === "dashboard") return { placedRows: placing.rows, fadedRowIds: undefined };
    const scope = placing.view.type === "list" && subitemsEnabled ? { faded: new Set<string>() as ReadonlySet<string> } : undefined;
    const rows = projectRows({ currentUserId, pageId: contextPageId ?? pageId, properties, rows: placing.rows, view: placing.view, workspaceId, subitems: scope,
      related: relationValueContext && { pagesById: relationValueContext.pagesById, propsByDb: relationValueContext.propsByDatabaseId } });
    // A map shows the top rows, with no word of it on its filter icon (Notion's, seen 2026-11-05).
    return { placedRows: placing.view.type === "map" && subitemsEnabled ? rows.filter((row) => !row.subitemParentId) : rows, fadedRowIds: scope?.faded };
  }, [contextPageId, currentUserId, pageId, placing, properties, relationValueContext, subitemsEnabled, workspaceId]);
  // Each placed row as it is now.
  const allRows = useMemo(() => rowsAsNow(placedRows, placing.rows, sourceRows), [placedRows, placing.rows, sourceRows]);
  // An inline list or gallery first shows 50 cards, or as many as its view
  // says, and as many more at each "더 불러오기", as a table does (Notion's,
  // seen 2026-10-01, 11-03); a card made here shows at once. A database's own page shows
  // them all, as does a grouped one.
  const ownPage = useDatabaseOwnPage();
  const grouped = properties.some((property) => property.id === view.config.groupBy);
  // A feed first shows 10 posts, or as many as its view says or was made with (firstLoadLimit), on any page (Notion's, seen 2026-10-05).
  const loadLimit = view.type === "feed" ? firstLoadLimit(view, FEED_INITIAL_LOAD_LIMIT)
    : ownPage || grouped || (view.type !== "list" && view.type !== "gallery") ? undefined : firstLoadLimit(view);
  const windowKey = `${view.id}:${loadLimit ?? "all"}`;
  const [cardWindow, setCardWindow] = useState<{ key: string; count: number; made: ReadonlySet<string> }>({ key: "", count: 0, made: new Set() });
  const currentWindow = cardWindow.key === windowKey ? cardWindow : { key: windowKey, count: loadLimit ?? 0, made: new Set<string>() };
  const windowRef = useRef(currentWindow);
  windowRef.current = currentWindow;
  const shown = useMemo(
    () => loadLimit === undefined ? { rows: allRows, hasMore: false } : tableRowWindow(allRows, currentWindow.count, currentWindow.made),
    [allRows, currentWindow.count, currentWindow.made, loadLimit],
  );
  const rows = shown.rows;
  // A locked database's map settings and first cards, part of its view, stay
  // as they are.
  const structureLocked = useDatabaseStructureLocked();
  const lifecycle = useCommittedOperationScope([snapshot.contextIdentity]);
  const loadMoreCards: CardsLoadMoreControl | undefined = shown.hasMore && loadLimit !== undefined ? {
    limit: loadLimit,
    // As many more as it first loads, as Notion's (seen 2026-11-03).
    onLoadMore: () => setCardWindow({ ...windowRef.current, count: windowRef.current.count + loadLimit }),
    onLimit: snapshot.readOnly || structureLocked || !tableAdapter ? undefined : (limit) => {
      void tableAdapter.updateView({ ...view, config: { ...view.config, initialLoadLimit: limit } }, () => !lifecycle.signal.aborted, true)
        .catch(onError);
    },
  } : undefined;
  const feedPostService = useMemo(() => session ? createFeedPostService(session) : null, [session]);
  const current = useRef(snapshot);
  current.current = snapshot;
  const isCurrent = (context: { contextIdentity: string; requestGeneration: number; signal: AbortSignal; snapshotIdentity: string; viewId?: string; dataSourceId?: string }) => {
    const next = current.current;
    return !context.signal.aborted
      && context.contextIdentity === next.contextIdentity
      && context.requestGeneration === next.requestGeneration
      && context.snapshotIdentity === next.snapshotIdentity
      && (!context.viewId || context.viewId === next.activeView.id)
      && (!context.dataSourceId || context.dataSourceId === next.dataSource.id);
  };
  // Every row the view has read, so a sub-item names a parent its filter
  // leaves out; made, as what follows from the rows, when they change, not
  // each time the view draws (presentedPages.ts).
  const pagesById = rowsById(sourceRows);
  // Relation titles and rollups read the related pages the view has read,
  // as the table's cells do; this database's own rows count as read.
  const hydratedRelationTargetIds = useMemo(() => idsWith(rowIds(allRows), relationValueContext?.hydratedRelationTargetIds),
    [allRows, relationValueContext?.hydratedRelationTargetIds]);
  const propertyValueContext = useMemo(() => ({
    bounds: nativeBoardQueryBounds.formula,
    contextIdentity: snapshot.contextIdentity,
    hydratedRelationTargetIds,
    pagesById: relationValueContext?.pagesById ?? NO_PAGES,
    propsByDatabaseId: relationValueContext?.propsByDatabaseId ?? NO_PROPERTIES,
    schemaReadOwner,
    snapshotIdentity: snapshot.snapshotIdentity,
    userId: currentUserId,
  }), [currentUserId, hydratedRelationTargetIds, relationValueContext?.pagesById, relationValueContext?.propsByDatabaseId, snapshot.contextIdentity, snapshot.snapshotIdentity]);
  const subitemWindows = useMemo(() => view.type === "list" ? rowWindows(rows, snapshot.snapshotIdentity) : undefined, [rows, snapshot.snapshotIdentity, view.type]);
  const shared = {
    snapshotIdentity: snapshot.snapshotIdentity,
    contextIdentity: snapshot.contextIdentity,
    requestGeneration: snapshot.requestGeneration,
    workspaceId,
    contextPageId: contextPageId ?? pageId,
    currentUserId,
    dataSource,
    view,
    properties,
    rows,
    pagesById,
    pagePresentationById: NO_PRESENTATION,
    propertyValueContext,
    queryBounds: nativeBoardQueryBounds,
    databaseFeatures: subitems ? { subitems } : undefined,
  };
  const openDefault = (rowId: string, mode?: "new_tab" | "side") => onOpenRow(rowId, mode ?? effectiveOpenPageIn(view));
  // A sub-item goes after its parent's others, a row after the last.
  // The cards shown take it in, and a view keeping its own order keeps it
  // where it was made (viewRowOrder.ts), as it shows, with what the view
  // changes alongside: drawn together (nativeTableRowSteps.ts).
  // Made by a row's "+" (`place.beside`) in a sorted view, it asks whether the sort goes (rowMadeBeside), told to `alongside`.
  const createRow = (seedValues: Readonly<Record<string, unknown>>, context: { signal: AbortSignal }, parentId: string | null = null,
    place?: { beforeRowId?: string; afterRowId?: string; beside?: boolean }, atOnce = false, alongside?: (row: GraphRow, asking?: boolean) => void) => {
    let shown = false;
    const along = (row: GraphRow) => {
      shown = true;
      // Named among the cards made here only where the cards shown would leave it out: the
      // view isn't drawn again for it, ahead of its rows (2026-11-14: a list of 30 drew whole).
      if (loadLimit !== undefined && topRowCount(allRows) >= windowRef.current.count) {
        setCardWindow({ ...windowRef.current, made: new Set([...windowRef.current.made, row.id]) });
      }
      let asking = false;
      if (tableAdapter && viewKeepsOwnRowOrder(view.config)) {
        const target = place?.afterRowId ? { id: place.afterRowId, side: "after" as const } : place?.beforeRowId ? { id: place.beforeRowId, side: "before" as const } : null;
        const placed = { ...view, config: { ...view.config, rowOrder: viewRowOrderPlacing(view.config, allRows.map(({ id }) => id), [row.id], target) } };
        void tableAdapter.updateView(placed, () => isCurrent(context as GalleryViewOperationContext)).catch(() => undefined);
        asking = Boolean(place?.beside && !parentId && tableAdapter.rowMadeBeside?.(placed));
      }
      alongside?.(row, asking);
    };
    return (tableAdapter?.createRow(
      dataSource,
      parentId,
      // A view keeping its own order puts the row at the database's end; its place is the view's alone.
      viewKeepsOwnRowOrder(view.config) ? null : place?.afterRowId ?? (parentId ? null : allRows.at(-1)?.id ?? null),
      () => isCurrent(context as GalleryViewOperationContext),
      seedValues,
      viewKeepsOwnRowOrder(view.config) ? undefined : place?.beforeRowId,
      undefined,
      atOnce,
      !atOnce,
      along,
    ) ?? Promise.resolve(null)).then((row) => {
      if (row && !shown) along(row);
      return row;
    });
  };
  // Moved in a list or gallery, several chosen together, as a table's rows move (moveShownRows): a step of ⌘Z either way, with another group's value (`values`, by row) in it.
  const moveRows = (rowIds: readonly string[], targetId: string, side: "before" | "after", context: { signal: AbortSignal }, values?: Readonly<Record<string, Readonly<Record<string, unknown>>>>) => tableAdapter
    ? moveShownRows(tableAdapter, view, allRows, rowIds, { id: targetId, side }, () => !context.signal.aborted && !lifecycle.signal.aborted, movedRowsValues(properties, values)) : Promise.resolve();
  const seedRecord = (seeds: { title?: string; properties: Record<string, unknown> }) => {
    const titleProperty = properties.find(({ type }) => type === "title");
    return {
      ...seeds.properties,
      ...(titleProperty && seeds.title != null ? { [titleProperty.id]: seeds.title } : {}),
    };
  };
  const updateRow = (rowId: string, propertyId: string, value: unknown, context: { signal: AbortSignal }) => {
    const row = allRows.find((candidate) => candidate.id === rowId);
    const property = properties.find((candidate) => candidate.id === propertyId);
    if (!row || !property) return Promise.resolve();
    return tableAdapter?.commitRows([{ ...row, values: { ...row.values, [propertyId]: { type: property.type, value } } }], () => isCurrent(context as CalendarViewOperationContext)) ?? Promise.resolve();
  };

  // A card or post starts with what the view's filters ask for, as a table's row does.
  const filterSeeds = () => graphViewFilterSeeds(properties, view,
    { currentUserId, currentPageId: contextPageId ?? pageId }, nativeBoardQueryBounds);
  if (view.type === "gallery") {
    const gallerySnapshot: GalleryViewSnapshot = {
      ...shared,
      pageContentPreviews: NO_PREVIEWS,
      useWorkspaceFile,
      propertyCellEnvironment,
    };
    const operations: GalleryViewOperations = {
      createRow: (context, seeds, place, alongside) => createRow({ ...filterSeeds(), ...seeds }, context, null, place, false, alongside),
      moveRows,
      setRowProperty: updateRow,
      ...noResults,
      ...(tableAdapter && !structureLocked ? {
        updateViewConfig: (config: Record<string, unknown>, context: GalleryViewOperationContext, step?: boolean) => tableAdapter.updateView({ ...view, config }, () => isCurrent(context), step),
        commitProperty: (property: GraphProperty, context: GalleryViewOperationContext) => tableAdapter.commitProperties([property], () => isCurrent(context)),
        openGroupSettings: onOpenGroupSettings,
      } : {}),
      trashRows,
      duplicateRows,
      loadMoreCards,
      isCurrent,
      onError,
      openRow: (rowId) => openDefault(rowId),
      openRowIn: (rowId, mode) => onOpenRow(rowId, mode === "default" ? effectiveOpenPageIn(view) : mode),
      openRowContextMenu,
      openRowContextMenuFromElement,
      rowContextMenu,
    };
    return <GalleryView snapshot={gallerySnapshot} operations={operations} readOnly={snapshot.readOnly || !tableAdapter} />;
  }

  if (view.type === "list") {
    const windows = subitemWindows ?? {};
    const listSnapshot: ListViewSnapshot = {
      ...shared,
      fadedRowIds,
      subitemsEnabled,
      subitemQueryIdentity: snapshot.snapshotIdentity,
      subitemWindowsByParent: windows,
      importedViewBounds,
      propertyCellEnvironment,
    };
    const localWindow = (parentId: string) => windows[parentId] ?? {
      queryIdentity: snapshot.snapshotIdentity, rowIds: [], nextCursor: null, hasMore: false,
    };
    const operations: ListViewOperations = {
      createRow: (seeds, context, parentId, place, atOnce, alongside) => createRow(seedRecord(seeds), context, parentId, place, atOnce, alongside),
      moveRows,
      loadMoreCards,
      isCurrent,
      loadSubitemRows: (parentId) => Promise.resolve(localWindow(parentId)),
      loadMoreSubitemRows: (parentId) => Promise.resolve(localWindow(parentId)),
      onError,
      openRow: (rowId) => openDefault(rowId),
      openRowIn: (rowId, mode) => onOpenRow(rowId, mode === "default" ? effectiveOpenPageIn(view) : mode),
      openRowContextMenu,
      openRowContextMenuFromElement,
      publishSubitemWindow: () => {},
      setRowProperty: updateRow,
      rowContextMenu,
      ...noResults,
      ...(tableAdapter && !structureLocked ? {
        updateViewConfig: (config: Record<string, unknown>, context: ListViewOperationContext, step?: boolean) => tableAdapter.updateView({ ...view, config }, () => isCurrent(context), step),
        commitProperty: (property: GraphProperty, context: ListViewOperationContext) => tableAdapter.commitProperties([property], () => isCurrent(context)),
        openGroupSettings: onOpenGroupSettings,
      } : {}),
      trashRows,
      duplicateRows,
    };
    return <ListView snapshot={listSnapshot} operations={operations} readOnly={snapshot.readOnly || !tableAdapter} />;
  }

  if (view.type === "calendar") {
    const calendarSnapshot: CalendarViewSnapshot = shared;
    // The date chosen is a step of ⌘Z; the one a date property just made for it is written with, part of making it.
    const updateView = (next: GraphView, context: CalendarViewOperationContext, step?: boolean) => tableAdapter?.updateView(next, () => isCurrent(context), step) ?? Promise.resolve();
    const operations: CalendarViewOperations = {
      async createDatePropertyAndSelect(name, context) {
        const property = await onAddProperty?.("date", name);
        if (property && isCurrent(context)) await updateView({ ...view, config: { ...view.config, calendarBy: property.id } }, context);
      },
      createDatedRow: (dayKey, seeds, context) => {
        // The date the calendar shows by when it names none (CalendarView).
        const propertyId = typeof view.config.calendarBy === "string"
          ? view.config.calendarBy
          : firstViewDateSource(properties)?.id;
        return createRow({ ...seedRecord(seeds), ...(propertyId ? { [propertyId]: dayKey } : {}) }, context);
      },
      isCurrent,
      moveRowDate: updateRow,
      onError,
      openRow: (rowId) => openDefault(rowId),
      openRowIn: (rowId, mode) => onOpenRow(rowId, mode === "default" ? effectiveOpenPageIn(view) : mode),
      openRowContextMenu,
      openRowContextMenuFromElement,
      selectDateProperty: (propertyId, context) => updateView({ ...view, config: { ...view.config, calendarBy: propertyId } }, context, true),
      rowContextMenu,
      trashRows,
      duplicateRows,
    };
    return <CalendarView snapshot={calendarSnapshot} operations={operations} readOnly={snapshot.readOnly || !tableAdapter} />;
  }

  const dbProperties = properties.map(galleryPresentationProperty);
  const titleProperty = properties.find(({ type }) => type === "title");
  // Its rows' pages, made again for the rows changed alone (presentedPages.ts); the views read them, never change them.
  const presented = presentedRowPages(rows, titleProperty, workspaceId);
  const pages = presented.pages as Page[], projectedPages = presented.byId;
  const db = dataSourcePage(dataSource, workspaceId);
  const dbView: DbView = { id: view.id, databaseId: view.dataSourceId, name: view.name, type: view.type as DbView["type"], position: 0, config: view.config as ViewConfig };
  const readSnapshot = {
    properties: dbProperties,
    propertyValueContext: {
      ...propertyValueContext,
      pagesById: pagesOver(propertyValueContext.pagesById, projectedPages),
      propsByDatabaseId: { ...propertyValueContext.propsByDatabaseId, [dataSource.id]: dbProperties },
    },
    queryBounds: nativeBoardQueryBounds,
  };
  if (view.type === "chart") {
    // A chart's settings change in its view's settings panel, as Notion's.
    return <ChartView db={db} view={dbView} snapshot={{
      snapshotIdentity: snapshot.snapshotIdentity,
      properties: dbProperties,
      rows: pages,
      pagesById: readSnapshot.propertyValueContext.pagesById,
      propertiesByDatabaseId: Object.fromEntries(Object.entries(readSnapshot.propertyValueContext.propsByDatabaseId).map(([id, properties]) => [id, Array.from(properties)])),
      currentUserId,
      queryBounds: nativeBoardQueryBounds,
      summaryValueBounds: { formula: nativeBoardQueryBounds.formula, projection: nativeBoardQueryBounds.formula.rollup },
    }} contextPageId={contextPageId ?? pageId} rowsSource={{
      // A group clicked opens its rows over the page, its rule first among the view's filters (Notion's, seen 2026-11-11).
      // Its "열기" opens a row in the middle over the window (Notion's, seen 2026-11-12).
      dataSource, properties: dbProperties, context: readSnapshot.propertyValueContext, onOpenRow: (rowId) => onOpenRow(rowId, "center"),
      filters: chartRows && {
        view, graphProperties: properties, snapshot: chartRows.filterSnapshot, lockedFilter: null, rows: pages,
        pagesById: readSnapshot.propertyValueContext.pagesById, bounds: nativeBoardQueryBounds, currentUserId,
        save: { containerId: chartRows.sourceContainerId, dataSourceId: dataSource.id },
      },
    }} />;
  }
  const currentProps = { db, view: dbView, rows: pages, onOpenRow: openDefault, snapshot: readSnapshot };
  if (view.type === "feed") {
    // Its "+ 새 페이지" opens the page it makes, as Notion's feed (seen 2026-10-11).
    const context = { contextIdentity: snapshot.contextIdentity, requestGeneration: snapshot.requestGeneration,
      snapshotIdentity: snapshot.snapshotIdentity, signal: lifecycle.signal, viewId: view.id };
    const addPost = snapshot.readOnly || !tableAdapter ? undefined
      : () => void makeRowThatOpens(() => createRow(filterSeeds(), context), openDefault).catch(onError);
    return <FeedView {...currentProps} loadMore={loadMoreCards} onAddRow={addPost}
      rowMenu={{ open: openRowContextMenuFromElement, menu: rowContextMenu }} posts={{
      service: feedPostService, readOnly: snapshot.readOnly || !session,
      analytics: session ? { workspaceId, pageId: dataSource.pageId ?? pageId } : undefined, session: session ?? undefined,
    }} />;
  }
  if (view.type === "map") {
    const selectMapBy = snapshot.readOnly || structureLocked || !tableAdapter ? undefined : (propertyId: string) => {
      void tableAdapter.updateView({ ...view, config: { ...view.config, mapBy: propertyId } }, () => !lifecycle.signal.aborted, true)
        .catch(onError);
    };
    // A pin placed in pin mode is a new page with that place, opened in the
    // center as Notion's (seen 2026-11-02).
    const context = { contextIdentity: snapshot.contextIdentity, requestGeneration: snapshot.requestGeneration,
      snapshotIdentity: snapshot.snapshotIdentity, signal: lifecycle.signal, viewId: view.id };
    // The page a pin makes takes the point's name, title and place, as Notion's
    // (seen 2026-11-02: "Decatur, KS, 미국"), where the server can name it.
    const addPin = snapshot.readOnly || !tableAdapter ? undefined : (propertyId: string, place: { lat: number; lon: number }) => {
      const titleProperty = dbProperties.find((property) => property.type === "title");
      const named = session ? api.reversePlace(session, dataSource.pageId ?? pageId, place).catch(() => null) : Promise.resolve(null);
      void named.then((found) => createRow({
        ...filterSeeds(),
        ...(found?.name && titleProperty ? { [titleProperty.id]: found.name } : {}),
        [propertyId]: found?.name || found?.address ? { ...place, name: found.name, address: found.address } : place,
      }, context))
        // The row made changes the view (in a template's body it leaves it): it opens while the view is there (newViewRow.ts).
        .then((row) => { if (row && !lifecycle.signal.aborted) onOpenRow(row.id, "center"); }).catch(onError);
    };
    return <MapView {...currentProps} onSelectMapBy={selectMapBy} onAddPin={addPin} />;
  }
  return null;
}
