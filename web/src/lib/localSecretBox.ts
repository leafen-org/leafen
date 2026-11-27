import { aesGcmDecrypt, aesGcmEncrypt, isLocalDataKey, randomBytes, type LocalDataKey } from "./browserCrypto";

export interface LocalSecretBox {
  readonly mode: "aes-gcm";
  open<Value = unknown>(stored: unknown, context?: string): Promise<Value | undefined>;
  seal(value: unknown, context?: string): Promise<unknown>;
}

export interface LocalSecretBoxOptions {
  rejectUnsealed?: boolean;
}

export interface LocalSecretBoxSessionSnapshot {
  readonly version: 1;
  readonly key: LocalDataKey;
  readonly rejectUnsealed: boolean;
}

interface SealedEnvelope {
  __sealed: 1;
  v?: number;
  alg?: string;
  data: Uint8Array<ArrayBuffer>;
  iv: Uint8Array<ArrayBuffer>;
}

const SEALED_VERSION_WITH_CONTEXT = 2;
const SEALED_ALGORITHM = "AES-GCM";
const sessionMetadata = new WeakMap<
  LocalSecretBox,
  { key: LocalDataKey; rejectUnsealed: boolean }
>();

export function isLocalCryptoBytes(value: unknown): value is Uint8Array<ArrayBuffer> {
  return (
    ArrayBuffer.isView(value)
    && (value as Uint8Array).BYTES_PER_ELEMENT === 1
    && value.constructor.name === "Uint8Array"
  );
}

function isSealedEnvelope(value: unknown): value is SealedEnvelope {
  return (
    value !== null
    && typeof value === "object"
    && (value as { __sealed?: unknown }).__sealed === 1
    && isLocalCryptoBytes((value as { data?: unknown }).data)
    && isLocalCryptoBytes((value as { iv?: unknown }).iv)
  );
}

// A value holding a long list (a 47,000-page workspace's page list, 37 MB of
// JSON) is written out a slice at a time, a task each, so keeping it blocks
// no frame for long: written whole it held the page 330 ms at the end of
// every list read on a slower device (2026-11-14, CPU ×2). The text is the
// same as JSON.stringify's.
const SLICE_ITEMS = 2000;
const SLICE_DEPTH = 3;

// The next task, not held back as a hidden tab's timers are.
function nextTask(): Promise<void> {
  return new Promise((resolve) => {
    const channel = new MessageChannel();
    channel.port1.onmessage = () => {
      channel.port1.close();
      resolve();
    };
    channel.port2.postMessage(null);
  });
}

function plainObject(value: unknown): value is Record<string, unknown> {
  if (value === null || typeof value !== "object" || Array.isArray(value)) return false;
  const prototype = Object.getPrototypeOf(value);
  return (prototype === Object.prototype || prototype === null) && typeof (value as { toJSON?: unknown }).toJSON !== "function";
}

function holdsLongList(value: unknown, depth: number): boolean {
  if (Array.isArray(value)) return value.length > SLICE_ITEMS;
  if (depth >= SLICE_DEPTH || !plainObject(value)) return false;
  return Object.values(value).some((item) => holdsLongList(item, depth + 1));
}

/** JSON.stringify's text, encoded, made a slice of a long list at a time. */
async function encodedInSlices(value: unknown, encoder: TextEncoder): Promise<Uint8Array<ArrayBuffer>> {
  const parts: Uint8Array[] = [];
  let pending = "";
  const put = (text: string) => { pending += text; };
  const flush = async () => {
    parts.push(encoder.encode(pending));
    pending = "";
    await nextTask();
  };
  const write = async (item: unknown, depth: number): Promise<void> => {
    if (Array.isArray(item) && item.length > SLICE_ITEMS) {
      put("[");
      for (let start = 0; start < item.length; start += SLICE_ITEMS) {
        // An array's own text, less its brackets: an item JSON leaves out writes as null there too.
        put((start ? "," : "") + JSON.stringify(item.slice(start, start + SLICE_ITEMS)).slice(1, -1));
        await flush();
      }
      put("]");
      return;
    }
    if (depth < SLICE_DEPTH && plainObject(item) && holdsLongList(item, depth)) {
      put("{");
      let first = true;
      for (const [key, field] of Object.entries(item)) {
        if (field === undefined || typeof field === "function" || typeof field === "symbol") continue;
        put((first ? "" : ",") + JSON.stringify(key) + ":");
        first = false;
        await write(field, depth + 1);
      }
      put("}");
      return;
    }
    put(JSON.stringify(item ?? null));
  };
  await write(value, 0);
  parts.push(encoder.encode(pending));
  const bytes = new Uint8Array(parts.reduce((total, part) => total + part.length, 0));
  let offset = 0;
  for (const part of parts) {
    bytes.set(part, offset);
    offset += part.length;
  }
  return bytes;
}

/**
 * A key for a box: the browser's non-extractable AES-256-GCM key, or on a
 * page without the browser's crypto the key's own bytes (browserCrypto.ts).
 */
export function isLocalSecretBoxKey(value: unknown): value is LocalDataKey {
  return isLocalDataKey(value);
}

function createBox(
  key: LocalDataKey,
  options: LocalSecretBoxOptions = {},
): LocalSecretBox {
  const encoder = new TextEncoder();
  const decoder = new TextDecoder();
  const rejectUnsealed = options.rejectUnsealed ?? false;
  const box: LocalSecretBox = {
    mode: "aes-gcm",
    async open<Value>(stored: unknown, context?: string) {
      if (!isSealedEnvelope(stored)) {
        return rejectUnsealed ? undefined : stored as Value;
      }
      const version = typeof stored.v === "number" ? stored.v : 1;
      if (version !== 1 && version !== SEALED_VERSION_WITH_CONTEXT) {
        return undefined;
      }
      const additionalData = version === SEALED_VERSION_WITH_CONTEXT ? encoder.encode(context ?? "") : undefined;
      try {
        const plain = await aesGcmDecrypt(key, stored.iv, stored.data, additionalData);
        return JSON.parse(decoder.decode(plain)) as Value;
      } catch {
        return undefined;
      }
    },
    async seal(value: unknown, context?: string) {
      let plain: Uint8Array<ArrayBuffer>;
      if (holdsLongList(value, 0)) {
        plain = await encodedInSlices(value, encoder);
      } else {
        const serialized = JSON.stringify(value ?? null);
        if (serialized === undefined) {
          throw new TypeError("Local secret-box values must be JSON serializable.");
        }
        plain = encoder.encode(serialized);
      }
      const iv = randomBytes(12);
      const data = await aesGcmEncrypt(key, iv, plain, encoder.encode(context ?? ""));
      return {
        __sealed: 1,
        alg: SEALED_ALGORITHM,
        data,
        iv,
        v: SEALED_VERSION_WITH_CONTEXT,
      } satisfies SealedEnvelope;
    },
  };
  sessionMetadata.set(box, { key, rejectUnsealed });
  return box;
}

export function createLocalSecretBox(
  key: LocalDataKey,
  options: LocalSecretBoxOptions = {},
): LocalSecretBox {
  if (!isLocalSecretBoxKey(key)) {
    throw new TypeError("A non-extractable AES-256 encrypt/decrypt key is required.");
  }
  return createBox(key, options);
}

export function snapshotLocalSecretBoxSession(
  box: LocalSecretBox,
): LocalSecretBoxSessionSnapshot | null {
  const metadata = sessionMetadata.get(box);
  if (!metadata || !isLocalSecretBoxKey(metadata.key)) return null;
  return {
    key: metadata.key,
    rejectUnsealed: metadata.rejectUnsealed,
    version: 1,
  };
}

export function restoreLocalSecretBoxSession(
  snapshot: unknown,
): LocalSecretBox | null {
  if (!snapshot || typeof snapshot !== "object") return null;
  const candidate = snapshot as Partial<LocalSecretBoxSessionSnapshot>;
  if (
    candidate.version !== 1
    || typeof candidate.rejectUnsealed !== "boolean"
    || !isLocalSecretBoxKey(candidate.key)
  ) {
    return null;
  }
  return createBox(candidate.key, {
    rejectUnsealed: candidate.rejectUnsealed,
  });
}
