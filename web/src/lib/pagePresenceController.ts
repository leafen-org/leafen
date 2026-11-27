import {
  PAGE_ROOM_MUTATION_EVENT,
  PAGE_ROOM_MUTATION_RECEIVED_EVENT,
  type PageRoomMutationChange,
  type PageRoomMutationKind,
  type PageRoomMutationReceived,
} from "./pageRoomEvents";
import {
  cleanPagePresenceMutation,
  cleanPagePresenceState,
  pagePresenceColor,
  PAGE_PRESENCE_AWARENESS_FIELD,
  projectPagePresenceSnapshot,
  type PagePresenceAwarenessState,
  type PagePresenceIdentity,
  type PagePresenceMutationPulse,
  type PagePresenceWireState,
} from "./pagePresenceModel";
import type {
  PagePresenceConnectionStatus,
  PagePresenceSnapshot,
} from "./pagePresenceView";
import { requestWorkspaceChangeCheck } from "./workspaceChangeCheck";

export const PAGE_AWARENESS_EVENT = "leafen:page-presence-awareness";
const AWARENESS_SEND_THROTTLE_MS = 180;

export type PageAwarenessChange = {
  blockId?: string | null;
  mode: "editing" | "idle" | "selecting";
  pageId: string;
  selectedBlockIds?: string[];
  textRange?: { end: number; start: number };
};

type AwarenessChange = {
  added: number[];
  removed: number[];
  updated: number[];
};

export type PagePresenceAwarenessTransport = {
  clientID: number;
  getLocalState(): Record<string, unknown> | null;
  getStates(): Map<number, Record<string, unknown>>;
  off(event: "change", listener: (change: AwarenessChange) => void): void;
  on(event: "change", listener: (change: AwarenessChange) => void): void;
  setLocalStateField(field: string, value: unknown): void;
};

type PagePresenceControllerOptions = {
  awareness: PagePresenceAwarenessTransport;
  identity: { email: string; id: string; name: string };
  isCurrent: () => boolean;
  now?: () => number;
  onSnapshot: (snapshot: PagePresenceSnapshot) => void;
  pageId: string;
};

function cleanIdList(previous?: string[], next?: string[]) {
  const seen = new Set<string>();
  const values: string[] = [];
  for (const source of [previous, next]) {
    for (const raw of source ?? []) {
      const value = raw.trim();
      if (!value || seen.has(value)) continue;
      seen.add(value);
      values.push(value);
    }
  }
  return values.length > 0 ? values : undefined;
}

function mergeMutation(
  previous: PageRoomMutationChange | undefined,
  next: PageRoomMutationChange,
): PageRoomMutationChange {
  if (!previous) return next;
  return {
    ...previous,
    ...next,
    blockIds: cleanIdList(previous.blockIds, next.blockIds),
    patch: previous.patch || next.patch ? { ...previous.patch, ...next.patch } : undefined,
    propertyIds: cleanIdList(previous.propertyIds, next.propertyIds),
    rowIds: cleanIdList(previous.rowIds, next.rowIds),
    viewIds: cleanIdList(previous.viewIds, next.viewIds),
  };
}

export function publishPageAwareness(change: PageAwarenessChange) {
  if (typeof window === "undefined") return;
  window.dispatchEvent(new CustomEvent<PageAwarenessChange>(PAGE_AWARENESS_EVENT, {
    detail: change,
  }));
}

export function createPagePresenceController({
  awareness,
  identity: user,
  isCurrent,
  now = () => Date.now(),
  onSnapshot,
  pageId,
}: PagePresenceControllerOptions) {
  let disposed = false;
  let status: PagePresenceConnectionStatus = "connecting";
  let recoveryFailed = false;
  let sequence = 0;
  let currentAwareness: PagePresenceAwarenessState = {
    mode: "idle",
    selectedBlockIds: [],
    updatedAt: now(),
  };
  let awarenessTimer: number | undefined;
  let pendingAwareness: PageAwarenessChange | undefined;
  let lastAwarenessSentAt = Number.NEGATIVE_INFINITY;
  let mutationFlushQueued = false;
  const pendingMutations = new Map<PageRoomMutationKind, PageRoomMutationChange>();
  const lastMutationSequenceByClient = new Map<number, number>();
  const identity: PagePresenceIdentity = {
    color: pagePresenceColor(user.id),
    label: user.name.trim() || user.email.trim() || "Guest",
    memberId: `ygo:${awareness.clientID}`,
    userId: user.id,
  };
  // Whether the page is out of sight in this tab, which the room hears so
  // that no one draws this person's face or cursor meanwhile, as Notion
  // (pagePresenceModel.ts). Out of sight a tab's timers wake about once a
  // minute, and the room lets go of a presence unheard for 30 seconds: drawn
  // all along, a person with the page in a hidden tab came and went in the
  // others' bars every minute (2026-11-13).
  const page = typeof document === "undefined" ? null : document;
  const outOfSight = () => page !== null && page.visibilityState !== "visible";
  let away = outOfSight();
  let localWire: PagePresenceWireState = {
    ...(away ? { away: true } : {}),
    awareness: currentAwareness,
    identity,
    pageId,
    sequence,
  };

  function current() {
    return !disposed && isCurrent();
  }

  // The presence last told, as text: an awareness change that changes no
  // one's presence (the server telling the room its graph's new place) is
  // not told, as each drew the page again, some 0.3 seconds with a table of
  // 100,000 rows, for every edit another session made (2026-11-11).
  let told = "";
  function emitSnapshot() {
    if (!current()) return;
    const snapshot = projectPagePresenceSnapshot(
      awareness.getStates(),
      pageId,
      user.id,
      status,
      recoveryFailed,
    );
    const text = JSON.stringify(snapshot);
    if (text === told) return;
    told = text;
    onSnapshot(snapshot);
  }

  function publishLocal(mutation?: PagePresenceMutationPulse) {
    if (!current()) return;
    sequence += 1;
    localWire = {
      ...(away ? { away: true } : {}),
      awareness: currentAwareness,
      identity,
      mutation: mutation ?? localWire.mutation,
      pageId,
      sequence,
    };
    awareness.setLocalStateField(PAGE_PRESENCE_AWARENESS_FIELD, localWire);
  }

  function dispatchMutation(clientId: number, state: PagePresenceWireState) {
    const mutation = state.mutation;
    if (!mutation) return;
    const previous = lastMutationSequenceByClient.get(clientId) ?? -1;
    if (mutation.sequence <= previous) return;
    lastMutationSequenceByClient.set(clientId, mutation.sequence);
    if (!current()) return;
    // Someone, or this person in another tab, changed the page's title, icon
    // or style: the workspace follow reads it from the server now rather than
    // at its next check (2026-10-13: 7 to 10 seconds late).
    if (mutation.kind === "page_meta_changed") requestWorkspaceChangeCheck();
    if (state.identity.userId === user.id) return;
    const detail: PageRoomMutationReceived = {
      ...mutation,
      label: state.identity.label,
      memberId: state.identity.memberId ?? `ygo:${clientId}`,
      receivedAt: now(),
      userId: state.identity.userId,
    };
    window.dispatchEvent(new CustomEvent<PageRoomMutationReceived>(
      PAGE_ROOM_MUTATION_RECEIVED_EVENT,
      { detail },
    ));
  }

  function onAwarenessChange(change: AwarenessChange) {
    if (!current()) return;
    const states = awareness.getStates();
    for (const clientId of [...change.added, ...change.updated]) {
      if (clientId === awareness.clientID) continue;
      const state = cleanPagePresenceState(states.get(clientId), pageId);
      if (state) dispatchMutation(clientId, state);
    }
    // A presence the room let go of and hears again (one out of sight, whose
    // timers slept past the room's 30 seconds) still carries the change it
    // last told, which was dispatched already: what was told stays known.
    emitSnapshot();
  }

  function onSightChange() {
    if (!current() || outOfSight() === away) return;
    away = !away;
    publishLocal();
  }

  function applyAwareness(change: PageAwarenessChange) {
    if (!current() || change.pageId !== pageId) return;
    currentAwareness = {
      blockId: change.blockId?.trim() || undefined,
      mode: change.mode,
      selectedBlockIds: change.selectedBlockIds ?? [],
      textRange: change.textRange,
      updatedAt: now(),
    };
    lastAwarenessSentAt = now();
    publishLocal();
  }

  function flushAwareness() {
    awarenessTimer = undefined;
    const change = pendingAwareness;
    pendingAwareness = undefined;
    if (change) applyAwareness(change);
  }

  function onAwarenessEvent(event: Event) {
    const change = (event as CustomEvent<PageAwarenessChange>).detail;
    if (!change || change.pageId !== pageId || !current()) return;
    pendingAwareness = change;
    if (change.mode === "idle" || now() - lastAwarenessSentAt >= AWARENESS_SEND_THROTTLE_MS) {
      if (awarenessTimer !== undefined) window.clearTimeout(awarenessTimer);
      flushAwareness();
      return;
    }
    if (awarenessTimer !== undefined) return;
    awarenessTimer = window.setTimeout(
      flushAwareness,
      Math.max(0, lastAwarenessSentAt + AWARENESS_SEND_THROTTLE_MS - now()),
    );
  }

  function flushPendingMutations() {
    mutationFlushQueued = false;
    if (!current()) return;
    for (const change of pendingMutations.values()) {
      const mutation = cleanPagePresenceMutation({
        ...change,
        pageId,
        sequence: sequence + 1,
      }, pageId);
      if (mutation) publishLocal(mutation);
    }
    pendingMutations.clear();
  }

  function onMutationEvent(event: Event) {
    const change = (event as CustomEvent<PageRoomMutationChange>).detail;
    if (!change || change.pageId !== pageId || !current()) return;
    pendingMutations.set(change.kind, mergeMutation(pendingMutations.get(change.kind), change));
    if (mutationFlushQueued) return;
    mutationFlushQueued = true;
    queueMicrotask(flushPendingMutations);
  }

  function refresh(nextStatus: PagePresenceConnectionStatus, nextRecoveryFailed: boolean) {
    if (!current()) return;
    status = nextStatus;
    recoveryFailed = nextRecoveryFailed;
    emitSnapshot();
  }

  function dispose() {
    if (disposed) return;
    if (isCurrent()) flushPendingMutations();
    disposed = true;
    if (awarenessTimer !== undefined) window.clearTimeout(awarenessTimer);
    awarenessTimer = undefined;
    pendingAwareness = undefined;
    pendingMutations.clear();
    awareness.off("change", onAwarenessChange);
    window.removeEventListener(PAGE_AWARENESS_EVENT, onAwarenessEvent);
    window.removeEventListener(PAGE_ROOM_MUTATION_EVENT, onMutationEvent);
    page?.removeEventListener("visibilitychange", onSightChange);
  }

  awareness.on("change", onAwarenessChange);
  window.addEventListener(PAGE_AWARENESS_EVENT, onAwarenessEvent);
  window.addEventListener(PAGE_ROOM_MUTATION_EVENT, onMutationEvent);
  page?.addEventListener("visibilitychange", onSightChange);
  publishLocal();
  emitSnapshot();

  return { dispose, refresh };
}
