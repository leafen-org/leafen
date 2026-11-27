export const OUTBOX_INDEXED_DB_SCHEMA_VERSION = 1;
export const OUTBOX_ENTRY_STORE = "entries";
export const OUTBOX_META_STORE = "meta";
export const OUTBOX_META_SEQUENCE_KEY = "seq";
export const OUTBOX_BY_SEQUENCE_INDEX = "by-seq";
export const OUTBOX_BY_TAB_SEQUENCE_INDEX = "by-tab-seq";
export const OUTBOX_BY_ENTRY_KEY_SEQUENCE_INDEX = "by-entry-key-seq";
export const OUTBOX_BY_TAB_ID_INDEX = "by-tab-id";

export interface OutboxIndexedDbMigration {
  apply(database: IDBDatabase): void;
  version: number;
}

function createInitialSchema(database: IDBDatabase): void {
  const entries = database.createObjectStore(OUTBOX_ENTRY_STORE, {
    keyPath: "storageKey",
  });
  entries.createIndex(OUTBOX_BY_SEQUENCE_INDEX, "seq", { unique: true });
  entries.createIndex(
    OUTBOX_BY_TAB_SEQUENCE_INDEX,
    ["tabId", "seq"],
    { unique: true },
  );
  entries.createIndex(
    OUTBOX_BY_ENTRY_KEY_SEQUENCE_INDEX,
    ["entryKey", "seq"],
    { unique: true },
  );
  entries.createIndex(OUTBOX_BY_TAB_ID_INDEX, "tabId", { unique: false });
  database.createObjectStore(OUTBOX_META_STORE);
}

export const OUTBOX_INDEXED_DB_MIGRATIONS: readonly OutboxIndexedDbMigration[] = [
  { apply: createInitialSchema, version: 1 },
];

export function applyOutboxIndexedDbMigrations(
  database: IDBDatabase,
  transaction: IDBTransaction,
  oldVersion: number,
  newVersion: number | null,
  beforeMigration?: (version: number) => void,
): void {
  try {
    if (
      !Number.isSafeInteger(oldVersion)
      || oldVersion < 0
      || newVersion !== OUTBOX_INDEXED_DB_SCHEMA_VERSION
      || oldVersion >= newVersion
    ) {
      throw new Error("Browser outbox schema upgrade has an unsupported version boundary.");
    }
    for (let version = oldVersion + 1; version <= newVersion; version += 1) {
      const migration = OUTBOX_INDEXED_DB_MIGRATIONS[version - 1];
      if (!migration || migration.version !== version) {
        throw new Error(`Browser outbox schema migration ${version} is missing or unordered.`);
      }
      beforeMigration?.(version);
      migration.apply(database);
    }
  } catch (cause) {
    // A versionchange transaction is the format's atomic pre-upgrade recovery
    // point: abort restores the complete prior schema and records.
    transaction.abort();
    throw cause;
  }
}
