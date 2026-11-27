/**
 * Whether a board's column, or a sub-group's cell of it, takes a card moved
 * there — dragged or by keys — decided here alone for the move and for what
 * shows while a card is dragged over it (BoardColumn: its drop line and the
 * column's wash), so a column that won't take a card shows neither.
 *
 * Its own column always takes it, to change its place there. Another takes it
 * when the card takes the value the column stands for (`value`: undefined for
 * a group of no one value, a stretch of dates or a worked-out value, or a
 * status the card's own database lacks — boardGroupReplacementValue).
 *
 * "내 작업"'s board (a view over tasks across sources) takes a card only
 * beside the cards a column already shows, as Notion's (seen 2026-11-24 in two
 * tabs, the page script's stepped drag and the extension's real mouse, by
 * groups and by options, sorted by due date): a card dragged over a column
 * with cards of a group or option its own database has showed the blue drop
 * line and moved there, ⌘Z taking it back; over an empty column — one the
 * view's status filter left out, one of options no source's database has
 * now, or one a search emptied — no line showed and the card stayed. A task
 * database's own board took a card into an empty column its filter left out
 * (the line at the column's top, the card leaving the view), so a database's
 * board keeps taking it.
 */
export function boardCellTakesCard({ sameColumn, value, cellRows, config }: {
  sameColumn: boolean;
  value: unknown;
  cellRows: readonly unknown[];
  config: Readonly<Record<string, unknown>> | null | undefined;
}): boolean {
  if (sameColumn) return true;
  if (value === undefined) return false;
  return config?.tasksAcrossSources !== true || cellRows.length > 0;
}
