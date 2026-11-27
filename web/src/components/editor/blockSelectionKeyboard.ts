import type { ContentGraph, GraphBlock, GraphOperation } from "@/contentGraph";
import {
  STRUCTURAL_BLOCK_TYPES, TAB_INTO_BLOCK_TYPES, TAKES_FIRST_CHILD_BLOCK_TYPES, TOGGLE_BLOCK_TYPES,
} from "@/lib/blockSemantics";
import { tidyColumns, type EditorGraphChange } from "./columnLayoutModel";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import { editorBlockChildren, editorDescendantIds, editorSelectionRoots, moveEditorBlock, updateEditorBlock } from "./editorGraphModel";
import { syncedBlockKeepingOut, syncedBlockSource } from "./syncedBlockModel";
import { syncedCopyCrossingNow, syncedCopyEnterable, syncedCopyLeavable, type SyncedCopyCrossing } from "./syncedCopyPlaces";
import { drawnBlocks, type DrawnBlock } from "./blockOccurrences";

/**
 * Blocks in reading order, without the children of collapsed toggles; an
 * editor showing one block's content reads from under that block.
 */
function visibleOrder(graph: ContentGraph, rootId: string | null) {
  const order: GraphBlock[] = [];
  const visit = (parentId: string | null) => {
    for (const block of editorBlockChildren(graph, parentId)) {
      order.push(block);
      if (!(TOGGLE_BLOCK_TYPES.has(block.type as never) && block.content.collapsed === true)) visit(block.id);
    }
  };
  visit(rootId);
  return order;
}

/** The selected blocks whose parents are not selected, in reading order. */
export function selectedRootsInOrder(
  graph: ContentGraph, selection: ReadonlySet<string>, rootId: string | null = null,
): GraphBlock[] {
  const roots = new Set(editorSelectionRoots(graph, [...selection]).map((block) => block.id));
  return visibleOrder(graph, rootId).filter((block) => roots.has(block.id));
}

/** Tab: each selected root goes inside the block before it, keeping its order. */
export function indentSelectedBlocks(adapter: NativeEditorGraphAdapter, selection: ReadonlySet<string>) {
  for (const root of selectedRootsInOrder(adapter.graph, selection)) {
    const current = adapter.graph.blocks.find((block) => block.id === root.id) ?? root;
    const siblings = editorBlockChildren(adapter.graph, current.parentId);
    const previous = siblings[siblings.findIndex(({ id }) => id === current.id) - 1];
    // A synced block's original takes it too, as its last child, as Notion's
    // selected-block Tab does (seen 2026-11-08); a copy shows another's.
    const syncedOriginal = previous?.type === "synced_block" && !syncedBlockSource(previous, adapter.pageId, adapter.graph.blocks).copy;
    if (!previous || !(TAB_INTO_BLOCK_TYPES.has(previous.type as never) || syncedOriginal)) continue;
    if (TOGGLE_BLOCK_TYPES.has(previous.type as never) && previous.content.collapsed === true) {
      adapter.updateBlock(previous, { collapsed: false });
    }
    adapter.move(current.id, previous.id, editorBlockChildren(adapter.graph, previous.id).at(-1)?.id ?? null);
  }
}

/**
 * Shift+Tab: the selected roots leave their parent and follow it, in order.
 * An editor showing one block's content keeps them inside that block.
 */
export function outdentSelectedBlocks(
  adapter: NativeEditorGraphAdapter, selection: ReadonlySet<string>, rootId: string | null = null,
) {
  const afterByParent = new Map<string, string>();
  for (const root of selectedRootsInOrder(adapter.graph, selection, rootId)) {
    const current = adapter.graph.blocks.find((block) => block.id === root.id) ?? root;
    const parent = current.parentId ? adapter.graph.blocks.find((block) => block.id === current.parentId) : undefined;
    if (!parent || parent.id === rootId) continue;
    const after = afterByParent.get(parent.id) ?? parent.id;
    adapter.move(current.id, parent.parentId, after);
    afterByParent.set(parent.id, current.id);
  }
}

/**
 * Cmd/Ctrl+Shift+Arrow: the selected blocks go one step up or down the page,
 * together and as one step to undo, as Notion's (seen 2026-11-08). They pass
 * the block beside them, or go into it when it takes blocks: last in it going
 * up, first going down. A list item, to-do, open toggle, quote, callout or
 * synced block takes them even when empty, a line of text once it holds
 * blocks, and a column list in its last or first column; a closed toggle is
 * passed. From the top or bottom of what holds them they come out before or
 * after it, a column's into the column beside it first. Going up, a synced
 * block stays where it is rather than go anywhere inside a synced block; going
 * down it goes in (Notion's, seen 2026-11-08 — keptOutOfSyncedBlocks). An
 * editor showing one block's content keeps them inside that block.
 *
 * A synced block's copy takes them too, into its original, and from the top
 * or bottom of its content they come out before or after the copy, not its
 * original, as Notion's (seen 2026-11-09), on the original's page and on
 * another page alike. `shownIn` names the copy on this page whose content
 * shows the line the caret is in. It answers what is left past this page's
 * own change (moveBlocksByKeys): blocks crossing to another page, and the
 * line that came out of a copy here.
 */
export function moveSelectedBlocks(
  adapter: NativeEditorGraphAdapter, selection: ReadonlySet<string>, direction: "up" | "down", rootId: string | null = null,
  shownIn: string | null = null,
): { crossing?: { blocks: GraphBlock[]; crossing: SyncedCopyCrossing }; outOfCopy?: string } {
  if (syncedCopyCrossingNow()) return {};
  const crossings: { blocks: GraphBlock[]; crossing: SyncedCopyCrossing }[] = [];
  const outOfCopy: string[] = [];
  adapter.changeLayout((graph) => {
    const runs: GraphBlock[][] = [];
    for (const root of selectedRootsInOrder(graph, selection, rootId)) {
      const run = runs.at(-1), siblings = editorBlockChildren(graph, root.parentId);
      if (run && siblings[siblings.findIndex(({ id }) => id === root.id) - 1]?.id === run.at(-1)!.id) run.push(root);
      else runs.push([root]);
    }
    let current = graph;
    const operations: GraphOperation[] = [];
    for (const run of direction === "up" ? runs : runs.reverse()) {
      const blocks = run.map((root) => current.blocks.find((block) => block.id === root.id) ?? root);
      const place = stepPlace(current, blocks, direction, selection, rootId, {
        pageId: adapter.pageId, shownIn,
        leaves: () => syncedCopyLeavable(adapter, blocks),
        enters: (copy) => syncedCopyEnterable(adapter, copy, blocks),
      });
      if (place && "crossing" in place) { if (!crossings.length) crossings.push({ blocks, crossing: place.crossing }); continue; }
      if (!place || (direction === "up" && syncedBlockKeepingOut(current, place.parentId, blocks.map((block) => block.type)))) continue;
      if (place.outOfCopy) outOfCopy.push(blocks[0]!.id);
      let afterId = place.afterId;
      for (const block of blocks) {
        const moved = moveEditorBlock(current, block.id, place.parentId, afterId);
        if (!moved) break;
        current = moved.graph;
        operations.push(...moved.operations);
        afterId = block.id;
      }
      const tidied = tidyColumns(current, blocks.flatMap((block) => block.parentId ? [block.parentId] : []));
      current = tidied.graph;
      operations.push(...tidied.operations);
    }
    return operations.length ? { graph: current, operations } : null;
  });
  return { crossing: crossings[0], outOfCopy: outOfCopy[0] };
}

/** What moving blocks know of synced copies around them (moveSelectedBlocks). */
type CopyEdges = {
  pageId: string;
  /** The copy on this page whose content shows the moving line. */
  shownIn: string | null;
  /** Whether they may leave the editor's root for the copy, on another page, it is shown in. */
  leaves(): boolean;
  /** Whether they may go into this copy's original, on another page. */
  enters(copy: GraphBlock): boolean;
};

/** Where blocks side by side under one parent go one step up or down (moveSelectedBlocks); null when nowhere. */
function stepPlace(
  graph: ContentGraph, blocks: readonly GraphBlock[], direction: "up" | "down", selection: ReadonlySet<string>, rootId: string | null,
  copies: CopyEdges,
): { parentId: string | null; afterId: string | null; outOfCopy?: boolean } | { crossing: SyncedCopyCrossing } | null {
  const up = direction === "up", edge = up ? blocks[0]! : blocks.at(-1)!;
  const byId = (id: string | null) => graph.blocks.find((block) => block.id === id);
  const into = (holder: GraphBlock) => ({ parentId: holder.id, afterId: up ? editorBlockChildren(graph, holder.id).at(-1)?.id ?? null : null });
  const beside = (block: GraphBlock) => {
    const siblings = editorBlockChildren(graph, block.parentId);
    return { parentId: block.parentId, afterId: up ? siblings[siblings.findIndex(({ id }) => id === block.id) - 1]?.id ?? null : block.id };
  };
  if (blocks.some((block) => block.parentId !== edge.parentId)) return null;
  const siblings = editorBlockChildren(graph, edge.parentId);
  const neighbour = siblings[siblings.findIndex(({ id }) => id === edge.id) + (up ? -1 : 1)];
  if (neighbour) {
    if (selection.has(neighbour.id)) return null;
    const source = neighbour.type === "synced_block" ? syncedBlockSource(neighbour, copies.pageId, graph.blocks) : null;
    if (source?.copy) {
      const original = source.pageId === copies.pageId ? byId(source.blockId) : undefined;
      if (original && !editorDescendantIds(graph, [original.id]).has(neighbour.id)) return into(original);
      return source.pageId !== copies.pageId && copies.enters(neighbour) ? { crossing: { kind: "enter", copy: neighbour } } : beside(neighbour);
    }
    const columns = neighbour.type === "column_list" && !blocks.some((block) => block.type === "column_list")
      ? editorBlockChildren(graph, neighbour.id) : [];
    const holder = neighbour.type === "column_list" ? (up ? columns.at(-1) : columns[0]) : takesBlocks(graph, neighbour) ? neighbour : undefined;
    return holder ? into(holder) : beside(neighbour);
  }
  if (rootId !== null && edge.parentId === rootId) return copies.leaves() ? { crossing: { kind: "leave" } } : null;
  const parent = edge.parentId === rootId ? undefined : byId(edge.parentId);
  if (!parent) return null;
  const shown = parent.type === "synced_block" && copies.shownIn ? byId(copies.shownIn) : undefined;
  if (shown?.type === "synced_block" && syncedBlockSource(shown, copies.pageId, graph.blocks).blockId === parent.id) {
    return { ...beside(shown), outOfCopy: true };
  }
  if (parent.type !== "column") return beside(parent);
  const columns = editorBlockChildren(graph, parent.parentId);
  const next = columns[columns.findIndex(({ id }) => id === parent.id) + (up ? -1 : 1)];
  if (next) return into(next);
  const list = parent.parentId === rootId ? undefined : byId(parent.parentId);
  return list ? beside(list) : null;
}

/** Whether a block takes blocks moved into it by Cmd/Ctrl+Shift+Arrow. */
function takesBlocks(graph: ContentGraph, block: GraphBlock) {
  if (TOGGLE_BLOCK_TYPES.has(block.type as never)) return block.content.collapsed !== true;
  if (block.type === "synced_block") return !syncedBlockSource(block, "").copy;
  return TAKES_FIRST_CHILD_BLOCK_TYPES.has(block.type as never)
    || block.type === "paragraph" && editorBlockChildren(graph, block.id).length > 0;
}

/** A Shift+arrow selection's ends: where it started from and where the end that moves is (occurrences, blockOccurrences). */
export type BlockSelectionWalk = { anchor: string; focus: string };

/** Blocks a selection walk takes, a column list among them: no columns or table rows (selectableOrder). */
const walkable = ({ block }: DrawnBlock) => block.type === "column_list" || !STRUCTURAL_BLOCK_TYPES.has(block.type as never);

/**
 * Drawn blocks chosen together, with where each was chosen: what a synced
 * copy draws goes with the copy when the copy is among them.
 */
function chosenDrawn(drawn: readonly DrawnBlock[]) {
  // In reading order a copy's blocks follow it, named under it.
  const kept: DrawnBlock[] = [];
  let under = "";
  for (const entry of drawn) {
    if (under && entry.at.startsWith(under)) continue;
    kept.push(entry);
    under = `${entry.at}/`;
  }
  return { selection: new Set(kept.map(({ block }) => block.id)), places: new Map(kept.map(({ block, at }) => [block.id, at] as const)) };
}

/**
 * Arrow keys move the selection to the block before or after it in the
 * keyboard's order; Shift moves the selection's far end from the block it
 * started from, as Notion's do (seen 2026-11-01): downward it steps over
 * what the far end holds; upward it stops at each block, or, coming back
 * towards the start, at the outermost block between them. A block taken
 * whole with the start inside becomes the start. So a column list is taken
 * whole only after everything in it.
 *
 * The order is the page as drawn (drawnBlocks): a synced copy on its
 * original's page holds the original's blocks again, and a block chosen
 * there walks from there — Shift+↑ takes the copy, not the original, as
 * Notion's (seen 2026-11-14). `chosen.at` says where a chosen block was
 * chosen (chosenPlace); the result's `places` where the new one's are.
 */
export function arrowBlockSelection(
  graph: ContentGraph, selection: ReadonlySet<string>, direction: "up" | "down", extend: boolean,
  rootId: string | null = null, walk: BlockSelectionWalk | null = null,
  chosen: { pageId: string; at?(id: string): string } = { pageId: "" },
): { selection: Set<string>; walk: BlockSelectionWalk | null; places: Map<string, string> } {
  const shown = drawnBlocks(graph, rootId, chosen.pageId);
  const selectable = shown.filter(walkable);
  const order = selectable.some(({ block }) => selection.has(block.id)) ? selectable : shown;
  const unchanged = () => ({ selection: new Set(selection), walk, places: new Map<string, string>() });
  const places = new Map<string, number>(), firsts = new Map<string, number>();
  order.forEach(({ at, block }, index) => {
    places.set(at, index);
    if (!firsts.has(block.id)) firsts.set(block.id, index);
  });
  const edges: number[] = [];
  for (const root of editorSelectionRoots(graph, [...selection])) {
    const edge = places.get(chosen.at?.(root.id) ?? root.id) ?? firsts.get(root.id);
    if (edge !== undefined) edges.push(edge);
  }
  if (!edges.length) return unchanged();
  const [top, bottom] = [Math.min(...edges), Math.max(...edges)];
  if (!extend) {
    const target = order[direction === "up" ? top - 1 : bottom + 1];
    if (!target) return unchanged();
    return { ...chosenDrawn([target]), walk: { anchor: target.at, focus: target.at } };
  }
  const at = (place: string | undefined) => place === undefined ? -1 : places.get(place) ?? -1;
  let [anchor, focus] = [at(walk?.anchor), at(walk?.focus)];
  if (anchor < 0 || focus < 0) [anchor, focus] = direction === "up" ? [bottom, top] : [top, bottom];
  const parents = new Map(shown.map((entry) => [entry.at, entry.parent]));
  const within = (index: number, outer: number) => {
    for (let parent = parents.get(order[index]!.at); parent; parent = parents.get(parent)) if (parent === order[outer]!.at) return true;
    return false;
  };
  let next: number;
  if (direction === "down") {
    next = focus + 1;
    while (next < order.length && within(next, focus)) next += 1;
  } else {
    next = focus - 1;
    if (focus > anchor) for (let outer = anchor; outer < next; outer += 1) if (within(next, outer)) { next = outer; break; }
  }
  if (next < 0 || next >= order.length) return unchanged();
  if (within(anchor, next)) anchor = next;
  const [from, to] = anchor < next ? [anchor, next] : [next, anchor];
  return { ...chosenDrawn(order.slice(from, to + 1)), walk: { anchor: order[anchor]!.at, focus: order[next]!.at } };
}

/**
 * Blocks the keyboard selects, in reading order: shown blocks other than
 * columns and table rows. A column list is one block of its own, before
 * the blocks in its columns, as Notion's (seen 2026-11-01).
 */
function selectableOrder(graph: ContentGraph, rootId: string | null) {
  return visibleOrder(graph, rootId).filter((block) => block.type === "column_list" || !STRUCTURAL_BLOCK_TYPES.has(block.type as never));
}

/**
 * The blocks from one to another in reading order, both ends taken, as a
 * Shift+press in another block chooses them from the caret's (Notion's,
 * seen 2026-11-11). The ends are where they are drawn (occurrences, a block's
 * id where it stands itself), the order the page as drawn (drawnBlocks).
 * Empty when either is not shown here.
 */
export function spanBlockSelection(graph: ContentGraph, from: string, to: string, rootId: string | null = null, pageId = "") {
  const order = drawnBlocks(graph, rootId, pageId).filter(walkable);
  const start = order.findIndex(({ at }) => at === from), end = order.findIndex(({ at }) => at === to);
  if (start < 0 || end < 0) return { selection: new Set<string>(), places: new Map<string, string>() };
  return chosenDrawn(order.slice(Math.min(start, end), Math.max(start, end) + 1));
}

/** The last block of the page or of the block whose body this is. */
export function lastTopLevelBlock(graph: ContentGraph, rootId: string | null = null) {
  return visibleOrder(graph, rootId).filter((block) => block.parentId === rootId).at(-1);
}

/** Cmd/Ctrl+A selects every block. */
export function allBlockSelection(graph: ContentGraph, rootId: string | null = null): Set<string> {
  return new Set(selectableOrder(graph, rootId).map((block) => block.id));
}

/** ⌘↑ and ⌘↓ select the first or last block (useBlockSelectionKeys); with `extend`, the selection reaches it. */
export function edgeBlockSelection(
  graph: ContentGraph, selection: ReadonlySet<string>, edge: "first" | "last", extend: boolean,
  rootId: string | null = null,
): Set<string> {
  const order = selectableOrder(graph, rootId);
  const target = edge === "first" ? 0 : order.length - 1;
  if (target < 0) return new Set(selection);
  const indexes = order.flatMap((block, index) => selection.has(block.id) ? [index] : []);
  if (!extend || indexes.length === 0) return new Set([order[target]!.id]);
  const [from, to] = edge === "first" ? [0, Math.max(...indexes)] : [Math.min(...indexes), target];
  return new Set(order.slice(from, to + 1).map((block) => block.id));
}

function contentChange(graph: ContentGraph, changes: readonly [GraphBlock, Record<string, unknown>][]): EditorGraphChange | null {
  if (changes.length === 0) return null;
  let current = graph;
  const operations: GraphOperation[] = [];
  for (const [block, content] of changes) {
    const updated = updateEditorBlock(current, current.blocks.find((candidate) => candidate.id === block.id) ?? block, content);
    current = updated.graph;
    operations.push(...updated.operations);
  }
  return { graph: current, operations };
}

/**
 * Space and Cmd/Ctrl+Enter check the selected to-dos, or uncheck them when
 * all are checked, and open the selected toggles, or close them when all are
 * open; null when none is selected.
 */
export function blockStateChange(graph: ContentGraph, selection: ReadonlySet<string>): EditorGraphChange | null {
  const selected = graph.blocks.filter((block) => selection.has(block.id));
  const todos = selected.filter((block) => block.type === "to_do");
  const toggles = selected.filter((block) => TOGGLE_BLOCK_TYPES.has(block.type as never));
  const checked = todos.some((block) => block.content.checked !== true);
  const collapsed = toggles.some((block) => block.content.collapsed !== true);
  return contentChange(graph, [
    ...todos.map((block) => [block, { checked }] as [GraphBlock, Record<string, unknown>]),
    ...toggles.map((block) => [block, { collapsed }] as [GraphBlock, Record<string, unknown>]),
  ]);
}

/** Left and Right close or open the selected toggles; null when none changes. */
export function togglesCollapsedChange(graph: ContentGraph, selection: ReadonlySet<string>, collapsed: boolean): EditorGraphChange | null {
  return contentChange(graph, graph.blocks
    .filter((block) => selection.has(block.id) && TOGGLE_BLOCK_TYPES.has(block.type as never)
      && (block.content.collapsed === true) !== collapsed)
    .map((block) => [block, { collapsed }]));
}
