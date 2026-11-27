import type { PageAnalyticsTransportRequest } from "../pageAnalyticsWire.generated";

const ROOT_KEY = "leafen.page-analytics.queue:v1:root";
const ENTRY_PREFIX = "leafen.page-analytics.queue:v1:entry:";
const SCHEMA_VERSION = 1;

export type PageAnalyticsQueueLane =
  | { kind: "member"; workspaceId: string }
  | { kind: "public"; token: string; anonymousToken: string }
  | { kind: "publicSite"; siteSlug: string; anonymousToken: string };

export type PageAnalyticsQueueObservation = {
  pageId: string;
  /** A database row page's view, recorded through its page. */
  rowId?: string;
  /** An inline database's view opened alone, recorded through its page. */
  containerId?: string;
  visitId: string;
};

type QueueEntry = {
  schemaVersion: 1;
  key: string;
  laneId: string;
  lane: PageAnalyticsQueueLane;
  observation: PageAnalyticsQueueObservation;
  memberGeneration: number;
  nextKey: string | null;
};

type DeliveryClaim = {
  kind: "delivery";
  entryKeys: string[];
  nextKey: string | null;
  lane: PageAnalyticsQueueLane;
  memberGeneration: number;
  request: PageAnalyticsTransportRequest;
};

type DiscardClaim = {
  kind: "discard";
  entryKeys: string[];
  nextKey: string | null;
  lane: PageAnalyticsQueueLane;
  memberGeneration: number;
};

export type PageAnalyticsQueueClaim = DeliveryClaim | DiscardClaim;

type QueueRoot = {
  schemaVersion: 1;
  headKey: string | null;
  tailKey: string | null;
  memberGeneration: number;
  append?: QueueEntry;
  claim?: PageAnalyticsQueueClaim;
};

type RequestFactory = (
  lane: PageAnalyticsQueueLane,
  observations: PageAnalyticsQueueObservation[],
) => PageAnalyticsTransportRequest;

const emptyRoot = (): QueueRoot => ({
  schemaVersion: SCHEMA_VERSION,
  headKey: null,
  tailKey: null,
  memberGeneration: 0,
});

function parseObject(value: string, label: string): Record<string, unknown> {
  let parsed: unknown;
  try {
    parsed = JSON.parse(value);
  } catch {
    throw new Error(`Page analytics queue ${label} is invalid JSON.`);
  }
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
    throw new Error(`Page analytics queue ${label} is invalid.`);
  }
  return parsed as Record<string, unknown>;
}

function parseRoot(value: string | null): QueueRoot {
  if (value === null) return emptyRoot();
  const parsed = parseObject(value, "root");
  if (
    parsed.schemaVersion !== SCHEMA_VERSION
    || !Number.isInteger(parsed.memberGeneration)
    || (parsed.memberGeneration as number) < 0
    || !(typeof parsed.headKey === "string" || parsed.headKey === null)
    || !(typeof parsed.tailKey === "string" || parsed.tailKey === null)
  ) {
    throw new Error("Page analytics queue root schema is invalid.");
  }
  return parsed as QueueRoot;
}

function validLane(lane: unknown): lane is PageAnalyticsQueueLane {
  if (!lane || typeof lane !== "object" || Array.isArray(lane)) return false;
  const value = lane as Record<string, unknown>;
  if (value.kind === "member") return typeof value.workspaceId === "string";
  if (value.kind === "public") {
    return typeof value.token === "string"
      && typeof value.anonymousToken === "string";
  }
  return value.kind === "publicSite"
    && typeof value.siteSlug === "string"
    && typeof value.anonymousToken === "string";
}

function parseEntry(value: string | null, expectedKey: string): QueueEntry {
  if (value === null) {
    throw new Error("Page analytics queue entry is missing.");
  }
  const parsed = parseObject(value, "entry");
  const observation = parsed.observation as Record<string, unknown> | undefined;
  if (
    parsed.schemaVersion !== SCHEMA_VERSION
    || parsed.key !== expectedKey
    || typeof parsed.laneId !== "string"
    || !validLane(parsed.lane)
    || !observation
    || typeof observation.pageId !== "string"
    || !(observation.rowId === undefined || typeof observation.rowId === "string")
    || !(observation.containerId === undefined || typeof observation.containerId === "string")
    || typeof observation.visitId !== "string"
    || !Number.isInteger(parsed.memberGeneration)
    || (parsed.memberGeneration as number) < 0
    || !(typeof parsed.nextKey === "string" || parsed.nextKey === null)
  ) {
    throw new Error("Page analytics queue entry schema is invalid.");
  }
  return parsed as QueueEntry;
}

function entryKey(laneId: string, visitId: string) {
  return `${ENTRY_PREFIX}${encodeURIComponent(laneId)}:${encodeURIComponent(visitId)}`;
}

function withoutAppend(root: QueueRoot): QueueRoot {
  const { append: _append, ...remaining } = root;
  return remaining;
}

function withoutClaim(root: QueueRoot): QueueRoot {
  const { claim: _claim, ...remaining } = root;
  return remaining;
}

export function createPageAnalyticsQueue(storage: Storage) {
  const readRoot = () => parseRoot(storage.getItem(ROOT_KEY));
  const writeRoot = (root: QueueRoot) => {
    if (root.headKey === null && !root.append && !root.claim) {
      storage.removeItem(ROOT_KEY);
      return;
    }
    storage.setItem(ROOT_KEY, JSON.stringify(root));
  };
  const readEntry = (key: string) => parseEntry(storage.getItem(key), key);
  const writeEntry = (entry: QueueEntry) => {
    storage.setItem(entry.key, JSON.stringify(entry));
  };

  const recoverAppend = (initial?: QueueRoot) => {
    const root = initial ?? readRoot();
    const appended = root.append;
    if (!appended) return root;

    const existing = storage.getItem(appended.key);
    if (existing === null) writeEntry(appended);
    else parseEntry(existing, appended.key);

    if (root.tailKey !== null) {
      const tail = readEntry(root.tailKey);
      if (tail.nextKey !== null && tail.nextKey !== appended.key) {
        throw new Error("Page analytics queue append order is inconsistent.");
      }
      if (tail.nextKey === null) writeEntry({ ...tail, nextKey: appended.key });
    }

    const claim = root.claim && root.claim.nextKey === null
      ? { ...root.claim, nextKey: appended.key }
      : root.claim;
    const recovered: QueueRoot = {
      ...withoutAppend(root),
      headKey: root.headKey ?? appended.key,
      tailKey: appended.key,
      ...(claim ? { claim } : {}),
    };
    writeRoot(recovered);
    return recovered;
  };

  const append = (
    laneId: string,
    lane: PageAnalyticsQueueLane,
    observation: PageAnalyticsQueueObservation,
    memberGeneration: number,
  ) => {
    let root = recoverAppend();
    const key = entryKey(laneId, observation.visitId);
    if (
      storage.getItem(key) !== null
      || root.claim?.entryKeys.includes(key)
    ) return false;

    const entry: QueueEntry = {
      schemaVersion: SCHEMA_VERSION,
      key,
      laneId,
      lane,
      observation,
      memberGeneration,
      nextKey: null,
    };
    root = {
      ...root,
      memberGeneration: Math.max(root.memberGeneration, memberGeneration),
      append: entry,
    };
    writeRoot(root);
    recoverAppend(root);
    return true;
  };

  const claim = (limit: number, requestFor: RequestFactory) => {
    const root = recoverAppend();
    if (root.claim) return root.claim;
    if (root.headKey === null) return null;

    const first = readEntry(root.headKey);
    const entries = [first];
    let nextKey = first.nextKey;
    while (entries.length < limit && nextKey !== null) {
      const next = readEntry(nextKey);
      if (
        next.laneId !== first.laneId
        || next.memberGeneration !== first.memberGeneration
      ) break;
      entries.push(next);
      nextKey = next.nextKey;
    }
    const staleMember = first.lane.kind === "member"
      && first.memberGeneration !== root.memberGeneration;
    const nextClaim: PageAnalyticsQueueClaim = staleMember
      ? {
        kind: "discard",
        entryKeys: entries.map(({ key }) => key),
        nextKey,
        lane: first.lane,
        memberGeneration: first.memberGeneration,
      }
      : {
        kind: "delivery",
        entryKeys: entries.map(({ key }) => key),
        nextKey,
        lane: first.lane,
        memberGeneration: first.memberGeneration,
        request: requestFor(first.lane, entries.map(({ observation }) => observation)),
      };
    writeRoot({ ...root, claim: nextClaim });
    return nextClaim;
  };

  const acknowledge = (expected: PageAnalyticsQueueClaim) => {
    const root = readRoot();
    const current = root.claim;
    if (
      !current
      || current.kind !== expected.kind
      || JSON.stringify(current.entryKeys) !== JSON.stringify(expected.entryKeys)
    ) {
      throw new Error("Page analytics queue claim changed before acknowledgement.");
    }
    for (const key of current.entryKeys) storage.removeItem(key);
    const nextRoot = {
      ...withoutClaim(root),
      headKey: current.nextKey,
      tailKey: current.nextKey === null ? null : root.tailKey,
    };
    writeRoot(nextRoot);
  };

  const memberGeneration = () => readRoot().memberGeneration;
  const resetMemberGeneration = (currentGeneration: number) => {
    const root = recoverAppend();
    const nextGeneration = Math.max(root.memberGeneration, currentGeneration) + 1;
    storage.setItem(ROOT_KEY, JSON.stringify({
      ...root,
      memberGeneration: nextGeneration,
    }));
    return nextGeneration;
  };

  return {
    acknowledge,
    append,
    claim,
    memberGeneration,
    resetMemberGeneration,
  };
}
