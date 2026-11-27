export const LOCAL_PASSPHRASE_BOX_KEY_STORE = "keys";
export const LOCAL_PASSPHRASE_BOX_WRAPPED_KEY = "wrapped";
export const LOCAL_PASSPHRASE_BOX_SCHEMA_VERSION = 1;

type LocalPassphraseBoxMigration = (
  database: IDBDatabase,
  transaction: IDBTransaction,
) => void;

export const LOCAL_PASSPHRASE_BOX_MIGRATIONS: Readonly<
  Record<number, LocalPassphraseBoxMigration>
> = {
  1(database) {
    database.createObjectStore(LOCAL_PASSPHRASE_BOX_KEY_STORE);
  },
};

function applyLocalPassphraseBoxMigrations(
  database: IDBDatabase,
  transaction: IDBTransaction,
  oldVersion: number,
  newVersion: number | null,
  beforeMigration?: (version: number) => void,
): void {
  const target = newVersion ?? LOCAL_PASSPHRASE_BOX_SCHEMA_VERSION;
  if (target !== LOCAL_PASSPHRASE_BOX_SCHEMA_VERSION || oldVersion > target) {
    throw new Error("Unsupported local passphrase-box schema transition.");
  }
  for (let version = oldVersion + 1; version <= target; version += 1) {
    const migration = LOCAL_PASSPHRASE_BOX_MIGRATIONS[version];
    if (!migration) {
      throw new Error(`Missing local passphrase-box schema migration ${version}.`);
    }
    beforeMigration?.(version);
    migration(database, transaction);
  }
}

export function openLocalPassphraseBoxDatabase(
  name: string,
  factory: IDBFactory | undefined = globalThis.indexedDB,
  beforeMigration?: (version: number) => void,
): Promise<IDBDatabase> {
  if (!factory || !name) {
    return Promise.reject(new Error("IndexedDB passphrase storage is unavailable."));
  }
  return new Promise((resolve, reject) => {
    let settled = false;
    let upgradeError: unknown = null;
    const request = factory.open(name, LOCAL_PASSPHRASE_BOX_SCHEMA_VERSION);
    request.onupgradeneeded = (event) => {
      if (!request.transaction) {
        upgradeError = new Error("Passphrase-box schema upgrade has no transaction.");
        return;
      }
      try {
        applyLocalPassphraseBoxMigrations(
          request.result,
          request.transaction,
          event.oldVersion,
          event.newVersion,
          beforeMigration,
        );
      } catch (error) {
        upgradeError = error;
        request.transaction.abort();
      }
    };
    request.onsuccess = () => {
      if (settled) {
        request.result.close();
        return;
      }
      settled = true;
      request.result.onversionchange = () => request.result.close();
      resolve(request.result);
    };
    request.onerror = () => {
      if (settled) return;
      settled = true;
      reject(upgradeError ?? request.error ?? new Error("Passphrase-box open failed."));
    };
    request.onblocked = () => {
      if (settled) return;
      settled = true;
      reject(new Error("Passphrase-box schema upgrade was blocked."));
    };
  });
}
