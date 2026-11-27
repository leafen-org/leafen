import {
  NATIVE_OFFLINE_MUTATION_ORDER_INDEX,
  NATIVE_OFFLINE_MUTATION_STORE,
  openNativeOfflineLegacyDatabase,
  withNativeOfflineLegacyDatabase,
} from "./nativeOfflineLegacySchema";

export interface NativeLegacyPageMutation {
  baseCursor: number;
  content: string;
  createdAt: number;
  mutationId: string;
  pageId: string;
}

export type NativeLegacyPageMutationCursor = readonly [number, string];

export interface NativePageMutationLegacyBounds {
  maxBaseCursor: number;
  maxContentCharacters: number;
  maxEntriesPerStep: number;
  maxMutationIdCharacters: number;
  maxPageIdCharacters: number;
}

export interface NativePageMutationLegacySource {
  listStep(input: {
    after: unknown;
    limit: number;
  }): Promise<{
    mutations: NativeLegacyPageMutation[];
    nextAfter: NativeLegacyPageMutationCursor | null;
  }>;
  remove(mutationId: string): Promise<void>;
}

function legacyCursor(value: unknown): NativeLegacyPageMutationCursor | null {
  if (value === null) return null;
  if (
    !Array.isArray(value)
    || value.length !== 2
    || !Number.isSafeInteger(value[0])
    || (value[0] as number) < 0
    || typeof value[1] !== "string"
    || value[1].length === 0
  ) {
    throw new RangeError("Legacy page-mutation continuation is invalid.");
  }
  return [value[0] as number, value[1]];
}

function assertPositiveBound(value: number, label: string): void {
  if (!Number.isSafeInteger(value) || value <= 0) {
    throw new RangeError(`${label} must be a positive safe integer.`);
  }
}

function validateBounds(bounds: NativePageMutationLegacyBounds): void {
  for (const [label, value] of Object.entries(bounds)) {
    assertPositiveBound(value, label);
  }
}

function boundedIdentity(value: unknown, maximum: number, label: string): string {
  if (typeof value !== "string" || value.length === 0 || value.length > maximum) {
    throw new DOMException(`Legacy ${label} is outside its supported bound.`, "DataError");
  }
  return value;
}

function legacyMutation(
  value: unknown,
  bounds: NativePageMutationLegacyBounds,
): NativeLegacyPageMutation {
  const record = value && typeof value === "object" && !Array.isArray(value)
    ? value as Record<string, unknown>
    : null;
  if (
    !record
    || !Number.isSafeInteger(record.baseCursor)
    || (record.baseCursor as number) < 0
    || (record.baseCursor as number) > bounds.maxBaseCursor
    || !Number.isSafeInteger(record.createdAt)
    || (record.createdAt as number) < 0
    || typeof record.content !== "string"
    || record.content.length > bounds.maxContentCharacters
  ) {
    throw new DOMException("Legacy page mutation is invalid.", "DataError");
  }
  return {
    baseCursor: record.baseCursor as number,
    content: record.content,
    createdAt: record.createdAt as number,
    mutationId: boundedIdentity(
      record.mutationId,
      bounds.maxMutationIdCharacters,
      "mutation id",
    ),
    pageId: boundedIdentity(record.pageId, bounds.maxPageIdCharacters, "page id"),
  };
}

// Nothing writes page changes from before the outbox any more: once a tab
// found none left, it doesn't look again until it loads anew, when another
// tab's left in the meantime are moved (2026-11-13: every page opened read
// the empty store, the outbox's recovery turn running on each).
const drainedInThisTab = { drained: false };

// The tab's connection to the store (nativeOfflineLegacySchema.ts), or a
// supplied factory's own, closed after.
async function withLegacyDatabase<T>(
  factory: IDBFactory | undefined,
  use: (database: IDBDatabase) => Promise<T>,
): Promise<T> {
  if (!factory) return withNativeOfflineLegacyDatabase(use);
  const database = await openNativeOfflineLegacyDatabase(factory);
  try {
    return await use(database);
  } finally {
    database.close();
  }
}

export function createNativePageMutationLegacySource(
  bounds: NativePageMutationLegacyBounds,
  factory?: IDBFactory,
): NativePageMutationLegacySource {
  validateBounds(bounds);
  const found = factory ? { drained: false } : drainedInThisTab;
  return {
    async listStep({ after: suppliedAfter, limit }) {
      assertPositiveBound(limit, "legacy page-mutation step limit");
      if (limit > bounds.maxEntriesPerStep) {
        throw new RangeError("Legacy page-mutation step exceeds its configured bound.");
      }
      const after = legacyCursor(suppliedAfter);
      if (after === null && found.drained) return { mutations: [], nextAfter: null };
      const step = await withLegacyDatabase(factory, (database) => (
        new Promise<{
          mutations: NativeLegacyPageMutation[];
          nextAfter: NativeLegacyPageMutationCursor | null;
        }>((resolve, reject) => {
          const transaction = database.transaction(NATIVE_OFFLINE_MUTATION_STORE);
          const index = transaction
            .objectStore(NATIVE_OFFLINE_MUTATION_STORE)
            .index(NATIVE_OFFLINE_MUTATION_ORDER_INDEX);
          const values: NativeLegacyPageMutation[] = [];
          let nextAfter: NativeLegacyPageMutationCursor | null = null;
          const range = after ? IDBKeyRange.lowerBound(after, true) : undefined;
          const request = index.openCursor(range);
          request.onsuccess = () => {
            const cursor = request.result;
            if (!cursor || values.length >= limit) return;
            const value = legacyMutation(cursor.value, bounds);
            values.push(value);
            nextAfter = [value.createdAt, value.mutationId];
            if (values.length < limit) cursor.continue();
          };
          request.onerror = () => reject(
            request.error
            ?? new DOMException("Unable to read legacy page mutations.", "InvalidStateError"),
          );
          transaction.oncomplete = () => resolve({
            mutations: values,
            nextAfter: values.length === limit ? nextAfter : null,
          });
          transaction.onerror = () => reject(
            transaction.error
            ?? new DOMException("Unable to read legacy page mutations.", "InvalidStateError"),
          );
          transaction.onabort = transaction.onerror;
        })
      ));
      if (after === null && step.mutations.length === 0) found.drained = true;
      return step;
    },

    async remove(mutationId) {
      boundedIdentity(mutationId, bounds.maxMutationIdCharacters, "mutation id");
      await withLegacyDatabase(factory, (database) => (
        new Promise<void>((resolve, reject) => {
          const transaction = database.transaction(
            NATIVE_OFFLINE_MUTATION_STORE,
            "readwrite",
          );
          transaction.objectStore(NATIVE_OFFLINE_MUTATION_STORE).delete(mutationId);
          transaction.oncomplete = () => resolve();
          transaction.onerror = () => reject(
            transaction.error
            ?? new DOMException("Unable to remove a legacy page mutation.", "InvalidStateError"),
          );
          transaction.onabort = transaction.onerror;
        })
      ));
    },
  };
}

async function withLegacyMutationStore<T>(
  mode: IDBTransactionMode,
  run: (store: IDBObjectStore) => IDBRequest<T> | void,
): Promise<T | undefined> {
  return withNativeOfflineLegacyDatabase((database) => (
    new Promise<T | undefined>((resolve, reject) => {
      const transaction = database.transaction(NATIVE_OFFLINE_MUTATION_STORE, mode);
      const request = run(transaction.objectStore(NATIVE_OFFLINE_MUTATION_STORE));
      transaction.oncomplete = () => resolve(request ? request.result : undefined);
      transaction.onerror = () => reject(
        transaction.error ?? new DOMException("Unable to use legacy page mutations.", "InvalidStateError"),
      );
      transaction.onabort = transaction.onerror;
    })
  ));
}

/** Page changes from before the outbox that this device has not sent yet. */
export async function countLegacyPageMutations(): Promise<number> {
  return await withLegacyMutationStore("readonly", (store) => store.count()) ?? 0;
}

/** Removes those changes, as at sign-out. */
export async function clearLegacyPageMutations(): Promise<void> {
  await withLegacyMutationStore("readwrite", (store) => { store.clear(); });
}
