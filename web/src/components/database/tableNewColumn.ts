/**
 * The column a column menu's "왼쪽에 삽입" or "오른쪽에 삽입" makes room for
 * while its box is open, as Notion's (seen 2026-11-14 at 1440, a filled and
 * an empty table): an empty 410px column at that edge of the column, in the
 * head (no lines) and in every line under it — the rows' cells, an empty
 * table's lines and the calculations' — each with its lines, pushing the
 * columns after it along and leaving the table's sideways scroll where it
 * is. The box opens over its start (TableInsertProperty); the column becomes
 * the property chosen there, or goes as the box closes with none.
 */
export const TABLE_NEW_COLUMN_WIDTH = 410;

/** Where the new column stands among the shown columns, if one is made and the place is among them. */
export function tableNewColumnPlace(count: number, at: number | undefined) {
  return at !== undefined && at >= 0 && at <= count ? at : undefined;
}

/** The widths of a table's tracks: its shown columns', and the new column's at its place. */
export function tableTrackWidths(widths: readonly number[], count: number, at: number | undefined) {
  const tracks = widths.slice(0, count);
  const place = tableNewColumnPlace(count, at);
  if (place !== undefined) tracks.splice(place, 0, TABLE_NEW_COLUMN_WIDTH);
  return tracks;
}

/** A line's grid columns, the new column's among them. */
export function tableTrackColumns(widths: readonly number[], count: number, at: number | undefined) {
  return tableTrackWidths(widths, count, at).map((width) => `${width}px`).join(" ");
}

/** A line's cells, the new column's empty one at its place. */
export function withNewColumnCell<T>(cells: readonly T[], at: number | undefined, cell: () => T): T[] {
  const place = tableNewColumnPlace(cells.length, at);
  return place === undefined ? [...cells] : [...cells.slice(0, place), cell(), ...cells.slice(place)];
}
