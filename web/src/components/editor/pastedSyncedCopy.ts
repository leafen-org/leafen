import { contentGraphOutboxOf, sendContentGraphChange } from "@/lib/contentGraphSend";
import { collectContentGraphStructure } from "@/lib/authenticatedContentGraph";
import { localFirstProductCache } from "@/lib/localFirstProductCache";
import { isServerUnreachable } from "@/lib/serverReach";
import { serverIdentity } from "@/lib/serverOrigin";
import { overlayPendingGraphOperations } from "@/components/product/pendingGraphOverlay";
import type { ContentGraph, GraphOperation } from "@/contentGraph";
import type { Session } from "@/types";
import { replaceSelectedBlocks } from "./blockClipboard";
import { wrapInSyncedBlock } from "./blockTurnInto";
import { blockPlaces, returnBlocks } from "./blockDropModel";
import { removeEditorBlocks } from "./editorGraphModel";
import { joinCommittedNativeEditorCommand } from "./nativeEditorUndoOwner";
import { syncedBlockSource } from "./syncedBlockModel";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";

/**
 * How a synced copy's paste ended: made, given up because what was pasted is
 * gone, refused, or not made because the server wasn't there and this device
 * keeps no copy of the original's page.
 */
export type PastedSyncedCopy = "made" | "gone" | "failed" | "unreachable";

/**
 * The original's page as the server has it — or, the server not there, as
 * this device keeps it with the changes waiting to be sent (an edit kept
 * offline is made on the page as it shows here, its changes then sent in the
 * order made, as every edit offline is). "unreachable" when it keeps none.
 */
async function readOriginalPage(session: Session, pageId: string) {
  try {
    const read = await collectContentGraphStructure(session, pageId);
    return read ? { graph: read.graph, canEdit: read.canEdit } : null;
  } catch (error) {
    if (!isServerUnreachable(error)) throw error;
    const kept = await localFirstProductCache.graph.readCache({ key: pageId, principalId: session.user.id, serverKey: serverIdentity() })
      .catch(() => null);
    if (!kept?.complete || kept.key !== pageId) return "unreachable" as const;
    // Change by change, each placing its blocks as the server will (pendingGraphOverlay.ts).
    const writes = await (contentGraphOutboxOf(session)?.pendingGraphWrites(pageId) ?? Promise.resolve([])).catch(() => []);
    const waiting = writes.filter(({ mutationId }) => !kept.value.shown?.includes(mutationId)).map(({ operations }) => operations as GraphOperation[]);
    return { graph: overlayPendingGraphOperations(kept.value.graph, waiting), canEdit: kept.value.canEdit !== false };
  }
}

/** The synced block a lone block already is, which its copy names as it is. */
function syncedOriginal(graph: ContentGraph, pageId: string, blockIds: readonly string[]) {
  const block = blockIds.length === 1 ? graph.blocks.find((candidate) => candidate.id === blockIds[0]) : undefined;
  if (block?.type !== "synced_block") return null;
  const source = syncedBlockSource(block, pageId, graph.blocks);
  return { syncedBlockId: source.blockId, syncedPageId: source.pageId || pageId };
}

/**
 * Where a synced copy goes: in place of the blocks pasted, or right after a
 * block, once `first` has done its part of the same step (a pasted link
 * taken out of its line) and said it could.
 */
export type SyncedCopySpot = readonly string[] | { after: string; first(): boolean };

function putSyncedCopy(adapter: NativeEditorGraphAdapter, spot: SyncedCopySpot, content: Record<string, unknown>) {
  if (!("after" in spot)) return replaceSelectedBlocks(adapter, new Set(spot), [{ type: "synced_block", content }]) !== null;
  return spot.first() && adapter.insertAfter(spot.after, "synced_block", content) !== null;
}

/**
 * Puts a synced copy of blocks where what was pasted here says, as Notion's
 * "동기화된 블록" for blocks pasted from another page and for a pasted link to
 * a block (seen 2026-11-05, 2026-11-11): the blocks become the original of a
 * new synced block where they stand, on their page or this one; a lone block
 * that already is a synced block is copied as it is. ⌘Z puts the blocks back
 * where they were as one step with the copy. `still` says whether what was
 * pasted is still there to replace.
 */
export async function pasteSyncedCopy(
  adapter: NativeEditorGraphAdapter,
  source: { pageId: string; blockIds: readonly string[] },
  spot: SyncedCopySpot,
  still: () => boolean,
  run: (change: () => void) => void,
): Promise<PastedSyncedCopy> {
  const replace = (content: Record<string, unknown>) => {
    let replaced = false;
    run(() => { replaced = putSyncedCopy(adapter, spot, content); });
    return replaced;
  };
  if (source.pageId === adapter.pageId) return pasteSyncedCopyHere(adapter, source.blockIds, spot, run);
  const session = adapter.session;
  if (!session || !still()) return "gone";
  const read = await readOriginalPage(session, source.pageId);
  if (read === "unreachable") return "unreachable";
  const ids = source.blockIds.filter(id => read?.graph.blocks.some(block => block.id === id));
  if (!read || ids.length !== source.blockIds.length) return "failed";
  const named = syncedOriginal(read.graph, source.pageId, ids);
  if (named) return !still() ? "gone" : replace(named) ? "made" : "failed";
  const change = read.canEdit ? wrapInSyncedBlock(read.graph, ids) : null;
  const original = change?.graph.blocks.find(block => block.type === "synced_block" && !read.graph.blocks.some(old => old.id === block.id));
  if (!change || !original) return "failed";
  const back = returnBlocks(change.graph, blockPlaces(read.graph, ids));
  const unwrapped = back ? removeEditorBlocks(back.graph, [original.id]) : null;
  // The original's page is changed as this one is: kept for a server out
  // of reach, in the order made, before the copy here that names it.
  const write = (operations: typeof change.operations) =>
    sendContentGraphChange(session, source.pageId, operations).then(() => true, () => false);
  await sendContentGraphChange(session, source.pageId, change.operations);
  if (!still()) { if (back && unwrapped) void write([...back.operations, ...unwrapped.operations]); return "gone"; }
  const replaced = replace({ syncedBlockId: original.id, syncedPageId: source.pageId });
  if (replaced && back && unwrapped) {
    joinCommittedNativeEditorCommand(`${adapter.workspaceId}/${adapter.pageId}/${session.user.id}`, forward => {
      if (forward) return write(change.operations);
      // Going back, the copy here goes first: an original taken away
      // takes its copies with it, and the undo here then found nothing
      // to take away (2026-11-05).
      window.setTimeout(() => void (adapter.graphSync?.flush() ?? Promise.resolve(true))
        .then(() => write([...back.operations, ...unwrapped.operations])), 0);
      return Promise.resolve(true);
    });
  }
  return replaced ? "made" : "failed";
}

/** A synced copy of blocks of this page, made as one change here. */
function pasteSyncedCopyHere(
  adapter: NativeEditorGraphAdapter,
  blockIds: readonly string[],
  spot: SyncedCopySpot,
  run: (change: () => void) => void,
): PastedSyncedCopy {
  if (!blockIds.every(id => adapter.graph.blocks.some(block => block.id === id))) return "failed";
  let made = false;
  run(() => {
    const named = syncedOriginal(adapter.graph, adapter.pageId, blockIds);
    let content: Record<string, unknown> | null = named;
    if (!content) {
      const before = new Set(adapter.graph.blocks.map(block => block.id));
      if (!adapter.changeLayout(graph => wrapInSyncedBlock(graph, blockIds))) return;
      const original = adapter.graph.blocks.find(block => block.type === "synced_block" && !before.has(block.id));
      if (!original) return;
      content = { syncedBlockId: original.id, syncedPageId: adapter.pageId };
    }
    made = putSyncedCopy(adapter, spot, content);
  });
  return made ? "made" : "failed";
}
