import { memo, useContext, type CSSProperties, type KeyboardEvent, type MouseEvent, type ReactNode, type RefObject } from "react";
import type { TFunction } from "i18next";
import type { GraphProperty, GraphRow } from "@/contentGraph";
import type { DragReorder } from "@/lib/useDragReorder";
import { usePageNames } from "@/lib/pageNames";
import { primaryModifier } from "@/lib/keyboardShortcuts";
import { CaretRightFill } from "../icons";
import { PropertyCell, OTHER_ROWS_LOOKUPS } from "./PropertyCell";
import { cellReadsSame } from "./otherRowsRead";
import type { PropertyCellSnapshot } from "./propertyCellTypes";
import { PropertyNoAccessValue } from "./PropertyAccessMarks";
import { conditionalColorBackground } from "./conditionalColorModel";
import { PropValue, type PropertyValueReadContext } from "./PropValue";
import { LOCAL_PROPERTY_CELL_TYPES, type PropertyCellEnvironment } from "./propertyCellTypes";
import { TableStatusCheckbox } from "./TableStatusDisplay";
import { WrappedTextExpand } from "./WrappedTextExpand";
import { fallbackEditor, tableRowPage } from "./tableGridRowCells";
import { RowCommentCount } from "./RowCommentCount";
import { stickyPad, type TablePart } from "./tableInteractionModel";
import { caretAtEndOfPressedText, cellClears, cellFills, cellOpensFromRoom, cellTyping, clearsChosenCell, editChosenCellByKey, enterMovesDown, escapeKeepsChosen, fitCellEditor, keepCellChosen, openCellFromRoom, phoneCellLeavesKey, stepsPageHistory, tabChoosesNextCell, typeIntoChosenCell } from "./tableCellKeys";
import { undoneCellKey } from "./tableCellUndoneChoice";
import type { TableTreeRow } from "./tableViewModel";
import type { TableProjectedRow } from "./tableGridLines";
import type { useTableCellRange } from "./useTableCellRange";
import { PeekHalo } from "./PeekHalo";
import { CellCommentsContext } from "./cellCommentsContext";
import { toolsCellKey, type CellToolsShown } from "./cellToolsShown";
import { TableCellValueTools } from "./TableCellValueTools";
import { TableRowOpenButton } from "./TableRowOpenButton";
import { secondTapStaysATap } from "./tableCellDoubleTap";
import { cellValue } from "./queryProjection";
import styles from "./databaseSheet";
import { rowOpenModeOfClick } from "./rowOpenClick";
import { UrlDisplayContext } from "./urlDisplay";
import { rowValuesLocked } from "./rowValuesLocked";
import { TableRowHandles } from "./TableRowHandles";
import { withNewColumnCell } from "./tableNewColumn";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";
import { rowHoldsCellFocus } from "@/lib/chosenItemFocus";
import { takeBackShiftPressClick } from "./cellShiftPress";

const SHORT_URL_DISPLAY = { full: false };
const FULL_URL_DISPLAY = { full: true };
const TABLE_ROW_HALO: CSSProperties = { insetBlockEnd: "1px" };
const TABLE_ROW_HALO_PAST_GUTTER: CSSProperties = { ...TABLE_ROW_HALO, insetInlineStart: "var(--table-gutter-width)" };

/** What every row of a table is drawn with, the same while only rows' values change. */
export type TableRowFrame = {
  properties: readonly GraphProperty[];
  /** The rows' grid columns, an insert's empty one among them. */
  columns: string;
  /** Where an insert's empty column stands among the cells (tableNewColumn.ts), and where it sticks among frozen ones. */
  newColumnAt?: number;
  newColumnLeft?: string;
  readOnly: boolean;
  addable: boolean;
  frozen: number;
  /** Where each frozen column sticks; none for the others. */
  stickyLefts: readonly (string | undefined)[];
  gutterStyle: CSSProperties;
  titleIcon: "none" | "page";
  workspaceId: string;
  preview: boolean;
  wrapsAny: boolean;
  /** Whether each column's wrapped text is cut ("1"), shown whole once cut ("2"), or shown whole ("0"). */
  cuts: string;
  wrappedPropertyIds?: ReadonlySet<string>;
  fullUrlPropertyIds?: ReadonlySet<string>;
  statusCheckboxIds?: ReadonlySet<string>;
  /** Sub-items on: every row keeps a toggle's room on its title, or in its first column ("first"). */
  subitems?: boolean | "first";
  staticReadOnlySemantics: boolean;
  openRowIn?: "side" | "center" | "full";
  openLabels: { open: string; close: string; openAria?: string };
  rollupForms: boolean;
  fills: boolean;
  closes: boolean;
  cellTools: CellToolsShown;
  t: TFunction<["tableView", "databaseView", "boardView", "pageTreeItem", "propValue"]>;
  /** The cells' ways as the table's latest, which their rows' own cells write with (TableGrid). */
  liveEnvironment: boolean;
};

/** The table as last drawn, which a row's handlers read as they run though the row isn't drawn again. */
export type TableRowLive = {
  cellRange: ReturnType<typeof useTableCellRange>;
  reorder: DragReorder;
  environment?: PropertyCellEnvironment;
  valueContext?: PropertyValueReadContext;
  cellText?: (page: ReturnType<typeof tableRowPage>, property: GraphProperty) => string;
  /** Makes a row under the line (`afterLine`) or over it (`beforeLine`), in its group (viewGroupLines.ts). */
  onAddRow(parentId: string | null, afterLine: string | null, beforeLine?: string): void;
  onCellValue(row: GraphRow, property: GraphProperty, value: unknown): void;
  onConfigureProperty?(property: GraphProperty, cell: HTMLElement): void;
  onCloseRow?(): void;
  onLoadSubitems(rowId: string): void;
  onOpenRow(rowId: string, mode?: "default" | "new_tab" | "side"): void;
  onOpenRowContextMenu(rowId: string, event: MouseEvent<HTMLElement>): void;
  onOpenRowContextMenuFromElement(rowId: string, element: HTMLElement): void;
  /** Chooses these lines alone (a row's own id where the table isn't grouped; viewGroupLines.ts). */
  onSelectAll?(rowIds: readonly string[]): void;
  onSelectRow(line: string, selected: boolean, range: boolean): void;
  onToggleLeaf?(rowId: string): void;
  onToggleRow(rowId: string): void;
  chooseRows(rowIds: readonly string[]): void;
  moveFocus(event: KeyboardEvent<HTMLElement>, row: number, column: number): void;
  /** The lines of the part of the table a line stands in: a grouped table's group (tablePart). */
  part(line: number): TablePart;
  expandText(rowId: string): void;
  /** Where a row's line stands among the table's shown rows now, which its handlers read (TableGrid). */
  placeOf(line: string): number;
};

type TableGridRowProps = {
  frame: TableRowFrame;
  live: RefObject<TableRowLive>;
  item: TableTreeRow<TableProjectedRow>;
  displayIndex: number;
  index: number;
  rowRef: (element: HTMLElement | null) => void;
  /** Its cells' ways: the table's latest (frame.liveEnvironment) or as drawn. */
  environment?: PropertyCellEnvironment;
  valueContext?: PropertyValueReadContext;
  /** Its line chosen: ticked and tinted. */
  selected: boolean;
  /** Its row chosen on another of its lines: tinted and read as selected, not ticked, as Notion's grouped table (seen 2026-11-24; viewGroupLines.ts). */
  chosenRow?: boolean;
  dragging?: string;
  dropSide?: string;
  nativeDragSource?: string;
  focused: boolean;
  /** Whether each column's cell is in the chosen range ("1"). */
  range: string;
  textExpanded: boolean;
  subitemsMore: boolean;
  open: boolean;
  faded: boolean;
  /** Each cell's conditional color, line by line. */
  colors: string;
  /** The line its handle drags and those before and after it: under its group, a row under several groups having one in each (tableGridDrag). */
  line?: string;
  above?: string;
  below?: string;
  meta: ((row: GraphRow) => ReactNode) | null;
  /** A row the view has no more, drawn on out of sight while its value's editor is open (useHeldCellEditor). */
  heldOut?: boolean;
  /** The column whose value's editor stands where it opened, the row moved away since: its cell shows the value (useHeldCellEditor). */
  heldMoved?: number;
};

// A row is drawn again for its own changes and the table's frame's; its
// cells' ways are the table's latest whatever drew it (2026-11-12: each
// character typed in a cell drew every row of the table again, ~28 ms on a
// table of 30 rows and 40 columns). A row only moved up or down by a row
// coming or going above it is not drawn again: the table writes its place
// on it (TableGrid, rowPlaces) and its handlers read where it stands now
// (2026-11-13: a row taken out drew every row under it again, 30 ms). Nor
// is one whose cells read the same of the other rows, the snapshot made anew
// as any row comes or goes (cellsReadSame).
function sameRow(previous: TableGridRowProps, next: TableGridRowProps) {
  for (const key of Object.keys(next) as (keyof TableGridRowProps)[]) {
    if (previous[key] === next[key] || key === "environment" || key === "valueContext" || key === "item"
      || key === "displayIndex" || key === "index") continue;
    return false;
  }
  // Its line is made again as the table's are; its row is the same while unchanged.
  const before = previous.item, after = next.item;
  if (before.row.row !== after.row.row || before.depth !== after.depth || before.hasChildren !== after.hasChildren) return false;
  if (previous.environment !== next.environment) {
    if (!next.frame.liveEnvironment || !previous.environment || !next.environment) return false;
    if (!cellsReadSame(next.frame, next.item.row.row, previous.environment.snapshot, next.environment.snapshot)) return false;
  }
  // Values shown as read only read the table's other rows (PropValue).
  return previous.valueContext === next.valueContext || !readsValueContext(next.frame, next.item.row.row, Boolean(next.environment));
}

// Whether a row's cells read the same of the table's snapshot in both: what
// isn't other rows' alike, and each cell what it reads of other rows and of
// its database's properties (otherRowsRead.ts) — a relation's its related
// pages, a formula's of its own row alone its database's properties.
function cellsReadSame(frame: TableRowFrame, row: GraphRow, before: PropertyCellSnapshot, after: PropertyCellSnapshot) {
  const was = before as unknown as Record<string, unknown>, now = after as unknown as Record<string, unknown>;
  const keys = Object.keys(now);
  if (keys.some((key) => !OTHER_ROWS_LOOKUPS.has(key) && was[key] !== now[key])) return false;
  if (keys.every((key) => was[key] === now[key])) return true;
  const page = tableRowPage(row, frame.properties, frame.workspaceId);
  return frame.properties.every((property, position) => cellReadsSame({ id: property.id, databaseId: property.dataSourceId, name: property.name,
    type: property.type, config: property.config, position } as Parameters<typeof PropertyCell>[0]["prop"], page, page, before, after));
}

function readsValueContext(frame: TableRowFrame, row: GraphRow, cellsDrawn: boolean) {
  return (frame.readOnly && !cellsDrawn) || rowValuesLocked(row) || Boolean(row.readOnlyValues?.length);
}

export const TableGridRow = memo(function TableGridRow(props: TableGridRowProps) {
  const pageNames = usePageNames();
  const { frame, live, item, displayIndex, index, selected } = props;
  const { properties, readOnly, t } = frame;
  const row = item.row.row, line = props.line ?? row.id;
  const page = tableRowPage(row, properties, frame.workspaceId);
  const colors = props.colors.split("\n");
  // The row's own controls read its title wherever that column stands, or
  // "새 페이지" as an untitled page is called, never a bare " 선택".
  const label = pageNames.title(page);
  const cellComments = useContext(CellCommentsContext);
  const valueContext = props.valueContext;
  const stickyStyle = (index: number, raised = false): CSSProperties | undefined => {
    const left = frame.stickyLefts[index];
    return left === undefined ? undefined : { ...stickyPad, position: "sticky", insetInlineStart: left, zIndex: raised ? 3 : 2, background: "var(--table-surface, var(--bg-app))" };
  };
  // A row's sub-item toggle and its indent stand on the title, or in the table's first column where
  // "제목에 중첩 토글 표시" is off ("first"), as Notion's (seen 2026-11-22 with the title second).
  const nestsAt = frame.subitems === "first" ? 0 : properties.findIndex((property) => property.type === "title");
  const writable = (property: GraphProperty) => !readOnly && cellFills(property.type)
    && !rowValuesLocked(row) && !row.readOnlyValues?.includes(property.id) && !row.hiddenValues?.includes(property.id);
  return (
    <div
      ref={props.rowRef}
      data-native-drag-source={props.nativeDragSource}
      data-index={index}
      aria-rowindex={displayIndex + 2}
      data-row-dragging={props.dragging}
      data-row-drag-over={props.dropSide ? "true" : undefined}
      data-drop-side={props.dropSide}
      data-subitem-context={props.faded ? "true" : undefined}
      className={styles.tableRow}
      role="row"
      // Chosen on each line its row shows on, as it is drawn (tinted) and as
      // the card views' halo, its box ticked on the line chosen alone.
      aria-selected={selected || Boolean(props.chosenRow)}
      data-table-row-id={row.id}
      data-table-line={line}
      data-row-selected={selected ? "true" : props.chosenRow ? "row" : undefined}
      data-text-expanded={props.textExpanded ? "true" : undefined}
      data-wraps={frame.wrapsAny ? "true" : undefined}
      data-row-held-out={props.heldOut ? "true" : undefined}
      style={{ gridTemplateColumns: `${readOnly ? "" : "var(--table-gutter-width) "}${frame.columns}${frame.addable ? " auto" : ""}` }}
      onContextMenu={(event) => live.current.onOpenRowContextMenu(row.id, event)}
      inert={frame.preview || undefined}
    >
      {/* Its ring stands over the row's cells and stops above its bottom line, as Notion's (seen 2026-11-13: 36px of a 37px row). */}
      <PeekHalo itemId={row.id} style={readOnly ? TABLE_ROW_HALO : TABLE_ROW_HALO_PAST_GUTTER} />
      {!readOnly && (
        <div className={styles.rowGutterCell} data-table-row-gutter-cell role="gridcell" aria-label={t("rowActionsFor", { title: label })}
          style={frame.gutterStyle}>
          <TableRowHandles rowId={line} reorder={live.current.reorder}
            neighbours={{ above: props.above, below: props.below }}
            onAdd={(above) => above ? live.current.onAddRow(row.subitemParentId, null, line) : live.current.onAddRow(row.subitemParentId, line)}
            onMenu={(handle) => { live.current.onSelectAll?.([line]); live.current.onOpenRowContextMenuFromElement(row.id, handle); }} />
          <input className={styles.rowSelect} data-table-row-select type="checkbox" aria-label={t("selectRow", { title: label })} checked={selected}
            onChange={(event) => live.current.onSelectRow(line, event.target.checked, event.nativeEvent instanceof globalThis.MouseEvent && event.nativeEvent.shiftKey)} />
        </div>
      )}
      {withNewColumnCell(properties.map((property, columnIndex) => {
        const isTitle = property.type === "title";
        const meta = isTitle ? props.meta?.(row) : null;
        const valueLocked = rowValuesLocked(row) || Boolean(row.readOnlyValues?.includes(property.id));
        const editable = !readOnly && !valueLocked && !row.hiddenValues?.includes(property.id);
        // Drawn by its property's cell, which may only show its value (readOnly).
        const cellDrawn = !valueLocked && Boolean(props.environment) && LOCAL_PROPERTY_CELL_TYPES.has(property.type);
        const dbProperty = {
          id: property.id, databaseId: property.dataSourceId, name: property.name,
          type: property.type, config: property.config, position: columnIndex,
        } as Parameters<typeof PropertyCell>[0]["prop"];
        const cellColor = colors[columnIndex] || null;
        const wrapped = Boolean(frame.wrappedPropertyIds?.has(property.id));
        const cellTint = cellColor ? conditionalColorBackground(cellColor as Parameters<typeof conditionalColorBackground>[0]) : undefined;
        const sticky = stickyStyle(columnIndex, props.focused);
        const cut = wrapped && frame.cuts[columnIndex] === "1";
        const cutOnce = wrapped && (cut || frame.cuts[columnIndex] === "2");
        // A rollup's cell opens its small form, set up or not, as Notion's (seen 2026-11-06); a link in it still opens.
        const rollupForm = !readOnly && property.type === "rollup" && frame.rollupForms;
        const configure = (cell: HTMLElement) => live.current.onConfigureProperty?.(property, cell);
        return (
          <div
            key={property.id}
            ref={secondTapStaysATap}
            className={styles.cell}
            onClick={(event) => {
              if (rollupForm) { if (!(event.target as HTMLElement).closest("a, button")) configure(event.currentTarget); return; }
              if (event.button === 0 && !event.shiftKey && !event.metaKey && !event.ctrlKey && !event.altKey) openCellFromRoom(event.currentTarget, event.target);
            }}
            role="gridcell"
            tabIndex={0}
            style={cellTint ? { ...sticky, background: cellTint } : sticky}
            data-conditional-color={cellColor ?? undefined}
            data-frozen={columnIndex < frame.frozen ? "true" : undefined}
            data-frozen-edge={columnIndex === frame.frozen - 1 ? "true" : undefined}
            aria-colindex={columnIndex + 1 + (readOnly ? 0 : 1)}
            aria-keyshortcuts={isTitle ? "Meta+Enter Control+Enter" : undefined}
            data-table-cell
            data-title={isTitle ? "true" : undefined}
            data-row-commented={isTitle && cellComments && cellComments.rowDiscussions(row.id) > 0 ? "true" : undefined}
            data-subitem-meta={meta ? "true" : undefined}
            data-wrap={wrapped ? "true" : undefined}
            data-wrap-cut={cut ? "true" : undefined}
            data-col-index={columnIndex}
            data-cell-typing={!editable || (property.type === "status" && frame.statusCheckboxIds?.has(property.id)) ? undefined : cellTyping(property.type)}
            data-cell-clears={editable && cellClears(property.type) ? "true" : undefined}
            data-cell-opens={editable && cellOpensFromRoom(property.type) ? "true" : undefined}
            data-cell-range={props.range[columnIndex] === "1" ? "true" : undefined}
            data-cell-inert={readOnly && !cellComments?.canComment ? "true" : undefined}
            data-cell-discussing={cellComments?.active?.rowId === row.id && cellComments.active.propertyId === property.id ? "true" : undefined}
            data-property-discussed={cellComments?.discussed(row.id, property.id) ? "true" : undefined}
            onPointerDown={(event) => {
              // Shift and a press choose the cell's row, as Notion's (seen 2026-11-02), unless its value is being written;
              // the value pressed opens nothing (cellShiftPress.ts).
              if (event.shiftKey && event.button === 0 && !readOnly && !event.currentTarget.contains(document.activeElement)) {
                event.preventDefault();
                live.current.onSelectRow(line, true, true);
                takeBackShiftPressClick(event.currentTarget);
                return;
              }
              caretAtEndOfPressedText(event);
              live.current.cellRange.pointerDown(event, live.current.placeOf(line), columnIndex);
            }}
            onFocus={(event) => {
              if (event.target !== event.currentTarget) fitCellEditor(event.currentTarget);
              else live.current.cellRange.chosen(live.current.placeOf(line), columnIndex);
            }}
            onKeyDownCapture={(event) => {
              // The page's history first: ⌘Z in its text, then the first key after it took back the letter that opened it.
              if (stepsPageHistory(event.currentTarget, event.nativeEvent) || undoneCellKey(event.currentTarget, event.nativeEvent)) {
                event.preventDefault();
                event.stopPropagation();
                return;
              }
              // Its value's editor left with Escape or Enter keeps the cell chosen; a title's or
              // text's left with Enter chooses the one under it in its group (Notion's, seen 2026-11-02
              // and 2026-11-26; tablePart), with Escape none while words are in it (escapeKeepsChosen).
              if ((event.key === "Escape" || fieldEnter(event)) && event.target !== event.currentTarget
                && !imeComposing(event) && !(event.key === "Escape" && !escapeKeepsChosen(property.type, event.target))) {
                keepCellChosen(event.currentTarget, fieldEnter(event) && !event.shiftKey && enterMovesDown(property.type)
                  && live.current.part(live.current.placeOf(line)));
              }
            }}
            onKeyDown={(event) => {
              const table = live.current;
              // A phone's cell is a button Enter opens, not a chosen cell: no other plain key does anything on it.
              if (event.target === event.currentTarget && phoneCellLeavesKey(event.currentTarget, event)) return;
              // A title cell holding the focus for its row (chosenItemFocus.ts) takes Enter alone, writing that row's
              // title with the rows let go; its other keys are the rows' (useTableRowSelectionKeys), a letter nowhere.
              if (event.target === event.currentTarget && rowHoldsCellFocus(event.currentTarget) && event.key !== "ContextMenu"
                && !(event.shiftKey && event.key === "F10") && !(isTitle && primaryModifier(event) && fieldEnter(event))) {
                if (fieldEnter(event) && !event.shiftKey && !event.altKey && !event.metaKey && !event.ctrlKey
                  && editable && editChosenCellByKey(event.currentTarget, event.key)) { event.preventDefault(); table.chooseRows([]); }
                return;
              }
              if (event.target === event.currentTarget && table.cellRange.keyDown(event, table.placeOf(line), columnIndex)) return;
              if (event.key === "Escape" && event.target === event.currentTarget && !readOnly && !selected
                && !event.shiftKey && !event.altKey && !event.metaKey && !event.ctrlKey) {
                event.preventDefault();
                event.stopPropagation();
                table.chooseRows([line]);
                return;
              }
              if (fieldEnter(event) && event.target === event.currentTarget
                && !event.shiftKey && !event.altKey && !event.metaKey && !event.ctrlKey) {
                // A cell that only shows its value draws its editing parts asleep (cellReadOnly.ts).
                if (editable && editChosenCellByKey(event.currentTarget, event.key)) event.preventDefault();
                else if (rollupForm) { event.preventDefault(); configure(event.currentTarget); }
                return;
              }
              // Backspace and Delete clear a chosen cell's value, the cell staying chosen; ⌘Z, the
              // page's, brings it back (Notion's, seen 2026-10-21).
              if (event.target === event.currentTarget && clearsChosenCell(event.currentTarget, event)) {
                event.preventDefault();
                event.stopPropagation();
                table.onCellValue(row, property, null);
                return;
              }
              if (event.target === event.currentTarget && typeIntoChosenCell(event.currentTarget, event)) return;
              if (isTitle && primaryModifier(event) && fieldEnter(event)) {
                event.preventDefault();
                table.onOpenRow(row.id, "new_tab");
                return;
              }
              if (event.key === "ContextMenu" || (event.shiftKey && event.key === "F10")) {
                event.preventDefault();
                table.onOpenRowContextMenuFromElement(row.id, event.currentTarget);
                return;
              }
              // Tab on the chosen cell moves the choice on, Shift+Tab keeps it; in a control or a field in it, its own.
              if (event.key === "Tab") { if (tabChoosesNextCell(event)) table.moveFocus(event, table.placeOf(line), columnIndex); return; }
              table.moveFocus(event, table.placeOf(line), columnIndex);
            }}
          >
            {columnIndex === nestsAt && item.depth > 0 && (
              <span className={styles.subitemIndent} style={{ width: item.depth * 24 }} aria-hidden="true" />
            )}
            {/* With sub-items on, every row keeps a toggle's room there, as Notion's (seen 2026-10-10): a row
                with sub-items shows its toggle, one without shows it under the pointer, to open and add one. */}
            {columnIndex === nestsAt && (frame.subitems || item.hasChildren) && (item.hasChildren || !readOnly ? (
              <button type="button" className={styles.subitemToggle} data-visible={item.hasChildren || props.open ? "true" : undefined}
                aria-expanded={props.open} aria-label={t(props.open ? "collapseSubitems" : "expandSubitems", { title: label })}
                onClick={() => item.hasChildren ? live.current.onToggleRow(row.id) : live.current.onToggleLeaf?.(row.id)}>
                <CaretRightFill aria-hidden="true" />
              </button>
            ) : <span className={styles.subitemToggle} aria-hidden="true" />)}
            {columnIndex === nestsAt && props.subitemsMore && (
              <button type="button" className={styles.subitemAdd} data-subitem-load-more aria-label={t("loadMoreSubitems")} onClick={() => live.current.onLoadSubitems(row.id)}>
                …
              </button>
            )}
            <UrlDisplayContext.Provider value={property.type === "url" && frame.fullUrlPropertyIds?.has(property.id) ? FULL_URL_DISPLAY : SHORT_URL_DISPLAY}>
            {/* Its value shown in the cell under the editor that stayed where it opened, as a locked row's is (Notion's, seen 2026-11-14). */}
            {props.heldMoved === columnIndex && valueContext ? <div className={styles.cellEditor} data-held-cell-value aria-hidden="true">
              <span data-table-typed-value><PropValue context={valueContext} row={page} prop={dbProperty} interactive={false} titleIcon={frame.titleIcon} /></span>
            </div> : null}
            <div className={styles.cellEditor} data-cell-editor data-row-locked={rowValuesLocked(row) || undefined}
              data-cell-read-only={readOnly && cellDrawn ? "true" : undefined}>
              {/* A row whose database page is locked shows its values without editing them; a value the
                  reader may not see shows "사용 권한 없음", one they may not change shows as read-only.
                  A table that may not be edited (a locked page's) draws the cells it draws when it may,
                  asleep, so locking and unlocking flips only that (cellReadOnly.ts). */}
              {row.hiddenValues?.includes(property.id) ? <PropertyNoAccessValue /> : property.type === "status" && frame.statusCheckboxIds?.has(property.id) ? (
                <TableStatusCheckbox property={property} value={cellValue(page, dbProperty)} readOnly={readOnly || valueLocked}
                  label={t("propertyCell:edit", { name: property.name })} onChange={(next) => live.current.onCellValue(row, property, next)} />
              ) : (valueLocked || (readOnly && !cellDrawn)) && valueContext ? (
                <span data-table-typed-value>
                  <PropValue context={valueContext} row={page} prop={dbProperty}
                    interactive={!readOnly} titleIcon={frame.titleIcon} />
                </span>
              ) : cellDrawn ? (
                <PropertyCell {...props.environment!} row={page} prop={dbProperty} titleIcon={frame.titleIcon} readOnly={readOnly}
                  onOpenPage={(id) => live.current.onOpenRow(id)} />
              ) : fallbackEditor({ property, readOnly: readOnly || valueLocked, row, staticReadOnlySemantics: frame.staticReadOnlySemantics,
                onCellValue: (...args) => live.current.onCellValue(...args) })}
            </div>
            </UrlDisplayContext.Provider>
            {frame.fills && writable(property) && (
              <span className={styles.cellFill} data-cell-fill aria-hidden="true"
                onPointerDown={(event) => live.current.cellRange.fillPointerDown(event, live.current.placeOf(line), columnIndex)} />
            )}
            <TableCellValueTools shown={frame.cellTools} cell={toolsCellKey(line, columnIndex)} page={page} property={dbProperty}
              hidden={row.hiddenValues?.includes(property.id)} configure={rollupForm ? configure : undefined}
              read={() => ({ valueContext: live.current.valueContext, text: () => live.current.cellText?.(page, property) ?? "" })} />
            {meta}
            {cutOnce && !props.textExpanded && (
              <WrappedTextExpand cut={cut} onExpand={() => live.current.expandText(row.id)} />
            )}
            {isTitle ? <RowCommentCount rowId={row.id} /> : null}
            {isTitle && <TableRowOpenButton rowId={row.id} openIn={frame.openRowIn} labels={frame.openLabels} onClose={frame.closes ? () => live.current.onCloseRow?.() : undefined}
              onOpen={(event) => live.current.onOpenRow(row.id, rowOpenModeOfClick(event))} />}
          </div>
        );
      }), frame.newColumnAt, () => <div key="new-column" className={styles.cell} aria-hidden="true" data-table-new-column
        style={frame.newColumnLeft ? { ...stickyPad, position: "sticky", insetInlineStart: frame.newColumnLeft, zIndex: 2, background: "var(--table-surface, var(--bg-app))" } : undefined} />)}
      {/* Under the head's "+", the row's line runs on to the table's end, as Notion's (seen 2026-11-02). */}
      {frame.addable && <div className={styles.trailingGridCell} aria-hidden="true" data-table-row-trailing />}
    </div>
  );
}, sameRow);
