import {
  changeLocalDataPassphrase,
  createLocalDataPassphraseBox,
  localBoxIfSettled,
  primeUnlockedGate,
  removeLocalDataPassphraseBox,
  resetGateToDevice,
  setLocalEncryptionMode,
  unlockLocalData,
} from "./localLock";
import { createLocalLockSettings } from "./localLockSettings";
import type { LocalSecretBox } from "./localSecretBox";
import { localFirstProductCache } from "./localFirstProductCache";
import { logProductError } from "./safeErrorLog";

const MAX_FLUSH_TURNS = 32;

export interface LocalLockMutationOutbox {
  clearEmptyUnderCustody(): Promise<void>;
  hasPending(): Promise<boolean>;
  replay(): Promise<{
    committed: number;
    deferred: boolean;
    hasMore: boolean;
  }>;
  resetCustody(): Promise<void>;
  runCustodyExclusive<Result>(work: () => Promise<Result>): Promise<Result>;
}

export function createBrowserLocalLockSettings(options: {
  mutationOutbox: LocalLockMutationOutbox;
  userId: string;
}) {
  const observePassphraseFailure = (stage: string, error: unknown) => {
    logProductError("warn", "local-lock.settings", error, {
      causeStage: stage,
    });
  };
  return createLocalLockSettings<LocalSecretBox>({
    changeBox: (currentPassphrase, nextPassphrase) => (
      changeLocalDataPassphrase(
        options.userId,
        currentPassphrase,
        nextPassphrase,
        { onFailure: observePassphraseFailure },
      )
    ),
    clearCaches: async (userId, change) => {
      await localFirstProductCache.carryUnderCustody(userId, change);
      await options.mutationOutbox.clearEmptyUnderCustody();
    },
    createBox: (passphrase) => createLocalDataPassphraseBox(
      options.userId,
      passphrase,
      { onFailure: observePassphraseFailure },
    ),
    async flushPending() {
      for (let turn = 0; turn < MAX_FLUSH_TURNS; turn += 1) {
        if (!(await options.mutationOutbox.hasPending())) return;
        const result = await options.mutationOutbox.replay();
        if (result.deferred || !result.hasMore) return;
      }
    },
    pendingCount: async () => await options.mutationOutbox.hasPending() ? 1 : 0,
    onFailure: observePassphraseFailure,
    prime: primeUnlockedGate,
    removeBox: () => removeLocalDataPassphraseBox(options.userId),
    resetDurableRuntimes: async () => {
      await Promise.all([
        options.mutationOutbox.resetCustody(),
        localFirstProductCache.resetRuntime(),
      ]);
    },
    resetToDevice: resetGateToDevice,
    runCustodyExclusive: (work) => options.mutationOutbox.runCustodyExclusive(work),
    setMode: setLocalEncryptionMode,
    async unlockFirst(passphrase) {
      const settled = localBoxIfSettled(options.userId);
      // Opened here already, or no lock to open.
      if (settled !== "pending" && settled !== null) return "ok";
      const opened = await unlockLocalData(options.userId, passphrase);
      // Online only had opened the stores without their box: they open again with it.
      if (opened === "ok" && settled === null) {
        await Promise.all([options.mutationOutbox.resetCustody(), localFirstProductCache.resetRuntime()]);
      }
      return opened;
    },
    userId: () => options.userId,
  });
}

export type BrowserLocalLockSettings = ReturnType<
  typeof createBrowserLocalLockSettings
>;
