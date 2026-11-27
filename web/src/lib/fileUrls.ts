import { fileTransferBounds, type FileDownloadCapability } from "@/fileTransferWire.generated";
import { createContext, useContext, useEffect } from "react";
import type { Session } from "@/types";
import { loadSession } from "./authSessionStore";
import {
  createFileDownloadCapabilityResolver,
  type FileDownloadCapabilityResolver,
} from "./fileDownloadCapability";
import { SHOWN_PICTURE_WIDTH, cachedWorkspaceFileObjectUrl, evictCachedWorkspaceFiles, keepShownWorkspacePicture } from "./offlineFiles";
import { usePublicFileUrl } from "./publicFileUrls";
import { subscribeSessionGenerationReset } from "./sessionGeneration";
import { createWorkspaceFileDownloadUrl } from "./storage";
import { NOT_ANSWERED, answeredWithin, isServerUnreachable, retryWhenServerMayAnswer } from "./serverReach";
import { serverPath } from "./serverOrigin";
import { safeStoredFileUrl } from "./urls";
import { useStateUnlessSame } from "./useStateUnlessSame";
import { managedFileValueLocator, nativeWorkspaceFileIdFromLocator } from "./workspaceFileTransport";

const FILE_BUCKET = "files";
const FILE_CAPABILITY_REFRESH_WINDOW_MS = fileTransferBounds.capabilityRefreshSeconds * 1_000;
const MOUNTED_SIGNED_URL_REFRESH_CONCURRENCY = 32;
const SIGNED_URL_CACHE_MAX_ENTRIES = 256;

// How long a file's address waits for the server before the copy kept here, or none for now.
const FILE_URL_ANSWER_WAIT_MS = 10_000;
// How soon a file's address with none for now is asked again.
const FILE_URL_RETRY_MS = 5_000;
const CLEAR_SIGNED_FILE_URL_CACHE_EVENT = "leafen:clear-signed-file-url-cache";
export const FreshPublicFileCapabilityContext = createContext(false);

type SessionIdentity = Pick<Session, "token"> & { userId: string };

type SignedUrlCacheEntry = {
  fileId: string;
  lastUsedAt: number;
  resolver: FileDownloadCapabilityResolver;
};

type ResolvedWorkspaceFileUrl = {
  expiresAt?: number;
  url: string;
};

type WorkspaceFileUrlServiceOptions = {
  cachedFile?: typeof cachedWorkspaceFileObjectUrl;
  currentSession?: typeof loadSession;
  downloadUrl?: typeof createWorkspaceFileDownloadUrl;
  evictCached?: typeof evictCachedWorkspaceFiles;
  now?: () => number;
  online?: () => boolean;
  subscribeReset?: typeof subscribeSessionGenerationReset;
};

function fileAccessErrorStatus(error: unknown): number | null {
  if (!error || typeof error !== "object") return null;
  const record = error as {
    code?: unknown;
    response?: { status?: unknown };
    status?: unknown;
    statusCode?: unknown;
  };
  for (const value of [record.status, record.statusCode, record.response?.status, record.code]) {
    const parsed = typeof value === "number" ? value : Number(value);
    if (Number.isInteger(parsed) && parsed >= 0 && parsed <= 599) return parsed;
  }
  return null;
}

function workspaceFileCacheFallbackAllowed(error: unknown) {
  // A server that can't be reached keeps what was kept here (2026-11-11: its
  // transport error read as a refusal and let the kept pictures go).
  if (isServerUnreachable(error)) return true;
  const status = fileAccessErrorStatus(error);
  if (status !== null) {
    return status === 0 || status === 408 || status === 425 || status === 429 || status >= 500;
  }
  if (error instanceof TypeError) return true;
  const message = error instanceof Error ? error.message : String(error ?? "");
  return /(?:network|failed to fetch|fetch failed|timed?\s*out|temporar(?:y|ily)|connection)/iu.test(
    message,
  );
}

function currentIdentity(currentSession: () => Session | null): SessionIdentity | null {
  const session = currentSession();
  if (!session?.token || !session.user?.id) return null;
  return { token: session.token, userId: session.user.id };
}

function sameIdentity(left: SessionIdentity | null, right: SessionIdentity | null) {
  return left?.token === right?.token && left?.userId === right?.userId;
}

function signedUrlContextChanged() {
  return Object.assign(new Error("Signed file access context changed."), {
    code: "SIGNED_FILE_CONTEXT_CHANGED" as const,
  });
}

function releaseOfflineObjectUrl(url: string) {
  if (url.startsWith("blob:") && typeof URL.revokeObjectURL === "function") {
    URL.revokeObjectURL(url);
  }
}

export function createWorkspaceFileUrlService(options: WorkspaceFileUrlServiceOptions = {}) {
  const cachedFile = options.cachedFile ?? cachedWorkspaceFileObjectUrl;
  const currentSession = options.currentSession ?? loadSession;
  const downloadUrl = options.downloadUrl ?? createWorkspaceFileDownloadUrl;
  const evictCached = options.evictCached ?? evictCachedWorkspaceFiles;
  const now = options.now ?? Date.now;
  const online = options.online ?? (() => typeof navigator === "undefined" || navigator.onLine !== false);
  const cache = new Map<string, SignedUrlCacheEntry>();
  const pendingByFileId = new Map<string, Set<Promise<FileDownloadCapability>>>();
  let epoch = 0;

  function contextCurrent(capturedEpoch: number, identity: SessionIdentity | null) {
    return capturedEpoch === epoch && sameIdentity(identity, currentIdentity(currentSession));
  }

  function clear() {
    epoch += 1;
    for (const entry of cache.values()) entry.resolver.clear();
    cache.clear();
  }

  function trimCache() {
    while (cache.size > SIGNED_URL_CACHE_MAX_ENTRIES) {
      let oldestKey = "";
      let oldestUsedAt = Number.POSITIVE_INFINITY;
      for (const [key, entry] of cache) {
        if (entry.lastUsedAt < oldestUsedAt) {
          oldestKey = key;
          oldestUsedAt = entry.lastUsedAt;
        }
      }
      if (!oldestKey) return;
      cache.get(oldestKey)?.resolver.clear();
      cache.delete(oldestKey);
    }
  }

  function track(fileId: string, promise: Promise<FileDownloadCapability>) {
    const pending = pendingByFileId.get(fileId) ?? new Set<Promise<FileDownloadCapability>>();
    pending.add(promise);
    pendingByFileId.set(fileId, pending);
    const release = () => {
      pending.delete(promise);
      if (pending.size === 0 && pendingByFileId.get(fileId) === pending) {
        pendingByFileId.delete(fileId);
      }
    };
    void promise.then(release, release);
    return promise;
  }

  async function signed(fileId: string): Promise<FileDownloadCapability> {
    const identity = currentIdentity(currentSession);
    if (!identity) throw new Error("A logged-in session is required.");
    const capturedEpoch = epoch;
    const cacheKey = `${identity.userId}\0${fileId}`;
    let entry = cache.get(cacheKey);
    if (!entry) {
      entry = {
        fileId,
        lastUsedAt: now(),
        resolver: createFileDownloadCapabilityResolver(
          () => downloadUrl({ fileId }),
          { now },
        ),
      };
      cache.set(cacheKey, entry);
      trimCache();
    } else {
      entry.lastUsedAt = now();
    }
    const capturedEntry = entry;
    const request = capturedEntry.resolver.resolve().then((capability) => {
      if (!contextCurrent(capturedEpoch, identity)) {
        if (cache.get(cacheKey) === capturedEntry) cache.delete(cacheKey);
        capturedEntry.resolver.clear();
        throw signedUrlContextChanged();
      }
      return capability;
    });
    return track(fileId, request);
  }

  async function resolve(fileId: string): Promise<ResolvedWorkspaceFileUrl> {
    const identity = currentIdentity(currentSession);
    const capturedEpoch = epoch;
    if (!online()) {
      const cached = await cachedFile(fileId);
      if (cached) {
        if (contextCurrent(capturedEpoch, identity)) return { url: cached };
        releaseOfflineObjectUrl(cached);
        throw signedUrlContextChanged();
      }
    }
    try {
      const answer = await answeredWithin(signed(fileId), FILE_URL_ANSWER_WAIT_MS);
      // A server that doesn't answer is one that can't be reached: the copy
      // kept here, or none for now, and the place reads again (below).
      if (answer !== NOT_ANSWERED) return answer;
      const cached = await cachedFile(fileId);
      if (!cached) return { url: "" };
      if (contextCurrent(capturedEpoch, identity)) return { url: cached };
      releaseOfflineObjectUrl(cached);
      throw signedUrlContextChanged();
    } catch (error) {
      if (!workspaceFileCacheFallbackAllowed(error)) {
        await evictCached([fileId], identity?.userId ?? "");
        throw error;
      }
      const cached = await cachedFile(fileId);
      if (!cached) return { url: "" };
      if (contextCurrent(capturedEpoch, identity)) return { url: cached };
      releaseOfflineObjectUrl(cached);
      throw signedUrlContextChanged();
    }
  }

  async function settle(fileId: string) {
    const pending = pendingByFileId.get(fileId);
    if (pending?.size) await Promise.allSettled(Array.from(pending));
  }

  function evict(fileId: string) {
    for (const [cacheKey, entry] of cache) {
      if (entry.fileId !== fileId) continue;
      entry.resolver.clear();
      cache.delete(cacheKey);
    }
  }

  const unsubscribe = (options.subscribeReset ?? subscribeSessionGenerationReset)(clear);
  return { clear, dispose: unsubscribe, evict, resolve, settle, signed };
}

const workspaceFileUrlService = createWorkspaceFileUrlService();

function clearSignedWorkspaceFileUrlCache() {
  workspaceFileUrlService.clear();
}

export function storageKeyFromUrl(value: string | undefined, bucket = FILE_BUCKET) {
  return bucket === FILE_BUCKET ? nativeWorkspaceFileIdFromLocator(value) : "";
}

export async function resolveWorkspaceFileUrl(fileId: string) {
  return (await workspaceFileUrlService.resolve(fileId)).url;
}

if (typeof window !== "undefined") {
  window.addEventListener(CLEAR_SIGNED_FILE_URL_CACHE_EVENT, clearSignedWorkspaceFileUrlCache);
}
subscribeSessionGenerationReset(() => {
  if (typeof window !== "undefined") {
    window.dispatchEvent(new Event(CLEAR_SIGNED_FILE_URL_CACHE_EVENT));
  }
});

type MountedSignedUrlRefreshJob = {
  dueAt: number;
  run(lateByMs: number): Promise<void>;
};

const mountedRefreshJobs = new Map<number, MountedSignedUrlRefreshJob>();
let mountedRefreshJobId = 0;
let mountedRefreshTimer = 0;
let mountedRefreshDraining = false;
let mountedRefreshVisibilityListening = false;

function mountedRefreshHidden() {
  return typeof document !== "undefined" && document.visibilityState === "hidden";
}

function clearMountedRefreshTimer() {
  if (!mountedRefreshTimer || typeof window === "undefined") return;
  window.clearTimeout(mountedRefreshTimer);
  mountedRefreshTimer = 0;
}

function maybeReleaseVisibilityListener() {
  if (
    mountedRefreshJobs.size > 0
    || mountedRefreshDraining
    || !mountedRefreshVisibilityListening
    || typeof document === "undefined"
  ) return;
  document.removeEventListener("visibilitychange", handleVisibilityChange);
  mountedRefreshVisibilityListening = false;
}

function armMountedRefreshTimer() {
  clearMountedRefreshTimer();
  if (
    typeof window === "undefined"
    || mountedRefreshDraining
    || mountedRefreshHidden()
    || mountedRefreshJobs.size === 0
  ) {
    maybeReleaseVisibilityListener();
    return;
  }
  let nextDueAt = Number.POSITIVE_INFINITY;
  for (const job of mountedRefreshJobs.values()) nextDueAt = Math.min(nextDueAt, job.dueAt);
  mountedRefreshTimer = window.setTimeout(() => {
    mountedRefreshTimer = 0;
    void drainMountedRefreshJobs();
  }, Math.max(0, nextDueAt - Date.now()));
}

async function drainMountedRefreshJobs() {
  if (mountedRefreshDraining || mountedRefreshHidden()) {
    armMountedRefreshTimer();
    return;
  }
  mountedRefreshDraining = true;
  try {
    while (!mountedRefreshHidden()) {
      const now = Date.now();
      const dueJobs = Array.from(mountedRefreshJobs.entries())
        .filter(([, job]) => job.dueAt <= now)
        .sort(([leftId, left], [rightId, right]) => left.dueAt - right.dueAt || leftId - rightId)
        .slice(0, MOUNTED_SIGNED_URL_REFRESH_CONCURRENCY);
      if (!dueJobs.length) break;
      for (const [id] of dueJobs) mountedRefreshJobs.delete(id);
      await Promise.allSettled(dueJobs.map(([, job]) => job.run(Math.max(0, now - job.dueAt))));
    }
  } finally {
    mountedRefreshDraining = false;
    armMountedRefreshTimer();
  }
}

function handleVisibilityChange() {
  if (mountedRefreshHidden()) clearMountedRefreshTimer();
  else armMountedRefreshTimer();
}

function scheduleMountedRefresh(run: MountedSignedUrlRefreshJob["run"], dueAt: number) {
  const id = ++mountedRefreshJobId;
  let scheduled = true;
  mountedRefreshJobs.set(id, { dueAt, run });
  if (!mountedRefreshVisibilityListening && typeof document !== "undefined") {
    document.addEventListener("visibilitychange", handleVisibilityChange);
    mountedRefreshVisibilityListening = true;
  }
  armMountedRefreshTimer();
  return () => {
    if (!scheduled) return;
    scheduled = false;
    mountedRefreshJobs.delete(id);
    armMountedRefreshTimer();
  };
}

/** The widths the server makes a photo's preview at (product_file_preview.go). */
const FILE_PREVIEW_WIDTHS = [512, 1024, 2048] as const;
export type FilePreviewWidth = (typeof FILE_PREVIEW_WIDTHS)[number];

/**
 * The preview width for a place shown so many CSS pixels wide: twice as many
 * pixels, as Notion asks for on any screen (2026-10-17: 1640 for an 821px
 * window, 750 for a 375px phone).
 */
export function filePreviewWidth(cssPixels: number): FilePreviewWidth {
  return FILE_PREVIEW_WIDTHS.find((width) => width >= cssPixels * 2) ?? 2048;
}

/** A stored file's address asking for a preview width: only Leafen's own file routes take one. */
function withPreviewWidth(url: string, width: FilePreviewWidth | undefined) {
  if (!width || !url || !(url.includes("/file-capabilities/") || url.includes("/public-content/"))) return url;
  return `${url}${url.includes("?") ? "&" : "?"}w=${width}`;
}

type WorkspaceFileUrlOptions = { enabled?: boolean; trustedFreshCapability?: boolean; width?: FilePreviewWidth };

// The small pictures a page shows — icons, emoji, a file's thumbnail, asked
// at the smallest preview — are kept on this device as they are shown, once
// each, so a page kept here shows them without the server as it shows its
// text (2026-11-11: offline a table's picture icons were a plain file glyph).
// Larger pictures and files are kept with a page kept for offline use.
const shownPicturesKept = new Set<string>();
function keepShownPicture(fileId: string, url: string, width: FilePreviewWidth | undefined) {
  const userId = loadSession()?.user?.id;
  if (width !== SHOWN_PICTURE_WIDTH || !userId || !url || url.startsWith("blob:")) return;
  const key = `${userId}\0${fileId}`;
  if (shownPicturesKept.has(key)) return;
  shownPicturesKept.add(key);
  void keepShownWorkspacePicture(fileId, serverPath(withPreviewWidth(url, width)));
}

/**
 * A stored file's address to show. With a width, a photo comes at that size,
 * made by the server the first time (2026-10-17: phones received the full
 * several megabytes for a card); opening and downloading leave it out and
 * get the original.
 */
export function useWorkspaceFileUrl(
  value: string | undefined,
  dataPrefixes: string[] = [],
  options: WorkspaceFileUrlOptions = {},
) {
  return useWorkspaceFile(value, dataPrefixes, options).url;
}

/**
 * A stored file's address, and the server's error when it gave none — a
 * file gone (404) or damaged (409) — which a picture's place draws as a
 * picture that couldn't be loaded.
 */
export function useWorkspaceFile(
  value: string | undefined,
  dataPrefixes: string[] = [],
  options: WorkspaceFileUrlOptions = {},
) {
  const dataPrefixKey = dataPrefixes.join("\0");
  const enabled = options.enabled !== false;
  const width = options.width;
  const publicSnapshotCapability = useContext(FreshPublicFileCapabilityContext);
  const trustedFreshCapability = options.trustedFreshCapability === true || publicSnapshotCapability;
  // A published page reads its managed files from the public file route.
  const publicFileUrl = usePublicFileUrl();
  const safeUrl = safeStoredFileUrl(managedFileValueLocator(value), dataPrefixKey ? dataPrefixKey.split("\0") : []);
  // A place mounts with what it holds (no file, not a stored file, not read
  // yet): setting the same again queues nothing.
  const [resolvedUrl, setResolvedUrl] = useStateUnlessSame(() => {
    const fileId = storageKeyFromUrl(safeUrl);
    if (publicFileUrl && fileId) return publicFileUrl(fileId);
    return trustedFreshCapability || !fileId ? safeUrl : "";
  });
  const [failedStatus, setFailedStatus] = useStateUnlessSame(0);

  useEffect(() => {
    const prefixes = dataPrefixKey ? dataPrefixKey.split("\0") : [];
    const nextSafeUrl = safeStoredFileUrl(managedFileValueLocator(value), prefixes);
    const fileId = storageKeyFromUrl(nextSafeUrl);
    setFailedStatus(0);

    // An address known without the server reads nothing, not even the sign-in.
    if (publicFileUrl && fileId) {
      setResolvedUrl(publicFileUrl(fileId));
      return;
    }
    if (trustedFreshCapability || !fileId) {
      setResolvedUrl(nextSafeUrl);
      return;
    }
    if (!enabled) {
      setResolvedUrl("");
      return;
    }

    const requestIdentity = currentIdentity(loadSession);
    let cancelled = false;
    let offlineObjectUrl = "";
    let cancelScheduledRefresh = () => {};
    setResolvedUrl("");
    const invalidate = () => {
      cancelled = true;
      cancelScheduledRefresh();
      setResolvedUrl("");
    };
    window.addEventListener(CLEAR_SIGNED_FILE_URL_CACHE_EVENT, invalidate);
    const resolveAndSchedule = (lateByMs = 0): Promise<void> => {
      if (!cancelled && lateByMs >= FILE_CAPABILITY_REFRESH_WINDOW_MS) setResolvedUrl("");
      return workspaceFileUrlService.resolve(fileId).then((resolved) => {
        if (resolved.url.startsWith("blob:")) offlineObjectUrl = resolved.url;
        if (!cancelled && sameIdentity(requestIdentity, currentIdentity(loadSession))) {
          setResolvedUrl(resolved.url);
          keepShownPicture(fileId, resolved.url, width);
          // None while the server can't be reached: asked again when it may
          // answer, or in a few seconds — a network that stopped sends no
          // failure for "answers again" to follow.
          if (!resolved.url) {
            let timer = 0;
            const again = () => {
              stop();
              if (!cancelled) void resolveAndSchedule();
            };
            const stopAnswers = retryWhenServerMayAnswer(again);
            const stop = () => {
              stopAnswers();
              window.clearTimeout(timer);
            };
            timer = window.setTimeout(again, FILE_URL_RETRY_MS);
            cancelScheduledRefresh = stop;
          }
          if (resolved.expiresAt !== undefined) {
            cancelScheduledRefresh = scheduleMountedRefresh(
              resolveAndSchedule,
              Math.max(Date.now(), resolved.expiresAt - FILE_CAPABILITY_REFRESH_WINDOW_MS),
            );
          }
        } else if (offlineObjectUrl) {
          URL.revokeObjectURL(offlineObjectUrl);
        }
      }).catch((error: unknown) => {
        if (cancelled) return;
        setResolvedUrl("");
        const status = fileAccessErrorStatus(error) ?? 0;
        if (status >= 400) setFailedStatus(status);
      });
    };
    void resolveAndSchedule();

    return () => {
      cancelled = true;
      cancelScheduledRefresh();
      window.removeEventListener(CLEAR_SIGNED_FILE_URL_CACHE_EVENT, invalidate);
      if (offlineObjectUrl) URL.revokeObjectURL(offlineObjectUrl);
    };
  }, [value, dataPrefixKey, enabled, trustedFreshCapability, publicFileUrl, width]);

  return { url: withPreviewWidth(resolvedUrl, options.width), failedStatus };
}
