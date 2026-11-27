import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { usePagesWithEditTimes } from "@/lib/pageEditedHere";
import { api } from "@/api";
import {
  commentBounds,
  type NotificationInboxResponse,
  type NotificationReadInput,
  type NotificationReadSettlement,
} from "@/commentWire.generated";
import { newId } from "@/lib/ids";
import { onNoticeArrival, onUnreadCountChange } from "@/lib/noticeArrivals";
import { holdOwnWrite, readAfterOwnWrites } from "@/lib/ownWritesInFlight";
import { isServerUnreachable } from "@/lib/serverReach";
import { useReadAgainWhenAnswered } from "@/lib/unansweredRead";
import type { Page, Session } from "@/types";
import { buildPageEditActivityInputs, type InboxState } from "./updatesPanelModel";

type NotificationReadClient = Pick<
  typeof api,
  "mutateNotificationRead" | "continueNotificationSettlement"
>;

const EMPTY_SNAPSHOT: NotificationInboxResponse = {
  workspaceId: "",
  items: [],
  actorProfiles: [],
  complete: true,
  unreadCount: 0,
  total: 0,
};

export async function drainNotificationReadSettlement({
  client,
  input,
  session,
  signal,
  workspaceId,
}: {
  client: NotificationReadClient;
  input: NotificationReadInput;
  session: Session;
  signal: AbortSignal;
  workspaceId: string;
}) {
  signal.throwIfAborted();
  let response = await client.mutateNotificationRead(
    session,
    workspaceId,
    input,
    signal,
  );
  let settlement = response.settlement;
  // However many notifications marking everything read reaches, it goes on
  // to the end (2026-10-13: it stopped past the newest 2,000).
  while (settlement.status === "pending") {
    signal.throwIfAborted();
    response = await client.continueNotificationSettlement(
      session,
      workspaceId,
      settlement.id,
      signal,
    );
    settlement = response.settlement;
  }
  return settlement as NotificationReadSettlement;
}

/** Personal notification states. Workspace activity has its own journal reader. */
function readInbox(session: Session, workspaceId: string, state: InboxState, signal?: AbortSignal, cursor?: string) {
  return state === "workspace"
    ? Promise.resolve({ ...EMPTY_SNAPSHOT, workspaceId })
    : api.listNotifications(session, workspaceId, commentBounds.notificationInboxMax, signal, "personal", state, cursor);
}

function announceReadChange(allRead: boolean) {
  window.dispatchEvent(new CustomEvent("leafen:updates-read-changed", { detail: { allRead } }));
}

export function useUpdatesPanelController({
  pages,
  session,
  state,
  workspaceId,
}: {
  pages: Page[];
  session: Session;
  state: InboxState;
  workspaceId: string;
}) {
  const [snapshot, setSnapshot] = useState<NotificationInboxResponse>(() => ({
    ...EMPTY_SNAPSHOT,
    workspaceId,
  }));
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  // The tab's read the server wasn't there for: it says so, not that nothing
  // came, and is read again once the server may answer (unansweredRead.ts).
  const [unanswered, setUnanswered] = useState(false);
  const [readTurn, setReadTurn] = useState(0);
  useReadAgainWhenAnswered(unanswered, () => setReadTurn((turn) => turn + 1));
  const unansweredRef = useRef(unanswered);
  unansweredRef.current = unanswered;
  const generationRef = useRef(0);
  const snapshotRef = useRef(snapshot);
  const mutationQueueRef = useRef<Promise<void>>(Promise.resolve());
  snapshotRef.current = snapshot;

  // Each page's last edit as last known, one made here after the list's (lib/pageEditedHere.ts).
  const edited = usePagesWithEditTimes(pages);
  const pageActivityInputs = useMemo(
    () => buildPageEditActivityInputs(edited, session.user.id),
    [edited, session.user.id],
  );
  const pageActivityKey = JSON.stringify(pageActivityInputs);

  useEffect(() => {
    const generation = generationRef.current + 1;
    generationRef.current = generation;
    const controller = new AbortController();
    // A read again after one the server wasn't there for keeps its words until it answers.
    if (!unansweredRef.current) setLoading(true);

    if (state === "workspace") {
      snapshotRef.current = { ...EMPTY_SNAPSHOT, workspaceId };
      setSnapshot(snapshotRef.current);
      setLoading(false);
      setUnanswered(false);
      return () => { generationRef.current += 1; controller.abort(); };
    }

    void (async () => {
      if (pageActivityInputs.length > 0) {
        try {
          await api.syncNotificationActivities(
            session,
            workspaceId,
            { activities: pageActivityInputs },
            controller.signal,
          );
        } catch (error) {
          if (controller.signal.aborted) return;
          // A page can advance between bootstrap and sync validation. The
          // authoritative Inbox read remains useful and must still publish.
          void error;
        }
      }
      if (generationRef.current !== generation || controller.signal.aborted) return;
      try {
        const result = await readInbox(session, workspaceId, state, controller.signal);
        if (generationRef.current !== generation || controller.signal.aborted) return;
        snapshotRef.current = result;
        setSnapshot(result);
        setUnanswered(false);
      } catch (error) {
        // Another tab's list isn't this one's: a tab not read shows nothing of the one before.
        if (generationRef.current === generation && !controller.signal.aborted && isServerUnreachable(error)) {
          snapshotRef.current = { ...EMPTY_SNAPSHOT, workspaceId };
          setSnapshot(snapshotRef.current);
          setUnanswered(true);
        }
        throw error;
      } finally {
        if (generationRef.current === generation && !controller.signal.aborted) {
          setLoading(false);
        }
      }
    })().catch(() => {
      if (generationRef.current === generation && !controller.signal.aborted) {
        setLoading(false);
      }
    });

    return () => {
      generationRef.current += 1;
      controller.abort();
    };
    // The serialized page input is the bounded remote-work identity. A new
    // array with identical data must not restart the lifecycle.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pageActivityKey, readTurn, session, state, workspaceId]);

  const reconcile = useCallback(async (generation: number) => {
    try {
      const result = await readInbox(session, workspaceId, state);
      if (generationRef.current !== generation) return;
      snapshotRef.current = result;
      setSnapshot(result);
    } catch {
      // Keep the last coherent snapshot; the next arrival or badge change reads again.
    }
  }, [session, state, workspaceId]);
  // An open inbox shows a notice as it comes, and whatever the badge learned
  // while the app was away or on the open page — more, or fewer (noticeArrivals.ts).
  // Its own read or archive moves the badge too, whose change would read the
  // list back before the server has it: the read waits for this tab's
  // changes on their way (lib/ownWritesInFlight.ts).
  useEffect(() => {
    const readOnce = readAfterOwnWrites(() => reconcile(generationRef.current));
    const readAgain = (cameIn: string) => {
      if (cameIn === workspaceId) readOnce();
    };
    const stopArrivals = onNoticeArrival(readAgain);
    const stopChanges = onUnreadCountChange(readAgain);
    return () => {
      stopArrivals();
      stopChanges();
    };
  }, [reconcile, workspaceId]);

  const enqueueRead = useCallback((input: NotificationReadInput) => {
    const generation = generationRef.current;
    const controller = new AbortController();
    const release = holdOwnWrite();
    const run = async () => {
      try {
        await drainNotificationReadSettlement({
          client: {
            mutateNotificationRead: api.mutateNotificationRead,
            continueNotificationSettlement: api.continueNotificationSettlement,
          },
          input,
          session,
          signal: controller.signal,
          workspaceId,
        });
        announceReadChange(input.mode === "all");
      } catch {
        if (generationRef.current === generation) await reconcile(generation);
      } finally {
        release();
      }
    };
    const queued = mutationQueueRef.current.then(run, run);
    mutationQueueRef.current = queued.catch(() => {});
    return queued;
  }, [reconcile, session, workspaceId]);

  const markRead = useCallback((notificationId: string) => {
    const current = snapshotRef.current;
    if (current.items.find((item) => item.id === notificationId)?.readAt) return Promise.resolve();
    const readAt = new Date().toISOString();
    const next = {
      ...current,
      unreadCount: Math.max(0, current.unreadCount - 1),
      items: current.items.map((item) => item.id === notificationId ? { ...item, readAt } : item),
    };
    snapshotRef.current = next;
    setSnapshot(next);
    return enqueueRead({ requestId: newId(), mode: "selected", notificationIds: [notificationId] });
  }, [enqueueRead]);

  const markAllRead = useCallback(() => {
    const current = snapshotRef.current;
    if (current.unreadCount === 0 || !current.through) return Promise.resolve();
    const readAt = new Date().toISOString();
    const next = {
      ...current,
      unreadCount: 0,
      items: current.items.map((item) => item.readAt ? item : { ...item, readAt }),
    };
    snapshotRef.current = next;
    setSnapshot(next);
    // Queued first, so the badge's change this announces doesn't read the list back before it lands.
    const queued = enqueueRead({ requestId: newId(), mode: "all", through: current.through });
    announceReadChange(true);
    return queued;
  }, [enqueueRead]);

  // Runs one change after the ones before it; when it fails, the list is
  // read again so it shows what the server kept.
  const enqueueChange = useCallback((change: (signal: AbortSignal) => Promise<unknown>) => {
    const generation = generationRef.current;
    const controller = new AbortController();
    const release = holdOwnWrite();
    const run = async () => {
      try {
        await change(controller.signal);
        announceReadChange(snapshotRef.current.unreadCount === 0);
      } catch {
        if (generationRef.current === generation) await reconcile(generation);
      } finally {
        release();
      }
    };
    const queued = mutationQueueRef.current.then(run, run);
    mutationQueueRef.current = queued.catch(() => {});
    return queued;
  }, [reconcile]);

  // Takes notifications off the list now; the inbox's unread count drops by
  // the unread ones among them while they leave the inbox.
  const takeOff = useCallback((ids: ReadonlySet<string>, leavingInbox: boolean) => {
    const current = snapshotRef.current;
    const taken = current.items.filter((item) => ids.has(item.id));
    if (taken.length === 0) return false;
    const unread = leavingInbox ? taken.filter((item) => !item.readAt).length : 0;
    const next = {
      ...current,
      items: current.items.filter((item) => !ids.has(item.id)),
      total: Math.max(0, current.total - taken.length),
      unreadCount: Math.max(0, current.unreadCount - unread),
    };
    snapshotRef.current = next;
    setSnapshot(next);
    return true;
  }, []);

  const archiveNotifications = useCallback((notificationIds: string[]) => {
    const ids = new Set(notificationIds.slice(0, commentBounds.notificationArchiveSelected));
    if (!takeOff(ids, true)) return Promise.resolve();
    return enqueueChange((signal) => api.archiveNotifications(
      session, workspaceId, { requestId: newId(), notificationIds: [...ids] }, signal,
    ));
  }, [enqueueChange, session, takeOff, workspaceId]);

  const archiveNotification = useCallback(
    (notificationId: string) => archiveNotifications([notificationId]),
    [archiveNotifications],
  );

  const archiveAll = useCallback(
    () => archiveNotifications(snapshotRef.current.items.map((item) => item.id)),
    [archiveNotifications],
  );

  const unarchiveNotification = useCallback((notificationId: string) => {
    if (!takeOff(new Set([notificationId]), false)) return Promise.resolve();
    return enqueueChange((signal) => api.changeNotificationState(
      session, workspaceId, { notificationIds: [notificationId], change: "unarchive" }, signal,
    ));
  }, [enqueueChange, session, takeOff, workspaceId]);

  const markUnread = useCallback((notificationId: string) => {
    const current = snapshotRef.current;
    const item = current.items.find((entry) => entry.id === notificationId);
    if (!item?.readAt) return Promise.resolve();
    const next = {
      ...current,
      unreadCount: current.unreadCount + 1,
      items: current.items.map((entry) => entry.id === notificationId ? { ...entry, readAt: "" } : entry),
    };
    snapshotRef.current = next;
    setSnapshot(next);
    return enqueueChange((signal) => api.changeNotificationState(
      session, workspaceId, { notificationIds: [notificationId], change: "unread" }, signal,
    ));
  }, [enqueueChange, session, workspaceId]);

  // The next page of the tab, read on from where the list ends, as Notion's
  // inbox goes on while it scrolls.
  const loadMore = useCallback(async () => {
    const current = snapshotRef.current;
    if (!current.hasMore || !current.nextCursor || loadingMore) return;
    const generation = generationRef.current;
    setLoadingMore(true);
    try {
      const next = await readInbox(session, workspaceId, state, undefined, current.nextCursor);
      if (generationRef.current !== generation || snapshotRef.current.nextCursor !== current.nextCursor) return;
      const seen = new Set(snapshotRef.current.items.map((item) => item.id));
      const profiles = new Map(snapshotRef.current.actorProfiles.map((profile) => [profile.userId, profile]));
      for (const profile of next.actorProfiles) profiles.set(profile.userId, profile);
      const merged = {
        ...next,
        items: [...snapshotRef.current.items, ...next.items.filter((item) => !seen.has(item.id))],
        actorProfiles: [...profiles.values()],
      };
      snapshotRef.current = merged;
      setSnapshot(merged);
    } catch {
      // The button stays to try again.
    } finally {
      if (generationRef.current === generation) setLoadingMore(false);
    }
  }, [loadingMore, session, state, workspaceId]);

  return {
    archiveAll, archiveNotification, loading, loadingMore, loadMore, markAllRead, markRead, markUnread, snapshot, unarchiveNotification,
    unanswered,
  };
}
