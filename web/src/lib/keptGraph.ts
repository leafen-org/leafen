import type { ContentGraph } from "@/contentGraph";
import { itemsReplacedFrom, noteItemsReplaced, replaceItems } from "@/lib/rowsReplaced";

const listKeys = ["blocks", "databaseContainers", "dataSources", "views", "properties", "rows"] as const;

// The graph shown for each graph given that was shown as another object.
const shownFor = new WeakMap<ContentGraph, ContentGraph>();

/** The graph shown for `graph` once given to the page's graph (a read, as kept). */
export function graphAsShown(graph: ContentGraph) {
  return shownFor.get(graph) ?? graph;
}

/**
 * `next`, with what it holds equal to `shown` taken from `shown`: each block,
 * view, property and row equal to the one shown stays that object, and a list
 * with nothing changed stays that list. A graph read again comes as all new
 * objects, so everything a table works out from its rows (their pages, their
 * order, their filters) was worked out again for every row whenever one
 * changed, 1.8–2.3 seconds of the main thread on a table of 100,000 rows
 * (2026-11-11); the work kept for each row now stays.
 */
export function keepUnchangedGraph(shown: ContentGraph, next: ContentGraph): ContentGraph {
  if (shown === next) return next;
  let fromShown = true;
  let fromNext = true;
  const kept = { ...next };
  for (const key of listKeys) {
    const list = keepUnchangedItems(shown[key] as readonly Identified[], next[key] as readonly Identified[]);
    (kept as Record<string, unknown>)[key] = list;
    if (list !== shown[key]) fromShown = false;
    if (list !== next[key]) fromNext = false;
  }
  const result = fromShown && sameOutsideLists(shown, next) ? shown : fromNext ? next : kept;
  if (result !== next) shownFor.set(next, result);
  return result;
}

type Identified = { id: string };

function keepUnchangedItems<T extends Identified>(shown: readonly T[], next: readonly T[]): readonly T[] {
  if (shown === next || shown.length === 0) return next;
  // Made from `shown` by replacing items in place: those alone are compared.
  const made = itemsReplacedFrom(next);
  if (made?.from === shown) {
    const changed = made.at.filter((index) => !sameValue(shown[index], next[index]));
    if (changed.length === made.at.length) return next;
    return changed.length === 0 ? shown : replaceItems(shown, new Map(changed.map((index) => [index, next[index]])));
  }
  let byId: Map<string, T> | null = null;
  let allShown = shown.length === next.length;
  let anyShown = false;
  // A list that keeps no item of `shown` it doesn't hold already stays
  // itself, and so stays made from `shown` (lib/rowsReplaced.ts).
  let kept: T[] | null = null;
  // The places where what is kept isn't what was shown there.
  const changed: number[] = [];
  next.forEach((item, index) => {
    // Mostly the same items in the same places; by ID when they moved.
    let before: T | undefined = shown[index];
    if (before?.id !== item.id) before = (byId ??= new Map(shown.map((candidate) => [candidate.id, candidate]))).get(item.id);
    const value = before !== undefined && sameValue(before, item) ? before : item;
    if (value === before) anyShown = true;
    if (shown[index] !== value) {
      allShown = false;
      changed.push(index);
    }
    if (value !== item) kept ??= next.slice(0, index);
    kept?.push(value);
  });
  if (allShown) return shown;
  if (!anyShown) return next;
  // A list made by mapping the one shown (a value changed here, a row's
  // last edit) holds its own changed items alone: it is kept as made from
  // `shown` too, so what is worked out from it is worked out for those items
  // alone (2026-11-15: a card's checkbox made every card's page again, and
  // a gallery drew all 34 cards).
  noteItemsReplaced<T>(kept ?? next, shown, changed);
  return kept ?? next;
}

function sameOutsideLists(shown: ContentGraph, next: ContentGraph) {
  const keys = new Set([...Object.keys(shown), ...Object.keys(next)]);
  for (const key of keys) {
    if ((listKeys as readonly string[]).includes(key)) continue;
    if (!sameValue((shown as Record<string, unknown>)[key], (next as Record<string, unknown>)[key])) return false;
  }
  return true;
}

/** Whether two values read from JSON hold the same. */
function sameValue(left: unknown, right: unknown): boolean {
  if (left === right) return true;
  if (typeof left !== "object" || typeof right !== "object" || left === null || right === null) return false;
  if (Array.isArray(left) || Array.isArray(right)) {
    if (!Array.isArray(left) || !Array.isArray(right) || left.length !== right.length) return false;
    for (let index = 0; index < left.length; index += 1) if (!sameValue(left[index], right[index])) return false;
    return true;
  }
  // Plain objects as read, whose keys are all their own.
  const leftRecord = left as Record<string, unknown>;
  const rightRecord = right as Record<string, unknown>;
  let count = 0;
  for (const key in leftRecord) {
    count += 1;
    const value = rightRecord[key];
    if (value === undefined ? !(key in rightRecord) || leftRecord[key] !== undefined : !sameValue(leftRecord[key], value)) return false;
  }
  for (const _key in rightRecord) count -= 1;
  return count === 0;
}
