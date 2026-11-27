import { compareGraphOrder, type ContentGraph, type GraphBlock } from "@/contentGraph";

// Each graph's blocks by parent, in order, made once for its blocks (a
// graph's blocks are never changed in place once read; one pushed to while
// it is made is told by its length). Every block of a long page asked for its
// siblings, each time sorting the whole page's blocks (2026-10-16: a
// 3,000-block page spent half a second on it as it opened).
type ChildIndex = { length: number; byParent: Map<string | null, GraphBlock[]>; place: Map<string, number> };
const childIndexes = new WeakMap<readonly GraphBlock[], ChildIndex>();

function childIndex(graph: ContentGraph): ChildIndex {
  const known = childIndexes.get(graph.blocks);
  if (known && known.length === graph.blocks.length) return known;
  const byParent = new Map<string | null, GraphBlock[]>();
  for (const block of graph.blocks) {
    const siblings = byParent.get(block.parentId);
    if (siblings) siblings.push(block);
    else byParent.set(block.parentId, [block]);
  }
  const place = new Map<string, number>();
  for (const siblings of byParent.values()) {
    siblings.sort(compareGraphOrder);
    siblings.forEach((block, index) => place.set(block.id, index));
  }
  const index = { length: graph.blocks.length, byParent, place };
  childIndexes.set(graph.blocks, index);
  return index;
}

export function editorBlockChildren(graph: ContentGraph, parentId: string | null) {
  return [...(childIndex(graph).byParent.get(parentId) ?? [])];
}

/** The block after this one among its siblings, and its first child. */
export function editorBlockNeighbors(graph: ContentGraph, block: GraphBlock) {
  const index = childIndex(graph);
  const place = index.place.get(block.id);
  return {
    next: place === undefined ? undefined : index.byParent.get(block.parentId)?.[place + 1],
    firstChild: index.byParent.get(block.id)?.[0],
  };
}

export function editorDescendantIds(graph: ContentGraph, roots: readonly string[]) {
  const selected = new Set(roots);
  const queue = [...roots];
  while (queue.length) {
    const parentId = queue.shift();
    for (const child of graph.blocks) {
      if (child.parentId !== parentId || selected.has(child.id)) continue;
      selected.add(child.id);
      queue.push(child.id);
    }
  }
  return selected;
}

// The characters a place made here is written in (printable, no space: the
// server trims a place's spaces), and the most a place may run to.
const LOWEST = 0x21, HIGHEST = 0x7e, LONGEST_PLACE = 200;

/**
 * A place that sorts after `low` ("" for none) and before `high` (null for
 * none), code unit by code unit as places sort; null when none is short
 * enough. Places made one after another before the same block grow by one
 * character in every ninety or so.
 */
function placeBetween(low: string, high: string | null): string | null {
  let at = 0;
  while (high !== null && at < low.length && at < high.length && low.charCodeAt(at) === high.charCodeAt(at)) at += 1;
  if (at >= LONGEST_PLACE) return null;
  const below = at < low.length ? low.charCodeAt(at) : LOWEST - 1;
  const above = high === null ? HIGHEST + 1 : at < high.length ? high.charCodeAt(at) : -1;
  if (above <= below) return null;
  // Room at this character: just above the low one when nothing bounds it
  // above (places made one after another), else midway (room left on both
  // sides, for a block made again right after the same one).
  const from = Math.max(below, LOWEST - 1), to = Math.min(above, HIGHEST + 1);
  if (to - from >= 2) return low.slice(0, at) + String.fromCharCode(high === null && at < low.length ? from + 1 : Math.floor((from + to) / 2));
  // None: keep the low character and go on past the rest of the low place.
  if (at >= low.length || below < LOWEST - 1) return null;
  const rest = placeBetween(low.slice(at + 1), null);
  return rest === null ? null : low.slice(0, at + 1) + rest;
}

/**
 * A new block's place between the sibling before it and the one after it
 * (either missing), leaving theirs as they are; null when their places leave
 * none between them.
 */
export function newBlockPlace(before: GraphBlock | undefined, after: GraphBlock | undefined) {
  return newBlockPlaces(before, after, 1)?.[0] ?? null;
}

/**
 * Places for new blocks one after another between two siblings (either
 * missing), theirs left as they are; null when theirs leave no room for all.
 * A place made between a block and the one after it grows by a character in
 * every ninety or so, however often it is made again after the last one.
 */
export function newBlockPlaces(before: GraphBlock | undefined, after: GraphBlock | undefined, count: number) {
  if (before && after && before.position >= after.position) return null;
  const places: string[] = [];
  for (let low = before?.position ?? ""; places.length < count;) {
    const place = placeBetween(low, after?.position ?? null);
    if (place === null || place.length > LONGEST_PLACE) return null;
    places.push(place);
    low = place;
  }
  return places;
}

/** The place of a parent's `index`th block when its blocks take places anew, as a move gives them. */
export function siblingPlace(index: number, id: string) {
  return `a${String(index).padStart(8, "0")}:${id}`;
}

/** A parent's children in order, not to be changed. */
export function editorBlockChildrenNow(graph: ContentGraph, parentId: string | null): readonly GraphBlock[] {
  return childIndex(graph).byParent.get(parentId) ?? [];
}
