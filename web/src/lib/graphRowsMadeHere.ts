import type { ContentGraph } from "@/contentGraph";
import { itemsSplicedFrom, spliceItems } from "@/lib/rowsReplaced";

/**
 * A row made on this page enters the graph before the server's reread gives
 * it the time it was made and who made it; it takes them now, so a view that
 * orders or shows rows by them (a new view's newest first, "생성 일시",
 * "생성자") places it at once. A row the graph already had, or one that came
 * with its time, is left as it is; the server keeps its own on the next read.
 */
export function stampRowsMadeHere(before: ContentGraph, after: ContentGraph, userId: string, now = new Date().toISOString()): ContentGraph {
  if (after.rows === before.rows) return after;
  const known = new Set(before.rows.map((row) => row.id));
  const stamped: number[] = [];
  const rows = after.rows.map((row, index) => {
    if (known.has(row.id) || row.createdAt) return row;
    stamped.push(index);
    return { ...row, createdAt: now, createdBy: row.createdBy || userId, updatedAt: now, lastEditedBy: row.lastEditedBy || userId };
  });
  if (!stamped.length) return after;
  // Rows put in as a splice of the rows shown stay one, stamped (lib/rowsReplaced.ts):
  // what the views work out of their rows is then worked out for them alone.
  const spliced = itemsSplicedFrom(after.rows);
  const put = spliced && new Set(spliced.added);
  if (spliced && stamped.every((index) => put!.has(index))) {
    return { ...after, rows: spliceItems(spliced.from, spliced.removed, spliced.added.map((index) => [index, rows[index]] as const)) };
  }
  return { ...after, rows };
}
