import type {
  NativePageMutationLegacyTurn,
  NativePageMutationReplayTurn,
} from "./nativePageMutationOutbox";
import { createRetryPacing, type RetryPacing } from "./retryPacing";

export type NativePageMutationRecoveryStage = "legacy" | "replay" | "refresh";

export interface NativePageMutationRecovery {
  dispose(): void;
  /** A turn, and its end; `now` for a person's press, past the pause after one that waited for the server. */
  flush(now?: boolean): Promise<void>;
  trigger(): void;
}

const BUSY_RETRY_MS = 500;
const BUSY_RETRY_MAX_MS = 2_000;

export function createNativePageMutationRecovery(options: {
  isOnline(): boolean;
  migrateLegacy(): Promise<NativePageMutationLegacyTurn>;
  /** A turn ended with nothing waiting here, whoever sent it. */
  onCaughtUp?: () => void;
  onCommitted?: (count: number) => Promise<void> | void;
  onDeferred?: () => void;
  /** What waits, waits only for pages an operation holds for now: it is sent again soon, without a word. */
  onBusy?: () => void;
  onError?: (error: unknown, stage: NativePageMutationRecoveryStage) => void;
  /** When a turn goes after one that waited for the server (retryPacing.ts). */
  pacing?: RetryPacing;
  replay(): Promise<NativePageMutationReplayTurn>;
  schedule?: (work: () => Promise<void>) => void;
}): NativePageMutationRecovery {
  const schedule = options.schedule ?? ((work: () => Promise<void>) => {
    globalThis.setTimeout(() => void work(), 0);
  });
  let committedSincePublication = 0;
  let disposed = false;
  let running = false;
  let scheduled = false;
  let triggerAfterRun = false;
  // While an operation holds a page (a move, a database turned into a page),
  // its writes go again soon after — not half a minute later as for a server
  // away — so the page reads again once it is let go (BUSY_RETRY_MS, doubling).
  let busyTimer: ReturnType<typeof globalThis.setTimeout> | undefined;
  let busyTries = 0;
  const retryWhileBusy = () => {
    if (disposed || busyTimer !== undefined) return;
    const delay = Math.min(BUSY_RETRY_MAX_MS, BUSY_RETRY_MS * 2 ** busyTries);
    busyTries += 1;
    busyTimer = globalThis.setTimeout(() => { busyTimer = undefined; queueTurn(); }, delay);
  };
  const waiters = new Set<{
    reject(error: unknown): void;
    resolve(): void;
  }>();

  const settleWaiters = (error?: unknown) => {
    const current = [...waiters];
    waiters.clear();
    for (const waiter of current) {
      if (error === undefined) waiter.resolve();
      else waiter.reject(error);
    }
  };

  const reportError = (error: unknown, stage: NativePageMutationRecoveryStage) => {
    try {
      options.onError?.(error, stage);
    } catch {
      // A presentation callback never replaces the recovery failure.
    }
  };

  const startTurn = () => {
    if (disposed || scheduled) return;
    if (running) {
      triggerAfterRun = true;
      return;
    }
    scheduled = true;
    schedule(async () => {
      scheduled = false;
      await runTurn();
    });
  };
  // Whatever asks for a turn — the server answering again, the browser back
  // online, a change kept, the window shown — gets one only once the pause
  // after a turn that waited for the server has passed (retryPacing.ts), one
  // turn however often it was asked meanwhile: every one of them asked again
  // as a write that failed at once marked the server away and the next read
  // answered marked it back (2026-11-21, ~100 sends in half a second).
  const pacing = options.pacing ?? createRetryPacing();
  const queueTurn = (now = false) => {
    if (disposed || scheduled) return;
    if (running) {
      triggerAfterRun = true;
      return;
    }
    pacing.request(startTurn, now);
  };

  const runTurn = async () => {
    if (disposed) return;
    if (!options.isOnline()) {
      settleWaiters();
      return;
    }
    if (running) {
      triggerAfterRun = true;
      return;
    }
    running = true;
    let shouldContinue = false;
    let failure: unknown;
    try {
      let legacy: NativePageMutationLegacyTurn;
      try {
        legacy = await options.migrateLegacy();
      } catch (error) {
        reportError(error, "legacy");
        failure = error;
        return;
      }
      let replay: NativePageMutationReplayTurn;
      try {
        replay = await options.replay();
      } catch (error) {
        reportError(error, "replay");
        failure = error;
        return;
      }
      // What the server refused and the outbox dropped is read back from it too.
      committedSincePublication += replay.committed + (replay.refused ?? 0);
      shouldContinue = legacy.hasMore || replay.hasMore;
      // A page held for now has its own pause (retryWhileBusy).
      if (replay.deferred && !replay.busy) pacing.waited();
      else pacing.answered();
      if (replay.deferred && replay.busy) {
        options.onBusy?.();
        retryWhileBusy();
        shouldContinue = false;
      } else if (replay.deferred) {
        options.onDeferred?.();
        shouldContinue = false;
      }
      if (!replay.busy) busyTries = 0;
      if (!shouldContinue && committedSincePublication > 0) {
        try {
          await options.onCommitted?.(committedSincePublication);
          committedSincePublication = 0;
        } catch (error) {
          reportError(error, "refresh");
          failure = error;
        }
      }
      if (!shouldContinue && !replay.deferred && failure === undefined) options.onCaughtUp?.();
    } finally {
      running = false;
      const next = shouldContinue || triggerAfterRun;
      triggerAfterRun = false;
      if (next) queueTurn();
      else settleWaiters(failure);
    }
  };

  return {
    dispose() {
      disposed = true;
      pacing.dispose();
      if (busyTimer !== undefined) globalThis.clearTimeout(busyTimer);
      settleWaiters(new Error("Page-mutation recovery is disposed."));
    },
    flush(now = false) {
      if (disposed) {
        return Promise.reject(new Error("Page-mutation recovery is disposed."));
      }
      return new Promise<void>((resolve, reject) => {
        waiters.add({ reject, resolve });
        queueTurn(now);
      });
    },
    trigger: () => queueTurn(),
  };
}
