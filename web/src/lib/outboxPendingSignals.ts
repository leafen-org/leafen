export type OutboxSettlementOutcome = "committed" | "discarded";

export interface OutboxPendingSettlement {
  entryKey: string;
  outcome: OutboxSettlementOutcome;
}

export interface OutboxPendingSignalEvent {
  pendingCount: number;
  settlement?: OutboxPendingSettlement;
  userId: string;
}

export interface OutboxPendingSignalBounds {
  maxActiveUsers: number;
  maxEntryKeyCharacters: number;
  maxListeners: number;
  maxUserIdCharacters: number;
}

export interface OutboxPendingSignals {
  begin(userId: string, entryKey: string): number;
  cancel(userId: string, entryKey: string, expectedGeneration: number): boolean;
  clearUser(userId: string): void;
  generation(userId: string, entryKey: string): number | undefined;
  pendingCount(userId: string): number;
  settle(input: {
    entryKey: string;
    expectedGeneration: number | undefined;
    outcome: OutboxSettlementOutcome;
    userId: string;
  }): boolean;
  subscribe(listener: (event: OutboxPendingSignalEvent) => void): () => void;
}

export interface OutboxPendingSignalOptions {
  onListenerError?: (error: unknown) => void;
}

function assertNonNegativeSafeInteger(value: number, label: string): void {
  if (!Number.isSafeInteger(value) || value < 0) {
    throw new RangeError(`${label} must be a non-negative safe integer.`);
  }
}

function validateBounds(bounds: OutboxPendingSignalBounds): void {
  assertNonNegativeSafeInteger(bounds.maxActiveUsers, "maxActiveUsers");
  assertNonNegativeSafeInteger(bounds.maxEntryKeyCharacters, "maxEntryKeyCharacters");
  assertNonNegativeSafeInteger(bounds.maxListeners, "maxListeners");
  assertNonNegativeSafeInteger(bounds.maxUserIdCharacters, "maxUserIdCharacters");
}

function assertBoundedIdentity(value: string, maximum: number, label: string): void {
  if (typeof value !== "string" || value.length === 0 || value.length > maximum) {
    throw new RangeError(`${label} exceeds its configured character bound.`);
  }
}

function assertOutcome(value: OutboxSettlementOutcome): void {
  if (value !== "committed" && value !== "discarded") {
    throw new RangeError("Outbox settlement outcome is invalid.");
  }
}

export function createOutboxPendingSignals(
  bounds: OutboxPendingSignalBounds,
  options: OutboxPendingSignalOptions = {},
): OutboxPendingSignals {
  validateBounds(bounds);
  const listeners = new Set<(event: OutboxPendingSignalEvent) => void>();
  const pendingByUser = new Map<string, Map<string, number>>();
  let nextGeneration = 0;

  const assertUserId = (userId: string) =>
    assertBoundedIdentity(userId, bounds.maxUserIdCharacters, "userId");
  const assertEntryKey = (entryKey: string) =>
    assertBoundedIdentity(entryKey, bounds.maxEntryKeyCharacters, "entryKey");
  const observeListenerError = (error: unknown) => {
    try {
      options.onListenerError?.(error);
    } catch {
      // Observation must never change pending or durable settlement meaning.
    }
  };
  const emit = (userId: string, settlement?: OutboxPendingSettlement) => {
    const event = Object.freeze({
      pendingCount: pendingByUser.get(userId)?.size ?? 0,
      ...(settlement ? { settlement: Object.freeze({ ...settlement }) } : {}),
      userId,
    });
    for (const listener of [...listeners]) {
      try {
        listener(event);
      } catch (error) {
        observeListenerError(error);
      }
    }
  };

  return {
    begin(userId, entryKey) {
      assertUserId(userId);
      assertEntryKey(entryKey);
      if (nextGeneration === Number.MAX_SAFE_INTEGER) {
        throw new RangeError("Outbox pending signal generation space is exhausted.");
      }
      let pending = pendingByUser.get(userId);
      if (!pending) {
        if (pendingByUser.size >= bounds.maxActiveUsers) {
          throw new RangeError("Outbox pending signal active-user bound exceeded.");
        }
        pending = new Map();
        pendingByUser.set(userId, pending);
      }
      nextGeneration += 1;
      pending.set(entryKey, nextGeneration);
      emit(userId);
      return nextGeneration;
    },

    cancel(userId, entryKey, expectedGeneration) {
      assertUserId(userId);
      assertEntryKey(entryKey);
      if (!Number.isSafeInteger(expectedGeneration) || expectedGeneration <= 0) {
        throw new RangeError("Outbox pending cancellation generation is invalid.");
      }
      const pending = pendingByUser.get(userId);
      if (pending?.get(entryKey) !== expectedGeneration) return false;
      pending.delete(entryKey);
      if (pending.size === 0) pendingByUser.delete(userId);
      emit(userId);
      return true;
    },

    clearUser(userId) {
      assertUserId(userId);
      pendingByUser.delete(userId);
      emit(userId);
    },

    generation(userId, entryKey) {
      assertUserId(userId);
      assertEntryKey(entryKey);
      return pendingByUser.get(userId)?.get(entryKey);
    },

    pendingCount(userId) {
      assertUserId(userId);
      return pendingByUser.get(userId)?.size ?? 0;
    },

    settle({ entryKey, expectedGeneration, outcome, userId }) {
      assertUserId(userId);
      assertEntryKey(entryKey);
      assertOutcome(outcome);
      if (
        expectedGeneration !== undefined
        && (!Number.isSafeInteger(expectedGeneration) || expectedGeneration <= 0)
      ) {
        throw new RangeError("Outbox settlement generation is invalid.");
      }
      const pending = pendingByUser.get(userId);
      const currentGeneration = pending?.get(entryKey);
      const ownsGeneration = expectedGeneration === undefined
        ? currentGeneration === undefined
        : currentGeneration === expectedGeneration;
      if (ownsGeneration && currentGeneration !== undefined) pending?.delete(entryKey);
      if (pending?.size === 0) pendingByUser.delete(userId);
      emit(
        userId,
        ownsGeneration ? { entryKey, outcome } : undefined,
      );
      return ownsGeneration;
    },

    subscribe(listener) {
      if (listeners.size >= bounds.maxListeners) {
        throw new RangeError("Outbox pending signal listener bound exceeded.");
      }
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
  };
}
