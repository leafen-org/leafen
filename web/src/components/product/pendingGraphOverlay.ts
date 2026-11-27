import { compareGraphOrder, withDataSourceChange, type ContentGraph, type GraphBlock, type GraphOperation } from "@/contentGraph";
import { editorDescendantIds, removeEditorBlocks } from "@/components/editor/editorGraphModel";

function upsert<Item extends { id: string }>(items: readonly Item[], item: Item): Item[] {
  const index = items.findIndex((candidate) => candidate.id === item.id);
  return index < 0 ? [...items, item] : items.map((candidate, at) => at === index ? item : candidate);
}

// A row made here that waits for the server stands among its database's rows
// where its place puts it, as the server reads it back (2026-11-09: a
// sidebar section's new row showed last until it was sent, then first).
// A change of a row it has writes the values it names over the row's, the
// others kept, as the server writes them (writeDatabaseRowRecord): a title
// renamed, or a template chosen for the row (rowTemplateChosenHere.ts), sends
// the row with those alone, and the row showed without its other values
// while the change waited.
function upsertRow(rows: ContentGraph["rows"], row: ContentGraph["rows"][number]): ContentGraph["rows"] {
  if (rows.some(({ id }) => id === row.id)) {
    return rows.map((held) => held.id === row.id ? { ...held, ...row, values: { ...held.values, ...row.values } } : held);
  }
  const at = rows.findIndex((other) => other.dataSourceId === row.dataSourceId && other.position > row.position);
  return at < 0 ? [...rows, row] : [...rows.slice(0, at), row, ...rows.slice(at)];
}

function without<Item extends { id: string }>(items: readonly Item[], id: string): Item[] {
  return items.filter((item) => item.id !== id);
}

// A block put among its siblings here gives them new places in their order,
// as the editor does: the places the server gave them ("p" and ten digits)
// don't sort with one made here (2026-11-13: a line an undo put back after
// the line above it showed first on the page opened from its copy).
function placeAt(index: number, id: string) {
  return `a${String(index).padStart(8, "0")}:${id}`;
}

function childrenOf(graph: ContentGraph, parentId: string | null) {
  return graph.blocks.filter((block) => (block.parentId ?? null) === parentId).sort(compareGraphOrder);
}

/**
 * The graph with a block among its parent's blocks right after `afterId`, or
 * first with none, as the server puts a block made or moved so. One whose
 * block to follow isn't there (the server refuses that change) stays where
 * it stands, or goes last.
 */
function placeBlock(graph: ContentGraph, block: GraphBlock, afterId: string | null): ContentGraph {
  const siblings = childrenOf(graph, block.parentId ?? null);
  const here = siblings.findIndex(({ id }) => id === block.id);
  if (here >= 0) siblings.splice(here, 1);
  const anchor = afterId === null ? -1 : siblings.findIndex(({ id }) => id === afterId);
  siblings.splice(afterId === null ? 0 : anchor >= 0 ? anchor + 1 : here >= 0 ? here : siblings.length, 0, block);
  const places = new Map(siblings.map(({ id }, index) => [id, placeAt(index, id)]));
  const placed = { ...block, position: places.get(block.id)! };
  let found = false;
  const blocks = graph.blocks.map((candidate) => {
    if (candidate.id === block.id) {
      found = true;
      return placed;
    }
    const position = places.get(candidate.id);
    return position ? { ...candidate, position } : candidate;
  });
  if (!found) blocks.push(placed);
  return { ...graph, blocks };
}

/** A block that stands here moved under a parent, never under itself. */
function moveBlock(graph: ContentGraph, block: GraphBlock, parentId: string | null, afterId: string | null) {
  if (parentId !== (block.parentId ?? null) && parentId !== null && editorDescendantIds(graph, [block.id]).has(parentId)) return graph;
  return placeBlock(graph, { ...block, parentId, afterId }, afterId);
}

/** What placing blocks by their positions alone needs across the overlay. */
type Placing = {
  /** Blocks made here placed by their positions alone, as given. */
  byPosition: { id: string; parentId: string | null; position: string }[];
  /** The places the graph was read with. */
  read: () => ReadonlyMap<string, string>;
};

const STORED_PLACE = /^p\d{10}/;
const placedFirst = (position: string) => /^a[+-]?\d+$/.test(position) && Number(position.slice(1)) <= 0;

/**
 * The block a new block written with only a position follows, as the server
 * finds it (content_graph_position_place.go): a place the server gave ("p"
 * and ten digits) sorts among the blocks it was read with; "a0" goes first,
 * after those so placed before it whose position sorts before its; any other
 * position after the last block so placed before it whose position sorts
 * before its, else after the blocks there before them; none goes last. Null
 * puts it first.
 */
function anchorByPosition(graph: ContentGraph, block: GraphBlock, placing: Placing): string | null {
  const parentId = block.parentId ?? null;
  const siblings = childrenOf(graph, parentId);
  if (STORED_PLACE.test(block.position)) {
    const placedHere = new Set(placing.byPosition.map(({ id }) => id));
    const read = placing.read();
    let before: { id: string; position: string } | undefined;
    for (const { id } of siblings) {
      const position = read.get(id);
      if (placedHere.has(id) || position === undefined) continue;
      if (position < block.position && (!before || position > before.position)) before = { id, position };
    }
    return before?.id ?? null;
  }
  if (block.position === "") return siblings.at(-1)?.id ?? null;
  const first = placedFirst(block.position);
  const here = new Set(siblings.map(({ id }) => id));
  const group = placing.byPosition.filter((place) => place.parentId === parentId && here.has(place.id)
    && place.position !== "" && !STORED_PLACE.test(place.position) && placedFirst(place.position) === first);
  let made: Placing["byPosition"][number] | undefined;
  for (const place of group) {
    if (place.position >= block.position) continue;
    if (!made || place.position > made.position) made = place;
  }
  if (made) return made.id;
  if (first) return null;
  const placedAfter = new Set(group.map(({ id }) => id));
  return siblings.filter(({ id }) => !placedAfter.has(id)).at(-1)?.id ?? null;
}

/**
 * A block written here: a new one goes after the block it names, or by its
 * position, as the server puts it. One that stands here changes where it
 * stands — the block a write names is the one it was made after, which the
 * copy holding it already shows (an existing block's write names none,
 * nativeEditorGraphAdapter's withoutStaleAnchors) — unless the write puts
 * it under another parent, which moves it there as the server does.
 */
function overlayBlock(graph: ContentGraph, block: GraphBlock, placing: Placing): ContentGraph {
  const parentId = block.parentId ?? null;
  const stored = graph.blocks.find(({ id }) => id === block.id);
  if (stored) {
    const written = { ...block, parentId: stored.parentId, position: stored.position };
    const blocks = upsert(graph.blocks, written);
    if ((stored.parentId ?? null) === parentId) return { ...graph, blocks };
    return moveBlock({ ...graph, blocks }, written, parentId, block.afterId);
  }
  if (block.afterId !== null) return placeBlock(graph, block, block.afterId);
  const afterId = anchorByPosition(graph, block, placing);
  placing.byPosition.push({ id: block.id, parentId, position: block.position });
  return placeBlock(graph, block, afterId);
}

function overlayOne(graph: ContentGraph, operation: GraphOperation, placing: Placing): ContentGraph {
  switch (operation.kind) {
    case "upsertBlock":
      return overlayBlock(graph, operation.block, placing);
    case "moveBlock": {
      const { afterId, blockId, parentId } = operation.move;
      const block = graph.blocks.find(({ id }) => id === blockId);
      return block ? moveBlock(graph, block, parentId, afterId) : graph;
    }
    case "deleteBlock":
      return removeEditorBlocks(graph, [operation.deletion.blockId]).graph;
    case "upsertDataSource": {
      // A write names only the fields it changes (withDataSourceChange); a
      // new database written without a name has none.
      const stored = graph.dataSources.find((source) => source.id === operation.dataSource.id)
        ?? { id: operation.dataSource.id, title: "" };
      return { ...graph, dataSources: upsert(graph.dataSources, withDataSourceChange(stored, operation.dataSource)) };
    }
    case "deleteDataSource": {
      const { id } = operation.dataSourceDeletion;
      return {
        ...graph,
        dataSources: without(graph.dataSources, id),
        properties: graph.properties.filter((property) => property.dataSourceId !== id),
        rows: graph.rows.filter((row) => row.dataSourceId !== id),
        views: graph.views.filter((view) => view.dataSourceId !== id),
      };
    }
    case "upsertView":
      return { ...graph, views: upsert(graph.views, operation.view) };
    case "updateView":
      return { ...graph, views: upsert(graph.views, operation.viewUpdate.view) };
    case "deleteView":
      return { ...graph, views: without(graph.views, operation.viewDeletion.id) };
    case "upsertProperty":
      return { ...graph, properties: upsert(graph.properties, operation.property) };
    case "deleteProperty":
      return { ...graph, properties: without(graph.properties, operation.propertyDeletion.id) };
    case "upsertRow":
      return { ...graph, rows: upsertRow(graph.rows, operation.row) };
    case "deleteRow":
      return { ...graph, rows: without(graph.rows, operation.rowDeletion.id) };
    default:
      // Task dependencies are not part of the page's graph.
      return graph;
  }
}

/**
 * The page's graph with the rows the outbox's kept changes make that it
 * doesn't show (a sidebar section's "+" while the server is out of reach,
 * 2026-11-09), each as the changes kept since its last kept deletion write
 * it; rows shown stay as the page has them. A row a later kept change deletes
 * stays gone, as the server will have it once they are sent: Escape in a new
 * row's empty title, or a row deleted, while the server was away took the row
 * off the screen and the next kept change drew it back from its making kept
 * before (2026-11-25: back 16 ms after the press, until the page was read
 * again). The same graph when there is nothing to add.
 */
export function withRowsKeptMade(graph: ContentGraph, operations: readonly GraphOperation[]): ContentGraph {
  const shown = new Set(graph.rows.map(({ id }) => id));
  const lastDeleted = new Map<string, number>();
  operations.forEach((operation, at) => { if (operation.kind === "deleteRow") lastDeleted.set(operation.rowDeletion.id, at); });
  const made = operations.filter((operation, at) => operation.kind === "upsertRow" && !shown.has(operation.row.id)
    && at > (lastDeleted.get(operation.row.id) ?? -1));
  return made.length ? overlayPendingGraphOperations(graph, made) : graph;
}

/**
 * A page's cached graph with the structure changes made here that the outbox
 * keeps for the server, in the order made, so a page opened without the
 * server shows what was written in it, each block where the server puts it.
 * Given change by change (each the operations of one write), a block given a
 * position alone is placed among its own change's as the server places it,
 * one change after another (content_graph_position_place.go): one change's
 * blocks stand after an earlier change's. Spread into one, a later change's
 * "a3" stood before an earlier change's "a5" until the page was read again.
 * A plain list of operations is one change.
 */
export function overlayPendingGraphOperations(
  graph: ContentGraph,
  changes: readonly GraphOperation[] | readonly (readonly GraphOperation[])[],
): ContentGraph {
  let read: ReadonlyMap<string, string> | undefined;
  const places = () => read ??= new Map(graph.blocks.map(({ id, position }) => [id, position]));
  const each = (Array.isArray(changes[0]) ? changes : [changes]) as readonly (readonly GraphOperation[])[];
  return each.reduce((current, operations) => {
    const placing: Placing = { byPosition: [], read: places };
    return operations.reduce((now, operation) => overlayOne(now, operation, placing), current);
  }, graph);
}
