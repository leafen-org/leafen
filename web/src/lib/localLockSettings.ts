export type LocalLockChangeResult =
  | "ok"
  | "pending-changes"
  | "unavailable"
  | "wrong-passphrase";

export interface LocalLockBoxResult<Box> {
  box?: Box;
  created?: boolean;
  error?: "unavailable" | "wrong-passphrase";
}

export interface LocalLockSettingsOptions<Box> {
  changeBox(currentPassphrase: string, nextPassphrase: string): Promise<LocalLockBoxResult<Box>>;
  /**
   * Leaves the device's kept copies for the key a lock change goes to:
   * sealed again with it where they can be, cleared otherwise. `"device"` is
   * the device's own key, kept while the lock is off.
   */
  clearCaches(userId: string, change: { from: Box | "device"; to: Box | "device" }): Promise<void>;
  createBox(passphrase: string): Promise<LocalLockBoxResult<Box>>;
  flushPending(): Promise<void>;
  pendingCount(): Promise<number>;
  onFailure?(stage: string, error: unknown): void;
  prime(userId: string, box: Box): void;
  removeBox(): Promise<void>;
  resetDurableRuntimes(): Promise<void> | void;
  resetToDevice(userId: string): void;
  runCustodyExclusive<Result>(work: () => Promise<Result>): Promise<Result>;
  setMode(mode: "device" | "passphrase"): void;
  /**
   * Opens with the passphrase given a lock this page hasn't opened (its unlock
   * prompt unanswered, or online-only chosen), so turning it off or changing
   * it waits on nothing (2026-10-18: it waited on the unanswered prompt).
   */
  unlockFirst?(passphrase: string): Promise<"ok" | "unavailable" | "wrong-passphrase">;
  userId(): string;
}

function boxResult<Box>(
  result: LocalLockBoxResult<Box>,
): { box: Box; created: boolean } | { error: "unavailable" | "wrong-passphrase" } {
  if (result.error) return { error: result.error };
  if (result.box === undefined) return { error: "unavailable" };
  return { box: result.box, created: result.created === true };
}

export function createLocalLockSettings<Box>(
  options: LocalLockSettingsOptions<Box>,
) {
  const observe = async <Result>(
    stage: string,
    operation: () => Promise<Result>,
  ): Promise<Result> => {
    try {
      return await operation();
    } catch (error) {
      try {
        options.onFailure?.(stage, error);
      } catch {
        // Diagnostics are observational and never replace lock behavior.
      }
      throw error;
    }
  };
  const report = (stage: string, error: unknown): void => {
    try {
      options.onFailure?.(stage, error);
    } catch {
      // Diagnostics are observational and never replace lock behavior.
    }
  };
  const resetAfterCommit = async (): Promise<void> => {
    try {
      await options.resetDurableRuntimes();
    } catch (error) {
      // The mode change already invalidated same-tab and cross-tab runtimes.
      // A failed eager reopen must not make the UI deny a committed custody
      // change; later storage access still fails closed and retries the open.
      report("reset-durable-runtimes", error);
    }
  };
  const drainedUser = async (): Promise<
    { kind: "ready"; userId: string }
    | { kind: "pending" }
    | { kind: "unavailable" }
  > => {
    const userId = options.userId();
    if (!userId) return { kind: "unavailable" };
    await observe("flush-pending", options.flushPending);
    return await observe("pending-count", options.pendingCount) === 0
      ? { kind: "ready", userId }
      : { kind: "pending" };
  };

  return {
    async enable(passphrase: string): Promise<LocalLockChangeResult> {
      const drained = await drainedUser();
      if (drained.kind !== "ready") {
        return drained.kind === "pending" ? "pending-changes" : "unavailable";
      }
      if (!passphrase) return "unavailable";
      const { userId } = drained;
      const result = await observe("custody", () => options.runCustodyExclusive(async () => {
        if (await observe("custody-pending-count", options.pendingCount) > 0) {
          return "pending-changes";
        }
        const created = boxResult(await observe(
          "create-box",
          () => options.createBox(passphrase),
        ));
        if ("error" in created) return created.error;
        try {
          await observe("clear-caches", () => options.clearCaches(userId, { from: "device", to: created.box }));
          options.setMode("passphrase");
          options.prime(userId, created.box);
        } catch (error) {
          if (created.created) {
            try {
              await options.removeBox();
            } catch (rollbackError) {
              report("rollback-created-box", rollbackError);
            }
          }
          throw error;
        }
        return "ok";
      }));
      if (result !== "ok") return result;
      await resetAfterCommit();
      return "ok";
    },

    async change(
      currentPassphrase: string,
      nextPassphrase: string,
    ): Promise<LocalLockChangeResult> {
      const userId = options.userId();
      if (!userId || !currentPassphrase || !nextPassphrase) return "unavailable";
      const opened = await observe("unlock-first", () => options.unlockFirst?.(currentPassphrase) ?? Promise.resolve("ok" as const));
      if (opened !== "ok") return opened;
      const result = await observe("custody", () => options.runCustodyExclusive(async () => {
        const changed = boxResult(await observe(
          "change-box",
          () => options.changeBox(currentPassphrase, nextPassphrase),
        ));
        if ("error" in changed) return changed.error;
        options.prime(userId, changed.box);
        return "ok";
      }));
      if (result !== "ok") return result;
      await resetAfterCommit();
      return "ok";
    },

    async disable(passphrase: string): Promise<LocalLockChangeResult> {
      if (!passphrase) return "unavailable";
      const opened = await observe("unlock-first", () => options.unlockFirst?.(passphrase) ?? Promise.resolve("ok" as const));
      if (opened !== "ok") return opened;
      const drained = await drainedUser();
      if (drained.kind !== "ready") {
        return drained.kind === "pending" ? "pending-changes" : "unavailable";
      }
      const { userId } = drained;
      const result = await observe("custody", () => options.runCustodyExclusive(async () => {
        if (await observe("custody-pending-count", options.pendingCount) > 0) {
          return "pending-changes";
        }
        const verified = boxResult(await observe(
          "create-box",
          () => options.createBox(passphrase),
        ));
        if ("error" in verified) return verified.error;
        await observe("clear-caches", () => options.clearCaches(userId, { from: verified.box, to: "device" }));
        options.setMode("device");
        options.resetToDevice(userId);
        try {
          await observe("remove-box", options.removeBox);
        } catch (error) {
          try {
            options.setMode("passphrase");
            options.prime(userId, verified.box);
          } catch (rollbackError) {
            report("rollback-device-mode", rollbackError);
          }
          throw error;
        }
        return "ok";
      }));
      if (result !== "ok") return result;
      await resetAfterCommit();
      return "ok";
    },
  };
}
