import { graphWireBounds } from "@/contentGraphWire.generated";
import { newCalloutLook } from "@/lib/blockDefaults";
import { newId } from "@/lib/ids";
import { databaseViewBlock } from "@/lib/nativeDataSourceManagement";
import type {
  ContentGraph, GraphBlock, GraphBlockType, GraphDatabaseContainer, GraphOperation, GraphViewType,
} from "@/contentGraph";
import { editorBlockChildren, editorBlockChildrenNow, editorDescendantIds, newBlockPlace, siblingPlace } from "./editorBlockIndex";

export { editorBlockChildren, editorBlockNeighbors, editorDescendantIds } from "./editorBlockIndex";
export { duplicateEditorBlocks, editorSelectionRoots } from "./editorBlockCopies";
export { applyEditorBlockChanges } from "./editorGraphChanges";

export function insertEditorBlock(
  graph: ContentGraph,
  afterId: string | null,
  type: GraphBlockType = "paragraph",
  content: Record<string, unknown> = {},
) {
  const after = afterId ? graph.blocks.find((block) => block.id === afterId) : undefined;
  const placed = placeNewEditorBlock(graph, {
    id: newId(), parentId: after?.parentId ?? null, afterId, position: "", type, content,
  });
  return { ...placed, operations: [{ kind: "upsertBlock", block: placed.block }] as GraphOperation[] };
}

// placeNewEditorBlock puts a new block after the block it names in its
// container, or last there. The new block shows in place at once, the server
// ordering it by the block it follows: it takes a place between its new
// neighbours' places, theirs left as they are (2026-11-15: every sibling
// placed anew for each line pasted, some 4 ms of a six-line paste in a
// 1,031-block page at CPU ×2). Where theirs leave no place between them,
// the siblings take places around it here, as a move does.
function placeNewEditorBlock(graph: ContentGraph, block: GraphBlock) {
  const siblings = editorBlockChildrenNow(graph, block.parentId);
  const after = block.afterId ? siblings.findIndex((candidate) => candidate.id === block.afterId) : -1;
  const insertionIndex = after >= 0 ? after + 1 : siblings.length;
  const between = newBlockPlace(siblings[insertionIndex - 1], siblings[insertionIndex]);
  if (between !== null) {
    const placed = { ...block, position: between };
    return { block: placed, graph: { ...graph, blocks: [...graph.blocks, placed] } };
  }
  const ordered = [...siblings];
  ordered.splice(insertionIndex, 0, block);
  const positions = new Map(ordered.map((candidate, index) => [candidate.id, siblingPlace(index, candidate.id)]));
  const placed = { ...block, position: positions.get(block.id) ?? block.position };
  return {
    block: placed,
    graph: {
      ...graph,
      blocks: [...graph.blocks.map((candidate) => {
        const position = positions.get(candidate.id);
        return position ? { ...candidate, position } : candidate;
      }), placed],
    },
  };
}

/**
 * A database a line's menu adds goes by that line, as in Notion: in the
 * line's place when the line is empty and goes, or right after it. A new
 * database starts as the view the menu names.
 */
export type EditorDatabaseChoice = {
  lineId: string;
  replaceLine: boolean;
  view?: GraphViewType;
  /** The view's first settings, as a chart's kind. */
  viewConfig?: Record<string, unknown>;
  /**
   * Makes the new database at once, as Notion's database and view items do;
   * a linked view of a data source asks which source instead.
   */
  create?: boolean;
  /**
   * Made to show a data source linked into it, as Notion's "데이터 소스 보기
   * 연결" (seen 2026-11-02): the new block waits, empty, for its source.
   */
  link?: boolean;
  /** Told what the database's making changed, for the page's history. */
  made?: (before: ContentGraph, after: ContentGraph, operations: readonly GraphOperation[]) => void;
};

/**
 * Puts a new database, made to go at the end of the body, by the line a
 * menu chose: its block goes right after the line in the line's container,
 * and a line that goes, empty and holding no blocks, is deleted after it.
 * Returns the placed block and container, the body's blocks and the
 * database's operations, or null when the line is gone.
 */
export function placeNewDatabase(
  graph: ContentGraph,
  database: { block: GraphBlock; container: GraphDatabaseContainer; operations: readonly GraphOperation[] },
  choice: EditorDatabaseChoice,
) {
  const line = graph.blocks.find((block) => block.id === choice.lineId);
  if (!line) return null;
  const placed = placeNewEditorBlock(graph, { ...database.block, parentId: line.parentId, afterId: line.id });
  const replaced = choice.replaceLine && editorBlockChildren(graph, line.id).length === 0;
  const operations = database.operations.map((operation): GraphOperation => (
    operation.kind === "upsertBlock" && operation.block.id === placed.block.id
      ? { kind: "upsertBlock", block: placed.block }
      : operation
  ));
  if (replaced) operations.push({ kind: "deleteBlock", deletion: { blockId: line.id } });
  return {
    block: placed.block,
    container: { ...database.container, parentBlockId: placed.block.parentId, position: placed.block.position },
    blocks: replaced ? placed.graph.blocks.filter((block) => block.id !== line.id) : placed.graph.blocks,
    operations,
  };
}

/**
 * A new block last in its container (null: the page's own lines) or, with
 * `after`, right after that child or first (null) there — one change, as a
 * table's row put between two others.
 */
export function insertEditorChildBlock(
  graph: ContentGraph,
  parentId: string | null,
  type: GraphBlockType = "paragraph",
  content: Record<string, unknown> = {},
  after?: string | null,
): { block: GraphBlock; graph: ContentGraph; operations: GraphOperation[] } {
  const siblings = editorBlockChildren(graph, parentId);
  if (after !== undefined && after !== (siblings.at(-1)?.id ?? null)) {
    const added = insertEditorChildBlock(graph, parentId, type, content);
    const moved = moveEditorBlock(added.graph, added.block.id, parentId, after);
    if (!moved) return added;
    const block = moved.graph.blocks.find((candidate) => candidate.id === added.block.id) ?? added.block;
    return { block, graph: moved.graph, operations: [...added.operations, ...moved.operations] };
  }
  // Last among them, placed as a sibling is (placeNewEditorBlock): its own
  // position alone would sort before positions the server gave (2026-10-31:
  // a table made by the API took its new row at the top).
  const placed = placeNewEditorBlock(graph, {
    id: newId(), parentId, afterId: siblings.at(-1)?.id ?? null, position: "", type, content,
  });
  return { ...placed, operations: [{ kind: "upsertBlock", block: placed.block }] as GraphOperation[] };
}

/** How many tabs a new tab block starts with, as Notion's ("탭 1–3", seen 2026-10-06). */
const NEW_TAB_BLOCK_TABS = 3;

function addEditorTabItems(graph: ContentGraph, parentId: string, count: number) {
  let nextGraph = graph;
  const items: GraphBlock[] = [];
  const operations: GraphOperation[] = [];
  for (let index = 0; index < count; index++) {
    const item = insertEditorChildBlock(nextGraph, parentId, "paragraph", {});
    nextGraph = item.graph;
    items.push(item.block);
    operations.push(...item.operations);
  }
  return { graph: nextGraph, items, operations };
}

/** A new tab block: its tabs, still empty, whose names the caller writes. */
export function insertEditorTabBlock(
  graph: ContentGraph,
  afterId: string | null,
  content: Record<string, unknown> = {},
) {
  const container = insertEditorBlock(graph, afterId, "tab", content);
  const tabs = addEditorTabItems(container.graph, container.block.id, NEW_TAB_BLOCK_TABS);
  return { block: container.block, items: tabs.items, graph: tabs.graph, operations: [...container.operations, ...tabs.operations] };
}

/** An empty line becomes a new tab block in its place. */
export function turnEditorBlockIntoNewTabs(graph: ContentGraph, blockId: string) {
  const source = graph.blocks.find((block) => block.id === blockId);
  if (!source) return null;
  const { label: _label, ...content } = source.content;
  const container = { ...source, type: "tab" as const, content };
  const turned = { ...graph, blocks: graph.blocks.map((candidate) => candidate.id === source.id ? container : candidate) };
  const tabs = addEditorTabItems(turned, source.id, NEW_TAB_BLOCK_TABS);
  return { block: container, items: tabs.items, graph: tabs.graph,
    operations: [{ kind: "upsertBlock", block: container } as GraphOperation, ...tabs.operations] };
}

function emptyTableCells(width: number) {
  return Array.from({ length: width }, () => [] as Array<Record<string, unknown>>);
}

/** A new simple table's columns and rows, as Notion's "/표" makes it (3×3, seen 2026-10-06). */
export const NEW_TABLE_SIZE = 3;

function addEditorTableRows(graph: ContentGraph, tableId: string, width: number) {
  let nextGraph = graph;
  const rows: GraphBlock[] = [];
  const operations: GraphOperation[] = [];
  for (let index = 0; index < NEW_TABLE_SIZE; index++) {
    const row = insertEditorChildBlock(nextGraph, tableId, "table_row", { cells: emptyTableCells(width) });
    nextGraph = row.graph;
    rows.push(row.block);
    operations.push(...row.operations);
  }
  return { graph: nextGraph, rows, operations };
}

/** A new simple table: empty cells, no header row or column, as Notion's. */
export function insertEditorTableBlock(
  graph: ContentGraph,
  afterId: string | null,
  content: Record<string, unknown> = {},
) {
  const width = Math.max(1, Math.min(graphWireBounds.tableWidth, Number(content.tableWidth) || NEW_TABLE_SIZE));
  const container = insertEditorBlock(graph, afterId, "table", {
    tableWidth: width,
    hasColumnHeader: content.hasColumnHeader === true,
    hasRowHeader: content.hasRowHeader === true,
    ...(Array.isArray(content.columnWidths) ? { columnWidths: content.columnWidths } : {}),
  });
  const table = addEditorTableRows(container.graph, container.block.id, width);
  return {
    block: container.block,
    rows: table.rows,
    graph: table.graph,
    operations: [...container.operations, ...table.operations],
  };
}

export function changeEditorBlocksToTables(
  graph: ContentGraph,
  blockIds: readonly string[],
  columnWidths: (blockId: string) => readonly number[] | undefined = () => undefined,
) {
  const selected = new Set(blockIds);
  let nextGraph = graph;
  const operations: GraphOperation[] = [];
  for (const source of graph.blocks.filter((block) => selected.has(block.id) && block.type !== "table")) {
    if (editorBlockChildren(nextGraph, source.id).length > 0) continue;
    const widths = columnWidths(source.id);
    const container: GraphBlock = {
      ...source,
      type: "table",
      content: { tableWidth: NEW_TABLE_SIZE, hasColumnHeader: false, hasRowHeader: false, ...(widths ? { columnWidths: [...widths] } : {}) },
    };
    nextGraph = {
      ...nextGraph,
      blocks: nextGraph.blocks.map((block) => block.id === source.id ? container : block),
    };
    operations.push({ kind: "upsertBlock", block: container });
    const table = addEditorTableRows(nextGraph, container.id, NEW_TABLE_SIZE);
    nextGraph = table.graph;
    operations.push(...table.operations);
  }
  return { graph: nextGraph, operations };
}

/** A tab added to a tab block, empty as Notion's "+" adds it. */
export function insertEditorTabItem(
  graph: ContentGraph,
  parentId: string,
) {
  const item = insertEditorChildBlock(graph, parentId, "paragraph", {});
  return { item: item.block, graph: item.graph, operations: item.operations };
}

export function updateEditorBlock(
  graph: ContentGraph,
  block: GraphBlock,
  content: Record<string, unknown>,
) {
  const next = { ...block, content: { ...block.content, ...content } };
  return {
    graph: { ...graph, blocks: graph.blocks.map((candidate) => candidate.id === block.id ? next : candidate) },
    operations: [{ kind: "upsertBlock", block: next }] as GraphOperation[],
  };
}

export function replaceEditorBlocks(graph: ContentGraph, blocks: readonly GraphBlock[]) {
  const replacements = new Map(blocks.map((block) => [block.id, block]));
  return {
    graph: {
      ...graph,
      blocks: graph.blocks.map((block) => replacements.get(block.id) ?? block),
    },
    operations: blocks.map((block) => ({ kind: "upsertBlock", block }) as GraphOperation),
  };
}

// A list's marker format belongs to its kind of list: a numbered list's
// "a." is no bullet's, so a block turned into the other kind of list leaves
// it behind.
const LIST_TYPES: ReadonlySet<GraphBlockType> = new Set(["bulleted_list_item", "numbered_list_item"]);

function withoutListFormat(content: GraphBlock["content"]) {
  if (!("listFormat" in content)) return content;
  const { listFormat: _listFormat, ...rest } = content;
  return rest;
}

// What a block keeps when it turns into another type: a list item leaves
// its list format, and a block turned into a callout takes a new callout's
// icon and, without a color of its own, its color (newCalloutLook).
function turnedContent(block: GraphBlock, type: GraphBlockType): GraphBlock["content"] {
  if (block.type === type) return block.content;
  if (LIST_TYPES.has(type)) return withoutListFormat(block.content);
  if (type === "callout") {
    const colorless = block.content.color === undefined || block.content.color === "default";
    return { ...block.content, ...newCalloutLook(), ...(colorless ? {} : { color: block.content.color }) };
  }
  return block.content;
}

export function changeEditorBlockTypes(
  graph: ContentGraph,
  blockIds: readonly string[],
  type: GraphBlockType,
) {
  const ids = new Set(blockIds);
  const changed = graph.blocks.filter((block) => ids.has(block.id)).map((block) => (
    // A block turned into a page is that page's block, named by its own ID;
    // a sub-page turned into a block keeps nothing of the page's link.
    type === "child_page" && block.type !== "child_page"
      ? { ...block, type, content: { childPageId: block.id } }
      : block.type === "child_page" && type !== "child_page"
        ? { ...block, type, content: {} }
        : { ...block, type, content: turnedContent(block, type) }
  ));
  // The server turns such a page back into its block, taking its blocks
  // inside and the page to the trash, only when the change asks.
  const unmade = new Set(graph.blocks.filter((block) => ids.has(block.id) && block.type === "child_page" && type !== "child_page")
    .map((block) => block.id));
  const byId = new Map(changed.map((block) => [block.id, block]));
  const removed = editorDescendantIds(
    graph,
    changed.filter((block) => graph.blocks.find((candidate) => candidate.id === block.id)?.type === "table")
      .map((block) => block.id),
  );
  for (const block of changed) removed.delete(block.id);
  return {
    graph: {
      ...graph,
      blocks: graph.blocks.filter((block) => !removed.has(block.id))
        .map((block) => byId.get(block.id) ?? block),
    },
    operations: [
      ...changed.map((block) => ({ kind: "upsertBlock",
        block: unmade.has(block.id) ? { ...block, restorePageConversion: true } : block }) as GraphOperation),
      ...[...removed].map((blockId) => ({ kind: "deleteBlock", deletion: { blockId } }) as GraphOperation),
    ],
  };
}

export function changeEditorBlocksToTabs(
  graph: ContentGraph,
  blockIds: readonly string[],
) {
  const ids = new Set(blockIds);
  const sources = graph.blocks.filter((block) => ids.has(block.id) && block.type !== "tab");
  let nextGraph = graph;
  const operations: GraphOperation[] = [];
  const transfers: Array<{ source: GraphBlock; item: GraphBlock }> = [];

  for (const source of sources) {
    const directChildren = editorBlockChildren(nextGraph, source.id);
    const { label: _label, ...content } = source.content;
    const container = { ...source, type: "tab" as const, content };
    nextGraph = {
      ...nextGraph,
      blocks: nextGraph.blocks.map((candidate) => candidate.id === source.id ? container : candidate),
    };
    operations.push({ kind: "upsertBlock", block: container });

    const itemResult = insertEditorChildBlock(nextGraph, source.id, "paragraph", {});
    nextGraph = itemResult.graph;
    operations.push(...itemResult.operations);
    transfers.push({ source, item: itemResult.block });

    if (directChildren.length === 0) {
      const bodyResult = insertEditorChildBlock(nextGraph, itemResult.block.id, "paragraph", {});
      nextGraph = bodyResult.graph;
      operations.push(...bodyResult.operations);
      continue;
    }

    let afterId: string | null = null;
    for (const child of directChildren) {
      const moved = moveEditorBlock(nextGraph, child.id, itemResult.block.id, afterId);
      if (!moved) continue;
      nextGraph = moved.graph;
      operations.push(...moved.operations);
      afterId = child.id;
    }
  }

  return { graph: nextGraph, operations, transfers };
}

// editorSelectionOwnsDatabase reports whether blocks, or blocks under them,
// include an inline database that owns its data rather than a linked view.
export function editorSelectionOwnsDatabase(graph: ContentGraph, blockIds: readonly string[]) {
  const ids = editorDescendantIds(graph, blockIds);
  return graph.blocks.some((block) => ids.has(block.id) && block.type === "inline_database"
    && block.content.linkedDatabaseSource !== true);
}

// editorSelectionCopiesDatabase reports whether blocks, or blocks under them,
// include a database a duplicate copies with its pages or without them
// ("복제 ›", ⌘⇧D): one that isn't a view block, whose duplicate is "복제 ⌘D"
// alone (databaseViewBlock; Notion's, seen 2026-11-09).
export function editorSelectionCopiesDatabase(graph: ContentGraph, blockIds: readonly string[]) {
  const ids = editorDescendantIds(graph, blockIds);
  return graph.blocks.some((block) => ids.has(block.id) && block.type === "inline_database"
    && !databaseViewBlock(block, graph.views));
}

export function removeEditorBlocks(graph: ContentGraph, blockIds: readonly string[]) {
  const removed = editorDescendantIds(graph, blockIds);
  return {
    graph: { ...graph, blocks: graph.blocks.filter((block) => !removed.has(block.id)) },
    operations: [...removed].map((blockId) => ({ kind: "deleteBlock", deletion: { blockId } }) as GraphOperation),
  };
}

export function moveEditorBlock(
  graph: ContentGraph,
  blockId: string,
  parentId: string | null,
  afterId: string | null,
) {
  const descendants = editorDescendantIds(graph, [blockId]);
  if ((parentId && descendants.has(parentId)) || afterId === blockId) return null;
  const block = graph.blocks.find((candidate) => candidate.id === blockId);
  if (!block) return null;
  const remaining = editorBlockChildren(graph, parentId).filter((candidate) => candidate.id !== blockId);
  const insertionIndex = afterId === null
    ? 0
    : Math.max(0, remaining.findIndex((candidate) => candidate.id === afterId) + 1);
  remaining.splice(insertionIndex, 0, { ...block, parentId, afterId });
  const positions = new Map(remaining.map((candidate, index) => [candidate.id, siblingPlace(index, candidate.id)]));
  const next = { ...block, parentId, afterId, position: positions.get(block.id) ?? block.position };
  return {
    graph: {
      ...graph,
      blocks: graph.blocks.map((candidate) => {
        if (candidate.id === blockId) return next;
        const position = positions.get(candidate.id);
        return position ? { ...candidate, position } : candidate;
      }),
    },
    operations: [{ kind: "moveBlock", move: { blockId, parentId, afterId } }] as GraphOperation[],
  };
}
