import {
  collectCursorPage,
  collectKeyCursorPage,
  requestResult,
  runIndexedDbTransaction,
} from "./outboxIndexedDbPrimitives";
import {
  applyOutboxIndexedDbMigrations,
  OUTBOX_BY_ENTRY_KEY_SEQUENCE_INDEX,
  OUTBOX_BY_SEQUENCE_INDEX,
  OUTBOX_BY_TAB_ID_INDEX,
  OUTBOX_BY_TAB_SEQUENCE_INDEX,
  OUTBOX_ENTRY_STORE,
  OUTBOX_INDEXED_DB_SCHEMA_VERSION,
  OUTBOX_META_SEQUENCE_KEY,
  OUTBOX_META_STORE,
} from "./outboxIndexedDbSchema";
import type { OutboxEntry } from "./outboxOperationTypes";
import {
  assertOutboxEntryKey,
  assertOutboxEntryStep,
  assertOutboxTabId,
  assertOutboxTabStep,
  outboxCompositeKey,
  outboxDestinationSupersedesSource,
  outboxMutationMatches,
  validateOutboxStorageBounds,
  validateOutboxStorageMutationBatch,
  type OutboxEntryPage,
  type OutboxStorageMutation,
  type OutboxStorageMutationResult,
  type OutboxStorageBounds,
  type OutboxStorageModel,
  type OutboxTabIdPage,
} from "./outboxStorageModel";

export interface OutboxIndexedDbBounds {
  maxDatabaseNameCharacters: number;
  storage: OutboxStorageBounds;
}

export interface OutboxIndexedDbStorageOptions {
  beforeSchemaMigration?: (version: number) => void;
  bounds: OutboxIndexedDbBounds;
  factory?: IDBFactory;
  name: string;
}

export interface OutboxIndexedDbStorage extends OutboxStorageModel {
  close(): void;
}

interface StoredOutboxEntry extends OutboxEntry {
  storageKey: string;
}

function assertDatabaseOptions(options: OutboxIndexedDbStorageOptions): void {
  if (
    !Number.isSafeInteger(options.bounds.maxDatabaseNameCharacters)
    || options.bounds.maxDatabaseNameCharacters <= 0
    || options.name.length > options.bounds.maxDatabaseNameCharacters
  ) {
    throw new RangeError("Browser outbox database name exceeds its configured bound.");
  }
  validateOutboxStorageBounds(options.bounds.storage);
}

function publicEntry(stored: StoredOutboxEntry): OutboxEntry {
  return {
    entryKey: stored.entryKey,
    seq: stored.seq,
    tabId: stored.tabId,
    updatedAt: stored.updatedAt,
    value: stored.value,
  };
}

function entryPage(
  page: { hasMore: boolean; values: StoredOutboxEntry[] },
): OutboxEntryPage {
  const entries = page.values.map(publicEntry);
  return {
    entries,
    nextAfterSeq:
      page.hasMore && entries.length > 0 ? entries[entries.length - 1].seq : null,
  };
}

function sequenceRange(keyRange: typeof IDBKeyRange, afterSeq: number | null): IDBKeyRange {
  return keyRange.lowerBound(afterSeq ?? 0, afterSeq !== null);
}

function groupedSequenceRange(
  keyRange: typeof IDBKeyRange,
  group: string,
  afterSeq: number | null,
): IDBKeyRange {
  return keyRange.bound(
    [group, afterSeq ?? 0],
    [group, Number.MAX_SAFE_INTEGER],
    afterSeq !== null,
    false,
  );
}

function resolveFactory(factory?: IDBFactory): IDBFactory | null {
  if (factory) return factory;
  try {
    return globalThis.indexedDB ?? null;
  } catch {
    return null;
  }
}

function resolveKeyRange(): typeof IDBKeyRange | null {
  try {
    return globalThis.IDBKeyRange ?? null;
  } catch {
    return null;
  }
}

export function createIndexedDbOutboxStorage(
  options: OutboxIndexedDbStorageOptions,
): OutboxIndexedDbStorage | null {
  assertDatabaseOptions(options);
  const factory = resolveFactory(options.factory);
  const keyRange = resolveKeyRange();
  if (!factory || !keyRange) return null;
  const bounds = options.bounds.storage;
  let databasePromise: Promise<IDBDatabase> | null = null;
  let openedDatabase: IDBDatabase | null = null;
  // Given up past the store's wait, as another tab may hold the database (deviceStoreLock.ts).
  const inTransaction = <T>(transaction: IDBTransaction, work: () => Promise<T>) => (
    runIndexedDbTransaction(transaction, work, options.name)
  );

  const open = (): Promise<IDBDatabase> => {
    if (databasePromise) return databasePromise;
    databasePromise = new Promise((resolve, reject) => {
      let settled = false;
      let upgradeError: unknown = null;
      const request = factory.open(options.name, OUTBOX_INDEXED_DB_SCHEMA_VERSION);
      request.onupgradeneeded = (event) => {
        if (settled) {
          request.transaction?.abort();
          return;
        }
        if (!request.transaction) {
          upgradeError = new Error("Browser outbox schema upgrade has no transaction.");
          return;
        }
        try {
          applyOutboxIndexedDbMigrations(
            request.result,
            request.transaction,
            event.oldVersion,
            event.newVersion,
            options.beforeSchemaMigration,
          );
        } catch (cause) {
          upgradeError = cause;
        }
      };
      request.onsuccess = () => {
        if (settled) {
          request.result.close();
          return;
        }
        settled = true;
        openedDatabase = request.result;
        openedDatabase.onversionchange = () => {
          openedDatabase?.close();
          openedDatabase = null;
          databasePromise = null;
        };
        resolve(openedDatabase);
      };
      request.onerror = () => {
        if (settled) return;
        settled = true;
        reject(upgradeError ?? request.error ?? new Error("Browser outbox database open failed."));
      };
      request.onblocked = () => {
        if (settled) return;
        settled = true;
        reject(new Error("Browser outbox database upgrade was blocked."));
      };
    });
    databasePromise.catch(() => {
      databasePromise = null;
    });
    return databasePromise;
  };

  async function applyBatch(
    mutations: readonly OutboxStorageMutation[],
  ): Promise<OutboxStorageMutationResult[]> {
    validateOutboxStorageMutationBatch(mutations, bounds);
    if (mutations.length === 0) return [];
    const database = await open();
    const transaction = database.transaction(
      [OUTBOX_ENTRY_STORE, OUTBOX_META_STORE],
      "readwrite",
    );
    return inTransaction(transaction, async () => {
      const store = transaction.objectStore(OUTBOX_ENTRY_STORE);
      const meta = transaction.objectStore(OUTBOX_META_STORE);
      const results: OutboxStorageMutationResult[] = [];
      for (const mutation of mutations) {
        const storageKey = outboxCompositeKey(mutation.tabId, mutation.entryKey);
        const existing = await requestResult(
          store.get(storageKey) as IDBRequest<StoredOutboxEntry | undefined>,
        );
        if (!outboxMutationMatches(existing, mutation.ifUpdatedAt)) {
          results.push({ applied: false });
          continue;
        }
        if (mutation.kind === "remove") {
          if (existing) await requestResult(store.delete(storageKey));
          results.push({ applied: existing !== undefined });
          continue;
        }
        let seq = existing?.seq;
        if (seq === undefined) {
          const current = await requestResult(
            meta.get(OUTBOX_META_SEQUENCE_KEY) as IDBRequest<number | undefined>,
          );
          if (current !== undefined && (!Number.isSafeInteger(current) || current < 0)) {
            throw new Error("Browser outbox sequence metadata is invalid.");
          }
          if (current === Number.MAX_SAFE_INTEGER) {
            throw new RangeError("Browser outbox sequence space is exhausted.");
          }
          seq = (current ?? 0) + 1;
          await requestResult(meta.put(seq, OUTBOX_META_SEQUENCE_KEY));
        }
        await requestResult(store.put({
          entryKey: mutation.entryKey,
          seq,
          storageKey,
          tabId: mutation.tabId,
          updatedAt: mutation.updatedAt,
          value: mutation.value,
        }));
        results.push({ applied: true });
      }
      return results;
    });
  }

  return {
    applyBatch,

    async claimTabStep({ afterSeq, fromTabId, limit, toTabId }) {
      assertOutboxTabId(fromTabId, bounds);
      assertOutboxTabId(toTabId, bounds);
      assertOutboxEntryStep(afterSeq, limit, bounds);
      const database = await open();
      const transaction = database.transaction(OUTBOX_ENTRY_STORE, "readwrite");
      return inTransaction(transaction, async () => {
        const store = transaction.objectStore(OUTBOX_ENTRY_STORE);
        const sourcePage = await collectCursorPage<StoredOutboxEntry>(
          store.index(OUTBOX_BY_TAB_SEQUENCE_INDEX).openCursor(
            groupedSequenceRange(keyRange, fromTabId, afterSeq),
          ),
          limit,
        );
        if (fromTabId === toTabId) return entryPage(sourcePage);

        const claimed: OutboxEntry[] = [];
        for (const source of sourcePage.values) {
          const destinationKey = outboxCompositeKey(toTabId, source.entryKey);
          const destination = await requestResult(
            store.get(destinationKey) as IDBRequest<StoredOutboxEntry | undefined>,
          );
          if (destination && outboxDestinationSupersedesSource(destination, source)) {
            await requestResult(store.delete(source.storageKey));
            continue;
          }
          if (destination) await requestResult(store.delete(destination.storageKey));
          await requestResult(store.delete(source.storageKey));
          const moved = { ...source, storageKey: destinationKey, tabId: toTabId };
          await requestResult(store.put(moved));
          claimed.push(publicEntry(moved));
        }
        return {
          entries: claimed,
          nextAfterSeq:
            sourcePage.hasMore && sourcePage.values.length > 0
              ? sourcePage.values[sourcePage.values.length - 1].seq
              : null,
        };
      });
    },

    async clear() {
      const database = await open();
      const transaction = database.transaction(
        [OUTBOX_ENTRY_STORE, OUTBOX_META_STORE],
        "readwrite",
      );
      return inTransaction(transaction, async () => {
        await requestResult(transaction.objectStore(OUTBOX_ENTRY_STORE).clear());
        await requestResult(transaction.objectStore(OUTBOX_META_STORE).clear());
      });
    },

    async countEntries() {
      const database = await open();
      const transaction = database.transaction(OUTBOX_ENTRY_STORE, "readonly");
      return inTransaction(transaction, () => requestResult(
        transaction.objectStore(OUTBOX_ENTRY_STORE).count(),
      ));
    },

    close() {
      openedDatabase?.close();
      openedDatabase = null;
      databasePromise = null;
    },

    async listEntries({ afterSeq, limit, tabId }) {
      assertOutboxEntryStep(afterSeq, limit, bounds);
      if (tabId !== undefined) assertOutboxTabId(tabId, bounds);
      const database = await open();
      const transaction = database.transaction(OUTBOX_ENTRY_STORE, "readonly");
      return inTransaction(transaction, async () => {
        const store = transaction.objectStore(OUTBOX_ENTRY_STORE);
        const index = store.index(
          tabId === undefined ? OUTBOX_BY_SEQUENCE_INDEX : OUTBOX_BY_TAB_SEQUENCE_INDEX,
        );
        const range = tabId === undefined
          ? sequenceRange(keyRange, afterSeq)
          : groupedSequenceRange(keyRange, tabId, afterSeq);
        return entryPage(await collectCursorPage<StoredOutboxEntry>(
          index.openCursor(range),
          limit,
        ));
      });
    },

    async listEntriesByKey({ afterSeq, entryKey, limit }) {
      assertOutboxEntryKey(entryKey, bounds);
      assertOutboxEntryStep(afterSeq, limit, bounds);
      const database = await open();
      const transaction = database.transaction(OUTBOX_ENTRY_STORE, "readonly");
      return inTransaction(transaction, async () => entryPage(
        await collectCursorPage<StoredOutboxEntry>(
          transaction.objectStore(OUTBOX_ENTRY_STORE)
            .index(OUTBOX_BY_ENTRY_KEY_SEQUENCE_INDEX)
            .openCursor(groupedSequenceRange(keyRange, entryKey, afterSeq)),
          limit,
        ),
      ));
    },

    async listTabIds({ afterTabId, limit }): Promise<OutboxTabIdPage> {
      assertOutboxTabStep(afterTabId, limit, bounds);
      const database = await open();
      const transaction = database.transaction(OUTBOX_ENTRY_STORE, "readonly");
      return inTransaction(transaction, async () => {
        const range = afterTabId === null ? null : keyRange.lowerBound(afterTabId, true);
        const page = await collectKeyCursorPage<string>(
          transaction.objectStore(OUTBOX_ENTRY_STORE)
            .index(OUTBOX_BY_TAB_ID_INDEX)
            .openKeyCursor(range, "nextunique"),
          limit,
        );
        return {
          nextAfterTabId:
            page.hasMore && page.values.length > 0
              ? page.values[page.values.length - 1]
              : null,
          tabIds: page.values,
        };
      });
    },

    async put({ entryKey, tabId, updatedAt, value }) {
      await applyBatch([{ entryKey, kind: "put", tabId, updatedAt, value }]);
    },

    async remove(tabId, entryKey) {
      await applyBatch([{ entryKey, kind: "remove", tabId }]);
    },
  };
}
