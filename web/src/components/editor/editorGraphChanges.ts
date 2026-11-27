import type { ContentGraph, GraphOperation } from "@/contentGraph";
import { withoutViewsOfGone } from "./blockViewCopies";

/**
 * Applies an editor change to the graph the page holds now: the blocks the
 * change touched take their state from the change and every other block and
 * database entity stays as it is, so a change never undoes one that landed
 * after the graph it was computed from. A block that goes takes the views it
 * owns with it, as the server does (withoutViewsOfGone).
 */
export function applyEditorBlockChanges(
  current: ContentGraph,
  next: ContentGraph,
  operations: readonly GraphOperation[],
): ContentGraph {
  const touched = new Set<string>();
  for (const operation of operations) {
    if (operation.kind === "upsertBlock") touched.add(operation.block.id);
    else if (operation.kind === "moveBlock") touched.add(operation.move.blockId);
    else if (operation.kind === "deleteBlock") touched.add(operation.deletion.blockId);
  }
  if (touched.size === 0) return current;
  // A change places its blocks by giving their siblings new positions, which
  // the siblings take too: the positions the server gave them don't sort
  // with the change's.
  const placed = new Map(next.blocks.map((block) => [block.id, block]));
  const gone = new Set([...touched].filter((id) => !placed.has(id)));
  const parents = new Set<string | null>();
  for (const id of touched) {
    const block = placed.get(id);
    if (block) parents.add(block.parentId ?? null);
  }
  return {
    ...withoutViewsOfGone(current, gone),
    blocks: [
      ...current.blocks.filter((block) => !touched.has(block.id)).map((block) => {
        const sibling = placed.get(block.id);
        const parentId = block.parentId ?? null;
        return sibling && parents.has(parentId) && (sibling.parentId ?? null) === parentId && sibling.position !== block.position
          ? { ...block, position: sibling.position }
          : block;
      }),
      ...next.blocks.filter((block) => touched.has(block.id)),
    ],
  };
}
