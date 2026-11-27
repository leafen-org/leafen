import {
  NATIVE_OFFLINE_YGO_STORE,
  NATIVE_OFFLINE_YGO_UPDATE_STORE,
  withNativeOfflineLegacyDatabase,
} from "./nativeOfflineLegacySchema";

const YGO_RECOVERY_FORMAT_VERSION = 3;
const LEGACY_YGO_RECOVERY_FORMAT_VERSION = 2;
const YGO_COMPACTION_MAX_UPDATES = 128;
const YGO_COMPACTION_MAX_DELTA_BYTES = 1 << 20;
const YGO_COMPACTION_WRITE_THRESHOLD = 128;

type YgoRecoveryMetadata = {
  id: string;
  formatVersion: number;
  nextSequence: number;
  totalBytes: number;
  updatesSinceCompaction: number;
  updatedAt: number;
  /** Whether this device made text edits of its own to the page. */
  localEdits?: boolean;
  /** The sequence of the last update holding this device's own edits. */
  lastLocalSequence?: number;
  /** The copy's next sequence when the server was last seen to hold all of it. */
  serverHasThrough?: number;
};

type LegacyYgoRecovery = {
  id: string;
  update?: Uint8Array;
};

type YgoRecoveryUpdate = {
  documentId: string;
  sequence: number;
  update: Uint8Array;
};

function ygoRecoveryId(userId: string, pageId: string): string {
  return JSON.stringify([userId, pageId]);
}

// Every recovery id of one account shares this prefix.
function ygoRecoveryRange(userId: string): IDBKeyRange {
  const prefix = `${JSON.stringify([userId]).slice(0, -1)},`;
  return IDBKeyRange.bound(prefix, `${prefix}\uffff`);
}

// Accounts that signed out on this device keep no recovery log, even from
// an editor that is still closing; opening a page again lifts this.
const forgottenUsers = new Set<string>();

function invalidRecoveryState(message: string): DOMException {
  return new DOMException(message, "InvalidStateError");
}

function parsedMetadata(value: unknown, documentId: string): YgoRecoveryMetadata | null {
  if (!value || typeof value !== "object") return null;
  const candidate = value as Partial<YgoRecoveryMetadata>;
  if (
    candidate.formatVersion !== YGO_RECOVERY_FORMAT_VERSION
    && candidate.formatVersion !== LEGACY_YGO_RECOVERY_FORMAT_VERSION
  ) return null;
  if (
    candidate.id !== documentId
    || !Number.isSafeInteger(candidate.nextSequence)
    || (candidate.nextSequence ?? -1) < 0
    || !Number.isSafeInteger(candidate.totalBytes)
    || (candidate.totalBytes ?? -1) < 0
    || (
      candidate.formatVersion === YGO_RECOVERY_FORMAT_VERSION
      && (
        !Number.isSafeInteger(candidate.updatesSinceCompaction)
        || (candidate.updatesSinceCompaction ?? -1) < 0
      )
    )
  ) {
    throw invalidRecoveryState("Ygo recovery metadata is invalid.");
  }
  return {
    ...(candidate as YgoRecoveryMetadata),
    updatesSinceCompaction: candidate.formatVersion === LEGACY_YGO_RECOVERY_FORMAT_VERSION
      ? Math.min(candidate.nextSequence ?? 0, YGO_COMPACTION_WRITE_THRESHOLD)
      : candidate.updatesSinceCompaction ?? 0,
  };
}

export async function appendYgoRecoveryUpdate(
  userId: string,
  pageId: string,
  update: Uint8Array,
  local = false,
): Promise<boolean> {
  if (forgottenUsers.has(userId)) return false;
  const documentId = ygoRecoveryId(userId, pageId);
  return withNativeOfflineLegacyDatabase((database) => (
    new Promise<boolean>((resolve, reject) => {
      const transaction = database.transaction(
        [NATIVE_OFFLINE_YGO_STORE, NATIVE_OFFLINE_YGO_UPDATE_STORE],
        "readwrite",
      );
      const metadataStore = transaction.objectStore(NATIVE_OFFLINE_YGO_STORE);
      const updateStore = transaction.objectStore(NATIVE_OFFLINE_YGO_UPDATE_STORE);
      let operationFailure: unknown;
      let compactionReady = false;
      const request = metadataStore.get(documentId);
      request.onsuccess = () => {
        try {
          const stored = request.result as LegacyYgoRecovery | YgoRecoveryMetadata | undefined;
          let metadata = parsedMetadata(stored, documentId);
          const legacy = stored as LegacyYgoRecovery | undefined;
          let nextSequence = metadata?.nextSequence ?? 0;
          let totalBytes = metadata?.totalBytes ?? 0;
          let updatesSinceCompaction = metadata?.updatesSinceCompaction ?? 0;
          if (!metadata && stored && "formatVersion" in stored) {
            throw invalidRecoveryState("Ygo recovery format is unsupported.");
          }
          if (!metadata && legacy?.update) {
            const legacyUpdate = new Uint8Array(legacy.update);
            updateStore.put({ documentId, sequence: nextSequence, update: legacyUpdate.slice() });
            nextSequence += 1;
            totalBytes += legacyUpdate.byteLength;
            updatesSinceCompaction += 1;
          }
          if (!Number.isSafeInteger(nextSequence + 1)) {
            throw invalidRecoveryState("Ygo recovery sequence is exhausted.");
          }
          updateStore.put({ documentId, sequence: nextSequence, update: update.slice() });
          updatesSinceCompaction += 1;
          const lastLocalSequence = local ? nextSequence : metadata?.lastLocalSequence;
          metadata = {
            id: documentId,
            formatVersion: YGO_RECOVERY_FORMAT_VERSION,
            nextSequence: nextSequence + 1,
            totalBytes: totalBytes + update.byteLength,
            updatesSinceCompaction,
            updatedAt: Date.now(),
            localEdits: (metadata?.localEdits ?? false) || local,
            ...(lastLocalSequence === undefined ? {} : { lastLocalSequence }),
            ...(metadata?.serverHasThrough === undefined ? {} : { serverHasThrough: metadata.serverHasThrough }),
          };
          metadataStore.put(metadata);
          compactionReady = updatesSinceCompaction >= YGO_COMPACTION_WRITE_THRESHOLD;
        } catch (error) {
          operationFailure = error;
          transaction.abort();
        }
      };
      request.onerror = () => {
        operationFailure = request.error;
        transaction.abort();
      };
      transaction.oncomplete = () => resolve(compactionReady);
      transaction.onerror = () => reject(
        operationFailure ?? transaction.error ?? invalidRecoveryState("Unable to persist Ygo recovery update."),
      );
      transaction.onabort = () => reject(
        operationFailure ?? transaction.error ?? invalidRecoveryState("Unable to persist Ygo recovery update."),
      );
    })
  ));
}

export async function compactYgoRecoveryUpdates(
  userId: string,
  pageId: string,
  merge: (updates: Uint8Array[]) => Uint8Array,
): Promise<boolean> {
  const documentId = ygoRecoveryId(userId, pageId);
  return withNativeOfflineLegacyDatabase((database) => (
    new Promise<boolean>((resolve, reject) => {
      const transaction = database.transaction(
        [NATIVE_OFFLINE_YGO_STORE, NATIVE_OFFLINE_YGO_UPDATE_STORE],
        "readwrite",
      );
      const metadataStore = transaction.objectStore(NATIVE_OFFLINE_YGO_STORE);
      const updateStore = transaction.objectStore(NATIVE_OFFLINE_YGO_UPDATE_STORE);
      let operationFailure: unknown;
      let hasMore = false;
      const fail = (error: unknown) => {
        operationFailure = error;
        transaction.abort();
      };
      const metadataRequest = metadataStore.get(documentId);
      metadataRequest.onsuccess = () => {
        try {
          const stored = metadataRequest.result as LegacyYgoRecovery | YgoRecoveryMetadata | undefined;
          const metadata = parsedMetadata(stored, documentId);
          const legacy = stored as LegacyYgoRecovery | undefined;
          if (!metadata) {
            if (stored && "formatVersion" in stored) {
              throw invalidRecoveryState("Ygo recovery format is unsupported.");
            }
            if (legacy?.update) {
              const legacyUpdate = new Uint8Array(legacy.update);
              updateStore.put({ documentId, sequence: 0, update: legacyUpdate.slice() });
              metadataStore.put({
                id: documentId,
                formatVersion: YGO_RECOVERY_FORMAT_VERSION,
                nextSequence: 1,
                totalBytes: legacyUpdate.byteLength,
                updatesSinceCompaction: 0,
                updatedAt: Date.now(),
              } satisfies YgoRecoveryMetadata);
            }
            return;
          }

          const updates: Uint8Array[] = [];
          const sequences: number[] = [];
          let capturedBytes = 0;
          let capturedDeltaBytes = 0;
          let finished = false;
          const finish = (more: boolean) => {
            if (finished) return;
            finished = true;
            hasMore = more;
            if (updates.length < 2) {
              if (
                metadata.formatVersion !== YGO_RECOVERY_FORMAT_VERSION
                || metadata.updatesSinceCompaction !== 0
              ) {
                metadataStore.put({
                  ...metadata,
                  formatVersion: YGO_RECOVERY_FORMAT_VERSION,
                  updatesSinceCompaction: 0,
                  updatedAt: Date.now(),
                });
              }
              hasMore = false;
              return;
            }
            const merged = merge(updates);
            if (!(merged instanceof Uint8Array)) {
              throw invalidRecoveryState("Ygo recovery compaction result is invalid.");
            }
            const totalBytes = metadata.totalBytes - capturedBytes + merged.byteLength;
            if (!Number.isSafeInteger(totalBytes) || totalBytes < 0) {
              throw invalidRecoveryState("Ygo recovery byte total is invalid.");
            }
            for (const sequence of sequences) updateStore.delete([documentId, sequence]);
            updateStore.put({
              documentId,
              sequence: sequences[0],
              update: merged.slice(),
            } satisfies YgoRecoveryUpdate);
            metadataStore.put({
              ...metadata,
              formatVersion: YGO_RECOVERY_FORMAT_VERSION,
              totalBytes,
              updatesSinceCompaction: more ? YGO_COMPACTION_WRITE_THRESHOLD : 0,
              updatedAt: Date.now(),
            });
          };
          const range = IDBKeyRange.bound(
            [documentId, 0],
            [documentId, Number.MAX_SAFE_INTEGER],
          );
          const cursorRequest = updateStore.openCursor(range);
          cursorRequest.onsuccess = () => {
            try {
              const cursor = cursorRequest.result;
              if (!cursor) {
                finish(false);
                return;
              }
              if (updates.length >= YGO_COMPACTION_MAX_UPDATES) {
                finish(true);
                return;
              }
              const record = cursor.value as Partial<YgoRecoveryUpdate>;
              if (
                record.documentId !== documentId
                || !Number.isSafeInteger(record.sequence)
                || !(record.update instanceof Uint8Array)
              ) {
                throw invalidRecoveryState("Ygo recovery update is invalid.");
              }
              const sequence = record.sequence as number;
              const update = new Uint8Array(record.update);
              if (
                updates.length >= 2
                && capturedDeltaBytes + update.byteLength > YGO_COMPACTION_MAX_DELTA_BYTES
              ) {
                finish(true);
                return;
              }
              updates.push(update);
              sequences.push(sequence);
              capturedBytes += update.byteLength;
              if (updates.length > 1) capturedDeltaBytes += update.byteLength;
              cursor.continue();
            } catch (error) {
              fail(error);
            }
          };
          cursorRequest.onerror = () => fail(cursorRequest.error);
        } catch (error) {
          fail(error);
        }
      };
      metadataRequest.onerror = () => fail(metadataRequest.error);
      transaction.oncomplete = () => resolve(hasMore);
      transaction.onerror = () => reject(
        operationFailure ?? transaction.error ?? invalidRecoveryState("Unable to compact Ygo recovery updates."),
      );
      transaction.onabort = () => reject(
        operationFailure ?? transaction.error ?? invalidRecoveryState("Unable to compact Ygo recovery updates."),
      );
    })
  ));
}

export async function replayYgoRecoveryUpdates(
  userId: string,
  pageId: string,
  apply: (update: Uint8Array) => void,
): Promise<void> {
  forgottenUsers.delete(userId);
  const documentId = ygoRecoveryId(userId, pageId);
  return withNativeOfflineLegacyDatabase((database) => (
    new Promise<void>((resolve, reject) => {
      const transaction = database.transaction(
        [NATIVE_OFFLINE_YGO_STORE, NATIVE_OFFLINE_YGO_UPDATE_STORE],
      );
      let operationFailure: unknown;
      const metadataRequest = transaction.objectStore(NATIVE_OFFLINE_YGO_STORE).get(documentId);
      metadataRequest.onsuccess = () => {
        try {
          const stored = metadataRequest.result as LegacyYgoRecovery | YgoRecoveryMetadata | undefined;
          const metadata = parsedMetadata(stored, documentId);
          const legacy = stored as LegacyYgoRecovery | undefined;
          if (!metadata) {
            if (stored && "formatVersion" in stored) {
              throw invalidRecoveryState("Ygo recovery format is unsupported.");
            }
            if (legacy?.update) apply(new Uint8Array(legacy.update));
            return;
          }
          const range = IDBKeyRange.bound(
            [documentId, 0],
            [documentId, Number.MAX_SAFE_INTEGER],
          );
          const cursorRequest = transaction
            .objectStore(NATIVE_OFFLINE_YGO_UPDATE_STORE)
            .openCursor(range);
          cursorRequest.onsuccess = () => {
            try {
              const cursor = cursorRequest.result;
              if (!cursor) return;
              const record = cursor.value as Partial<YgoRecoveryUpdate>;
              if (
                record.documentId !== documentId
                || !Number.isSafeInteger(record.sequence)
                || !(record.update instanceof Uint8Array)
              ) {
                throw invalidRecoveryState("Ygo recovery update is invalid.");
              }
              apply(new Uint8Array(record.update));
              cursor.continue();
            } catch (error) {
              operationFailure = error;
              transaction.abort();
            }
          };
          cursorRequest.onerror = () => {
            operationFailure = cursorRequest.error;
            transaction.abort();
          };
        } catch (error) {
          operationFailure = error;
          transaction.abort();
        }
      };
      metadataRequest.onerror = () => {
        operationFailure = metadataRequest.error;
        transaction.abort();
      };
      transaction.oncomplete = () => resolve();
      transaction.onerror = () => reject(
        operationFailure ?? transaction.error ?? invalidRecoveryState("Unable to read Ygo recovery updates."),
      );
      transaction.onabort = () => reject(
        operationFailure ?? transaction.error ?? invalidRecoveryState("Unable to read Ygo recovery updates."),
      );
    })
  ));
}

/** A page this device edited: every update kept for it, and the copy's next sequence as read. */
export type YgoRecoveryLocalEdits = { pageId: string; updates: Uint8Array[]; through: number };

// Whether the server may lack edits made here: none was seen held there since.
const mayBeUnsent = (stored: Partial<YgoRecoveryMetadata>) => stored.serverHasThrough === undefined
  || (stored.lastLocalSequence ?? -1) >= stored.serverHasThrough;

/**
 * The pages an account edited on this device, with their updates; with
 * `unsent`, only those the server wasn't seen to hold since their last edit
 * here (noteYgoRecoveryServerHas).
 */
export async function listYgoRecoveryLocalEdits(userId: string, { unsent = false } = {}): Promise<YgoRecoveryLocalEdits[]> {
  if (unsent && forgottenUsers.has(userId)) return [];
  return withNativeOfflineLegacyDatabase((database) => (
    new Promise<YgoRecoveryLocalEdits[]>((resolve, reject) => {
      const transaction = database.transaction(
        [NATIVE_OFFLINE_YGO_STORE, NATIVE_OFFLINE_YGO_UPDATE_STORE],
      );
      const edited: YgoRecoveryLocalEdits[] = [];
      const metadataRequest = transaction.objectStore(NATIVE_OFFLINE_YGO_STORE).getAll(ygoRecoveryRange(userId));
      metadataRequest.onsuccess = () => {
        for (const stored of metadataRequest.result as Array<Partial<YgoRecoveryMetadata>>) {
          if (!stored.localEdits || typeof stored.id !== "string") continue;
          if (unsent && !mayBeUnsent(stored)) continue;
          const [, pageId] = JSON.parse(stored.id) as [string, string];
          const pageEdits: YgoRecoveryLocalEdits = { pageId, updates: [], through: stored.nextSequence ?? 0 };
          edited.push(pageEdits);
          const updatesRequest = transaction.objectStore(NATIVE_OFFLINE_YGO_UPDATE_STORE).getAll(
            IDBKeyRange.bound([stored.id, 0], [stored.id, Number.MAX_SAFE_INTEGER]),
          );
          updatesRequest.onsuccess = () => {
            for (const record of updatesRequest.result as Array<Partial<YgoRecoveryUpdate>>) {
              if (record.update instanceof Uint8Array) pageEdits.updates.push(new Uint8Array(record.update));
            }
          };
        }
      };
      transaction.oncomplete = () => resolve(edited);
      transaction.onerror = () => reject(transaction.error ?? invalidRecoveryState("Unable to read Ygo recovery updates."));
      transaction.onabort = () => reject(transaction.error ?? invalidRecoveryState("Unable to read Ygo recovery updates."));
    })
  ));
}

/** Notes that the server holds the page's copy up to `through` (YgoRecoveryLocalEdits); edits after it it may lack. */
export async function noteYgoRecoveryServerHas(userId: string, pageId: string, through: number): Promise<void> {
  if (forgottenUsers.has(userId)) return;
  const documentId = ygoRecoveryId(userId, pageId);
  return withNativeOfflineLegacyDatabase((database) => (
    new Promise<void>((resolve, reject) => {
      const transaction = database.transaction(NATIVE_OFFLINE_YGO_STORE, "readwrite");
      const store = transaction.objectStore(NATIVE_OFFLINE_YGO_STORE);
      const request = store.get(documentId);
      request.onsuccess = () => {
        const stored = request.result as YgoRecoveryMetadata | undefined;
        // A copy let go meanwhile isn't made again.
        if (stored?.formatVersion !== YGO_RECOVERY_FORMAT_VERSION || (stored.serverHasThrough ?? -1) >= through) return;
        store.put({ ...stored, serverHasThrough: through } satisfies YgoRecoveryMetadata);
      };
      transaction.oncomplete = () => resolve();
      transaction.onerror = transaction.onabort = () => reject(transaction.error ?? invalidRecoveryState("Unable to note Ygo recovery state."));
    })
  ));
}

/** Removes an account's page copies from this device, as at sign-out. */
export async function forgetYgoRecovery(userId: string): Promise<void> {
  forgottenUsers.add(userId);
  return withNativeOfflineLegacyDatabase((database) => (
    new Promise<void>((resolve, reject) => {
      const transaction = database.transaction(
        [NATIVE_OFFLINE_YGO_STORE, NATIVE_OFFLINE_YGO_UPDATE_STORE],
        "readwrite",
      );
      const range = ygoRecoveryRange(userId);
      transaction.objectStore(NATIVE_OFFLINE_YGO_STORE).delete(range);
      transaction.objectStore(NATIVE_OFFLINE_YGO_UPDATE_STORE).delete(
        IDBKeyRange.bound([range.lower], [range.upper]),
      );
      transaction.oncomplete = () => resolve();
      transaction.onerror = () => reject(transaction.error ?? invalidRecoveryState("Unable to remove Ygo recovery updates."));
      transaction.onabort = () => reject(transaction.error ?? invalidRecoveryState("Unable to remove Ygo recovery updates."));
    })
  ));
}

/**
 * Removes one page's copy from this device, as when the page turned
 * read-only for the account and the edits it kept can't be saved.
 */
export async function forgetYgoRecoveryPage(userId: string, pageId: string): Promise<void> {
  const documentId = ygoRecoveryId(userId, pageId);
  return withNativeOfflineLegacyDatabase((database) => (
    new Promise<void>((resolve, reject) => {
      const transaction = database.transaction(
        [NATIVE_OFFLINE_YGO_STORE, NATIVE_OFFLINE_YGO_UPDATE_STORE],
        "readwrite",
      );
      transaction.objectStore(NATIVE_OFFLINE_YGO_STORE).delete(documentId);
      transaction.objectStore(NATIVE_OFFLINE_YGO_UPDATE_STORE).delete(
        IDBKeyRange.bound([documentId, 0], [documentId, Number.MAX_SAFE_INTEGER]),
      );
      transaction.oncomplete = () => resolve();
      transaction.onerror = () => reject(transaction.error ?? invalidRecoveryState("Unable to remove Ygo recovery updates."));
      transaction.onabort = () => reject(transaction.error ?? invalidRecoveryState("Unable to remove Ygo recovery updates."));
    })
  ));
}
