import type { GraphBlock } from "@/contentGraph";
import type { PageGraphStore } from "@/lib/pageGraphStore";
import { keptOutOfSyncedBlocks } from "@/lib/blockSemantics";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import { syncedBlockSource } from "./syncedBlockModel";

/** A page on one side of a synced copy's edge: the copy's page, or its original's. */
export type SyncedCopySide = Pick<NativeEditorGraphAdapter, "pageId" | "workspaceId" | "session" | "graph" | "graphSync" | "readOnly" | "collaborationDocument">;

/**
 * The original's page as a copy shows it, with the graph its editor draws
 * and the open page's, which draws the copy when it holds it: blocks crossing
 * the edge show there at the press (syncedCopyMove).
 */
export type SyncedOriginalSide = SyncedCopySide & { store: PageGraphStore; copyStore: PageGraphStore | null };

/**
 * Blocks crossing a synced copy's edge by Cmd/Ctrl+Shift+Arrow, as Notion's
 * do (seen 2026-11-09): from the copy's content, shown from another page,
 * out beside the copy; or from beside the copy into its original there.
 */
export type SyncedCopyCrossing = { kind: "leave" } | { kind: "enter"; copy: GraphBlock };

// The original's page as each copy of it on another page shows it, by the
// copy's page and the copy (SyncedOriginalEditor).
const shownOriginals = new Map<string, () => SyncedOriginalSide>();

/** The original's page a copy on another page shows, while it shows it. */
export function registerSyncedOriginalShown(copyPageId: string, copyId: string, side: () => SyncedOriginalSide) {
  const key = `${copyPageId}/${copyId}`;
  shownOriginals.set(key, side);
  return () => { if (shownOriginals.get(key) === side) shownOriginals.delete(key); };
}

/** The original's page a copy on that page shows, while it shows it. */
export function syncedOriginalShown(copyPageId: string, copyId: string) {
  return shownOriginals.get(`${copyPageId}/${copyId}`)?.();
}

/** Whether these blocks may cross a copy's edge: a synced block stays out of another page's. */
function crossable(adapter: NativeEditorGraphAdapter, blocks: readonly GraphBlock[]) {
  return !adapter.readOnly && !!adapter.session && !blocks.some((block) => keptOutOfSyncedBlocks(block.type));
}

/** Whether blocks leaving this editor's root go beside the copy, on another page, that shows it. */
export function syncedCopyLeavable(adapter: NativeEditorGraphAdapter, blocks: readonly GraphBlock[]) {
  const copy = adapter.moveContext?.syncedCopy, page = copy?.page();
  return crossable(adapter, blocks) && !!page && !page.readOnly && page.graph.blocks.some((block) => block.id === copy!.blockId);
}

/** Whether blocks beside this copy may go into its original on another page. */
export function syncedCopyEnterable(adapter: NativeEditorGraphAdapter, copy: GraphBlock, blocks: readonly GraphBlock[]) {
  const page = syncedOriginalShown(adapter.pageId, copy.id);
  const original = syncedBlockSource(copy, adapter.pageId).blockId;
  return crossable(adapter, blocks) && !!page && !page.readOnly && page.graph.blocks.some((block) => block.id === original);
}

// One crossing at a time: keys pressed again before the blocks have arrived
// move nothing (moveSelectedBlocks), where they are not yet.
let crossingNow = false;

/** Whether blocks are crossing a synced copy's edge now. */
export function syncedCopyCrossingNow() {
  return crossingNow;
}

/** Blocks start or end crossing a synced copy's edge (syncedCopyMove). */
export function setSyncedCopyCrossing(crossing: boolean) {
  crossingNow = crossing;
}
