// The browser's own crypto (crypto.subtle, crypto.randomUUID) is there only
// on a secure origin — https, or this computer's own address. Leafen is also
// opened at a plain address on its network (http://192.168.…, a NAS or
// another device joining the account), where it isn't (2026-10-26: no change could
// be kept on the device there, uploads failed, and the page said the
// connection was lost). Every use of it goes through here, which does the
// same work in script where the browser has none — with the audited, widely
// used @noble implementations, fetched only then (browserCryptoScript.ts).
// Random bytes (crypto.getRandomValues) are there on every origin.

function browserSubtle(): SubtleCrypto | null {
  try {
    return globalThis.crypto?.subtle ?? null;
  } catch {
    return null;
  }
}

type ScriptCrypto = typeof import("./browserCryptoScript");

let scriptCrypto: Promise<ScriptCrypto> | null = null;

function script(): Promise<ScriptCrypto> {
  scriptCrypto ??= import("./browserCryptoScript").catch((error: unknown) => {
    scriptCrypto = null;
    throw error;
  });
  return scriptCrypto;
}

// A page without the browser's crypto fetches the script's as it starts, so
// it is here should the network go away later.
if (!browserSubtle()) void script().catch(() => undefined);

function bytesOf(data: ArrayBuffer | Uint8Array): Uint8Array<ArrayBuffer> {
  return data instanceof Uint8Array ? data as Uint8Array<ArrayBuffer> : new Uint8Array(data);
}

function owned(bytes: Uint8Array): Uint8Array<ArrayBuffer> {
  return new Uint8Array(bytes);
}

export function randomBytes(length: number): Uint8Array<ArrayBuffer> {
  return globalThis.crypto.getRandomValues(new Uint8Array(length));
}

/** A random (version 4) UUID. */
export function randomUUID(): string {
  if (typeof globalThis.crypto?.randomUUID === "function") return globalThis.crypto.randomUUID();
  const bytes = randomBytes(16);
  bytes[6] = (bytes[6]! & 0x0f) | 0x40;
  bytes[8] = (bytes[8]! & 0x3f) | 0x80;
  const hex = bytesHex(bytes);
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}

function bytesHex(bytes: Uint8Array): string {
  return Array.from(bytes, (value) => value.toString(16).padStart(2, "0")).join("");
}

export type DigestAlgorithm = "SHA-1" | "SHA-256" | "SHA-384" | "SHA-512";

export async function digest(algorithm: DigestAlgorithm, data: ArrayBuffer | Uint8Array): Promise<Uint8Array<ArrayBuffer>> {
  const subtle = browserSubtle();
  if (subtle) return new Uint8Array(await subtle.digest(algorithm, bytesOf(data)));
  const { sha1, sha256, sha384, sha512 } = await script();
  const hash = algorithm === "SHA-512" ? sha512 : algorithm === "SHA-384" ? sha384 : algorithm === "SHA-1" ? sha1 : sha256;
  return owned(hash(bytesOf(data)));
}

// The OID namespace's UUID (RFC 9562), 6ba7b812-9dad-11d1-80b4-00c04fd430c8.
const OID_NAMESPACE = [0x6b, 0xa7, 0xb8, 0x12, 0x9d, 0xad, 0x11, 0xd1, 0x80, 0xb4, 0x00, 0xc0, 0x4f, 0xd4, 0x30, 0xc8];

/** A name-based (version 5) UUID in the OID namespace, as the server's uuid.NewSHA1(uuid.NameSpaceOID, name). */
export async function nameUUID(name: string): Promise<string> {
  const bytes = (await digest("SHA-1", Uint8Array.from([...OID_NAMESPACE, ...new TextEncoder().encode(name)]))).slice(0, 16);
  bytes[6] = (bytes[6]! & 0x0f) | 0x50;
  bytes[8] = (bytes[8]! & 0x3f) | 0x80;
  const hex = bytesHex(bytes);
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}

export async function sha256Hex(data: ArrayBuffer | Uint8Array): Promise<string> {
  return bytesHex(await digest("SHA-256", data));
}

/** AES-CBC over whole blocks, no padding added. */
export async function aesCbcEncryptBlocks(key: Uint8Array, iv: Uint8Array, data: Uint8Array): Promise<Uint8Array<ArrayBuffer>> {
  const subtle = browserSubtle();
  if (!subtle) return owned((await script()).cbc(key, iv, { disablePadding: true }).encrypt(data));
  const browserKey = await subtle.importKey("raw", owned(key), "AES-CBC", false, ["encrypt"]);
  // Web Crypto always pads: its padding block is cut off.
  return new Uint8Array(await subtle.encrypt({ iv: owned(iv), name: "AES-CBC" }, browserKey, owned(data))).subarray(0, data.length);
}

/** AES-CBC decryption of whole blocks; `padded` data ends in PKCS#7 padding, taken off where it is whole. */
export async function aesCbcDecrypt(key: Uint8Array, iv: Uint8Array, data: Uint8Array, padded: boolean): Promise<Uint8Array<ArrayBuffer>> {
  if (data.length === 0 || data.length % 16 !== 0) return new Uint8Array();
  const subtle = browserSubtle();
  if (!subtle) {
    const plain = owned((await script()).cbc(key, iv, { disablePadding: true }).decrypt(data));
    const pad = plain[plain.length - 1] ?? 0;
    const whole = padded && pad >= 1 && pad <= 16 && plain.subarray(plain.length - pad).every((value) => value === pad);
    return whole ? plain.subarray(0, plain.length - pad) : plain;
  }
  const browserKey = await subtle.importKey("raw", owned(key), "AES-CBC", false, ["decrypt"]);
  if (padded) {
    try {
      return new Uint8Array(await subtle.decrypt({ iv: owned(iv), name: "AES-CBC" }, browserKey, owned(data)));
    } catch {
      // Damaged padding still leaves the whole blocks readable.
    }
  }
  // Web Crypto only removes valid padding, so a full padding block is
  // appended as the cipher would have written it.
  const tail = data.subarray(data.length - 16);
  const paddingBlock = await aesCbcEncryptBlocks(key, tail, new Uint8Array(16).fill(16));
  const whole = new Uint8Array(data.length + 16);
  whole.set(data);
  whole.set(paddingBlock, data.length);
  return new Uint8Array(await subtle.decrypt({ iv: owned(iv), name: "AES-CBC" }, browserKey, whole));
}

/**
 * A key for what this device keeps: the browser's own AES-256-GCM key, which
 * never leaves it, where it has its crypto; else the key's bytes, kept where
 * the browser would keep its key.
 */
export type LocalDataKey = CryptoKey | ScriptDataKey;

export interface ScriptDataKey {
  readonly kind: "aes-256-gcm";
  readonly bytes: Uint8Array<ArrayBuffer>;
}

function isBytes(value: unknown): value is Uint8Array<ArrayBuffer> {
  return ArrayBuffer.isView(value) && (value as Uint8Array).BYTES_PER_ELEMENT === 1 && value.constructor.name === "Uint8Array";
}

function isScriptDataKey(value: unknown): value is ScriptDataKey {
  if (!value || typeof value !== "object") return false;
  const key = value as Partial<ScriptDataKey>;
  return key.kind === "aes-256-gcm" && isBytes(key.bytes) && key.bytes.byteLength === 32;
}

function isBrowserDataKey(value: unknown): value is CryptoKey {
  if (!value || typeof value !== "object" || "kind" in value) return false;
  const key = value as Partial<CryptoKey>;
  const algorithm = key.algorithm as Partial<AesKeyAlgorithm> | undefined;
  const usages = Array.isArray(key.usages) ? [...key.usages].sort() : [];
  return key.type === "secret" && key.extractable === false && algorithm?.name === "AES-GCM" && algorithm.length === 256
    && usages.length === 2 && usages[0] === "decrypt" && usages[1] === "encrypt";
}

/** Whether a value is a key for what this device keeps (the browser's must not be extractable). */
export function isLocalDataKey(value: unknown): value is LocalDataKey {
  return isScriptDataKey(value) || isBrowserDataKey(value);
}

export async function generateLocalDataKey(): Promise<LocalDataKey> {
  const subtle = browserSubtle();
  if (!subtle) return { bytes: randomBytes(32), kind: "aes-256-gcm" };
  return subtle.generateKey({ length: 256, name: "AES-GCM" }, false, ["decrypt", "encrypt"]);
}

/** The key of these 32 bytes, the browser's (not extractable) where it has its crypto. */
export async function importLocalDataKey(raw: Uint8Array): Promise<LocalDataKey> {
  const subtle = browserSubtle();
  if (!subtle) return { bytes: owned(raw), kind: "aes-256-gcm" };
  return subtle.importKey("raw", owned(raw), { length: 256, name: "AES-GCM" }, false, ["decrypt", "encrypt"]);
}

/** The key a passphrase gives with this salt (PBKDF2-SHA256), the browser's where it has its crypto. */
export async function passphraseDataKey(passphrase: string, salt: Uint8Array, iterations: number): Promise<LocalDataKey> {
  const words = new TextEncoder().encode(passphrase);
  const subtle = browserSubtle();
  if (!subtle) {
    const { pbkdf2Async, sha256 } = await script();
    const bytes = owned(await pbkdf2Async(sha256, words, salt, { c: iterations, dkLen: 32 }));
    return { bytes, kind: "aes-256-gcm" };
  }
  const material = await subtle.importKey("raw", words, "PBKDF2", false, ["deriveKey"]);
  return subtle.deriveKey({ hash: "SHA-256", iterations, name: "PBKDF2", salt: owned(salt) }, material,
    { length: 256, name: "AES-GCM" }, false, ["decrypt", "encrypt"]);
}

function browserKeyCrypto(): SubtleCrypto {
  const subtle = browserSubtle();
  if (!subtle) throw new Error("The browser's key needs its crypto, which this page lacks.");
  return subtle;
}

export async function aesGcmEncrypt(key: LocalDataKey, iv: Uint8Array, data: Uint8Array, additionalData?: Uint8Array): Promise<Uint8Array<ArrayBuffer>> {
  if (isScriptDataKey(key)) return owned((await script()).gcm(key.bytes, iv, additionalData).encrypt(data));
  return new Uint8Array(await browserKeyCrypto().encrypt(
    { iv: owned(iv), name: "AES-GCM", ...(additionalData ? { additionalData: owned(additionalData) } : {}) }, key, owned(data)));
}

/** The plain bytes, or a throw where the key, the context or the data don't match. */
export async function aesGcmDecrypt(key: LocalDataKey, iv: Uint8Array, data: Uint8Array, additionalData?: Uint8Array): Promise<Uint8Array<ArrayBuffer>> {
  if (isScriptDataKey(key)) return owned((await script()).gcm(key.bytes, iv, additionalData).decrypt(data));
  return new Uint8Array(await browserKeyCrypto().decrypt(
    { iv: owned(iv), name: "AES-GCM", ...(additionalData ? { additionalData: owned(additionalData) } : {}) }, key, owned(data)));
}
