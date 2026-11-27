/**
 * A view's own order of its rows, as Notion's views keep one each (seen
 * 2026-11-02): a view made since keeps `rowOrder`, a list of row ids. With
 * none placed yet it shows the newest rows first; once a row is made at a
 * place in it or moved in it, it keeps that order, and rows it has not
 * placed — made through the API, in another view or brought in — follow
 * right after, newest first (Notion's, seen 2026-11-11), those of one time
 * by their ids. The server's query
 * keeps the same order (database_query_linked.go databaseNaturalOrder). A
 * view from before keeps the database's one order (no `rowOrder`), so its
 * rows stay where they were.
 */

import { graphPositionBetween } from "@/lib/graphPosition";
import { lineRowId } from "./viewGroupLines";

type OrderedRow = { id: string; createdAt?: string };

export function viewKeepsOwnRowOrder(config: Readonly<Record<string, unknown>> | null | undefined) {
  return Array.isArray(config?.rowOrder);
}

/** Newest first; rows of one created time by their ids, as Notion's (seen 2026-11-17 twice: the rows one MCP call makes share its time). */
function newestFirst(left: OrderedRow, right: OrderedRow) {
  const a = left.createdAt ?? "", b = right.createdAt ?? "";
  return a > b ? -1 : a < b ? 1 : left.id < right.id ? -1 : left.id > right.id ? 1 : 0;
}

/** The rows in the view's own order, or as they came when it has none. */
export function rowsInViewOrder<T extends OrderedRow>(rows: readonly T[], config: Readonly<Record<string, unknown>> | null | undefined): T[] {
  const order = config?.rowOrder;
  if (!Array.isArray(order)) return [...rows];
  if (order.length === 0) return [...rows].sort(newestFirst);
  const place = new Map(order.map((id, index) => [String(id), index]));
  const placed = rows.filter((row) => place.has(row.id)).sort((left, right) => place.get(left.id)! - place.get(right.id)!);
  const rest = rows.filter((row) => !place.has(row.id)).sort(newestFirst);
  return [...placed, ...rest];
}

/**
 * The view's own order as IDs to write it again from: every row it has
 * placed keeps its place, those not among `rows` too — a row in the trash
 * comes back where it stood, as Notion's does (seen 2026-11-15: a row
 * trashed, another moved past its place, the row brought back stood again
 * between the rows it stood between) — and the rows of `rows` it hasn't
 * placed follow, newest first. `rows` is one database's.
 */
export function viewOrderIds(rows: readonly OrderedRow[], config: Readonly<Record<string, unknown>> | null | undefined): string[] {
  const order = config?.rowOrder;
  if (!Array.isArray(order) || order.length === 0) return rowsInViewOrder(rows, config).map(({ id }) => id);
  const placed = [...new Set(order.map(String))], placedIds = new Set(placed);
  return [...placed, ...rows.filter(({ id }) => !placedIds.has(id)).sort(newestFirst).map(({ id }) => id)];
}

/**
 * The view's own order once a row New made is placed in it, as Notion's New
 * places it (seen 2026-11-12): a table, list or gallery writes down the order
 * it shows (`shownIds`, in that order) with the new row first, so rows made
 * since — through the API or in another view — follow them all; a board puts
 * the new row last in its order alone, at the end of its group, and the rows
 * it hasn't placed still come after, newest first. Rows the view keeps but
 * doesn't show now keep their places after those shown. Null when the view
 * keeps the database's one order.
 */
export function rowOrderWithNew(config: Readonly<Record<string, unknown>> | null | undefined, rowId: string,
  place: "first" | "last", shownIds: readonly string[]): string[] | null {
  const order = config?.rowOrder;
  if (!Array.isArray(order)) return null;
  const kept = order.map(String).filter((id) => id !== rowId);
  if (place === "last") return [...kept, rowId];
  const shown = shownIds.filter((id) => id !== rowId);
  const shownSet = new Set(shown);
  return [rowId, ...shown, ...kept.filter((id) => !shownSet.has(id))];
}

/**
 * The view's own order with the rows it shows put in the order shown (a
 * sorted view's), and the rows it leaves out where they were.
 */
export function orderAsShown(kept: readonly string[], shown: readonly string[]): string[] {
  const keptIds = new Set(kept), shownIds = new Set(shown);
  const inShownOrder = [...shownIds].filter((id) => keptIds.has(id));
  let next = 0;
  return kept.map((id) => shownIds.has(id) ? inShownOrder[next++]! : id);
}

/**
 * Rows just copied (`detail: { from, copies }`): a view whose chosen rows
 * were the ones copied chooses the copies instead, as Notion's does. Copies
 * the server refused go as `from`, their rows as `copies`: chosen again.
 */
export const ROWS_COPIED_EVENT = "leafen:rows-copied";

/**
 * Where copies of rows go in a view: its order as shown, the row they go
 * beside, and the rows of that row's group as shown.
 */
export type CopiesPlace = { shown: readonly string[]; anchor: { id: string; side: "before" | "after" }; group: readonly string[] };

/**
 * Where a view puts the copies of chosen rows, as Notion's does (seen
 * 2026-11-08): the copies come in the order their rows show, a grouped view's
 * groups one after another, and together in the view's order after the last
 * one shown — just before the row after it in its group, so a copy that falls
 * in another group goes where that row stands. `groups` holds each group's
 * rows as shown (one for a view without groups); a sorted view's order is
 * written down as `shown` first. `beside` says which rows stand side by side
 * (a row's sub-items don't), all of them without it. Chosen rows the view's
 * filter hides are copied too, after those shown, in the order given; the
 * first `shownCount` copies are the shown rows' (Notion's table, seen
 * 2026-11-17 in two tabs: of a shown and a hidden chosen row, ⌘D and the
 * menu's "복제" both copied both, and "1개 선택됨" chose the shown one's copy).
 * A row given more than once — chosen on two lines of a grouped view — is
 * copied as many times, its copies together (Notion's, seen 2026-11-24: 나
 * ticked under x and y, two copies "나 (1)"). The groups hold a grouped
 * view's lines (viewGroupLines.ts) and the choice its lines, so the last
 * line chosen is the one that places the copies, in its own group: 마,
 * tagged x and y, chosen under x alone went before the row after it under x,
 * x's rows written down first; chosen under y alone, or under both, y's
 * (Notion's, seen 2026-11-24 in a table no row of which was placed). A row
 * chosen whole stands for its first line, as chosenAsLines reads it; where
 * the groups hold rows (a board's columns) a row chosen is chosen in each.
 */
export function copiesPlace(
  groups: readonly (readonly string[])[], chosen: Iterable<string>, shown: readonly string[],
  beside: (left: string, right: string) => boolean = () => true,
): { rowIds: string[]; place?: CopiesPlace; shownCount: number } {
  const picked = [...chosen], lines = groups.flat(), present = new Set(lines), first = new Map<string, string>(), times = new Map<string, number>();
  for (const line of lines) if (!first.has(lineRowId(line))) first.set(lineRowId(line), line);
  for (const id of picked) times.set(lineRowId(id), (times.get(lineRowId(id)) ?? 0) + 1);
  const wanted = new Set(times.keys()), each = (ids: readonly string[]) => ids.flatMap((id) => Array.from({ length: times.get(id) ?? 1 }, () => id));
  const wantedLines = new Set(picked.flatMap((id) => present.has(id) ? [id] : first.has(id) ? [first.get(id)!] : []));
  const shownIds = [...new Set(lines.filter((line) => wantedLines.has(line)).map(lineRowId))];
  let group: readonly string[] = [], index = -1;
  for (const candidate of groups) {
    for (let at = 0; at < candidate.length; at += 1) if (wantedLines.has(candidate[at]!)) { group = candidate.map(lineRowId); index = at; }
  }
  if (index < 0) return { rowIds: each([...wanted]), shownCount: each([...wanted]).length };
  const lastId = group[index]!;
  const next = group.slice(index + 1).find((id) => !wanted.has(id) && beside(lastId, id));
  const placed = new Set(shownIds), rowIds = each([...shownIds, ...[...wanted].filter((id) => !placed.has(id))]);
  return { rowIds, shownCount: each(shownIds).length, place: { shown, group, anchor: next ? { id: next, side: "before" } : { id: lastId, side: "after" } } };
}

/**
 * A view's own order once copies go where `place` puts them. A view with no
 * rows placed yet places, as Notion's does (seen 2026-11-11), only the rows
 * of the group the copies go into, in the order shown, so in a grouped view
 * a copy that falls in another group comes first in it; the other rows follow
 * in the order they showed.
 */
export function rowOrderWithCopies(kept: readonly string[], copies: readonly string[], place: CopiesPlace | undefined,
  lastId: string, placedNone: boolean): string[] {
  let order = place ? orderAsShown(kept, place.shown) : [...kept];
  if (place && placedNone) {
    const group = place.group.filter((id) => order.includes(id)), inGroup = new Set(group);
    order = [...group, ...order.filter((id) => !inGroup.has(id))];
  }
  return rowOrderPlacing(order, copies, place?.anchor ?? { id: lastId, side: "after" });
}

/**
 * Positions for `count` copies in a view that shows the database's one order
 * (`rows`, one database's): together right after the last row copied, or
 * just before the row `place` names while the view shows that order unsorted.
 * Null when no position fits between the neighbours.
 */
export function copyPositions(rows: readonly { id: string; position: string }[], count: number, lastId: string, place?: CopiesPlace): string[] | null {
  const byPosition = [...rows].sort((left, right) => left.position < right.position ? -1 : left.position > right.position ? 1
    : left.id < right.id ? -1 : left.id > right.id ? 1 : 0);
  const at = new Map(byPosition.map(({ id }, index) => [id, index]));
  const shown = place?.shown.flatMap((id) => at.get(id) ?? []) ?? [];
  const anchor = place && shown.every((index, step) => step === 0 || shown[step - 1]! < index) ? place.anchor : { id: lastId, side: "after" };
  const index = at.get(anchor.id);
  if (index === undefined) return null;
  const after = anchor.side === "after";
  let low = after ? byPosition[index]!.position : byPosition[index - 1]?.position;
  const high = after ? byPosition[index + 1]?.position : byPosition[index]!.position;
  try {
    return Array.from({ length: count }, () => (low = graphPositionBetween(low, high)));
  } catch {
    return null;
  }
}

/**
 * A view's own order once a row made or moved in it goes beside `target`,
 * or, without one, right after the last row shown: every row it has placed
 * keeps its place, those its search, filters or first rows leave out too,
 * the rows shown (`shownIds`, in the order shown) are written down so, and
 * rows it hasn't placed that aren't shown stay unplaced, after the placed
 * ones. Counted from the rows shown alone, a write left the others out of
 * the order (2026-11-15: a row made under a search kept 12 rows of 31).
 * Notion's, seen once (2026-11-15, Chrome): under a search, a row made at
 * the table's end came right after the last row found and a row moved
 * among those found took its place by them, the rows not found staying
 * where they were, one it had never placed going last. A placed row no
 * longer among the view's rows — in the trash — keeps its place too, as
 * viewOrderIds keeps it.
 */
export function viewRowOrderPlacing(
  config: Readonly<Record<string, unknown>> | null | undefined, shownIds: readonly string[],
  moving: readonly string[], target?: { id: string; side: "before" | "after" } | null,
): string[] {
  const order = config?.rowOrder;
  const placed = Array.isArray(order) ? [...new Set(order.map(String))] : [];
  const placedIds = new Set(placed), movingIds = new Set(moving);
  let last: string | undefined;
  for (const id of shownIds) if (!movingIds.has(id)) last = id;
  return rowOrderPlacing([...orderAsShown(placed, shownIds), ...shownIds.filter((id) => !placedIds.has(id))], moving,
    target ?? (last === undefined ? null : { id: last, side: "after" }));
}

/**
 * The order once `moving` rows go before or after `targetId`, or at the end
 * without one: `shownIds` written down so.
 */
export function rowOrderPlacing(
  shownIds: readonly string[], moving: readonly string[], target?: { id: string; side: "before" | "after" } | null,
): string[] {
  const movingSet = new Set(moving);
  const next = shownIds.filter((id) => !movingSet.has(id));
  const at = target ? next.indexOf(target.id) : -1;
  if (at < 0) return [...next, ...moving];
  next.splice(at + (target!.side === "after" ? 1 : 0), 0, ...moving);
  return next;
}
