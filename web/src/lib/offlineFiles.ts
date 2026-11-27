import type { Session } from "@/types";
import { loadSession } from "./authSessionStore";
import { localEncryptionMode, onLocalEncryptionModeChange } from "./localLock";
import { subscribeSessionGenerationReset } from "./sessionGeneration";
import { createWorkspaceFileDownloadUrl } from "./storage";
import { serverPath, serverScopedName } from "./serverOrigin";

const OFFLINE_FILE_CACHE = "leafen-offline-files-v1";
const OFFLINE_FILE_CACHE_PREFIX = "/__leafen_offline_file__/";
const MAX_OFFLINE_FILES = 500;
const MAX_OFFLINE_FILE_BYTES = 100 * 1024 * 1024;
const MAX_OFFLINE_CACHE_BYTES = 500 * 1024 * 1024;
const MAX_OFFLINE_FILE_KEY_CHARACTERS = 255;
const CLEAR_OFFLINE_FILE_CACHE_EVENT = "leafen:clear-offline-file-cache";
/**
 * The preview width of the small pictures a page shows — icons, emoji, a
 * file's thumbnail — which are kept here as they are shown (fileUrls.ts).
 */
export const SHOWN_PICTURE_WIDTH = 512;

// A shown picture is kept apart from its original, which a page kept for
// offline use keeps whole (2026-11-11: offline a table's picture icons
// showed a plain file glyph, as nothing of them was kept).
function shownKey(key: string) {
  return `${key}#w${SHOWN_PICTURE_WIDTH}`;
}

type OfflineFileCacheStorage = Pick<CacheStorage, "delete" | "open">;

type OfflineWorkspaceFilesOptions = {
  cacheStorage?: OfflineFileCacheStorage | null;
  createObjectUrl?: (blob: Blob) => string;
  currentSession?: () => Session | null;
  downloadUrl?: typeof createWorkspaceFileDownloadUrl;
  encryptionMode?: typeof localEncryptionMode;
  fetchResponse?: typeof globalThis.fetch | null;
};

function availableCacheStorage(): OfflineFileCacheStorage | null {
  return typeof caches === "undefined" ? null : caches;
}

function availableFetch(): typeof globalThis.fetch | null {
  return typeof fetch === "undefined" ? null : fetch;
}

function normalizedKey(value: unknown) {
  if (typeof value !== "string") throw new TypeError("Offline file key must be a string.");
  const key = value.trim();
  if (key.length > MAX_OFFLINE_FILE_KEY_CHARACTERS) {
    throw new RangeError("Offline file key exceeds its configured bound.");
  }
  return key;
}

// A page's files are kept or let go however many there are (2026-10-13:
// past 500 at once it threw, or a file wasn't kept while 500 were pending).
function boundedUniqueKeys(keys: Iterable<string>) {
  const unique: string[] = [];
  const seen = new Set<string>();
  for (const value of keys) {
    const key = normalizedKey(value);
    if (key && !seen.has(key)) {
      seen.add(key);
      unique.push(key);
    }
  }
  return unique;
}

export function createOfflineWorkspaceFiles(options: OfflineWorkspaceFilesOptions = {}) {
  const cacheStorage = options.cacheStorage === undefined
    ? availableCacheStorage()
    : options.cacheStorage;
  const fetchResponse = options.fetchResponse === undefined
    ? availableFetch()
    : options.fetchResponse;
  const currentSession = options.currentSession ?? loadSession;
  const encryptionMode = options.encryptionMode ?? localEncryptionMode;
  const downloadUrl = options.downloadUrl ?? createWorkspaceFileDownloadUrl;
  const createObjectUrl = options.createObjectUrl ?? ((blob: Blob) => (
    typeof URL.createObjectURL === "function" ? URL.createObjectURL(blob) : ""
  ));
  const pendingByOwnerKey = new Map<string, Promise<boolean>>();
  let cacheEpoch = 0;
  let mutationTail = Promise.resolve();

  function apiAvailable() {
    return cacheStorage !== null && fetchResponse !== null;
  }

  function allowed() {
    // Passphrase mode promises sealed durable local data. CacheStorage cannot
    // bind a streaming response to that non-extractable key without buffering
    // the whole file, so plaintext attachment persistence fails closed.
    return encryptionMode() !== "passphrase";
  }

  function ownerId() {
    return currentSession()?.user?.id.trim() ?? "";
  }

  function cacheUrl(key: string, origin?: string, owner = "") {
    const base = origin
      || (typeof window === "undefined" ? "https://leafen.invalid" : window.location.origin);
    const ownerPrefix = owner ? `${encodeURIComponent(serverScopedName(owner))}/` : "";
    return new URL(
      `${OFFLINE_FILE_CACHE_PREFIX}${ownerPrefix}${encodeURIComponent(key)}`,
      base,
    ).toString();
  }

  function contextCurrent(epoch: number, owner: string) {
    return epoch === cacheEpoch && !!owner && owner === ownerId() && allowed();
  }

  function enqueueMutation<T>(mutation: () => Promise<T>) {
    const result = mutationTail.then(mutation, mutation);
    mutationTail = result.then(() => undefined, () => undefined);
    return result;
  }

  function sizeAllowed(contentLength: string | null) {
    if (contentLength === null || !/^\d+$/u.test(contentLength)) return false;
    const bytes = Number(contentLength);
    return Number.isSafeInteger(bytes) && bytes >= 0 && bytes <= MAX_OFFLINE_FILE_BYTES;
  }

  async function trim(cacheStore: Cache) {
    const requests = await cacheStore.keys();
    if (requests.length > MAX_OFFLINE_FILES + 1) {
      await cacheStorage?.delete(OFFLINE_FILE_CACHE);
      return false;
    }
    const entries: Array<{ request: Request; bytes: number }> = [];
    let totalBytes = 0;
    for (const request of requests) {
      const response = await cacheStore.match(request);
      const contentLength = response?.headers.get("content-length") ?? null;
      if (!sizeAllowed(contentLength)) {
        await cacheStore.delete(request);
        continue;
      }
      const bytes = Number(contentLength);
      entries.push({ request, bytes });
      totalBytes += bytes;
    }
    while (entries.length > MAX_OFFLINE_FILES || totalBytes > MAX_OFFLINE_CACHE_BYTES) {
      const oldest = entries.shift();
      if (!oldest) break;
      await cacheStore.delete(oldest.request);
      totalBytes -= oldest.bytes;
    }
    return true;
  }

  function clear() {
    // Invalidate synchronously. Any admitted fetch must fail its next context
    // check before this serialized deletion can complete.
    cacheEpoch += 1;
    if (!cacheStorage) return Promise.resolve(false);
    return enqueueMutation(() => cacheStorage.delete(OFFLINE_FILE_CACHE));
  }

  async function evict(keys: Iterable<string>, explicitOwner = ownerId()) {
    const unique = boundedUniqueKeys(keys);
    const owner = normalizedKey(explicitOwner);
    if (!unique.length || !apiAvailable() || !owner || !cacheStorage) return;
    cacheEpoch += 1;
    await enqueueMutation(async () => {
      const cacheStore = await cacheStorage.open(OFFLINE_FILE_CACHE);
      for (const key of unique) {
        await cacheStore.delete(cacheUrl(key, undefined, owner));
        await cacheStore.delete(cacheUrl(shownKey(key), undefined, owner));
      }
    });
  }

  // A file kept from its signed address, or from the address it was shown at.
  async function cacheOne(key: string, owner: string, shownAt?: string) {
    if (!apiAvailable() || !allowed() || !cacheStorage || !fetchResponse) return false;
    const epoch = cacheEpoch;
    const address = shownAt ?? serverPath((await downloadUrl({ fileId: key })).url);
    if (!contextCurrent(epoch, owner)) return false;
    const response = await fetchResponse(address);
    if (!response.ok || !sizeAllowed(response.headers.get("content-length"))) return false;
    if (!contextCurrent(epoch, owner)) return false;
    return enqueueMutation(async () => {
      if (!contextCurrent(epoch, owner)) return false;
      const cacheStore = await cacheStorage.open(OFFLINE_FILE_CACHE);
      if (!contextCurrent(epoch, owner)) return false;
      const url = cacheUrl(shownAt ? shownKey(key) : key, undefined, owner);
      await cacheStore.put(url, response.clone());
      if (!contextCurrent(epoch, owner)) {
        await cacheStore.delete(url);
        return false;
      }
      if (!(await trim(cacheStore))) return false;
      if (!contextCurrent(epoch, owner)) {
        await cacheStore.delete(url);
        return false;
      }
      return true;
    });
  }

  function cache(keyValue: string, shownAt?: string) {
    const key = normalizedKey(keyValue);
    if (!key) return Promise.resolve(false);
    const owner = ownerId();
    if (!owner) return Promise.resolve(false);
    const pendingKey = `${owner}\u0000${shownAt ? shownKey(key) : key}`;
    const current = pendingByOwnerKey.get(pendingKey);
    if (current) return current;
    const pending = cacheOne(key, owner, shownAt).catch(() => false).finally(() => {
      if (pendingByOwnerKey.get(pendingKey) === pending) pendingByOwnerKey.delete(pendingKey);
    });
    pendingByOwnerKey.set(pendingKey, pending);
    return pending;
  }

  async function cacheMany(keys: Iterable<string>) {
    const unique = boundedUniqueKeys(keys);
    let complete = true;
    // Preserve retained sequential admission: file-heavy pinning must not burst
    // through auth/storage limits or monopolize the browser connection pool.
    // A file kept already is not fetched again: a page kept for offline use
    // is walked again each time a page comes from the server, and fetched
    // all its files, covers and pictures again each time.
    for (const key of unique) complete = (await has(key).catch(() => false) || await cache(key)) && complete;
    return complete;
  }

  async function has(keyValue: string, shown = false) {
    const key = shown ? shownKey(normalizedKey(keyValue)) : normalizedKey(keyValue);
    if (!key || !apiAvailable() || !allowed() || !cacheStorage) return false;
    const owner = ownerId();
    if (!owner) return false;
    const epoch = cacheEpoch;
    const cacheStore = await cacheStorage.open(OFFLINE_FILE_CACHE);
    const cached = await cacheStore.match(cacheUrl(key, undefined, owner));
    return !!cached && sizeAllowed(cached.headers.get("content-length"))
      && contextCurrent(epoch, owner);
  }

  async function hasMany(keys: Iterable<string>) {
    for (const key of boundedUniqueKeys(keys)) {
      if (!(await has(key))) return false;
    }
    return true;
  }

  // The original kept here, or else the picture as it was shown.
  async function objectUrl(keyValue: string) {
    const key = normalizedKey(keyValue);
    if (!key || !apiAvailable() || !allowed() || !cacheStorage) return "";
    const owner = ownerId();
    if (!owner) return "";
    const epoch = cacheEpoch;
    const cacheStore = await cacheStorage.open(OFFLINE_FILE_CACHE);
    const response = await cacheStore.match(cacheUrl(key, undefined, owner))
      ?? await cacheStore.match(cacheUrl(shownKey(key), undefined, owner));
    if (
      !response
      || !sizeAllowed(response.headers.get("content-length"))
      || !contextCurrent(epoch, owner)
    ) return "";
    const blob = await response.blob();
    if (
      blob.size !== Number(response.headers.get("content-length"))
      || blob.size > MAX_OFFLINE_FILE_BYTES
      || !contextCurrent(epoch, owner)
    ) return "";
    return createObjectUrl(blob);
  }

  /** Keeps a small picture as it was shown at `address`, once per file. */
  async function keepShown(keyValue: string, address: string) {
    if (!address || await has(keyValue, true).catch(() => true)) return;
    await cache(keyValue, address);
  }

  return { allowed, cache, cacheMany, cacheUrl, clear, evict, has, hasMany, keepShown, objectUrl, sizeAllowed };
}

const offlineWorkspaceFiles = createOfflineWorkspaceFiles();

if (typeof window !== "undefined") {
  window.addEventListener(CLEAR_OFFLINE_FILE_CACHE_EVENT, () => {
    void offlineWorkspaceFiles.clear().catch(() => {});
  });
}
onLocalEncryptionModeChange(() => {
  void offlineWorkspaceFiles.clear().catch(() => {});
});
subscribeSessionGenerationReset(() => {
  void offlineWorkspaceFiles.clear().catch(() => {});
});

export function clearOfflineWorkspaceFileCache() {
  return offlineWorkspaceFiles.clear();
}

export function evictCachedWorkspaceFiles(keys: Iterable<string>, ownerId?: string) {
  return offlineWorkspaceFiles.evict(keys, ownerId);
}

export function cacheWorkspaceFilesForOffline(keys: Iterable<string>) {
  return offlineWorkspaceFiles.cacheMany(keys);
}

export function hasCachedWorkspaceFiles(keys: Iterable<string>) {
  return offlineWorkspaceFiles.hasMany(keys);
}

export function cachedWorkspaceFileObjectUrl(key: string) {
  return offlineWorkspaceFiles.objectUrl(key);
}

/** Keeps a small picture a page showed, as shown at `address` (SHOWN_PICTURE_WIDTH). */
export function keepShownWorkspacePicture(key: string, address: string) {
  return offlineWorkspaceFiles.keepShown(key, address).catch(() => undefined);
}
