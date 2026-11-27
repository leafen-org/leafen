const NATIVE_OFFLINE_LEGACY_DATABASE_NAME = "leafen-v2-offline-v1";
export const NATIVE_OFFLINE_LEGACY_SCHEMA_VERSION = 4;
export const NATIVE_OFFLINE_MUTATION_STORE = "mutations";
export const NATIVE_OFFLINE_YGO_STORE = "ygoDocuments";
export const NATIVE_OFFLINE_YGO_UPDATE_STORE = "ygoRecoveryUpdates";
export const NATIVE_OFFLINE_MUTATION_ORDER_INDEX = "byCreatedAtMutationId";

interface NativeOfflineLegacyMigration {
  apply(database: IDBDatabase, transaction: IDBTransaction): void;
  version: number;
}

export const NATIVE_OFFLINE_LEGACY_MIGRATIONS: readonly NativeOfflineLegacyMigration[] = [
  {
    version: 1,
    apply(database) {
      database.createObjectStore(NATIVE_OFFLINE_MUTATION_STORE, {
        keyPath: "mutationId",
      });
    },
  },
  {
    version: 2,
    apply(database) {
      database.createObjectStore(NATIVE_OFFLINE_YGO_STORE, { keyPath: "id" });
    },
  },
  {
    version: 3,
    apply(_database, transaction) {
      transaction.objectStore(NATIVE_OFFLINE_MUTATION_STORE).createIndex(
        NATIVE_OFFLINE_MUTATION_ORDER_INDEX,
        ["createdAt", "mutationId"],
        { unique: true },
      );
    },
  },
  {
    version: 4,
    apply(database) {
      database.createObjectStore(NATIVE_OFFLINE_YGO_UPDATE_STORE, {
        keyPath: ["documentId", "sequence"],
      });
    },
  },
];

function applyNativeOfflineLegacyMigrations(
  database: IDBDatabase,
  transaction: IDBTransaction,
  oldVersion: number,
  beforeMigration?: (version: number) => void,
): void {
  for (const migration of NATIVE_OFFLINE_LEGACY_MIGRATIONS) {
    if (migration.version <= oldVersion) continue;
    beforeMigration?.(migration.version);
    migration.apply(database, transaction);
  }
}

// The one connection a tab keeps to this store, opened when first asked for:
// each read and write of a page's copy opened it anew, and every page open
// waited for that before its room started (2026-11-13: some 35 ms under
// load). It gives way at once to another tab's upgrade or deletion of the
// store, and opens again when next asked for.
let tabConnection: Promise<IDBDatabase> | null = null;

function tabNativeOfflineLegacyDatabase(): Promise<IDBDatabase> {
  if (tabConnection) return tabConnection;
  const opening = openNativeOfflineLegacyDatabase();
  tabConnection = opening;
  const letGo = () => {
    if (tabConnection === opening) tabConnection = null;
  };
  opening.then((database) => {
    database.onversionchange = () => {
      letGo();
      database.close();
    };
    database.onclose = letGo;
  }, letGo);
  return opening;
}

/**
 * Runs `use` on the tab's connection to the store, and once more on a new
 * one when that connection was let go or lost meanwhile (WebKit says a lost
 * one with UnknownError, "Connection to Indexed Database server lost").
 */
export async function withNativeOfflineLegacyDatabase<T>(
  use: (database: IDBDatabase) => Promise<T>,
): Promise<T> {
  const opening = tabNativeOfflineLegacyDatabase();
  const database = await opening;
  try {
    return await use(database);
  } catch (error) {
    if (tabConnection === opening) {
      if (!(error instanceof DOMException && error.name === "UnknownError")) throw error;
      tabConnection = null;
      database.close();
    }
    return use(await tabNativeOfflineLegacyDatabase());
  }
}

export function openNativeOfflineLegacyDatabase(
  suppliedFactory?: IDBFactory,
  beforeMigration?: (version: number) => void,
): Promise<IDBDatabase> {
  const factory = suppliedFactory ?? globalThis.indexedDB;
  if (!factory) {
    return Promise.reject(new DOMException(
      "Native offline storage is unavailable.",
      "NotSupportedError",
    ));
  }
  return new Promise((resolve, reject) => {
    const request = factory.open(
      NATIVE_OFFLINE_LEGACY_DATABASE_NAME,
      NATIVE_OFFLINE_LEGACY_SCHEMA_VERSION,
    );
    let migrationFailure: unknown;
    let blocked = false;
    request.onupgradeneeded = (event) => {
      try {
        if (!request.transaction) {
          throw new DOMException("Native offline upgrade transaction is absent.", "InvalidStateError");
        }
        applyNativeOfflineLegacyMigrations(
          request.result,
          request.transaction,
          event.oldVersion,
          beforeMigration,
        );
      } catch (error) {
        migrationFailure = error;
        request.transaction?.abort();
      }
    };
    request.onsuccess = () => {
      if (blocked) request.result.close();
      else resolve(request.result);
    };
    request.onerror = () => reject(
      migrationFailure
      ?? request.error
      ?? new DOMException("Unable to open native offline storage.", "InvalidStateError"),
    );
    request.onblocked = () => {
      blocked = true;
      reject(new DOMException(
        "Native offline storage upgrade is blocked.",
        "InvalidStateError",
      ));
    };
  });
}
