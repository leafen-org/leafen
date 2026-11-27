import type { ContentGraph, GraphBlock, GraphOperation } from "@/contentGraph";
import { changeEditorBlockTypes, editorBlockChildren, moveEditorBlock } from "./editorGraphModel";

// A block that becomes a toggle heading holds the blocks after it up to the
// next heading of its size or a larger one, as Notion's does when a heading
// or a toggle turns into one by typed Markdown (seen 2026-10-01); a toggle
// list holds nothing more. The heading stays open, so they show inside it.

type ToggleHeadingType = `toggle_heading_${1 | 2 | 3 | 4}`;

/** A toggle heading type, or null for any other. */
export function toggleHeadingType(type: string): ToggleHeadingType | null {
  return /^toggle_heading_[1-4]$/u.test(type) ? type as ToggleHeadingType : null;
}

function headingLevel(block: GraphBlock) {
  const level = /^(?:toggle_)?heading_([1-4])$/u.exec(block.type)?.[1];
  return level ? Number(level) : null;
}

/** The blocks after a block that a toggle heading of this level takes in. */
function toggleHeadingFollowers(graph: ContentGraph, blockId: string, level: number) {
  const block = graph.blocks.find((candidate) => candidate.id === blockId);
  if (!block) return [];
  const siblings = editorBlockChildren(graph, block.parentId ?? null);
  const followers: string[] = [];
  for (const sibling of siblings.slice(siblings.findIndex((candidate) => candidate.id === blockId) + 1)) {
    const siblingLevel = headingLevel(sibling);
    if (siblingLevel !== null && siblingLevel <= level) break;
    followers.push(sibling.id);
  }
  return followers;
}

/** A block turned into a toggle heading of a size, holding its followers after its own children. */
export function changeToToggleHeading(
  graph: ContentGraph,
  blockId: string,
  type: ToggleHeadingType,
): { graph: ContentGraph; operations: GraphOperation[] } | null {
  const level = Number(type.slice(-1));
  const changed = changeEditorBlockTypes(graph, [blockId], type);
  let next = changed.graph;
  const operations = [...changed.operations];
  // The last follower goes first, each right after the heading's own last
  // child: a move in one change is placed after a block its parent held
  // before the change.
  const afterId = editorBlockChildren(next, blockId).at(-1)?.id ?? null;
  for (const followerId of toggleHeadingFollowers(graph, blockId, level).reverse()) {
    const moved = moveEditorBlock(next, followerId, blockId, afterId);
    if (!moved) return null;
    next = moved.graph;
    operations.push(...moved.operations);
  }
  return { graph: next, operations };
}
