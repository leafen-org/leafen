import { useCallback, useEffect, useRef, useState } from "react";
import { PAGE_ROOM_MUTATION_EVENT, PAGE_ROOM_MUTATION_RECEIVED_EVENT, LOCAL_DATABASE_MUTATION_EVENT } from "@/lib/pageRoomEvents";
import { readWorkspaceActivities, readWorkspaceActivityChanges } from "@/workspaceActivityApi";
import { api } from "@/api";
import { workspaceChangeDecisions } from "@/workspaceMembershipWire.generated";
import type { WorkspaceActivityChangesResponse, WorkspaceActivityRecord, WorkspaceActivityResponse } from "@/workspaceActivityWire.generated";
import { onWindowReturn } from "@/lib/windowReturn";
import { isServerUnreachable } from "@/lib/serverReach";
import { useReadAgainWhenAnswered } from "@/lib/unansweredRead";
import type { Page, Session } from "@/types";

const EMPTY: WorkspaceActivityResponse = { workspaceId: "", items: [], actorProfiles: [], nextCursor: "", retentionDays: 0, refreshToken: "" };
/** Updates whose changes are read at once; the others wait their turn. */
const CHANGE_READS = 2;
type OwnedSnapshot = { session: Session; pages: Page[]; data: WorkspaceActivityResponse };

/** Every request belongs to one panel state, session and workspace. Changing
 * authority clears the old list before a replacement response can publish. */
export function useWorkspaceActivityController({ enabled, pages, session, workspaceId }: {
  enabled: boolean; pages: Page[]; session: Session; workspaceId: string;
}) {
  const [snapshot, setSnapshot] = useState<OwnedSnapshot | null>(null);
  const [loading, setLoading] = useState(false);
  const [failed, setFailed] = useState(false);
  // A read the server wasn't there for says so and is read again once it may answer (unansweredRead.ts).
  const [unanswered, setUnanswered] = useState(false);
  const [revision, setRevision] = useState(0);
  const life = useRef<{ abort: AbortController; busy: boolean } | null>(null);
  const nextCursor = useRef("");
  const currentSnapshot = useRef(snapshot);
  currentSnapshot.current = snapshot;
  const restart = useCallback(() => { setSnapshot(null); setRevision((value) => value + 1); }, []);
  useReadAgainWhenAnswered(enabled && unanswered, restart);
  // An update's changes, read once as it comes into view and kept while the
  // list is read again; an update that grew since is read anew.
  const changes = useRef({ owner: "", entries: new Map<string, Promise<WorkspaceActivityChangesResponse>>(), reading: 0, waiting: [] as (() => void)[] });
  const readChanges = useCallback((item: WorkspaceActivityRecord) => {
    const owner = `${session.user.id}\u0000${workspaceId}`;
    if (changes.current.owner !== owner) changes.current = { owner, entries: new Map(), reading: 0, waiting: [] };
    const store = changes.current;
    const key = `${item.id}\u0000${item.occurredAt}`;
    let read = store.entries.get(key);
    if (!read) {
      read = new Promise<void>((begin) => {
        const start = () => { store.reading += 1; begin(); };
        if (store.reading < CHANGE_READS) start();
        else store.waiting.push(start);
      }).then(() => readWorkspaceActivityChanges(session, workspaceId, item.id)).finally(() => {
        store.reading -= 1;
        store.waiting.shift()?.();
      });
      read.catch(() => { if (store.entries.get(key) === read) store.entries.delete(key); });
      store.entries.set(key, read);
    }
    return read;
  }, [session, workspaceId]);

  const fetchTurn = useCallback(async (cursor: string, current: NonNullable<typeof life.current>, append: boolean) => {
    if (current.busy || current.abort.signal.aborted) return;
    current.busy = true;
    setLoading(true);
    setFailed(false);
    try {
      let result: WorkspaceActivityResponse;
      // A turn with no readable rows still advances. There is no total scan
      // cap that can strand readable activity behind someone else's pages.
      do {
        result = await readWorkspaceActivities(session, workspaceId, cursor, current.abort.signal);
        current.abort.signal.throwIfAborted();
        if (life.current !== current || result.workspaceId !== workspaceId) return;
        if (result.nextCursor && result.nextCursor === cursor) throw new Error("Activity cursor did not advance.");
        cursor = result.nextCursor;
      } while (result.items.length === 0 && cursor);
      if (life.current !== current) return;
      nextCursor.current = result.nextCursor;
      setSnapshot((previous) => {
        if (!append || previous?.session !== session || previous.pages !== pages || previous.data.workspaceId !== workspaceId) return { session, pages, data: result };
        const items = new Map(previous.data.items.map((item) => [item.id, item]));
        for (const item of result.items) items.set(item.id, item);
        const profiles = new Map(previous.data.actorProfiles.map((profile) => [profile.userId, profile]));
        for (const profile of result.actorProfiles) profiles.set(profile.userId, profile);
        return { session, pages, data: { ...result, refreshToken: previous.data.refreshToken, items: [...items.values()], actorProfiles: [...profiles.values()] } };
      });
      setUnanswered(false);
    } catch (error) {
      if (!current.abort.signal.aborted && life.current === current) {
        setSnapshot(null);
        setFailed(true);
        setUnanswered(isServerUnreachable(error));
      }
    } finally {
      current.busy = false;
      if (life.current === current && !current.abort.signal.aborted) setLoading(false);
    }
  }, [session, workspaceId, pages]);

  useEffect(() => {
    setSnapshot(null);
    setFailed(false);
    nextCursor.current = "";
    if (!enabled) { setLoading(false); return; }
    const current = { abort: new AbortController(), busy: false };
    life.current = current;
    void fetchTurn("", current, false);
    return () => { current.abort.abort(); if (life.current === current) life.current = null; };
  }, [enabled, fetchTurn, revision]);

  useEffect(() => {
    if (!enabled) return;
    const changed = () => { life.current?.abort.abort(); restart(); };
    const events = [PAGE_ROOM_MUTATION_EVENT, PAGE_ROOM_MUTATION_RECEIVED_EVENT, LOCAL_DATABASE_MUTATION_EVENT];
    for (const event of events) window.addEventListener(event, changed);
    return () => { for (const event of events) window.removeEventListener(event, changed); };
  }, [enabled, restart]);

  useEffect(() => {
    if (!enabled) return;
    let stopped = false;
    let checking = false;
    const check = async () => {
      const current = life.current;
      const owned = currentSnapshot.current;
      if (stopped || checking || !current || current.busy || current.abort.signal.aborted
        || document.visibilityState !== "visible" || owned?.session !== session
        || owned.data.workspaceId !== workspaceId || !owned.data.refreshToken) return;
      checking = true;
      try {
        const result = await api.probeWorkspaceChange(session, workspaceId, owned.data.refreshToken);
        if (!stopped && life.current === current && !current.abort.signal.aborted
          && result.decision === workspaceChangeDecisions.bootstrapRequired) {
          current.abort.abort();
          restart();
        }
      } catch {
        if (!stopped && life.current === current && !current.abort.signal.aborted) {
          current.abort.abort();
          restart();
        }
      } finally { checking = false; }
    };
    const timer = window.setInterval(() => void check(), 10_000);
    const stopReturn = onWindowReturn(() => void check());
    return () => {
      stopped = true;
      window.clearInterval(timer);
      stopReturn();
    };
  }, [enabled, session, workspaceId, restart]);

  const loadMore = useCallback(() => {
    const current = life.current;
    if (current && nextCursor.current) void fetchTurn(nextCursor.current, current, true);
  }, [fetchTurn]);
  const data = enabled && snapshot?.session === session && snapshot.pages === pages && snapshot.data.workspaceId === workspaceId ? snapshot.data : EMPTY;
  return { data, loading, failed, unanswered, loadMore, retry: restart, readChanges };
}
