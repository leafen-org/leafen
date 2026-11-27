"use client";

import { useCommittedOperationScope } from "../product/useCommittedOperationScope";
import {
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent as ReactKeyboardEvent,
  type MouseEvent as ReactMouseEvent,
  type ReactNode,
} from "react";
import { useTranslation } from "react-i18next";
import type { GraphDataSource, GraphProperty, GraphRow, GraphView } from "../../contentGraph";
import { useDataFindSource } from "@/lib/useDataFindSource";
import { holdTypingForField, holdTypingWhileRowOpens, newRowOpensItsPage, requestPageTitleFocus } from "@/lib/pageTitleFocus"; import { newRowSeedsShow } from "./newViewRow";
import { useKeyedRefs } from "@/lib/useKeyedRefs";
import { useDragReorder } from "@/lib/useDragReorder";
import { useWindowedRows, windowGapStyle } from "@/lib/useWindowedRows";
import type { DbView, ViewConfig } from "@/lib/types";
import { Plus } from "../icons";
import { ListGroupEnd } from "./ListGroupEnd";
import { rowValuesLocked } from "./rowValuesLocked";
import type { PropertyCellEnvironment } from "./propertyCellTypes";
import type { PropertyValueReadContext } from "./PropValue";
import type { ImportedViewModelBounds } from "./importedViewModel";
import {
  visibleViewProperties,
  viewFilterSeedValues,
  type QueryBounds,
  type ViewFilterSeedValues,
} from "./query";
import {
  galleryPresentationProperty,
  type GalleryPagePresentation,
} from "./galleryViewModel";
import { createListOperationCoordinator, listCompletionBinding, listRowSides, projectListHierarchy } from "./listViewModel";
import { pagesOver, presentedPagesById, rowPagesInOrder, type PagePresenting } from "./presentedPages";
import { conditionalColorResolver } from "./conditionalColorModel";
import styles from "./databaseSheet";
import { propertySearchText } from "./query";
import { CardsLoadMore, type CardsLoadMoreControl } from "./CardsLoadMore";
import { viewShowsPageIcons } from "./CardTitleIcon";
import { subitemFilterScope } from "./subitemFilterScope";
import { DatabaseNoResults, viewNothingToAdd, viewQuery, type ViewNoResultsCommands } from "./DatabaseNoResults";
import { DashboardFilteredContext } from "./dashboardWidgetHeight";
import { rowOpenModeOfClick } from "./rowOpenClick";
import { useViewingArchivedRows, useViewOffersNewRows } from "./databaseArchivedRowsContext";
import { GroupHeadContent } from "./GroupHeadContent";
import { listGroups } from "./listGroups";
import { tableGroupSeedValue } from "./tableGrouping";
import { groupMoveValues } from "./viewGroupMove";
import { groupLine, lineGroup, viewLinesDrag } from "./viewGroupLines";
import { ViewGroupOptions, type ViewGroupMenuAt } from "./ViewGroupOptions";
import { groupOptionNames } from "./TableGroupAddRow";
import { ViewGroupAdd } from "./ViewGroupAdd";
import { useViewAddsGroups, viewGroupAddedKey, viewGroupAddKind } from "./viewGroupAddKind";
import { tableConfigAfterGroupKept } from "./tableGroupSettingsModel";
import { newNamedOption } from "./optionNameText";
import { popoverAnchorOf } from "@/lib/popoverPlacement";
import { useKeepNewGroup } from "./useKeepNewGroup";
import { ListGroupMore, useListGroupLoads } from "./ListGroupLoads";
import { CardSelectionProvider, useCardSelection } from "./cardSelection";
import { useRowDisplayTitle, useUntitledRowName } from "./untitledRowName";
import { useCollapsedGroups } from "./useCollapsedGroups";
import { copiesPlace, type CopiesPlace } from "./viewRowOrder";
import { DatabaseEmptyRowsContext, DatabaseOwnValueContext } from "./databaseNewRowControl";
import { useNewPageExtra } from "./tableNewRowExtra";
import { ROW_ICON_START, SUBITEM_INDENT, ListViewRow, liveEnvironment, useSteadyFrame, type ListRowWays } from "./ListViewRow";
import { fieldEnter } from "@/lib/fieldKeyBoundary";

export type ListRowOpenMode = "default" | "new_tab" | "side" | "full";

export type ListPagePresentation = GalleryPagePresentation & {
  structuralPlaceholder?: true;
};

export interface ListSubitemWindow {
  queryIdentity: string;
  rowIds: readonly string[];
  nextCursor: string | null;
  hasMore: boolean;
  loadingMore?: boolean;
}

export interface ListViewSnapshot {
  snapshotIdentity: string;
  contextIdentity: string;
  requestGeneration: number;
  workspaceId: string;
  contextPageId?: string;
  currentUserId?: string;
  /** Rows shown only around those the filter keeps: faded, as Notion's (seen 2026-10-11). */
  fadedRowIds?: ReadonlySet<string>;
  subitemQueryIdentity: string;
  dataSource: GraphDataSource;
  view: GraphView;
  properties: readonly GraphProperty[];
  rows: readonly GraphRow[];
  pagesById: Readonly<Record<string, GraphRow>>;
  pagePresentationById: Readonly<Record<string, ListPagePresentation>>;
  subitemWindowsByParent: Readonly<Record<string, ListSubitemWindow>>;
  /** Whether its source's sub-items are on. */
  subitemsEnabled?: boolean;
  propertyValueContext: PropertyValueReadContext;
  /** Edits a value pressed in place, where the rows may change. */
  propertyCellEnvironment?: PropertyCellEnvironment;
  queryBounds: QueryBounds;
  importedViewBounds: ImportedViewModelBounds;
}

export interface ListViewOperationContext {
  contextIdentity: string;
  dataSourceId: string;
  requestGeneration: number;
  requestKey: string;
  signal: AbortSignal;
  snapshotIdentity: string;
  viewId: string;
}

export interface ListViewOperations extends ViewNoResultsCommands {
  /** A new row, or with parentId a new sub-item of that row; last, or just before or after another. */
  /** atOnce: the row shows before the server has it, its title typed where it shows (nativeTableProductAdapter). */
  /** alongside: what the press changes with the row, run as it shows so both draw together; `asking`, a row's "+" (`place.beside`) asks whether a sort goes. */
  createRow(seeds: ViewFilterSeedValues, context: ListViewOperationContext, parentId?: string | null,
    place?: { beforeRowId?: string; afterRowId?: string; beside?: boolean }, atOnce?: boolean, alongside?: (row: GraphRow, asking?: boolean) => void): Promise<GraphRow | null>;
  /** Moves rows before or after another in the view, with the values they take there by row. */
  moveRows?(rowIds: readonly string[], targetId: string, side: "before" | "after", context: ListViewOperationContext, values?: Readonly<Record<string, Readonly<Record<string, unknown>>>>): Promise<void>;
  /** What a group's "⋯" changes: the view's settings (a person's change, `step`, a step of the page's ⌘Z), its property's colors, its pages to the trash, and the grouping settings it opens. */
  updateViewConfig?(config: Record<string, unknown>, context: ListViewOperationContext, step?: boolean): Promise<void>;
  commitProperty?(property: GraphProperty, context: ListViewOperationContext): Promise<void>;
  trashRows?(rowIds: readonly string[]): Promise<void>;
  duplicateRows?(rowIds: readonly string[], place?: CopiesPlace): Promise<readonly string[] | void>;
  openGroupSettings?(trigger: HTMLElement): void;
  /** Its first cards and how to show more, when the list shows only its first. */
  loadMoreCards?: CardsLoadMoreControl;
  isCurrent(context: ListViewOperationContext): boolean;
  loadSubitemRows(parentId: string, context: ListViewOperationContext): Promise<ListSubitemWindow | null>;
  loadMoreSubitemRows(parentId: string, cursor: string, context: ListViewOperationContext): Promise<ListSubitemWindow | null>;
  onError(error: unknown, context: ListViewOperationContext): void;
  openRow(rowId: string, context: ListViewOperationContext): void;
  openRowIn(rowId: string, mode: ListRowOpenMode, context: ListViewOperationContext): void;
  openRowContextMenu(rowId: string, event: ReactMouseEvent<HTMLElement>, duplicate?: () => void): void;
  openRowContextMenuFromElement(rowId: string, element: HTMLElement, duplicate?: () => void): void;
  publishSubitemWindow(parentId: string, window: ListSubitemWindow, context: ListViewOperationContext): void;
  setRowProperty(rowId: string, propertyId: string, value: unknown, context: ListViewOperationContext): Promise<void>;
  rowContextMenu: ReactNode;
}

// A list longer than this draws only the rows near the view.
const LIST_WINDOW_THRESHOLD = 100;
// A page whose place is all its presentation holds shows as one.
const LIST_PAGES: PagePresenting = { finish: (page, presentation) => { if (presentation?.structuralPlaceholder) page.__structuralPlaceholder = true; } };

export function ListView({
  snapshot,
  operations,
  readOnly = false,
}: {
  snapshot: ListViewSnapshot;
  operations: ListViewOperations;
  readOnly?: boolean;
}) {
  const { t } = useTranslation(["listView", "tableView", "common", "boardView"]);
  const rowTitle = useRowDisplayTitle();
  // "+ 새 작업" in a task database, as its other views say (untitledRowName.ts).
  const newItemText = useUntitledRowName();
  // Where the page says its rows take no new one ("내 작업" with no source to add to), nothing offers one.
  const offersNewRows = useViewOffersNewRows();
  const emptyRows = useContext(DatabaseEmptyRowsContext);
  const adds = !readOnly && offersNewRows;
  const ownValue = useContext(DatabaseOwnValueContext);
  const [expandedRows, setExpandedRows] = useState<Set<string>>(() => new Set());
  // A row shown for a sub-item the filter keeps opens onto it, as Notion's (seen 2026-10-11).
  useEffect(() => { const faded = snapshot.fadedRowIds; if (faded?.size && subitemFilterScope(snapshot.view.config) !== "parents") setExpandedRows((current) => new Set([...current, ...faded])); }, [snapshot.fadedRowIds, snapshot.view.config]);
  const [createBusy, setCreateBusy] = useState(false);
  const [collapsedGroups, setCollapsedGroups] = useCollapsedGroups(snapshot.view.id), groupLoads = useListGroupLoads(snapshot.view), cutGroup = groupLoads.cut;
  const [groupMenu, setGroupMenu] = useState<ViewGroupMenuAt | null>(null);
  // A row just made here, its title edited in place as Notion's list does.
  const [editingRowId, setEditingRowId] = useState<string | null>(null);
  // Where the caret goes in the title being typed, from the press that started it (titleCaretAt); its end otherwise.
  const editCaret = useRef<number | undefined>(undefined);
  const editStarted = useRef<string | null>(null);
  // The row just added, whose empty dates say how to fill them until its title is done (Notion's, seen 2026-11-05).
  const [addedRowId, setAddedRowId] = useState<string | null>(null);
  const datePressed = useRef(false);
  const coordinator = useMemo(() => createListOperationCoordinator(), []);
  const lifecycleController = useCommittedOperationScope([
    snapshot.contextIdentity,
    snapshot.requestGeneration,
    snapshot.snapshotIdentity,
    snapshot.subitemQueryIdentity,
  ]);
  useEffect(() => setCreateBusy(false), [lifecycleController]);
  // Open rows stay open as the rows change, until the view does.
  useEffect(() => setExpandedRows(new Set()), [snapshot.contextIdentity, snapshot.view.id]);
  const context = useMemo<ListViewOperationContext>(() => ({
    contextIdentity: snapshot.contextIdentity,
    dataSourceId: snapshot.dataSource.id,
    requestGeneration: snapshot.requestGeneration,
    requestKey: "view",
    signal: lifecycleController.signal,
    snapshotIdentity: snapshot.snapshotIdentity,
    viewId: snapshot.view.id,
  }), [lifecycleController, snapshot]);
  const operationContext = (requestKey: string): ListViewOperationContext => ({
    ...context,
    requestKey,
  });
  const matchingSubitemWindow = (parentId: string) => {
    const window = snapshot.subitemWindowsByParent[parentId];
    return window?.queryIdentity === snapshot.subitemQueryIdentity ? window : undefined;
  };

  const props = useMemo(
    () => snapshot.properties.map(galleryPresentationProperty),
    [snapshot.properties],
  );
  const titleProperty = snapshot.properties.find((property) => property.type === "title");
  // Its rows' pages, made again for the rows changed alone (presentedPages.ts).
  const pagesById = useMemo(() => presentedPagesById({ pagesById: snapshot.pagesById, rows: snapshot.rows, workspaceId: snapshot.workspaceId,
    pagePresentationById: snapshot.pagePresentationById }, titleProperty, LIST_PAGES),
  [snapshot.pagePresentationById, snapshot.pagesById, snapshot.rows, snapshot.workspaceId, titleProperty]);
  const shownRows = useMemo(() => {
    const merged = [...rowPagesInOrder(snapshot.rows, pagesById) ?? snapshot.rows.map((row) => pagesById[row.id]).filter(Boolean)];
    const seen = new Set(merged.map((row) => row.id));
    for (const parentId of expandedRows) {
      const window = matchingSubitemWindow(parentId);
      if (!window) continue;
      for (const rowId of window.rowIds) {
        const row = pagesById[rowId];
        if (!row || row.subitemParentId !== parentId || seen.has(row.id)) continue;
        merged.push(row);
        seen.add(row.id);
      }
    }
    return merged;
  }, [expandedRows, pagesById, snapshot.rows, snapshot.subitemQueryIdentity, snapshot.subitemWindowsByParent]);
  // Each item's lock for its reader, which a table's cells and a board's cards keep to too.
  const graphRowsById = useMemo(() => new Map(snapshot.rows.map((row) => [row.id, row])), [snapshot.rows]);
  const view = useMemo<DbView>(() => ({
    id: snapshot.view.id,
    databaseId: snapshot.view.dataSourceId,
    name: snapshot.view.name,
    type: snapshot.view.type as DbView["type"],
    config: snapshot.view.config as ViewConfig,
    position: 0,
  }), [snapshot.view]);
  const rowColor = useMemo(() => conditionalColorResolver(view.config as Record<string, unknown>, props, {
    pagesById, currentPageId: snapshot.contextPageId,
  }, snapshot.queryBounds), [pagesById, props, snapshot.contextPageId, snapshot.queryBounds, view.config]);
  const dashboardFiltered = useContext(DashboardFilteredContext);
  const query = viewQuery(view, props, snapshot.queryBounds, dashboardFiltered);
  const subtaskConfig = view.config?.subtasks;
  const hierarchy = useMemo(() => projectListHierarchy({
    displayMode: subtaskConfig?.displayMode ?? "show",
    expandedRowIds: expandedRows,
    rows: shownRows,
    subitemsEnabled: snapshot.subitemsEnabled === true,
    subitemAdds: !readOnly,
  }), [expandedRows, readOnly, shownRows, snapshot.subitemsEnabled, subtaskConfig]);
  // A search or filter leaving no row shows the "no results" actions, its database holding rows or none, as Notion's (seen 2026-11-27 twice).
  const noResults = hierarchy.items.length === 0 && query.queried;
  // A grouped list draws its groups as the table does, each with its head and
  // "+ 새 페이지" (Notion's, seen 2026-10-25).
  const groupBy = snapshot.view.config?.groupBy;
  const groupProperty = typeof groupBy === "string" ? snapshot.properties.find((property) => property.id === groupBy) : undefined;
  const groups = useMemo(() => groupProperty ? listGroups({ property: groupProperty, properties: snapshot.properties,
    config: snapshot.view.config as Record<string, unknown>, currentUserId: snapshot.currentUserId, rows: shownRows, graphRows: snapshot.pagesById,
    pagesById: snapshot.propertyValueContext.pagesById, propsByDatabaseId: snapshot.propertyValueContext.propsByDatabaseId }) : null,
  [groupProperty, shownRows, snapshot.currentUserId, snapshot.pagesById, snapshot.properties, snapshot.propertyValueContext, snapshot.view.config]);
  type Group = NonNullable<typeof groups>[number];
  type Entry = (typeof hierarchy.entries)[number] | { kind: "groupHead"; group: Group } | { kind: "groupAdd"; group: Group; empty?: boolean } | { kind: "groupMore"; group: Group } | { kind: "newGroup" };
  // Under the last group "+ 신규 그룹" adds what its grouping takes (viewGroupAddKind), which shows at once though empty.
  const addKind = !useViewAddsGroups("list") || readOnly || !operations.commitProperty || !operations.updateViewConfig ? undefined
    : viewGroupAddKind(groupProperty, snapshot.view.config as Record<string, unknown>);
  const canAddGroup = Boolean(addKind);
  const keepGroup = useKeepNewGroup((key) => void operations.updateViewConfig?.(
    tableConfigAfterGroupKept(snapshot.view.config as Record<string, unknown>, key), operationContext("view-group-kept")));
  function addGroup(name: string) {
    if (!groupProperty) return;
    if (addKind && addKind !== "option") return void operations.updateViewConfig?.(tableConfigAfterGroupKept(snapshot.view.config as Record<string, unknown>,
      viewGroupAddedKey(addKind, name), addKind === "person" ? (groups ?? []).map((group) => group.key) : undefined), operationContext("view-group-kept"), true);
    const config = groupProperty.config as Record<string, unknown>;
    const options = Array.isArray(config.options) ? config.options : [];
    const option = newNamedOption(name);
    void operations.commitProperty?.({ ...groupProperty, config: { ...config, options: [...options, option] } } as GraphProperty, operationContext(`property:${groupProperty.id}`))
      .then(() => keepGroup(option.id));
  }
  const tasksAcrossSources = view.config?.tasksAcrossSources === true;
  const entries = useMemo<Entry[]>(() => groups ? groups.flatMap((group): Entry[] => { const cut = cutGroup(group.key, group.rows); return collapsedGroups.has(group.key) ? [{ kind: "groupHead", group }] : [
    { kind: "groupHead", group },
    ...projectListHierarchy({ displayMode: subtaskConfig?.displayMode ?? "show", expandedRowIds: expandedRows, rows: cut.rows,
      subitemsEnabled: snapshot.subitemsEnabled === true, subitemAdds: !readOnly }).entries,
    // "내 작업"'s empty group shows Notion's empty body in place of its "+ 새 작업" (ListGroupEnd).
    ...(tasksAcrossSources && group.rows.length === 0 ? [{ kind: "groupAdd" as const, group, empty: true }] : adds ? [{ kind: "groupAdd" as const, group }] : []), ...(cut.more ? [{ kind: "groupMore" as const, group }] : []),
  ]; }).concat(canAddGroup ? [{ kind: "newGroup" }] : []) : hierarchy.entries,
  [adds, canAddGroup, collapsedGroups, cutGroup, expandedRows, groups, hierarchy.entries, readOnly, snapshot.subitemsEnabled, subtaskConfig, tasksAcrossSources]);
  const visible = useMemo(
    () => visibleViewProperties(props, view, snapshot.queryBounds),
    [props, snapshot.queryBounds, view],
  );
  const completionBinding = useMemo(
    () => listCompletionBinding(props, view, snapshot.importedViewBounds),
    [props, snapshot.importedViewBounds, view],
  );
  // What the view lists before its title shows before it, the rest at the
  // row's end, as Notion's list (listRowSides).
  const { leading, others } = useMemo(() => listRowSides(visible, completionBinding?.property.id), [completionBinding, visible]);
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
  }), [pagesById, props, snapshot]);
  const newPageLabel = t("listView:newPageIn", { dbTitle: snapshot.dataSource.title });
  const newPageExtra = useNewPageExtra(snapshot.view.config);
  const newPageButton = (
    <button type="button" disabled={createBusy} className={styles.listNew}
      title={newPageLabel} aria-label={newPageLabel} onClick={() => void createRow(null, newRowOpensItsPage())}>
      <Plus size="small" aria-hidden="true" /> {newItemText}
    </button>
  );
  const viewingArchived = useViewingArchivedRows();
  const ownEmpty = Boolean(emptyRows) && hierarchy.items.length === 0 && !query.searching && !viewingArchived; // grouped or not ("내 작업", Notion's 2026-11-15)
  const flattenedSubtasks = snapshot.subitemsEnabled === true && subtaskConfig?.displayMode === "flattened";
  const hiddenSubtasks = snapshot.subitemsEnabled === true && subtaskConfig?.displayMode === "hidden";

  // TanStack Virtual draws a long list's rows near the view only; the row
  // holding focus stays drawn, and page find reads every row's text.
  const listRef = useRef<HTMLDivElement>(null);
  const [focusedRowId, setFocusedRowId] = useState<string | null>(null);
  // A page of several groups is in each; its key says which.
  const entryKey = (entry: Entry | undefined) => !entry ? undefined : entry.kind === "newGroup" ? "new-group" : entry.kind === "groupHead" ? `group:${entry.group.key}`
    : entry.kind === "groupAdd" || entry.kind === "groupMore" ? `${entry.kind}:${entry.group.key}` : entry.kind === "subitemAdd" ? `subitem-add:${entry.parentId}` : entry.item.row.id;
  const entryKeys = useMemo(() => {
    let groupKey = "";
    return entries.map((entry) => {
      if (entry.kind === "groupHead") groupKey = entry.group.key;
      return groups && (entry.kind === "row" || entry.kind === "subitemAdd") ? `${groupKey}:${entryKey(entry)}` : entryKey(entry);
    });
  }, [entries, groups]);
  const windowedRows = useWindowedRows({
    listRef, count: entries.length, threshold: LIST_WINDOW_THRESHOLD, lines: () => drag.shown, estimateSize: (index) => entries[index]?.kind === "groupHead" ? 48 : 32,
    getItemKey: (index) => entryKeys[index] ?? index,
    pinned: focusedRowId ? [entries.findIndex((entry) => entry.kind === "row" && entry.item.row.id === focusedRowId)] : [],
  });
  const searchContext = useMemo(() => ({ props, pagesById, currentPageId: snapshot.contextPageId, bounds: snapshot.queryBounds }),
    [pagesById, props, snapshot.contextPageId, snapshot.queryBounds]);
  useDataFindSource({
    listRef,
    items: entries.flatMap((entry, index) => entry.kind === "row" ? [{ index, row: entry.item.row }] : []),
    reveal: ({ index }) => windowedRows.reveal(index),
    parts: ({ row }) => [{
      text: [rowTitle(row), ...[...leading, ...others].map((property) => propertySearchText(row, property, searchContext))].join("\n"),
      drawn: () => listRef.current?.querySelector<HTMLElement>(`[data-list-row-id="${CSS.escape(row.id)}"]`) ?? null,
    }],
  });

  function openRow(rowId: string, mode: ListRowOpenMode = "default") {
    if (!operations.isCurrent(context) || context.signal.aborted) return;
    if (mode === "default") operations.openRow(rowId, context);
    else operations.openRowIn(rowId, mode, context);
  }

  function openRowClick(rowId: string, e: ReactMouseEvent<HTMLElement>) {
    openRow(rowId, rowOpenModeOfClick(e));
  }

  function openRowKey(rowId: string, e: ReactKeyboardEvent<HTMLElement>) {
    if (e.key === "ContextMenu" || (e.shiftKey && e.key === "F10")) {
      e.preventDefault();
      e.stopPropagation();
      operations.openRowContextMenuFromElement(rowId, e.currentTarget, selection.copyHere(e.currentTarget, rowId));
      return;
    }
    if (!fieldEnter(e) && e.key !== " ") return;
    e.preventDefault(); openRow(rowId, e.metaKey || e.ctrlKey ? "new_tab" : "default");
  }

  async function runSubitemRequest(parentId: string, loadMore: boolean) {
    const window = matchingSubitemWindow(parentId);
    const next = operationContext(`subitems:${parentId}`);
    await coordinator.run(next, {
      isCurrent: (context) => operations.isCurrent(context),
      onError: (error, context) => operations.onError(error, context),
      perform: (context) => loadMore && window?.nextCursor
        ? operations.loadMoreSubitemRows(parentId, window.nextCursor, context)
        : operations.loadSubitemRows(parentId, context),
      publish: (result, context) => {
        if (result) operations.publishSubitemWindow(parentId, result, context);
      },
    });
  }

  function toggleExpanded(rowId: string, loads: boolean) {
    const expanding = !expandedRows.has(rowId);
    setExpandedRows((current) => {
      const next = new Set(current);
      if (next.has(rowId)) next.delete(rowId);
      else next.add(rowId);
      return next;
    });
    if (expanding && loads && !matchingSubitemWindow(rowId)) void runSubitemRequest(rowId, false);
  }

  // From the no-results state the new page opens, since the view would not show it.
  // A page made in a group starts with the group's value: from its head's "+" first and opened, from its "+ 새 페이지" last.
  // A row made from "+ 새 작업" is chosen; Delete moves the chosen ones to the trash (Notion's, seen 2026-11-03).
  const selection = useCardSelection({ keep: snapshot.view.id, onMenu: operations.openRowContextMenuFromElement, onOpen: openRow,
    onTrash: readOnly || !operations.trashRows ? undefined : (ids) => void operations.trashRows?.(ids),
    onDuplicate: readOnly || !operations.duplicateRows ? undefined : (ids) => {
      const { rowIds, place } = copiesPlace((groups ?? [{ key: undefined, rows: shownRows }]).map((group) => group.rows.map(({ id }) => groupLine(group.key, id))), ids, shownRows.map(({ id }) => id));
      return operations.duplicateRows?.(rowIds, place);
    } });
  async function createRow(parentId: string | null = null, open = false, group?: { key: string; beforeRowId?: string; afterRowId?: string; beside?: boolean }) {
    if (!adds) return;
    const next = operationContext(parentId ? `create:${parentId}` : "create");
    const filtered = viewFilterSeedValues(
      props,
      view,
      group && groupProperty ? [groupProperty.id] : [],
      { currentPageId: snapshot.contextPageId, currentUserId: snapshot.currentUserId },
      snapshot.queryBounds,
    );
    const seed = group && groupProperty ? tableGroupSeedValue(groupProperty, group.key, snapshot.view.config as Record<string, unknown>) : undefined;
    const seeds = seed === undefined ? filtered : { ...filtered, properties: { ...filtered.properties, [groupProperty!.id]: seed } };
    // One the view would leave out opens (newViewRow.ts). Only a row whose page opens waits (for its page's copy here): one typed in place is made in the press.
    open ||= !parentId && !newRowSeedsShow(seeds, snapshot); if (open) setCreateBusy(true);
    const place = group?.beforeRowId || group?.afterRowId ? { beforeRowId: group.beforeRowId, afterRowId: group.afterRowId, beside: group.beside } : undefined;
    // What is typed before a row shown here has its title goes into it; made here, its title takes the focus in this press (madeNow).
    let accepted = false;
    const make = async () => {
      let made = false;
      accepted = await coordinator.run(next, {
        isCurrent: (context) => operations.isCurrent(context),
        onError: (error, context) => operations.onError(error, context),
        // Its title is edited in place even when the rows have changed meanwhile.
        perform: async (context) => {
          // Its title's editor comes with the row, drawn as it shows (nativeTableRowSteps.ts).
          let shown = false;
          const alongside = (row: GraphRow, asking?: boolean) => {
            shown = true;
            if (open) { requestPageTitleFocus(row.id); operations.openRow(row.id, context); }
            else if (!asking) { editCaret.current = undefined; setEditingRowId(row.id); setAddedRowId(row.id); }
          };
          const row = await operations.createRow(seeds, context, parentId, place, !open, alongside);
          if (row && !shown) alongside(row);
          if (row && !open) made = true;
          return row;
        },
        publish: () => {},
      });
      return made;
    };
    await (open ? holdTypingWhileRowOpens(make) : holdTypingForField(make, undefined, true));
    if (open && accepted && operations.isCurrent(next) && !next.signal.aborted) setCreateBusy(false);
  }

  // Into another group they take that group's value in the move's one step of ⌘Z, as a board's do (moveShownRows, groupMoveValues); a chosen row carries the other chosen rows beside it,
  // and a row under several groups goes by its line under each, from that group (viewLinesDrag).
  const groupKeyOf = (rowId: string) => groups?.find((group) => group.rows.some((row) => row.id === rowId))?.key;
  const rowParent = (rowId: string) => pagesById[rowId]?.subitemParentId ?? null;
  const drag = useMemo(() => viewLinesDrag(entries, (entry, at) => entry.kind !== "row" ? undefined : { line: groups ? groupLine(entryKeys[at]!.slice(0, -(entry.item.row.id.length + 1)), entry.item.row.id)
    : entry.item.row.id, parentId: entry.item.row.subitemParentId ?? null, depth: entry.item.depth }, selection.value.chosen), [entries, entryKeys, groups, selection.value.chosen]);
  const rowReorder = useDragReorder(`list-row:${view.id}`, (sourceId, targetId, side, carried) => {
    const moved = drag.moved(carried, targetId), context = operationContext(`row-order:${drag.rowOf(sourceId)}`);
    selection.chooseMany(moved.landed); // The rows dropped are chosen, each by its line where it lands, as Notion's (seen 2026-11-12, several 2026-11-24).
    void operations.moveRows?.(moved.rowIds, moved.targetId, side, context, groupMoveValues(groupProperty, moved.rowIds, moved.to, moved.groupOf, snapshot.view.config as Record<string, unknown>, snapshot.rows)).catch((error) => operations.onError(error, context));
  }, { band: true, next: (sourceId) => drag.next(sourceId), carry: (sourceId) => drag.carry(sourceId) });
  const measureRow = useRef(windowedRows.measure); measureRow.current = windowedRows.measure;
  const rowElement = useKeyedRefs<string>((id, element) => { measureRow.current?.(element); return rowReorder.elementRef(id)(element); });

  async function setCompletion(rowId: string, propertyId: string, value: string) {
    const next = operationContext(`property:${rowId}:${propertyId}`);
    try {
      await operations.setRowProperty(rowId, propertyId, value, next);
    } catch (error) {
      if (operations.isCurrent(next) && !next.signal.aborted) operations.onError(error, next);
    }
  }

  // What every row draws alike, and what its presses do by the list as last drawn (ListViewRow); no "+" by "내 작업"'s rows (Notion's, 2026-11-15).
  const showIcons = viewShowsPageIcons(view.config ?? {}), directEdit = view.config?.directEditPageName === true && !readOnly && Boolean(titleProperty);
  // Its values' editors take the list's latest ways, the same object as the rows' own drawings stay (liveEnvironment).
  const environmentNow = useRef(snapshot.propertyCellEnvironment);
  environmentNow.current = snapshot.propertyCellEnvironment;
  const [latestEnvironment] = useState(() => liveEnvironment(environmentNow));
  const rowFrame = useSteadyFrame({
    leading, others, environment: snapshot.propertyCellEnvironment ? latestEnvironment : undefined, ownValue, readOnly, adds: adds && !tasksAcrossSources,
    moves: !readOnly && !viewingArchived && Boolean(operations.moveRows), expandable: hierarchy.expandable, completion: completionBinding, showIcons, directEdit,
    titled: Boolean(titleProperty), contextIdentity: snapshot.contextIdentity, flattened: flattenedSubtasks, hiddenSubtasks,
  });
  const waysNow = useRef<ListRowWays>(null!);
  waysNow.current = {
    openRow, openClick: openRowClick, openKey: openRowKey,
    contextMenu: (rowId, event) => { selection.menuChoose(event.currentTarget); operations.openRowContextMenu(rowId, event, selection.copyHere(event.currentTarget, rowId)); },
    // Its menu opens with the row chosen, and Escape leaves it chosen (Notion's, seen 2026-11-05; a right press's too, cardSelection.tsx).
    handleMenu: (rowId, element) => { selection.choose(element.dataset.cardHandleFor ?? rowId); operations.openRowContextMenuFromElement(rowId, element, selection.copyHere(element, rowId)); },
    handle: (rowId) => rowReorder.handle(rowId, true, {}),
    addNear: (row, before, line) => void createRow(row.subitemParentId ?? null, false,
      { key: lineGroup(line ?? "") ?? groupKeyOf(row.id) ?? "", beside: true, ...(before ? { beforeRowId: row.id } : { afterRowId: row.id }) }),
    toggle: toggleExpanded,
    loadMore: (rowId) => void runSubitemRequest(rowId, true),
    setCompletion: (rowId, propertyId, value) => void setCompletion(rowId, propertyId, value),
    datePressed: () => { datePressed.current = true; },
    commitDate: (rowId, propertyId, value) => {
      setAddedRowId(null);
      void operations.setRowProperty(rowId, propertyId, value, operationContext(`property:${rowId}:${propertyId}`));
    },
    editTitle: (rowId, caret) => { editCaret.current = caret; setEditingRowId(rowId); },
    editingChange: (rowId, editing) => {
      if (editing) editStarted.current = rowId;
      else if (editStarted.current === rowId) { editStarted.current = null; setEditingRowId(null); }
    },
    choose: (rowId) => selection.choose(rowId),
    commitTitle: (rowId, value) => {
      if (!datePressed.current && addedRowId === rowId) setAddedRowId(null);
      datePressed.current = false;
      return titleProperty ? setCompletion(rowId, titleProperty.id, value) : Promise.resolve();
    },
  };
  const [rowWays] = useState(() => new Proxy({} as ListRowWays, {
    get: (_, key) => (...args: unknown[]) => (waysNow.current[key as keyof ListRowWays] as (...args: unknown[]) => unknown)(...args),
  }));

  return (
    <CardSelectionProvider value={selection.value}>
    <div className={styles.list} data-grouped={groups ? "true" : undefined}>
      {/* A page's own empty list ("내 작업"'s) stands for the filters' "no results" too, but not for a search's. */}
      {ownEmpty ? emptyRows!(adds ? () => void createRow() : undefined) : null}
      {ownEmpty ? null : noResults ? <DatabaseNoResults searching={query.searching} onEditFilters={operations.openFilterSettings}
        onClearSearch={operations.clearSearch} onAddRow={adds ? () => void createRow(null, newRowOpensItsPage()) : undefined} /> : viewNothingToAdd((groups ?? hierarchy.items).length, query.queried, readOnly)}
      <div ref={listRef} data-list-rows
        onFocus={(event) => setFocusedRowId((event.target as Element).closest("[data-list-row-id]")?.getAttribute("data-list-row-id") ?? null)}
        onBlur={(event) => { if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setFocusedRowId(null); }}>
        {(ownEmpty ? [] : windowedRows.entries).map((slot) => {
          if (slot.kind === "gap") return <div key={slot.key} aria-hidden="true" style={windowGapStyle(slot.size)} />;
          const entry = entries[slot.index];
          if (entry.kind === "groupHead") {
            const { group } = entry;
            const topRows = group.rows.filter((row) => !row.subitemParentId || !group.rows.some((candidate) => candidate.id === row.subitemParentId));
            return (
              <div key={entryKeys[slot.index]} ref={windowedRows.measure} data-index={slot.index} className={styles.listGroupHead} data-list-group-key={group.key}>
                <GroupHeadContent group={group} collapsed={collapsedGroups.has(group.key)}
                  onToggle={() => setCollapsedGroups((current) => { const next = new Set(current); if (next.has(group.key)) next.delete(group.key); else next.add(group.key); return next; })}
                  count={snapshot.view.config?.showGroupCounts === true ? topRows.length : undefined}
                  onMenu={readOnly || !operations.updateViewConfig ? undefined : (element) => setGroupMenu({ group: { ...group, rows: group.rows.map((row) => ({ row })) },
                    position: popoverAnchorOf(element), returnTo: element })}
                  onAdd={!adds || viewingArchived ? undefined : () => void createRow(null, false, { key: group.key, beforeRowId: topRows[0]?.id })} />
              </div>
            );
          }
          if (entry.kind === "newGroup") {
            return (
              <div key="new-group" ref={windowedRows.measure} data-index={slot.index} className={styles.listNewGroupRow}>
                {addKind && groupProperty ? <ViewGroupAdd kind={addKind} property={groupProperty} environment={snapshot.propertyCellEnvironment}
                  className={styles.listNewGroup} marker="list-new-group"
                  taken={addKind === "option" ? groupOptionNames(groupProperty) : (groups ?? []).map((group) => group.label ?? "")}
                  has={(value) => addKind !== "option" && (groups ?? []).some((group) => group.key === viewGroupAddedKey(addKind, value))} onAdd={addGroup} /> : null}
              </div>
            );
          }
          if (entry.kind === "groupMore") return <ListGroupMore key={entryKeys[slot.index]} groupKey={entry.group.key} measure={windowedRows.measure} index={slot.index} loads={groupLoads} />;
          if (entry.kind === "groupAdd") {
            const topRows = entry.group.rows.filter((row) => !row.subitemParentId || !entry.group.rows.some((candidate) => candidate.id === row.subitemParentId));
            return <ListGroupEnd key={entryKeys[slot.index]} measure={windowedRows.measure} index={slot.index} groupKey={entry.group.key}
              empty={entry.empty === true} disabled={createBusy} label={newItemText} onEditFilters={operations.openFilterSettings}
              onAdd={adds ? () => void createRow(null, newRowOpensItsPage(), { key: entry.group.key, afterRowId: topRows.at(-1)?.id }) : undefined} />;
          }
          if (entry.kind === "subitemAdd") {
            return <button key={entryKeys[slot.index]} ref={windowedRows.measure} data-index={slot.index} type="button"
              className={styles.listSubitemAdd} data-list-subitem-add={entry.parentId} disabled={createBusy}
              style={{ paddingInlineStart: ROW_ICON_START + entry.depth * SUBITEM_INDENT }} onClick={() => void createRow(entry.parentId)}>
              <Plus size="small" aria-hidden="true" /> {t("tableView:newSubitem")}
            </button>;
          }
          const { row, depth, hasChildren, childCount } = entry.item;
          const window = expandedRows.has(row.id) ? matchingSubitemWindow(row.id) : undefined;
          const parent = row.subitemParentId ? pagesById[row.subitemParentId] : undefined;
          // Where a dragged row would land, asked of every row as the list draws (useDragReorder).
          const line = drag.lineAt(slot.index) ?? row.id, reorderRow = rowFrame.moves && row.__structuralPlaceholder !== true
            ? rowReorder.row(line, (sourceId) => rowParent(drag.rowOf(sourceId)) === (row.subitemParentId ?? null) && drag.takes(sourceId, row.id)) : undefined;
          return <ListViewRow key={entryKeys[slot.index]} row={row} index={slot.index} depth={depth} hasChildren={hasChildren} childCount={childCount}
            expanded={expandedRows.has(row.id)} more={window?.hasMore && window.nextCursor ? window.loadingMore === true ? "loading" : "ready" : "none"}
            parentTitle={parent?.__structuralPlaceholder ? t("tableView:restrictedParent") : parent ? rowTitle(parent) : ""}
            tint={row.__structuralPlaceholder === true ? undefined : rowColor(row)?.color}
            dropSide={reorderRow?.["data-drop-side"]} dragging={reorderRow?.["data-dragging"]}
            faded={snapshot.fadedRowIds?.has(row.id) === true} editNow={editingRowId === row.id || editingRowId === line} editCaret={editingRowId === row.id || editingRowId === line ? editCaret.current : undefined} added={addedRowId === row.id}
            locked={rowValuesLocked(graphRowsById.get(row.id))} frame={rowFrame} context={propertyValueContext} ways={rowWays}
            line={line} rowRef={rowElement(line)} />;
        })}
        {!ownEmpty && windowedRows.after > 0 && <div aria-hidden="true" style={windowGapStyle(windowedRows.after)} />}
      </div>
      {/* As Notion's, an empty list is only its "+ 새 페이지" (seen 2026-10-11). */}
      {!noResults && !ownEmpty && adds && !viewingArchived && !groups && (newPageExtra
        ? <div className={styles.listNewRow}>{newPageButton}{newPageExtra}</div> : newPageButton)}
      {operations.loadMoreCards ? <CardsLoadMore placement="view" {...operations.loadMoreCards} /> : null}
      {operations.rowContextMenu}
      {groupMenu && groupProperty ? <ViewGroupOptions at={groupMenu} property={groupProperty} config={snapshot.view.config as Record<string, unknown>}
        structureLocked={readOnly} onClose={() => setGroupMenu(null)}
        onConfig={(next) => void operations.updateViewConfig?.(next, operationContext("view-group-options"), true)}
        onProperty={(next) => void operations.commitProperty?.(next, operationContext(`property:${next.id}`))}
        onTrash={operations.trashRows ? (ids) => void operations.trashRows?.(ids) : undefined}
        onOpenSettings={operations.openGroupSettings} /> : null}
    </div>
    </CardSelectionProvider>
  );
}
