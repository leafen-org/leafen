import { dropUnlockedKeys, holdUnlockedKey } from "./localLockHold";
import {
  snapshotLocalSecretBoxSession,
  type LocalSecretBox,
  type LocalSecretBoxSessionSnapshot,
} from "./localSecretBox";

const MODE_KEY = "leafen.encryption.mode";
const ONLINE_ONLY_SESSION_KEY_PREFIX = "leafen.local-lock.online-only:";
const OLD_HISTORY_UNLOCK_KEY = "__leafenLocalLockSessionV1";

export type LocalEncryptionMode = "device" | "passphrase";
export type LocalBoxGate = LocalSecretBox | null | "device";

export interface LocalLockStorage {
  readonly length: number;
  getItem(key: string): string | null;
  key(index: number): string | null;
  removeItem(key: string): void;
  setItem(key: string, value: string): void;
}

export interface LocalLockGateHost {
  readonly localStorage?: LocalLockStorage;
  readonly sessionStorage?: LocalLockStorage;
  /** Keeps an unlocked key where a reload finds it and the disk doesn't (localLockHold.ts). */
  holdUnlocked?(userId: string, snapshot: LocalSecretBoxSessionSnapshot): void;
  dropUnlocked?(): void;
  readHistoryState(): unknown;
  replaceHistoryState(state: Record<string, unknown>): void;
  subscribeStorage(listener: (key: string | null) => void): () => void;
}

interface Gate {
  promise: Promise<LocalBoxGate>;
  resolve(value: LocalBoxGate): void;
  settled: LocalBoxGate | "pending";
}

export interface LocalLockGateRuntime {
  awaitLocalBox(userId: string): Promise<LocalBoxGate>;
  clearOnlineOnlyChoice(userId?: string): void;
  dispose(): void;
  localBoxIfSettled(userId: string): LocalBoxGate | "pending";
  localEncryptionMode(): LocalEncryptionMode;
  localLockPending(userId: string): boolean;
  onModeChange(listener: () => void): () => void;
  prime(userId: string, box: LocalSecretBox): void;
  reset(userId: string): void;
  resetToDevice(userId: string): void;
  setLocalEncryptionMode(mode: LocalEncryptionMode): void;
  skip(userId: string): void;
}

function record(value: unknown): Record<string, unknown> | null {
  return value && typeof value === "object" && !Array.isArray(value)
    ? value as Record<string, unknown>
    : null;
}

function storageGet(storage: LocalLockStorage | undefined, key: string): string | null {
  try {
    return storage?.getItem(key) ?? null;
  } catch {
    return null;
  }
}

function storageRemove(storage: LocalLockStorage | undefined, key: string): void {
  try {
    storage?.removeItem(key);
  } catch {
    // Restricted browser storage leaves the runtime memory-only.
  }
}

function storageSet(storage: LocalLockStorage | undefined, key: string, value: string): void {
  try {
    storage?.setItem(key, value);
  } catch {
    // Restricted browser storage leaves the runtime memory-only.
  }
}

export function createLocalLockGateRuntime(host: LocalLockGateHost): LocalLockGateRuntime {
  const gates = new Map<string, Gate>();
  const modeChangeListeners = new Set<() => void>();
  let disposed = false;

  const historyState = () => record(host.readHistoryState());
  const replaceHistoryState = (state: Record<string, unknown>) => {
    try {
      host.replaceHistoryState(state);
    } catch {
      // A restricted history API asks again after reload without weakening this tab.
    }
  };
  const onlineOnlyKey = (userId: string) => `${ONLINE_ONLY_SESSION_KEY_PREFIX}${userId}`;

  // Earlier versions kept the key in the tab's history, which reaches the
  // browser's session files: it goes as soon as it is seen.
  const clearOldHistoryGrant = () => {
    const current = historyState();
    if (!current || !(OLD_HISTORY_UNLOCK_KEY in current)) return;
    const next = { ...current };
    delete next[OLD_HISTORY_UNLOCK_KEY];
    replaceHistoryState(next);
  };

  clearOldHistoryGrant();

  const clearOnlineOnlyChoice = (userId?: string) => {
    const storage = host.sessionStorage;
    if (userId) {
      storageRemove(storage, onlineOnlyKey(userId));
      return;
    }
    try {
      const keys = Array.from(
        { length: storage?.length ?? 0 },
        (_, index) => storage?.key(index) ?? null,
      ).filter((key): key is string => key?.startsWith(ONLINE_ONLY_SESSION_KEY_PREFIX) === true);
      for (const key of keys) storageRemove(storage, key);
    } catch {
      // There is no durable online-only choice to clear when storage is unavailable.
    }
  };

  const holdUnlocked = (userId: string, box: LocalSecretBox) => {
    const snapshot = snapshotLocalSecretBoxSession(box);
    if (snapshot) host.holdUnlocked?.(userId, snapshot);
  };

  const localEncryptionMode = (): LocalEncryptionMode => (
    storageGet(host.localStorage, MODE_KEY) === "passphrase" ? "passphrase" : "device"
  );

  const gateFor = (userId: string): Gate => {
    if (disposed) throw new Error("Local-lock gate runtime is disposed.");
    const existing = gates.get(userId);
    if (existing) return existing;
    let settled: LocalBoxGate | "pending" = "pending";
    if (!userId || localEncryptionMode() === "device") settled = "device";
    else if (storageGet(host.sessionStorage, onlineOnlyKey(userId)) === "1") settled = null;
    else settled = "pending";
    if (settled !== "pending") {
      const gate: Gate = {
        promise: Promise.resolve(settled),
        resolve() {},
        settled,
      };
      gates.set(userId, gate);
      return gate;
    }
    let resolve!: (value: LocalBoxGate) => void;
    const promise = new Promise<LocalBoxGate>((accept) => { resolve = accept; });
    const gate: Gate = { promise, resolve, settled: "pending" };
    gates.set(userId, gate);
    return gate;
  };

  const releasePending = (gate: Gate, value: LocalBoxGate) => {
    if (gate.settled !== "pending") return;
    gate.settled = value;
    gate.resolve(value);
  };

  const invalidate = () => {
    for (const gate of gates.values()) releasePending(gate, null);
    gates.clear();
    clearOnlineOnlyChoice();
    host.dropUnlocked?.();
    for (const listener of modeChangeListeners) {
      try {
        listener();
      } catch {
        // Observational subscribers cannot block gate recovery.
      }
    }
  };

  const unsubscribeStorage = host.subscribeStorage((key) => {
    if (key !== null && key !== MODE_KEY) return;
    invalidate();
  });

  return {
    awaitLocalBox(userId) {
      return gateFor(userId).promise;
    },
    clearOnlineOnlyChoice,
    dispose() {
      if (disposed) return;
      disposed = true;
      unsubscribeStorage();
      for (const gate of gates.values()) releasePending(gate, null);
      gates.clear();
      modeChangeListeners.clear();
    },
    localBoxIfSettled(userId) {
      return gateFor(userId).settled;
    },
    localEncryptionMode,
    localLockPending(userId) {
      return gateFor(userId).settled === "pending";
    },
    onModeChange(listener) {
      if (disposed) throw new Error("Local-lock gate runtime is disposed.");
      modeChangeListeners.add(listener);
      return () => modeChangeListeners.delete(listener);
    },
    prime(userId, box) {
      clearOnlineOnlyChoice(userId);
      holdUnlocked(userId, box);
      const current = gates.get(userId);
      if (current) releasePending(current, box);
      gates.set(userId, {
        promise: Promise.resolve(box),
        resolve() {},
        settled: box,
      });
    },
    reset(userId) {
      const current = gates.get(userId);
      if (current) releasePending(current, null);
      gates.delete(userId);
    },
    resetToDevice(userId) {
      clearOnlineOnlyChoice(userId);
      host.dropUnlocked?.();
      const current = gates.get(userId);
      if (current) releasePending(current, "device");
      gates.set(userId, {
        promise: Promise.resolve("device"),
        resolve() {},
        settled: "device",
      });
    },
    setLocalEncryptionMode(mode) {
      const previous = localEncryptionMode();
      clearOnlineOnlyChoice();
      host.dropUnlocked?.();
      if (mode === "passphrase") storageSet(host.localStorage, MODE_KEY, "passphrase");
      else storageRemove(host.localStorage, MODE_KEY);
      if (localEncryptionMode() !== mode) {
        throw new Error("Local encryption mode could not be persisted.");
      }
      // A browser does not dispatch a StorageEvent back to the tab that made
      // the change. Notify same-tab durable runtimes through the same
      // invalidation path that cross-tab changes use.
      if (mode !== previous) invalidate();
    },
    skip(userId) {
      const gate = gateFor(userId);
      if (gate.settled !== "pending") return;
      host.dropUnlocked?.();
      storageSet(host.sessionStorage, onlineOnlyKey(userId), "1");
      releasePending(gate, null);
    },
  };
}

export function createBrowserLocalLockGateRuntime(
  browserWindow: Window | undefined = typeof window === "undefined" ? undefined : window,
): LocalLockGateRuntime {
  return createLocalLockGateRuntime({
    get localStorage() {
      try { return browserWindow?.localStorage; } catch { return undefined; }
    },
    get sessionStorage() {
      try { return browserWindow?.sessionStorage; } catch { return undefined; }
    },
    holdUnlocked: holdUnlockedKey,
    dropUnlocked: dropUnlockedKeys,
    readHistoryState() {
      return browserWindow?.history.state ?? null;
    },
    replaceHistoryState(state) {
      browserWindow?.history.replaceState(state, "", browserWindow.location.href);
    },
    subscribeStorage(listener) {
      if (!browserWindow) return () => {};
      const handle = (event: StorageEvent) => listener(event.key);
      browserWindow.addEventListener("storage", handle);
      return () => browserWindow.removeEventListener("storage", handle);
    },
  });
}
