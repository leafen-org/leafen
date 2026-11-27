import { newId } from "./ids";
import { serverScopedName } from "./serverOrigin";

export const OUTBOX_CHANGE_SIGNAL_KEY = "leafen.outbox.changed";
export const OUTBOX_CHANGE_COLLECTION_MS = 25;

export interface OutboxCrossTabInvalidationBounds {
  maxListeners: number;
}

export interface OutboxCrossTabInvalidationHost {
  cancelTimer(handle: unknown): void;
  createNonce(): string;
  scheduleTimer(callback: () => void, delay: number): unknown;
  subscribeStorage(listener: (key: string | null) => void): () => void;
  writeStorage(key: string, value: string): void;
}

export interface OutboxCrossTabInvalidationOptions {
  host?: OutboxCrossTabInvalidationHost | null;
  onError?: (error: unknown) => void;
}

export interface OutboxCrossTabInvalidation {
  dispose(): void;
  schedule(): void;
  subscribe(listener: () => void): () => void;
}

function assertBounds(bounds: OutboxCrossTabInvalidationBounds): void {
  if (!Number.isSafeInteger(bounds.maxListeners) || bounds.maxListeners < 0) {
    throw new RangeError("maxListeners must be a non-negative safe integer.");
  }
}

function browserNonce(): string {
  try {
    return newId();
  } catch {
    // The bounded anonymous fallback below remains sufficient for invalidation.
  }
  let instant = 0;
  let random = 0;
  try {
    instant = Date.now();
    random = Math.floor(Math.random() * 0x1000000);
  } catch {
    // A deterministic signal sequence is still safe when entropy is absent.
  }
  return `${instant.toString(36)}-${random.toString(36)}`;
}

export function createBrowserOutboxCrossTabInvalidationHost():
  OutboxCrossTabInvalidationHost | null {
  if (typeof window === "undefined") return null;
  return {
    cancelTimer(handle) {
      window.clearTimeout(handle as number);
    },
    createNonce: browserNonce,
    scheduleTimer(callback, delay) {
      return window.setTimeout(callback, delay);
    },
    subscribeStorage(listener) {
      const handleStorage = (event: StorageEvent) => listener(event.key);
      window.addEventListener("storage", handleStorage);
      return () => window.removeEventListener("storage", handleStorage);
    },
    writeStorage(key, value) {
      window.localStorage.setItem(key, value);
    },
  };
}

function boundedNonce(value: string, sequence: number): string {
  return /^[A-Za-z0-9-]{1,64}$/u.test(value)
    ? value
    : `fallback-${sequence.toString(36)}`;
}

export function createOutboxCrossTabInvalidation(
  bounds: OutboxCrossTabInvalidationBounds,
  options: OutboxCrossTabInvalidationOptions = {},
): OutboxCrossTabInvalidation {
  assertBounds(bounds);
  const host = options.host === undefined
    ? createBrowserOutboxCrossTabInvalidationHost()
    : options.host;
  const listeners = new Set<() => void>();
  let disposed = false;
  let sequence = 0;
  let timerActive = false;
  let timerHandle: unknown;

  const observeError = (error: unknown) => {
    try {
      options.onError?.(error);
    } catch {
      // Observation cannot change durable behavior or channel availability.
    }
  };
  const emit = () => {
    for (const listener of [...listeners]) {
      try {
        listener();
      } catch (error) {
        observeError(error);
      }
    }
  };
  let stopStorage: (() => void) | undefined;
  if (host) {
    try {
      stopStorage = host.subscribeStorage((key) => {
        if (!disposed && key === serverScopedName(OUTBOX_CHANGE_SIGNAL_KEY)) emit();
      });
    } catch (error) {
      observeError(error);
    }
  }

  const flush = () => {
    timerActive = false;
    timerHandle = undefined;
    if (disposed) return;
    emit();
    if (!host) return;
    sequence = sequence === Number.MAX_SAFE_INTEGER ? 1 : sequence + 1;
    let nonce = `fallback-${sequence.toString(36)}`;
    try {
      nonce = boundedNonce(host.createNonce(), sequence);
    } catch (error) {
      observeError(error);
    }
    try {
      host.writeStorage(
        serverScopedName(OUTBOX_CHANGE_SIGNAL_KEY),
        `v1:${nonce}:${sequence}`,
      );
    } catch (error) {
      observeError(error);
    }
  };

  return {
    dispose() {
      if (disposed) return;
      disposed = true;
      listeners.clear();
      if (host && timerActive) {
        try {
          host.cancelTimer(timerHandle);
        } catch (error) {
          observeError(error);
        }
      }
      timerActive = false;
      timerHandle = undefined;
      try {
        stopStorage?.();
      } catch (error) {
        observeError(error);
      }
      stopStorage = undefined;
    },

    schedule() {
      if (disposed || timerActive) return;
      if (!host) {
        emit();
        return;
      }
      timerActive = true;
      try {
        timerHandle = host.scheduleTimer(flush, OUTBOX_CHANGE_COLLECTION_MS);
      } catch (error) {
        timerActive = false;
        timerHandle = undefined;
        observeError(error);
      }
    },

    subscribe(listener) {
      if (disposed) throw new Error("Outbox invalidation channel is disposed.");
      if (listeners.size >= bounds.maxListeners && !listeners.has(listener)) {
        throw new RangeError("Outbox invalidation listener bound exceeded.");
      }
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
  };
}
