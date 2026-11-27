import type { ContentGraph, GraphBlock } from "@/contentGraph";
import { keptOutOfSyncedBlocks } from "@/lib/blockSemantics";
import { insertEditorBlock, insertEditorChildBlock } from "./editorGraphModel";

export type SyncedBlockSource = {
  /** The original synced block. */
  blockId: string;
  /** The page the original is on, "" when the copy doesn't name it and the
   * original isn't on this page — its place is read from the server. */
  pageId: string;
  /** Whether this block is a copy that shows the original. */
  copy: boolean;
};

/**
 * The original a copy syncing from no block names: one a synced block
 * reference written through the API made from an address naming nothing,
 * which shows nothing, as Notion's (seen twice 2026-11-21: a block of no
 * height; synced_block_sourceless.go).
 */
export const SYNCED_SOURCELESS = "00000000-0000-0000-0000-000000000000";

function text(value: unknown) {
  return typeof value === "string" ? value.trim() : "";
}

/**
 * The original a synced block shows: itself, or the one a copy names. A copy
 * that names only its original (an API or import wrote it before 2026-10-31)
 * is on this page's original when the page holds it, given its blocks.
 */
export function syncedBlockSource(block: GraphBlock, pageId: string, pageBlocks?: readonly GraphBlock[]): SyncedBlockSource {
  const blockId = text(block.content.syncedBlockId) || text(block.content.sourceId);
  if (!blockId) return { blockId: block.id, pageId, copy: false };
  const named = text(block.content.syncedPageId);
  if (named || !pageBlocks) return { blockId, pageId: named || pageId, copy: true };
  return { blockId, pageId: pageBlocks.some((candidate) => candidate.id === blockId) ? pageId : "", copy: true };
}

/**
 * What a synced block duplicated alone (⌘D, the menu's "복제", an ⌥-drag)
 * becomes: a copy of its original, as Notion's (seen 2026-11-14: an
 * original's ⌘D and "복제" made a copy, a copy's another copy of the same
 * original); duplicated with other blocks, or inside one, an original is a
 * new original with its blocks (duplicateEditorBlocks), as a copied page's.
 * Null for any other block.
 */
export function syncedLoneDuplicate(block: GraphBlock, pageId: string, pageBlocks: readonly GraphBlock[]): Record<string, unknown> | null {
  if (block.type !== "synced_block") return null;
  const source = syncedBlockSource(block, pageId, pageBlocks);
  return { syncedBlockId: source.blockId, ...(source.pageId ? { syncedPageId: source.pageId } : {}) };
}

/**
 * The synced block blocks of these types would stand in, placed inside this
 * one (null: the page), when a person may not place them there
 * (keptOutOfSyncedBlocks): the outermost synced block it is or stands in.
 * Null when they may go there.
 */
export function syncedBlockKeepingOut(graph: ContentGraph, holderId: string | null, types: Iterable<string>): GraphBlock | null {
  if (![...types].some(keptOutOfSyncedBlocks)) return null;
  const byId = new Map(graph.blocks.map((block) => [block.id, block]));
  const seen = new Set<string>();
  let outer: GraphBlock | null = null;
  for (let at = holderId ? byId.get(holderId) : undefined; at && !seen.has(at.id); at = at.parentId ? byId.get(at.parentId) : undefined) {
    seen.add(at.id);
    if (at.type === "synced_block") outer = at;
  }
  return outer;
}

/** Whether a synced block made right inside this one (null: the page) is one a person may make there. */
export function syncedBlockMayGoIn(graph: ContentGraph, holderId: string | null) {
  return !syncedBlockKeepingOut(graph, holderId, ["synced_block"]);
}

/** A newly created original starts with a writable paragraph; a copy only links. */
export function insertEditorSyncedBlock(graph: ContentGraph, afterId: string | null, content: Record<string, unknown> = {}) {
  const original = insertEditorBlock(graph, afterId, "synced_block", content);
  if (syncedBlockSource(original.block, "").copy) return original;
  const child = insertEditorChildBlock(original.graph, original.block.id);
  return { block: original.block, graph: child.graph, operations: [...original.operations, ...child.operations] };
}
