export interface RecordCacheRecord<V = unknown> {
  id: string;
  value: V;
}

export interface RecordCacheStorageBounds {
  maxMetaKeyCharacters: number;
  maxRecordIdCharacters: number;
  maxRecordsPerMutation: number;
  maxRecordsPerStep: number;
  maxTableCharacters: number;
}

export interface RecordCacheTablePage<V = unknown> {
  nextAfterId: string | null;
  records: RecordCacheRecord<V>[];
}

export interface RecordCacheTableStepInput {
  afterId: string | null;
  limit: number;
  table: string;
}

/** A metadata write; one whose value is undefined removes its key. */
export interface RecordCacheMetaWrite {
  key: string;
  value: unknown;
}

export interface RecordCacheStorageAdapter {
  clear(): Promise<void>;
  close(): void;
  getMeta(key: string): Promise<unknown>;
  /** Several metadata values, in the keys' order, read in one transaction. */
  getMetaMany?(keys: readonly string[]): Promise<unknown[]>;
  listTableStep(input: RecordCacheTableStepInput): Promise<RecordCacheTablePage>;
  putRecords(table: string, records: readonly RecordCacheRecord[]): Promise<void>;
  removeMeta(key: string): Promise<void>;
  removeRecords(table: string, ids: readonly string[]): Promise<void>;
  replaceTable(table: string, records: readonly RecordCacheRecord[]): Promise<void>;
  setMeta(key: string, value: unknown): Promise<void>;
  /** Several metadata writes, made in one transaction. */
  writeMeta?(entries: readonly RecordCacheMetaWrite[]): Promise<void>;
}

export interface RecordCacheStorage {
  clear(): Promise<void>;
  getMeta<V = unknown>(key: string): Promise<V | undefined>;
  getMetaMany?(keys: readonly string[]): Promise<unknown[]>;
  /**
   * Reads these keys together ahead of their reads, where one task's
   * metadata is read once (recordCacheMetaBatch.ts); elsewhere absent.
   */
  prefetchMeta?(keys: readonly string[]): Promise<void>;
  listTableStep<V = unknown>(input: RecordCacheTableStepInput): Promise<RecordCacheTablePage<V>>;
  putRecords<V = unknown>(
    table: string,
    records: readonly RecordCacheRecord<V>[],
  ): Promise<void>;
  removeMeta(key: string): Promise<void>;
  removeRecords(table: string, ids: readonly string[]): Promise<void>;
  replaceTable<V = unknown>(
    table: string,
    records: readonly RecordCacheRecord<V>[],
  ): Promise<void>;
  setMeta(key: string, value: unknown): Promise<void>;
  writeMeta?(entries: readonly RecordCacheMetaWrite[]): Promise<void>;
}

/** A store whose metadata can be read and written several keys at a time. */
export type RecordCacheMetaManyStorage = RecordCacheStorage & Required<
  Pick<RecordCacheStorage, "getMetaMany" | "writeMeta">
>;

const RECORD_CACHE_SCHEMA_META_KEY = "__recordCacheSchemaVersion";

export interface RecordCacheStorageOptions {
  bounds: RecordCacheStorageBounds;
  schemaVersion: number;
}

function assertPositiveSafeInteger(value: number, label: string): void {
  if (!Number.isSafeInteger(value) || value <= 0) {
    throw new RangeError(`${label} must be a positive safe integer.`);
  }
}

function assertBoundedString(value: string, maximum: number, label: string): void {
  if (typeof value !== "string" || value.length > maximum) {
    throw new RangeError(`${label} exceeds its configured character bound.`);
  }
}

function validateBounds(bounds: RecordCacheStorageBounds): void {
  assertPositiveSafeInteger(bounds.maxMetaKeyCharacters, "maxMetaKeyCharacters");
  assertPositiveSafeInteger(bounds.maxRecordIdCharacters, "maxRecordIdCharacters");
  assertPositiveSafeInteger(bounds.maxRecordsPerMutation, "maxRecordsPerMutation");
  assertPositiveSafeInteger(bounds.maxRecordsPerStep, "maxRecordsPerStep");
  assertPositiveSafeInteger(bounds.maxTableCharacters, "maxTableCharacters");
  if (bounds.maxMetaKeyCharacters < RECORD_CACHE_SCHEMA_META_KEY.length) {
    throw new RangeError("maxMetaKeyCharacters cannot admit the record-cache schema key.");
  }
}

function assertMetaKey(key: string, bounds: RecordCacheStorageBounds): void {
  assertBoundedString(key, bounds.maxMetaKeyCharacters, "record-cache meta key");
}

function assertTable(table: string, bounds: RecordCacheStorageBounds): void {
  assertBoundedString(table, bounds.maxTableCharacters, "record-cache table");
}

function assertRecordId(id: string, bounds: RecordCacheStorageBounds): void {
  assertBoundedString(id, bounds.maxRecordIdCharacters, "record-cache record id");
}

function validateMutation(
  records: readonly RecordCacheRecord[],
  bounds: RecordCacheStorageBounds,
): void {
  if (!Array.isArray(records) || records.length > bounds.maxRecordsPerMutation) {
    throw new RangeError("Record-cache mutation exceeds its configured record bound.");
  }
  for (const record of records) {
    if (!record || typeof record !== "object") {
      throw new TypeError("Record-cache record is invalid.");
    }
    assertRecordId(record.id, bounds);
  }
}

function validateIds(ids: readonly string[], bounds: RecordCacheStorageBounds): void {
  if (!Array.isArray(ids) || ids.length > bounds.maxRecordsPerMutation) {
    throw new RangeError("Record-cache removal exceeds its configured record bound.");
  }
  for (const id of ids) assertRecordId(id, bounds);
}

function validateTableStep(
  input: RecordCacheTableStepInput,
  bounds: RecordCacheStorageBounds,
): void {
  assertTable(input.table, bounds);
  if (input.afterId !== null) assertRecordId(input.afterId, bounds);
  assertPositiveSafeInteger(input.limit, "record-cache step limit");
  if (input.limit > bounds.maxRecordsPerStep) {
    throw new RangeError("Record-cache step limit exceeds its configured bound.");
  }
}

function validateTablePage(
  page: RecordCacheTablePage,
  input: RecordCacheTableStepInput,
  bounds: RecordCacheStorageBounds,
): void {
  if (!page || !Array.isArray(page.records) || page.records.length > input.limit) {
    throw new RangeError("Record-cache adapter returned an over-bound table page.");
  }
  for (const record of page.records) {
    if (!record || typeof record !== "object") {
      throw new TypeError("Record-cache adapter returned an invalid record.");
    }
    assertRecordId(record.id, bounds);
  }
  if (page.nextAfterId === null) return;
  assertRecordId(page.nextAfterId, bounds);
  if (
    page.records.length === 0
    || page.records[page.records.length - 1].id !== page.nextAfterId
    || (input.afterId !== null && page.nextAfterId <= input.afterId)
  ) {
    throw new Error("Record-cache adapter returned an invalid continuation.");
  }
}

export function createRecordCacheStorage(
  adapter: RecordCacheStorageAdapter,
  options: RecordCacheStorageOptions,
): RecordCacheMetaManyStorage {
  validateBounds(options.bounds);
  assertPositiveSafeInteger(options.schemaVersion, "record-cache schemaVersion");
  let ready: Promise<void> | null = null;

  const ensureSchema = (): Promise<void> => {
    if (ready) return ready;
    const admission = (async () => {
      const stored = await adapter.getMeta(RECORD_CACHE_SCHEMA_META_KEY);
      if (stored === options.schemaVersion) return;
      await adapter.clear();
      await adapter.setMeta(RECORD_CACHE_SCHEMA_META_KEY, options.schemaVersion);
    })();
    ready = admission;
    void admission.catch(() => {
      if (ready === admission) ready = null;
    });
    return admission;
  };

  return {
    async clear() {
      await ensureSchema();
      await adapter.clear();
      await adapter.setMeta(RECORD_CACHE_SCHEMA_META_KEY, options.schemaVersion);
    },

    async getMeta<V>(key: string) {
      assertMetaKey(key, options.bounds);
      await ensureSchema();
      return await adapter.getMeta(key) as V | undefined;
    },

    async getMetaMany(keys) {
      for (const key of keys) assertMetaKey(key, options.bounds);
      await ensureSchema();
      if (adapter.getMetaMany) return adapter.getMetaMany(keys);
      const values: unknown[] = [];
      for (const key of keys) values.push(await adapter.getMeta(key));
      return values;
    },

    async listTableStep<V>(input: RecordCacheTableStepInput) {
      validateTableStep(input, options.bounds);
      await ensureSchema();
      const page = await adapter.listTableStep(input);
      validateTablePage(page, input, options.bounds);
      return page as RecordCacheTablePage<V>;
    },

    async putRecords(table, records) {
      assertTable(table, options.bounds);
      validateMutation(records, options.bounds);
      await ensureSchema();
      await adapter.putRecords(table, records);
    },

    async removeMeta(key) {
      assertMetaKey(key, options.bounds);
      await ensureSchema();
      await adapter.removeMeta(key);
    },

    async removeRecords(table, ids) {
      assertTable(table, options.bounds);
      validateIds(ids, options.bounds);
      await ensureSchema();
      await adapter.removeRecords(table, ids);
    },

    async replaceTable(table, records) {
      assertTable(table, options.bounds);
      validateMutation(records, options.bounds);
      await ensureSchema();
      await adapter.replaceTable(table, records);
    },

    async setMeta(key, value) {
      assertMetaKey(key, options.bounds);
      await ensureSchema();
      await adapter.setMeta(key, value);
    },

    async writeMeta(entries) {
      if (entries.length === 0) return;
      for (const { key } of entries) assertMetaKey(key, options.bounds);
      await ensureSchema();
      if (adapter.writeMeta) return adapter.writeMeta(entries);
      for (const { key, value } of entries) {
        if (value === undefined) await adapter.removeMeta(key);
        else await adapter.setMeta(key, value);
      }
    },
  };
}
