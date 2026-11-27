import {
  normalizeOutboxOperation,
  type OutboxCausalityBounds,
} from "./outboxOperationCausality";
import type {
  OutboxOp,
  RejectedMutationRecoveryOutboxOp,
} from "./outboxOperationTypes";
import type {
  OutboxPendingSignals,
  OutboxSettlementOutcome,
} from "./outboxPendingSignals";
import type {
  OutboxStorageModel,
  OutboxStorageMutation,
  OutboxStorageMutationResult,
} from "./outboxStorageModel";
import {
  captureErrorFactoryCaller,
  logProductError,
} from "./safeErrorLog";

export interface OutboxDurabilityReceipt {
  entryKey: string;
  generation?: number;
  tabId: string;
  updatedAt: number;
  userId: string;
}

export interface OutboxSettlementCoordinatorBounds {
  maxMutationsPerBatch: number;
}

export interface OutboxSettlementInvalidation {
  schedule(): void;
}

export interface OutboxSettlementCoordinatorOptions {
  bounds: OutboxSettlementCoordinatorBounds;
  clock: () => number;
  invalidation: OutboxSettlementInvalidation;
  operationBounds: OutboxCausalityBounds;
  pending: OutboxPendingSignals;
  runExclusive: <T>(work: () => Promise<T>) => Promise<T>;
  storage: OutboxStorageModel;
  tabId: string;
  userId: string;
}

export interface OutboxRecoveryResult {
  applied: boolean;
  receipt?: OutboxDurabilityReceipt;
}

export interface OutboxSettlementCoordinator {
  advanceRecovery(
    receipt: OutboxDurabilityReceipt,
    recovery?: RejectedMutationRecoveryOutboxOp,
  ): Promise<OutboxRecoveryResult>;
  set(entryKey: string, operation: OutboxOp): Promise<OutboxDurabilityReceipt>;
  settle(
    receipt: OutboxDurabilityReceipt,
    outcome: OutboxSettlementOutcome,
  ): Promise<boolean>;
  settleAsRecovery(
    receipt: OutboxDurabilityReceipt,
    recovery: RejectedMutationRecoveryOutboxOp,
  ): Promise<OutboxRecoveryResult>;
}

export class OutboxDurabilityError extends Error {
  readonly reference: string;

  constructor(reference: string, cause: unknown) {
    super("Durable outbox operation failed.", { cause });
    this.name = "OutboxDurabilityError";
    this.reference = reference;
    captureErrorFactoryCaller(this, OutboxDurabilityError);
  }
}

interface QueuedMutation<T> {
  mutation: OutboxStorageMutation;
  onFailure?: () => void;
  onResult: (result: OutboxStorageMutationResult) => T;
  reject: (error: unknown) => void;
  resolve: (value: T | PromiseLike<T>) => void;
}

function assertPositiveSafeInteger(value: number, label: string): void {
  if (!Number.isSafeInteger(value) || value <= 0) {
    throw new RangeError(`${label} must be a positive safe integer.`);
  }
}

function durabilityError(cause: unknown): OutboxDurabilityError {
  const reference = logProductError("error", "outbox.persistence", cause, {
    causeStage: "outbox.batch",
  });
  return new OutboxDurabilityError(reference, cause);
}

export function createOutboxSettlementCoordinator(
  options: OutboxSettlementCoordinatorOptions,
): OutboxSettlementCoordinator {
  assertPositiveSafeInteger(options.bounds.maxMutationsPerBatch, "maxMutationsPerBatch");
  const queue: QueuedMutation<unknown>[] = [];
  let drainScheduled = false;
  let draining = false;
  let lastUpdatedAt = Number.NEGATIVE_INFINITY;

  const nextUpdatedAt = () => {
    const observed = options.clock();
    if (!Number.isFinite(observed)) throw new RangeError("Outbox clock must be finite.");
    const updatedAt = Math.max(observed, lastUpdatedAt + 1);
    if (!Number.isSafeInteger(updatedAt)) {
      throw new RangeError("Outbox durability timestamp space is exhausted.");
    }
    lastUpdatedAt = updatedAt;
    return updatedAt;
  };
  const assertReceipt = (receipt: OutboxDurabilityReceipt) => {
    if (
      receipt.userId !== options.userId
      || receipt.tabId !== options.tabId
      || !receipt.entryKey
      || !Number.isFinite(receipt.updatedAt)
    ) {
      throw new RangeError("Outbox durability receipt does not belong to this coordinator.");
    }
    if (
      receipt.generation !== undefined
      && (!Number.isSafeInteger(receipt.generation) || receipt.generation <= 0)
    ) {
      throw new RangeError("Outbox durability receipt generation is invalid.");
    }
  };

  const drain = async () => {
    if (draining) return;
    draining = true;
    drainScheduled = false;
    try {
      while (queue.length > 0) {
        const batch = queue.splice(0, options.bounds.maxMutationsPerBatch);
        let results: OutboxStorageMutationResult[];
        try {
          results = await options.runExclusive(() =>
            options.storage.applyBatch(batch.map(({ mutation }) => mutation))
          );
          if (results.length !== batch.length) {
            throw new Error("Outbox storage returned an incomplete mutation batch result.");
          }
        } catch (cause) {
          const error = durabilityError(cause);
          for (const item of batch) {
            try {
              item.onFailure?.();
            } finally {
              item.reject(error);
            }
          }
          continue;
        }
        let changed = false;
        for (let index = 0; index < batch.length; index += 1) {
          const result = results[index];
          changed ||= result.applied;
          batch[index].resolve(batch[index].onResult(result));
        }
        if (changed) {
          try {
            options.invalidation.schedule();
          } catch (cause) {
            logProductError("warn", "outbox.persistence", cause, {
              causeStage: "outbox.invalidation",
            });
          }
        }
      }
    } finally {
      draining = false;
      if (queue.length > 0) scheduleDrain();
    }
  };
  const scheduleDrain = () => {
    if (drainScheduled || draining) return;
    drainScheduled = true;
    queueMicrotask(() => void drain());
  };
  const enqueue = <T>(
    mutation: OutboxStorageMutation,
    onResult: (result: OutboxStorageMutationResult) => T,
    onFailure?: () => void,
  ) => new Promise<T>((resolve, reject) => {
    queue.push({ mutation, onFailure, onResult, reject, resolve } as QueuedMutation<unknown>);
    scheduleDrain();
  });
  const recoveryReceipt = (
    original: OutboxDurabilityReceipt,
    updatedAt: number,
  ): OutboxDurabilityReceipt => ({
    entryKey: original.entryKey,
    tabId: options.tabId,
    updatedAt,
    userId: options.userId,
  });

  return {
    advanceRecovery(receipt, recovery) {
      assertReceipt(receipt);
      const updatedAt = recovery ? nextUpdatedAt() : undefined;
      const normalized = recovery
        ? normalizeOutboxOperation(receipt.entryKey, recovery, options.operationBounds)
        : undefined;
      return enqueue(
        normalized
          ? {
            entryKey: receipt.entryKey,
            ifUpdatedAt: receipt.updatedAt,
            kind: "put",
            tabId: options.tabId,
            updatedAt: updatedAt!,
            value: normalized,
          }
          : {
            entryKey: receipt.entryKey,
            ifUpdatedAt: receipt.updatedAt,
            kind: "remove",
            tabId: options.tabId,
          },
        ({ applied }) => ({
          applied,
          ...(applied && updatedAt !== undefined
            ? { receipt: recoveryReceipt(receipt, updatedAt) }
            : {}),
        }),
      );
    },

    set(entryKey, operation) {
      const normalized = normalizeOutboxOperation(entryKey, operation, options.operationBounds);
      const updatedAt = nextUpdatedAt();
      const generation = options.pending.begin(options.userId, entryKey);
      const receipt = {
        entryKey,
        generation,
        tabId: options.tabId,
        updatedAt,
        userId: options.userId,
      };
      return enqueue(
        { entryKey, kind: "put", tabId: options.tabId, updatedAt, value: normalized },
        () => receipt,
        () => options.pending.cancel(options.userId, entryKey, generation),
      );
    },

    settle(receipt, outcome) {
      assertReceipt(receipt);
      return enqueue(
        {
          entryKey: receipt.entryKey,
          ifUpdatedAt: receipt.updatedAt,
          kind: "remove",
          tabId: options.tabId,
        },
        ({ applied }) => {
          if (applied) options.pending.settle({
            entryKey: receipt.entryKey,
            expectedGeneration: receipt.generation,
            outcome,
            userId: options.userId,
          });
          return applied;
        },
      );
    },

    settleAsRecovery(receipt, recovery) {
      assertReceipt(receipt);
      const updatedAt = nextUpdatedAt();
      const normalized = normalizeOutboxOperation(
        receipt.entryKey,
        recovery,
        options.operationBounds,
      );
      return enqueue(
        {
          entryKey: receipt.entryKey,
          ifUpdatedAt: receipt.updatedAt,
          kind: "put",
          tabId: options.tabId,
          updatedAt,
          value: normalized,
        },
        ({ applied }) => {
          if (applied) options.pending.settle({
            entryKey: receipt.entryKey,
            expectedGeneration: receipt.generation,
            outcome: "discarded",
            userId: options.userId,
          });
          return {
            applied,
            ...(applied ? { receipt: recoveryReceipt(receipt, updatedAt) } : {}),
          };
        },
      );
    },
  };
}
