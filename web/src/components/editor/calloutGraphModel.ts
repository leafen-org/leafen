import type { ContentGraph, GraphBlock, GraphOperation } from "@/contentGraph";
import { newCalloutLook, rememberCalloutLook } from "@/lib/blockDefaults";
import { editorBlockChildren, insertEditorBlock, moveEditorBlock, removeEditorBlocks } from "./editorGraphModel";

type Change = { graph: ContentGraph; operations: GraphOperation[] };

// A callout made on the screen has no text of its own, as Notion's (seen
// 2026-11-05): the line that becomes one goes inside it as the block it is
// (a heading stays a heading), the color it had going to the callout; else
// it takes its person's last callout look (newCalloutLook).
export function wrapEditorBlocksInCallouts(graph: ContentGraph, blockIds: readonly string[]) {
  let change: Change = { graph, operations: [] };
  const ids: string[] = [];
  for (const id of blockIds) {
    const block = change.graph.blocks.find(candidate => candidate.id === id);
    if (!block) continue;
    if (block.type === "callout") { ids.push(id); continue; }
    const { color, ...own } = block.content;
    const painted = typeof color === "string" && color !== "default";
    const look = newCalloutLook();
    const made = insertEditorBlock(change.graph, id, "callout", { ...look, ...(painted ? { color } : {}) });
    const moved = moveEditorBlock(made.graph, id, made.block.id, null);
    if (!moved) continue;
    const held: GraphBlock = { ...moved.graph.blocks.find(candidate => candidate.id === id)!, content: own };
    change = {
      graph: { ...moved.graph, blocks: moved.graph.blocks.map(candidate => candidate.id === id ? held : candidate) },
      operations: [...change.operations, ...made.operations, ...moved.operations,
        ...(painted ? [{ kind: "upsertBlock", block: { ...held, afterId: null } } as GraphOperation] : [])],
    };
    ids.push(made.block.id);
  }
  return { ...change, ids };
}

const heldLines = (graph: ContentGraph, id: string) => editorBlockChildren(graph, id);

// A callout with no text of its own turned into something else lets its lines
// go where it stood, all of them chosen (seen 2026-11-05). `turned` are the
// ones the new type falls to; `ids` are all the change leaves for the ones asked for.
export function unwrapEditorCallouts(graph: ContentGraph, blockIds: readonly string[], text: (block: GraphBlock) => string) {
  let change: Change = { graph, operations: [] };
  const ids: string[] = [], turned: string[] = [];
  for (const id of blockIds) {
    const block = change.graph.blocks.find(candidate => candidate.id === id);
    const lines = block?.type === "callout" && !text(block) ? heldLines(change.graph, id) : [];
    if (!block || !lines.length) { ids.push(id); turned.push(id); continue; }
    let after = id;
    const operations = [...change.operations];
    let next = change.graph;
    for (const line of lines) {
      const moved = moveEditorBlock(next, line.id, block.parentId, after);
      if (!moved) continue;
      next = moved.graph;
      operations.push(...moved.operations);
      after = line.id;
      ids.push(line.id);
    }
    const removed = removeEditorBlocks(next, [id]);
    change = { graph: removed.graph, operations: [...operations, ...removed.operations] };
    turned.push(lines[0].id);
  }
  return { ...change, ids, turned };
}

/** What a person gives a callout — its icon or its color — the next one they make takes. */
export function rememberCalloutChange(block: GraphBlock, change: Record<string, unknown>) {
  if (block.type === "callout") rememberCalloutLook(change);
}
