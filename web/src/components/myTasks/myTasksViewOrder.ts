import type { GraphView } from "@/contentGraph";
import { databaseToolbarConfigBounds } from "@/databaseToolbarConfigWire.generated";

/**
 * "내 작업"'s views keep their own order of its tasks, as a database's views
 * do (viewRowOrder.ts `rowOrder`) and as Notion's "내 작업" does (seen
 * 2026-11-24 in two tabs, its list and its board sorted by due date): a task
 * dragged among tasks of the same due date takes its new place and keeps it
 * after a reload, ⌘Z puts it back and ⌘⇧Z moves it again; one dragged where
 * the sort puts it elsewhere stays where the sort puts it, its place kept for
 * when the sort is taken off; the order is the view's own, the task's
 * database's views keeping theirs. The order the view shows is lent to the
 * screen alone: a view is saved with an order only once the screen gave it
 * one of its own (a task moved or made at a place).
 *
 * A view keeps the list of the tasks it placed (`rowOrder`) and shows them
 * first, in that order, and the tasks it never placed after them, newest
 * first — those made at one time by their ids — whatever their due dates, as
 * Notion's "내 작업" (seen 2026-11-24 in Chrome, a list in one tab and a board
 * in another, its kept order read after each step): a drop writes down every
 * task the view shows in the group dropped in, in the order shown, the ones
 * never moved there too (myTasksDropGroup.ts), together where the first of
 * them stood in the list, else at its end — another group's tasks stay as
 * they were; a task placed keeps its place when its due date changes, or
 * when it moves to another group, among the tasks placed there; a task made
 * or brought in since (assigned, its status changed back, its source added)
 * comes after the tasks placed in its group, before older ones never placed;
 * a sort comes first, its ties in this order. Until then (2026-11-24) a view
 * kept the runs of tasks moved (`rowPlaces`) and the others stood by due
 * date, so a task never moved, or new, went where its due date put it; such
 * a view shows as it did, its runs among the tasks by due date, each of them
 * placed from then on. What a view keeps grows with the tasks shown in the
 * groups dropped in, apart from its settings' 16 KiB, all views within the
 * request's bound: past it an order is refused and the person told, never
 * cut (fitMyTaskViews).
 */

type Config = GraphView["config"];
/** Tasks placed together, as kept before (2026-11-24): the task they stand after, the one they stand before ("" for the start, the end), then them. */
type Run = readonly string[];
/** What a view keeps of its order: the tasks it placed, the runs kept before them, or none. */
export type MyTaskViewOrder = { rowOrder?: unknown; rowPlaces?: unknown };
/** The tasks a view's drop writes down, given the tasks it moved or made: its group's (myTasksDropGroup.ts), all shown without one. */
export type MyTasksDropGroup = (view: GraphView, moved: readonly string[]) => ReadonlySet<string> | undefined;

const isRun = (value: unknown): value is Run => Array.isArray(value) && value.length > 2 && value.every((id) => typeof id === "string");
const runsOf = (config: Config | undefined): Run[] | undefined => {
  const places = config?.rowPlaces;
  return Array.isArray(places) ? (places as unknown[]).filter(isRun) : undefined;
};

// The tasks as the server reads them (by due date, then name) for each list of them newest first: runs kept before stand among them.
const asRead = new WeakMap<readonly string[], readonly string[]>();

/** The tasks' ids newest first, those made at one time by their ids, as Notion's views place the rows they never placed (viewRowOrder.ts). */
export function taskIdsNewestFirst(tasks: readonly { rowId: string; created?: number }[]): string[] {
  const ids = [...tasks].sort((left, right) => (right.created ?? 0) - (left.created ?? 0)
    || (left.rowId < right.rowId ? -1 : left.rowId > right.rowId ? 1 : 0)).map(({ rowId }) => rowId);
  asRead.set(ids, tasks.map(({ rowId }) => rowId));
  return ids;
}

/** The tasks (`taskIds`) with runs kept before placed among them, each after the task it was dropped after. */
function withRuns(runs: readonly Run[], taskIds: readonly string[]): string[] {
  const moved = new Set(runs.flatMap((run) => run.slice(2)));
  const read = taskIds.filter((id) => !moved.has(id)), standing = new Set(read);
  const rank = new Map(taskIds.map((id, index) => [id, index]));
  const after = new Map<string, string[]>(), before = new Map<string, string[]>();
  const put = (at: Map<string, string[]>, key: string, ids: readonly string[]) => { const list = at.get(key); if (list) list.push(...ids); else at.set(key, [...ids]); };
  for (const [first, next, ...ids] of runs) {
    if (first === "" || standing.has(first!)) put(after, first!, ids);
    else if (next === "" || standing.has(next!)) put(before, next!, ids);
    else {
      const at = rank.get(ids[0]!);
      put(before, at === undefined ? "" : read.find((id) => rank.get(id)! > at) ?? "", ids);
    }
  }
  // "" is the start among the runs placed after a task, the end among those placed before one.
  const order = [...(after.get("") ?? [])];
  for (const id of read) order.push(...(before.get(id) ?? []), id, ...(after.get(id) ?? []));
  order.push(...(before.get("") ?? []));
  return [...new Set(order)];
}

/** The tasks a kept view placed, in its order (`taskIds`: the tasks as read, newest first) — those no longer read too. */
function placedOf(config: Config | undefined, taskIds: readonly string[]): string[] {
  const runs = runsOf(config);
  if (runs) return withRuns(runs, asRead.get(taskIds) ?? taskIds);
  const saved = config?.rowOrder;
  return Array.isArray(saved) ? [...new Set(saved.map(String))] : [];
}

/** Every task of the view in its order: those it placed, then the others as read (`taskIds`, newest first). */
function orderOfTasks(config: Config | undefined, taskIds: readonly string[]): string[] {
  const placed = placedOf(config, taskIds), placedIds = new Set(placed);
  return [...placed, ...taskIds.filter((id) => !placedIds.has(id))];
}

/** The longest list of `ids` in the order they stood (`rank`), by patience sorting. */
function longestInOrder(ids: readonly string[], rank: ReadonlyMap<string, number>): Set<string> {
  const tails: number[] = [], previous = new Array<number>(ids.length).fill(-1);
  ids.forEach((id, index) => {
    const at = rank.get(id);
    if (at === undefined) return;
    let low = 0, high = tails.length;
    while (low < high) {
      const middle = (low + high) >> 1;
      if (rank.get(ids[tails[middle]!]!)! < at) low = middle + 1; else high = middle;
    }
    if (low > 0) previous[index] = tails[low - 1]!;
    tails[low] = index;
  });
  const kept = new Set<string>();
  for (let index = tails.at(-1) ?? -1; index >= 0; index = previous[index]!) kept.add(ids[index]!);
  return kept;
}

/** The tasks an order the screen gave (`shown`) moved or made against the one it showed (`before`): those out of their order, or new. */
function movedIn(before: readonly string[], shown: readonly string[]): string[] {
  const stayed = longestInOrder(shown, new Map(before.map((id, index) => [id, index])));
  return shown.filter((id) => !stayed.has(id));
}

/**
 * The tasks placed once a drop wrote down `group` (all of `shown` without
 * one) in the order shown: together where the first of them stood among
 * those placed, else after them all; the others keep their places.
 */
function placedWith(placed: readonly string[], shown: readonly string[], group: ReadonlySet<string> | undefined): string[] {
  const written = group ? shown.filter((id) => group.has(id)) : [...shown], writtenIds = new Set(written);
  const rest = placed.filter((id) => !writtenIds.has(id)), first = placed.findIndex((id) => writtenIds.has(id));
  const at = first < 0 ? rest.length : placed.slice(0, first).filter((id) => !writtenIds.has(id)).length;
  return [...rest.slice(0, at), ...written, ...rest.slice(at)];
}

const lent = new WeakSet<readonly unknown[]>();
// Orders kept as they are when saved again: one to put back by (⌘Z, ⌘⇧Z).
const given = new WeakSet<readonly unknown[]>();
const asGiven = (order: MyTaskViewOrder): MyTaskViewOrder => { if (Array.isArray(order.rowOrder)) given.add(order.rowOrder); return order; };
// The view last shown for each view kept, the same while its order is: tasks read again or changed in place draw no other view.
const lastShown = new WeakMap<GraphView, GraphView>();

/** The view as the screen shows it: every task in its own order (`taskIds`: the tasks as read, newest first). */
export function withOwnRowOrder(view: GraphView, taskIds: readonly string[]): GraphView {
  const rowOrder = orderOfTasks(view.config, taskIds);
  const last = lastShown.get(view), lastOrder = last?.config.rowOrder as readonly unknown[] | undefined;
  if (last && lastOrder?.length === rowOrder.length && rowOrder.every((id, index) => lastOrder[index] === id)) return last;
  lent.add(rowOrder);
  const { rowPlaces: _kept, ...config } = view.config;
  const shown = { ...view, config: { ...config, rowOrder } };
  lastShown.set(view, shown);
  return shown;
}

/** What a kept view (`config`, not one lent to the screen) keeps of its order. */
export function orderKept(config: Config | undefined): MyTaskViewOrder {
  const places = config?.rowPlaces, order = config?.rowOrder;
  return Array.isArray(places) ? { rowPlaces: places } : Array.isArray(order) ? asGiven({ rowOrder: order }) : {};
}

/** A kept view's order as the tasks it placed, to put back by (⌘Z): runs kept before read as they show now. */
export function orderAsList(config: Config | undefined, taskIds: readonly string[]): MyTaskViewOrder {
  return runsOf(config) ? asGiven({ rowOrder: placedOf(config, taskIds) }) : orderKept(config);
}

/** The settings with the order given in place of theirs. */
export function withOrder(config: Config, order: MyTaskViewOrder): Config {
  const { rowOrder: _order, rowPlaces: _places, ...rest } = config;
  return { ...rest, ...order };
}

/**
 * A view to save: an order the screen only showed goes back to what was
 * saved (`saved`, the view as last kept), one kept or put back stays; one it
 * gave — a task moved or made at a place (`moved`, else read off the order)
 * — writes down the tasks of the group dropped in (`groupOf`) among those
 * placed (`taskIds`: the tasks as read, newest first).
 */
export function ownOrderKept(view: GraphView, saved: GraphView | undefined, taskIds: readonly string[],
  groupOf?: MyTasksDropGroup, moved?: readonly string[]): GraphView {
  const order = view.config.rowOrder;
  if (!Array.isArray(order)) return view;
  if (lent.has(order)) return { ...view, config: withOrder(view.config, orderKept(saved?.config)) };
  if (given.has(order) || order === saved?.config.rowOrder) return view;
  const shown = [...new Set(order.map(String))];
  const placing = moved ?? movedIn(orderOfTasks(saved?.config, taskIds), shown);
  return { ...view, config: withOrder(view.config, { rowOrder: placedWith(placedOf(saved?.config, taskIds), shown, groupOf?.(view, placing)) }) };
}

const encoder = new TextEncoder();
const bytes = (value: unknown) => encoder.encode(JSON.stringify(value)).length;
const sameOrder = (left: GraphView, right: GraphView | undefined) => JSON.stringify(orderKept(left.config)) === JSON.stringify(orderKept(right?.config));

/**
 * The views as the server takes them: a view's settings within
 * myTaskViewMaxBytes, its order apart from them, and all of them within
 * myTaskViewsMaxBytes. Views over that first let go of the places of tasks
 * no longer read (`taskIds`); still over, those whose order changed keep the
 * order they had (`saved`) and `refused` says so, for the person to be told —
 * no order is cut short.
 */
export function fitMyTaskViews(views: readonly GraphView[], saved: readonly GraphView[], taskIds: readonly string[]) {
  const { myTaskViewsMaxBytes } = databaseToolbarConfigBounds;
  const fits = (list: readonly GraphView[]) => bytes({ views: list }) <= myTaskViewsMaxBytes;
  if (fits(views)) return { views: [...views], refused: false };
  const read = new Set(taskIds);
  const trimmed = views.map((view) => {
    const runs = runsOf(view.config), order = view.config.rowOrder;
    if (runs) return { ...view, config: withOrder(view.config, {
      rowPlaces: runs.map((run) => run.filter((id, index) => index < 2 || read.has(id))).filter((run) => run.length > 2) }) };
    return Array.isArray(order) ? { ...view, config: withOrder(view.config, { rowOrder: order.filter((id) => read.has(String(id))) }) } : view;
  });
  if (fits(trimmed)) return { views: trimmed, refused: false };
  const was = (view: GraphView) => saved.find(({ id }) => id === view.id);
  return { views: trimmed.map((view) => sameOrder(view, was(view)) ? view : { ...view, config: withOrder(view.config, orderKept(was(view)?.config)) }), refused: true };
}
