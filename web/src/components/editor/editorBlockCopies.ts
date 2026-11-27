import { compareGraphOrder, type ContentGraph, type GraphBlock } from "@/contentGraph";
import { TOGGLE_BLOCK_TYPES } from "@/lib/blockSemantics";
import { newId } from "@/lib/ids";
import { withOwnViewCopies } from "./blockViewCopies";
import { editorBlockChildren, editorBlockChildrenNow, editorDescendantIds, newBlockPlaces, siblingPlace } from "./editorBlockIndex";

// editorSelectionRoots returns the selected blocks with no selected block
// around them, in graph order; a selected column list holds the blocks in
// its columns though its columns are not selected.
export function editorSelectionRoots(graph: ContentGraph, blockIds: readonly string[]) {
  const selected = new Set(blockIds);
  const parents = new Map(graph.blocks.map((block) => [block.id, block.parentId]));
  const held = (id: string) => {
    for (let parent = parents.get(id); parent; parent = parents.get(parent)) if (selected.has(parent)) return true;
    return false;
  };
  return graph.blocks.filter((block) => selected.has(block.id) && !held(block.id));
}

/**
 * Places for copies that follow `last` among its siblings, in their order,
 * taken as a new block takes its place (placeNewEditorBlock): between `last`
 * and the block after it, theirs left as they are, or, where theirs leave no
 * room, the siblings and the copies taking places anew around them. A copy's
 * place was its original's with ':copy:…' added, so a copy of a copy made
 * before the page was read again grew by it each time, and at the sixth quick
 * ⌘D passed the 255 bytes a place may hold: the server refused the change and
 * its copies were lost (2026-11-25).
 */
function copyPlaces(graph: ContentGraph, last: GraphBlock, copyIds: readonly string[]) {
  const siblings = editorBlockChildrenNow(graph, last.parentId);
  const at = siblings.findIndex((block) => block.id === last.id);
  const places = newBlockPlaces(last, siblings[at + 1], copyIds.length);
  if (places) return { places, respaced: new Map<string, string>() };
  const order = [...siblings.slice(0, at + 1).map(({ id }) => id), ...copyIds, ...siblings.slice(at + 1).map(({ id }) => id)];
  const respaced = new Map(order.map((id, index) => [id, siblingPlace(index, id)]));
  return { places: copyIds.map((id) => respaced.get(id)!), respaced };
}

// duplicateEditorBlocks copies blocks with their blocks and the views they own
// (withOwnViewCopies); a block copied alone may instead become a reference to
// what it shows, without its blocks (`referenceAlone`: a synced block's copy).
// The blocks under a copy keep their originals' places among themselves.
export function duplicateEditorBlocks(
  graph: ContentGraph, blockIds: readonly string[], referenceAlone?: (block: GraphBlock) => Record<string, unknown> | null,
) {
  const roots = editorSelectionRoots(graph, blockIds);
  const reference = roots.length === 1 ? referenceAlone?.(roots[0]!) ?? null : null;
  const sourceIds = reference ? new Set([roots[0]!.id]) : editorDescendantIds(graph, roots.map((block) => block.id));
  const orderedSources: GraphBlock[] = [];
  const queue = [...roots];
  while (queue.length > 0) {
    const source = queue.shift();
    if (!source || !sourceIds.has(source.id)) continue;
    orderedSources.push(source);
    queue.push(...editorBlockChildren(graph, source.id));
  }
  const mappedIds = new Map(orderedSources.map((source) => [source.id, newId()]));
  // The copies of blocks chosen under one parent follow the last of them
  // together, in their order, as Notion's (seen 2026-10-31: two blocks
  // duplicated read A, B, A copy, B copy).
  const rootPlaces = new Map<string, { after: string; position: string }>();
  const siblings = new Map<string | null, GraphBlock[]>();
  const respaced = new Map<string, string>();
  for (const root of roots) siblings.set(root.parentId, [...siblings.get(root.parentId) ?? [], root]);
  for (const group of siblings.values()) {
    const ordered = [...group].sort(compareGraphOrder);
    const placed = copyPlaces(graph, ordered.at(-1)!, ordered.map((root) => mappedIds.get(root.id)!));
    for (const [id, position] of placed.respaced) respaced.set(id, position);
    ordered.forEach((root, index) => rootPlaces.set(root.id, {
      after: index === 0 ? ordered.at(-1)!.id : mappedIds.get(ordered[index - 1].id)!,
      position: placed.places[index]!,
    }));
  }
  const copies = orderedSources.map((source) => {
    const id = mappedIds.get(source.id)!;
    const parentId = source.parentId && mappedIds.get(source.parentId) || source.parentId;
    const place = rootPlaces.get(source.id);
    const afterId = place ? place.after : (source.afterId && mappedIds.get(source.afterId)) ?? null;
    return {
      ...source,
      id,
      parentId,
      afterId,
      position: place ? place.position : source.position,
      // A toggle's copy starts closed, as Notion's (seen 2026-11-07): open is the source's own.
      content: reference && source.id === roots[0]!.id ? reference
        : TOGGLE_BLOCK_TYPES.has(source.type as never) ? { ...structuredClone(source.content), collapsed: true } : structuredClone(source.content),
    } as GraphBlock;
  });
  const spaced = respaced.size ? { ...graph, blocks: graph.blocks.map((block) => {
    const position = respaced.get(block.id);
    return position ? { ...block, position } : block;
  }) } : graph;
  const made = withOwnViewCopies(spaced, copies, mappedIds);
  const copiedBySource = new Map(orderedSources.map((source, index) => [source.id, made.copies[index]]));
  return {
    blocks: roots.map((source) => copiedBySource.get(source.id)!).filter(Boolean),
    // Each copied block and its copy, which takes its text.
    copied: orderedSources.map((source) => ({ source, copyId: mappedIds.get(source.id)! })),
    graph: made.graph, operations: made.operations,
  };
}
