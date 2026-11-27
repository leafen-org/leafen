import {
  pageAnalyticsBounds,
  type PageAnalyticsTransportRequest,
} from "../pageAnalyticsWire.generated";
import {
  createPageAnalyticsQueue,
  type PageAnalyticsQueueLane,
  type PageAnalyticsQueueObservation,
} from "./pageAnalyticsQueue";
import { randomUUID } from "./browserCrypto";

export type { PageAnalyticsTransportRequest } from "../pageAnalyticsWire.generated";

export const PAGE_ANALYTICS_BATCH_LIMIT = pageAnalyticsBounds.batchObservations;
export const PAGE_ANALYTICS_ANONYMOUS_TTL_MS = pageAnalyticsBounds.anonymousTokenTtlMs;

const ANONYMOUS_TOKEN_KEY = "leafen.page-analytics.anonymous";

type CollectorOptions = {
  storage: Storage;
  onBackgroundError(error: unknown): void;
  /**
   * Whether the server refused a batch for good: it leaves the queue, the
   * refusal reported, rather than going again ahead of every visit after it.
   */
  refusedForGood?(error: unknown): boolean;
  transport(
    request: PageAnalyticsTransportRequest,
    options?: { signal?: AbortSignal },
  ): Promise<unknown>;
  now?: () => number;
  randomId?: () => string;
  schedule?: (callback: () => void) => void;
};

function memberLaneId(workspaceId: string) {
  return JSON.stringify(["member", workspaceId]);
}

function publicLaneId(token: string, anonymousToken: string) {
  return JSON.stringify(["public", token, anonymousToken]);
}

function publicSiteLaneId(siteSlug: string, anonymousToken: string) {
  return JSON.stringify(["publicSite", siteSlug, anonymousToken]);
}

export function createPageAnalyticsCollector(options: CollectorOptions) {
  const queue = createPageAnalyticsQueue(options.storage);
  const randomId = options.randomId ?? randomUUID;
  const schedule = options.schedule ?? ((callback: () => void) => queueMicrotask(callback));
  let scheduled = false;
  let flushFlight: Promise<void> | null = null;
  let memberGeneration = queue.memberGeneration();
  let memberController = new AbortController();

  const scheduleFlush = () => {
    if (scheduled) return;
    scheduled = true;
    schedule(() => {
      scheduled = false;
      void flush().catch((error) => options.onBackgroundError(error));
    });
  };

  const observe = (
    laneId: string,
    lane: PageAnalyticsQueueLane,
    observation: PageAnalyticsQueueObservation,
  ) => {
    const observationGeneration = lane.kind === "member" ? memberGeneration : 0;
    if (queue.append(laneId, lane, observation, observationGeneration)) scheduleFlush();
  };

  const requestFor = (
    lane: PageAnalyticsQueueLane,
    pending: PageAnalyticsQueueObservation[],
  ): PageAnalyticsTransportRequest => {
    const observations = pending.map(({ pageId, rowId, containerId }) => (
      rowId ? { pageId, rowId } : containerId ? { pageId, containerId } : { pageId }));
    const batchId = randomId();
    if (lane.kind === "member") {
      return {
        action: "record",
        workspaceId: lane.workspaceId,
        batchId,
        observations,
      };
    }
    if (lane.kind === "public") {
      return {
        action: "recordPublic",
        token: lane.token,
        anonymousToken: lane.anonymousToken,
        batchId,
        observations,
      };
    }
    return {
      action: "recordPublicSite",
      siteSlug: lane.siteSlug,
      anonymousToken: lane.anonymousToken,
      batchId,
      observations,
    };
  };

  const drain = async () => {
    while (true) {
      const current = queue.claim(PAGE_ANALYTICS_BATCH_LIMIT, requestFor);
      if (!current) return;
      if (current.kind === "discard") {
        queue.acknowledge(current);
        continue;
      }
      if (
        current.lane.kind === "member"
        && current.memberGeneration !== queue.memberGeneration()
      ) {
        queue.acknowledge(current);
        continue;
      }
      const request = current.request;
      const requestMemberGeneration = current.memberGeneration;
      const memberSignal = current.lane.kind === "member"
        ? memberController.signal
        : undefined;
      try {
        await options.transport(
          request,
          memberSignal ? { signal: memberSignal } : undefined,
        );
      } catch (error) {
        if (
          current.lane.kind === "member"
          && (
            requestMemberGeneration !== queue.memberGeneration()
            || memberSignal?.aborted
          )
        ) {
          continue;
        }
        if (options.refusedForGood?.(error)) {
          queue.acknowledge(current);
          options.onBackgroundError(error);
          continue;
        }
        throw error;
      }
      if (
        current.lane.kind === "member"
        && requestMemberGeneration !== queue.memberGeneration()
      ) {
        continue;
      }
      queue.acknowledge(current);
    }
  };

  function flush() {
    if (flushFlight) return flushFlight;
    flushFlight = drain().finally(() => {
      flushFlight = null;
    });
    return flushFlight;
  }

  function resetMemberGeneration() {
    memberGeneration = queue.resetMemberGeneration(memberGeneration);
    memberController.abort();
    memberController = new AbortController();
    scheduleFlush();
  }

  return {
    observeMember(input: { workspaceId: string; pageId: string; rowId?: string; containerId?: string; visitId: string }) {
      observe(memberLaneId(input.workspaceId), {
        kind: "member",
        workspaceId: input.workspaceId,
      }, {
        pageId: input.pageId,
        ...(input.rowId ? { rowId: input.rowId } : {}),
        ...(input.containerId ? { containerId: input.containerId } : {}),
        visitId: input.visitId,
      });
    },
    observePublic(input: {
      token: string;
      anonymousToken: string;
      pageId: string;
      visitId: string;
    }) {
      observe(publicLaneId(input.token, input.anonymousToken), {
        kind: "public",
        token: input.token,
        anonymousToken: input.anonymousToken,
      }, { pageId: input.pageId, visitId: input.visitId });
    },
    observePublicSite(input: {
      siteSlug: string;
      anonymousToken: string;
      pageId: string;
      visitId: string;
    }) {
      observe(publicSiteLaneId(input.siteSlug, input.anonymousToken), {
        kind: "publicSite",
        siteSlug: input.siteSlug,
        anonymousToken: input.anonymousToken,
      }, { pageId: input.pageId, visitId: input.visitId });
    },
    flush,
    resetMemberGeneration,
  };
}

export function anonymousPageAnalyticsToken(
  storage: Storage,
  now = Date.now(),
  randomId: () => string = randomUUID,
) {
  try {
    const raw = storage.getItem(ANONYMOUS_TOKEN_KEY);
    if (raw) {
      const parsed = JSON.parse(raw) as { token?: unknown; issuedAt?: unknown };
      if (
        typeof parsed.token === "string"
        && typeof parsed.issuedAt === "number"
        && now >= parsed.issuedAt
        && now - parsed.issuedAt < PAGE_ANALYTICS_ANONYMOUS_TTL_MS
      ) return parsed.token;
    }
  } catch {
    // Replace malformed or inaccessible state with a fresh pseudonym below.
  }
  const token = randomId();
  storage.setItem(ANONYMOUS_TOKEN_KEY, JSON.stringify({ token, issuedAt: now }));
  return token;
}
