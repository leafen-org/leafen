import { api } from "@/api";
import { sendContentGraphChange } from "@/lib/contentGraphSend";
import type { Session } from "@/types";

/** As in Notion, an original with more copies than this loses them instead. */
const SYNCED_BLOCK_UNSYNC_LIMIT = 10;

export type SyncedCopiesLetGo = {
  /** Whether the copies were removed rather than unsynced. */
  removed: boolean;
  /** Copies unsynced or removed. */
  done: number;
  /** Copies left as they were, on pages the reader can't change. */
  kept: number;
};

/**
 * Lets an original's copies go, as Notion does when an original is unsynced
 * from all of them or deleted: with at most ten copies each becomes ordinary
 * blocks holding the original's content; with more, once the reader agrees,
 * every copy is removed, which can't be undone. A copy on a page the reader
 * can't change, or one that can't be unsynced, stays as it is. Null when the
 * reader did not agree.
 */
export async function letSyncedCopiesGo(
  session: Session,
  pageId: string,
  originalId: string,
  agreeToRemove: (count: number) => boolean | Promise<boolean>,
): Promise<SyncedCopiesLetGo | null> {
  // Every copy, a window at a time, each window after the last one read: a
  // copy that stays (a page the reader can't change) is passed, not read again.
  const tried = new Set<string>();
  let removed: boolean | null = null;
  let done = 0;
  let kept = 0;
  let after: string | undefined;
  for (;;) {
    const places = await api.syncedBlockLocations(session, pageId, originalId, after);
    const copies = Array.isArray(places?.copies) ? places.copies : [];
    if (removed === null) {
      removed = copies.length > SYNCED_BLOCK_UNSYNC_LIMIT || places.hasMore === true;
      if (removed && !await agreeToRemove(copies.length)) return null;
    }
    const pending = copies.filter((copied) => !tried.has(copied.blockId));
    for (const copied of pending) {
      tried.add(copied.blockId);
      try {
        if (removed) {
          await sendContentGraphChange(session, copied.pageId, [{ kind: "deleteBlock", deletion: { blockId: copied.blockId } }]);
        } else {
          await api.unsyncSyncedBlock(session, copied.pageId, copied.blockId);
        }
        done += 1;
      } catch {
        kept += 1;
      }
    }
    if (!places.hasMore || !places.nextAfter || places.nextAfter === after) break;
    after = places.nextAfter;
  }
  return { removed: removed === true, done, kept };
}
