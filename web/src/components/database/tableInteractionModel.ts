import { arrowAlongRow } from "@/lib/readingDirection";

export type TableDropSide = "before" | "after";

export function moveTableIds(
  ids: readonly string[],
  movingIds: readonly string[],
  targetId: string,
  side: TableDropSide,
) {
  const moving = new Set(movingIds);
  const retained = ids.filter((id) => !moving.has(id));
  const target = retained.indexOf(targetId);
  const insertion = target < 0 ? retained.length : target + (side === "after" ? 1 : 0);
  retained.splice(insertion, 0, ...ids.filter((id) => moving.has(id)));
  return retained;
}

/**
 * How narrow a table column drags: a checkbox's 32px, any other's 100px, as
 * Notion's (seen 2026-11-03); none has a widest (Notion's went past 1458).
 */
export function tableColumnMinWidth(type: string | undefined) {
  return type === "checkbox" ? 32 : 100;
}

export function resizeTableColumn(width: number, type: string | undefined) {
  return Math.max(tableColumnMinWidth(type), Math.round(width));
}

/**
 * The cell a key moves a table's choice to, `key` along the reading order.
 * `wraps`, a chosen cell's own move: ← and → go on across rows — past a
 * row's last cell to the next row's first, before its first to the row
 * before's last — and stop at the table's first and last cells, as Notion's
 * (2026-11-26, Chrome 1440, two tabs at two times, judged by the screen after
 * the key with nothing run in the page before it: → from a row's last column
 * chose the next row's title cell, ← from a title the row above's last cell,
 * the table's last cell staying). A range being stretched stops at the row's
 * ends. `first` and `rows` bound the lines the keys go over: from `first` to
 * before `rows` (a grouped table's group, tablePart).
 */
export function nextTableCell({
  row,
  column,
  rows,
  columns,
  key,
  wraps = false,
  first = 0,
}: {
  row: number;
  column: number;
  rows: number;
  columns: number;
  key: TableArrow;
  wraps?: boolean;
  first?: number;
}) {
  const delta = key === "ArrowDown" ? [1, 0]
    : key === "ArrowUp" ? [-1, 0]
      : key === "ArrowRight" ? [0, 1] : [0, -1];
  if (wraps && delta[1] !== 0) {
    const at = row * columns + column + delta[1];
    return at < first * columns || at >= rows * columns ? { row, column } : { row: Math.floor(at / columns), column: at % columns };
  }
  return {
    row: Math.max(first, Math.min(rows - 1, row + delta[0])),
    column: Math.max(0, Math.min(columns - 1, column + delta[1])),
  };
}

export type TableArrow = "ArrowDown" | "ArrowLeft" | "ArrowRight" | "ArrowUp";
const TABLE_ARROWS = new Set<string>(["ArrowDown", "ArrowLeft", "ArrowRight", "ArrowUp"]);

/** The lines of a part of a table: from `first` to before `end`. */
export type TablePart = { first: number; end: number };

/**
 * The lines of the part of a table `line` stands in, `starts` the first line
 * of each part in order: a grouped table's group, each its own table to a
 * chosen cell's keys, as Notion's (2026-11-26, Chrome at 1000 and 1440 in
 * two tabs at two times, a table and a timeline's table grouped by a select,
 * judged by the screen after the key with nothing run in the page before it:
 * ←, → and Tab went on across rows within a group and stopped at its first
 * and last cells, ↑ and ↓ at its first and last rows, a sub-item the last
 * row of its group too) — and to the rest a cell does to the lines around
 * it: Enter leaving a title's or a text's editor on a group's last row keeps
 * its own cell (keepCellChosen), a range stretched by Shift and the arrows,
 * by the pointer pressed on a cell or by the chosen cell's corner dot stays
 * in the group, and a paste of more lines than the group has left fills its
 * rows and makes the rest new rows in it (tableCellPaste.ts) (Notion's, 2026-11-26 in Chrome 1440, two tabs at two times,
 * judged after the action with a letter typed then: Enter on a group's last
 * row kept its title and its text chosen in a table and a timeline's table,
 * the letter written there; Shift+↓ there and Shift+↑ on the next group's
 * first row grew nothing, in both; a pointer or the dot pulled from one
 * group onto another's row chose nothing and filled nothing; two lines
 * pasted on a group's last row left the next group's rows as they were).
 */
export function tablePart(starts: readonly number[], line: number, lines: number): TablePart {
  let first = 0, end = lines;
  for (const start of starts) {
    if (start > line) { end = start; break; }
    first = start;
  }
  return { first, end };
}

/**
 * Where a key moves a table's chosen cell, the one rule a table's and a
 * timeline's table follow: an arrow by the reading direction — ← and →
 * going on across rows (nextTableCell `wraps`) — and Tab, which the chosen
 * cell alone hands on (tabChoosesNextCell), the cell after it in reading
 * order, each within the cell's part of the table (tablePart), as Notion's
 * (2026-11-26, Chrome, two tabs at two times each: a table's and a
 * timeline's table, a sub-item's row a line in its place and the "+ 새 하위
 * 항목" line passed over). Its own place where it stays, the key still the
 * cell's: Notion's page did not roll for an arrow at the end of a group.
 * Null for a key not its own.
 */
export function chosenCellAfterKey(key: string, element: Element, at: { row: number; column: number }, columns: number, part: TablePart) {
  const step = key === "Tab" ? "ArrowRight" : arrowAlongRow(key, element);
  if (!TABLE_ARROWS.has(step)) return null;
  const next = nextTableCell({ ...at, columns, wraps: true, first: part.first, rows: part.end, key: step as TableArrow });
  return { ...next, moved: next.row !== at.row || next.column !== at.column };
}

export function selectedRowMutation(rowIds: readonly string[], kind: "delete" | "duplicate") {
  return { kind, rowIds: [...new Set(rowIds)].sort() };
}

export function tableInsertionNeighbours<Row extends { id: string }>(
  rows: readonly Row[],
  movingIds: readonly string[],
  targetId: string,
  side: TableDropSide,
) {
  const order = moveTableIds(rows.map((row) => row.id), movingIds, targetId, side);
  const moved = new Set(movingIds);
  const first = order.findIndex((id) => moved.has(id));
  let last = first;
  while (last + 1 < order.length && moved.has(order[last + 1])) last += 1;
  return {
    beforeId: first > 0 ? order[first - 1] : null,
    afterId: last >= 0 && last + 1 < order.length ? order[last + 1] : null,
  };
}

/**
 * A value the selection bar sets for every selected row, from the change made
 * in the row it shows. A list (tags, people, relations) changes in each row
 * as it changed there: what was added is added, what was taken away goes, and
 * each row keeps the rest of its own. Any other value replaces each row's.
 */
export function selectedRowsCellValue(shown: unknown, next: unknown, current: unknown): unknown {
  if (!Array.isArray(next)) return next;
  const key = (item: unknown) => typeof item === "string" ? item : JSON.stringify(item);
  const before = new Set((Array.isArray(shown) ? shown : []).map(key));
  const after = new Set(next.map(key));
  const kept = (Array.isArray(current) ? current : []).filter((item) => !before.has(key(item)) || after.has(key(item)));
  const keptKeys = new Set(kept.map(key));
  return [...kept, ...next.filter((item) => !before.has(key(item)) && !keptKeys.has(key(item)))];
}

/**
 * A sticking cell's style takes where its table's cells stick from its row,
 * which is not inherited (DatabaseTableChrome --table-frozen-zone): its
 * frozen cells, row controls and freeze edge.
 */
export const stickyPad = { ["--table-frozen-zone" as string]: "inherit" } as const;
