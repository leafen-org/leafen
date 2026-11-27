import type { LocalSecretBox } from "./localSecretBox";
import type { OutboxEntry } from "./outboxOperationTypes";
import type {
  OutboxEntryPage,
  OutboxStorageModel,
  OutboxStorageMutation,
} from "./outboxStorageModel";

const OUTBOX_VALUE_CONTEXT = "leafen-outbox-value";

export interface OutboxSealedStorageOptions {
  box: LocalSecretBox;
  maxMutationsPerBatch: number;
  storageName: string;
}

function assertOptions(options: OutboxSealedStorageOptions): void {
  if (
    !Number.isSafeInteger(options.maxMutationsPerBatch)
    || options.maxMutationsPerBatch <= 0
  ) {
    throw new RangeError("maxMutationsPerBatch must be a positive safe integer.");
  }
  if (typeof options.storageName !== "string" || options.storageName.length === 0) {
    throw new TypeError("storageName must be a non-empty string.");
  }
}

function valueContext(storageName: string, entryKey: string): string {
  return JSON.stringify([OUTBOX_VALUE_CONTEXT, storageName, entryKey]);
}

export function createOutboxSealedStorage(
  storage: OutboxStorageModel,
  options: OutboxSealedStorageOptions,
): OutboxStorageModel {
  assertOptions(options);

  const sealValue = async (
    entryKey: string,
    value: OutboxEntry["value"],
  ): Promise<OutboxEntry["value"]> => (
    await options.box.seal(value, valueContext(options.storageName, entryKey))
  ) as OutboxEntry["value"];

  const openEntry = async (entry: OutboxEntry): Promise<OutboxEntry> => {
    const value = await options.box.open<OutboxEntry["value"]>(
      entry.value,
      valueContext(options.storageName, entry.entryKey),
    );
    if (value === undefined) {
      throw new Error("Stored outbox value could not be opened.");
    }
    return { ...entry, value };
  };

  const openPage = async (page: OutboxEntryPage): Promise<OutboxEntryPage> => ({
    entries: await Promise.all(page.entries.map(openEntry)),
    nextAfterSeq: page.nextAfterSeq,
  });

  const sealMutation = async (
    mutation: OutboxStorageMutation,
  ): Promise<OutboxStorageMutation> => (
    mutation.kind === "put"
      ? {
          ...mutation,
          value: await sealValue(mutation.entryKey, mutation.value),
        }
      : mutation
  );

  return {
    async applyBatch(mutations) {
      if (
        !Array.isArray(mutations)
        || mutations.length > options.maxMutationsPerBatch
      ) {
        throw new RangeError("Outbox mutations exceed their configured batch bound.");
      }
      return storage.applyBatch(await Promise.all(mutations.map(sealMutation)));
    },

    async claimTabStep(input) {
      return openPage(await storage.claimTabStep(input));
    },

    clear() {
      return storage.clear();
    },

    countEntries() {
      return storage.countEntries();
    },

    async listEntries(input) {
      return openPage(await storage.listEntries(input));
    },

    async listEntriesByKey(input) {
      return openPage(await storage.listEntriesByKey(input));
    },

    listTabIds(input) {
      return storage.listTabIds(input);
    },

    async put(entry) {
      return storage.put({
        ...entry,
        value: await sealValue(entry.entryKey, entry.value),
      });
    },

    remove(tabId, entryKey) {
      return storage.remove(tabId, entryKey);
    },
  };
}
