import type { RecordCacheWorkScheduler } from "./recordCacheWorkScheduler";
import type {
  RecordCacheStorage,
  RecordCacheTablePage,
  RecordCacheTableStepInput,
} from "./recordCacheStorageModel";

export interface RecordCacheReadFacadeOptions {
  canRead(userId: string): boolean;
  maxRecordsPerPage: number;
  onError(error: unknown): void;
  scheduler: Pick<
    RecordCacheWorkScheduler,
    "readMeta" | "readStorage" | "readTableStep"
  >;
}

export interface RecordCacheReadFacade {
  getMeta<V = unknown>(userId: string, key: string): Promise<V | undefined>;
  listTablePage<V = unknown>(
    userId: string,
    input: RecordCacheTableStepInput,
  ): Promise<RecordCacheTablePage<V>>;
  readStorage<V>(
    userId: string,
    fallback: V,
    read: (storage: RecordCacheStorage) => Promise<V> | V,
  ): Promise<V>;
}

function assertPositiveSafeInteger(value: number, label: string): void {
  if (!Number.isSafeInteger(value) || value <= 0) {
    throw new RangeError(`${label} must be a positive safe integer.`);
  }
}

function emptyPage<V>(): RecordCacheTablePage<V> {
  return { nextAfterId: null, records: [] };
}

export function createRecordCacheReadFacade(
  options: RecordCacheReadFacadeOptions,
): RecordCacheReadFacade {
  assertPositiveSafeInteger(options.maxRecordsPerPage, "maxRecordsPerPage");
  let warnedOnce = false;

  const warn = (error: unknown): void => {
    if (warnedOnce) return;
    warnedOnce = true;
    options.onError(error);
  };

  const canRead = (userId: string): boolean => {
    try {
      return options.canRead(userId);
    } catch (error) {
      warn(error);
      return false;
    }
  };

  return {
    async getMeta<V = unknown>(userId: string, key: string) {
      if (!canRead(userId)) return undefined;
      try {
        return await options.scheduler.readMeta<V>(userId, key);
      } catch (error) {
        warn(error);
        return undefined;
      }
    },

    async listTablePage<V = unknown>(userId: string, input: RecordCacheTableStepInput) {
      assertPositiveSafeInteger(input.limit, "record-cache page size");
      if (input.limit > options.maxRecordsPerPage) {
        throw new RangeError("Record-cache read exceeds its configured page bound.");
      }
      if (!canRead(userId)) return emptyPage<V>();
      try {
        return await options.scheduler.readTableStep<V>(userId, input) ?? emptyPage<V>();
      } catch (error) {
        warn(error);
        return emptyPage<V>();
      }
    },

    async readStorage<V>(
      userId: string,
      fallback: V,
      read: (storage: RecordCacheStorage) => Promise<V> | V,
    ) {
      if (!canRead(userId)) return fallback;
      try {
        const value = await options.scheduler.readStorage<V>(userId, read);
        return value === undefined ? fallback : value;
      } catch (error) {
        warn(error);
        return fallback;
      }
    },
  };
}
