import * as Y from "yjs";
import { appendYgoRecoveryUpdate, compactYgoRecoveryUpdates } from "@/offline";
import { createCollaborationRecoveryCollector } from "./collaborationRecovery";

const RECOVERY_COLLECTION_MS = 100;
const MAX_RECOVERY_BATCH_BYTES = 1 << 20;
const MAX_RECOVERY_UPDATE_BYTES = 64 << 20;

export type PageCopyFailureStage = "browser_recovery_append" | "browser_recovery_compaction";

/**
 * This device's copy of an open page's text (offline.ts): the room's
 * updates and this tab's own collected and appended as they come, the
 * appended ones compacted after, a piece at a time while more wait. A
 * failure to keep it is told with its stage; stop() ends the compaction
 * (the collector is disposed by its owner, which waits for what it holds).
 */
export function keepPageCopy(userId: string, pageId: string, failed: (error: unknown, stage: PageCopyFailureStage) => void) {
  let stopped = false;
  let compactionTimer: number | null = null;
  let compactionRunning = false;
  let compactionRequested = false;
  const scheduleCompaction = () => {
    if (stopped) return;
    compactionRequested = true;
    if (compactionRunning || compactionTimer !== null) return;
    compactionTimer = window.setTimeout(() => {
      compactionTimer = null;
      void runCompaction();
    }, 0);
  };
  const runCompaction = async () => {
    if (stopped || compactionRunning) return;
    compactionRunning = true;
    compactionRequested = false;
    let hasMore = false;
    let failure = false;
    try {
      hasMore = await compactYgoRecoveryUpdates(userId, pageId, (updates) => Y.mergeUpdates(updates));
    } catch (error) {
      failure = true;
      compactionRequested = false;
      failed(error, "browser_recovery_compaction");
    } finally {
      compactionRunning = false;
    }
    if (!failure && (hasMore || compactionRequested)) scheduleCompaction();
  };
  const recovery = createCollaborationRecoveryCollector({
    append: async (update, local) => {
      const ready = await appendYgoRecoveryUpdate(userId, pageId, update, local);
      if (ready) scheduleCompaction();
    },
    merge: (updates) => Y.mergeUpdates(updates),
    onFailure: (error) => failed(error, "browser_recovery_append"),
    schedule: (callback, delayMs) => window.setTimeout(callback, delayMs),
    cancel: (timer) => window.clearTimeout(timer),
    collectionMs: RECOVERY_COLLECTION_MS,
    maxBatchBytes: MAX_RECOVERY_BATCH_BYTES,
    maxUpdateBytes: MAX_RECOVERY_UPDATE_BYTES,
  });
  const stop = () => {
    stopped = true;
    compactionRequested = false;
    if (compactionTimer !== null) window.clearTimeout(compactionTimer);
    compactionTimer = null;
  };
  return { recovery, scheduleCompaction, stop };
}
