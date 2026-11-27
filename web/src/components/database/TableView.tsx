"use client";

import { copiesPlace, viewKeepsOwnRowOrder, viewRowOrderPlacing, type CopiesPlace } from "./viewRowOrder";
import { useChosenRowList, useChosenRows, useTableRowChoice } from "./useTableRowChoice";
import { chosenAsLines, chosenRowsOf, groupLine, lineGroup, lineRowId, linesBetween, rowsPerLine, viewLinesOf } from "./viewGroupLines";
import { useCommittedOperationScope } from "../product/useCommittedOperationScope";
import { useCreatedTableRowFocus } from "./useCreatedTableRowFocus";
import { tableRowAfterSearch, useNewPageExtra } from "./tableNewRowExtra";
import { TableEmptyRows } from "./TableEmptyRows";

import { useContext, useDeferredValue, useEffect, useMemo, useRef, useState, type MouseEvent, type ReactNode } from "react";
import { useKeptChoice } from "@/lib/selectionMemory";
import { holdTypingForField, holdTypingWhileRowOpens, newRowOpensItsPage, requestPageTitleFocus } from "@/lib/pageTitleFocus";
import { useTranslation } from "react-i18next";
import { compareGraphOrder, type GraphDataSource, type GraphProperty, type GraphRow, type GraphRowValue, type GraphView } from "@/contentGraph";
import { newId } from "@/lib/ids";
import { newNamedOption } from "./optionNameText";
import { graphPositionBetween } from "@/lib/nativeBoardProductAdapter";
import type { FormulaBounds } from "./formulaBudget";
import type { PropertyCellEnvironment } from "./propertyCellTypes";
import type { PropertyValueReadContext } from "./PropValue";
import { RelationPropertyCreationDialog } from "./RelationPropertyCreationDialog";
import { useDatabasePropertySettings } from "./databasePropertySettings";
import type { SelectedRowsMenu } from "./useRowContextMenu";
import { configForType, propertyTypeLabel } from "./propertyTypes";
import { newTablePropertyName } from "./tableAddPropertyModel";
import { groupOptionNames, keepFocusAtPress } from "./TableGroupAddRow";
import { TableGrid, tableRowPage } from "./TableGrid";
import { TableInsertProperty } from "./TableInsertProperty";
import { TablePropertyMenu, type TableRelationCreation } from "./TablePropertyMenu";
import { TableSelectionProperties, useSelectionBarProperties } from "./TableSelectionProperties";
import { TableSelectionToolbar } from "./TableSelectionToolbar";
import { TableSummaryRow } from "./TableSummaryRow";
import { tableGroupPages, tableGroupValueReader } from "./tableGroupValues";
import { createListOperationCoordinator } from "./listViewModel";
import { copyTextWithMime, LEAFEN_BLOCKS_MIME, LEAFEN_TABLE_ROWS_MIME } from "@/lib/clipboard";
import { rowPagesClipboard } from "./rowPagesClipboard";
import { absolutePageUrl } from "@/lib/navigation";
import { usePageNames } from "@/lib/pageNames";
import type { Page, PropertyType, TextSpan } from "@/lib/types";
import { TABLE_CLIPBOARD_BOUNDS, tableClipboardEntry } from "./tableClipboardModel";
import type { TablePaste } from "./tableCellPaste";
import { copyTableCells, pastedLines, pastedRowsMade, tableCellsClipboard } from "./tablePasteWrite";
import { resizeTableColumn, selectedRowMutation, selectedRowsCellValue } from "./tableInteractionModel";
import { tableRowsDropped } from "./tableRowDrop";
import { tableGroupSeedValue, tableGrouper, tableGroupsOf, type TableGroupDescriptor } from "./tableGrouping";
import { DatabaseNoResults, type ViewNoResultsCommands } from "./DatabaseNoResults"; import { newRowShows } from "./newViewRow";
import { TableSubitemMeta, tableSubitemMetaShown, type TableFlatSubitems } from "./TableSubitemMeta";
import { ViewGroupOptions, type ViewGroupMenuAt } from "./ViewGroupOptions";
import { popoverAnchorOf } from "@/lib/popoverPlacement";
import { arrangeViewGroups, tableConfigAfterGroupKept } from "./tableGroupSettingsModel";
import { useKeepNewGroup } from "./useKeepNewGroup";
import { ViewGroupAdd } from "./ViewGroupAdd";
import { useViewAddsGroups, viewGroupAddedKey, viewGroupAddKind } from "./viewGroupAddKind";
import { propertiesAfterDuplicate } from "./rowPropertiesModel";
import { groupedLinesAsNow, projectGroupedTableRows, projectUngroupedTableRows, projectVisibleProperties, rowsUnderParents, tableColumnWidth, tableProjectedRows, tableViewFiltered } from "./tableViewModel";
import { useDeferredPlacing } from "./viewRowProjection";
import {
  tableColumnSettings, tableConfigAfterAdd, tableConfigAfterCalculation, tableConfigAfterDuplicate, tableConfigAfterInsert, tableConfigAfterReorder, tableConfigAfterResize,
  tableFullUrlColumnIds, tableShowsPageIcons, tableStatusCheckboxIds, tableWrappedColumnIds,
} from "./tablePropertyMenuModel";
import { Plus } from "../icons";
import styles from "./databaseSheet";
import { conditionalCellColorResolver } from "./conditionalColorModel";
import { galleryPresentationProperty } from "./galleryViewModel";
import { propertySearchText } from "./query";
import { nativeBoardQueryBounds } from "./boardViewModel";
import { graphViewFilterSeeds } from "./graphViewFilterSeeds";
import { useDatabaseStructureLocked } from "./databaseStructureLock";
import { useColumnHeadsReadOnly } from "./databaseRowsPreview";
import { subitemFilterScope } from "./subitemFilterScope";
import { useViewOffersNewRows } from "./databaseArchivedRowsContext";
import { rowValuesLocked } from "./rowValuesLocked";
import { effectiveOpenPageIn } from "./databaseViewShared";
import { RowsOpenInContext } from "./databaseRowOpeners";
import { useCollapsedGroups } from "./useCollapsedGroups";
import { useUntitledRowName } from "./untitledRowName";
import { DashboardWidgetHeightContext, useDashboardWidgetViewing } from "./dashboardWidgetHeight";
import { usePageLayoutTabMode } from "./pageLayoutTabEnvironment";
import { inlineStartX, readsRightToLeft } from "@/lib/readingDirection";
import { useViewMarquee } from "./viewMarquee";
import { useTableRowSelectionKeys } from "./useTableRowSelectionKeys";
import { longPressKeptForWords, openRowMenuByKey, rowMenuChoosesRow } from "./rowMenuByKey";
import { VIEW_CHOICE_ROOT } from "./viewChoiceSteps";
import { keysLedFocus } from "@/lib/lastPress";

export type TableRowOpenMode = "default" | "new_tab" | "side" | "full";
export type TableSubitemWindow = { hasMore: boolean; nextCursor: string | null; rowIds: readonly string[]; snapshotIdentity: string };

export interface TableViewSnapshot {
  snapshotIdentity: string;
  contextIdentity: string;
  requestGeneration: number;
  workspaceId: string;
  dataSource: GraphDataSource;
  view: GraphView;
  properties: readonly GraphProperty[];
  rows: readonly GraphRow[];
  /** Every row of its source, those its filter hides too: a chosen one stays chosen while hidden (useChosenRowList). */
  sourceRows?: readonly GraphRow[];
  /** Whether its search is on, and whether a search or filter narrows it. */
  query?: { searching: boolean; queried: boolean };
  /** Sub-items shown as a flattened list: no row nests, a title shows its count or parent. */
  flatSubitems?: TableFlatSubitems;
  /** Sub-items shown as a flattened list or disabled: no row nests under another. */
  subitemsFlat?: boolean;
  /** Every row of the view when `rows` shows only its first ones; its calculations count these. */
  viewRows?: readonly GraphRow[];
  /** How many rows the table first shows, when it shows only its first ones. */
  rowLoadLimit?: number;
  /** How many rows each group of a grouped table first shows, when it shows only its first ones. */
  groupRowLimit?: number;
  groupRowStep?: number;
  /** Rows made here, which show however few rows a group shows. */
  madeRowIds?: ReadonlySet<string>;
  rowHasMore: boolean;
  rowNextCursor: string | null;
  subitemWindows: Readonly<Record<string, TableSubitemWindow | undefined>>;
  /** Whether the source's sub-items are on, giving every row a toggle's room: on the title, or in the first column ("first", Notion's "제목에 중첩 토글 표시" off). */
  subitemsEnabled?: boolean | "first";
  /** A task database's last row reads "+ 새 작업", as Notion's (seen 2026-11-02). */
  taskDatabase?: boolean;
  /** Rows shown for a sub-item or parent the filter keeps, not kept themselves: faded, as Notion's (seen 2026-10-11). */
  fadedRowIds?: ReadonlySet<string>;
  formulaBounds: FormulaBounds;
  propertyCellEnvironment?: PropertyCellEnvironment;
  propertyValueContext?: PropertyValueReadContext;
  relationCreation?: TableRelationCreation;
  currentUserId?: string;
  /** The page the view is on, a linked view's own: a filter's "this page". */
  contextPageId?: string;
}

export interface TableViewOperationContext {
  contextIdentity: string;
  dataSourceId: string;
  requestGeneration: number;
  requestKey: string;
  signal: AbortSignal;
  snapshotIdentity: string;
  viewId: string;
}

export interface TableReadResult {
  hasMore: boolean;
  nextCursor: string | null;
  rows: readonly GraphRow[];
  snapshotIdentity: string;
}

export interface TableViewOperations extends ViewNoResultsCommands {
  /** Makes a row, after another or last, or just before one; `seeds` are values it starts with, such as its group's. */
  /** atOnce: the row shows before the server has it, its title typed where it shows (nativeTableProductAdapter). */
  /** alongside: what the press changes with the row, run as it shows so both draw together. */
  createRow(parentId: string | null, afterRowId: string | null, context: TableViewOperationContext, seeds?: Readonly<Record<string, unknown>>, beforeRowId?: string, atOnce?: boolean,
    alongside?: (row: GraphRow) => void): Promise<GraphRow | null>;
  commitRows(rows: readonly GraphRow[], context: TableViewOperationContext, typed?: boolean): Promise<void>;
  /** A paste's rows filled and rows made past them, one step of ⌘Z (nativeTableProductAdapter pasteRows); gives the rows made. */
  pasteRows?(changed: readonly GraphRow[], made: { parentId: string | null; afterRowId: string | null; seeds: Readonly<Record<string, unknown>>;
    rows: readonly GraphRow["values"][] }, context: TableViewOperationContext): Promise<readonly string[]>;
  /** A row a row's "+" made, placed so in the view: a sorted view asks whether its sort goes, true when it asks (sortedViewDrop.ts). */
  rowMadeBeside?(view: GraphView): boolean;
  /** Rows dragged to new places, or the view with `rowIds` in its own order, with the values by row the move sets (another group's): one step of ⌘Z. */
  moveRows?(rows: readonly GraphRow[], view: GraphView | undefined, rowIds: readonly string[], context: TableViewOperationContext, values?: Readonly<Record<string, GraphRow["values"]>>): Promise<void>;
  /** Saves properties with, when given, the view config that shows them. */
  commitProperties(properties: readonly GraphProperty[], context: TableViewOperationContext, view?: GraphView): Promise<void>;
  deleteProperties(propertyIds: readonly string[], context: TableViewOperationContext): Promise<void>;
  /** A person's change to the view (`step`) is a step of the page's ⌘Z; one made for them, as a column placed for a new relation, is not. */
  updateView(view: GraphView, context: TableViewOperationContext, step?: boolean): Promise<void>;
  deleteRows(rowIds: readonly string[], context: TableViewOperationContext): Promise<void>;
  /** A row "+ 새 페이지" made with its title to type in, and the values it was made with: Escape in its empty title takes it away (freshRows.ts). */
  freshRow?(rowId: string, made: GraphRow["values"]): void;
  /** Duplicates the rows, in the order given, and says which rows are their copies; `place` says where they go. */
  duplicateRows(rowIds: readonly string[], context: TableViewOperationContext, place?: CopiesPlace): Promise<readonly string[] | void>;
  loadMoreRows(cursor: string | null, context: TableViewOperationContext): Promise<TableReadResult>;
  loadMoreSubitems(parentId: string, cursor: string | null, context: TableViewOperationContext): Promise<TableReadResult>;
  isCurrent(context: TableViewOperationContext): boolean;
  onError(error: unknown, context: TableViewOperationContext): void;
  openRow(rowId: string, context: TableViewOperationContext): void;
  openRowIn(rowId: string, mode: TableRowOpenMode, context: TableViewOperationContext): void;
  /** Closes the row its side peek shows. */
  closeRow?(): void;
  openRowContextMenu(rowId: string, event: MouseEvent<HTMLElement>, duplicate?: () => void): void;
  openRowContextMenuFromElement(rowId: string, element: HTMLElement, duplicate?: () => void): void;
  /** Several selected rows' menu, from their bar's "⋯" where it was pressed. */
  openRowsContextMenuAt?(rows: SelectedRowsMenu, anchor: { x: number; y: number; top?: number }, returnTo: HTMLElement): void;
  rowContextMenu: ReactNode;
  /** Opens the view's properties menu beside the header's "⋯". */
  openProperties?(trigger: HTMLElement): void;
  /** Filters the view by a property in its filter bar, opening the filter. */
  filterProperty?(propertyId: string): void;
  /** Opens the view's grouping settings beside a group's "⋯", for its "그룹 편집하기". */
  openGroupSettings?(trigger: HTMLElement): void;
}

type ColumnSide = "left" | "right";

type NativeProps = { snapshot: TableViewSnapshot; operations: TableViewOperations; readOnly?: boolean; staticReadOnlySemantics?: boolean };
const NO_ROWS: readonly GraphRow[] = [];
// Groups read from other rows (their titles, what they roll up or work out) are made again whole.
const GROUPS_READ_OTHER_ROWS = new Set(["relation", "rollup", "formula", "verification"]);
function contextFrom(snapshot: TableViewSnapshot, signal: AbortSignal, requestKey: string): TableViewOperationContext {
  return { contextIdentity: snapshot.contextIdentity, dataSourceId: snapshot.dataSource.id, requestGeneration: snapshot.requestGeneration, requestKey, signal, snapshotIdentity: snapshot.snapshotIdentity, viewId: snapshot.view.id };
}

function NativeTableView({ snapshot, operations, readOnly = false, staticReadOnlySemantics = false }: NativeProps) {
  // A preview's column heads set its columns, its rows only looked at (databaseRowsPreview).
  const headReadOnly = useColumnHeadsReadOnly(readOnly), pageNames = usePageNames();
  // Its chosen lines, or rows chosen whole, read on the table's lines as `selectedRows` (chosenAsLines).
  const [chosenIds, setSelectedRows] = useChosenRows(), selectedNow = useRef(chosenIds);
  // Drawn again in a language chosen anew, with its words' namespaces read.
  useTranslation(["tableView", "databaseView"]);
  // "+ 새 페이지" says what a new row of the database is called, as the other views' do (untitledRowName.ts).
  const newPageLabel = useUntitledRowName();
  const newRowExtra = useNewPageExtra(snapshot.view.config);
  const offersNewRows = useViewOffersNewRows();
  const structureLocked = useDatabaseStructureLocked();
  // Its head ends with no "+ ⋯" in a dashboard widget being viewed or in a page's
  // layout tab (its builder's keeps it), as Notion's (seen 2026-11-12).
  const headAddsNone = useDashboardWidgetViewing() || usePageLayoutTabMode() === "page";
  const [expandedRows, setExpandedRows] = useState<ReadonlySet<string>>(new Set());
  // A row shown for a sub-item the filter keeps opens onto it, as Notion's (seen 2026-10-11).
  useEffect(() => { const faded = snapshot.fadedRowIds; if (faded?.size && subitemFilterScope(snapshot.view.config) !== "parents") setExpandedRows((current) => new Set([...current, ...faded])); }, [snapshot.fadedRowIds, snapshot.view.config]);
  // Rows without sub-items opened to add one, as Notion's toggle opens them.
  const [openLeaves, setOpenLeaves] = useState<ReadonlySet<string>>(new Set());
  const [collapsedGroups, setCollapsedGroups] = useCollapsedGroups(snapshot.view.id);
  const [groupMenu, setGroupMenu] = useState<ViewGroupMenuAt | null>(null);
  const [loaded, setLoaded] = useState<{ scope: object | null; rows: readonly GraphRow[] }>({ scope: null, rows: NO_ROWS });
  const [propertyMenu, setPropertyMenu] = useState<{ propertyId: string; left: number; top: number; headerTop?: number; edges: { left: number; right: number }; trigger: HTMLElement; returnFocus: boolean; editing?: boolean; settingsOnly?: boolean; frameWidth?: number } | null>(null);
  // A property inserted from a column's menu is named and typed beside it.
  const [insertion, setInsertion] = useState<{ targetId: string; side: ColumnSide; left: number; top: number; trigger: HTMLElement } | null>(null);
  const [relationDraft, setRelationDraft] = useState<{ beside?: { targetId: string; side: ColumnSide }; name: string; icon?: string; anchor: HTMLElement | null } | null>(null);
  const addOmits: PropertyType[] = snapshot.relationCreation ? [] : ["relation"];
  const idSource = { dataSourceId: snapshot.dataSource.id, has: snapshot.properties.some((property) => property.type === "unique_id") };
  // A relation just made opens on its settings once it shows: the view
  // settings' "속성 편집" in the place it was made in, as Notion's follows
  // "관계형 추가" (seen 2026-11-02); its column's menu where there are no
  // view settings.
  const [editCreated, setEditCreated] = useState<string | null>(null);
  const openPropertySettings = useDatabasePropertySettings();
  const rootRef = useRef<HTMLDivElement>(null);
  // A drag from the table's empty room selects the rows it touches (viewMarquee.tsx).
  const marquee = useViewMarquee({ root: rootRef, item: "[data-table-line]", idOf: (row) => row.dataset.tableLine,
    chosen: () => selectedNow.current, choose: (rowIds) => setSelectedRows(new Set(rowIds)), enabled: !readOnly });
  const [rowsOpening, setRowsOpening] = useState(0);
  const [loading, setLoading] = useState(false);
  const lifecycle = useCommittedOperationScope([snapshot.contextIdentity, snapshot.requestGeneration, snapshot.snapshotIdentity]);
  const focusLifecycle = useCommittedOperationScope([snapshot.contextIdentity]);
  const requestCreatedRowFocus = useCreatedTableRowFocus(rootRef, focusLifecycle.signal, readOnly);
  const coordinator = useMemo(() => createListOperationCoordinator(), []);
  const lastSelected = useRef<string | null>(null);
  // Rows loaded on belong to the snapshot they were loaded for, and a new
  // snapshot starts without them: read here, not reset by an effect after it
  // draws, so an edit draws once. (Reset after each keystroke's drawing, fast
  // typing kept React counting nested updates until its 50th threw in a cell
  // editor's next keystroke, which was lost: 2026-11-08, ".co" then "m".)
  const loadedRows = loaded.scope === lifecycle ? loaded.rows : NO_ROWS;
  const addLoadedRows = (rows: readonly GraphRow[]) => setLoaded((current) => (
    { scope: lifecycle, rows: [...(current.scope === lifecycle ? current.rows : []), ...rows] }));
  // The selection outlasts the rows' changes, its own edits included, as
  // Notion's does; another view or source clears it.
  useEffect(() => setSelectedRows(new Set()), [snapshot.contextIdentity]);
  // It comes back when the tab reloads, as Notion's (selectionMemory.ts).
  useKeptChoice(`rows:${snapshot.view.id}`, chosenIds, setSelectedRows, (id) => snapshot.rows.some((row) => row.id === lineRowId(id)), () => snapshot.rows.length > 0);
  // A property's menu stays open while the property changes, as Notion's
  // does, showing it as it now is; another view or source, or the property
  // going away, closes it.
  const menuProperty = propertyMenu ? snapshot.properties.find((property) => property.id === propertyMenu.propertyId) : undefined;
  useEffect(() => setPropertyMenu(null), [snapshot.contextIdentity]);
  useEffect(() => { if (propertyMenu && !menuProperty) setPropertyMenu(null); }, [propertyMenu, menuProperty]);
  useEffect(() => setEditCreated(null), [snapshot.contextIdentity]);
  // The view's own list when nothing more was loaded, so rows changed in it change alone here (lib/rowsReplaced.ts).
  const allRows = useMemo(() => loadedRows.length === 0 ? snapshot.rows as GraphRow[]
    : [...new Map([...snapshot.rows, ...loadedRows].map((row) => [row.id, row])).values()], [loadedRows, snapshot.rows]);
  // Sub-items just turned on open the first row onto its "새 하위 항목", as Notion's (seen 2026-11-03).
  const subitemsWereOn = useRef(Boolean(snapshot.subitemsEnabled));
  useEffect(() => {
    const on = Boolean(snapshot.subitemsEnabled), first = allRows.find((row) => !row.subitemParentId)?.id;
    if (on && !subitemsWereOn.current && first) setOpenLeaves((current) => new Set(current).add(first));
    subitemsWereOn.current = on;
  }, [allRows, snapshot.subitemsEnabled]);
  const properties = useMemo(() => projectVisibleProperties({ properties: snapshot.properties, ...tableColumnSettings(snapshot.view.config) }), [snapshot.properties, snapshot.view.config]);
  // An insert's empty column (tableNewColumn.ts) comes the frame after its box, every row drawn again with it, and goes with it.
  const insertAt = insertion ? properties.findIndex((property) => property.id === insertion.targetId) : -1;
  const deferredInsertAt = useDeferredValue(insertAt < 0 ? undefined : insertAt + (insertion?.side === "right" ? 1 : 0));
  const newColumnAt = insertAt < 0 ? undefined : deferredInsertAt;
  const statusCheckboxIds = useMemo(() => tableStatusCheckboxIds(snapshot.view.config), [snapshot.view.config]);
  const fullUrlPropertyIds = useMemo(() => tableFullUrlColumnIds(snapshot.view.config), [snapshot.view.config]);
  const wrappedPropertyIds = useMemo(() => tableWrappedColumnIds(snapshot.view.config, snapshot.properties.map((property) => property.id)),
    [snapshot.properties, snapshot.view.config]);
  // A selected row that goes away leaves the selection, one the filter hides stays; copies and rows brought back are selected as they come.
  const chooseRows = useTableRowChoice(allRows, chosenIds, setSelectedRows, snapshot.sourceRows);
  const nested = snapshot.subitemsFlat !== true;
  const groupPropertyId = typeof snapshot.view.config.groupBy === "string" ? snapshot.view.config.groupBy : null;
  const groupProperty = groupPropertyId ? snapshot.properties.find((property) => property.id === groupPropertyId) : undefined;
  // Grouped, a row whose value moves it to another group moves as soon as
  // the groups are worked out behind the press (useDeferredPlacing), its
  // value showing at once (groupedLinesAsNow), as an ungrouped table's order
  // is worked out before it comes here (NativeTableViewMount); rows made or
  // taken out are placed at once.
  const placing = useDeferredPlacing(allRows, snapshot.view);
  const groupingRows = groupProperty ? placing.rows : allRows;
  const projectedInput = useMemo(() => tableProjectedRows(groupingRows, nested), [groupingRows, nested]);
  // Groups are named and ordered by what their property holds.
  const placedProjection = useMemo(() => {
    const pagesById = snapshot.propertyValueContext?.pagesById;
    const grouper = groupProperty ? tableGrouper(groupProperty, { config: snapshot.view.config, currentUserId: snapshot.currentUserId,
      ...tableGroupPages(snapshot.properties, groupingRows, pagesById) }) : null;
    const groupsOf = groupProperty && grouper ? tableGroupsOf(groupProperty, grouper) : null;
    const groupValue = groupProperty ? tableGroupValueReader(groupProperty, snapshot.properties, groupingRows, pagesById,
      snapshot.propertyValueContext?.propsByDatabaseId) : null;
    // Rows changed in place change alone, moving between groups when theirs did (tableViewModel.ts).
    const projected = !groupsOf || !groupValue ? { ...projectUngroupedTableRows(projectedInput, expandedRows), described: new Map<string, TableGroupDescriptor>() }
      : projectGroupedTableRows(projectedInput, expandedRows, [groupProperty, snapshot.view.config, snapshot.currentUserId, snapshot.properties],
        (item) => groupsOf(groupValue(item.row)), !GROUPS_READ_OTHER_ROWS.has(groupProperty!.type));
    const described = new Map(projected.described);
    // As Notion's, a table shows its empty groups when told to, leaves out
    // the hidden ones and keeps an order set by hand.
    const groups = grouper && groupProperty ? arrangeViewGroups(projected.groups, { propertyType: groupProperty.type, propertyConfig: groupProperty.config,
      grouper, config: snapshot.view.config, described, empty: (key) => ({ key, rows: [] }) }) : projected.groups;
    return { ...projected, groups };
  }, [groupingRows, expandedRows, groupProperty, projectedInput, snapshot.currentUserId, snapshot.properties, snapshot.propertyValueContext?.pagesById, snapshot.view.config]);
  const projection = useMemo(() => groupedLinesAsNow(placedProjection, groupingRows, allRows), [allRows, groupingRows, placedProjection]);
  const viewLines = useMemo(() => viewLinesOf(projection.groups, Boolean(groupProperty)), [groupProperty, projection.groups]);
  const selectedRows = useMemo(() => chosenAsLines(chosenIds, viewLines), [chosenIds, viewLines]); selectedNow.current = selectedRows;
  const { keyDown: selectionKeyDown, joined: rowsJoined } = useTableRowSelectionKeys({ root: rootRef, readOnly, selectedRows, setSelectedRows,
    onDuplicate: () => duplicateSelected(), onCopy: (cut) => copySelected(cut), onMenu: () => openSelectedMenuByKey(), onTrash: () => deleteSelected() });
  // The selected rows in the order shown; the bar's pickers show the first.
  const { rows: selectedList, shown: selectedShown } = useChosenRowList(selectedRows, allRows, projection.rows, snapshot.sourceRows);
  const widths = properties.map((property) => tableColumnWidth(snapshot.view.config, property.id, property.type));
  const rowColor = useMemo(() => conditionalCellColorResolver(snapshot.view.config, snapshot.properties.map(galleryPresentationProperty), {
    pagesById: (snapshot.propertyValueContext?.pagesById ?? {}) as Record<string, Page>, currentUserId: snapshot.currentUserId,
  }, nativeBoardQueryBounds), [snapshot.currentUserId, snapshot.properties, snapshot.propertyValueContext?.pagesById, snapshot.view.config]);
  // What a cell shows, as the database's own search reads it; page find
  // reads rows the table has not drawn from it.
  const cellText = useMemo(() => {
    const columns = snapshot.properties.map(galleryPresentationProperty);
    const byId = new Map(columns.map((property) => [property.id, property]));
    // Read as they are, not copied: a copy of 100,000 rows' pages took some 40 ms each time.
    const context = { props: columns, pagesById: (snapshot.propertyValueContext?.pagesById ?? {}) as Record<string, Page>,
      currentUserId: snapshot.currentUserId, bounds: nativeBoardQueryBounds };
    return (page: Page, property: GraphProperty) => {
      const column = byId.get(property.id);
      return column ? propertySearchText(page, column, context) : "";
    };
  }, [snapshot.currentUserId, snapshot.properties, snapshot.propertyValueContext?.pagesById]);
  const calculations = (snapshot.view.config.calculations && typeof snapshot.view.config.calculations === "object" ? snapshot.view.config.calculations : {}) as Record<string, unknown>;
  const placeOpensIn = useContext(RowsOpenInContext);
  const newRowButton = <button type="button" className={styles.newRow} data-table-new-row aria-label={newPageLabel} disabled={rowsOpening > 0}
    onMouseDown={keepFocusAtPress} onClick={() => void addRow(null, null, undefined, newRowOpensItsPage())}><Plus size="small" aria-hidden="true" /> {newPageLabel}</button>;

  async function run<Result>(requestKey: string, perform: (context: TableViewOperationContext) => Promise<Result>, publish: (value: Result) => void = () => {}) {
    const context = contextFrom(snapshot, lifecycle.signal, requestKey);
    return coordinator.run(context, { isCurrent: operations.isCurrent, onError: (error, context) => operations.onError(error, context), perform, publish });
  }
  async function mutate<Result>(requestKey: string, perform: (context: TableViewOperationContext) => Promise<Result>) {
    if (headReadOnly) return false; // A write is never refused for another of its key still saving (listViewModel.ts).
    return coordinator.write(contextFrom(snapshot, lifecycle.signal, requestKey), { isCurrent: operations.isCurrent, onError: (error, context) => operations.onError(error, context), perform, publish: () => {} });
  }
  // A new row opens its page where the table would leave it out (newViewRow.ts), from its no-results state too only then, and on a touch screen (newRowOpensItsPage).
  async function addRow(parentId: string | null, afterRowId: string | null, groupKey?: string, open = false, beforeRowId?: string) {
    if (readOnly) return;
    // A row made under a group, or by the "+" of a row in it, starts with that
    // group's value, and with what the view's filters ask for, as Notion's.
    const seed = groupKey && groupProperty ? tableGroupSeedValue(groupProperty, groupKey, snapshot.view.config) : undefined;
    const grouped = seed !== undefined && groupProperty ? { [groupProperty.id]: seed } : {};
    const seeds = { ...graphViewFilterSeeds(snapshot.properties, snapshot.view, {
      currentUserId: snapshot.currentUserId, currentPageId: snapshot.contextPageId, omitPropertyIds: Object.keys(grouped),
    }, nativeBoardQueryBounds), ...grouped };
    open ||= !parentId && !newRowShows(seeds, snapshot); const beside = !open && !parentId && Boolean(afterRowId || beforeRowId); if (!open) afterRowId = tableRowAfterSearch(snapshot.view.config.search, operations.clearSearch, projection, parentId, afterRowId, beforeRowId, groupKey);
    const focus = requestCreatedRowFocus();
    // Only a row whose page opens locks "+ 새 페이지" (for its page's copy), not a save still
    // out (2026-11-19: the name typed in the row before held it 18 s at a 5 s round trip), nor
    // a row typed in place, made in the press: its button locked lost the focus and took it
    // back in the press's frame (lib/focusThroughWaits.ts, 2026-11-14), as a list's does not.
    if (open) setRowsOpening(count => count + 1);
    // What is typed from the press on is held for its title, which takes the focus only after the row's first frame: letters put in with no key (an input method's, a tool's) reach no button.
    const make = async () => {
      let made = false;
      await run(`row-create:${parentId ?? snapshot.dataSource.id}`,
        async context => {
          // A view keeping its own order puts the row at the database's end; its place is the view's alone.
          const ownOrder = viewKeepsOwnRowOrder(snapshot.view.config) && !parentId;
          // What the press changes with the row draws with it, as it shows (nativeTableRowSteps.ts).
          let shown = false;
          const alongside = (row: GraphRow) => {
            shown = true;
            // A view keeping its own order keeps the row where it was made (viewRowOrder.ts); by a row's "+" in a sorted one it asks whether the sort goes (rowMadeBeside), its title not opened.
            const rowOrder = viewKeepsOwnRowOrder(snapshot.view.config) ? viewRowOrderPlacing(snapshot.view.config, allRows.map(({ id }) => id), [row.id], afterRowId ? { id: afterRowId, side: "after" } : beforeRowId ? { id: beforeRowId, side: "before" } : null) : null;
            if (rowOrder) updateViewConfig({ rowOrder }, `row-order:${snapshot.view.id}`, false); const asking = Boolean(rowOrder && beside && operations.rowMadeBeside?.({ ...snapshot.view, config: { ...snapshot.view.config, rowOrder } }));
            if (parentId) setExpandedRows(current => new Set(current).add(parentId));
            const group = groupProperty ? tableGrouper(groupProperty, { config: snapshot.view.config, currentUserId: snapshot.currentUserId }) : null;
            const value = groupProperty ? tableGroupValueReader(groupProperty, snapshot.properties, [row], snapshot.propertyValueContext?.pagesById) : null;
            const key = group?.(value?.(row) ?? null).key ?? "__no_group__";
            setCollapsedGroups(current => { if (!current.has(key)) return current; const next = new Set(current); next.delete(key); return next; });
            // It opens with its title ready for typing, as Notion's group "+" (seen 2026-11-05).
            if (open) { requestPageTitleFocus(row.id); operations.openRow(row.id, context); }
            else if (!asking) { focus(row.id); operations.freshRow?.(row.id, row.values); }
          };
          const row = await operations.createRow(parentId, ownOrder ? null : afterRowId, context, seeds, ownOrder ? undefined : beforeRowId, !open, alongside);
          // The adapter admits publication before changing the row snapshot.
          // That own change must not invalidate the creation's focus gesture.
          if (!row) return null;
          if (!shown) alongside(row);
          made = true;
          return row;
        });
      return made;
    };
    try {
      await (open ? holdTypingWhileRowOpens(make) : holdTypingForField(make));
    } finally { if (open) setRowsOpening(count => Math.max(0, count - 1)); }
  }
  // "+ 신규 그룹" under the last group adds what its grouping takes (viewGroupAddKind), as a board's does.
  const addKind = !useViewAddsGroups("table") || readOnly || structureLocked ? undefined : viewGroupAddKind(groupProperty, snapshot.view.config);
  // The named group shows at once, though empty, as Notion's does.
  const keepGroup = useKeepNewGroup((key) => replaceViewConfig(tableConfigAfterGroupKept(snapshot.view.config, key), `view-group-kept:${key}`, false));
  function addGroup(name: string) {
    if (!groupProperty || !name.trim()) return;
    if (addKind && addKind !== "option") {
      const key = viewGroupAddedKey(addKind, name.trim());
      return replaceViewConfig(tableConfigAfterGroupKept(snapshot.view.config, key, addKind === "person" ? projection.groups.map((group) => group.key) : undefined), `view-group-kept:${key}`);
    }
    const config = groupProperty.config as Record<string, unknown>;
    const options = Array.isArray(config.options) ? config.options : [];
    const option = newNamedOption(name);
    const next = { ...groupProperty, config: { ...config, options: [...options, option] } };
    void mutate(`property:${groupProperty.id}`, (context) => operations.commitProperties([next], context))
      .then(() => keepGroup(option.id));
  }
  // A locked database keeps its rows editable but not its view and properties.
  function updateViewConfig(patch: Record<string, unknown>, requestKey: string, step = true) { if (structureLocked) return; void mutate(requestKey, (context) => operations.updateView({ ...snapshot.view, config: { ...snapshot.view.config, ...patch } }, context, step)); }
  function replaceViewConfig(config: Record<string, unknown>, requestKey: string, step = true) { if (structureLocked) return; void mutate(requestKey, (context) => operations.updateView({ ...snapshot.view, config }, context, step)); }
  function updateCell(row: GraphRow, property: GraphProperty, value: unknown) { void mutate(`row:${row.id}`, (context) => operations.commitRows([{ ...row, values: { ...row.values, [property.id]: { type: property.type, value } } }], context)); }
  // A row's box chooses its line; with Shift, the lines from the last box pressed in its group (linesBetween).
  function selectRow(line: string, selected: boolean, range: boolean) {
    const next = new Set(selectedRows);
    if (range && lastSelected.current) linesBetween(viewLines, lastSelected.current, line).forEach((id) => next.add(id));
    else if (selected) next.add(line); else next.delete(line);
    lastSelected.current = line; setSelectedRows(next);
  }
  // Cells changed together — a range cleared, a value pulled down its column — are one step of ⌘Z.
  function updateCells(changes: readonly { row: GraphRow; property: GraphProperty; value: GraphRowValue | null }[]) {
    const rows = new Map<string, GraphRow>();
    for (const { row, property, value } of changes) {
      const current = rows.get(row.id) ?? row;
      rows.set(row.id, { ...current, values: { ...current.values, [property.id]: value ?? { type: property.type, value: null } } });
    }
    if (rows.size) void mutate(`cells:${snapshot.dataSource.id}`, (context) => operations.commitRows([...rows.values()], context));
  }
  // Selected rows go to other apps as their pages, and to a page in Leafen as copies of them, as
  // Notion's (rowPagesClipboard.ts), cut ones as leaving their database for it; a paste in a table still takes the rows' values.
  function copySelected(cut = false) {
    const rows = selectedList;
    if (!rows.length) return;
    const copied = rowPagesClipboard(rows.map((row) => ({ id: row.id, href: absolutePageUrl(row.id),
      title: pageNames.title(tableRowPage(row, snapshot.properties, snapshot.workspaceId)) })), cut);
    const payload = tableRowsClipboard(rows, properties);
    void copyTextWithMime(copied.text, LEAFEN_TABLE_ROWS_MIME, tableClipboardEntry(payload),
      { "text/html": copied.html, [LEAFEN_BLOCKS_MIME]: copied.blocks }, TABLE_CLIPBOARD_BOUNDS);
  }
  // Other apps read the cells as shown; a paste in Leafen keeps formatting and, in the same column, the value itself (tablePasteWrite.ts).
  const copyCells = (rows: readonly GraphRow[], shown: readonly GraphProperty[]) => copyTableCells(snapshot.dataSource.id, rows, shown, snapshot.propertyValueContext);
  const tableRowsClipboard = (rows: readonly GraphRow[], shown: readonly GraphProperty[]) => tableCellsClipboard(snapshot.dataSource.id, rows, shown, snapshot.propertyValueContext);
  // Selected rows go to the trash with their sub-items, a sub-item whose row goes too not sent again; one pressed
  // while another is out goes too, as every write does (mutate; the bar's trash waits on no save: 2026-11-19).
  function deleteSelected() {
    const byId = new Map(allRows.map((row) => [row.id, row])), chosen = chosenRowsOf(selectedRows);
    const underSelected = (row: GraphRow) => {
      for (let parent = row.subitemParentId; parent; parent = byId.get(parent)?.subitemParentId ?? null) {
        if (chosen.has(parent)) return true;
      }
      return false;
    };
    const mutation = selectedRowMutation(selectedList.filter((row) => !underSelected(row)).map((row) => row.id), "delete");
    void mutate("rows:delete", (context) => operations.deleteRows(mutation.rowIds, context));
  }
  // The copies go after the last row shown, in the order their rows show, those the filter hides copied too, and the shown ones'
  // are selected in place of their rows, as in Notion (copiesPlace); a copy for each chosen line (seen 2026-11-24: 나 ticked twice, two copies), placed by the last.
  function duplicateSelected() { duplicateRowsSelectingCopies([...selectedRows], [...selectedRows]); }
  function duplicateRowsSelectingCopies(chosen: readonly string[], lines?: readonly string[]) {
    const parents = new Map(projection.rows.map(({ row }) => [row.id, row.parentId ?? null]));
    const { rowIds: copied, place, shownCount } = copiesPlace(projection.groups.map((group) => group.rows.map(({ row }) => groupLine(groupProperty ? group.key : undefined, row.id))), chosen,
      allRows.map(({ id }) => id), (left, right) => parents.get(left) === parents.get(right));
    const scope = focusLifecycle.signal;
    void mutate("rows:duplicate", async (context) => {
      const copies = await operations.duplicateRows(copied, context, place);
      if (copies?.length && !scope.aborted) chooseRows(copies.slice(0, shownCount), lines);
    });
  }
  // A value set from the selection bar goes to every selected row it may.
  function setSelectedValue(property: GraphProperty, value: unknown, spans?: TextSpan[], typed?: boolean) {
    const shown = selectedList[0]?.values[property.id]?.value;
    const changes = selectedList.filter((row) => !rowValuesLocked(row)).map((row) => ({ ...row, values: { ...row.values,
      [property.id]: { type: property.type, value: selectedRowsCellValue(shown, value, row.values[property.id]?.value), ...(spans?.length ? { spans } : {}) } } }));
    if (changes.length) void mutate(`rows:selected:${property.id}`, (context) => operations.commitRows(changes, context, typed));
  }
  function openSelectedMenuByKey() {
    const more = rootRef.current?.querySelector<HTMLElement>("[data-table-selection-more]");
    const first = [...rootRef.current?.querySelectorAll<HTMLElement>("[data-table-row-id]") ?? []]
      .find((row) => selectedRows.has(row.dataset.tableLine ?? ""));
    // One row: its page item menu (rowMenuByKey.ts), as Notion's (seen 2026-11-12); one row on two lines is two (seen 2026-11-24).
    if (selectedRows.size === 1 && first) { openRowMenuByKey(() => operations.openRowContextMenuFromElement(selectedList[0].id, first, duplicateSelected)); return; }
    if (selectedRows.size < 2 || !first || !operations.openRowsContextMenuAt) { more?.click(); return; }
    const row = first.getBoundingClientRect();
    const cell = first.querySelector<HTMLElement>("[data-table-cell]")?.getBoundingClientRect() ?? row;
    operations.openRowsContextMenuAt({ rowIds: selectedList.map((line) => line.id),
      titles: rowsPerLine(selectedList, selectedRows).map((line) => tableRowPage(line, snapshot.properties, snapshot.workspaceId).title),
      onDuplicate: duplicateSelected, onTrash: deleteSelected, onValue: setSelectedValue, byKey: true }, { x: inlineStartX(cell, -8, first), y: row.bottom, top: row.top }, first);
  }
  // A paste's rows take its values in one step, a locked row keeping its own; its lines past them are new rows in the same step (tablePasteWrite.ts).
  function paste(targetRows: readonly GraphRow[], columns: readonly GraphProperty[], pasted: TablePaste) {
    if (readOnly) return;
    const lines = pastedLines(pasted, columns, snapshot.propertyValueContext?.pagesById ?? {});
    const changes = targetRows.flatMap((row, offset) => rowValuesLocked(row) ? [] : [{ ...row, values: { ...row.values, ...lines[offset] } }]);
    const rows = lines.length > targetRows.length && pasted.after && operations.pasteRows ? pastedRowsMade({ view: snapshot.view, groupProperty,
      filterSeeds: (omit) => graphViewFilterSeeds(snapshot.properties, snapshot.view, { currentUserId: snapshot.currentUserId, currentPageId: snapshot.contextPageId, omitPropertyIds: omit }, nativeBoardQueryBounds),
      row: (id) => allRows.find((row) => row.id === id), shownIds: () => allRows.map(({ id }) => id) }, lines.slice(targetRows.length), pasted.after) : null;
    if (rows) void mutate(`row-paste:${snapshot.dataSource.id}`, async (context) => {
      const order = rows.order(await operations.pasteRows!(changes, rows.made, context));
      if (order) updateViewConfig({ rowOrder: order }, `row-order:${snapshot.view.id}`, false);
    });
    else if (changes.length) void mutate(`row-paste:${snapshot.dataSource.id}`, (context) => operations.commitRows(changes, context));
  }
  function moveRows(sourceIds: readonly string[], targetId: string, side: "before" | "after", lines?: { to?: string; groupOf(rowId: string): string | undefined }) {
    // The rows dropped are chosen, each by its line where it lands, and stay chosen through ⌘Z and ⌘⇧Z (Notion's, seen 2026-11-11).
    setSelectedRows(new Set(sourceIds.map((id) => lines?.to === undefined ? id : groupLine(lines.to, id))));
    // A view keeping its own order moves the rows in it alone, as Notion's views do; else in the database's one order. Into another group they take its value (tableRowDrop.ts).
    const dropped = tableRowsDropped(snapshot.view, allRows, sourceIds, { id: targetId, side }, groupProperty && { property: groupProperty, groups: projection.groups, lines });
    if (dropped.view && structureLocked) return;
    if (dropped.view && !operations.moveRows) { updateViewConfig({ rowOrder: dropped.view.config.rowOrder }, `row-order:${snapshot.view.id}`); if (dropped.values) void mutate(`row-order:${snapshot.dataSource.id}`, (context) => operations.commitRows(dropped.written, context)); return; }
    void mutate(`row-order:${dropped.view ? snapshot.view.id : snapshot.dataSource.id}`, (context) => operations.moveRows
      ? operations.moveRows(dropped.rows, dropped.view, sourceIds, context, dropped.values) : operations.commitRows(dropped.written, context));
  }
  // A property added from the header's "+" comes after every other and is
  // shown as the table's last column; one inserted from a column's menu
  // comes beside that column. A relation first asks for its database.
  // An icon chosen in the new column's box comes with the property (Notion's, seen 2026-11-03).
  function addProperty(type: PropertyType, typedName: string, beside?: { targetId: string; side: ColumnSide }, icon?: string) {
    if (structureLocked) return;
    if (type === "relation") { setRelationDraft({ beside, name: typedName, icon, anchor: rootRef.current }); return; }
    const ordered = [...snapshot.properties].sort(compareGraphOrder);
    const target = beside ? ordered.findIndex((property) => property.id === beside.targetId) : -1;
    const [before, after] = target < 0 ? [ordered.at(-1), undefined]
      : beside?.side === "left" ? [ordered[target - 1], ordered[target]] : [ordered[target], ordered[target + 1]];
    const property: GraphProperty = {
      id: newId(), dataSourceId: snapshot.dataSource.id, type,
      name: newTablePropertyName(typedName, propertyTypeLabel(type), snapshot.properties.filter((property) => property.dataSourceId === snapshot.dataSource.id).map((property) => property.name)),
      position: graphPositionBetween(before?.position, after?.position),
      config: { ...(configForType(type, undefined, snapshot.dataSource.id) ?? {}), ...(icon ? { icon } : {}) },
    };
    const withNew = [...snapshot.properties, property];
    const config = beside
      ? tableConfigAfterInsert(snapshot.view.config, withNew, property.id, beside.targetId, beside.side)
      : tableConfigAfterAdd(snapshot.view.config, withNew, property.id);
    void mutate(`property-create:${property.id}`, (context) =>
      operations.commitProperties([property], context, config ? { ...snapshot.view, config } : undefined));
  }
  function openPropertyMenu(property: GraphProperty, element: HTMLElement, editing = false, settingsOnly = false) {
    const rect = element.getBoundingClientRect();
    // Its closing gives the focus back to its header only when the keys opened it: pressed,
    // Notion's leaves it on the page, with no ring or grip left on the header (seen 2026-11-06).
    const returnFocus = keysLedFocus(element);
    // A header's menu opens right under it, 6px before its start, or over it
    // where the window ends, as Notion's (seen 2026-11-02); a rollup cell's
    // form over the cell, 2 above it (seen 2026-11-03). Its point is where
    // the menu starts (placeAtPoint): the header's right read from the right.
    setPropertyMenu(settingsOnly
      ? { propertyId: property.id, left: inlineStartX(rect, 0, element), top: rect.top - 2, edges: { left: rect.left, right: rect.right }, trigger: element, returnFocus, editing, settingsOnly }
      : { propertyId: property.id, left: inlineStartX(rect, -6, element), top: rect.bottom, headerTop: rect.top, edges: { left: rect.left, right: rect.right }, trigger: element, returnFocus, editing,
        // The frame the table is in, which bounds how much of it may freeze.
        frameWidth: element.closest("[data-row-peek-panel], #app-main-content")?.getBoundingClientRect().width ?? window.innerWidth });
  }
  useEffect(() => {
    if (!editCreated) return;
    const property = snapshot.properties.find((candidate) => candidate.id === editCreated);
    if (property && openPropertySettings) {
      setEditCreated(null);
      openPropertySettings(property.id);
      return;
    }
    const header = Array.from(rootRef.current?.querySelectorAll<HTMLElement>("[data-property-id]") ?? [])
      .find((element) => element.dataset.propertyId === editCreated)?.querySelector<HTMLElement>("button");
    if (!property || !header) return;
    setEditCreated(null);
    header.scrollIntoView({ block: "nearest", inline: "nearest" });
    openPropertyMenu(property, header, true);
  }, [editCreated, openPropertySettings, properties, snapshot.properties]);
  function relationCreated(property: GraphProperty) {
    const beside = relationDraft?.beside;
    const icon = relationDraft?.icon;
    setRelationDraft(null);
    if (icon) void mutate(`property-icon:${property.id}`, (context) => operations.commitProperties([{ ...property, config: { ...property.config, icon } }], context));
    setEditCreated(property.id);
    const withNew = [...snapshot.properties, property];
    replaceViewConfig(beside ? tableConfigAfterInsert(snapshot.view.config, withNew, property.id, beside.targetId, beside.side)
      : tableConfigAfterAdd(snapshot.view.config, withNew, property.id), `view-property-order:${snapshot.dataSource.id}`, false);
  }
  // A closed column menu first takes a name still being typed (its field
  // loses focus to the header), then leaves the focus on the page with no
  // ring on the header, as Notion's (2026-11-15, Chrome, no script before the
  // action; judged by the screen and one typed letter: a header pressed,
  // Escape — no ring, the letter landing nowhere, the page's body focused).
  function closePropertyMenu() {
    // A rollup cell's form, opened by a press or the keys, leaves its cell
    // chosen as it closes, as Notion's keeps its value (seen 2026-11-11).
    const trigger = propertyMenu?.returnFocus || propertyMenu?.settingsOnly ? propertyMenu.trigger : null;
    setPropertyMenu(null);
    if (trigger?.isConnected) { trigger.focus({ preventScroll: true }); if (!propertyMenu?.settingsOnly) trigger.blur(); }
  }
  function moveProperty(sourceId: string, targetId: string, side: "before" | "after") {
    const next = tableConfigAfterReorder(snapshot.view.config, snapshot.properties, sourceId, targetId, side);
    if (next) replaceViewConfig(next, `view-property-order:${snapshot.dataSource.id}`);
  }
  async function loadRows() { setLoading(true); try { await run("rows:continue", (context) => operations.loadMoreRows(snapshot.rowNextCursor, context), (result) => addLoadedRows(result.rows)); } finally { setLoading(false); } }
  async function loadSubitems(rowId: string) { const window = snapshot.subitemWindows[rowId]; setLoading(true); try { await run(`subitems:${rowId}`, (context) => operations.loadMoreSubitems(rowId, window?.nextCursor ?? null, context), (result) => addLoadedRows(result.rows)); } finally { setLoading(false); } }
  function openRow(rowId: string, mode: TableRowOpenMode = "default") { const context = contextFrom(snapshot, lifecycle.signal, "open-row"); if (!operations.isCurrent(context)) return; if (mode === "default") operations.openRow(rowId, context); else operations.openRowIn(rowId, mode, context); }

  const barProperties = useSelectionBarProperties(properties, selectedList.map(({ id }) => id));
  // A search or filter leaving no row, or no group, shows Notion's two actions under the header (seen 2026-10-11); no row at all for one who may not add one, why (DatabaseNoResults).
  const noResults = (snapshot.query?.queried === true || readOnly) && projection.rows.length === 0 && (!groupProperty || projection.groups.length === 0);
  const inWidget = useContext(DashboardWidgetHeightContext) !== null;
  const noResultsBox = noResults ? <DatabaseNoResults searching={snapshot.query?.searching === true} nothingToAdd={snapshot.query?.queried !== true} onEditFilters={operations.openFilterSettings}
    onClearSearch={operations.clearSearch} onAddRow={readOnly ? undefined : () => void addRow(null, null, undefined, newRowOpensItsPage())} /> : null;
  return <div ref={rootRef} onKeyDown={selectionKeyDown} {...{ [VIEW_CHOICE_ROOT]: "" }}>
    {!readOnly && <TableSelectionToolbar count={rowsJoined ? 0 : selectedRows.size} onClear={() => setSelectedRows(new Set())}
      onDelete={deleteSelected} onDuplicate={duplicateSelected}
      onRowMenu={(trigger, point) => {
        if (selectedRows.size === 1) { operations.openRowContextMenuFromElement(selectedList[0].id, trigger, duplicateSelected); return true; }
        if (!operations.openRowsContextMenuAt) return false;
        operations.openRowsContextMenuAt({ rowIds: selectedList.map((row) => row.id),
          titles: rowsPerLine(selectedList, selectedRows).map((row) => tableRowPage(row, snapshot.properties, snapshot.workspaceId).title),
          onDuplicate: duplicateSelected, onTrash: deleteSelected, onValue: setSelectedValue }, point, trigger);
        return true;
      }}
      properties={barProperties.length > 0 && <TableSelectionProperties environment={snapshot.propertyCellEnvironment}
        page={selectedList[0] && tableRowPage(selectedList[0], properties, snapshot.workspaceId)} properties={barProperties} rows={selectedList}
        onValue={setSelectedValue} />} />}
    <TableGrid openRowIn={placeOpensIn ?? effectiveOpenPageIn(snapshot.view)} onCloseRow={operations.closeRow} collapsedGroups={collapsedGroups} expandedRows={expandedRows}
      groups={projection.groups} grouped={Boolean(groupProperty)} hasMore={snapshot.rowHasMore}
      onAddGroupRow={readOnly ? undefined : (groupKey) => void addRow(null, null, groupKey, newRowOpensItsPage())}
      onEditEmptyGroupFilters={tableViewFiltered(snapshot.view.config) && (!readOnly || snapshot.dataSource?.sourcePageInTrash) ? operations.openFilterSettings : undefined}
      onAddGroupRowFirst={readOnly ? undefined : (groupKey, beforeRowId) => void addRow(null, null, groupKey, true, beforeRowId ?? undefined)}
      onGroupMenu={readOnly || !groupProperty ? undefined : (group, element) => setGroupMenu({ group, position: popoverAnchorOf(element), returnTo: element })}
      groupCounts={snapshot.view.config.showGroupCounts === true}
      groupAdd={addKind && groupProperty ? (className) => <ViewGroupAdd kind={addKind} property={groupProperty} environment={snapshot.propertyCellEnvironment}
        className={className} taken={addKind === "option" ? groupOptionNames(groupProperty) : projection.groups.map((group) => group.label ?? "")}
        has={(value) => addKind !== "option" && projection.groups.some((group) => group.key === viewGroupAddedKey(addKind, value))} onAdd={addGroup} /> : undefined}
      loading={loading} properties={properties}
      loadLimit={snapshot.rowLoadLimit} groupRowLimit={snapshot.groupRowLimit} groupRowStep={snapshot.groupRowStep} madeRowIds={snapshot.madeRowIds}
      onLoadLimit={readOnly || structureLocked ? undefined : (limit) => updateViewConfig({ initialLoadLimit: limit }, `view-load-limit:${snapshot.view.id}`)}
      propertyCellEnvironment={snapshot.propertyCellEnvironment} propertyValueContext={snapshot.propertyValueContext}
      readOnly={readOnly} selectedRows={selectedShown} onSelectAll={(rowIds) => setSelectedRows(new Set(rowIds))} staticReadOnlySemantics={staticReadOnlySemantics}
      subitemWindows={snapshot.subitemWindows} widths={widths} newColumnAt={newColumnAt} workspaceId={snapshot.workspaceId} wrappedPropertyIds={wrappedPropertyIds}
      fullUrlPropertyIds={fullUrlPropertyIds} statusCheckboxIds={statusCheckboxIds}
      frozenColumns={Number(snapshot.view.config.frozenColumns) || 0}
      onFreezeColumns={readOnly || structureLocked ? undefined : (count) => updateViewConfig({ frozenColumns: count || undefined }, `view-frozen:${snapshot.view.id}`)}
      verticalLines={snapshot.view.config.showVerticalLines !== false}
      pageIcons={tableShowsPageIcons(snapshot.view.config)}
      rowColor={rowColor} cellText={cellText}
      onAddRow={(parentId, after, before) => void addRow(parentId, after && lineRowId(after), parentId ? undefined : lineGroup(after ?? before ?? "") ?? projection.groups.find(({ rows }) => rows.some(({ row }) => row.id === lineRowId(after ?? before ?? "")))?.key, false, before && lineRowId(before))}
      onAddProperty={structureLocked || headAddsNone ? undefined : (type, name, icon) => addProperty(type, name, undefined, icon)} addPropertyOmits={addOmits} addPropertyIdSource={idSource} onlyTitle={snapshot.properties.every((property) => property.type === "title")}
      onOpenProperties={operations.openProperties}
      onCellValue={updateCell} onCellsValue={readOnly ? undefined : updateCells} onCopyCells={copyCells} onPasteCells={readOnly ? undefined : paste} onLoadMore={() => void loadRows()} onLoadSubitems={(id) => void loadSubitems(id)}
      onMoveProperty={moveProperty} onMoveRow={moveRows}
      onOpenProperty={(property, element) => openPropertyMenu(property, element)}
      onConfigureProperty={readOnly || structureLocked ? undefined : (property, cell) => openPropertyMenu(property, cell, false, true)}
      onOpenRow={openRow} onOpenRowContextMenu={(rowId, event) => {
        // A row's own menu selects it but on a phone (rowMenuChoosesRow), and its duplicate leaves the copy selected; the press
        // leaves no cell being edited behind the menu (Notion's, seen 2026-11-05), but for a long press on its words (longPressKeptForWords).
        const editing = document.activeElement;
        if (editing instanceof HTMLElement && event.currentTarget.contains(editing)) { if (longPressKeptForWords(event)) return; editing.blur(); }
        const line = (event.target as Element).closest<HTMLElement>("[data-table-line]")?.dataset.tableLine ?? rowId;
        if (!selectedRows.has(line) && rowMenuChoosesRow()) setSelectedRows(new Set([line]));
        operations.openRowContextMenu(rowId, event, () => duplicateRowsSelectingCopies([line]));
      }}
      onOpenRowContextMenuFromElement={(rowId, element) => operations.openRowContextMenuFromElement(rowId, element, () => duplicateRowsSelectingCopies([element.closest<HTMLElement>("[data-table-line]")?.dataset.tableLine ?? element.dataset.tableHandleLine ?? rowId]))}
      onResizeProperty={(id, width) => replaceViewConfig(tableConfigAfterResize(snapshot.view.config, snapshot.properties, id, resizeTableColumn(width, properties.find((property) => property.id === id)?.type)), `view-width:${id}`)}
      onSelectRow={selectRow}
      onToggleGroup={(id) => setCollapsedGroups((current) => { const next = new Set(current); if (next.has(id)) next.delete(id); else next.add(id); return next; })}
      onToggleRow={(id) => setExpandedRows((current) => { const next = new Set(current); if (next.has(id)) next.delete(id); else next.add(id); return next; })}
      subitems={nested && (Boolean(snapshot.subitemsEnabled) || rowsUnderParents(allRows).length > 0) && (snapshot.subitemsEnabled === "first" ? "first" : true)} openLeaves={openLeaves} fadedRowIds={snapshot.fadedRowIds}
      subitemMeta={snapshot.flatSubitems ? (row) => tableSubitemMetaShown(row, snapshot.flatSubitems!)
        ? <TableSubitemMeta row={row} flat={snapshot.flatSubitems!} onOpenParent={(id) => openRow(id)} /> : null : undefined}
      onToggleLeaf={(id) => setOpenLeaves((current) => { const next = new Set(current); if (next.has(id)) next.delete(id); else next.add(id); return next; })}
      // Under the head inside the table's sideways scroll, its bar under it (Notion's, seen 2026-11-12); a widget's over its card (DashboardWidgets).
      footer={noResults ? inWidget ? null : <div className={styles.tableNoResults} data-table-no-results>{noResultsBox}</div> : <>
        {/* As Notion's, "New page" and the calculations sit in the table under its first column and scroll with it. */}
        {/* Grouped, each group has its own; with no group to show, the table's stays. */}
        {!readOnly && offersNewRows && !(groupProperty && projection.groups.length > 0) && (allRows.length === 0 && !groupProperty
          ? <TableEmptyRows widths={widths} newColumnAt={newColumnAt} newRow={<>{newRowButton}{newRowExtra}</>} />
          : <div className={styles.newRowRail} role="row" data-table-new-row-rail style={{ gridTemplateColumns: "var(--table-gutter-width) minmax(0, 1fr)" }}>
            <div className={styles.newRowGutterCell} role="gridcell" aria-hidden="true" />
            <div className={styles.newRowCell} role="gridcell">{newRowButton}{newRowExtra}</div>
          </div>)}
        <TableSummaryRow properties={properties} allProperties={snapshot.properties} rows={snapshot.viewRows ?? allRows} widths={widths} newColumnAt={newColumnAt} calculations={calculations}
          limitLargeCounts={snapshot.view.config.limitLargeCounts === true} pagesById={snapshot.propertyValueContext?.pagesById}
          propertiesByDatabaseId={snapshot.propertyCellEnvironment?.snapshot.propertiesByDataSourceId}
          workspaceId={snapshot.workspaceId} readOnly={readOnly}
          onChange={(id, value) => replaceViewConfig(tableConfigAfterCalculation(snapshot.view.config, id, value), `view-calculation:${id}`)}
          onLimitLargeCounts={(limit) => updateViewConfig({ limitLargeCounts: limit || undefined }, `view-limit-large-counts:${snapshot.view.id}`)} />
      </>} />
    {inWidget && noResultsBox}
    {groupMenu && groupProperty && <ViewGroupOptions at={groupMenu} property={groupProperty} config={snapshot.view.config} structureLocked={structureLocked}
      onConfig={(next) => replaceViewConfig(next, `view-group-options:${snapshot.view.id}`)}
      onProperty={(next) => void mutate(`property:${next.id}`, (context) => operations.commitProperties([next], context))}
      onTrash={(ids) => void mutate(`group-trash:${groupMenu.group.key}`, (context) => operations.deleteRows(ids, context))}
      onOpenSettings={operations.openGroupSettings} onClose={() => setGroupMenu(null)} />}
    {propertyMenu && menuProperty && <TablePropertyMenu anchor={{ left: propertyMenu.left, top: propertyMenu.top, headerTop: propertyMenu.headerTop }} editing={propertyMenu.editing} settingsOnly={propertyMenu.settingsOnly} config={snapshot.view.config} readOnly={headReadOnly || structureLocked} shownPropertyIds={properties.map((property) => property.id)} frameWidth={propertyMenu.frameWidth} onViewConfig={(next) => replaceViewConfig(next, `view-config:${snapshot.view.id}`)} onDuplicate={(duplicate) => { const sourceId = menuProperty.id; const config = tableConfigAfterDuplicate(snapshot.view.config, [...snapshot.properties, duplicate], sourceId, duplicate.id); void mutate(`property-duplicate:${sourceId}`, (context) => operations.commitProperties(propertiesAfterDuplicate(snapshot.properties, sourceId, duplicate, config.propertyOrder), context, { ...snapshot.view, config })); }} contextIdentity={snapshot.contextIdentity} dataSource={snapshot.dataSource} formulaBounds={snapshot.formulaBounds} pagesById={snapshot.propertyCellEnvironment?.snapshot.pagesById} propertiesByDataSourceId={snapshot.propertyCellEnvironment?.snapshot.propertiesByDataSourceId} property={menuProperty} properties={snapshot.properties} relationCreation={snapshot.relationCreation} snapshotIdentity={snapshot.snapshotIdentity} workspaceId={snapshot.workspaceId} onClose={closePropertyMenu} onDelete={(id) => { void mutate(`property-delete:${id}`, (context) => operations.deleteProperties([id], context)); setPropertyMenu(null); }} onUpdate={(property) => void mutate(`property:${property.id}`, (context) => operations.commitProperties([property], context))}
      onFilter={operations.filterProperty ? () => operations.filterProperty?.(menuProperty.id) : undefined}
      // "왼쪽에 삽입" puts the column on the screen's left: after this one
      // where the table reads from the right. Its menu opens at that edge.
      onInsert={(side) => { setPropertyMenu(null); setInsertion({ targetId: menuProperty.id, side: readsRightToLeft(propertyMenu.trigger) ? (side === "left" ? "right" : "left") : side,
        left: side === "left" ? propertyMenu.edges.left : propertyMenu.edges.right, top: propertyMenu.headerTop ?? propertyMenu.top, trigger: propertyMenu.trigger }); }} />}
    {insertion && <TableInsertProperty anchor={insertion} omit={addOmits} idSource={idSource}
      onClose={() => setInsertion(null)}
      onAdd={(type, name, icon) => addProperty(type, name, { targetId: insertion.targetId, side: insertion.side }, icon)} />}
    {relationDraft && snapshot.relationCreation && <RelationPropertyCreationDialog anchor={relationDraft.anchor} context={snapshot.relationCreation.context} databaseId={snapshot.dataSource.id} initialName={relationDraft.name} takenNames={snapshot.properties.filter((property) => property.dataSourceId === snapshot.dataSource.id).map((property) => property.name)} snapshot={snapshot.relationCreation.snapshot} onCancel={() => setRelationDraft(null)} onCreated={relationCreated} />}
    {operations.rowContextMenu}
    {marquee}
  </div>;
}

export function TableView(props: NativeProps) {
  return <NativeTableView {...props} />;
}
