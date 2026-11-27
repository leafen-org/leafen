import type { Dispatch, SetStateAction } from "react";
import type { ContentGraph, GraphOperation, GraphRow, GraphRowValue, GraphView } from "@/contentGraph";
import { i18next } from "@/i18n";
import { newId } from "@/lib/ids";
import type { ProductStatusSink } from "@/lib/productStatus";
import { saveRowsShownAtOnce, saveShownAtOnce } from "@/lib/nativeGraphAuthority";
import { rowOrderPlacing } from "@/components/database/viewRowOrder";
import { chooseAsAtUndo, chooseStepRows, keepChoiceForRedo } from "@/lib/stepRowChoice";

type RowPlace = { position: GraphRow["position"]; values: GraphRow["values"] };

/**
 * Rows moved in a view — a table's, list's or gallery's rows dragged, a
 * board's card moved in its group or to another — as one step of the page's
 * ⌘Z: ⌘Z puts them back in their place and group, ⌘⇧Z moves them again, as
 * Notion's (seen 2026-11-11: a card moved within its group and then to
 * another, and a table's row, each taken back and done again a step at a
 * time). It holds each moved row's place and the values the move set, and the
 * order of a view keeping its own (viewRowOrder.ts), before and after.
 */
export type RowMoveStep = {
  key: string;
  rows: ReadonlyArray<{ id: string; before: RowPlace; after: RowPlace }>;
  view?: ViewOrderStep;
};

/** The order of a view keeping its own, before and after a step placed `rowIds` in it (a move, or copies made: nativeTableRowSteps.ts). */
export type ViewOrderStep = { id: string; rowIds: readonly string[]; before: readonly string[]; after: readonly string[] };

/**
 * A view kept outside the page's graph — a page layout's tab, kept by the
 * layout (PageLayoutTabView) — read and written through its keeper while it
 * shows, so the order a move or a copy writes in it joins the same ⌘Z step
 * as its rows, as a view of the graph's does (Notion's views, seen
 * 2026-11-24: one ⌘Z took a copy and the order it wrote).
 */
export type ViewKeeper = {
  read(): GraphView;
  /** The rows it shows before its filters. */
  rows(): readonly GraphRow[];
  save(view: GraphView): Promise<unknown>;
};
const keepers = new Map<string, ViewKeeper>();

/** Keeps view `id` outside the graph while it shows; what it gives lets it go. */
export function keepViewOutsideGraph(id: string, keeper: ViewKeeper): () => void {
  keepers.set(id, keeper);
  return () => { if (keepers.get(id) === keeper) keepers.delete(id); };
}

/** The view as it shows, its keeper's or the graph's. */
export function viewShown(graph: ContentGraph, id: string | undefined): GraphView | undefined {
  const keeper = id === undefined ? undefined : keepers.get(id);
  return keeper ? keeper.read() : graph.views.find((view) => view.id === id);
}

/** The rows a view kept outside the graph shows before its filters; none for the graph's own. */
export const keptViewRows = (id: string): readonly GraphRow[] | undefined => keepers.get(id)?.rows();

/** Writes a view's change, shown at once: through its keeper, or to the page's graph. */
export function saveViewShown(at: Omit<RowMoveWrites, "editedBy">, view: GraphView): Promise<boolean> {
  const keeper = keepers.get(view.id);
  if (keeper) return keeper.save(view).then(() => true, () => false);
  return saveShownAtOnce(at.graph, at.pageId, [{ kind: "upsertView", view }], at.persist, () => true, () => at.setGraph((current) => ({
    ...current, views: current.views.map((candidate) => candidate.id === view.id ? view : candidate),
  })));
}

const viewOrder = (view: GraphView | undefined) => Array.isArray(view?.config.rowOrder) ? (view.config.rowOrder as unknown[]).map(String) : undefined;
const sameValue = (left: GraphRowValue | undefined, right: GraphRowValue | undefined) =>
  JSON.stringify(left?.value ?? null) === JSON.stringify(right?.value ?? null);

/**
 * The step of rows going to `moved` — each with the values the move sets
 * alone — and of the view to `view`, from the graph as it shows them.
 */
export function rowMoveStep(graph: ContentGraph, moved: readonly GraphRow[], view?: GraphView, rowIds: readonly string[] = moved.map(({ id }) => id)): RowMoveStep {
  const rows = moved.flatMap((next) => {
    const shown = graph.rows.find(({ id }) => id === next.id);
    if (!shown) return [];
    const before = Object.fromEntries(Object.entries(next.values).map(([id, value]) => [id, shown.values[id] ?? { type: value.type, value: null }]));
    const changed = shown.position !== next.position || Object.entries(next.values).some(([id, value]) => !sameValue(before[id], value));
    return changed ? [{ id: next.id, before: { position: shown.position, values: before }, after: { position: next.position, values: next.values } }] : [];
  });
  const before = viewOrder(viewShown(graph, view?.id)), after = viewOrder(view);
  const ordered = view && before && after && JSON.stringify(before) !== JSON.stringify(after);
  return { key: `row-move/${newId()}`, rows, ...(ordered ? { view: { id: view.id, rowIds, before, after } } : {}) };
}

/** The moved rows put where `target` has them, beside the rows they stand by there, in an order changed since. */
function orderPlacingAsIn(current: readonly string[], target: readonly string[], rowIds: readonly string[]) {
  const moving = new Set(rowIds);
  let next = current.filter((id) => !moving.has(id));
  for (const id of target.filter((candidate) => moving.has(candidate))) {
    const at = target.indexOf(id), placed = new Set(next);
    const before = target.slice(0, at).reverse().find((other) => placed.has(other));
    const after = before === undefined ? target.slice(at + 1).find((other) => placed.has(other)) : undefined;
    next = rowOrderPlacing(next, [id], before !== undefined ? { id: before, side: "after" } : after !== undefined ? { id: after, side: "before" } : null);
  }
  return next;
}

/**
 * What taking a step back (forward false) or doing it again writes: its rows
 * and view as the step leaves them. A row moved again or given another value
 * since, here or by someone else, keeps it — null, and nothing is written. A
 * view's order changed since by other moves keeps them, the step's rows going
 * back beside the rows they stood by.
 */
export function rowMoveStepWrites(graph: ContentGraph, step: RowMoveStep, forward: boolean): { rows: GraphRow[]; view?: GraphView } | null {
  const rows: GraphRow[] = [];
  for (const moved of step.rows) {
    const row = graph.rows.find(({ id }) => id === moved.id);
    const from = forward ? moved.before : moved.after, to = forward ? moved.after : moved.before;
    if (!row || row.position !== from.position || Object.entries(from.values).some(([id, value]) => !sameValue(row.values[id], value))) return null;
    rows.push({ ...row, position: to.position, values: to.values });
  }
  if (!step.view) return { rows };
  const view = viewOrderStepWrite(graph, step.view, forward);
  return view ? { rows, view } : null;
}

/**
 * The view as a step's order leaves it, taken back (forward false) or done
 * again; null when the view is gone or keeps no order of its own. An order
 * changed since by other moves keeps them, the step's rows going back beside
 * the rows they stood by — rows the order before had no place for (copies)
 * leaving it.
 */
export function viewOrderStepWrite(graph: ContentGraph, step: ViewOrderStep, forward: boolean): GraphView | null {
  const view = viewShown(graph, step.id), current = viewOrder(view);
  if (!view || !current) return null;
  const from = forward ? step.before : step.after, to = forward ? step.after : step.before;
  const rowOrder = JSON.stringify(current) === JSON.stringify(from) ? [...to] : orderPlacingAsIn(current, to, step.rowIds);
  return { ...view, config: { ...view.config, rowOrder } };
}

/** Where a move is written: the page's graph and the writes its adapter makes. */
export type RowMoveWrites = {
  graph: ContentGraph; pageId: string | null; persist(operations: GraphOperation[]): Promise<boolean>;
  setGraph: Dispatch<SetStateAction<ContentGraph>>; editedBy?: string;
};

/**
 * Writes a move — the view's order, then its rows' values and places — all
 * shown at once (nativeGraphAuthority.ts), so a card dropped at a group's end
 * is drawn there from the drop on and stays there; false when a write is
 * refused. (2026-11-12: the order went out only once the rows' write had
 * answered while the move's view was still the one shown — by then the
 * board, drawn again with the card in its new group, was another — so a
 * board keeping its own order put the card where that order put it, newest
 * first: a group's top.)
 */
export async function saveRowMove(at: RowMoveWrites, rows: readonly GraphRow[], view: GraphView | undefined, isCurrent: () => boolean) {
  const { graph, pageId, persist, setGraph, editedBy } = at;
  const viewSaved = !view || saveViewShown(at, view);
  const rowsSaved = !rows.length || saveRowsShownAtOnce(graph, pageId, rows, persist, () => true, setGraph, editedBy);
  const [viewWritten, rowsWritten] = await Promise.all([viewSaved, rowsSaved]);
  return viewWritten && rowsWritten && isCurrent();
}

/**
 * Takes a move back (forward false) or makes it again; one changed since
 * stays, and the view says so. Taken back, its rows are chosen in their
 * places wherever the focus is, as Notion's (seen 2026-11-24: a row moved,
 * a press on the empty room below the table, ⌘Z — the row back and
 * chosen); made again, what was chosen at that ⌘Z comes back (stepRowChoice.ts).
 */
export async function stepRowMove(at: RowMoveWrites, step: RowMoveStep, forward: boolean, setMessage: ProductStatusSink) {
  const writes = rowMoveStepWrites(at.graph, step, forward);
  if (!writes) {
    setMessage(i18next.t("databaseView:status.undoChanged"), "default");
    return true;
  }
  if (forward) chooseAsAtUndo(step);
  else {
    keepChoiceForRedo(step);
    chooseStepRows([...new Set([...step.view?.rowIds ?? [], ...step.rows.map(({ id }) => id)])]);
  }
  return saveRowMove(at, writes.rows, writes.view, () => true);
}
