import { aesGcmDecrypt, aesGcmEncrypt, importLocalDataKey, passphraseDataKey, randomBytes } from "./browserCrypto";
import { LOCAL_PASSPHRASE_BOX_WRAPPED_KEY } from "./localPassphraseBoxSchema";
import {
  claimLocalKeyValue,
  readLocalKeyValue,
  removeLocalKeyValue,
  writeLocalKeyValue,
} from "./localKeyDatabase";
import {
  createLocalSecretBox,
  isLocalCryptoBytes,
  type LocalSecretBox,
  type LocalSecretBoxOptions,
} from "./localSecretBox";

const WRAP_ALGORITHM = "PBKDF2-SHA256+AES-GCM";
const DEFAULT_PBKDF2_ITERATIONS = 600_000;
const MIN_PBKDF2_ITERATIONS = 100_000;
const MAX_STORED_PBKDF2_ITERATIONS = 10_000_000;

interface WrappedKeyRecord {
  algorithm: typeof WRAP_ALGORITHM;
  iterations: number;
  iv: Uint8Array<ArrayBuffer>;
  salt: Uint8Array<ArrayBuffer>;
  wrappedKey: Uint8Array<ArrayBuffer>;
}

export interface LocalPassphraseBoxOptions extends LocalSecretBoxOptions {
  allowUnsafeLowIterationsForTest?: boolean;
  beforeSchemaMigration?: (version: number) => void;
  factory?: IDBFactory;
  iterations?: number;
  onFailure?: (
    stage: "capability" | "key-claim" | "key-rewrap",
    error: unknown,
  ) => void;
}

export type LocalPassphraseBoxResult =
  | { box: LocalSecretBox; created: boolean }
  | { error: "unavailable" | "wrong-passphrase" };

function observeFailure(
  options: LocalPassphraseBoxOptions,
  stage: "capability" | "key-claim" | "key-rewrap",
  error: unknown,
): void {
  try {
    options.onFailure?.(stage, error);
  } catch {
    // Diagnostics are observational and never replace lock behavior.
  }
}

function resolveIterations(options: LocalPassphraseBoxOptions): number {
  const iterations = options.iterations ?? DEFAULT_PBKDF2_ITERATIONS;
  if (!Number.isSafeInteger(iterations) || iterations <= 0) {
    throw new RangeError("PBKDF2 iterations must be a positive safe integer.");
  }
  if (iterations < MIN_PBKDF2_ITERATIONS && !options.allowUnsafeLowIterationsForTest) {
    throw new RangeError(
      `PBKDF2 iterations must be at least ${MIN_PBKDF2_ITERATIONS}.`,
    );
  }
  if (iterations > MAX_STORED_PBKDF2_ITERATIONS) {
    throw new RangeError("PBKDF2 iterations exceed the supported safety bound.");
  }
  return iterations;
}

function validWrappedKeyRecord(value: unknown): value is WrappedKeyRecord {
  if (!value || typeof value !== "object") return false;
  const record = value as Partial<WrappedKeyRecord>;
  return (
    record.algorithm === WRAP_ALGORITHM
    && Number.isSafeInteger(record.iterations)
    && (record.iterations ?? 0) > 0
    && (record.iterations ?? 0) <= MAX_STORED_PBKDF2_ITERATIONS
    && isLocalCryptoBytes(record.iv)
    && record.iv.byteLength === 12
    && isLocalCryptoBytes(record.salt)
    && record.salt.byteLength === 16
    && isLocalCryptoBytes(record.wrappedKey)
    && record.wrappedKey.byteLength > 0
    && record.wrappedKey.byteLength <= 128
  );
}

// The data key is wrapped as its raw bytes under AES-GCM with the key the
// passphrase gives (PBKDF2-SHA256) — as Web Crypto's wrapKey("raw") writes
// it, so a record kept before reads the same — in the browser's crypto, or
// in script on a page without it (browserCrypto.ts).
async function unwrapRawKey(passphrase: string, record: WrappedKeyRecord): Promise<Uint8Array<ArrayBuffer> | null> {
  const wrappingKey = await passphraseDataKey(passphrase, record.salt, record.iterations);
  try {
    return await aesGcmDecrypt(wrappingKey, record.iv, record.wrappedKey);
  } catch {
    return null;
  }
}

async function unwrapRecord(
  passphrase: string,
  record: WrappedKeyRecord,
  options: LocalPassphraseBoxOptions,
  created: boolean,
): Promise<LocalPassphraseBoxResult> {
  const raw = await unwrapRawKey(passphrase, record);
  if (!raw) return { error: "wrong-passphrase" };
  try {
    return {
      box: createLocalSecretBox(await importLocalDataKey(raw), {
        rejectUnsealed: options.rejectUnsealed,
      }),
      created,
    };
  } finally {
    raw.fill(0);
  }
}

async function nextWrappedKeyRecord(
  raw: Uint8Array<ArrayBuffer>,
  passphrase: string,
  iterations: number,
): Promise<WrappedKeyRecord> {
  const salt = randomBytes(16);
  const iv = randomBytes(12);
  const wrappingKey = await passphraseDataKey(passphrase, salt, iterations);
  return {
    algorithm: WRAP_ALGORITHM,
    iterations,
    iv,
    salt,
    wrappedKey: await aesGcmEncrypt(wrappingKey, iv, raw),
  };
}

export async function createLocalPassphraseBox(
  name: string,
  passphrase: string,
  options: LocalPassphraseBoxOptions = {},
): Promise<LocalPassphraseBoxResult> {
  const factory = options.factory ?? globalThis.indexedDB;
  if (!factory || !name || !passphrase) {
    observeFailure(
      options,
      "capability",
      new Error("Local passphrase capability is unavailable."),
    );
    return { error: "unavailable" };
  }
  const iterations = resolveIterations(options);
  const databaseName = `${name}::keys`;
  try {
    const claim = await claimLocalKeyValue<WrappedKeyRecord>({
      ...(options.beforeSchemaMigration
        ? { beforeSchemaMigration: options.beforeSchemaMigration }
        : {}),
      async createValue() {
        const raw = randomBytes(32);
        try {
          return await nextWrappedKeyRecord(raw, passphrase, iterations);
        } finally {
          raw.fill(0);
        }
      },
      databaseName,
      factory,
      isValid: validWrappedKeyRecord,
      key: LOCAL_PASSPHRASE_BOX_WRAPPED_KEY,
    });
    return claim
      ? unwrapRecord(passphrase, claim.value, options, claim.created)
      : { error: "unavailable" };
  } catch (error) {
    observeFailure(options, "key-claim", error);
    return { error: "unavailable" };
  }
}

export async function changeLocalPassphraseBox(
  name: string,
  currentPassphrase: string,
  nextPassphrase: string,
  options: LocalPassphraseBoxOptions = {},
): Promise<LocalPassphraseBoxResult> {
  const factory = options.factory ?? globalThis.indexedDB;
  if (!factory || !name || !currentPassphrase || !nextPassphrase) {
    observeFailure(
      options,
      "capability",
      new Error("Local passphrase capability is unavailable."),
    );
    return { error: "unavailable" };
  }
  const iterations = resolveIterations(options);
  const databaseName = `${name}::keys`;
  try {
    const stored = await readLocalKeyValue(
      databaseName,
      LOCAL_PASSPHRASE_BOX_WRAPPED_KEY,
      factory,
      options.beforeSchemaMigration,
    );
    if (!validWrappedKeyRecord(stored)) return { error: "unavailable" };
    const raw = await unwrapRawKey(currentPassphrase, stored);
    if (!raw) return { error: "wrong-passphrase" };
    try {
      const nextRecord = await nextWrappedKeyRecord(raw, nextPassphrase, iterations);
      const dataKey = await importLocalDataKey(raw);
      await writeLocalKeyValue(
        databaseName,
        LOCAL_PASSPHRASE_BOX_WRAPPED_KEY,
        nextRecord,
        factory,
      );
      return {
        box: createLocalSecretBox(dataKey, {
          rejectUnsealed: options.rejectUnsealed,
        }),
        created: false,
      };
    } finally {
      raw.fill(0);
    }
  } catch (error) {
    observeFailure(options, "key-rewrap", error);
    return { error: "unavailable" };
  }
}

export async function removeLocalPassphraseBox(
  name: string,
  factory: IDBFactory | undefined = globalThis.indexedDB,
): Promise<void> {
  if (!factory || !name) throw new Error("Passphrase key storage is unavailable.");
  await removeLocalKeyValue(
    `${name}::keys`,
    LOCAL_PASSPHRASE_BOX_WRAPPED_KEY,
    factory,
  );
}
