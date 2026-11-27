import type { ContentGraph, GraphBlock } from "@/contentGraph";
import { TOGGLE_BLOCK_TYPES } from "@/lib/blockSemantics";
import { parentChain } from "@/lib/parentChain";
import { editorBlockChildren } from "./editorBlockIndex";
import { syncedBlockSource } from "./syncedBlockModel";

/**
 * A synced copy whose original is on the same page draws the original's
 * blocks again inside it (nativeTiptapBlocks), so one block can stand on a
 * page twice. Each place it is drawn is an occurrence, named as the editor
 * names its node: the copies it is drawn in and its own id, "copy/block",
 * or its id alone where it stands itself.
 */
export type DrawnBlock = { block: GraphBlock; at: string; parent: string | null };

/** The block an occurrence draws. */
export function occurrenceBlockId(at: string) {
  return at.slice(at.lastIndexOf("/") + 1);
}

/** The original whose blocks this block, a synced copy on its original's page, draws inside it; null for any other block. */
export function syncedCopyDrawing(block: GraphBlock, pageId: string, blocks: readonly GraphBlock[]) {
  if (block.type !== "synced_block") return null;
  const source = syncedBlockSource(block, pageId, blocks);
  // One showing a whole page's body draws it with that page's editor (SyncedPageCopy), here none of it.
  return source.copy && !source.page && source.pageId === pageId ? source.blockId : null;
}

/**
 * The blocks a page, or the block whose content an editor shows, draws, in
 * reading order with where each is drawn: without the children of closed
 * toggles, of templates and of buttons, and with a synced copy's original's
 * blocks inside the copy, as the editor draws them (nativeTiptapBlocks) —
 * none inside a copy of an original it is already inside.
 */
export function drawnBlocks(graph: ContentGraph, rootId: string | null, pageId: string): DrawnBlock[] {
  const drawn: DrawnBlock[] = [];
  const sources: string[] = [];
  const visit = (parentId: string | null, instance: string, parent: string | null) => {
    for (const block of editorBlockChildren(graph, parentId)) {
      const at = instance + block.id;
      drawn.push({ block, at, parent });
      if (TOGGLE_BLOCK_TYPES.has(block.type as never) && block.content.collapsed === true) continue;
      if (block.type === "template" || block.type === "button") continue;
      const original = pageId ? syncedCopyDrawing(block, pageId, graph.blocks) : null;
      const source = original ?? block.id;
      if (sources.includes(source)) continue;
      sources.push(source);
      visit(source, original ? `${at}/` : instance, at);
      sources.pop();
    }
  };
  visit(rootId, "", null);
  return drawn;
}

/**
 * Where the children of the block drawn at `at` are drawn, as the scope a
 * caret asks for there (focusEditable): in the copies `at` is drawn in, ""
 * where the block stands itself — or in `at` itself when the block is a
 * synced copy drawing its original's blocks again on their page
 * (drawsOriginal).
 */
export function childrenScope(at: string, drawsOriginal = false) {
  return drawsOriginal ? at : at.slice(0, Math.max(0, at.lastIndexOf("/")));
}

/** The occurrence an occurrence is drawn in: its parent's, or the copy that draws it; null at the top. */
export function occurrenceParent(at: string, byId: ReadonlyMap<string, GraphBlock>): string | null {
  const cut = at.lastIndexOf("/");
  const instance = cut < 0 ? "" : at.slice(0, cut + 1);
  const parentId = byId.get(at.slice(cut + 1))?.parentId ?? null;
  if (instance) {
    const copyAt = instance.slice(0, -1);
    const copy = byId.get(occurrenceBlockId(copyAt));
    if (copy && parentId === syncedBlockSource(copy, "").blockId) return copyAt;
  }
  return parentId === null ? null : instance + parentId;
}

/** Whether an occurrence is drawn inside another, however deep (lib/parentChain.ts). */
export function occurrenceInside(at: string, outer: string, byId: ReadonlyMap<string, GraphBlock>) {
  const climb = parentChain(at);
  for (let parent = occurrenceParent(at, byId); parent !== null && climb(parent); parent = occurrenceParent(parent, byId)) {
    if (parent === outer) return true;
  }
  return false;
}

// Where the blocks of a selection were chosen, for the blocks chosen in a
// synced copy that draws them again: the selection holds the blocks (both
// places show them chosen, as Notion's), and the walk from it — Shift+↑ to
// the block around, ↑ and ↓ — and the copy's frame go from where they were
// chosen. Kept by the selection itself, so a selection made anew forgets it.
const chosenPlaces = new WeakMap<ReadonlySet<string>, ReadonlyMap<string, string>>();

/** Notes where a selection's blocks were chosen (a block's occurrence, by its id); the selection returned. */
export function placeChoice<T extends ReadonlySet<string>>(selection: T, places: Iterable<readonly [string, string]> | undefined): T {
  const elsewhere = new Map<string, string>();
  for (const [id, at] of places ?? []) if (at !== id && selection.has(id) && occurrenceBlockId(at) === id) elsewhere.set(id, at);
  if (elsewhere.size) chosenPlaces.set(selection, elsewhere);
  return selection;
}

/** Where a chosen block was chosen: an occurrence in a synced copy, or its own id. */
export function chosenPlace(selection: ReadonlySet<string>, id: string) {
  return chosenPlaces.get(selection)?.get(id) ?? id;
}

/** A selection made from another, its blocks or some of them, keeps where they were chosen. */
export function keepChoicePlaces<T extends ReadonlySet<string>>(from: ReadonlySet<string>, to: T): T {
  const places = chosenPlaces.get(from);
  return places ? placeChoice(to, places) : to;
}

/**
 * What a selection is to a synced block drawn at `frame`: "inside" when all
 * of it was chosen inside the block, as Escape from its text chooses its line
 * — the block then is being worked in, as while the caret is in it — "self"
 * when the block itself was chosen there, alone or with what is inside it,
 * and null otherwise (several blocks chosen with it, or none in it). Notion
 * frames it and shows its toolbar both ways, and its lines' handles for the
 * first (seen 2026-11-14).
 */
export function syncedFrameChoice(selection: ReadonlySet<string>, frame: string, byId: ReadonlyMap<string, GraphBlock>) {
  if (!selection.size) return null;
  const frameId = occurrenceBlockId(frame);
  let self = false;
  for (const id of selection) {
    const at = chosenPlace(selection, id);
    if (id === frameId && at === frame) self = true;
    else if (!occurrenceInside(at, frame, byId)) return null;
  }
  return self ? "self" : "inside";
}
