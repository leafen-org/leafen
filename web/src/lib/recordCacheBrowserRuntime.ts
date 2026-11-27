import { requestDeviceStoreLock, type DeviceStoreLockManager } from "./deviceStoreLock";
import { awaitLocalDurableStoreGate, resolveLocalDurableStoreBox } from "./localDurableStoreBox";
import { localBoxIfSettled, onLocalEncryptionModeChange, type LocalBoxGate } from "./localLock";
import { settleRecordCacheCustody } from "./recordCacheCustodyCarry";
import {
  createIndexedDbRecordCacheStorage,
  type RecordCacheIndexedDbStorage,
} from "./recordCacheIndexedDbStorage";
import { createRecordCacheMetaBatch } from "./recordCacheMetaBatch";
import { createRecordCacheSealedStorage } from "./recordCacheSealedStorage";
import {
  createRecordCacheStorage,
  type RecordCacheMetaManyStorage,
  type RecordCacheMetaWrite,
  type RecordCacheRecord,
  type RecordCacheStorageBounds,
  type RecordCacheTablePage,
  type RecordCacheTableStepInput,
} from "./recordCacheStorageModel";

export type RecordCacheWebLockManager = DeviceStoreLockManager;

const MAX_CUSTODY_ATTEMPTS = 3;

export interface RecordCacheBrowserRuntimeBounds {
  database: { maxDatabaseNameCharacters: number };
  maxUserIdCharacters: number;
  storage: RecordCacheStorageBounds;
}

export interface RecordCacheBrowserRuntimeOptions {
  bounds: RecordCacheBrowserRuntimeBounds;
  factory?: IDBFactory;
  locks?: RecordCacheWebLockManager | null;
  onCustodyInvalidated?: () => void;
  schemaVersion: number;
  /** Kept apart from other servers' caches in the same origin. */
  scope?: string;
  userId: string;
}

export interface RecordCacheBrowserRuntime extends RecordCacheMetaManyStorage {
  dispose(): Promise<void>;
  /**
   * Runs one task of the cache's work with its metadata read once and
   * written together when it ends (recordCacheMetaBatch.ts).
   */
  runTask?(task: (runtime: RecordCacheBrowserRuntime) => Promise<void>): Promise<void>;
}

export type RecordCacheBrowserRuntimeOpenResult =
  | { kind: "network-only" }
  | { kind: "unavailable" }
  | { kind: "ready"; runtime: RecordCacheBrowserRuntime };

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
  supplied: RecordCacheWebLockManager | null | undefined,
): RecordCacheWebLockManager | null {
  if (supplied !== undefined) return supplied;
  try {
    const candidate = globalThis.navigator?.locks;
    return candidate && typeof candidate.request === "function"
      ? candidate as unknown as RecordCacheWebLockManager
      : null;
  } catch {
    return null;
  }
}

function createReadyRuntime(
  cache: RecordCacheMetaManyStorage,
  rawStorage: RecordCacheIndexedDbStorage,
  options: RecordCacheBrowserRuntimeOptions,
  locks: RecordCacheWebLockManager | null,
  lockName: string,
): RecordCacheBrowserRuntime {
  const inFlight = new Set<Promise<unknown>>();
  const subscriptions = new Set<() => void>();
  let disposed = false;
  let disposePromise: Promise<void> | null = null;

  const assertActive = (): void => {
    if (disposed) throw new Error("Browser record-cache runtime is disposed.");
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
      const guarded = locks
        ? requestDeviceStoreLock(locks, lockName, async () => {
            assertActive();
            return operation();
          })
        : operation();
      return track(guarded);
    } catch (error) {
      return Promise.reject(error);
    }
  };

  const runtime: RecordCacheBrowserRuntime = {
    clear() {
      return start(() => cache.clear());
    },

    dispose() {
      if (disposePromise) return disposePromise;
      disposed = true;
      for (const unsubscribe of [...subscriptions]) unsubscribe();
      disposePromise = Promise.allSettled([...inFlight]).then(() => {
        rawStorage.close();
      });
      return disposePromise;
    },

    getMeta<V = unknown>(key: string): Promise<V | undefined> {
      return start(() => cache.getMeta<V>(key));
    },

    getMetaMany(keys: readonly string[]): Promise<unknown[]> {
      return start(() => cache.getMetaMany(keys));
    },

    listTableStep<V = unknown>(
      input: RecordCacheTableStepInput,
    ): Promise<RecordCacheTablePage<V>> {
      return start(() => cache.listTableStep<V>(input));
    },

    putRecords<V = unknown>(
      table: string,
      records: readonly RecordCacheRecord<V>[],
    ): Promise<void> {
      return start(() => cache.putRecords(table, records));
    },

    removeMeta(key: string): Promise<void> {
      return start(() => cache.removeMeta(key));
    },

    removeRecords(table: string, ids: readonly string[]): Promise<void> {
      return start(() => cache.removeRecords(table, ids));
    },

    replaceTable<V = unknown>(
      table: string,
      records: readonly RecordCacheRecord<V>[],
    ): Promise<void> {
      return start(() => cache.replaceTable(table, records));
    },

    async runTask(task) {
      const batch = createRecordCacheMetaBatch(runtime);
      let failure: { error: unknown } | null = null;
      try {
        await task(batch.storage);
      } catch (error) {
        failure = { error };
      }
      try {
        await batch.commit();
      } catch (error) {
        if (!failure) throw error;
      }
      if (failure) throw failure.error;
    },

    setMeta(key: string, value: unknown): Promise<void> {
      return start(() => cache.setMeta(key, value));
    },

    writeMeta(entries: readonly RecordCacheMetaWrite[]): Promise<void> {
      return start(() => cache.writeMeta(entries));
    },
  };

  let custodyInvalidated = false;
  const unsubscribeMode = onLocalEncryptionModeChange(() => {
    if (custodyInvalidated || disposed) return;
    custodyInvalidated = true;
    void runtime.dispose().then(() => options.onCustodyInvalidated?.());
  });
  subscriptions.add(unsubscribeMode);
  return runtime;
}

export async function createRecordCacheBrowserRuntime(
  options: RecordCacheBrowserRuntimeOptions,
): Promise<RecordCacheBrowserRuntimeOpenResult> {
  assertPositiveSafeInteger(options.bounds.maxUserIdCharacters, "maxUserIdCharacters");
  assertPositiveSafeInteger(options.schemaVersion, "record-cache schemaVersion");
  assertIdentity(options.userId, options.bounds.maxUserIdCharacters, "userId");
  const migrationLockName = `leafen-outbox:${options.userId}${options.scope ?? ""}`;
  const storageName = `leafen-records:${options.userId}${options.scope ?? ""}`;
  if (storageName.length > options.bounds.database.maxDatabaseNameCharacters) {
    throw new RangeError("Browser record-cache database name exceeds its configured bound.");
  }
  const locks = resolveLocks(options.locks);
  const openExclusive = <T>(work: () => Promise<T>): Promise<T> => (
    locks
      ? requestDeviceStoreLock(locks, migrationLockName, work)
      : work()
  );

  // The person's answer to the passphrase lock comes first, outside the
  // store's lock (localDurableStoreBox.ts). A lock turned on or off while
  // the store's lock was awaited carried the store to its next key: the
  // store opens with that one instead.
  for (let attempt = 0; attempt < MAX_CUSTODY_ATTEMPTS; attempt += 1) {
    const gate = await awaitLocalDurableStoreGate(options.userId);
    const opened = await openExclusive(async (): Promise<RecordCacheBrowserRuntimeOpenResult | null> => {
      if (localBoxIfSettled(options.userId) !== gate) return null;
      return openUnderCustody(options, gate, storageName, locks, migrationLockName);
    });
    if (opened) return opened;
  }
  return { kind: "unavailable" };
}

async function openUnderCustody(
  options: RecordCacheBrowserRuntimeOptions,
  gate: LocalBoxGate,
  storageName: string,
  locks: RecordCacheWebLockManager | null,
  migrationLockName: string,
): Promise<RecordCacheBrowserRuntimeOpenResult> {
  const custody = await resolveLocalDurableStoreBox({
    ...(options.factory === undefined ? {} : { factory: options.factory }),
    gate,
    maxDatabaseNameCharacters: options.bounds.database.maxDatabaseNameCharacters,
    storeName: storageName,
    userId: options.userId,
  });
  if (custody.kind === "network-only") return { kind: "network-only" };
  if (custody.kind === "unavailable") return { kind: "unavailable" };

  const rawStorage = createIndexedDbRecordCacheStorage({
    ...(options.factory === undefined ? {} : { factory: options.factory }),
    maxDatabaseNameCharacters: options.bounds.database.maxDatabaseNameCharacters,
    name: storageName,
  });
  if (!rawStorage) return { kind: "unavailable" };
  try {
    await settleRecordCacheCustody(
      rawStorage,
      custody.kind === "plaintext" ? "plaintext" : gate === "device" ? "device" : "passphrase",
    );
    const storage = custody.kind === "sealed"
      ? createRecordCacheSealedStorage(rawStorage, {
          box: custody.box,
          maxRecordsPerMutation: options.bounds.storage.maxRecordsPerMutation,
          maxRecordsPerStep: options.bounds.storage.maxRecordsPerStep,
          storageName,
        })
      : rawStorage;
    const cache = createRecordCacheStorage(storage, {
      bounds: options.bounds.storage,
      schemaVersion: options.schemaVersion,
    });
    return {
      kind: "ready",
      runtime: createReadyRuntime(
        cache,
        rawStorage,
        options,
        locks,
        migrationLockName,
      ),
    };
  } catch (cause) {
    rawStorage.close();
    throw cause;
  }
}
