import type { GraphProperty, GraphRow, GraphView } from "@/contentGraph";
import { graphPositionBetween } from "@/lib/graphPosition";
import { tableInsertionNeighbours } from "./tableInteractionModel";
import { viewKeepsOwnRowOrder, viewRowOrderPlacing } from "./viewRowOrder";

/** What writes a row's move: the table's writer (nativeTableProductAdapter.ts), as the view's personal queries wrap it. */
type RowMover = {
  moveRows?(rows: readonly GraphRow[], view: GraphView | undefined, rowIds: readonly string[], isCurrent: () => boolean,
    values?: Readonly<Record<string, GraphRow["values"]>>, withDates?: boolean): Promise<void>;
  updateView(view: GraphView, isCurrent: () => boolean, step?: boolean): Promise<void>;
  commitRows(rows: readonly GraphRow[], isCurrent: () => boolean): Promise<void>;
};

/** The values a move sets (`values`, by property), as a row holds them, of the properties there are. */
export function movedRowValues(properties: readonly GraphProperty[], values?: Readonly<Record<string, unknown>>): GraphRow["values"] | undefined {
  return values && Object.fromEntries(Object.entries(values).flatMap(([id, value]) => {
    const property = properties.find((candidate) => candidate.id === id);
    return property ? [[id, { type: property.type, value: value ?? null }]] : [];
  }));
}

/** The values a move sets by row (`values`, by row and property), as rows hold them. */
export function movedRowsValues(properties: readonly GraphProperty[], values?: Readonly<Record<string, Readonly<Record<string, unknown>>>>) {
  const byRow = Object.entries(values ?? {}).flatMap(([id, set]) => { const moved = movedRowValues(properties, set); return moved && Object.keys(moved).length ? [[id, moved] as const] : []; });
  return byRow.length ? Object.fromEntries(byRow) as Record<string, GraphRow["values"]> : undefined;
}

/**
 * Where rows dropped beside `target` go among a view's rows as it shows them
 * (`shown`, in its sorts' and filters' order): in the view alone once it
 * keeps its own order (viewRowOrder.ts: `view`, its order with the rows
 * placed there), as Notion's views do, else between their neighbours in the
 * database's one order. Several go together in the order given, the view's
 * (chosenCarried). `values` are the values the move sets, by row (another
 * group's); `rows` are the rows the move writes — their new places, or, in a
 * view keeping its own order, the rows taking a value — and `written`, those
 * rows with their values, for a writer that saves rows alone.
 */
export function shownRowsPlaced(view: GraphView, shown: readonly GraphRow[], rowIds: readonly string[],
  target: { id: string; side: "before" | "after" }, values?: Readonly<Record<string, GraphRow["values"]>>) {
  const setsOn = (id: string) => Boolean(values?.[id] && Object.keys(values[id]).length);
  const withValues = (rows: readonly GraphRow[]) => rows.map((row) => setsOn(row.id) ? { ...row, values: { ...row.values, ...values![row.id] } } : row);
  if (viewKeepsOwnRowOrder(view.config)) {
    const rows = shown.filter((row) => setsOn(row.id));
    return { view: { ...view, config: { ...view.config, rowOrder: viewRowOrderPlacing(view.config, shown.map(({ id }) => id), rowIds, target) } },
      rows, written: withValues(rows) };
  }
  const neighbours = tableInsertionNeighbours(shown, rowIds, target.id, target.side);
  const after = shown.find((row) => row.id === neighbours.afterId)?.position;
  let cursor = shown.find((row) => row.id === neighbours.beforeId)?.position;
  const rows = rowIds.flatMap((id) => {
    const row = shown.find((candidate) => candidate.id === id);
    if (!row) return [];
    cursor = graphPositionBetween(cursor, after);
    return [{ ...row, position: cursor }];
  });
  return { view: undefined as GraphView | undefined, rows, written: withValues(rows) };
}

/**
 * Rows moved beside another among a view's rows as it shows them — a list's,
 * a gallery's or a timeline's — as a table's rows move (TableView.tsx), where
 * shownRowsPlaced puts them. It goes through the writer's moveRows, which
 * makes it a step of ⌘Z (nativeRowMoveSteps.ts) and, in a sorted view, asks
 * whether the sort goes (useDatabasePersonalQueries, sortedViewDrop.ts). The
 * values the move sets (`values`, by row: another group's) go in that one
 * step, as Notion's: a row dropped in another group of a list or a timeline
 * went back to its group and place at one ⌘Z (seen 2026-11-24 in Chrome, two
 * tabs, several times), and so did two chosen rows dragged there together.
 * `withDates`: the row's dates were written at once by another write (a
 * timeline's bar carried along the days too), whose step joins this one.
 */
export function moveShownRows(writer: RowMover, view: GraphView, shown: readonly GraphRow[], rowIds: readonly string[],
  target: { id: string; side: "before" | "after" }, isCurrent: () => boolean, values?: Readonly<Record<string, GraphRow["values"]>>, withDates?: boolean): Promise<void> {
  const placed = shownRowsPlaced(view, shown, rowIds, target, values);
  const set = placed.rows.some(({ id }) => values?.[id] && Object.keys(values[id]).length) ? values : undefined;
  if (placed.view && !writer.moveRows) return placed.rows.length
    ? writer.updateView(placed.view, isCurrent).then(() => writer.commitRows(placed.written, isCurrent)) : writer.updateView(placed.view, isCurrent);
  if (!placed.view && !placed.rows.length) return Promise.resolve();
  return writer.moveRows ? writer.moveRows(placed.rows, placed.view, rowIds, isCurrent, set, withDates) : writer.commitRows(placed.written, isCurrent);
}
