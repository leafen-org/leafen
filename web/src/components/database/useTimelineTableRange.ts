import { useLayoutEffect, type ClipboardEvent, type RefObject } from "react";
import type { GraphProperty, GraphRow } from "@/contentGraph";
import type { TimelineViewOperationContext, TimelineViewOperations, TimelineViewSnapshot } from "./TimelineView";
import type { TimelineTableLane } from "./timelineLaneProjection";
import { useTableCellRange } from "./useTableCellRange";
import { pasteOnTableCell, type TablePaste } from "./tableCellPaste";
import { copyTableCells, pastedLines } from "./tablePasteWrite";
import { cellOnLine } from "./tableCellLine";
import { cellClears, chooseCellByKey } from "./tableCellKeys";
import { rowValuesLocked } from "./rowValuesLocked";
import type { TablePart } from "./tableInteractionModel";

/** What a timeline's table's range does with its rows' values, by their ids and its columns' properties'. */
export type TimelineTableCells = {
  copy(rowIds: readonly string[], propertyIds: readonly string[]): void;
  clear(rowIds: readonly string[], propertyIds: readonly string[]): void;
  /** Writes a paste from the rows given on, its lines past them made new rows after its `after` line. */
  paste?(rowIds: readonly string[], propertyIds: readonly string[], paste: TablePaste): void;
};

type Run = <Result>(requestKey: string, perform: (context: TimelineViewOperationContext) => Promise<Result>,
  publish?: (result: Result, context: TimelineViewOperationContext) => void, write?: boolean) => Promise<void>;

/**
 * A timeline's table's cells as its range copies, clears and pastes them, a
 * table's way (TableView): values written to all the rows at once, one step
 * of ⌘Z (moveOrResizeRow with the other rows alongside); a locked row, a
 * value Leafen works out and a table that only shows its values keep theirs.
 */
export function timelineTableCells({ snapshot, readOnly, operations, run, makeRows }: {
  snapshot: Pick<TimelineViewSnapshot, "rows" | "properties" | "dataSource" | "propertyValueContext">;
  readOnly: boolean;
  operations: Pick<TimelineViewOperations, "moveOrResizeRow">;
  run: Run;
  /** Writes the rows a paste fills and makes rows of its lines past the table's part, after a line, one step (useTimelineRowsMade). */
  makeRows?(changed: readonly GraphRow[], lines: readonly GraphRow["values"][], after: string): void;
}): TimelineTableCells {
  const rowsOf = (ids: readonly string[]) => ids.flatMap((id) => snapshot.rows.find((row) => row.id === id) ?? []);
  const propertiesOf = (ids: readonly string[]) => ids.flatMap((id) => snapshot.properties.find((property) => property.id === id) ?? []);
  const write = (values: Map<string, Record<string, unknown>>) => {
    const [first, ...rest] = [...values.keys()];
    if (first) void run(`row:cells:${first}`, (context) => operations.moveOrResizeRow(first, values.get(first)!, context,
      Object.fromEntries(rest.map((id) => [id, values.get(id)!]))), undefined, true);
  };
  const writable = (row: GraphRow, property: GraphProperty) => !rowValuesLocked(row) && !row.readOnlyValues?.includes(property.id) && !row.hiddenValues?.includes(property.id);
  return {
    copy: (rowIds, propertyIds) => copyTableCells(snapshot.dataSource.id, rowsOf(rowIds), propertiesOf(propertyIds), snapshot.propertyValueContext),
    clear: (rowIds, propertyIds) => {
      if (readOnly) return;
      const columns = propertiesOf(propertyIds).filter((property) => cellClears(property.type));
      write(new Map(rowsOf(rowIds).map((row) => [row.id, Object.fromEntries(columns.filter((property) => writable(row, property)).map((property) => [property.id, null]))])));
    },
    paste: readOnly ? undefined : (rowIds, propertyIds, paste) => {
      const lines = pastedLines(paste, propertiesOf(propertyIds), snapshot.propertyValueContext.pagesById), rows = rowsOf(rowIds);
      const kept = (row: GraphRow, values: GraphRow["values"] = {}) => Object.fromEntries(Object.entries(values)
        .filter(([id]) => writable(row, snapshot.properties.find((property) => property.id === id)!)));
      if (lines.length > rows.length && paste.after && makeRows) {
        makeRows(rows.map((row, at) => ({ ...row, values: { ...row.values, ...kept(row, lines[at]) } })), lines.slice(rows.length), paste.after);
        return;
      }
      write(new Map(rows.map((row, at) => [row.id, Object.fromEntries(Object.entries(kept(row, lines[at])).map(([id, value]) => [id, value.value]))])));
    },
  };
}

/**
 * A timeline's table's range of cells, the table's own piece
 * (useTableCellRange), as Notion's timeline table (2026-11-26, Chrome, two
 * tabs at two times, judged by the screen after each key with a letter typed
 * then): Shift and the arrows grow it from the chosen cell within its group,
 * Escape chooses its rows, Delete and Backspace clear it, ⌘C copies it, ⌘X
 * copies and clears it, a paste fills it, a letter writes the chosen cell.
 * Notion's timeline table's chosen cell has no corner dot (seen once), nor
 * this one. Its cells take the range's tint themselves (`data-cell-range`), its
 * rows not drawn again for it.
 */
export function useTimelineTableRange({ rowsRef, part, items, propertyIds, cells, chooseLines }: {
  rowsRef: RefObject<HTMLElement | null>;
  part(line: number): TablePart;
  /** Its rows in their lines' order. */
  items: readonly TimelineTableLane[];
  propertyIds: readonly string[];
  cells?: TimelineTableCells;
  /** Chooses the rows of the lines given, as its rows' choice. */
  chooseLines(lines: readonly string[]): void;
}) {
  const lineId = (line: number) => items[line] && (items[line].line ?? items[line].row.id);
  const rowIds = (top: number, bottom: number) => items.slice(top, bottom + 1).map((item) => item.row.id);
  const range = useTableCellRange({
    gridRef: rowsRef, part, columns: propertyIds.length,
    focusCell: (row, column) => { const cell = cellOnLine(rowsRef.current, row, column); if (cell) chooseCellByKey(cell); },
    // Its rows chosen, the cell lets the focus go, as a chosen cell's Escape does (TimelineTableCell).
    onRows: (rows) => {
      const active = document.activeElement;
      if (active instanceof HTMLElement && active.matches("[data-table-cell]")) active.blur();
      chooseLines(rows.flatMap((row) => lineId(row) ?? []));
    },
    onClear: (box) => cells?.clear(rowIds(box.top, box.bottom), propertyIds.slice(box.left, box.right + 1)),
    onCopy: (box) => cells?.copy(rowIds(box.top, box.bottom), propertyIds.slice(box.left, box.right + 1)),
    onFill: () => {},
  });
  const { box } = range;
  useLayoutEffect(() => {
    const grid = rowsRef.current;
    if (!grid) return;
    for (const cell of grid.querySelectorAll<HTMLElement>("[data-cell-range]")) delete cell.dataset.cellRange;
    if (!box) return;
    for (let line = box.top; line <= box.bottom; line += 1) {
      for (let column = box.left; column <= box.right; column += 1) {
        const cell = cellOnLine(grid, line, column);
        if (cell) cell.dataset.cellRange = "true";
      }
    }
  }, [box?.top, box?.bottom, box?.left, box?.right, rowsRef, items]); // eslint-disable-line react-hooks/exhaustive-deps
  return {
    keyDown: range.keyDown,
    chosen: range.chosen,
    clear: range.clear,
    paste: (event: ClipboardEvent<HTMLElement>) => pasteOnTableCell(event, { range, part, lineOf: lineId,
      rows: (from, to) => items.slice(from, to).map((item) => item.row.id),
      write: cells?.paste ? (ids, column, paste) => cells.paste!(ids, propertyIds.slice(column), paste) : undefined }),
  };
}
