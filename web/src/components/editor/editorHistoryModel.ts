import { compareGraphOrder, type ContentGraph, type GraphBlock, type GraphOperation, type GraphView } from "@/contentGraph";
import { ownViewsOf, viewsToMakeAgain, withoutViewsOfGone } from "./blockViewCopies";
import { moveEditorBlock } from "./editorGraphModel";
import { applyEditorContentDelta, editorContentEqual } from "./editorContentDelta";

type Place = { parentId: string | null; afterId: string | null; siblings: readonly string[] };
type BlockEdit = {
  id: string; before: GraphBlock | null; after: GraphBlock | null;
  beforePlace: Place; afterPlace: Place; moved: boolean;
  // The views a block removed or made owns on the side it stands, which come
  // back with it (ownViewsOf).
  beforeViews?: GraphView[]; afterViews?: GraphView[];
};
type EditorGraphEdit = { blocks: BlockEdit[] };

export type EditorHistory = {
  past: EditorGraphEdit[];
  present: ContentGraph;
  future: EditorGraphEdit[];
  publication: GraphOperation[];
};

export function createEditorHistory(graph: ContentGraph): EditorHistory {
  return { past: [], present: graph, future: [], publication: [] };
}

const same = editorContentEqual;

function places(graph: ContentGraph) {
  const siblings = new Map<string | null, GraphBlock[]>();
  for (const block of graph.blocks) {
    const group = siblings.get(block.parentId) ?? [];
    group.push(block);
    siblings.set(block.parentId, group);
  }
  const result = new Map<string, Place>();
  for (const group of siblings.values()) {
    group.sort(compareGraphOrder);
    const ids = group.map((block) => block.id);
    group.forEach((block, index) => result.set(block.id, { parentId: block.parentId, afterId: group[index - 1]?.id ?? null, siblings: ids }));
  }
  return result;
}

export function recordEditorHistory(
  history: EditorHistory, next: ContentGraph, limit = 100, combine = false, operations?: readonly GraphOperation[],
): EditorHistory {
  const before = new Map(history.present.blocks.map((block) => [block.id, block]));
  const after = new Map(next.blocks.map((block) => [block.id, block]));
  const beforePlaces = places(history.present), afterPlaces = places(next);
  const moved = new Set(operations?.flatMap((op) => op.kind === "moveBlock" ? [op.move.blockId] : []) ?? []);
  // Optimistic insertion/move rebalances every sibling's display position.
  // The operation list, rather than those incidental positions, owns the edit.
  const touched = operations ? new Set(operations.flatMap((op) => op.kind === "upsertBlock" ? [op.block.id]
    : op.kind === "moveBlock" ? [op.move.blockId] : op.kind === "deleteBlock" ? [op.deletion.blockId] : []))
    : new Set([...before.keys(), ...after.keys()]);
  const blocks: BlockEdit[] = [];
  const databases = (side: Map<string, GraphBlock>, other: Map<string, GraphBlock>) =>
    new Set([...touched].filter((id) => side.get(id)?.type === "inline_database" && !other.has(id)));
  const removedViews = ownViewsOf(history.present, databases(before, after)), madeViews = ownViewsOf(next, databases(after, before));
  for (const id of touched) {
    if (same(before.get(id), after.get(id))) continue;
    const first = before.get(id) ?? null, last = after.get(id) ?? null;
    blocks.push({ id, before: first, after: last,
      beforePlace: beforePlaces.get(id) ?? { parentId: null, afterId: null, siblings: [] },
      afterPlace: afterPlaces.get(id) ?? { parentId: null, afterId: null, siblings: [] },
      moved: moved.has(id) || !!first && !!last && first.parentId !== last.parentId,
      beforeViews: removedViews.get(id), afterViews: madeViews.get(id) });
  }
  if (!blocks.length) return { ...history, present: next };
  let past = history.past;
  const previous = combine ? past.at(-1) : undefined;
  if (previous) {
    const merged = new Map(previous.blocks.map((edit) => [edit.id, edit]));
    for (const edit of blocks) {
      const first = merged.get(edit.id);
      merged.set(edit.id, first ? { ...edit, before: first.before, beforePlace: first.beforePlace, beforeViews: first.beforeViews,
        moved: first.moved || edit.moved } : edit);
    }
    past = [...past.slice(0, -1), { blocks: [...merged.values()] }];
  } else past = [...past, { blocks }];
  return { past: past.slice(-limit), present: next, future: [], publication: [] };
}

// Apply a local delta to the current graph. Foreign blocks, database state
// and content keys changed by a peer stay current. Publish just this delta.
function applyEdit(graph: ContentGraph, edit: EditorGraphEdit, forward: boolean, keep: ReadonlySet<string>) {
  const blocks = new Map(graph.blocks.map((block) => [block.id, block]));
  const originalIds = new Set(blocks.keys());
  const removals = new Set(edit.blocks.filter((change) => !(forward ? change.after : change.before)).map(({ id }) => id));
  const protectedParents = new Set<string>(keep);
  for (const change of edit.blocks) {
    if (!removals.has(change.id)) continue;
    const expected = forward ? change.before : change.after, current = blocks.get(change.id);
    if (expected && current && (current.type !== expected.type || current.parentId !== expected.parentId
      || !same(current.content, expected.content))) protectedParents.add(change.id);
  }
  // A block this edit puts back elsewhere doesn't keep the parent it leaves
  // (2026-11-04: Undo of a drop that made columns left them empty).
  const leaving = new Set(edit.blocks.filter((change) => {
    const target = forward ? change.after : change.before;
    return target && blocks.has(change.id) && target.parentId !== blocks.get(change.id)!.parentId;
  }).map(({ id }) => id));
  for (const block of graph.blocks) {
    if (leaving.has(block.id) || removals.has(block.id) && !protectedParents.has(block.id)) continue;
    let parent = block.parentId;
    while (parent && removals.has(parent) && !protectedParents.has(parent)) {
      protectedParents.add(parent);
      parent = blocks.get(parent)?.parentId ?? null;
    }
  }
  const publication: GraphOperation[] = [];
  const madeViews: GraphView[] = [];
  const placements: { id: string; place: Place }[] = [];
  const currentPlaces = places(graph);
  const restoredIds = new Set(edit.blocks.filter((change) => (forward ? change.after : change.before)
    && !(forward ? change.before : change.after) && !blocks.has(change.id)).map(({ id }) => id));
  for (const change of edit.blocks) {
    const target = forward ? change.after : change.before;
    const expected = forward ? change.before : change.after;
    const place = forward ? change.afterPlace : change.beforePlace;
    const current = blocks.get(change.id);
    if (!target) {
      if (current && !protectedParents.has(change.id)) {
        blocks.delete(change.id);
        publication.push({ kind: "deleteBlock", deletion: { blockId: change.id } });
      }
      continue;
    }
    // Do not put a restored block under a parent a peer has removed.
    if (place.parentId && !blocks.has(place.parentId) && !restoredIds.has(place.parentId)) continue;
    if (!expected) {
      if (!current) {
        blocks.set(change.id, { ...target, parentId: place.parentId, afterId: place.afterId });
        placements.push({ id: change.id, place });
        publication.push({ kind: "upsertBlock", block: { ...target, parentId: place.parentId, afterId: place.afterId } });
        madeViews.push(...viewsToMakeAgain(graph, (forward ? change.afterViews : change.beforeViews) ?? []));
      }
      continue;
    }
    if (!current) continue;
    const restored = { ...current, content: applyEditorContentDelta(current.content, expected.content, target.content, true) };
    if (!same(target.type, expected.type) && same(current.type, expected.type)) restored.type = target.type;
    if (!same(current, restored)) {
      blocks.set(change.id, restored);
      publication.push({ kind: "upsertBlock", block: { ...restored, afterId: null,
        ...(expected.type === "child_page" && restored.type !== "child_page" ? { restorePageConversion: true } : {}),
      } });
    }
    if (change.moved && current.parentId === expected.parentId) {
      const expectedPlace = forward ? change.beforePlace : change.afterPlace;
      const known = new Set(expectedPlace.siblings);
      const currentSiblings = currentPlaces.get(change.id)?.siblings ?? [];
      const before = currentSiblings.slice(0, currentSiblings.indexOf(change.id));
      const anchor = before.reverse().find((id) => known.has(id)) ?? null;
      // New peer siblings do not invalidate our move, but a peer moving this
      // block relative to the known siblings owns its new location.
      if (anchor === expectedPlace.afterId) placements.push({ id: change.id, place });
    }
  }
  // The views of blocks this step removes go with them, those of blocks it
  // brings back come back with them (ownViewsOf).
  const gone = new Set(originalIds); for (const id of blocks.keys()) gone.delete(id);
  const kept = withoutViewsOfGone(graph, gone);
  let present = { ...kept, blocks: [...blocks.values()], views: madeViews.length ? [...kept.views, ...madeViews] : kept.views };
  const displacedFirst = new Map<string, string | null>();
  // A group's blocks go in their recorded order, so each one's anchor is in
  // place first (2026-11-03: undoing a merge put the line after the merged
  // one after it before it was placed, and two lines swapped on screen while
  // the server kept them right).
  const groups = new Map<string | null, { id: string; place: Place }[]>();
  for (const placement of placements) groups.set(placement.place.parentId, [...groups.get(placement.place.parentId) ?? [], placement]);
  const ordered = [...groups.values()].flatMap((group) => group.sort((left, right) =>
    left.place.siblings.indexOf(left.id) - right.place.siblings.indexOf(right.id)));
  for (const { id, place } of ordered) {
    if (place.parentId && !present.blocks.some((block) => block.id === place.parentId)) continue;
    const byId = new Map(present.blocks.map((block) => [block.id, block]));
    const preceding = place.siblings.slice(0, place.siblings.indexOf(id));
    const anchor = preceding.reverse().find((candidate) => byId.get(candidate)?.parentId === place.parentId) ?? null;
    const moved = moveEditorBlock(present, id, place.parentId, anchor);
    if (!moved) continue;
    present = moved.graph;
    // Creation's upsert already puts the new block after this anchor.
    const creation = publication.findIndex((op) => op.kind === "upsertBlock" && op.block.id === id && !originalIds.has(id));
    if (creation >= 0) {
      const block = present.blocks.find((block) => block.id === id)!;
      const first = present.blocks.filter((candidate) => originalIds.has(candidate.id)
        && candidate.parentId === place.parentId).sort(compareGraphOrder)[0]?.id;
      // A batch can anchor creation to a new block but cannot move a newly
      // created target. To lead an existing group, use ordinary insertion's
      // policy: make it after the old first block, then move that block.
      if (anchor === null && first && first !== id) {
        publication[creation] = { kind: "upsertBlock", block: { ...block, afterId: first } };
        displacedFirst.set(first, place.parentId);
      } else publication[creation] = { kind: "upsertBlock", block };
    } else publication.push(...moved.operations);
  }
  // A leading creation can be a whole group. Move the old first sibling
  // after that group's final block right once that block is made, before
  // any later creation anchored after the old first one: until then the
  // server has the group after it, and such a creation went before the
  // group (2026-10-25: two restored blocks came back in the wrong order).
  for (const [first, parentId] of displacedFirst) {
    const siblings = present.blocks.filter(block => block.parentId === parentId).sort(compareGraphOrder);
    const index = siblings.findIndex(block => block.id === first);
    if (index <= 0) continue;
    const last = siblings[index - 1].id;
    const move: GraphOperation = { kind: "moveBlock", move: { blockId: first, parentId, afterId: last } };
    let made = publication.length - 1;
    while (made >= 0) {
      const op = publication[made];
      if (op.kind === "upsertBlock" && op.block.id === last) break;
      made -= 1;
    }
    if (made >= 0) publication.splice(made + 1, 0, move); else publication.push(move);
  }
  // Blocks made again go parents first, each after the block it follows when
  // that is made again too: the edit lists them in the order they went
  // (2026-11-07: undoing a line's removal that dissolved its columns made the
  // second column before the first, and the server refused the whole step).
  const creations = new Map(publication.flatMap((op) => op.kind === "upsertBlock" && !originalIds.has(op.block.id)
    ? [[op.block.id, op] as const] : []));
  const inOrder: GraphOperation[] = [], emitted = new Set<string>();
  const emit = (op: GraphOperation) => {
    if (op.kind === "upsertBlock" && creations.has(op.block.id)) {
      if (emitted.has(op.block.id)) return;
      emitted.add(op.block.id);
      for (const needed of [op.block.parentId, op.block.afterId]) {
        const first = needed ? creations.get(needed) : undefined;
        if (first) emit(first);
      }
    }
    inOrder.push(op);
  };
  for (const op of publication) if (op.kind !== "deleteBlock") emit(op);
  // What goes goes last, inner blocks first, after what it held has left it;
  // the views blocks made again own come after those blocks.
  const removed = publication.filter((op) => op.kind === "deleteBlock").reverse();
  const views = madeViews.map((view) => ({ kind: "upsertView", view }) as GraphOperation);
  return { present, publication: [...inOrder, ...views, ...removed] };
}

export function undoEditorHistory(history: EditorHistory, keep: ReadonlySet<string> = new Set()): EditorHistory {
  const previous = history.past.at(-1);
  if (!previous) return history;
  return { past: history.past.slice(0, -1), ...applyEdit(history.present, previous, false, keep), future: [previous, ...history.future] };
}

export function redoEditorHistory(history: EditorHistory): EditorHistory {
  const next = history.future[0];
  if (!next) return history;
  return { past: [...history.past, next], ...applyEdit(history.present, next, true, new Set()), future: history.future.slice(1) };
}

export function remoteGraphRebase(history: EditorHistory, remote: ContentGraph): EditorHistory {
  return { ...history, present: remote, publication: [] };
}

/**
 * What the app read for a block on its own (a bookmark's preview, a video's
 * size: no step of the history) goes into every step that holds the block as
 * that kind of block, as if it had been there from the first: an Undo then
 * finds the block as its step left it, and takes back the block it made
 * rather than keep it as a peer's (2026-11-11: a bookmark a pasted address
 * made after its line stayed through ⌘Z).
 */
export function fillEditorHistory(history: EditorHistory, blockId: string, type: string, content: Record<string, unknown>): EditorHistory {
  const fill = (block: GraphBlock | null) => block?.id === blockId && block.type === type
    ? { ...block, content: { ...block.content, ...content } } : block;
  const edits = (list: EditorGraphEdit[]) => list.map((edit) => edit.blocks.some((change) => change.id === blockId)
    ? { blocks: edit.blocks.map((change) => change.id === blockId ? { ...change, before: fill(change.before), after: fill(change.after) } : change) }
    : edit);
  return { ...history, past: edits(history.past), future: edits(history.future) };
}

