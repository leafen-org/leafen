import {
  createOutboxCrossTabInvalidation,
  type OutboxCrossTabInvalidationBounds,
  type OutboxCrossTabInvalidationHost,
} from "./outboxCrossTabInvalidation";
import {
  createIndexedDbOutboxStorage,
  type OutboxIndexedDbBounds,
  type OutboxIndexedDbStorage,
} from "./outboxIndexedDbStorage";
import { awaitLocalDurableStoreGate, resolveLocalDurableStoreBox } from "./localDurableStoreBox";
import { onLocalEncryptionModeChange } from "./localLock";
import {
  createOutboxPendingSignals,
  type OutboxPendingSettlement,
  type OutboxPendingSignalBounds,
  type OutboxSettlementOutcome,
} from "./outboxPendingSignals";
import {
  createOutboxSettlementCoordinator,
  type OutboxDurabilityReceipt,
  type OutboxRecoveryResult,
  type OutboxSettlementCoordinatorBounds,
} from "./outboxSettlementCoordinator";
import { createOutboxSealedStorage } from "./outboxSealedStorage";
import { outboxRememberingEmpty } from "./outboxEmptyMemory";
import type { OutboxStorageModel } from "./outboxStorageModel";
import type { OutboxCausalityBounds } from "./outboxOperationCausality";
import type {
  OutboxEntry,
  OutboxOp,
  RejectedMutationRecoveryOutboxOp,
} from "./outboxOperationTypes";
import { logProductError } from "./safeErrorLog";
import { requestDeviceStoreLane, requestDeviceStoreLock, type DeviceStoreLockManager } from "./deviceStoreLock";

export type OutboxWebLockManager = DeviceStoreLockManager;

export interface OutboxBrowserRuntimeBounds {
  coordinator: OutboxSettlementCoordinatorBounds;
  database: OutboxIndexedDbBounds;
  invalidation: OutboxCrossTabInvalidationBounds;
  maxUserIdCharacters: number;
  operation: OutboxCausalityBounds;
  pending: OutboxPendingSignalBounds;
}

export interface OutboxBrowserRuntimeOptions {
  bounds: OutboxBrowserRuntimeBounds;
  clock?: () => number;
  factory?: IDBFactory;
  invalidationHost?: OutboxCrossTabInvalidationHost | null;
  locks?: OutboxWebLockManager | null;
  onCustodyInvalidated?: () => void;
  /** Kept apart from other servers' outboxes in the same origin. */
  scope?: string;
  tabId: string;
  userId: string;
}

export interface OutboxReplayStepInput {
  afterSeq: number | null;
  limit: number;
}

export interface OutboxReplayItem {
  entry: OutboxEntry;
  receipt: OutboxDurabilityReceipt;
}

export interface OutboxReplayStep {
  items: OutboxReplayItem[];
  nextAfterSeq: number | null;
}

export interface OutboxBrowserRuntime {
  advanceRecovery(
    receipt: OutboxDurabilityReceipt,
    recovery?: RejectedMutationRecoveryOutboxOp,
  ): Promise<OutboxRecoveryResult>;
  clearEmptyUnderCustody(): Promise<void>;
  discardAllEntries(): Promise<void>;
  dispose(): Promise<void>;
  durablePendingCount(): Promise<number>;
  hasAnyEntries(): Promise<boolean>;
  listReplayStep(input: OutboxReplayStepInput): Promise<OutboxReplayStep>;
  pendingCount(): number;
  runCustodyExclusive<T>(work: () => Promise<T>): Promise<T>;
  /**
   * The outbox's sending lane, for a change just made or a replay: taken
   * over from a tab that keeps it past the wait, and `taken` tells the work
   * when another tab took it over in turn (deviceStoreLock.ts).
   */
  runReplayExclusive<T>(work: (taken: AbortSignal) => Promise<T>): Promise<T>;
  set(entryKey: string, operation: OutboxOp): Promise<OutboxDurabilityReceipt>;
  settle(
    receipt: OutboxDurabilityReceipt,
    outcome: OutboxSettlementOutcome,
  ): Promise<boolean>;
  settleAsRecovery(
    receipt: OutboxDurabilityReceipt,
    recovery: RejectedMutationRecoveryOutboxOp,
  ): Promise<OutboxRecoveryResult>;
  subscribeChanges(listener: () => void): () => void;
  subscribePending(
    listener: (pendingCount: number, settlement?: OutboxPendingSettlement) => void,
  ): () => void;
}

export type OutboxBrowserRuntimeOpenResult =
  | { kind: "network-only" }
  | { kind: "unavailable" }
  | { kind: "ready"; runtime: OutboxBrowserRuntime };

function assertPositiveSafeInteger(value: number, label: string): void {
  if (!Number.isSafeInteger(value) || value <= 0) {
    throw new RangeError(`${label} must be a positive safe integer.`);
  }
}

function assertIdentity(value: string, maximum: number, label: string): void {
  if (typeof value !== "string" || value.length === 0 || value.length > maximum) {
    throw new RangeError(`${label} exceeds its configured character bound.`);
  }
}

function resolveLocks(
  supplied: OutboxWebLockManager | null | undefined,
): OutboxWebLockManager | null {
  if (supplied !== undefined) return supplied;
  try {
    const candidate = globalThis.navigator?.locks;
    return candidate && typeof candidate.request === "function"
      ? candidate as unknown as OutboxWebLockManager
      : null;
  } catch {
    return null;
  }
}

function observeRuntimeError(error: unknown, stage: string): void {
  logProductError("warn", "outbox.persistence", error, { causeStage: stage });
}

function createReadyRuntime(
  options: OutboxBrowserRuntimeOptions,
  name: string,
  locks: OutboxWebLockManager | null,
  keptStorage: OutboxStorageModel,
  databaseStorage: OutboxIndexedDbStorage,
): OutboxBrowserRuntime {
  // This tab reads an outbox it found empty again only once something was
  // written to it, here or by another tab (outboxEmptyMemory.ts).
  const emptyMemory = outboxRememberingEmpty(keptStorage, options.tabId, options.clock ?? Date.now);
  const storage = emptyMemory.storage;
  const pending = createOutboxPendingSignals(options.bounds.pending, {
    onListenerError: (error) => observeRuntimeError(error, "outbox.pending.listener"),
  });
  const invalidation = createOutboxCrossTabInvalidation(
    options.bounds.invalidation,
    {
      ...(options.invalidationHost === undefined
        ? {}
        : { host: options.invalidationHost }),
      onError: (error) => observeRuntimeError(error, "outbox.invalidation.browser"),
    },
  );
  invalidation.subscribe(emptyMemory.forget);
  // The store's own steps wait for its lock as long as this tab's steps
  // before them hold it, and for another tab's only so long
  // (deviceStoreLock.ts). One that waited out a lock change writes nothing
  // sealed with the key that change let go: it fails as an unavailable
  // store does, as the record cache's steps do (recordCacheBrowserRuntime.ts).
  const runExclusive = <T>(work: () => Promise<T>): Promise<T> => (
    locks
      ? requestDeviceStoreLock(locks, name, () => {
          if (disposed) throw new Error("Browser outbox runtime is disposed.");
          return work();
        })
      : work()
  );
  const coordinator = createOutboxSettlementCoordinator({
    bounds: options.bounds.coordinator,
    clock: options.clock ?? Date.now,
    invalidation,
    operationBounds: options.bounds.operation,
    pending,
    runExclusive,
    storage,
    tabId: options.tabId,
    userId: options.userId,
  });
  const inFlight = new Set<Promise<unknown>>();
  const subscriptions = new Set<() => void>();
  let disposed = false;
  let disposePromise: Promise<void> | null = null;

  const assertActive = () => {
    if (disposed) throw new Error("Browser outbox runtime is disposed.");
  };
  const track = <T>(operation: Promise<T>): Promise<T> => {
    inFlight.add(operation);
    void operation.then(
      () => inFlight.delete(operation),
      () => inFlight.delete(operation),
    );
    return operation;
  };
  const start = <T>(operation: () => Promise<T>): Promise<T> => {
    try {
      assertActive();
      return track(operation());
    } catch (error) {
      return Promise.reject(error);
    }
  };
  const subscribe = (start: () => () => void): (() => void) => {
    assertActive();
    const stop = start();
    let active = true;
    const unsubscribe = () => {
      if (!active) return;
      active = false;
      subscriptions.delete(unsubscribe);
      stop();
    };
    subscriptions.add(unsubscribe);
    return unsubscribe;
  };

  const runtime: OutboxBrowserRuntime = {
    advanceRecovery(receipt, recovery) {
      return start(() => coordinator.advanceRecovery(receipt, recovery));
    },

    clearEmptyUnderCustody() {
      return start(async () => {
        const page = await storage.listEntries({ afterSeq: null, limit: 1 });
        if (page.entries.length > 0) {
          throw new Error("Durable outbox must be empty before custody changes.");
        }
        // The caller already owns the same user-scoped Web Lock. Clear raw
        // schema metadata without trying to open old ciphertext through the
        // next custody key.
        await databaseStorage.clear();
      });
    },

    discardAllEntries() {
      return start(async () => {
        // Only an explicit sign-out discard reaches here. The clear takes the
        // same user-scoped lock as every durable batch so no write lands
        // halfway through it, and the pending count drops with it.
        await runExclusive(() => storage.clear());
        pending.clearUser(options.userId);
        try {
          invalidation.schedule();
        } catch (error) {
          observeRuntimeError(error, "outbox.invalidation.discard");
        }
      });
    },

    dispose() {
      if (disposePromise) return disposePromise;
      disposed = true;
      for (const unsubscribe of [...subscriptions]) unsubscribe();
      disposePromise = Promise.allSettled([...inFlight]).then(() => {
        try {
          invalidation.dispose();
        } catch (error) {
          observeRuntimeError(error, "outbox.invalidation.dispose");
        }
        try {
          databaseStorage.close();
        } catch (error) {
          observeRuntimeError(error, "outbox.storage.close");
        }
      });
      return disposePromise;
    },

    durablePendingCount() {
      return start(() => storage.countEntries());
    },

    hasAnyEntries() {
      return start(async () => (
        await storage.listEntries({ afterSeq: null, limit: 1 })
      ).entries.length > 0);
    },

    listReplayStep(input) {
      return start(async () => {
        const page = await storage.listEntries({
          afterSeq: input.afterSeq,
          limit: input.limit,
          tabId: options.tabId,
        });
        return {
          items: page.entries.map((entry) => ({
            entry,
            receipt: {
              entryKey: entry.entryKey,
              generation: pending.generation(options.userId, entry.entryKey),
              tabId: entry.tabId,
              updatedAt: entry.updatedAt,
              userId: options.userId,
            },
          })),
          nextAfterSeq: page.nextAfterSeq,
        };
      });
    },

    pendingCount() {
      assertActive();
      return pending.pendingCount(options.userId);
    },

    runCustodyExclusive(work) {
      return start(() => (
        locks
          ? requestDeviceStoreLock(locks, name, work, 10_000)
          : Promise.reject(new Error(
              "Cross-tab custody exclusion is unavailable.",
            ))
      ));
    },

    // Taken over from a tab that keeps it past the wait (2026-10-17: a tab
    // left holding it held every other tab's sending).
    runReplayExclusive(work) {
      return start(() => (
        locks
          ? requestDeviceStoreLane(locks, `${name}:replay`, work)
          : work(new AbortController().signal)
      ));
    },

    set(entryKey, operation) {
      return start(() => coordinator.set(entryKey, operation));
    },

    settle(receipt, outcome) {
      return start(() => coordinator.settle(receipt, outcome));
    },

    settleAsRecovery(receipt, recovery) {
      return start(() => coordinator.settleAsRecovery(receipt, recovery));
    },

    subscribeChanges(listener) {
      return subscribe(() => invalidation.subscribe(listener));
    },

    subscribePending(listener) {
      return subscribe(() => pending.subscribe((event) => {
        if (event.userId !== options.userId) return;
        listener(event.pendingCount, event.settlement);
      }));
    },
  };
  let custodyInvalidated = false;
  const unsubscribeMode = onLocalEncryptionModeChange(() => {
    if (custodyInvalidated || disposed) return;
    custodyInvalidated = true;
    void runtime.dispose().then(() => {
      try {
        options.onCustodyInvalidated?.();
      } catch (error) {
        observeRuntimeError(error, "outbox.custody.rebuild");
      }
    });
  });
  subscriptions.add(unsubscribeMode);
  return runtime;
}

export async function createOutboxBrowserRuntime(
  options: OutboxBrowserRuntimeOptions,
): Promise<OutboxBrowserRuntimeOpenResult> {
  assertPositiveSafeInteger(options.bounds.maxUserIdCharacters, "maxUserIdCharacters");
  assertIdentity(
    options.userId,
    Math.min(
      options.bounds.maxUserIdCharacters,
      options.bounds.pending.maxUserIdCharacters,
    ),
    "userId",
  );
  assertIdentity(
    options.tabId,
    options.bounds.database.storage.maxTabIdCharacters,
    "tabId",
  );
  const name = `leafen-outbox:${options.userId}${options.scope ?? ""}`;
  if (name.length > options.bounds.database.maxDatabaseNameCharacters) {
    throw new RangeError("Browser outbox database name exceeds its configured bound.");
  }
  const locks = resolveLocks(options.locks);
  const openExclusive = <T>(work: () => Promise<T>): Promise<T> => (
    locks
      ? requestDeviceStoreLock(locks, name, work)
      : work()
  );

  // The person's answer to the passphrase lock comes first, outside the
  // store's lock (localDurableStoreBox.ts).
  const gate = await awaitLocalDurableStoreGate(options.userId);
  return openExclusive(async () => {
    const custody = await resolveLocalDurableStoreBox({
      ...(options.factory === undefined ? {} : { factory: options.factory }),
      gate,
      maxDatabaseNameCharacters: options.bounds.database.maxDatabaseNameCharacters,
      storeName: name,
      userId: options.userId,
    });
    if (custody.kind === "network-only") return { kind: "network-only" };
    if (custody.kind === "unavailable") return { kind: "unavailable" };

    const databaseStorage = createIndexedDbOutboxStorage({
      bounds: options.bounds.database,
      ...(options.factory === undefined ? {} : { factory: options.factory }),
      name,
    });
    if (!databaseStorage) return { kind: "unavailable" };
    const storage = custody.kind === "sealed"
      ? createOutboxSealedStorage(databaseStorage, {
          box: custody.box,
          maxMutationsPerBatch: options.bounds.database.storage.maxMutationsPerBatch,
          storageName: name,
        })
      : databaseStorage;
    return {
      kind: "ready",
      runtime: createReadyRuntime(
        options,
        name,
        locks,
        storage,
        databaseStorage,
      ),
    };
  });
}
