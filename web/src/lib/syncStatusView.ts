import {
  productErrorMessage,
  type ProductErrorContext,
} from "@/lib/productErrorPresentation";

export interface SyncStatusSettlement {
  outcome: "committed" | "discarded";
}

export interface SyncStatusService {
  /** Total platform snapshot; adapters return false when no browser exists. */
  initialOffline(): boolean;
  isVisible(): boolean;
  subscribeConnectivity(handlers: {
    onOnline(): void;
    onOffline(): void;
  }): () => void;
  subscribeVisibility(listener: () => void): () => void;
  /**
   * Returns the finite aggregate of the durable queue and every compatible
   * pending hint. Leaf consumers never enumerate raw outbox entries.
   */
  readPendingCount(userId: string): Promise<number>;
  subscribePending(listener: (
    changedUserId: string,
    pendingCount: number,
    settlement?: SyncStatusSettlement,
  ) => void): () => void;
  subscribeChanges(listener: () => void): () => void;
  flushAllPending(): Promise<void>;
  retrySyncHealth(): Promise<void>;
}

export interface SyncStatusBounds {
  maxPendingCount: number;
  maxUserIdCharacters: number;
}

function assertBounds(bounds: SyncStatusBounds) {
  for (const [name, value] of Object.entries(bounds)) {
    if (!Number.isSafeInteger(value) || value < 0) {
      throw new RangeError(`Sync status ${name} must be a non-negative safe integer.`);
    }
  }
}

export function admitSyncStatusUserId(
  userId: string,
  bounds: SyncStatusBounds,
) {
  assertBounds(bounds);
  if (typeof userId !== "string"
    || !userId
    || userId.length > bounds.maxUserIdCharacters) {
    throw new RangeError("Sync status user ID bound exceeded.");
  }
  return userId;
}

export function admitSyncPendingCount(
  count: number,
  bounds: SyncStatusBounds,
) {
  assertBounds(bounds);
  if (!Number.isSafeInteger(count)
    || count < 0
    || count > bounds.maxPendingCount) {
    throw new RangeError("Sync status pending count bound exceeded.");
  }
  return count;
}

export const SYNC_STATUS_FAILURE_STAGES = [
  "pending.read",
  "flush",
  "retry",
] as const;

export type SyncStatusFailureStage =
  typeof SYNC_STATUS_FAILURE_STAGES[number];

const syncStatusFailureContexts = {
  "pending.read": "syncStatus.pendingRead",
  flush: "syncStatus.flush",
  retry: "syncStatus.retry",
} as const satisfies Record<SyncStatusFailureStage, ProductErrorContext>;

export function syncStatusFailureMessage(
  error: unknown,
  stage: SyncStatusFailureStage,
) {
  return productErrorMessage(error, syncStatusFailureContexts[stage]);
}
