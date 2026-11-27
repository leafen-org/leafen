import { graphTextBlockTypes, type ContentGraph, type GraphOperation } from "@/contentGraph";
import { CONTAINER_BLOCK_TYPES } from "@/lib/blockSemantics";
import { selectedRootsInOrder } from "./blockSelectionKeyboard";
import { placeBeside, tidyColumns, type EditorGraphChange } from "./columnLayoutModel";
import { editorBlockChildren, moveEditorBlock } from "./editorGraphModel";
import { syncedBlockKeepingOut } from "./syncedBlockModel";

/**
 * Where blocks dropped on a block go: before or after it, inside it as its
 * first children ("start") or its last ("inside"), or beside it in columns.
 */
export type BlockDrop = { id: string; mode: "before" | "after" | "start" | "inside" | "left" | "right" };

/** Where a block is: its parent and the block before it. */
export type BlockPlace = { id: string; parentId: string | null; afterId: string | null };

const textBlockTypes = new Set<string>(graphTextBlockTypes);

/** Whether blocks dropped on a block can go inside it: a column, or a text block that holds blocks. */
export function takesDroppedChildren(type: string) {
  return type === "column" || (textBlockTypes.has(type) && CONTAINER_BLOCK_TYPES.has(type as never));
}

/** The block and the blocks it is inside. */
export function blockLine(graph: ContentGraph, blockId: string) {
  const line = new Set<string>();
  const byId = new Map(graph.blocks.map((block) => [block.id, block]));
  for (let at = byId.get(blockId); at && !line.has(at.id); at = at.parentId ? byId.get(at.parentId) : undefined) line.add(at.id);
  return line;
}

/**
 * Puts dropped blocks where they were dropped, in their reading order, as one
 * change; null when one would go on or inside itself, or a synced block
 * inside a synced block (keptOutOfSyncedBlocks — the drop's place is beside
 * it then, editorDropTarget.ts). A closed toggle they go into stays closed,
 * as Notion's (seen 2026-11-07), and a column they leave empty goes away.
 */
export function placeDroppedBlocks(graph: ContentGraph, blockIds: readonly string[], drop: BlockDrop): EditorGraphChange | null {
  const target = graph.blocks.find((block) => block.id === drop.id);
  const roots = selectedRootsInOrder(graph, new Set(blockIds));
  const holderId = drop.mode === "inside" || drop.mode === "start" ? drop.id : target?.parentId ?? null;
  if (!target || syncedBlockKeepingOut(graph, holderId, roots.map((root) => root.type))) return null;
  if (drop.mode === "left" || drop.mode === "right") return placeBeside(graph, drop.id, blockIds, drop.mode);
  const line = blockLine(graph, drop.id);
  if (roots.length === 0 || roots.some((root) => line.has(root.id))) return null;
  const moving = new Set(roots.map((root) => root.id));
  const inTarget = drop.mode === "inside" || drop.mode === "start";
  const parentId = inTarget ? target.id : target.parentId;
  const siblings = editorBlockChildren(graph, parentId).filter((block) => !moving.has(block.id));
  let afterId = drop.mode === "start" ? null
    : drop.mode === "inside" ? siblings.at(-1)?.id ?? null
      : drop.mode === "after" ? target.id
        : siblings[siblings.findIndex((block) => block.id === target.id) - 1]?.id ?? null;
  let current = graph;
  const operations: GraphOperation[] = [];
  for (const root of roots) {
    const moved = moveEditorBlock(current, root.id, parentId, afterId);
    if (!moved) return null;
    current = moved.graph;
    operations.push(...moved.operations);
    afterId = root.id;
  }
  const tidied = tidyColumns(current, roots.flatMap((root) => root.parentId ? [root.parentId] : []));
  return { graph: tidied.graph, operations: [...operations, ...tidied.operations] };
}

/** Where blocks are now, in reading order, to put them back after a move. */
export function blockPlaces(graph: ContentGraph, blockIds: readonly string[]): BlockPlace[] {
  return selectedRootsInOrder(graph, new Set(blockIds)).map((root) => {
    const siblings = editorBlockChildren(graph, root.parentId);
    return { id: root.id, parentId: root.parentId, afterId: siblings[siblings.findIndex((block) => block.id === root.id) - 1]?.id ?? null };
  });
}

/**
 * Puts moved blocks back where they were, as one change; a block whose place
 * is gone stays. Null when none can go back.
 */
export function returnBlocks(graph: ContentGraph, places: readonly BlockPlace[]): EditorGraphChange | null {
  let current = graph;
  const operations: GraphOperation[] = [];
  const left: string[] = [];
  for (const place of places) {
    const block = current.blocks.find((candidate) => candidate.id === place.id);
    if (!block || (place.parentId && !current.blocks.some((candidate) => candidate.id === place.parentId))) continue;
    const after = place.afterId ? current.blocks.find((candidate) => candidate.id === place.afterId) : undefined;
    const moved = moveEditorBlock(current, place.id, place.parentId, after?.parentId === place.parentId ? after.id : null);
    if (!moved) continue;
    if (block.parentId) left.push(block.parentId);
    current = moved.graph;
    operations.push(...moved.operations);
  }
  if (operations.length === 0) return null;
  const tidied = tidyColumns(current, left);
  return { graph: tidied.graph, operations: [...operations, ...tidied.operations] };
}
