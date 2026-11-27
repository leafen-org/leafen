export type CollaborationRecoveryCollectorOptions = {
  /** `local` says whether the batch holds this device's own edits. */
  append: (update: Uint8Array, local: boolean) => Promise<void>;
  merge: (updates: Uint8Array[]) => Uint8Array;
  onFailure: (error: unknown) => void;
  schedule: (callback: () => void, delayMs: number) => number;
  cancel: (timer: number) => void;
  collectionMs: number;
  maxBatchBytes: number;
  maxUpdateBytes: number;
};

export type CollaborationRecoveryCollector = {
  collect: (update: Uint8Array, local?: boolean) => void;
  flush: () => Promise<void>;
  whenIdle: () => Promise<void>;
  dispose: () => Promise<void>;
};

// A read or write of the device's copy the browser fails is tried again
// before it counts as failed (2026-10-26: iOS Safari's IndexedDB failed about
// one append in four, "…from database without an in-progress transaction",
// and the edit was gone from the copy; the next try went in). Trying again is
// safe: a failed write wrote nothing, and a copy read again applies the same
// updates again.
const RECOVERY_RETRY_DELAYS_MS = [250, 1_000] as const;

export async function tryingRecoveryAgain<T>(work: () => Promise<T>, wait: (delayMs: number) => Promise<void>): Promise<T> {
  for (let attempt = 0; ; attempt += 1) {
    try {
      return await work();
    } catch (error) {
      const delay = RECOVERY_RETRY_DELAYS_MS[attempt];
      if (delay === undefined) throw error;
      await wait(delay);
    }
  }
}

export function createCollaborationRecoveryCollector(
  options: CollaborationRecoveryCollectorOptions,
): CollaborationRecoveryCollector {
  let pending: Uint8Array[] = [];
  let pendingBytes = 0;
  let pendingLocal = false;
  let timer: number | null = null;
  let disposed = false;
  let writeChain = Promise.resolve();

  const cancelTimer = () => {
    if (timer === null) return;
    options.cancel(timer);
    timer = null;
  };

  const queuePending = () => {
    cancelTimer();
    if (pending.length === 0) return writeChain;
    const batch = pending;
    const local = pendingLocal;
    pending = [];
    pendingBytes = 0;
    pendingLocal = false;
    let merged: Uint8Array;
    try {
      merged = batch.length === 1 ? batch[0] : options.merge(batch);
      if (merged.byteLength > options.maxUpdateBytes) {
        throw new RangeError("Collaboration recovery update exceeds the durable bound.");
      }
    } catch (error) {
      options.onFailure(error);
      return writeChain;
    }
    writeChain = writeChain
      .then(() => appendTryingAgain(merged, local))
      .catch((error) => { options.onFailure(error); });
    return writeChain;
  };

  const appendTryingAgain = (merged: Uint8Array, local: boolean) => tryingRecoveryAgain(
    () => options.append(merged, local),
    (delayMs) => new Promise<void>((resolve) => { options.schedule(resolve, delayMs); }),
  );

  const schedulePending = () => {
    cancelTimer();
    timer = options.schedule(() => {
      timer = null;
      void queuePending();
    }, options.collectionMs);
  };

  return {
    collect(update, local = false) {
      if (disposed) return;
      if (update.byteLength > options.maxUpdateBytes) {
        options.onFailure(new RangeError("Collaboration recovery update exceeds the durable bound."));
        return;
      }
      if (pendingBytes > 0 && pendingBytes + update.byteLength > options.maxBatchBytes) {
        void queuePending();
      }
      pending.push(update.slice());
      pendingBytes += update.byteLength;
      pendingLocal ||= local;
      if (pendingBytes >= options.maxBatchBytes) void queuePending();
      else schedulePending();
    },
    flush() {
      return queuePending();
    },
    whenIdle() {
      return writeChain;
    },
    dispose() {
      disposed = true;
      return queuePending();
    },
  };
}

/**
 * What of a page's text its copy here takes, and when. The room's updates,
 * as they come; the copy's own, replayed, are in it already. This tab's own
 * text, as it goes to the server (`keepOwn`): held behind the block changes
 * made with it, it is kept once they are saved (lib/textBehindBlockChanges.ts)
 * — kept as it was written, a tab opening the page in this browser meanwhile
 * drew it from the copy beside the server's blocks from before it, a merged
 * line beside its emptied one (2026-11-13). Before the room opens, all of it
 * is this tab's own.
 */
export function recoveryIntake(collector: Pick<CollaborationRecoveryCollector, "collect">, replayed: unknown, room: () => unknown) {
  const fromCopy = new WeakSet<Uint8Array>();
  return {
    collect(update: Uint8Array, origin: unknown) {
      const from = room();
      if (origin === replayed) fromCopy.add(update);
      else if (from === null) collector.collect(update, true);
      else if (origin === from) collector.collect(update, false);
    },
    keepOwn(update: Uint8Array) {
      if (!fromCopy.has(update)) collector.collect(update, true);
    },
  };
}
