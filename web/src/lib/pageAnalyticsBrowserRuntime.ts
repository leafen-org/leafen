import {
  anonymousPageAnalyticsToken,
  createPageAnalyticsCollector,
  type PageAnalyticsTransportRequest,
} from "./pageAnalyticsCollector";
import { pageAnalyticsErrorCodes } from "../pageAnalyticsWire.generated";
import { logProductError } from "./safeErrorLog";
import { serverScopedName } from "./serverOrigin";

export interface PageAnalyticsBrowserHost {
  readonly localStorage: Storage;
  readonly sessionStorage: Storage;
  addEventListener(type: "online", listener: () => void): void;
  removeEventListener(type: "online", listener: () => void): void;
}

export interface PageAnalyticsBrowserRuntimeOptions {
  host?: PageAnalyticsBrowserHost | null;
  now?: () => number;
  randomId?: () => string;
  schedule?: (callback: () => void) => void;
  subscribeMemberGenerationReset(listener: () => void): () => void;
  /** Whether a refused send is to go again later: a page an operation holds (lib/pageBusy.ts). */
  sendsAgainLater?(error: unknown): boolean;
  transport(
    request: PageAnalyticsTransportRequest,
    options?: { signal?: AbortSignal },
  ): Promise<unknown>;
}

export interface PageAnalyticsBrowserRuntime {
  dispose(): void;
  flush(): Promise<void>;
  observeMember(input: {
    workspaceId: string;
    pageId: string;
    /** A database row page, read through the page that holds it. */
    rowId?: string;
    /** An inline database opened alone, read through the page that holds it. */
    containerId?: string;
    visitId: string;
  }): void;
  observePublic(input: {
    token: string;
    pageId: string;
    visitId: string;
  }): void;
  observePublicSite(input: {
    siteSlug: string;
    pageId: string;
    visitId: string;
  }): void;
}

class MemoryStorage implements Storage {
  private readonly entries = new Map<string, string>();

  get length(): number {
    return this.entries.size;
  }

  clear(): void {
    this.entries.clear();
  }

  getItem(key: string): string | null {
    return this.entries.get(String(key)) ?? null;
  }

  key(index: number): string | null {
    return [...this.entries.keys()][index] ?? null;
  }

  removeItem(key: string): void {
    this.entries.delete(String(key));
  }

  setItem(key: string, value: string): void {
    this.entries.set(String(key), String(value));
  }
}

function observeRuntimeError(error: unknown, stage: string): void {
  logProductError("warn", "page-analytics.request", error, {
    causeStage: stage,
  });
}

// A visit to a page an operation holds for a while — its databases being
// copied into it — is answered as busy (page_busy.go): the batch stays at the
// head of the queue and goes again after a wait, longer each time, until the
// operation is done, rather than waiting for the next visit to be sent.
const BUSY_RETRY_FIRST_MS = 2_000;
const BUSY_RETRY_LONGEST_MS = 30_000;

// A batch the server read and won't take — a malformed or reused batch, or
// one none of whose pages it can count any more, a link taken away
// (page_analytics_readable.go) — would be refused the same way each time it
// went again: it leaves the queue instead of holding every visit after it.
const REFUSED_FOR_GOOD: ReadonlySet<string> = new Set([
  pageAnalyticsErrorCodes.invalid,
  pageAnalyticsErrorCodes.batchLimitExceeded,
  pageAnalyticsErrorCodes.batchIdentityReused,
  pageAnalyticsErrorCodes.notFound,
]);

function refusedForGood(error: unknown): boolean {
  const code = (error as { code?: unknown } | null)?.code;
  return typeof code === "string" && REFUSED_FOR_GOOD.has(code);
}

function defaultHost(): PageAnalyticsBrowserHost | null {
  if (typeof window === "undefined") return null;
  return window;
}

function resolveStorage(
  host: PageAnalyticsBrowserHost,
  kind: "local" | "session",
): Storage | null {
  try {
    return kind === "local" ? host.localStorage : host.sessionStorage;
  } catch (error) {
    observeRuntimeError(error, `analytics.storage.${kind}`);
    return null;
  }
}

// The queue of one server's page visits, kept apart from another server's
// in a packaged client, whose one origin works with several.
function serverScopedStorage(storage: Storage): Storage {
  if (serverScopedName("") === "") return storage;
  return {
    get length() { return storage.length; },
    clear: () => storage.clear(),
    getItem: (key) => storage.getItem(serverScopedName(key)),
    key: (index) => storage.key(index),
    removeItem: (key) => storage.removeItem(serverScopedName(key)),
    setItem: (key, value) => storage.setItem(serverScopedName(key), value),
  };
}

export function createPageAnalyticsBrowserRuntime(
  options: PageAnalyticsBrowserRuntimeOptions,
): PageAnalyticsBrowserRuntime | null {
  const host = options.host === undefined ? defaultHost() : options.host;
  if (!host) return null;

  const sessionStorage = resolveStorage(host, "session");
  if (!sessionStorage) return null;
  const localStorage = resolveStorage(host, "local");
  const memoryAnonymousStorage = new MemoryStorage();
  let anonymousStorage = localStorage ?? memoryAnonymousStorage;
  let disposed = false;

  let collector: ReturnType<typeof createPageAnalyticsCollector>;
  let busyWait = 0;
  let busyTimer: ReturnType<typeof setTimeout> | null = null;
  const undelivered = (error: unknown, stage: string) => {
    if (!options.sendsAgainLater?.(error)) {
      observeRuntimeError(error, stage);
      return;
    }
    if (disposed || busyTimer !== null) return;
    busyWait = Math.min(busyWait ? busyWait * 2 : BUSY_RETRY_FIRST_MS, BUSY_RETRY_LONGEST_MS);
    busyTimer = setTimeout(() => {
      busyTimer = null;
      if (disposed) return;
      void collector.flush().then(() => { busyWait = 0; }, (next: unknown) => undelivered(next, "analytics.delivery.busy"));
    }, busyWait);
  };
  try {
    collector = createPageAnalyticsCollector({
      storage: serverScopedStorage(sessionStorage),
      transport: options.transport,
      refusedForGood,
      ...(options.randomId ? { randomId: options.randomId } : {}),
      ...(options.schedule ? { schedule: options.schedule } : {}),
      onBackgroundError: (error) => {
        undelivered(error, "analytics.delivery.scheduled");
      },
    });
  } catch (error) {
    observeRuntimeError(error, "analytics.storage.session");
    return null;
  }

  const assertActive = () => {
    if (disposed) throw new Error("Page analytics browser runtime is disposed.");
  };
  const observe = (work: () => void) => {
    assertActive();
    try {
      work();
    } catch (error) {
      observeRuntimeError(error, "analytics.storage.session");
    }
  };
  const anonymousToken = () => {
    try {
      return anonymousPageAnalyticsToken(
        anonymousStorage,
        options.now?.() ?? Date.now(),
        options.randomId,
      );
    } catch (error) {
      observeRuntimeError(error, "analytics.identity.storage");
      anonymousStorage = memoryAnonymousStorage;
      return anonymousPageAnalyticsToken(
        anonymousStorage,
        options.now?.() ?? Date.now(),
        options.randomId,
      );
    }
  };
  const onOnline = () => {
    if (disposed) return;
    void collector.flush().catch((error) => {
      undelivered(error, "analytics.delivery.online");
    });
  };

  let unsubscribeMemberGenerationReset: () => void;
  try {
    unsubscribeMemberGenerationReset = options.subscribeMemberGenerationReset(
      () => {
        if (!disposed) collector.resetMemberGeneration();
      },
    );
  } catch (error) {
    observeRuntimeError(error, "analytics.lifecycle.member-reset");
    return null;
  }
  try {
    host.addEventListener("online", onOnline);
  } catch (error) {
    try {
      unsubscribeMemberGenerationReset();
    } catch (unsubscribeError) {
      observeRuntimeError(
        unsubscribeError,
        "analytics.lifecycle.member-reset-dispose",
      );
    }
    observeRuntimeError(error, "analytics.lifecycle.online-listener");
    return null;
  }

  return {
    dispose() {
      if (disposed) return;
      disposed = true;
      if (busyTimer !== null) clearTimeout(busyTimer);
      try {
        host.removeEventListener("online", onOnline);
      } catch (error) {
        observeRuntimeError(error, "analytics.lifecycle.online-dispose");
      }
      try {
        unsubscribeMemberGenerationReset();
      } catch (error) {
        observeRuntimeError(error, "analytics.lifecycle.member-reset-dispose");
      }
    },

    flush() {
      assertActive();
      return collector.flush();
    },

    observeMember(input) {
      observe(() => collector.observeMember(input));
    },

    observePublic(input) {
      observe(() => collector.observePublic({
        ...input,
        anonymousToken: anonymousToken(),
      }));
    },

    observePublicSite(input) {
      observe(() => collector.observePublicSite({
        ...input,
        anonymousToken: anonymousToken(),
      }));
    },
  };
}
