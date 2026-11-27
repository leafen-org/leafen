import {
  applyRecordCacheIndexedDbMigrations,
  RECORD_CACHE_BY_TABLE_INDEX,
  RECORD_CACHE_INDEXED_DB_SCHEMA_VERSION,
  RECORD_CACHE_META_STORE,
  RECORD_CACHE_RECORD_STORE,
} from "./recordCacheIndexedDbSchema";
import type {
  RecordCacheRecord,
  RecordCacheStorageAdapter,
  RecordCacheTablePage,
  RecordCacheTableStepInput,
} from "./recordCacheStorageModel";
import {
  createRecordCacheTableWrites,
  type RecordCacheTableWrite,
  writtenValues,
  wroteValue,
} from "./recordCacheTableWrites";

export interface RecordCacheIndexedDbStorageOptions {
  beforeSchemaMigration?: (version: number) => void;
  factory?: IDBFactory;
  maxDatabaseNameCharacters: number;
  name: string;
}

/** A value as the store keeps it, sealed or not, under its own key. */
export interface RecordCacheStoredEntry {
  key: string;
  value: unknown;
}

export interface RecordCacheStoredRecord extends RecordCacheStoredEntry {
  id: string;
  table: string;
}

/** A step of what is kept; the next step starts after `nextAfterKey`, null at the end. */
export interface RecordCacheStoredStep<Entry> {
  entries: Entry[];
  nextAfterKey: string | null;
}

/** What a step puts back where it was listed; an entry whose value is undefined goes. */
export interface RecordCacheStoredRewrite {
  meta: readonly RecordCacheStoredEntry[];
  records: readonly RecordCacheStoredRecord[];
}

export interface RecordCacheIndexedDbStorage extends RecordCacheStorageAdapter {
  close(): void;
  /**
   * The store's records and metadata as kept, in key order after `afterKey`,
   * which a change of the lock seals again a step at a time
   * (recordCacheCustodyCarry.ts).
   */
  listStoredMeta(afterKey: string | null, limit: number): Promise<RecordCacheStoredStep<RecordCacheStoredEntry>>;
  listStoredRecords(afterKey: string | null, limit: number): Promise<RecordCacheStoredStep<RecordCacheStoredRecord>>;
  rewriteStored(rewrite: RecordCacheStoredRewrite): Promise<void>;
}

interface StoredRecordCacheRecord extends RecordCacheRecord {
  key: string;
  table: string;
  /** The table write that put it, which writes after it check (recordCacheTableWrites.ts). */
  writer?: string;
}

function requestResult<T>(request: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(
      request.error ?? new Error("Browser record-cache request failed."),
    );
  });
}

function transactionDone(transaction: IDBTransaction): Promise<void> {
  return new Promise((resolve, reject) => {
    transaction.oncomplete = () => resolve();
    transaction.onabort = () => reject(
      transaction.error ?? new Error("Browser record-cache transaction was aborted."),
    );
    transaction.onerror = () => reject(
      transaction.error ?? new Error("Browser record-cache transaction failed."),
    );
  });
}

async function runTransaction<T>(
  transaction: IDBTransaction,
  work: () => Promise<T>,
): Promise<T> {
  const done = transactionDone(transaction);
  try {
    const result = await work();
    await done;
    return result;
  } catch (cause) {
    try {
      transaction.abort();
    } catch {
      // The transaction may already have completed or aborted.
    }
    await done.catch(() => undefined);
    throw cause;
  }
}

function assertDatabaseOptions(options: RecordCacheIndexedDbStorageOptions): void {
  if (
    typeof options.name !== "string"
    || !Number.isSafeInteger(options.maxDatabaseNameCharacters)
    || options.maxDatabaseNameCharacters <= 0
    || options.name.length > options.maxDatabaseNameCharacters
  ) {
    throw new RangeError("Browser record-cache database name exceeds its configured bound.");
  }
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

function storageKey(table: string, id: string): string {
  return `${encodeURIComponent(table)} ${encodeURIComponent(id)}`;
}

function publicRecord(stored: StoredRecordCacheRecord): RecordCacheRecord {
  return { id: stored.id, value: stored.value };
}

function collectTablePage(
  request: IDBRequest<IDBCursorWithValue | null>,
  table: string,
  afterId: string | null,
  limit: number,
): Promise<RecordCacheTablePage> {
  return new Promise((resolve, reject) => {
    const values: RecordCacheRecord[] = [];
    const afterKey = afterId === null ? null : storageKey(table, afterId);
    let positioned = afterKey === null;
    request.onerror = () => reject(
      request.error ?? new Error("Browser record-cache cursor failed."),
    );
    request.onsuccess = () => {
      const cursor = request.result;
      if (!cursor) {
        resolve({ nextAfterId: null, records: values });
        return;
      }
      if (!positioned && afterKey !== null) {
        positioned = true;
        if (String(cursor.primaryKey) <= afterKey) {
          cursor.continuePrimaryKey(table, `${afterKey}\u0000`);
          return;
        }
      }
      if (values.length >= limit) {
        resolve({
          nextAfterId: values.length > 0 ? values[values.length - 1].id : null,
          records: values,
        });
        return;
      }
      values.push(publicRecord(cursor.value as StoredRecordCacheRecord));
      cursor.continue();
    };
  });
}

// A step walks `limit` keys, whether or not each gives an entry, so the next
// one starts after the last key walked.
function collectStoredStep<Entry>(
  request: IDBRequest<IDBCursorWithValue | null>,
  limit: number,
  entry: (cursor: IDBCursorWithValue) => Entry | null,
): Promise<RecordCacheStoredStep<Entry>> {
  return new Promise((resolve, reject) => {
    const entries: Entry[] = [];
    let walked = 0;
    let lastKey: string | null = null;
    request.onerror = () => reject(
      request.error ?? new Error("Browser record-cache cursor failed."),
    );
    request.onsuccess = () => {
      const cursor = request.result;
      if (!cursor) {
        resolve({ entries, nextAfterKey: null });
        return;
      }
      if (walked >= limit) {
        resolve({ entries, nextAfterKey: lastKey });
        return;
      }
      walked += 1;
      if (typeof cursor.primaryKey === "string") lastKey = cursor.primaryKey;
      const next = entry(cursor);
      if (next) entries.push(next);
      cursor.continue();
    };
  });
}

// Whether a table holds what this store last wrote it with: as many records,
// and one that write put still carries its mark.
async function writtenAsBefore(
  store: IDBObjectStore,
  count: IDBRequest<number>,
  table: string,
  before: RecordCacheTableWrite,
): Promise<boolean> {
  if (await requestResult(count) !== before.values.size || before.checkId === null) return false;
  const stored = await requestResult(store.get(storageKey(table, before.checkId))) as StoredRecordCacheRecord | undefined;
  return stored?.writer === before.writer;
}

// Puts each metadata entry, or deletes it when its value is undefined.
async function writeMetaEntries(
  store: IDBObjectStore,
  entries: readonly RecordCacheStoredEntry[],
): Promise<void> {
  for (const entry of entries) {
    if (entry.value === undefined) await requestResult(store.delete(entry.key));
    else await requestResult(store.put(entry.value, entry.key));
  }
}

function deleteTableRecords(
  request: IDBRequest<IDBCursorWithValue | null>,
): Promise<void> {
  return new Promise((resolve, reject) => {
    request.onerror = () => reject(
      request.error ?? new Error("Browser record-cache cursor failed."),
    );
    request.onsuccess = () => {
      const cursor = request.result;
      if (!cursor) {
        resolve();
        return;
      }
      const deletion = cursor.delete();
      deletion.onerror = () => reject(
        deletion.error ?? new Error("Browser record-cache deletion failed."),
      );
      deletion.onsuccess = () => cursor.continue();
    };
  });
}

export function createIndexedDbRecordCacheStorage(
  options: RecordCacheIndexedDbStorageOptions,
): RecordCacheIndexedDbStorage | null {
  assertDatabaseOptions(options);
  const factory = resolveFactory(options.factory);
  const keyRange = resolveKeyRange();
  if (!factory || !keyRange) return null;
  let databasePromise: Promise<IDBDatabase> | null = null;
  let openedDatabase: IDBDatabase | null = null;
  const tableWrites = createRecordCacheTableWrites();

  const open = (): Promise<IDBDatabase> => {
    if (databasePromise) return databasePromise;
    databasePromise = new Promise((resolve, reject) => {
      let settled = false;
      let upgradeError: unknown = null;
      const request = factory.open(options.name, RECORD_CACHE_INDEXED_DB_SCHEMA_VERSION);
      request.onupgradeneeded = (event) => {
        if (settled) {
          request.transaction?.abort();
          return;
        }
        if (!request.transaction) {
          upgradeError = new Error("Browser record-cache schema upgrade has no transaction.");
          return;
        }
        try {
          applyRecordCacheIndexedDbMigrations(
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
        reject(
          upgradeError
          ?? request.error
          ?? new Error("Browser record-cache database open failed."),
        );
      };
      request.onblocked = () => {
        if (settled) return;
        settled = true;
        reject(new Error("Browser record-cache database upgrade was blocked."));
      };
    });
    void databasePromise.catch(() => {
      databasePromise = null;
    });
    return databasePromise;
  };

  return {
    async clear() {
      tableWrites.forget();
      const database = await open();
      const transaction = database.transaction(
        [RECORD_CACHE_RECORD_STORE, RECORD_CACHE_META_STORE],
        "readwrite",
      );
      return runTransaction(transaction, async () => {
        await requestResult(transaction.objectStore(RECORD_CACHE_RECORD_STORE).clear());
        await requestResult(transaction.objectStore(RECORD_CACHE_META_STORE).clear());
      });
    },

    close() {
      openedDatabase?.close();
      openedDatabase = null;
      databasePromise = null;
    },

    async getMeta(key) {
      const database = await open();
      const transaction = database.transaction(RECORD_CACHE_META_STORE, "readonly");
      return runTransaction(transaction, async () => await requestResult(
        transaction.objectStore(RECORD_CACHE_META_STORE).get(key),
      ));
    },

    async getMetaMany(keys) {
      const database = await open();
      const transaction = database.transaction(RECORD_CACHE_META_STORE, "readonly");
      const store = transaction.objectStore(RECORD_CACHE_META_STORE);
      return runTransaction(transaction, () => Promise.all(
        keys.map((key) => requestResult(store.get(key)) as Promise<unknown>),
      ));
    },

    async listTableStep({ afterId, limit, table }: RecordCacheTableStepInput) {
      const database = await open();
      const transaction = database.transaction(RECORD_CACHE_RECORD_STORE, "readonly");
      return runTransaction(transaction, async () => collectTablePage(
        transaction.objectStore(RECORD_CACHE_RECORD_STORE)
          .index(RECORD_CACHE_BY_TABLE_INDEX)
          .openCursor(keyRange.only(table)),
        table,
        afterId,
        limit,
      ));
    },

    async listStoredMeta(afterKey, limit) {
      const database = await open();
      const transaction = database.transaction(RECORD_CACHE_META_STORE, "readonly");
      return runTransaction(transaction, async () => collectStoredStep(
        transaction.objectStore(RECORD_CACHE_META_STORE)
          .openCursor(afterKey === null ? null : keyRange.lowerBound(afterKey, true)),
        limit,
        (cursor) => typeof cursor.key === "string"
          ? { key: cursor.key, value: cursor.value as unknown }
          : null,
      ));
    },

    async listStoredRecords(afterKey, limit) {
      const database = await open();
      const transaction = database.transaction(RECORD_CACHE_RECORD_STORE, "readonly");
      return runTransaction(transaction, async () => collectStoredStep(
        transaction.objectStore(RECORD_CACHE_RECORD_STORE)
          .openCursor(afterKey === null ? null : keyRange.lowerBound(afterKey, true)),
        limit,
        (cursor) => {
          const stored = cursor.value as StoredRecordCacheRecord;
          return typeof stored?.key === "string"
            && typeof stored.id === "string"
            && typeof stored.table === "string"
            ? { id: stored.id, key: stored.key, table: stored.table, value: stored.value }
            : null;
        },
      ));
    },

    async putRecords(table, records) {
      if (records.length === 0) return;
      tableWrites.forget(table);
      const database = await open();
      const transaction = database.transaction(RECORD_CACHE_RECORD_STORE, "readwrite");
      return runTransaction(transaction, async () => {
        const store = transaction.objectStore(RECORD_CACHE_RECORD_STORE);
        for (const record of records) {
          await requestResult(store.put({
            id: record.id,
            key: storageKey(table, record.id),
            table,
            value: record.value,
          } satisfies StoredRecordCacheRecord));
        }
      });
    },

    async removeMeta(key) {
      const database = await open();
      const transaction = database.transaction(RECORD_CACHE_META_STORE, "readwrite");
      return runTransaction(transaction, async () => {
        await requestResult(transaction.objectStore(RECORD_CACHE_META_STORE).delete(key));
      });
    },

    async removeRecords(table, ids) {
      if (ids.length === 0) return;
      tableWrites.forget(table);
      const database = await open();
      const transaction = database.transaction(RECORD_CACHE_RECORD_STORE, "readwrite");
      return runTransaction(transaction, async () => {
        const store = transaction.objectStore(RECORD_CACHE_RECORD_STORE);
        for (const id of ids) await requestResult(store.delete(storageKey(table, id)));
      });
    },

    // A table still as this store last wrote it keeps the records whose
    // values are the very objects written then (recordCacheTableWrites.ts).
    async replaceTable(table, records) {
      const before = tableWrites.take(table);
      const writer = tableWrites.nextWriter();
      let checkId: string | null = null;
      const database = await open();
      const transaction = database.transaction(RECORD_CACHE_RECORD_STORE, "readwrite");
      await runTransaction(transaction, async () => {
        const store = transaction.objectStore(RECORD_CACHE_RECORD_STORE);
        const byTable = store.index(RECORD_CACHE_BY_TABLE_INDEX);
        const kept = before && await writtenAsBefore(store, byTable.count(keyRange.only(table)), table, before)
          ? before : null;
        if (kept) {
          const ids = new Set(records.map(({ id }) => id));
          for (const id of kept.values.keys()) {
            if (!ids.has(id)) await requestResult(store.delete(storageKey(table, id)));
          }
        } else {
          await deleteTableRecords(byTable.openCursor(keyRange.only(table)));
        }
        for (const record of records) {
          if (kept && wroteValue(kept, record.id, record.value)) continue;
          checkId ??= record.id;
          await requestResult(store.put({
            id: record.id,
            key: storageKey(table, record.id),
            table,
            value: record.value,
            writer,
          } satisfies StoredRecordCacheRecord));
        }
      });
      tableWrites.remember(table, { values: writtenValues(records), writer, checkId });
    },

    async rewriteStored({ meta, records }) {
      if (meta.length === 0 && records.length === 0) return;
      tableWrites.forget();
      const database = await open();
      const transaction = database.transaction(
        [RECORD_CACHE_RECORD_STORE, RECORD_CACHE_META_STORE],
        "readwrite",
      );
      return runTransaction(transaction, async () => {
        const recordStore = transaction.objectStore(RECORD_CACHE_RECORD_STORE);
        for (const record of records) {
          if (record.value === undefined) {
            await requestResult(recordStore.delete(record.key));
            continue;
          }
          await requestResult(recordStore.put({
            id: record.id,
            key: record.key,
            table: record.table,
            value: record.value,
          } satisfies StoredRecordCacheRecord));
        }
        await writeMetaEntries(transaction.objectStore(RECORD_CACHE_META_STORE), meta);
      });
    },

    async setMeta(key, value) {
      const database = await open();
      const transaction = database.transaction(RECORD_CACHE_META_STORE, "readwrite");
      return runTransaction(transaction, async () => {
        await requestResult(transaction.objectStore(RECORD_CACHE_META_STORE).put(value, key));
      });
    },

    async writeMeta(entries) {
      if (entries.length === 0) return;
      const database = await open();
      const transaction = database.transaction(RECORD_CACHE_META_STORE, "readwrite");
      return runTransaction(
        transaction,
        () => writeMetaEntries(transaction.objectStore(RECORD_CACHE_META_STORE), entries),
      );
    },
  };
}
