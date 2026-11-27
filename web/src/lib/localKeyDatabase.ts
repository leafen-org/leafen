import {
  LOCAL_PASSPHRASE_BOX_KEY_STORE,
  openLocalPassphraseBoxDatabase,
} from "./localPassphraseBoxSchema";

export interface LocalKeyClaimOptions<Value> {
  beforeSchemaMigration?: (version: number) => void;
  createValue(): Promise<Value>;
  databaseName: string;
  factory?: IDBFactory;
  isValid(value: unknown): boolean;
  key: IDBValidKey;
}

export interface LocalKeyClaim<Value> {
  created: boolean;
  value: Value;
}

// A key stored here is kept by the browser under a key of its own; the Mac
// app's WebKit keeps that one in the keychain, and its read, when macOS asks
// first, stops the page's script until the prompt is answered. The packaged
// app passes each step that stores or reads a key through its gate
// (packagedCredentials.ts gateWebKitKeySteps), which says so first.
type KeyStepGate = <Value>(step: () => Promise<Value>) => Promise<Value>;
let keyStepGate: KeyStepGate = (step) => step();

export function gateLocalKeySteps(gate: KeyStepGate): void {
  keyStepGate = gate;
}

function requestResult<Value>(request: IDBRequest<Value>): Promise<Value> {
  return new Promise((resolve, reject) => {
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error ?? new Error("IndexedDB key request failed."));
  });
}

function transactionDone(transaction: IDBTransaction): Promise<void> {
  return new Promise((resolve, reject) => {
    transaction.oncomplete = () => resolve();
    transaction.onabort = () => reject(
      transaction.error ?? new Error("IndexedDB key transaction aborted."),
    );
    transaction.onerror = () => reject(
      transaction.error ?? new Error("IndexedDB key transaction failed."),
    );
  });
}

export async function readLocalKeyValue(
  databaseName: string,
  key: IDBValidKey,
  factory?: IDBFactory,
  beforeSchemaMigration?: (version: number) => void,
): Promise<unknown> {
  return keyStepGate(async () => {
    const database = await openLocalPassphraseBoxDatabase(
      databaseName,
      factory,
      beforeSchemaMigration,
    );
    try {
      return requestResult(
        database
          .transaction(LOCAL_PASSPHRASE_BOX_KEY_STORE, "readonly")
          .objectStore(LOCAL_PASSPHRASE_BOX_KEY_STORE)
          .get(key),
      );
    } finally {
      database.close();
    }
  });
}

export async function writeLocalKeyValue(
  databaseName: string,
  key: IDBValidKey,
  value: unknown,
  factory?: IDBFactory,
): Promise<void> {
  return keyStepGate(async () => {
    const database = await openLocalPassphraseBoxDatabase(databaseName, factory);
    try {
      const transaction = database.transaction(
        LOCAL_PASSPHRASE_BOX_KEY_STORE,
        "readwrite",
      );
      const done = transactionDone(transaction);
      transaction.objectStore(LOCAL_PASSPHRASE_BOX_KEY_STORE).put(value, key);
      await done;
    } finally {
      database.close();
    }
  });
}

export async function removeLocalKeyValue(
  databaseName: string,
  key: IDBValidKey,
  factory?: IDBFactory,
): Promise<void> {
  const database = await openLocalPassphraseBoxDatabase(databaseName, factory);
  try {
    const transaction = database.transaction(
      LOCAL_PASSPHRASE_BOX_KEY_STORE,
      "readwrite",
    );
    const done = transactionDone(transaction);
    transaction.objectStore(LOCAL_PASSPHRASE_BOX_KEY_STORE).delete(key);
    await done;
  } finally {
    database.close();
  }
}

async function addLocalKeyValueIfAbsent(
  databaseName: string,
  key: IDBValidKey,
  value: unknown,
  factory?: IDBFactory,
): Promise<boolean> {
  return keyStepGate(async () => {
    const database = await openLocalPassphraseBoxDatabase(databaseName, factory);
    try {
      const transaction = database.transaction(
        LOCAL_PASSPHRASE_BOX_KEY_STORE,
        "readwrite",
      );
      const done = transactionDone(transaction);
      let lostRace = false;
      const request = transaction
        .objectStore(LOCAL_PASSPHRASE_BOX_KEY_STORE)
        .add(value, key);
      request.onerror = (event) => {
        if (request.error?.name !== "ConstraintError") return;
        lostRace = true;
        event.preventDefault();
        event.stopPropagation();
      };
      await done;
      return !lostRace;
    } finally {
      database.close();
    }
  });
}

export async function claimLocalKeyValue<Value>(
  options: LocalKeyClaimOptions<Value>,
): Promise<LocalKeyClaim<Value> | null> {
  const existing = await readLocalKeyValue(
    options.databaseName,
    options.key,
    options.factory,
    options.beforeSchemaMigration,
  );
  if (existing !== undefined) {
    return options.isValid(existing)
      ? { created: false, value: existing as Value }
      : null;
  }
  const candidate = await options.createValue();
  if (!options.isValid(candidate)) {
    throw new TypeError("A local key claim created an invalid candidate.");
  }
  if (await addLocalKeyValueIfAbsent(
    options.databaseName,
    options.key,
    candidate,
    options.factory,
  )) {
    return { created: true, value: candidate };
  }
  const winner = await readLocalKeyValue(
    options.databaseName,
    options.key,
    options.factory,
  );
  return options.isValid(winner)
    ? { created: false, value: winner as Value }
    : null;
}
