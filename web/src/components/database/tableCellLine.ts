/**
 * Where a table cell stands among the table's shown rows, read from its row:
 * a row only moved by a row coming or going above it isn't drawn again, and
 * the table writes its place on the row alone (TableGrid, writeRowPlaces).
 * Written on each cell too, a row made at the top of a 30-row table rewrote
 * some 900 cells in the press that made it, each a change the page's editor
 * looked through (2026-11-15: some 7 ms of a grouped table's "+ 새 페이지"
 * at CPU ×2). The table's head is its first line, its rows from 2 on.
 */
export function cellLine(cell: HTMLElement): number {
  const line = cell.parentElement?.getAttribute("aria-rowindex");
  return line ? Number(line) - 2 : Number.NaN;
}

/** The cell of a column on a shown line of the table, as cellLine reads it. */
export function cellOnLine(grid: ParentNode | null | undefined, line: number, column: string | number | undefined): HTMLElement | null {
  if (!grid || !Number.isFinite(line) || column === undefined) return null;
  return grid.querySelector<HTMLElement>(`[role="row"][aria-rowindex="${line + 2}"] > [data-table-cell][data-col-index="${column}"]`);
}
