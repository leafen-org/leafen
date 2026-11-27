import type { LocalSecretBox } from "./localSecretBox";
import type {
  RecordCacheRecord,
  RecordCacheStorageAdapter,
  RecordCacheTablePage,
} from "./recordCacheStorageModel";

const RECORD_CACHE_VALUE_CONTEXT = "leafen-record-cache-value";

export interface RecordCacheSealedStorageOptions {
  box: LocalSecretBox;
  maxRecordsPerMutation: number;
  maxRecordsPerStep: number;
  storageName: string;
}

function assertPositiveSafeInteger(value: number, label: string): void {
  if (!Number.isSafeInteger(value) || value <= 0) {
    throw new RangeError(`${label} must be a positive safe integer.`);
  }
}

function assertOptions(options: RecordCacheSealedStorageOptions): void {
  assertPositiveSafeInteger(options.maxRecordsPerMutation, "maxRecordsPerMutation");
  assertPositiveSafeInteger(options.maxRecordsPerStep, "maxRecordsPerStep");
  if (typeof options.storageName !== "string" || options.storageName.length === 0) {
    throw new TypeError("storageName must be a non-empty string.");
  }
}

/**
 * What a stored value is sealed to: its store, and its record's table and id
 * or its metadata key, so a value moved elsewhere doesn't open. A change of
 * the lock seals each value again to the same place (recordCacheCustodyCarry.ts).
 */
export function recordCacheValueContext(
  storageName: string,
  ...parts: readonly string[]
): string {
  return JSON.stringify([RECORD_CACHE_VALUE_CONTEXT, storageName, ...parts]);
}

const valueContext = recordCacheValueContext;

function assertMutationBound(
  records: readonly RecordCacheRecord[],
  maximum: number,
): void {
  if (!Array.isArray(records) || records.length > maximum) {
    throw new RangeError("Record-cache records exceed their configured mutation bound.");
  }
}

// A frozen value sealed before, written again in the same place with the same
// key, is stored as it was sealed: it can't have changed (a graph's rows kept
// in parts, of which one changes at a time; localFirstProductCache.ts).
const sealedFrozen = new WeakMap<object, { box: LocalSecretBox; context: string; sealed: unknown }>();

async function sealRecords(
  records: readonly RecordCacheRecord[],
  table: string,
  options: RecordCacheSealedStorageOptions,
): Promise<RecordCacheRecord[]> {
  assertMutationBound(records, options.maxRecordsPerMutation);
  const sealed: RecordCacheRecord[] = [];
  for (const record of records) {
    const context = valueContext(options.storageName, "record", table, record.id);
    const frozen = typeof record.value === "object" && record.value !== null && Object.isFrozen(record.value) ? record.value : null;
    const known = frozen ? sealedFrozen.get(frozen) : undefined;
    let value = known?.box === options.box && known.context === context ? known.sealed : undefined;
    if (value === undefined) {
      value = await options.box.seal(record.value, context);
      if (frozen) sealedFrozen.set(frozen, { box: options.box, context, sealed: value });
    }
    sealed.push({ id: record.id, value });
  }
  return sealed;
}

async function openRecords(
  page: RecordCacheTablePage,
  table: string,
  limit: number,
  options: RecordCacheSealedStorageOptions,
): Promise<RecordCacheTablePage> {
  if (
    !page
    || !Array.isArray(page.records)
    || page.records.length > limit
    || page.records.length > options.maxRecordsPerStep
  ) {
    throw new RangeError("Record-cache storage returned an over-bound sealed page."); // screen words: not shown
  }
  const records: RecordCacheRecord[] = [];
  for (const record of page.records) {
    const value = await options.box.open(
      record.value,
      valueContext(options.storageName, "record", table, record.id),
    );
    if (value === undefined) {
      throw new Error("Stored record-cache value could not be opened.");
    }
    records.push({ id: record.id, value });
  }
  return { nextAfterId: page.nextAfterId, records };
}

export function createRecordCacheSealedStorage(
  storage: RecordCacheStorageAdapter,
  options: RecordCacheSealedStorageOptions,
): RecordCacheStorageAdapter {
  assertOptions(options);
  const sealMeta = (key: string, value: unknown) => options.box.seal(
    value,
    valueContext(options.storageName, "meta", key),
  );
  const openMeta = async (key: string, stored: unknown): Promise<unknown> => {
    if (stored === undefined) return undefined;
    const value = await options.box.open(stored, valueContext(options.storageName, "meta", key));
    if (value === undefined) {
      throw new Error("Stored record-cache metadata could not be opened.");
    }
    return value;
  };

  return {
    clear() {
      return storage.clear();
    },

    close() {
      storage.close();
    },

    async getMeta(key) {
      return openMeta(key, await storage.getMeta(key));
    },

    async getMetaMany(keys) {
      const stored = storage.getMetaMany
        ? await storage.getMetaMany(keys)
        : await Promise.all(keys.map((key) => storage.getMeta(key)));
      return Promise.all(keys.map((key, index) => openMeta(key, stored[index])));
    },

    async listTableStep(input) {
      assertPositiveSafeInteger(input.limit, "record-cache step limit");
      if (input.limit > options.maxRecordsPerStep) {
        throw new RangeError("Record-cache step exceeds its configured sealed-read bound."); // screen words: not shown
      }
      return openRecords(
        await storage.listTableStep(input),
        input.table,
        input.limit,
        options,
      );
    },

    async putRecords(table, records) {
      await storage.putRecords(table, await sealRecords(records, table, options));
    },

    removeMeta(key) {
      return storage.removeMeta(key);
    },

    removeRecords(table, ids) {
      return storage.removeRecords(table, ids);
    },

    async replaceTable(table, records) {
      await storage.replaceTable(table, await sealRecords(records, table, options));
    },

    async setMeta(key, value) {
      await storage.setMeta(key, await sealMeta(key, value));
    },

    async writeMeta(entries) {
      // One after another: each written out as its seal begins, not all in
      // the first one's task (a long page list's runs, bootstrapCacheParts.ts).
      const sealed: { key: string; value: unknown }[] = [];
      for (const { key, value } of entries) {
        sealed.push({ key, value: value === undefined ? undefined : await sealMeta(key, value) });
      }
      if (storage.writeMeta) return storage.writeMeta(sealed);
      for (const { key, value } of sealed) {
        if (value === undefined) await storage.removeMeta(key);
        else await storage.setMeta(key, value);
      }
    },
  };
}
