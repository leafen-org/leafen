import { copyTextWithMime, readTextWithMime, type ClipboardBounds } from "@/lib/clipboard";
import { clipboardServer, otherClipboardServer } from "@/lib/clipboardServer";
import { newId } from "@/lib/ids";
import { absolutePageUrl } from "@/lib/navigation";
import { blockAnchorId } from "./blockHandleModel";
import type { SyncedBlockSource } from "./syncedBlockModel";

// "Copy and sync" puts a synced block's original on the clipboard, as
// Notion's does: pasting it into a page makes a copy there. Elsewhere — on
// another Leafen server too, which has no such block — it pastes as a link
// to the block.

const LEAFEN_SYNCED_BLOCK_MIME = "application/x-leafen-synced-block";

const SYNCED_BLOCK_CLIPBOARD_BOUNDS: ClipboardBounds = {
  maxTextCharacters: 4_096,
  maxHtmlCharacters: 8_192,
  maxEntries: 8,
  maxEntryCharacters: 4_096,
  maxTotalEntryCharacters: 16_384,
};

export type SyncedBlockClipboard = Pick<SyncedBlockSource, "blockId" | "pageId">;

function escapeHtml(value: string) {
  return value.replaceAll("&", "&amp;").replaceAll('"', "&quot;").replaceAll("<", "&lt;").replaceAll(">", "&gt;");
}

function validId(value: unknown): value is string {
  return typeof value === "string" && value.length > 0 && value.length <= 255 && !/\s/u.test(value);
}

/** Puts the original on the clipboard; label names it where it pastes as a link. */
export async function copySyncedBlock(original: SyncedBlockClipboard, label: string): Promise<boolean> {
  const url = new URL(absolutePageUrl(original.pageId));
  url.hash = blockAnchorId(original.blockId);
  const link = url.toString();
  const html = `<html><body><!--StartFragment--><div data-leafen-copy="true" data-leafen-copy-id="${escapeHtml(newId())}">`
    + `<a href="${escapeHtml(link)}">${escapeHtml(label)}</a></div><!--EndFragment--></body></html>`;
  const payload = JSON.stringify({ version: 1, blockId: original.blockId, pageId: original.pageId, server: clipboardServer() });
  return copyTextWithMime(link, LEAFEN_SYNCED_BLOCK_MIME, payload, { "text/html": html }, SYNCED_BLOCK_CLIPBOARD_BOUNDS);
}

/** The original of this server a paste carries, if it carries one. */
export function pastedSyncedBlock(data: DataTransfer): SyncedBlockClipboard | null {
  const raw = readTextWithMime(data, LEAFEN_SYNCED_BLOCK_MIME, SYNCED_BLOCK_CLIPBOARD_BOUNDS);
  if (!raw) return null;
  try {
    const value = JSON.parse(raw) as Record<string, unknown>;
    if (value?.version !== 1 || !validId(value.blockId) || !validId(value.pageId) || otherClipboardServer(value.server) !== null) return null;
    return { blockId: value.blockId, pageId: value.pageId };
  } catch {
    return null;
  }
}
