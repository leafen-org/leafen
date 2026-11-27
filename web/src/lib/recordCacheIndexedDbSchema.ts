export const RECORD_CACHE_INDEXED_DB_SCHEMA_VERSION = 1;
export const RECORD_CACHE_RECORD_STORE = "records";
export const RECORD_CACHE_META_STORE = "meta";
export const RECORD_CACHE_BY_TABLE_INDEX = "byTable";

export interface RecordCacheIndexedDbMigration {
  apply(database: IDBDatabase): void;
  version: number;
}

function createInitialSchema(database: IDBDatabase): void {
  const records = database.createObjectStore(RECORD_CACHE_RECORD_STORE, {
    keyPath: "key",
  });
  records.createIndex(RECORD_CACHE_BY_TABLE_INDEX, "table", { unique: false });
  database.createObjectStore(RECORD_CACHE_META_STORE);
}

export const RECORD_CACHE_INDEXED_DB_MIGRATIONS:
readonly RecordCacheIndexedDbMigration[] = [
  { apply: createInitialSchema, version: 1 },
];

export function applyRecordCacheIndexedDbMigrations(
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
      || newVersion !== RECORD_CACHE_INDEXED_DB_SCHEMA_VERSION
      || oldVersion >= newVersion
    ) {
      throw new Error(
        "Browser record-cache schema upgrade has an unsupported version boundary.",
      );
    }
    for (let version = oldVersion + 1; version <= newVersion; version += 1) {
      const migration = RECORD_CACHE_INDEXED_DB_MIGRATIONS[version - 1];
      if (!migration || migration.version !== version) {
        throw new Error(
          `Browser record-cache schema migration ${version} is missing or unordered.`,
        );
      }
      beforeMigration?.(version);
      migration.apply(database);
    }
  } catch (cause) {
    // The versionchange transaction is the complete pre-upgrade recovery
    // point, so aborting retains the previous schema and all stored values.
    transaction.abort();
    throw cause;
  }
}
