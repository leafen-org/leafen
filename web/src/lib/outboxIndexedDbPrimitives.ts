import { deviceStoreDatabaseFree, deviceStoreDatabaseKept, watchDeviceStoreTransaction } from "./deviceStoreLock";

export interface CursorPage<T> {
  hasMore: boolean;
  values: T[];
}

export function requestResult<T>(request: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error ?? new Error("Browser storage request failed."));
  });
}

export function transactionDone(transaction: IDBTransaction): Promise<void> {
  return new Promise((resolve, reject) => {
    transaction.oncomplete = () => resolve();
    transaction.onabort = () => reject(
      transaction.error ?? new Error("Browser storage transaction was aborted."),
    );
    transaction.onerror = () => reject(
      transaction.error ?? new Error("Browser storage transaction failed."),
    );
  });
}

export function collectCursorPage<T>(
  request: IDBRequest<IDBCursorWithValue | null>,
  limit: number,
): Promise<CursorPage<T>> {
  return new Promise((resolve, reject) => {
    const values: T[] = [];
    request.onerror = () => reject(request.error ?? new Error("Browser storage cursor failed."));
    request.onsuccess = () => {
      const cursor = request.result;
      if (!cursor) {
        resolve({ hasMore: false, values });
        return;
      }
      if (values.length >= limit) {
        resolve({ hasMore: true, values });
        return;
      }
      values.push(cursor.value as T);
      cursor.continue();
    };
  });
}

export function collectKeyCursorPage<T extends IDBValidKey>(
  request: IDBRequest<IDBCursor | null>,
  limit: number,
): Promise<CursorPage<T>> {
  return new Promise((resolve, reject) => {
    const values: T[] = [];
    request.onerror = () => reject(request.error ?? new Error("Browser storage key cursor failed."));
    request.onsuccess = () => {
      const cursor = request.result;
      if (!cursor) {
        resolve({ hasMore: false, values });
        return;
      }
      if (values.length >= limit) {
        resolve({ hasMore: true, values });
        return;
      }
      values.push(cursor.key as T);
      cursor.continue();
    };
  });
}

/**
 * Runs `work` in a transaction on the store `storeName`, which is given up
 * when it waited past the store's wait for the database another tab holds
 * (deviceStoreLock.ts watchDeviceStoreTransaction): nothing of it lands
 * later, and the step fails as one that waited past the wait. One that
 * started goes on to its end, however large what it writes.
 */
export async function runIndexedDbTransaction<T>(
  transaction: IDBTransaction,
  work: () => Promise<T>,
  storeName: string,
): Promise<T> {
  const done = transactionDone(transaction);
  let givenUp = false;
  const ended = watchDeviceStoreTransaction(storeName, transaction, () => {
    try {
      transaction.abort();
      givenUp = true;
    } catch {
      // It ended meanwhile.
    }
  });
  try {
    const result = await work();
    await done;
    deviceStoreDatabaseFree(storeName);
    return result;
  } catch (cause) {
    try {
      transaction.abort();
    } catch {
      // The transaction may already have completed or aborted.
    }
    await done.catch(() => undefined);
    if (givenUp) throw deviceStoreDatabaseKept(storeName);
    throw cause;
  } finally {
    ended();
  }
}
