import { onWindowReturn } from "./windowReturn";

const ROOM_RETURNED_EVENT = "leafen:page-room-returned";
const COMMENT_RECONNECT_GIVEN_UP_EVENT = "leafen:comment-reconnect-given-up";
const COMMENT_FOLLOWING_CHANGED_EVENT = "leafen:comment-following-changed";
// A return waits for the reconnect's read as long as a connection is given to
// open (collaboration.ts); one not back by then reads the comments itself.
const RECONNECT_READ_WAIT_MS = 20_000;

// Pages whose room connection closed and reads their comments again once it
// is back, each counted once for every connection of it waiting so. A return
// meanwhile — the tab back in view, the network back — waits for that read
// rather than reading them too: the read as the connection is back, after it
// joined the room, also has every comment posted since the return, and those
// after it the room announces (2026-11-13: a tab whose connection Chrome
// closed while it was hidden read its comments twice as it showed again).
const reconnecting = new Map<string, number>();

/**
 * A page's room back after it closed (collaboration.ts). Awareness is
 * transient: what the room said meanwhile wasn't heard, so its durable
 * comments are read again (onCommentRefresh), and so is a row's own page
 * (onRoomReturn, lib/rowChangeWords.ts) — a lock set on another device
 * closes the room as it turns read-only, and the room's word that the row
 * changed went to no one (2026-11-18).
 */
export function announceRoomReturn(pageId: string) {
  if (typeof window === "undefined") return;
  window.dispatchEvent(new CustomEvent(ROOM_RETURNED_EVENT, { detail: pageId }));
}

/** Follows a page's room back after it closed (announceRoomReturn). */
export function onRoomReturn(pageId: string, listener: () => void): () => void {
  if (typeof window === "undefined") return () => {};
  const back = (event: Event) => {
    if ((event as CustomEvent<string>).detail === pageId) listener();
  };
  window.addEventListener(ROOM_RETURNED_EVENT, back);
  return () => window.removeEventListener(ROOM_RETURNED_EVENT, back);
}

/**
 * The page's connection closed and reads its comments once it is back
 * (announceRoomReturn). The call returned says it no longer waits —
 * back, or not coming back after all, when a return that waited reads now.
 */
export function awaitCommentReconnect(pageId: string): () => void {
  reconnecting.set(pageId, (reconnecting.get(pageId) ?? 0) + 1);
  let waiting = true;
  return () => {
    if (!waiting) return;
    waiting = false;
    const left = (reconnecting.get(pageId) ?? 1) - 1;
    if (left > 0) {
      reconnecting.set(pageId, left);
      return;
    }
    reconnecting.delete(pageId);
    if (typeof window !== "undefined") window.dispatchEvent(new CustomEvent(COMMENT_RECONNECT_GIVEN_UP_EVENT, { detail: pageId }));
  };
}

/**
 * The person's "알림 받기" of a page, a row page or a database changed: which
 * discussions they follow, and so which are unread to them, goes by it as it
 * is now (comment_thread_following.go, Notion's, seen 2026-11-25), so every
 * open page's comments are read again.
 */
export function announceCommentFollowingChange() {
  if (typeof window === "undefined") return;
  window.dispatchEvent(new CustomEvent(COMMENT_FOLLOWING_CHANGED_EVENT));
}

/** Only active comment readers follow browser and room recovery, and their own following. */
export function onCommentRefresh(pageId: string, refresh: () => void): () => void {
  if (typeof window === "undefined" || typeof document === "undefined") return () => {};
  // A return waiting for the reconnect's read.
  let waiting: number | undefined;
  const stopWaiting = () => {
    if (waiting === undefined) return;
    window.clearTimeout(waiting);
    waiting = undefined;
  };
  const read = () => {
    stopWaiting();
    refresh();
  };
  const back = () => {
    if (!reconnecting.has(pageId)) read();
    else if (waiting === undefined) waiting = window.setTimeout(read, RECONNECT_READ_WAIT_MS);
  };
  const resume = () => { if (document.visibilityState === "visible") back(); };
  const reconnect = (event: Event) => {
    if ((event as CustomEvent<string>).detail === pageId) read();
  };
  const givenUp = (event: Event) => {
    if ((event as CustomEvent<string>).detail === pageId && waiting !== undefined) read();
  };
  const stopReturn = onWindowReturn(back);
  window.addEventListener("online", resume);
  window.addEventListener(ROOM_RETURNED_EVENT, reconnect);
  window.addEventListener(COMMENT_RECONNECT_GIVEN_UP_EVENT, givenUp);
  window.addEventListener(COMMENT_FOLLOWING_CHANGED_EVENT, read);
  return () => {
    stopWaiting();
    stopReturn();
    window.removeEventListener("online", resume);
    window.removeEventListener(ROOM_RETURNED_EVENT, reconnect);
    window.removeEventListener(COMMENT_RECONNECT_GIVEN_UP_EVENT, givenUp);
    window.removeEventListener(COMMENT_FOLLOWING_CHANGED_EVENT, read);
  };
}
