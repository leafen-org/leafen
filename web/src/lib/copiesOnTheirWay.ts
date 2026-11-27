import { compareGraphOrder, type ContentGraph, type GraphOperation, type GraphRow } from "@/contentGraph";
import { tellPagesNotYetMade } from "@/lib/pagesNotYetMade";
import { itemsWithout } from "@/lib/rowsReplaced";

// Copies of database items drawn here at the press, the server still making
// them (rowCopiesHere.ts), until it answers:
// - what asks the server about one goes once the server has made it, not
//   before (pagesNotYetMade.ts): its page opened, its ⌘Z sending it to the
//   trash, its page sent to the trash from its menu;
// - a write of the page's graph that changes or deletes one goes once it is
//   made (afterCopiesMade): sent before, it reached the server ahead of the
//   copy;
// - the page read whole again meanwhile still shows it (withCopiesOnTheirWay),
//   unless a write here deleted it.

// Each copy on its way, as drawn; null once a write here deleted it.
const waiting = new Map<string, GraphRow | null>();
const listeners = new Set<() => void>();
const announce = () => { for (const listener of [...listeners]) listener(); };
tellPagesNotYetMade({
  has: (rowId) => waiting.has(rowId),
  ready: () => Promise.resolve(),
  subscribe(listener) {
    listeners.add(listener);
    return () => { listeners.delete(listener); };
  },
});

/** The copies are on their way, as drawn; what it gives says those the server made, or won't (all, by default). */
export function copiesOnTheirWay(rows: readonly GraphRow[]) {
  for (const row of rows) waiting.set(row.id, row);
  announce();
  return (answered: Iterable<string> = rows.map(({ id }) => id)) => {
    let changed = false;
    for (const rowId of answered) changed = waiting.delete(rowId) || changed;
    if (changed) announce();
  };
}

/** The graph with the copies on their way it doesn't hold, where it draws their databases. */
export function withCopiesOnTheirWay(graph: ContentGraph): ContentGraph {
  if (!waiting.size) return graph;
  const held = new Set(graph.rows.map(({ id }) => id));
  const back = [...waiting.values()].filter((row): row is GraphRow => row !== null && !held.has(row.id)
    && graph.dataSources.some((source) => source.id === row.dataSourceId));
  // Put in as a splice of the rows shown (lib/rowsReplaced.ts), so what the
  // views work out of their rows is worked out for the copies alone.
  return back.length ? { ...graph, rows: itemsWithout(graph.rows, () => false, back, (row, other) => compareGraphOrder(row, other) < 0) } : graph;
}

/**
 * Settles once the copies on their way the writes change or delete are made
 * (or refused), those deleted no longer drawn; null when they touch none, so
 * a write that waits for nothing goes in the same turn — what it shows next
 * (saveShownAtOnce) isn't yet the graph it is made against.
 */
export function afterCopiesMade(operations: readonly GraphOperation[]): Promise<void> | null {
  if (!waiting.size) return null;
  const rowIds = operations.flatMap((operation) => {
    if (operation.kind === "upsertRow") return [operation.row.id];
    if (operation.kind !== "deleteRow" || !operation.rowDeletion) return [];
    if (waiting.has(operation.rowDeletion.id)) waiting.set(operation.rowDeletion.id, null);
    return [operation.rowDeletion.id];
  }).filter((rowId) => waiting.has(rowId));
  if (!rowIds.length) return null;
  return new Promise((resolve) => {
    const made = () => {
      if (rowIds.some((rowId) => waiting.has(rowId))) return;
      listeners.delete(made);
      resolve();
    };
    listeners.add(made);
  });
}
