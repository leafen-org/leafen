import { useEffect, useMemo, useRef } from "react";
import { onCommentRevision } from "@/lib/commentRevisionEvents";
import { announceUnreadCountChange, onNoticeArrival } from "@/lib/noticeArrivals";
import { onWindowReturn } from "@/lib/windowReturn";
import { afterOwnWrites } from "@/lib/ownWritesInFlight";
import type {
  ListNotificationsInput,
  NotificationMutationResult,
} from "@/lib/notificationReadContract";

const UNREAD_REFRESH_THROTTLE_MS = 30_000;
const COMMENT_UPDATE_DELAYS_MS = [1_500, 5_000];

type RefreshMode = "fresh" | "normal";

export type SidebarUnreadNotificationReader = (
  options: Pick<ListNotificationsInput, "workspaceId"> & {
  limit: 1;
  includeRead: false;
  },
) => Promise<Pick<NotificationMutationResult, "unreadCount" | "complete">>;

export type SidebarUnreadRefreshBounds = {
  maxLanes: number;
};

interface UnreadRefreshLane {
  active?: Promise<void>;
  lastSuccessAt?: number;
  retryAfterFailure: boolean;
  trailingGeneration?: number;
}

type SidebarUnreadRefreshCoordinatorOptions = {
  bounds: SidebarUnreadRefreshBounds;
  /** How many personal notifications are unread, for the inbox's badge. */
  onUnreadChange: (unread: number) => void;
  readNotifications: SidebarUnreadNotificationReader;
  /** A count this tab read (or set), for the person's other tabs. */
  onShown?: (workspaceId: string, unread: number, at: number) => void;
};

function validateBounds(bounds: SidebarUnreadRefreshBounds) {
  if (!Number.isSafeInteger(bounds.maxLanes) || bounds.maxLanes < 0) {
    throw new RangeError("Invalid unread refresh lane bound.");
  }
}

export function createSidebarUnreadRefreshCoordinator({
  bounds,
  onUnreadChange,
  readNotifications,
  onShown,
}: SidebarUnreadRefreshCoordinatorOptions) {
  validateBounds(bounds);
  const lanes = new Map<string, UnreadRefreshLane>();
  // The count each workspace's badge last showed, and when it was known: one
  // that changes tells an open inbox to read again (noticeArrivals.ts). A
  // count another tab knew later than this one wins (shareUnreadAcrossTabs).
  const shown = new Map<string, number>();
  const shownAt = new Map<string, number>();
  const show = (workspaceId: string, unread: number, at = Date.now(), fromAnotherTab = false) => {
    if (at < (shownAt.get(workspaceId) ?? 0)) return;
    const before = shown.get(workspaceId);
    shown.set(workspaceId, unread);
    shownAt.set(workspaceId, at);
    onUnreadChange(unread);
    if (before !== undefined && unread !== before) announceUnreadCountChange(workspaceId);
    if (!fromAnotherTab) onShown?.(workspaceId, unread, at);
  };
  let currentGeneration = 0;
  let currentWorkspaceId: string | undefined;
  let inboxOpen: boolean | undefined;
  let mounted = false;
  // A switch made while the workspace being left still reads: the new
  // workspace reads once that read settles, within the lane bound.
  let owedRefresh = false;

  const laneFor = (workspaceId: string, replaceableWorkspaceId?: string) => {
    const existing = lanes.get(workspaceId);
    if (existing) return existing;
    if (replaceableWorkspaceId) {
      const replaceable = lanes.get(replaceableWorkspaceId);
      if (replaceable && !replaceable.active) lanes.delete(replaceableWorkspaceId);
    }
    if (lanes.size >= bounds.maxLanes) {
      throw new RangeError("Unread refresh lane capacity exceeded.");
    }
    const lane: UnreadRefreshLane = { retryAfterFailure: false };
    lanes.set(workspaceId, lane);
    return lane;
  };

  const start = (
    lane: UnreadRefreshLane,
    workspaceId: string,
    requestGeneration: number,
  ) => {
    const run = (async () => {
      // Defer the reader invocation until `lane.active` owns this generation.
      // This also turns a synchronous reader failure into the normal rejection path.
      await Promise.resolve();
      // A count read before this tab's own read or archive lands would bring
      // back what it just cleared (lib/ownWritesInFlight.ts).
      await afterOwnWrites();
      try {
        const result = await readNotifications({
          workspaceId,
          limit: 1,
          includeRead: false,
        });
        lane.lastSuccessAt = Date.now();
        lane.retryAfterFailure = false;

        // A fresh trigger that arrived during this request owns the next result.
        // Do not briefly re-apply the older generation while that trailing read drains.
        if (
          lane.trailingGeneration === undefined
          && mounted
          && currentWorkspaceId === workspaceId
          && currentGeneration === requestGeneration
        ) {
          // A list too long to count whole still has something unread.
          show(workspaceId, Math.max(result.unreadCount ?? 0, result.complete === true ? 0 : 1));
        }
      } catch {
        // A failed forced refresh must not inherit an earlier successful throttle.
        lane.retryAfterFailure = true;
      }
    })();
    lane.active = run;
    void run.then(() => {
      if (lane.active !== run) return;
      lane.active = undefined;
      const trailingGeneration = lane.trailingGeneration;
      lane.trailingGeneration = undefined;

      if (
        trailingGeneration !== undefined
        && mounted
        && currentWorkspaceId === workspaceId
        && currentGeneration === trailingGeneration
      ) {
        start(lane, workspaceId, trailingGeneration);
      } else if (currentWorkspaceId !== workspaceId) {
        lanes.delete(workspaceId);
        if (owedRefresh) {
          owedRefresh = false;
          refresh("fresh");
        }
      }
    });
  };

  const refresh = (mode: RefreshMode) => {
    const workspaceId = currentWorkspaceId;
    if (!mounted || !workspaceId) return;
    const lane = laneFor(workspaceId);
    if (lane.active) {
      if (mode === "fresh") lane.trailingGeneration = currentGeneration;
      return;
    }
    if (
      mode === "normal"
      && !lane.retryAfterFailure
      && lane.lastSuccessAt !== undefined
      && Date.now() - lane.lastSuccessAt < UNREAD_REFRESH_THROTTLE_MS
    ) {
      return;
    }
    start(lane, workspaceId, currentGeneration);
  };

  return {
    mount() {
      mounted = true;
    },
    unmount() {
      mounted = false;
    },
    setWorkspace(workspaceId: string | undefined, refreshWhenVisible: boolean) {
      const changed = currentWorkspaceId !== workspaceId;
      const previousWorkspaceId = currentWorkspaceId;
      let admitted = true;
      if (changed && workspaceId && refreshWhenVisible) {
        try {
          laneFor(workspaceId, previousWorkspaceId);
        } catch (error) {
          if (!(error instanceof RangeError)) throw error;
          admitted = false;
        }
      }
      if (changed) {
        currentWorkspaceId = workspaceId;
        currentGeneration += 1;
        if (previousWorkspaceId) {
          const previousLane = lanes.get(previousWorkspaceId);
          if (previousLane && !previousLane.active) lanes.delete(previousWorkspaceId);
        }
      }
      if (!workspaceId || !refreshWhenVisible) return;
      if (!admitted) {
        owedRefresh = true;
        return;
      }
      owedRefresh = false;

      // Re-entering a workspace whose older generation is still active requires
      // one post-settlement read; a normal StrictMode replay simply joins it.
      const lane = laneFor(workspaceId);
      refresh(changed && lane.active ? "fresh" : "normal");
    },
    setInboxOpen(open: boolean) {
      if (inboxOpen === undefined) {
        inboxOpen = open;
        return;
      }
      const justClosed = inboxOpen && !open;
      inboxOpen = open;
      if (justClosed) refresh("fresh");
    },
    refreshFresh() {
      refresh("fresh");
    },
    refreshNormal() {
      refresh("normal");
    },
    markAllReadOptimistically() {
      if (mounted && currentWorkspaceId) show(currentWorkspaceId, 0);
    },
    /** A count the person's other tab knew, shown here when it is newer. */
    adopt(workspaceId: string, unread: number, at: number) {
      if (mounted && currentWorkspaceId === workspaceId) show(workspaceId, unread, at, true);
    },
    /** The count this tab shows for a workspace and when it was known, if it has one. */
    known(workspaceId: string): { unread: number; at: number } | null {
      const unread = shown.get(workspaceId), at = shownAt.get(workspaceId);
      return unread === undefined || at === undefined ? null : { unread, at };
    },
  };
}

type UnreadShareMessage =
  | { kind: "count"; workspaceId: string; unread: number; at: number }
  | { kind: "ask"; workspaceId: string };

/**
 * The person's tabs show one count (2026-11-06: a tab opened behind showed
 * none, and one left behind an old one, so the tab strip read differently
 * tab by tab). A hidden tab reads nothing and holds no socket, to spare the
 * server; the tab in view shares each count it reads with the others in
 * this browser, and a tab opening behind asks the others what they know.
 */
function shareUnreadAcrossTabs(
  key: string,
  coordinator: ReturnType<typeof createSidebarUnreadRefreshCoordinator>,
  workspaceId: string | undefined,
  share: { current: ((workspaceId: string, unread: number, at: number) => void) | undefined },
) {
  if (typeof BroadcastChannel === "undefined") return () => undefined;
  const channel = new BroadcastChannel(`leafen:inbox-unread:${key}`);
  const post = (message: UnreadShareMessage) => { try { channel.postMessage(message); } catch { /* a closed channel says nothing */ } };
  channel.onmessage = ({ data }: MessageEvent<UnreadShareMessage>) => {
    if (!data || typeof data.workspaceId !== "string") return;
    if (data.kind === "count" && Number.isFinite(data.unread) && Number.isFinite(data.at)) {
      coordinator.adopt(data.workspaceId, Math.max(0, Math.floor(data.unread)), data.at);
    } else if (data.kind === "ask") {
      const known = coordinator.known(data.workspaceId);
      if (known) post({ kind: "count", workspaceId: data.workspaceId, ...known });
    }
  };
  share.current = (id, unread, at) => post({ kind: "count", workspaceId: id, unread, at });
  if (workspaceId) post({ kind: "ask", workspaceId });
  return () => {
    share.current = undefined;
    channel.close();
  };
}

export function useSidebarUnreadRefresh({
  bounds,
  onUnreadChange,
  readNotifications,
  updatesOpen,
  workspaceId,
  shareKey,
}: {
  bounds: SidebarUnreadRefreshBounds;
  onUnreadChange: (unread: number) => void;
  readNotifications: SidebarUnreadNotificationReader;
  updatesOpen: boolean;
  workspaceId: string | undefined;
  /** Whose count it is (the signed-in person), shared by their tabs in this browser. */
  shareKey?: string;
}) {
  const share = useRef<((workspaceId: string, unread: number, at: number) => void) | undefined>(undefined);
  const coordinator = useMemo(
    () => createSidebarUnreadRefreshCoordinator({
      bounds, onUnreadChange, readNotifications, onShown: (id, unread, at) => share.current?.(id, unread, at),
    }),
    [bounds, onUnreadChange, readNotifications],
  );

  useEffect(() => (shareKey ? shareUnreadAcrossTabs(shareKey, coordinator, workspaceId, share) : undefined),
    [coordinator, shareKey, workspaceId]);

  useEffect(() => {
    coordinator.mount();
    coordinator.setWorkspace(workspaceId, document.visibilityState !== "hidden");
    return () => coordinator.unmount();
  }, [coordinator, workspaceId]);

  useEffect(() => {
    coordinator.setInboxOpen(updatesOpen);
  }, [coordinator, updatesOpen]);

  // The inbox's badge follows updates as they come, as Notion's: while the
  // app is in view it asks now and then, and at once when someone comments
  // on an open page, which may mention the reader.
  useEffect(() => {
    const refreshWhenVisible = () => {
      if (document.visibilityState !== "hidden") coordinator.refreshNormal();
    };
    const timer = window.setInterval(refreshWhenVisible, UNREAD_REFRESH_THROTTLE_MS);
    // The update a comment makes is delivered a moment after the comment.
    let followUps: number[] = [];
    const stopComments = onCommentRevision(() => {
      for (const followUp of followUps) window.clearTimeout(followUp);
      followUps = COMMENT_UPDATE_DELAYS_MS.map((delay) => window.setTimeout(() => {
        if (document.visibilityState !== "hidden") coordinator.refreshFresh();
      }, delay));
    });
    const stopReturn = onWindowReturn(refreshWhenVisible);
    // A notice for the person, on whatever page, comes through the workspace's waiting check.
    const stopNotices = onNoticeArrival((arrivedIn) => {
      if (arrivedIn === workspaceId) coordinator.refreshFresh();
    });
    return () => {
      window.clearInterval(timer);
      for (const followUp of followUps) window.clearTimeout(followUp);
      stopComments();
      stopNotices();
      stopReturn();
    };
  }, [coordinator, workspaceId]);

  useEffect(() => {
    if (!workspaceId) return;
    let timer: number | undefined;
    const onReadChanged = (event: Event) => {
      const detail = (event as CustomEvent<{ allRead?: boolean }>).detail;
      if (detail?.allRead) coordinator.markAllReadOptimistically();
      window.clearTimeout(timer);
      timer = window.setTimeout(() => coordinator.refreshFresh(), 2000);
    };
    window.addEventListener("leafen:updates-read-changed", onReadChanged);
    return () => {
      window.clearTimeout(timer);
      window.removeEventListener("leafen:updates-read-changed", onReadChanged);
    };
  }, [coordinator, workspaceId]);
}
