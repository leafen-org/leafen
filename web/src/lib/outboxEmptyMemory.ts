import type { OutboxEntryPage, OutboxStorageModel } from "./outboxStorageModel";

// How long a tab goes by its own knowledge that the outbox holds nothing. A
// write here or another tab's change signal ends it at once; this bounds one
// whose signal never came (its tab closed within the signal's 25 ms).
const EMPTY_REMEMBERED_MS = 30_000;

const NOTHING: OutboxEntryPage = { entries: [], nextAfterSeq: null };

/**
 * The outbox's store for one tab's runtime, remembering that it holds nothing
 * of `tabId`'s once a read from the start found so: such a read then answers
 * from that until something is written here or `forget` is called (another
 * tab's change signal), or the memory runs out (2026-11-13: every page opened
 * read the empty outbox twice, for the page's kept changes and for the
 * recovery turn the server's answer started). A read that overlapped a write
 * remembers nothing; every other read goes to the store as before.
 */
export function outboxRememberingEmpty(storage: OutboxStorageModel, tabId: string, clock: () => number) {
  let writes = 0;
  let emptyAt: number | null = null;
  const forget = () => {
    writes += 1;
    emptyAt = null;
  };
  const remembered = () => emptyAt !== null && clock() - emptyAt < EMPTY_REMEMBERED_MS;
  const noting = async <T>(read: () => Promise<T>, empty: (value: T) => boolean): Promise<T> => {
    const before = writes;
    const value = await read();
    if (before === writes && empty(value)) emptyAt = clock();
    return value;
  };
  const writing = async <T>(write: () => Promise<T>): Promise<T> => {
    forget();
    try {
      return await write();
    } finally {
      forget();
    }
  };
  const remembering: OutboxStorageModel = {
    applyBatch: (mutations) => (mutations.some(({ kind }) => kind === "put")
      ? writing(() => storage.applyBatch(mutations))
      : storage.applyBatch(mutations)),
    claimTabStep: (input) => writing(() => storage.claimTabStep(input)),
    clear: () => storage.clear(),
    countEntries: () => noting(() => storage.countEntries(), (count) => count === 0),
    listEntries(input) {
      if (input.afterSeq !== null) return storage.listEntries(input);
      // Only this tab's own reads answer from it; one of every tab's that found none tells it too.
      if (input.tabId === tabId && remembered()) return Promise.resolve(NOTHING);
      if (input.tabId !== undefined && input.tabId !== tabId) return storage.listEntries(input);
      return noting(() => storage.listEntries(input), (page) => page.entries.length === 0);
    },
    listEntriesByKey: (input) => storage.listEntriesByKey(input),
    listTabIds: (input) => storage.listTabIds(input),
    put: (input) => writing(() => storage.put(input)),
    remove: (removedTabId, entryKey) => storage.remove(removedTabId, entryKey),
  };
  return { forget, storage: remembering };
}
