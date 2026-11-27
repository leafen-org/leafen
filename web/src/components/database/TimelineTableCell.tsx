import { useContext, type CSSProperties, type HTMLAttributes, type KeyboardEvent, type MouseEvent as ReactMouseEvent, type ReactNode } from "react";
import type { DbProperty, Page } from "@/lib/types";
import { primaryModifier } from "@/lib/keyboardShortcuts";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";
import {
  caretAtEndOfPressedText, cellClears, cellOpensFromRoom, cellTyping, chooseCellByKey, clearsChosenCell, editChosenCellByKey, enterMovesDown,
  escapeKeepsChosen, fitCellEditor, keepCellChosen, openCellFromRoom, stepsPageHistory, tabChoosesNextCell, typeIntoChosenCell,
} from "./tableCellKeys";
import { chosenCellAfterKey } from "./tableInteractionModel";
import { undoneCellKey } from "./tableCellUndoneChoice";
import { cellLine, cellOnLine } from "./tableCellLine";
import { PropertyCell } from "./PropertyCell";
import { PropValue, type PropertyValueReadContext } from "./PropValue";
import type { PropertyCellEnvironment, PropertyCellSnapshot } from "./propertyCellTypes";
import { RowCommentCount } from "./RowCommentCount";
import { CellCommentsContext } from "./cellCommentsContext";
import { TableRowOpenButton } from "./TableRowOpenButton";
import { rowOpenModeOfClick } from "./rowOpenClick";
import type { TimelineRowOpenMode } from "./TimelineRows";
import styles from "./databaseSheet";
import { secondTapStaysATap } from "./tableCellDoubleTap";

/** What a timeline table's cell does with what it is asked, by the table as last drawn. */
export type TimelineTableCellWays = {
  /** Opens a row: its own, "new_tab" for ⌘Enter on its name. */
  open(rowId: string, mode?: TimelineRowOpenMode): void;
  /** Its row's menu, by the menu key on a chosen cell. */
  menu(rowId: string, element: HTMLElement): void;
  /** Clears its value, by Backspace or Delete on a chosen cell. */
  clear(row: Page, property: DbProperty): void;
  /** Chooses its row alone, by Escape on a chosen cell. */
  chooseRow(rowId: string): void;
  /** The lines of the part of the table a line is in, a group's in a grouped timeline (tablePart). */
  part(line: number): { first: number; end: number };
  /** Its table's range of cells, a table's own (useTimelineTableRange): a key on a chosen cell goes to it first, true when it took it. */
  rangeKey?(event: KeyboardEvent<HTMLElement>, row: number, column: number): boolean;
  /** A cell chosen, which lets a range from another cell go. */
  rangeChosen?(row: number, column: number): void;
};

/**
 * A cell of a timeline's table, written in as a table's cell is (Notion's
 * timeline table, seen 2026-11-24 in Chrome at 1685 in two tabs: its name and
 * values are the table's cells): a press on its value or its room writes in it
 * — a name's too, ⌘ held or pressed twice, where the row opens from its
 * "열기" — in a box over the cell, Enter leaving a name's or a text's box
 * choosing the cell under it and Escape choosing none; a chosen cell takes
 * Enter to write again, typing, Backspace and Delete, and Tab and the arrow
 * keys as a table's chosen cell does (chosenCellAfterKey: Notion's timeline
 * table, seen 2026-11-26 in Chrome, went as its table did). The keys are the
 * table's own (tableCellKeys.ts), its
 * box the table cell's (DatabaseTableRows.module.css). The row stands for its
 * line of the table (`role="row"`, its `aria-rowindex`). Its value's tools
 * under the pointer and the marks of its discussions are a table cell's
 * (TableCellValueTools).
 */
export function TimelineTableCell({ row, property, column, title, editable, readOnly, wrap, width, ways, className, before, children, after, ...rest }: {
  row: Page;
  property: DbProperty;
  /** Its column among the table's shown ones. */
  column: number;
  title?: boolean;
  /** Its value may be written here. */
  editable: boolean;
  /** Its table only shows its values: a press chooses nothing (cellReadOnly.ts). */
  readOnly: boolean;
  wrap?: boolean;
  width?: number;
  ways: TimelineTableCellWays;
  className?: string;
  /** What stands before its value: a name's sub-item toggle and its row's halos. */
  before?: ReactNode;
  /** Its editor, or its value shown. */
  children: ReactNode;
  /** What stands after its value: a name's discussions and "열기". */
  after?: ReactNode;
} & Pick<HTMLAttributes<HTMLDivElement>, "onContextMenu"> & { "data-row-id"?: string; "data-row-commented"?: string }) {
  const style: CSSProperties | undefined = width === undefined ? undefined : { width };
  // Its value marked while its discussion is open or being written, as a table cell's (DatabaseTableRows.module.css).
  const comments = useContext(CellCommentsContext);
  return (
    <div {...rest} ref={secondTapStaysATap} className={`${styles.timelineTableCell} ${className ?? ""}`} style={style} role="gridcell" tabIndex={0}
      data-table-cell data-timeline-table-cell={title ? undefined : property.id} data-col-index={column}
      data-title={title ? "true" : undefined} data-wrap={wrap ? "true" : undefined}
      data-cell-typing={editable ? cellTyping(property.type) : undefined}
      data-cell-clears={editable && cellClears(property.type) ? "true" : undefined}
      data-cell-opens={editable && cellOpensFromRoom(property.type) ? "true" : undefined}
      data-cell-inert={readOnly ? "true" : undefined}
      data-cell-discussing={comments?.active?.rowId === row.id && comments.active.propertyId === property.id ? "true" : undefined}
      data-property-discussed={comments?.discussed(row.id, property.id) ? "true" : undefined}
      onPointerDown={(event) => { if (!event.shiftKey) caretAtEndOfPressedText(event); }}
      // A press on its room writes as a press on its value does; ⌘ held too on a name, which opens nothing here.
      onClick={(event) => { if (event.button === 0 && !event.shiftKey && !event.altKey) openCellFromRoom(event.currentTarget, event.target); }}
      onFocus={(event) => { if (event.target !== event.currentTarget) fitCellEditor(event.currentTarget); else ways.rangeChosen?.(cellLine(event.currentTarget), column); }}
      onKeyDownCapture={(event) => {
        // The page's history first, as a table's cell (TableGridRow).
        if (stepsPageHistory(event.currentTarget, event.nativeEvent) || undoneCellKey(event.currentTarget, event.nativeEvent)) {
          event.preventDefault();
          event.stopPropagation();
          return;
        }
        if ((event.key === "Escape" || fieldEnter(event)) && event.target !== event.currentTarget && !imeComposing(event)
          && !(event.key === "Escape" && !escapeKeepsChosen(property.type, event.target))) {
          // Enter goes down within its group, as a table's (tablePart).
          keepCellChosen(event.currentTarget, fieldEnter(event) && !event.shiftKey && enterMovesDown(property.type) && ways.part(cellLine(event.currentTarget)));
        }
      }}
      onKeyDown={(event) => chosenCellKey(event, row, property, Boolean(title), editable, ways)}
    >
      {before}
      <div className={styles.cellEditor} data-cell-editor data-cell-read-only={readOnly ? "true" : undefined}>{children}</div>
      {after}
    </div>
  );
}

// A key on the chosen cell itself, not on its editor, as on a table's (TableGridRow).
function chosenCellKey(event: KeyboardEvent<HTMLDivElement>, row: Page, property: DbProperty, title: boolean, editable: boolean, ways: TimelineTableCellWays) {
  const cell = event.currentTarget;
  if (event.target !== cell || imeComposing(event) || ways.rangeKey?.(event, cellLine(cell), Number(cell.dataset.colIndex))) return;
  const plain = !event.shiftKey && !event.altKey && !event.metaKey && !event.ctrlKey;
  const done = () => { event.preventDefault(); event.stopPropagation(); };
  if (fieldEnter(event) && plain) {
    if (editable && editChosenCellByKey(cell, event.key)) done();
    return;
  }
  if (title && fieldEnter(event) && primaryModifier(event)) { done(); ways.open(row.id, "new_tab"); return; }
  if (event.key === "Escape" && plain) { done(); cell.blur(); ways.chooseRow(row.id); return; }
  if (clearsChosenCell(cell, event)) { done(); ways.clear(row, property); return; }
  if (typeIntoChosenCell(cell, event)) { event.stopPropagation(); return; }
  if (event.key === "ContextMenu" || (event.shiftKey && event.key === "F10")) { done(); ways.menu(row.id, cell); return; }
  // Tab moves the choice on and Shift+Tab keeps it, the arrows go on across rows, each within its group, as a table's (chosenCellAfterKey).
  if (event.key === "Tab" ? !tabChoosesNextCell(event) : !plain) return;
  const line = cellLine(cell), columns = cell.parentElement?.querySelectorAll(":scope > [data-table-cell]").length ?? 0;
  const next = chosenCellAfterKey(event.key, cell, { row: line, column: Number(cell.dataset.colIndex) }, columns, ways.part(line));
  const target = next?.moved ? cellOnLine(cell.closest('[role="grid"]'), next.row, next.column) : null;
  if (next) done();
  if (target) chooseCellByKey(target);
}

/**
 * A timeline table row's name, as a table's title cell (Notion's timeline
 * table, seen 2026-11-24 in two tabs): pressed — ⌘ held or twice too — its
 * title is written in place; its row opens from the "열기" its row shows under
 * the pointer, which reads "닫기" while the row is open in the side peek.
 */
export function TimelineTableNameCell({ row, property, column, snapshot, cellWays, readOnly, context, ways, openIn, closes, before }: {
  row: Page;
  property: DbProperty;
  column: number;
  snapshot: PropertyCellSnapshot;
  cellWays: Pick<PropertyCellEnvironment, "context" | "operations">;
  readOnly: boolean;
  context: PropertyValueReadContext;
  ways: TimelineTableCellWays & {
    contextMenu(rowId: string, event: ReactMouseEvent<HTMLElement>): void;
    /** Closes the row open in the side peek. */
    close(): void;
    t(key: string, values?: Record<string, unknown>): string;
  };
  /** Where "열기" opens the row. */
  openIn?: "side" | "center" | "full";
  /** "열기" reads "닫기" while the row shows in the side peek, and closes it. */
  closes: boolean;
  before?: ReactNode;
}) {
  const commented = (useContext(CellCommentsContext)?.rowDiscussions(row.id) ?? 0) > 0;
  const labels = { open: ways.t("tableView:openRowLabel"), close: ways.t("tableView:closeRowLabel"),
    openAria: openIn === "center" || openIn === "full" ? ways.t(`databaseView:peekModes.${openIn}`) : ways.t("tableView:openInSidePeek") };
  return (
    <TimelineTableCell row={row} property={property} column={column} title editable={!readOnly && !row.isLocked} readOnly={readOnly} ways={ways}
      data-row-id={row.id} data-row-commented={commented ? "true" : undefined} onContextMenu={(event) => ways.contextMenu(row.id, event)} before={before}
      after={<>
        {/* Its open discussions after its name, as a table's title (Notion's timeline table, seen 2026-11-15). */}
        <RowCommentCount rowId={row.id} />
        <TableRowOpenButton rowId={row.id} openIn={openIn} labels={labels} onClose={closes ? ways.close : undefined}
          onOpen={(event) => ways.open(row.id, rowOpenModeOfClick(event))} />
      </>}>
      {row.isLocked ? <PropValue context={context} row={row} prop={property} interactive={false} titleIcon="page" />
        : <PropertyCell snapshot={snapshot} {...cellWays} row={row} prop={property} titleIcon="page" readOnly={readOnly}
          onOpenPage={(id) => ways.open(id)} />}
    </TimelineTableCell>
  );
}
