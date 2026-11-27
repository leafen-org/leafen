import type {
  SyncStatusBounds,
  SyncStatusService,
  SyncStatusSettlement,
} from "./syncStatusView";

export interface NativeSyncStatusBounds extends SyncStatusBounds {
  maxSources: number;
}

export interface NativeSyncStatusSource {
  flushAllPending(): Promise<void>;
  readPendingCount(): Promise<number>;
  retrySyncHealth(): Promise<void>;
  subscribeChanges(listener: () => void): () => void;
  subscribePending(listener: (
    pendingCount: number,
    settlement?: SyncStatusSettlement,
  ) => void): () => void;
}

export interface NativeSyncStatusHost {
  initialOffline(): boolean;
  isVisible(): boolean;
  subscribeConnectivity(handlers: {
    onOnline(): void;
    onOffline(): void;
  }): () => void;
  subscribeVisibility(listener: () => void): () => void;
}

/**
 * Whether the server answers the app, and when that changes: offline is the
 * server not answering, not the browser's word on its network — a server on
 * this computer answers a browser that says it has none (lib/serverAnswers.ts).
 */
export interface NativeSyncStatusConnectivity {
  away(): boolean;
  subscribe(handlers: { onOnline(): void; onOffline(): void }): () => void;
}

export const NATIVE_SYNC_STATUS_BOUNDS: NativeSyncStatusBounds = {
  maxPendingCount: Number.MAX_SAFE_INTEGER,
  maxSources: 1,
  maxUserIdCharacters: 64,
};

function assertPositiveSafeInteger(value: number, label: string): void {
  if (!Number.isSafeInteger(value) || value <= 0) {
    throw new RangeError(`${label} must be a positive safe integer.`);
  }
}

function assertUserId(userId: string, bounds: NativeSyncStatusBounds): void {
  if (!userId || userId.length > bounds.maxUserIdCharacters) {
    throw new RangeError("Sync-status user ID exceeds its configured bound.");
  }
}

function assertPendingCount(count: number, bounds: NativeSyncStatusBounds): number {
  if (!Number.isSafeInteger(count) || count < 0 || count > bounds.maxPendingCount) {
    throw new RangeError("Sync-status aggregate exceeds its pending-count bound.");
  }
  return count;
}

function browserHost(connectivity: NativeSyncStatusConnectivity | undefined): NativeSyncStatusHost {
  return {
    initialOffline: () => connectivity?.away() ?? false,
    isVisible: () => (
      typeof document === "undefined" || document.visibilityState !== "hidden"
    ),
    subscribeConnectivity(handlers) {
      return connectivity?.subscribe(handlers) ?? (() => {});
    },
    subscribeVisibility(listener) {
      if (typeof document === "undefined") return () => {};
      document.addEventListener("visibilitychange", listener);
      return () => document.removeEventListener("visibilitychange", listener);
    },
  };
}

function combine(unsubscribes: Array<() => void>): () => void {
  let active = true;
  return () => {
    if (!active) return;
    active = false;
    for (const unsubscribe of unsubscribes) unsubscribe();
  };
}

export function createNativeSyncStatusService(options: {
  bounds: NativeSyncStatusBounds;
  /** Where offline is read from, without a host of its own. */
  connectivity?: NativeSyncStatusConnectivity;
  host?: NativeSyncStatusHost;
  sources: readonly NativeSyncStatusSource[];
  userId: string;
}): SyncStatusService {
  assertPositiveSafeInteger(options.bounds.maxPendingCount, "maxPendingCount");
  assertPositiveSafeInteger(options.bounds.maxSources, "maxSources");
  assertPositiveSafeInteger(options.bounds.maxUserIdCharacters, "maxUserIdCharacters");
  assertUserId(options.userId, options.bounds);
  if (
    options.sources.length === 0
    || options.sources.length > options.bounds.maxSources
  ) {
    throw new RangeError("Sync-status source count exceeds its configured bound.");
  }
  const host = options.host ?? browserHost(options.connectivity);
  const [source] = options.sources;

  return {
    flushAllPending() {
      return source.flushAllPending();
    },
    initialOffline() {
      return host.initialOffline();
    },
    isVisible() {
      return host.isVisible();
    },
    async readPendingCount(userId) {
      if (userId !== options.userId) {
        throw new Error("Sync-status read does not belong to the current principal.");
      }
      return assertPendingCount(await source.readPendingCount(), options.bounds);
    },
    retrySyncHealth() {
      return source.retrySyncHealth();
    },
    subscribeChanges(listener) {
      return combine(options.sources.map((item) => item.subscribeChanges(listener)));
    },
    subscribeConnectivity(handlers) {
      return host.subscribeConnectivity(handlers);
    },
    subscribePending(listener) {
      return combine(options.sources.map((item) => item.subscribePending((
        pendingCount,
        settlement,
      ) => listener(
        options.userId,
        assertPendingCount(pendingCount, options.bounds),
        settlement,
      ))));
    },
    subscribeVisibility(listener) {
      return host.subscribeVisibility(listener);
    },
  };
}
