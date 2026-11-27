import type {
  RecordCacheBrowserRuntime,
  RecordCacheBrowserRuntimeOpenResult,
} from "./recordCacheBrowserRuntime";
import type {
  RecordCacheTablePage,
  RecordCacheTableStepInput,
} from "./recordCacheStorageModel";

export const ALL_RECORD_CACHE_TABLES = "*";

export interface RecordCacheScheduledWork {
  affectedTables?: readonly string[] | typeof ALL_RECORD_CACHE_TABLES;
  propagateFailure?: boolean;
  task(runtime: RecordCacheBrowserRuntime): Promise<void>;
  userId: string;
}

export interface RecordCacheWorkSchedulerOptions {
  onError(error: unknown): void;
  openRuntime(
    userId: string,
    onCustodyInvalidated: () => void,
  ): Promise<RecordCacheBrowserRuntimeOpenResult>;
  recoverQuota(runtime: RecordCacheBrowserRuntime, userId: string): Promise<void>;
}

export interface RecordCacheWorkScheduler {
  clear(userId: string): Promise<void>;
  dispose(): Promise<void>;
  enqueue(work: RecordCacheScheduledWork): Promise<void>;
  idle(): Promise<void>;
  readMeta<V = unknown>(userId: string, key: string): Promise<V | undefined>;
  readStorage<V>(
    userId: string,
    read: (runtime: RecordCacheBrowserRuntime) => Promise<V> | V,
  ): Promise<V | undefined>;
  readTableStep<V = unknown>(
    userId: string,
    input: RecordCacheTableStepInput,
  ): Promise<RecordCacheTablePage<V> | null>;
  reset(): Promise<void>;
}

interface CurrentRuntime {
  promise: Promise<RecordCacheBrowserRuntime | null>;
  token: object;
  userId: string;
}

function isQuotaError(error: unknown): boolean {
  return (
    error !== null
    && typeof error === "object"
    && Reflect.get(error, "name") === "QuotaExceededError"
  );
}

export function createRecordCacheWorkScheduler(
  options: RecordCacheWorkSchedulerOptions,
): RecordCacheWorkScheduler {
  let current: CurrentRuntime | null = null;
  let chain: Promise<void> = Promise.resolve();
  let generation = 0;
  let warnedOnce = false;
  const pendingTableWriteTails = new Map<string, Promise<void>>();

  const warn = (error: unknown): void => {
    if (warnedOnce) return;
    warnedOnce = true;
    options.onError(error);
  };

  const open = (userId: string): Promise<RecordCacheBrowserRuntime | null> => {
    if (current?.userId === userId) return current.promise;
    const previous = current;
    const token = {};
    let opening: Promise<RecordCacheBrowserRuntime | null>;
    opening = (async () => {
      if (previous) {
        const previousRuntime = await previous.promise.catch(() => null);
        await previousRuntime?.dispose();
      }
      const result = await options.openRuntime(userId, () => {
        if (current?.token === token) current = null;
      });
      return result.kind === "ready" ? result.runtime : null;
    })();
    current = { promise: opening, token, userId };
    void opening.catch(() => {
      if (current?.token === token) current = null;
    });
    return opening;
  };

  const affectedTableNames = (
    affected: RecordCacheScheduledWork["affectedTables"],
  ): string[] => {
    if (affected === ALL_RECORD_CACHE_TABLES) return [ALL_RECORD_CACHE_TABLES];
    return Array.from(new Set((affected ?? []).filter(Boolean)));
  };

  const recordBarriers = (
    scheduled: Promise<void>,
    affected: RecordCacheScheduledWork["affectedTables"],
  ): void => {
    const barrier = scheduled.then(
      () => undefined,
      () => undefined,
    );
    for (const table of affectedTableNames(affected)) {
      pendingTableWriteTails.set(table, barrier);
    }
    void barrier.finally(() => {
      for (const table of affectedTableNames(affected)) {
        if (pendingTableWriteTails.get(table) === barrier) {
          pendingTableWriteTails.delete(table);
        }
      }
    });
  };

  // A task's metadata is read once and written together where the runtime
  // can (recordCacheMetaBatch.ts).
  const runTask = (
    runtime: RecordCacheBrowserRuntime,
    task: RecordCacheScheduledWork["task"],
  ): Promise<void> => (runtime.runTask ? runtime.runTask(task) : task(runtime));

  const enqueue = (work: RecordCacheScheduledWork): Promise<void> => {
    const admittedGeneration = generation;
    const scheduled = chain.then(async () => {
      if (admittedGeneration !== generation) return;
      const runtime = await open(work.userId);
      if (!runtime || admittedGeneration !== generation) return;
      try {
        await runTask(runtime, work.task);
      } catch (error) {
        if (!isQuotaError(error)) throw error;
        await options.recoverQuota(runtime, work.userId);
        await runTask(runtime, work.task);
      }
    });
    chain = scheduled.catch(warn);
    recordBarriers(scheduled, work.affectedTables);
    return work.propagateFailure ? scheduled : chain;
  };

  const readStorage = async <V>(
    userId: string,
    read: (runtime: RecordCacheBrowserRuntime) => Promise<V> | V,
  ): Promise<V | undefined> => {
    await chain;
    const runtime = await open(userId);
    return runtime ? await read(runtime) : undefined;
  };

  return {
    async clear(userId) {
      const admittedGeneration = ++generation;
      const scheduled = chain.then(async () => {
        if (admittedGeneration !== generation) return;
        const runtime = await open(userId);
        if (!runtime || admittedGeneration !== generation) return;
        await runtime.clear();
      });
      chain = scheduled.catch(warn);
      recordBarriers(scheduled, ALL_RECORD_CACHE_TABLES);
      await chain;
    },

    async dispose() {
      generation += 1;
      await chain;
      const active = current;
      current = null;
      const runtime = await active?.promise.catch(() => null);
      await runtime?.dispose();
      pendingTableWriteTails.clear();
    },

    enqueue,

    async idle() {
      for (;;) {
        const observed = chain;
        await observed;
        await Promise.resolve();
        if (observed === chain) return;
      }
    },

    async readMeta<V = unknown>(userId: string, key: string) {
      return readStorage<V | undefined>(
        userId,
        (runtime) => runtime.getMeta<V>(key),
      );
    },

    readStorage,

    async readTableStep<V = unknown>(userId: string, input: RecordCacheTableStepInput) {
      const runtime = await open(userId);
      if (!runtime) return null;
      await Promise.all([
        pendingTableWriteTails.get(ALL_RECORD_CACHE_TABLES),
        pendingTableWriteTails.get(input.table),
      ]);
      return await runtime.listTableStep<V>(input);
    },

    async reset() {
      generation += 1;
      await chain;
      const active = current;
      current = null;
      const runtime = await active?.promise.catch(() => null);
      await runtime?.dispose();
      pendingTableWriteTails.clear();
    },
  };
}
