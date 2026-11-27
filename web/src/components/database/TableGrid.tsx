import { useOwnPageViewFill } from "./useOwnPageViewFill";
import { useTableEndKept } from "./useTableEndKept";
import { useColumnHeadsReadOnly, useDatabaseRowsPreview } from "./databaseRowsPreview";
import { OwnPageViewBar } from "./OwnPageViewBar";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { useDeferredValue, useEffect, useId, useLayoutEffect, useMemo, useRef, useState, type CSSProperties, type KeyboardEvent, type MouseEvent, type ReactElement, type ReactNode } from "react";
import { useDragReorder } from "@/lib/useDragReorder";
import { useKeyedRefs } from "@/lib/useKeyedRefs";
import { useWindowedRows, windowGapStyle } from "@/lib/useWindowedRows";
import { useDataFindSource } from "@/lib/useDataFindSource";
import { useTranslation } from "react-i18next";
import type { GraphProperty, GraphRow, GraphRowValue } from "@/contentGraph";
import type { Page, PropertyType } from "@/lib/types";
import type { UniqueIdSource } from "@/lib/databaseUniqueIdState";
import type { ColorName } from "./colors";
import type { PropertyValueReadContext } from "./PropValue";
import { LOCAL_PROPERTY_CELL_TYPES, type PropertyCellEnvironment } from "./propertyCellTypes";
import { TableAddProperty } from "./TableAddProperty";
import { tableNewColumnPlace, tableTrackWidths, withNewColumnCell } from "./tableNewColumn";
import { TableGroupAddRow } from "./TableGroupAddRow";
import { useFocusedTableRow } from "./useFocusedTableRow";
import { TableGroupHeadCell, type TableGroupHeadWays } from "./TableGroupHeadCell";
import { TableLoadMoreRow } from "./TableLoadMoreRow";
import { TablePropertyHeader } from "./TablePropertyHeader";
import { TableFreezeEdge } from "./TableFreezeEdge";
import { tableRowPage } from "./tableGridRowCells";
import { growingCutWrites, useWrappedTextCut } from "./useWrappedTextCut";
import { usePageWideTable } from "./usePageWideTable";
import { useStickyViewHeads } from "./useStickyViewHeads";
import { TABLE_LOAD_MORE_ROWS } from "./queryProjection";
import { chosenCellAfterKey, stickyPad, tablePart } from "./tableInteractionModel"; import { headBoxPointer, noteColumnsUnderBoxes } from "./tableBoxBand";
import { cellOnLine } from "./tableCellLine";
import { cellClears, cellFills, chooseCellByKey } from "./tableCellKeys";
import { type TableGroup, type TableTreeRow } from "./tableViewModel";
import { groupFirstLines, tableGridLines, type GridLines, type TableProjectedRow } from "./tableGridLines";
import { gridEntryLine, gridGroupLines, tableGridDrag } from "./tableGridDrag";
import { mapItems, placesById } from "@/lib/rowsReplaced";
import { pasteOnTableCell, type TablePaste } from "./tableCellPaste";
import { useTableCellRange, type CellBox } from "./useTableCellRange";
import { useChosenCellOutlivesRow } from "./useChosenCellOutlivesRow";
import { useCellChosenAfterPressAway } from "./useCellChosenAfterPressAway";
import { useHeldCellEditor } from "./useHeldCellEditor";
import { useCellToolsShown } from "./cellToolsShown";
import { useKeysHideRowTools } from "./useKeysHideRowTools";
import styles from "./databaseSheet";
import { useViewOffersNewRows } from "./databaseArchivedRowsContext";
import { rowValuesLocked } from "./rowValuesLocked";
import { useUntitledRowName } from "./untitledRowName";
import { TableRowHandleLayer, TableRowHandles } from "./TableRowHandles";
import { TableGridRow, type TableRowFrame, type TableRowLive } from "./TableGridRow";
import { cellTakesLatestWays, liveCellWays } from "./PropertyCell";
import { drawArrivalsWhole } from "@/lib/arrivingWhole";

export type { TableProjectedRow };

const shownIds = new WeakMap<readonly TableTreeRow<TableProjectedRow>[], readonly string[]>();
const NO_PINS: number[] = [];
const noRowRef = () => {};

type RowPlace = { shown: number; slot: number };

/**
 * Writes where each drawn row stands on it: a row only moved by a row coming
 * or going above it isn't drawn again (TableGridRow), so its places
 * (aria-rowindex, which keys, handles and its cells read, tableCellLine.ts,
 * and the window's data-index) are written here, after each drawing of the
 * table and before it is painted.
 */
function writeRowPlaces(grid: HTMLElement, places: ReadonlyMap<string, RowPlace>) {
  for (const row of grid.querySelectorAll<HTMLElement>('[role="row"][data-table-line]')) {
    const place = places.get(row.dataset.tableLine ?? "");
    if (!place) continue;
    const slot = String(place.slot), aria = String(place.shown + 2);
    if (row.dataset.index !== slot) row.dataset.index = slot;
    if (row.getAttribute("aria-rowindex") !== aria) row.setAttribute("aria-rowindex", aria);
  }
}

// A table longer than this draws only the rows near the view.
const TABLE_WINDOW_THRESHOLD = 100;

type TableGridProps = {
  wrappedPropertyIds?: ReadonlySet<string>;
  /** URL columns showing their links in full ("전체 URL 표시"); the others read short. */
  fullUrlPropertyIds?: ReadonlySet<string>;
  /** Status columns shown as checkboxes (Notion's "다음과 같이 표시: 체크박스"). */
  statusCheckboxIds?: ReadonlySet<string>;
  /** Columns frozen from the left while the table scrolls sideways. */
  frozenColumns?: number;
  /** Freezes the columns up to an edge dragged to; none unfreezes them. */
  onFreezeColumns?(count: number): void;
  /** Whether lines separate the columns (true unless the view hides them). */
  verticalLines?: boolean;
  /** Whether titles show their page's icon (true unless the view hides them). */
  pageIcons?: boolean;
  /** The conditional colors a row's page takes, cell by cell. */
  rowColor?: (page: Page) => ((propertyId: string) => ColorName | null) | null;
  /** The text a cell shows, which page find reads for rows not drawn. */
  cellText?: (page: Page, property: GraphProperty) => string;
  collapsedGroups: ReadonlySet<string>;
  expandedRows: ReadonlySet<string>;
  groups: readonly TableGroup<TableProjectedRow>[];
  /** Whether the view groups its rows: each group then has its own head and "New page", as Notion's. */
  grouped?: boolean;
  /** Makes a row in a group, starting with the group's value. */
  onAddGroupRow?(groupKey: string): void;
  /** Makes a page first in a group and opens it, from the group head's "+", as Notion's (seen 2026-10-25). */
  onAddGroupRowFirst?(groupKey: string, beforeRowId: string | null): void;
  /** Opens a group's "⋯" beside it. */
  onGroupMenu?(group: TableGroup<TableProjectedRow>, element: HTMLElement): void;
  /** Whether group heads show how many pages they hold (Notion's "집계 표시", off at first). */
  groupCounts?: boolean;
  /** "+ 신규 그룹" under the last group (ViewGroupAdd), given the class that places it. */
  groupAdd?(className: string): ReactNode;
  /** A filtered table's way to its filters, from an empty group. */
  onEditEmptyGroupFilters?(trigger: HTMLElement): void;
  hasMore: boolean;
  loading: boolean;
  /** How many rows the table first shows, when it shows only its first ones. */
  loadLimit?: number;
  /** Chooses how many rows the table first shows, from its "더 불러오기" line. */
  onLoadLimit?(limit: number): void;
  /** How many rows each group first shows, when a grouped table shows only its first ones. */
  groupRowLimit?: number;
  /** How many more a group's own "더 불러오기" shows. */
  groupRowStep?: number;
  /** Rows made here, which show however few rows a group shows. */
  madeRowIds?: ReadonlySet<string>;
  properties: readonly GraphProperty[];
  propertyCellEnvironment?: PropertyCellEnvironment;
  propertyValueContext?: PropertyValueReadContext;
  readOnly: boolean;
  selectedRows: ReadonlySet<string>;
  staticReadOnlySemantics: boolean;
  subitemWindows: Readonly<Record<string, { hasMore: boolean } | undefined>>;
  widths: readonly number[];
  workspaceId: string;
  /** Adds a row after the given one, or before the one given last (Option with the row's "+"). */
  onAddRow(parentId: string | null, afterLine: string | null, beforeLine?: string): void;
  /** Adds a property from the "+" after the last column, when the table may. */
  onAddProperty?(type: PropertyType, typedName: string, icon?: string): void;
  /** Types the "+" leaves out, such as a relation where none can be made. */
  addPropertyOmits?: readonly PropertyType[];
  /** The source new properties go to, whose ID property dims "ID" there. */
  addPropertyIdSource?: UniqueIdSource;
  /** The source has no property but its title: "+ 속성 추가" in words, as Notion's; a hidden one leaves "+ ⋯" to bring it back. */
  onlyTitle?: boolean;
  /** Opens the view's properties menu beside the header's "⋯". */
  onOpenProperties?: (trigger: HTMLElement) => void;
  /** The rows under the table's rows ("New page", the calculations), which scroll with its columns. */
  footer?: ReactNode;
  /** Where a column's "왼쪽에 삽입"/"오른쪽에 삽입" makes room for one among the shown columns (tableNewColumn.ts). */
  newColumnAt?: number;
  onCellValue(row: GraphRow, property: GraphProperty, value: unknown): void;
  /** Cells changed together — a range cleared, a value pulled down its column — as one step. */
  onCellsValue?(changes: readonly { row: GraphRow; property: GraphProperty; value: GraphRowValue | null }[]): void;
  /** Copies a range's cells, row by row. */
  onCopyCells?(rows: readonly GraphRow[], properties: readonly GraphProperty[]): void;
  /** Pastes into the rows shown from a cell on and its column on, as the table lists them; its lines past them are new rows (tableCellPaste). */
  onPasteCells?(rows: readonly GraphRow[], properties: readonly GraphProperty[], paste: TablePaste): void;
  onLoadMore(): void;
  onLoadSubitems(rowId: string): void;
  onMoveProperty(sourceId: string, targetId: string, side: "before" | "after"): void;
  /** `lines`: the group each row left and the one it lands in, a row under several groups dragged from one (tableGridDrag). */
  onMoveRow(sourceIds: readonly string[], targetId: string, side: "before" | "after", lines?: { to?: string; groupOf(rowId: string): string | undefined }): void;
  onOpenProperty(property: GraphProperty, returnTo: HTMLElement): void;
  /** A rollup with no relation yet opens its settings from its cell, as Notion's (seen 2026-10-17). */
  onConfigureProperty?(property: GraphProperty, cell: HTMLElement): void;
  /** Closes the side peek; the row it shows has "닫기" in place of "열기", as Notion's (seen 2026-11-06). */
  onCloseRow?(): void;
  onOpenRow(rowId: string, mode?: "default" | "new_tab" | "side"): void;
  /** Where the view opens its pages, which the row's button says, as Notion's. */
  openRowIn?: "side" | "center" | "full";
  onOpenRowContextMenu(rowId: string, event: MouseEvent<HTMLElement>): void;
  onOpenRowContextMenuFromElement(rowId: string, element: HTMLElement): void;
  onResizeProperty(propertyId: string, width: number): void;
  onSelectRow(rowId: string, selected: boolean, range: boolean): void;
  /** Selects these rows in place of the selection: the head's box. */
  onSelectAll?(rowIds: readonly string[]): void;
  onToggleGroup(groupId: string): void;
  onToggleRow(rowId: string): void;
  /** Whether sub-items are on: every row then keeps a toggle's room, on its title or in its first column ("first"). */
  subitems?: boolean | "first";
  /** Rows without sub-items opened to add one. */
  openLeaves?: ReadonlySet<string>;
  /** Rows shown only around those the filter keeps. */
  fadedRowIds?: ReadonlySet<string>;
  onToggleLeaf?(rowId: string): void;
  /** What follows a title in a flattened list of sub-items: a count or a parent. */
  subitemMeta?(row: GraphRow): ReactNode;
};

export { tableRowPage };

// A list made again with the same items, kept as drawn: the view's columns
// and their settings are read anew at each change of the view, and the rows
// drawn with them are drawn again only when they differ (a filter or a
// calculation picked drew every row of the table again).
function useSameItems<T>(items: readonly T[]): readonly T[] {
  const kept = useRef(items);
  const last = kept.current;
  if (last !== items && (last.length !== items.length || items.some((item, index) => item !== last[index]))) kept.current = items;
  return kept.current;
}

type HeadWays = {
  openProperty(property: GraphProperty, returnTo: HTMLElement): void;
  resize(propertyId: string, width: number): void;
  add?(type: PropertyType, typedName: string, icon?: string): void;
  more?(trigger: HTMLElement): void;
  choose(entryKey: string | undefined): void;
};

// The heads' ways, the same functions while the table's change.
function liveHeads(ways: { readonly current: HeadWays }) {
  return {
    openProperty: (property: GraphProperty, returnTo: HTMLElement) => ways.current.openProperty(property, returnTo),
    resize: (propertyId: string, width: number) => ways.current.resize(propertyId, width),
    add: (type: PropertyType, typedName: string, icon?: string) => ways.current.add?.(type, typedName, icon),
    more: (trigger: HTMLElement) => ways.current.more?.(trigger),
  };
}

// An object's number, the same while it is (a head's shape: headRow).
const keys = new WeakMap<object, number>();
let lastKey = 0;
function keyOf(value: object) {
  let key = keys.get(value);
  if (key === undefined) keys.set(value, key = ++lastKey);
  return key;
}

function useSameMembers(members: ReadonlySet<string> | undefined): ReadonlySet<string> | undefined {
  const kept = useRef(members);
  const last = kept.current;
  if (last !== members && (!last || !members || last.size !== members.size || [...members].some((member) => !last.has(member)))) kept.current = members;
  return kept.current;
}

export function TableGrid(props: TableGridProps) {
  const {
    collapsedGroups, expandedRows, groups, hasMore, loading, loadLimit, onLoadLimit,
    propertyCellEnvironment, propertyValueContext, readOnly, selectedRows, staticReadOnlySemantics, subitemWindows, widths,
    workspaceId, onAddRow, onCellValue, onLoadMore, onLoadSubitems, onMoveProperty, onMoveRow,
    onOpenProperty, onOpenRow, onOpenRowContextMenu, onOpenRowContextMenuFromElement, onConfigureProperty,
    onResizeProperty, onSelectRow, onToggleGroup, onToggleRow,
  } = props;
  const properties = useSameItems(props.properties);
  const wrappedPropertyIds = useSameMembers(props.wrappedPropertyIds);
  const fullUrlPropertyIds = useSameMembers(props.fullUrlPropertyIds);
  const statusCheckboxIds = useSameMembers(props.statusCheckboxIds);
  // A group's head folds or opens on the press's frame, its rows going or
  // coming once React has drawn that (useDeferredValue: behind the press,
  // dropped for a newer one).
  const foldedGroups = useDeferredValue(collapsedGroups);
  const { t } = useTranslation(["tableView", "databaseView", "boardView", "pageTreeItem", "propValue"]);
  // A phone opens the row as its own page (useDatabaseViewController), so the button says only "열기".
  const { phone } = useSidebarLayout(true);
  // A preview's rows are only looked at (databaseRowsPreview).
  const preview = useDatabaseRowsPreview();
  const headReadOnly = useColumnHeadsReadOnly(readOnly);
  const newPageLabel = useUntitledRowName();
  const offersNewRows = useViewOffersNewRows();
  // A grouped table's group shows its first rows and 50 more at each of its
  // own "더 불러오기"; a row made here shows at once.
  const [groupMore, setGroupMore] = useState<Readonly<Record<string, number>>>({});
  const { groupRowLimit, groupRowStep = TABLE_LOAD_MORE_ROWS, madeRowIds } = props;
  const shownRows = useMemo(() => new Map(groups.map((group) => {
    if (groupRowLimit === undefined) return [group.key, group.rows] as const;
    const limit = groupRowLimit + groupRowStep * (groupMore[group.key] ?? 0);
    return [group.key, group.rows.filter((item, index) => index < limit || madeRowIds?.has(item.row.row.id))] as const;
  })), [groupMore, groupRowLimit, groupRowStep, groups, madeRowIds]);
  // The table's lines in order — each group's header, its shown rows and its
  // "더 불러오기" — and its rows as shown (tableGridLines.ts).
  // A grouped table shows each group as Notion's does (seen 2026-10-06): its
  // title, its own columns' head, its rows and its own "New page", and
  // "+ 신규 그룹" under the last group.
  const grouped = props.grouped ?? (groups.length > 1 || groups.some((group) => group.key !== "__no_group__"));
  const { onAddGroupRow, onEditEmptyGroupFilters, openLeaves } = props;
  const addsGroup = Boolean(props.groupAdd);
  const subitemAdds = Boolean(props.subitems) && !readOnly;
  const linesDrawn = useRef<GridLines | null>(null);
  const { visibleItems, entries } = useMemo(() => tableGridLines({ groups, shown: shownRows, collapsedGroups: foldedGroups, expandedRows, openLeaves, grouped, subitemAdds,
    readOnly, addGroup: addsGroup, addGroupRow: Boolean(onAddGroupRow), editEmptyGroupFilters: Boolean(onEditEmptyGroupFilters) }, linesDrawn),
  [addsGroup, expandedRows, foldedGroups, grouped, groups, onAddGroupRow, onEditEmptyGroupFilters, openLeaves, readOnly, shownRows, subitemAdds]);
  const shownRowIds = useMemo(() => mapItems(visibleItems, shownIds, (item) => item.row.row.id) as string[], [visibleItems]);
  const allSelected = shownRowIds.length > 0 && shownRowIds.every((id) => selectedRows.has(id));
  const selectAllRef = useRef<HTMLInputElement>(null), drag = useMemo(() => tableGridDrag(entries, selectedRows), [entries, selectedRows]);
  useEffect(() => { if (selectAllRef.current) selectAllRef.current.indeterminate = selectedRows.size > 0 && !allSelected; }, [allSelected, selectedRows.size]);
  const newColumnAt = tableNewColumnPlace(properties.length, props.newColumnAt);
  const tracks = tableTrackWidths(widths, properties.length, newColumnAt);
  const columns = tracks.map((width) => `${width}px`).join(" ");
  const trackOf = (index: number) => newColumnAt !== undefined && index >= newColumnAt ? index + 1 : index;
  const tracksBefore = (track: number) => tracks.slice(0, track).reduce((sum, width) => sum + width, 0);
  // Wider than the page's text, it scrolls across the page's margins (usePageWideTable).
  const scrollRef = useRef<HTMLDivElement>(null);
  // The rows' list, whose lines TanStack Virtual draws (useWindowedRows).
  const listRef = useRef<HTMLDivElement>(null);
  usePageWideTable(scrollRef, tracksBefore(tracks.length));
  // Grouped, each group's head and column head stay at the page's top while its rows pass (useStickyViewHeads).
  useStickyViewHeads(scrollRef, { shape: grouped, rows: grouped ? listRef : undefined });
  useOwnPageViewFill(scrollRef);
  const addable = !readOnly && Boolean(props.onAddProperty);
  // Its head's "+ ⋯" also where only its heads may change: a page layout builder's tab, as Notion's (seen 2026-11-12).
  const headAddable = !headReadOnly && Boolean(props.onAddProperty);
  // Frozen columns stick past the rows' boxes (--table-frozen-zone), each
  // after the ones before it: as Notion's (seen 2026-11-03), the rows' boxes
  // stay in those 36px, frozen or not, and their "+" and handle slide out of
  // view (seen 2026-11-09). Sticking counts from the box's edge: what it
  // keeps before the first column is the table's margin, not its padding
  // (DatabaseTableChrome --table-scroll-pad), and where they stick each
  // sticking cell takes from its row (stickyPad, not inherited).
  const frozen = Math.max(0, Math.min(properties.length, Math.floor(props.frozenColumns ?? 0)));
  const frozenWidth = frozen > 0 ? tracksBefore(trackOf(frozen - 1) + 1) : 0;
  const stickyLeft = (index: number) => index < frozen ? `calc(var(--table-frozen-zone) + ${tracksBefore(trackOf(index))}px)` : undefined;
  // Made among frozen columns, the new column sticks with them.
  const newColumnLeft = newColumnAt !== undefined && newColumnAt < frozen ? `calc(var(--table-frozen-zone) + ${tracksBefore(newColumnAt)}px)` : undefined;
  // Every title shows its page's icon, or the plain page glyph, as Notion's
  // tables do; the view may hide them all.
  const titleIcon = props.pageIcons === false ? "none" : "page";
  const gutterLeft = "calc(var(--table-frozen-zone) - var(--table-gutter-width))";
  // Frozen columns and the row controls stick at the table's start: its right read from the right.
  const gutterStyle: CSSProperties = { ...stickyPad, position: "sticky", insetInlineStart: gutterLeft, zIndex: frozen > 0 ? 2 : undefined };

  // The lines of the part of the table a line stands in: a grouped table's group, its own table to a chosen cell's keys and Enter, a range and a paste (tablePart).
  const partOf = (line: number) => tablePart(grouped ? groupFirstLines(groups, shownRows, foldedGroups) : [], line, visibleItems.length);
  function moveFocus(event: KeyboardEvent<HTMLElement>, row: number, column: number) {
    if (event.target instanceof HTMLInputElement && event.target.type !== "checkbox") return;
    // Arrow keys move the caret within a text cell being edited.
    if (event.target instanceof HTMLElement && event.target.isContentEditable) return;
    // A chosen cell's keys, a group its own table (chosenCellAfterKey); Tab comes here from the chosen cell alone (tabChoosesNextCell).
    const next = chosenCellAfterKey(event.key, event.currentTarget, { row, column }, properties.length, partOf(row));
    if (next) event.preventDefault();
    if (next?.moved) focusCell(next.row, next.column);
  }

  // A cell the keys move to may be in a row not drawn yet: the row is
  // brought into view first, and the cell takes focus once it draws.
  function focusCell(row: number, column: number) {
    const cell = cellOnLine(gridRef.current, row, column);
    if (cell) { chooseCellByKey(cell); return; }
    const index = entries.findIndex((entry, at) => entry.kind === "row" && (entry.displayIndex ?? at) === row);
    if (index < 0) return;
    windowedRows.reveal(index);
    let frames = 0;
    const retry = () => {
      const drawn = cellOnLine(gridRef.current, row, column);
      if (drawn) chooseCellByKey(drawn);
      else if (++frames < 10) window.requestAnimationFrame(retry);
    };
    window.requestAnimationFrame(retry);
  }

  // Columns and rows drag within this table only; a chosen row carries the other chosen rows beside it, as Notion's, and a row under several groups goes by its line (tableGridDrag).
  const gridId = useId();
  // A head dragged goes with the pointer, the heads it passes making way, as Notion's (dragReorderLive).
  const columnReorder = useDragReorder(`table-column:${gridId}`, onMoveProperty, { axis: "horizontal", live: true });
  const rowReorder = useDragReorder(`table-row:${gridId}`, (_sourceId, targetId, side, carried) => { const moved = drag.moved(carried, targetId); onMoveRow(moved.rowIds, moved.targetId, side, moved); }, { band: true, next: (sourceId) => drag.next(sourceId), carry: (sourceId) => drag.carry(sourceId) });
  // Only the cell under the pointer, or whose tools have the focus, draws them.
  const cellTools = useCellToolsShown();
  useKeysHideRowTools(scrollRef, cellTools);
  // The column heads as last drawn, and what their presses do now (headRow).
  const heads = useRef(new Map<string, { shape: string; element: ReactElement }>());
  const headRowIds = useRef(new Map<string, readonly string[]>());
  const headWays = useRef<HeadWays>(null!);
  const [liveHeadWays] = useState(() => liveHeads(headWays));

  // TanStack Virtual draws a long table's rows near the view only; the row
  // being dragged and the row holding focus stay drawn.
  const gridRef = useRef<HTMLDivElement>(null);
  const heldEditor = useHeldCellEditor<TableTreeRow<TableProjectedRow>>(listRef);
  // Escape on chosen cells chooses their rows alone and lets the cells go,
  // as Notion's (seen 2026-11-03); the table's keys then act on the rows.
  const chooseRows = (rowIds: readonly string[]) => {
    props.onSelectAll?.(rowIds);
    const active = document.activeElement;
    if (active instanceof HTMLElement && active.matches("[data-table-cell]")) active.blur();
  };
  // A value a range clears or a pulled value fills: one its reader may change.
  const writable = (row: GraphRow, property: GraphProperty, types: (type: string) => boolean) => !readOnly && types(property.type)
    && !rowValuesLocked(row) && !row.readOnlyValues?.includes(property.id) && !row.hiddenValues?.includes(property.id);
  const boxRows = (box: CellBox) => visibleItems.slice(box.top, box.bottom + 1).map((item) => item.row.row);
  useChosenCellOutlivesRow(gridRef, drag.shown, focusCell);
  useCellChosenAfterPressAway(gridRef);
  const cellRange = useTableCellRange({
    gridRef, part: partOf, columns: properties.length, focusCell,
    onRows: (rows) => chooseRows(rows.flatMap((row) => drag.shown[row] ? [drag.shown[row]] : [])),
    onCopy: (box) => props.onCopyCells?.(boxRows(box), properties.slice(box.left, box.right + 1)),
    onClear: (box) => props.onCellsValue?.(boxRows(box).flatMap((row) => properties.slice(box.left, box.right + 1)
      .filter((property) => writable(row, property, cellClears)).map((property) => ({ row, property, value: null })))),
    onFill: (from, rows) => {
      const property = properties[from.column], source = visibleItems[from.row]?.row.row;
      if (!property || !source) return;
      const value = source.values[property.id] ?? null;
      props.onCellsValue?.(rows.flatMap((at) => {
        const row = visibleItems[at]?.row.row;
        return row && writable(row, property, cellFills) ? [{ row, property, value }] : [];
      }));
    },
  });
  const [focusedLine, setFocusedLine] = useFocusedTableRow(frozen > 0 || entries.length > TABLE_WINDOW_THRESHOLD);
  const groupWays = useRef<TableGroupHeadWays>(null);
  const openLabels = { open: t("openRowLabel"), close: t("closeRowLabel"),
    openAria: phone ? undefined : props.openRowIn === "center" || props.openRowIn === "full" ? t(`databaseView:peekModes.${props.openRowIn}`) : t("openInSidePeek") };
  const wrapCut = useWrappedTextCut(scrollRef, widths.slice(0, properties.length), properties.flatMap((property, index) =>
    wrappedPropertyIds?.has(property.id) ? [index] : []), properties.map((property) => property.id));
  // A cut value written as text shows whole (growingCutWrites).
  const cutWrites = useRef<{ grows(propertyId: string): boolean; expand(rowId: string): void }>({ grows: () => false, expand: wrapCut.expand });
  cutWrites.current = { expand: wrapCut.expand, grows: (propertyId) => {
    const index = properties.findIndex((property) => property.id === propertyId);
    return index >= 0 && Boolean(wrappedPropertyIds?.has(propertyId)) && wrapCut.cuts(index);
  } };
  const cellEnvironment = useMemo(() => propertyCellEnvironment && growingCutWrites(propertyCellEnvironment, cutWrites),
    [propertyCellEnvironment]);
  // A column that wraps lets every row take its values' height (TableRows' .tableRow[data-wraps]).
  const wrapsAny = properties.some((property) => wrappedPropertyIds?.has(property.id));
  useTableEndKept(scrollRef, properties.length);
  // The group the pointer is over, its head's "⋯" and "+" showing, as Notion's
  // show while the pointer is anywhere in the group (seen 2026-11-05).
  const [pointedGroup, setPointedGroup] = useState<string | null>(null);
  const pinned = !rowReorder.dragging && !focusedLine ? NO_PINS : entries.flatMap((entry, index) => entry.kind === "row"
    && (gridEntryLine(entry) === rowReorder.dragging || gridEntryLine(entry) === focusedLine) ? [index] : []);
  const windowedRows = useWindowedRows({
    listRef, count: entries.length, threshold: TABLE_WINDOW_THRESHOLD, pinned, lines: () => drag.shown,
    // Rows after the first frame's, some 120 cells a frame; rows whose height
    // their values give (wrapsAny) would leave a gap of a guessed height.
    progressive: wrapsAny ? 0 : Math.min(20, Math.max(4, Math.round(120 / Math.max(1, properties.length)))),
    estimateSize: (index) => entries[index]?.kind === "row" ? 37 : entries[index]?.kind === "head" ? 36 : 34,
    getItemKey: (index) => entries[index]?.key ?? index,
    // The group the window starts in keeps its heads drawn, staying at the page's top.
    pinFor: grouped ? (start) => {
      let at = Math.min(start, entries.length - 1);
      while (at > 0 && entries[at]?.kind !== "group") at -= 1;
      return entries[at]?.kind !== "group" ? [] : entries[at + 1]?.kind === "head" ? [at, at + 1] : [at];
    } : undefined,
  });
  const measure = useRef(windowedRows.measure);
  measure.current = windowedRows.measure;
  const measureEntry = useKeyedRefs<string>((_key, element) => { measure.current?.(element); });
  const rowElement = useKeyedRefs<string>((id, element) => {
    measure.current?.(element);
    return rowReorder.elementRef(id)(element);
  });
  // Page find reads each cell's text as it shows, drawn or not.
  const { cellText } = props;
  useDataFindSource({
    listRef, enabled: Boolean(cellText),
    // Its lines, the rows' read and the others' none, so they follow the lines as they change (tableGridLines.ts).
    items: entries,
    reveal: (entry) => {
      const index = placesById(entries, (line) => line.key).get(entry.key);
      if (index !== undefined) windowedRows.reveal(index);
    },
    parts: (entry) => {
      if (entry.kind !== "row") return [];
      const row = entry.item.row.row, page = tableRowPage(row, properties, workspaceId);
      return properties.map((property, column) => ({
        text: cellText?.(page, property) ?? "",
        drawn: () => listRef.current?.querySelector<HTMLElement>(
          `[data-table-line="${CSS.escape(gridEntryLine(entry))}"] [data-table-cell][data-col-index="${column}"]`) ?? null,
      }));
    },
  });
  // A row's "+ ⋮⋮", in its row or, the table scrolled under its box's edge, in the handle layer.
  const rowHandles = (row: GraphRow, displayIndex: number, outside = false) => (
    <TableRowHandles rowId={drag.shown[displayIndex] ?? row.id} reorder={rowReorder} outside={outside}
      neighbours={{ above: drag.shown[displayIndex - 1], below: drag.shown[displayIndex + 1] }}
      onAdd={(above) => above ? onAddRow(row.subitemParentId, null, drag.shown[displayIndex] ?? row.id) : onAddRow(row.subitemParentId, drag.shown[displayIndex] ?? row.id)}
      onMenu={(handle) => { props.onSelectAll?.([drag.shown[displayIndex] ?? row.id]); onOpenRowContextMenuFromElement(row.id, handle); }} />
  );
  const lineOf = (target: EventTarget | null) => target instanceof Element ? target.closest("[data-table-line]")?.getAttribute("data-table-line") ?? null : null;

  groupWays.current = { toggle: onToggleGroup, menu: (key, element) => { const group = groups.find((at) => at.key === key); if (group) props.onGroupMenu?.(group, element); },
    add: (key) => props.onAddGroupRowFirst?.(key, groups.find((at) => at.key === key)?.rows[0]?.row.id ?? null) };
  function groupHeader(group: TableGroup<TableProjectedRow>, index: number, key: string) {
    return (
      <div key={key} ref={measureEntry(key)} data-index={index} className={styles.tableGroupHeader} role="row" data-table-group-key={group.key}
        data-group-pointed={pointedGroup === group.key ? "true" : undefined} data-sticky-head="" data-sticky-stack={group.key}>
        <TableGroupHeadCell group={group} span={properties.length + 1} collapsed={collapsedGroups.has(group.key)} ways={groupWays}
          count={props.groupCounts ? group.rows.filter((entry) => entry.depth === 0).length : undefined}
          menu={Boolean(props.onGroupMenu)} add={!readOnly && offersNewRows && Boolean(props.onAddGroupRowFirst)} />
      </div>
    );
  }

  // The columns' head: the table's, or a group's, whose box selects that
  // group's shown rows. A head showing what it showed is the element it was,
  // which React leaves as it is, its presses going by the table as now drawn
  // (headWays): each group's head of 40 columns was drawn again for each
  // press in the table (seen 2026-11-13, a new row in a table of 6 groups:
  // 240 heads each of seven drawings).
  headWays.current = { openProperty: onOpenProperty, resize: onResizeProperty, add: props.onAddProperty, more: props.onOpenProperties,
    choose(entryKey) {
      const rowIds = headRowIds.current.get(entryKey ?? "") ?? [];
      const all = rowIds.length > 0 && rowIds.every((id) => selectedRows.has(id));
      if (entryKey === undefined) return props.onSelectAll?.(all ? [] : rowIds);
      const next = new Set(selectedRows);
      for (const id of rowIds) { if (all) next.delete(id); else next.add(id); }
      props.onSelectAll?.([...next]);
    } };
  const headsShown = new Map<string, { shape: string; element: ReactElement }>();
  // What every head shows alike, read as the heads are drawn (stickyKey comes below).
  const headShape = () => [readOnly, headReadOnly, headAddable, columns, frozen, stickyKey, keyOf(properties), keyOf(t), Boolean(props.onSelectAll),
    Boolean(props.onAddProperty), props.addPropertyOmits?.join(",") ?? "", props.addPropertyIdSource ? `${props.addPropertyIdSource.dataSourceId}:${props.addPropertyIdSource.has}` : "",
    Boolean(props.onOpenProperties), props.onlyTitle ?? "", columnReorder.dragging ?? ""].join("\n");
  function headRow(rowIds: readonly string[], entry?: { key: string; index: number; stack: string }) {
    const all = rowIds.length > 0 && rowIds.every((id) => selectedRows.has(id));
    const some = rowIds.some((id) => selectedRows.has(id));
    const key = entry?.key ?? "", shape = `${headShape()}\n${entry?.index}\n${all}\n${some}\n${selectedRows.size > 0}\n${rowIds.length > 0}`;
    headRowIds.current.set(key, rowIds);
    const kept = heads.current.get(key);
    if (kept?.shape === shape && !columnReorder.dragging) { headsShown.set(key, kept); return kept.element; }
    const element = (
      <div key={entry?.key} ref={entry ? measureEntry(entry.key) : undefined} data-index={entry?.index}
        className={styles.tableHead} role="row" data-table-head data-table-group-head={entry ? "true" : undefined} data-sticky-head="" data-sticky-stack={entry?.stack}
        style={{ gridTemplateColumns: `${readOnly ? "" : "var(--table-gutter-width) "}${columns}${headAddable ? " auto" : ""}` }}>
        {!readOnly && <div className={styles.rowGutterHead} role="columnheader" aria-label={t("rowActions")}
          data-some-selected={(entry ? some : selectedRows.size > 0) || undefined} {...headBoxPointer}
          style={{ ...gutterStyle, zIndex: frozen > 0 ? 3 : undefined }}>
          {/* As Notion's, the head's box selects every shown row, shows "−" while only some are and clears them when all are;
              with no row to choose it isn't there (seen 2026-11-07, an empty filtered table). */}
          {props.onSelectAll && rowIds.length > 0 && <input ref={entry ? (box) => { if (box) box.indeterminate = some && !all; } : selectAllRef}
            className={styles.rowSelect} data-table-select-all type="checkbox"
            aria-label={t("selectAllRows")} checked={all} disabled={rowIds.length === 0} onChange={() => headWays.current.choose(entry?.key)} />}
        </div>}
        {withNewColumnCell(properties.map((property, index) => (
          <TablePropertyHeader key={property.id} property={property} width={widths[index]} readOnly={headReadOnly}
            stickyLeft={stickyLeft(index)} frozenEdge={index === frozen - 1}
            reorder={columnReorder} onOpenMenu={liveHeadWays.openProperty} onResize={liveHeadWays.resize} />
        )), newColumnAt, () => <div key="new-column" aria-hidden="true" data-table-new-column
          style={newColumnLeft ? { ...stickyPad, position: "sticky", insetInlineStart: newColumnLeft, zIndex: 3, background: "var(--table-surface, var(--bg-app))" } : undefined} />)}
        {headAddable && props.onAddProperty && <TableAddProperty onAdd={liveHeadWays.add} omit={props.addPropertyOmits} idSource={props.addPropertyIdSource}
          onMore={props.onOpenProperties ? liveHeadWays.more : undefined} labeled={props.onlyTitle ?? properties.length === 1} />}
      </div>
    );
    headsShown.set(key, { shape, element });
    return element;
  }

  function groupMoreRow(group: TableGroup<TableProjectedRow>, index: number, key: string) {
    return (
      <div key={key} ref={measureEntry(key)} data-index={index}>
        <TableLoadMoreRow loading={loading}
          onLoadMore={() => setGroupMore((current) => ({ ...current, [group.key]: (current[group.key] ?? 0) + 1 }))} />
      </div>
    );
  }

  // Each row is drawn again for its own changes only (TableGridRow): what
  // every row is drawn with, the same while only rows' values change, and the
  // table as last drawn for what a row does. Its own cells write with the
  // table's latest ways, though their row isn't drawn again.
  const stickyKey = properties.map((_, index) => stickyLeft(index) ?? "").join("\n");
  const cuts = wrapCut.cutMarks(properties.length);
  const { open: openLabel, close: closeLabel, openAria } = openLabels;
  const liveEnvironment = useMemo(() => properties.every((property) => !LOCAL_PROPERTY_CELL_TYPES.has(property.type)
    || cellTakesLatestWays(property)), [properties]);
  const rollupForms = Boolean(onConfigureProperty), fills = Boolean(props.onCellsValue), closes = Boolean(props.onCloseRow);
  const frame = useMemo<TableRowFrame>(() => ({
    properties, columns, newColumnAt, newColumnLeft, readOnly, addable, frozen, stickyLefts: stickyKey.split("\n").map((left) => left || undefined),
    gutterStyle: { ...stickyPad, position: "sticky", insetInlineStart: gutterLeft, zIndex: frozen > 0 ? 2 : undefined },
    titleIcon, workspaceId, preview, wrapsAny, cuts, wrappedPropertyIds, fullUrlPropertyIds,
    statusCheckboxIds, subitems: props.subitems, staticReadOnlySemantics, openRowIn: props.openRowIn,
    openLabels: { open: openLabel, close: closeLabel, openAria }, rollupForms, fills, closes, cellTools, t, liveEnvironment,
  }), [addable, cellTools, closeLabel, closes, columns, cuts, fills, frozen, gutterLeft, liveEnvironment, newColumnAt, newColumnLeft, openAria, openLabel, preview, properties,
    fullUrlPropertyIds, props.openRowIn, statusCheckboxIds, props.subitems,
    wrappedPropertyIds, readOnly, rollupForms, staticReadOnlySemantics, stickyKey, t, titleIcon, workspaceId, wrapsAny]);
  // Where each row's line drawn now stands, filled as they are drawn (tableRow): a row under two groups stands on two.
  const places = new Map<string, RowPlace>();
  const live = useRef<TableRowLive>(null!);
  live.current = { cellRange, reorder: rowReorder, environment: cellEnvironment, valueContext: propertyValueContext, cellText,
    onAddRow, onCellValue, onConfigureProperty, onCloseRow: props.onCloseRow, onLoadSubitems, onOpenRow, onOpenRowContextMenu,
    onOpenRowContextMenuFromElement, onSelectAll: props.onSelectAll, onSelectRow, onToggleLeaf: props.onToggleLeaf, onToggleRow,
    chooseRows, moveFocus, part: partOf, expandText: wrapCut.expand, placeOf: (line) => places.get(line)?.shown ?? -1 };
  const drawnPlaces = useRef(places);
  drawnPlaces.current = places;
  // The heads as drawn now, kept for the next drawing (headRow).
  const shownHeads = useRef(headsShown);
  shownHeads.current = headsShown;
  useLayoutEffect(() => { heads.current = shownHeads.current; });
  useLayoutEffect(() => { if (gridRef.current) writeRowPlaces(gridRef.current, drawnPlaces.current); });
  // A row new to the drawn table paints its cells in its first frame (lib/arrivingWhole.ts).
  useLayoutEffect(() => drawArrivalsWhole(listRef.current, listRef.current?.querySelectorAll<HTMLElement>(":scope > [data-table-row-id]") ?? []));
  const environmentRef = useRef(cellEnvironment);
  environmentRef.current = cellEnvironment;
  const [latestWays] = useState(() => liveCellWays(environmentRef as { readonly current: PropertyCellEnvironment }));
  const rowEnvironment = useMemo(() => cellEnvironment && liveEnvironment ? { ...cellEnvironment, ...latestWays } : cellEnvironment,
    [latestWays, liveEnvironment, cellEnvironment]);

  // `outOfView`: a row the view has no more, drawn on out of sight while its value's editor is open (useHeldCellEditor).
  function tableRow(item: TableTreeRow<TableProjectedRow>, displayIndex: number, index: number, key: string, outOfView = false) {
    const row = item.row.row, line = gridEntryLine({ key, item });
    if (!outOfView) {
      places.set(line, { shown: displayIndex, slot: index });
      heldEditor.drawn(line, { key, item, displayIndex, index });
    }
    // A row moves by its handle only, as Notion's (seen 2026-11-03), so a press on its cells can choose a range of them.
    const reorder = rowReorder.row(line, (source) => drag.takes(source, row.id), readOnly ? false : undefined);
    // A row's colors fill its cells, from the title's left edge to the last
    // property's right one, not its handle (Notion's, seen 2026-10-24).
    const tint = props.rowColor?.(tableRowPage(row, properties, workspaceId)) ?? null;
    return <TableGridRow key={key} frame={frame} live={live} item={item} displayIndex={displayIndex} index={index}
      line={line} rowRef={outOfView ? noRowRef : rowElement(line)} heldOut={outOfView} heldMoved={heldEditor.movedColumn(line)} environment={rowEnvironment} valueContext={propertyValueContext} selected={selectedRows.has(line) && !rowReorder.carried?.has(line)} chosenRow={drag.chosenRows.has(row.id) && !rowReorder.carried?.has(line)}
      dragging={reorder["data-dragging"]} dropSide={reorder["data-drop-side"]} nativeDragSource={reorder["data-native-drag-source"]}
      focused={line === focusedLine} textExpanded={wrapCut.expanded(row.id)}
      range={cellRange.box ? properties.map((_, column) => cellRange.inRange(displayIndex, column) ? "1" : "0").join("") : ""}
      subitemsMore={Boolean(subitemWindows[row.id]?.hasMore)}
      open={item.hasChildren ? expandedRows.has(row.id) : Boolean(openLeaves?.has(row.id))} faded={Boolean(props.fadedRowIds?.has(row.id))}
      colors={tint ? properties.map((property) => tint(property.id) ?? "").join("\n") : ""}
      above={drag.shown[displayIndex - 1]} below={drag.shown[displayIndex + 1]} meta={props.subitemMeta ?? null} />;
  }

  // The row whose value's editor is open, the view having it no more, is drawn last among the rows, keyed as before.
  heldEditor.begin();
  const withHeldRow = (drawn: ReactNode[]) => {
    const out = heldEditor.outOfView();
    return out ? [...drawn, tableRow(out.item, out.displayIndex, out.index, out.key, true)] : drawn;
  };

  return (<>
    <div ref={scrollRef} className={styles.tableScroll} data-table-scroll data-row-gutter={readOnly ? undefined : "true"}
      onScroll={(event) => {
        // Scrolled sideways, a table keeping to its block is cut where Notion's box is (DatabaseTableChrome).
        const box = event.currentTarget, scrolled = box.scrollLeft !== 0 ? "true" : "false";
        if (box.dataset.scrolled !== scrolled) box.dataset.scrolled = scrolled; noteColumnsUnderBoxes(box);
      }}>
      <div ref={gridRef} className={styles.table} role="grid" aria-label={t("databaseView:viewTypes.table")} aria-rowcount={visibleItems.length + 1} aria-colcount={properties.length + (readOnly ? 0 : 1)} aria-busy={loading || undefined}
        data-vertical-lines={props.verticalLines === false ? "hidden" : undefined} data-frozen-columns={frozen || undefined}
        data-cell-range={cellRange.box ? "true" : undefined}
        onPointerOver={(event) => { if (scrollRef.current?.dataset.keysPressed !== "true") cellTools.pointerOver(event.target); }}
        onPointerLeave={cellTools.pointerLeave}
        onFocus={(event) => { setFocusedLine(lineOf(event.target)); cellTools.focusIn(event.target); }}
        onBlur={(event) => {
          if (event.currentTarget.contains(event.relatedTarget as Node | null)) return;
          cellTools.focusIn(null);
          setFocusedLine(null);
          cellRange.clear();
        }}
        // The rows as shown — sub-items, a row just made — not as the view sorts them alone, a group's alone (pasteOnTableCell).
        onPaste={(event) => pasteOnTableCell(event, { range: cellRange, part: partOf, lineOf: (line) => drag.shown[line],
          rows: (from, to) => visibleItems.slice(from, to).map((item) => item.row.row),
          write: readOnly || !props.onPasteCells ? undefined : (rows, column, paste) => props.onPasteCells!(rows, properties.slice(column), paste) })}>
        <div className={styles.tableGrid}>
          {!grouped && headRow(shownRowIds)}
          <div ref={listRef} data-table-rows
            onPointerMove={grouped ? (event) => {
              const at = event.target instanceof Element ? event.target.closest<HTMLElement>("[data-index]") : null;
              let index = at ? Number(at.dataset.index) : -1, key: string | null = null;
              for (; index >= 0; index -= 1) {
                const entry = entries[index];
                if (entry?.kind === "group") { key = entry.group.key; break; }
              }
              if (key !== pointedGroup) setPointedGroup(key);
            } : undefined}
            onPointerLeave={grouped ? () => setPointedGroup(null) : undefined}>
            {!readOnly && props.onFreezeColumns && properties.length > 0 && (
              <TableFreezeEdge frozen={frozen} frozenWidth={frozenWidth} stickyLeft={frozen > 0 ? gutterLeft : undefined}
                onFreeze={props.onFreezeColumns} />
            )}
            {withHeldRow(windowedRows.entries.map((slot) => {
              if (slot.kind === "gap") return <div key={slot.key} aria-hidden="true" style={windowGapStyle(slot.size)} />;
              const entry = entries[slot.index];
              if (entry.kind === "group") return groupHeader(entry.group, slot.index, entry.key);
              if (entry.kind === "head") return headRow(gridGroupLines(entry.group.key, shownRows.get(entry.group.key) ?? []), { key: entry.key, index: slot.index, stack: entry.group.key });
              if (entry.kind === "more") return groupMoreRow(entry.group, slot.index, entry.key);
              // A view showing its archived rows makes no page (seen 2026-10-12).
              if (!offersNewRows && (entry.kind === "subitemAdd" || entry.kind === "add")) return null;
              if (entry.kind === "subitemAdd") {
                return <TableGroupAddRow key={entry.key} ref={measureEntry(entry.key)} index={slot.index}
                  label={t("newSubitem")} kind="subitemAdd" indent={entry.depth * 24 + 17}
                  onAdd={() => onAddRow(entry.parentId, null)} />;
              }
              if (entry.kind === "add" || entry.kind === "newGroup") {
                const group = entry.kind === "add" ? entry.group : null;
                return <TableGroupAddRow key={entry.key} ref={measureEntry(entry.key)} index={slot.index}
                  label={group ? newPageLabel : t("boardView:newGroup")} kind={entry.kind} groupAdd={props.groupAdd}
                  onEditFilters={group && !group.rows.length ? props.onEditEmptyGroupFilters : undefined} filtersOnly={readOnly}
                  onAdd={() => { if (group) onAddGroupRow?.(group.key); }} />;
              }
              return entry.kind === "row" ? tableRow(entry.item, entry.displayIndex ?? slot.index, slot.index, entry.key) : null;
            }))}
            {windowedRows.after > 0 && <div aria-hidden="true" style={windowGapStyle(windowedRows.after)} />}
          </div>
          {/* Notion's empty table shows only its head and "New page" (seen 2026-10-06); a screen reader still hears it is empty. */}
          {groups.every((group) => group.rows.length === 0) && (
            <div className={styles.srOnly} role="row" data-table-empty>
              <div role="gridcell" aria-colspan={properties.length + (readOnly ? 0 : 1)}>{t("emptyTable")}</div>
            </div>
          )}
          {hasMore && <TableLoadMoreRow loading={loading} limit={loadLimit} onLoadMore={onLoadMore} onLimit={onLoadLimit} />}
          {props.footer}
        </div>
      </div>
    </div><OwnPageViewBar scrollRef={scrollRef} />
    {!readOnly && <TableRowHandleLayer scrollRef={scrollRef} listRef={listRef}
      keep={(id) => rowReorder.dragging !== null && drag.rowOf(rowReorder.dragging) === id}
      handles={(id, index) => {
        const row = visibleItems[index]?.row.row;
        return row?.id === id ? rowHandles(row, index, true) : null;
      }} />}
  </>);
}
