import type { ClipboardEvent } from "react";
import { LEAFEN_TABLE_ROWS_MIME, readTextWithMime } from "@/lib/clipboard";
import { parseTableClipboard, TABLE_CLIPBOARD_BOUNDS } from "./tableClipboardModel";
import { cellLine } from "./tableCellLine";
import type { TablePart } from "./tableInteractionModel";

/** A range of cells, both ends in (useTableCellRange). */
type Box = { top: number; bottom: number; left: number; right: number };

/**
 * What a paste writes in a table: each cell's text, line by line from the
 * first cell it fills (a hole where the copy had no cell), the copied cell it
 * comes from (its kept value and formatting, tableClipboardModel), Leafen's
 * own entry of the copy, and the line its lines past the part's last row are
 * made after (`after`, a grouped table's group read from it).
 */
export type TablePaste = {
  lines: readonly (readonly string[])[];
  from(line: number, column: number): readonly [number, number];
  entry: string;
  after?: string;
};

/**
 * The cells a paste's text makes: a grid by its tabs and lines where Leafen
 * copied it or where it has both a tab and a line break, else one value with
 * its lines in it, as Notion's (2026-11-26, Chrome, two tabs at two times,
 * judged by the cells after the paste: two lines with no tab went whole into
 * a text cell and a title, a line of tabbed values whole into a text cell and
 * a select, and tabbed lines spread over the cells). A last line break alone
 * makes no line.
 */
export function pastedCells(text: string, own: boolean): string[][] {
  if (!text) return [];
  const body = text.replace(/\r?\n$/, "");
  return own || (body.includes("\t") && /[\r\n]/.test(body)) ? parseTableClipboard(text) : [[body]];
}

/**
 * Where a paste goes and what each cell takes: from the chosen cell, or a
 * range's first cell, the copied cells once — over a range as many times as
 * they fit whole, both across and down, when it is that many of them, as
 * Notion's (2026-11-26, Chrome, two tabs at two times: one copied value
 * filled every cell of a range in a table and a timeline's table, two lines
 * four rows as 1, 2, 1, 2; two lines on three rows and three lines on two
 * went once from the range's first cell).
 */
export function tablePasteArea(cells: readonly (readonly string[])[], at: { row: number; column: number }, box: Box | null) {
  const height = cells.length, width = Math.max(1, ...cells.map((line) => line.length));
  const rows = box ? box.bottom - box.top + 1 : 0, columns = box ? box.right - box.left + 1 : 0;
  const start = box ? { row: box.top, column: box.left } : at;
  if (!box || rows % height !== 0 || columns % width !== 0) {
    return { ...start, lines: cells, from: (line: number, column: number) => [line, column] as const };
  }
  const lines = Array.from({ length: rows }, (_, line) => {
    const tiled: string[] = [];
    for (let column = 0; column < columns; column += 1) {
      const value = cells[line % height][column % width];
      if (value !== undefined) tiled[column] = value;
    }
    return tiled;
  });
  return { ...start, lines, from: (line: number, column: number) => [line % height, column % width] as const };
}

/**
 * A paste on a table's chosen cell or range, a table's and a timeline's
 * table's alike: the rows it writes from the first cell on, within that
 * cell's part of the table (a grouped table's group, tablePart), the lines
 * past the part's last row made as new rows after it (TablePaste `after`),
 * as Notion's (2026-11-26, Chrome, two tabs at two times: three lines pasted
 * on a table's last row, a group's, and a timeline's table's wrote the first
 * there and made two rows under it, in its group with its value).
 */
export function pasteOnTableCell<Row>(event: ClipboardEvent<HTMLElement>, ways: {
  range: { paste(row: number, column: number, cells: readonly (readonly string[])[]): ReturnType<typeof tablePasteArea> };
  part(line: number): TablePart;
  rows(from: number, to: number): Row[];
  /** The line a row stands on, as its new rows' place names it. */
  lineOf(line: number): string | undefined;
  /** Writes the paste; without it the table takes no paste. */
  write?(rows: Row[], column: number, paste: TablePaste): void;
}) {
  const cell = event.target as HTMLElement;
  if (!ways.write || !cell.matches("[data-table-cell]")) return;
  const entry = readTextWithMime(event.clipboardData, LEAFEN_TABLE_ROWS_MIME, TABLE_CLIPBOARD_BOUNDS);
  const cells = pastedCells(event.clipboardData.getData("text/plain"), Boolean(entry));
  if (!cells.length) return;
  event.preventDefault();
  const { row, column, lines, from } = ways.range.paste(cellLine(cell), Number(cell.dataset.colIndex), cells);
  const { end } = ways.part(row);
  ways.write(ways.rows(row, Math.min(row + lines.length, end)), column, { lines, from, entry, after: ways.lineOf(end - 1) });
}
