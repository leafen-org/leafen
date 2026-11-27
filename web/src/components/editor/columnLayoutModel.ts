import type { ContentGraph, GraphBlock, GraphOperation } from "@/contentGraph";
import {
  editorBlockChildren,
  editorDescendantIds,
  editorSelectionRoots,
  insertEditorBlock,
  insertEditorChildBlock,
  moveEditorBlock,
  removeEditorBlocks,
  updateEditorBlock,
} from "./editorGraphModel";

// A column stores its weight in the row as `width`, Notion's width_ratio ×
// 100: above 0 and up to 100, a column without one weighing 100 (Notion's
// ratio 1), and each column takes its weight's part of the row, as Notion's
// (seen 2026-11-24 through its API and screen: ratios 1 and 0.3125 stood at
// 76.19% and 23.81%, an unset ratio as 1). Every change here is one graph
// change, so it saves and undoes as one step.

export type EditorGraphChange = { graph: ContentGraph; operations: GraphOperation[] };
type Step = (graph: ContentGraph) => EditorGraphChange | null;

const MIN_SHARE = 8;

function block(graph: ContentGraph, id: string | null | undefined) {
  return id ? graph.blocks.find((candidate) => candidate.id === id) : undefined;
}

function storedWidth(column: GraphBlock) {
  const width = Number(column.content.width);
  return Number.isFinite(width) && width > 0 && width <= 100 ? width : null;
}

/** Each column's weight in its row: its stored width, or 100. */
export function columnWeights(columns: readonly GraphBlock[]) {
  return columns.map((column) => storedWidth(column) ?? 100);
}

/** Each column's share of its row in percent, adding up to 100. */
export function columnShares(columns: readonly GraphBlock[]) {
  return sharesOf(columnWeights(columns));
}

/** Weights as shares of the row in percent. */
export function sharesOf(weights: readonly number[]) {
  const total = weights.reduce((sum, weight) => sum + weight, 0);
  return weights.map((weight) => total > 0 ? (weight / total) * 100 : 100 / weights.length);
}

/**
 * The weights while the gap before column `index` is dragged `moved` pixels
 * along the reading direction in a row whose columns are `room` wide in all,
 * as Notion's (seen 2026-11-24 in Chrome at 1685, dpr 1, in two tabs, two,
 * three and four columns, its API read after each): the gap follows the
 * pointer, the pair trading weight in steps of 100 ÷ (8 × columns) — 6.25
 * for two, 4.17 for three, 3.125 for four — rounded to the nearest, their
 * sum kept and neither below one step; a weight may pass 100 until the
 * pointer lets go (settledWeights).
 */
export function draggedWeights(weights: readonly number[], index: number, moved: number, room: number) {
  const step = 100 / (8 * weights.length), total = weights.reduce((sum, weight) => sum + weight, 0);
  const pair = weights[index - 1] + weights[index];
  const traded = Math.round((moved / Math.max(1, room)) * total / step) * step;
  const before = Math.max(step, Math.min(pair - step, weights[index - 1] + traded));
  return weights.map((weight, at) => at === index - 1 ? before : at === index ? pair - before : weight);
}

/** Let go, no weight stays above 100, as Notion's (its ratio 1.3125 kept as 1, the row's shares moving with it). */
export function settledWeights(weights: readonly number[]) {
  return weights.map((weight) => Math.min(100, weight));
}

function run(graph: ContentGraph, steps: readonly Step[]): EditorGraphChange | null {
  let current = graph;
  const operations: GraphOperation[] = [];
  for (const step of steps) {
    const change = step(current);
    if (!change) return null;
    current = change.graph;
    operations.push(...change.operations);
  }
  return { graph: current, operations };
}

function setWidths(graph: ContentGraph, widths: ReadonlyMap<string, number>): EditorGraphChange {
  return run(graph, [...widths].map(([id, width]) => (current: ContentGraph) => {
    const column = block(current, id);
    if (!column) return null;
    const rounded = Math.round(width * 100) / 100;
    return storedWidth(column) === rounded ? { graph: current, operations: [] } : updateEditorBlock(current, column, { width: rounded });
  })) ?? { graph, operations: [] };
}

function columnsOf(graph: ContentGraph, column: GraphBlock) {
  return editorBlockChildren(graph, column.parentId).filter((candidate) => candidate.type === "column");
}

/** Adds an empty column after this one, splitting its share with it. */
export function addColumnAfter(graph: ContentGraph, columnId: string) {
  const column = block(graph, columnId);
  if (!column || column.type !== "column" || !column.parentId) return null;
  const columns = columnsOf(graph, column);
  const shares = columnShares(columns);
  const index = columns.findIndex((candidate) => candidate.id === column.id);
  const half = shares[index] / 2;
  const widths = new Map(columns.map((candidate, at) => [candidate.id, at === index ? half : shares[at]]));
  let addedId = "";
  let paragraphId = "";
  const change = run(graph, [
    (current) => setWidths(current, widths),
    (current) => {
      const added = insertEditorBlock(current, column.id, "column", { width: Math.round(half * 100) / 100 });
      addedId = added.block.id;
      return added;
    },
    (current) => {
      const paragraph = insertEditorChildBlock(current, addedId, "paragraph");
      paragraphId = paragraph.block.id;
      return paragraph;
    },
  ]);
  return change ? { ...change, focusId: paragraphId } : null;
}

/** Removes a column of three or more, giving its share to the column beside it. */
export function removeColumn(graph: ContentGraph, columnId: string) {
  const column = block(graph, columnId);
  if (!column || column.type !== "column") return null;
  const columns = columnsOf(graph, column);
  if (columns.length <= 2) return null;
  const shares = columnShares(columns);
  const index = columns.findIndex((candidate) => candidate.id === column.id);
  const neighbour = index > 0 ? index - 1 : index + 1;
  const widths = new Map(columns.flatMap((candidate, at) => at === index ? []
    : [[candidate.id, at === neighbour ? shares[at] + shares[index] : shares[at]] as const]));
  return run(graph, [(current) => setWidths(current, widths), (current) => removeEditorBlocks(current, [column.id])]);
}

/** Moves a column one place left or right. */
export function moveColumn(graph: ContentGraph, columnId: string, direction: -1 | 1) {
  const column = block(graph, columnId);
  if (!column || column.type !== "column") return null;
  const columns = columnsOf(graph, column);
  const index = columns.findIndex((candidate) => candidate.id === column.id);
  const target = index + direction;
  if (target < 0 || target >= columns.length) return null;
  const afterId = direction < 0 ? columns[index - 2]?.id ?? null : columns[index + 1].id;
  return moveEditorBlock(graph, column.id, column.parentId, afterId);
}

/** Stores the weights a drag of the gap before this column left (draggedWeights, settledWeights). */
export function setColumnWeights(graph: ContentGraph, columnId: string, weights: readonly number[]) {
  const column = block(graph, columnId);
  if (!column || column.type !== "column") return null;
  const columns = columnsOf(graph, column);
  if (columns.length !== weights.length) return null;
  const before = columnWeights(columns);
  return setWidths(graph, new Map(columns.flatMap((candidate, at) => before[at] === weights[at] ? [] : [[candidate.id, weights[at]] as const])));
}

/** Gives the column before this one `share` percent of the pair's width. */
export function resizeColumnPair(graph: ContentGraph, columnId: string, share: number) {
  const column = block(graph, columnId);
  if (!column || column.type !== "column") return null;
  const columns = columnsOf(graph, column);
  const index = columns.findIndex((candidate) => candidate.id === column.id);
  if (index < 1) return null;
  const shares = columnShares(columns);
  const pair = shares[index - 1] + shares[index];
  const minimum = Math.min(MIN_SHARE, pair / 4);
  const before = Math.max(minimum, Math.min(pair - minimum, share));
  const widths = new Map(columns.map((candidate, at) => [candidate.id,
    at === index - 1 ? before : at === index ? pair - before : shares[at]]));
  return setWidths(graph, widths);
}

/**
 * After blocks leave a column, an empty column goes and its share with it,
 * and a column list left with one column gives its blocks back to the page
 * in its place, as Notion does.
 */
export function tidyColumns(graph: ContentGraph, columnIds: Iterable<string>): EditorGraphChange {
  let current = graph;
  const operations: GraphOperation[] = [];
  for (const columnId of new Set(columnIds)) {
    const column = block(current, columnId);
    if (!column || column.type !== "column" || editorBlockChildren(current, column.id).length > 0) continue;
    const list = block(current, column.parentId);
    const removed = removeEditorBlocks(current, [column.id]);
    current = removed.graph;
    operations.push(...removed.operations);
    if (!list || list.type !== "column_list") continue;
    const remaining = editorBlockChildren(current, list.id).filter((candidate) => candidate.type === "column");
    if (remaining.length > 1) continue;
    let afterId = list.id;
    for (const child of remaining.flatMap((last) => editorBlockChildren(current, last.id))) {
      const moved = moveEditorBlock(current, child.id, list.parentId, afterId);
      if (!moved) continue;
      current = moved.graph;
      operations.push(...moved.operations);
      afterId = child.id;
    }
    const unwrapped = removeEditorBlocks(current, [list.id]);
    current = unwrapped.graph;
    operations.push(...unwrapped.operations);
  }
  return { graph: current, operations };
}

// New columns weigh alike with no width stored, as Notion's: its "/2개의 열"
// columns and its API's read with no width_ratio, each taken as 1.
function newColumns(graph: ContentGraph, listId: string, count: number): EditorGraphChange & { columnIds: string[] } | null {
  const columnIds: string[] = [];
  const change = run(graph, Array.from({ length: count }, () => (current: ContentGraph) => {
    const column = insertEditorChildBlock(current, listId, "column", {});
    columnIds.push(column.block.id);
    return column;
  }));
  return change ? { ...change, columnIds } : null;
}

/**
 * Turns a block into a column list of `count` columns: the block becomes the
 * first column's content and every other column starts with an empty
 * paragraph.
 */
export function wrapInColumns(graph: ContentGraph, blockId: string, count: number) {
  const target = block(graph, blockId);
  if (!target) return null;
  const columns = Math.max(2, Math.min(5, Math.floor(count)));
  let listId = "";
  let ids: string[] = [];
  const change = run(graph, [
    (current) => {
      const list = insertEditorBlock(current, target.id, "column_list");
      listId = list.block.id;
      return list;
    },
    (current) => {
      const made = newColumns(current, listId, columns);
      ids = made?.columnIds ?? [];
      return made;
    },
    (current) => moveEditorBlock(current, target.id, ids[0], null),
  ]);
  if (!change) return null;
  const filled = run(change.graph, ids.slice(1).map((id) => (current: ContentGraph) => insertEditorChildBlock(current, id, "paragraph")));
  return filled ? { graph: filled.graph, operations: [...change.operations, ...filled.operations] } : null;
}

/** Gives each block, in order, a column of its own in a new column list where the first one was. */
export function spreadInColumns(graph: ContentGraph, blockIds: readonly string[]) {
  const [first] = blockIds;
  if (!first || blockIds.length < 2) return null;
  let listId = "";
  let ids: string[] = [];
  return run(graph, [
    (current) => {
      const list = insertEditorBlock(current, first, "column_list");
      listId = list.block.id;
      return list;
    },
    (current) => {
      const made = newColumns(current, listId, blockIds.length);
      ids = made?.columnIds ?? [];
      return made;
    },
    ...blockIds.map((id, at) => (current: ContentGraph) => moveEditorBlock(current, id, ids[at], null)),
  ]);
}

/** Adds a column list of `count` columns, each with an empty paragraph, after a block. */
export function insertColumnsAfter(graph: ContentGraph, afterId: string | null, count: number) {
  const columns = Math.max(2, Math.min(5, Math.floor(count)));
  let listId = "";
  let ids: string[] = [];
  const steps: Step[] = [
    (current) => {
      const list = insertEditorBlock(current, afterId, "column_list");
      listId = list.block.id;
      return list;
    },
    (current) => {
      const made = newColumns(current, listId, columns);
      ids = made?.columnIds ?? [];
      return made;
    },
  ];
  const listed = run(graph, steps);
  if (!listed) return null;
  let firstParagraph = "";
  const change = run(listed.graph, ids.map((id, at) => (current: ContentGraph) => {
    const paragraph = insertEditorChildBlock(current, id, "paragraph");
    if (at === 0) firstParagraph = paragraph.block.id;
    return paragraph;
  }));
  return change ? { graph: change.graph, operations: [...listed.operations, ...change.operations], focusId: firstParagraph } : null;
}

/**
 * Drops blocks beside another, as Notion does: beside a block in a column
 * they get a new column next to it, and beside any other block the two sides
 * become a new two-column list. The columns the blocks left are tidied.
 */
export function placeBeside(graph: ContentGraph, targetId: string, blockIds: readonly string[], side: "left" | "right") {
  const target = block(graph, targetId);
  if (!target || target.type === "column" || target.type === "column_list") return null;
  const targetLine = new Set<string>();
  for (let at: GraphBlock | undefined = target; at; at = block(graph, at.parentId)) targetLine.add(at.id);
  const moving = editorSelectionRoots(graph, blockIds).filter((root) => !targetLine.has(root.id)
    && !editorDescendantIds(graph, [root.id]).has(target.id));
  if (moving.length === 0) return null;
  const sources = moving.map((root) => root.parentId).filter((id): id is string => Boolean(id));
  const parent = block(graph, target.parentId);
  let columnId = "";
  const steps: Step[] = [];
  if (parent?.type === "column" && block(graph, parent.parentId)?.type === "column_list") {
    const columns = columnsOf(graph, parent);
    const shares = columnShares(columns);
    const index = columns.findIndex((candidate) => candidate.id === parent.id);
    const half = shares[index] / 2;
    const widths = new Map(columns.map((candidate, at) => [candidate.id, at === index ? half : shares[at]]));
    steps.push(
      (current) => setWidths(current, widths),
      (current) => {
        const afterId = side === "right" ? parent.id : columns[index - 1]?.id ?? null;
        const added = afterId
          ? insertEditorBlock(current, afterId, "column", { width: Math.round(half * 100) / 100 })
          : insertEditorChildBlock(current, parent.parentId as string, "column", { width: Math.round(half * 100) / 100 });
        columnId = added.block.id;
        if (afterId) return added;
        const first = moveEditorBlock(added.graph, added.block.id, parent.parentId, null);
        return first ? { graph: first.graph, operations: [...added.operations, ...first.operations] } : null;
      },
    );
  } else {
    let listId = "";
    let ids: string[] = [];
    steps.push(
      (current) => {
        const list = insertEditorBlock(current, target.id, "column_list");
        listId = list.block.id;
        return list;
      },
      (current) => {
        const made = newColumns(current, listId, 2);
        ids = made?.columnIds ?? [];
        return made;
      },
      (current) => {
        columnId = side === "right" ? ids[1] : ids[0];
        return moveEditorBlock(current, target.id, side === "right" ? ids[0] : ids[1], null);
      },
    );
  }
  let previous: string | null = null;
  for (const root of moving) {
    steps.push((current) => {
      const moved = moveEditorBlock(current, root.id, columnId, previous);
      previous = root.id;
      return moved;
    });
  }
  const change = run(graph, steps);
  if (!change) return null;
  const tidied = tidyColumns(change.graph, sources);
  return { graph: tidied.graph, operations: [...change.operations, ...tidied.operations] };
}
