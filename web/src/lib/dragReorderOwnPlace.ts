type Side = "before" | "after";

/**
 * Where a row dragged onto `target` lands among a database view's rows, past
 * its own place, as Notion's: the row right after the dragged one takes it
 * after itself, the pointer over either half of it — before it is where the
 * dragged row already is. Seen 2026-11-24 in Chrome at 1685, dpr 1, two
 * tabs, the pointer's own events, the line read while dragging: a table's,
 * a list's (also grouped, within a group), a board column's and a
 * timeline's rows, by its table's ⠿ and by a bar carried up and down. The
 * row right before it keeps its halves: after it the line shows at the
 * dragged row's own top and the drop moves nothing, as Notion's; over the
 * dragged row itself nothing lands. A gallery's cards keep their halves
 * too, as Notion's did (its next card's near half drew the line at the
 * dragged card's own edge, and the drop moved nothing), so it never asks.
 * A list's own row after the dragged one is the row right after it in
 * its run (rowAfterInRun): the first row of the next group is not, since
 * before it is another group's place.
 */
export function sidePastOwnPlace(side: Side, targetFollowsSource: boolean): Side {
  return targetFollowsSource && side === "before" ? "after" : side;
}

/** A view's line as its rows' run reads it: a row at its depth, a line at a depth among them (a sub-item's "+"), or another line. */
type RunLine = { kind: string; item?: { row: { id: string }; depth: number }; depth?: number };

/**
 * The row right after `source` among a view's lines (a table's, a list's),
 * in their order: the next of its own parent's rows, passing over its own
 * rows under it; none when another line (a group's head, its "+", "더 보기")
 * or a row of a parent above it comes first.
 */
export function rowAfterInRun(lines: readonly RunLine[], source: string): string | undefined {
  const at = lines.findIndex((line) => line.kind === "row" && line.item?.row.id === source);
  const depth = lines[at]?.item?.depth;
  if (depth === undefined) return undefined;
  for (let index = at + 1; index < lines.length; index++) {
    const line = lines[index];
    const lineDepth = line.kind === "row" ? line.item?.depth : line.kind === "subitemAdd" ? line.depth : undefined;
    if (lineDepth === undefined || lineDepth < depth) return undefined;
    if (lineDepth === depth) return line.kind === "row" ? line.item?.row.id : undefined;
  }
  return undefined;
}
