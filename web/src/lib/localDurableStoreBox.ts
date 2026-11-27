import { createLocalDeviceSecretBox } from "./localDeviceSecretBox";
import { awaitLocalBox } from "./localLock";
import type { LocalBoxGate } from "./localLockGate";
import type { LocalSecretBox } from "./localSecretBox";

const DEVICE_ENCRYPTION_DISABLED_KEY = "leafen.encryption.disabled";

export type LocalDurableStoreBox =
  | { kind: "network-only" }
  | { kind: "plaintext" }
  | { kind: "unavailable" }
  | { box: LocalSecretBox; kind: "sealed" };

export interface LocalDurableStoreBoxOptions {
  encryptionDisabled?: () => boolean;
  factory?: IDBFactory;
  /**
   * The passphrase lock's answer, when the caller waited for it already: a
   * store's opener waits for it before taking the store's lock, since a
   * person may never answer the unlock prompt (2026-10-18: a tab whose
   * prompt nobody answered held the lock, and other tabs and the lock's
   * settings waited on it).
   */
  gate?: LocalBoxGate;
  maxDatabaseNameCharacters: number;
  storeName: string;
  userId: string;
}

function browserDeviceEncryptionDisabled(): boolean {
  try {
    return globalThis.localStorage?.getItem(DEVICE_ENCRYPTION_DISABLED_KEY) === "1";
  } catch {
    return false;
  }
}

function deviceEncryptionDisabled(options: LocalDurableStoreBoxOptions): boolean {
  try {
    return (options.encryptionDisabled ?? browserDeviceEncryptionDisabled)() === true;
  } catch {
    return false;
  }
}

/**
 * The passphrase lock's answer for a store's box, which its opener waits for
 * before taking the store's lock and passes as `gate`.
 */
export function awaitLocalDurableStoreGate(userId: string): Promise<LocalBoxGate> {
  return awaitLocalBox(userId);
}

export async function resolveLocalDurableStoreBox(
  options: LocalDurableStoreBoxOptions,
): Promise<LocalDurableStoreBox> {
  const gate = options.gate !== undefined ? options.gate : await awaitLocalBox(options.userId);
  if (gate === null) return { kind: "network-only" };
  if (gate !== "device") return { box: gate, kind: "sealed" };
  if (deviceEncryptionDisabled(options)) return { kind: "plaintext" };
  const device = await createLocalDeviceSecretBox(options.storeName, {
    ...(options.factory === undefined ? {} : { factory: options.factory }),
    maxDatabaseNameCharacters: options.maxDatabaseNameCharacters,
  });
  return device
    ? { box: device.box, kind: "sealed" }
    : { kind: "unavailable" };
}
