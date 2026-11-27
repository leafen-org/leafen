import type {
  RecordCacheMetaManyStorage,
  RecordCacheMetaWrite,
  RecordCacheStorage,
} from "./recordCacheStorageModel";

/**
 * One record-cache task's metadata (recordCacheWorkScheduler.ts): each key
 * is read from the store once and its writes are kept together, in one
 * transaction when the task ends — not one locked, sealed transaction for
 * every read and write (2026-11-13: a page opened read and wrote the cache's
 * bookkeeping 26 times, its own budget's summary three times over). Records
 * are read and written as before. What the task wrote before it failed is
 * written all the same, as when each write went at once.
 */
export interface RecordCacheMetaBatch<Storage extends RecordCacheStorage> {
  /** Writes what the task wrote, in one transaction. */
  commit(): Promise<void>;
  storage: Storage;
}

// Each read gets its own copy, as one from the store would: a value a task
// changes and doesn't write back stays as written.
function copied<V>(value: V): V {
  return value === undefined ? value : structuredClone(value);
}

export function createRecordCacheMetaBatch<Storage extends RecordCacheMetaManyStorage>(
  base: Storage,
): RecordCacheMetaBatch<Storage> {
  // A key whose value is undefined is absent.
  const known = new Map<string, unknown>();
  const written = new Map<string, unknown>();
  const remember = (key: string, value: unknown) => {
    if (!known.has(key)) known.set(key, copied(value));
  };
  const write = (key: string, value: unknown) => {
    // A frozen value is one its writer never changes (a page list kept whole,
    // localFirstProductCache.ts): it is kept as it is, not copied — copying a
    // 47,000-page list held the page 245 ms on a slower device (2026-11-14).
    const kept = value !== null && typeof value === "object" && Object.isFrozen(value) ? value : copied(value);
    known.set(key, kept);
    written.set(key, kept);
  };
  const storage: Storage = {
    ...base,
    async clear() {
      known.clear();
      written.clear();
      await base.clear();
    },
    async getMeta<V>(key: string): Promise<V | undefined> {
      if (known.has(key)) return copied(known.get(key) as V | undefined);
      const value = await base.getMeta<V>(key);
      remember(key, value);
      return value;
    },
    async prefetchMeta(keys: readonly string[]) {
      const missing = [...new Set(keys)].filter((key) => !known.has(key));
      if (missing.length === 0) return;
      const values = await base.getMetaMany(missing);
      missing.forEach((key, index) => remember(key, values[index]));
    },
    async removeMeta(key: string) {
      write(key, undefined);
    },
    async setMeta(key: string, value: unknown) {
      write(key, value);
    },
  };
  return {
    async commit() {
      if (written.size === 0) return;
      const entries: RecordCacheMetaWrite[] = [...written].map(([key, value]) => ({ key, value }));
      written.clear();
      await base.writeMeta(entries);
    },
    storage,
  };
}
