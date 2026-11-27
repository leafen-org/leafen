import { useSyncExternalStore } from "react";
import { personValue, storePersonValue } from "@/lib/browserStorage";

/**
 * The inbox's width, as Notion's (390px, seen 2026-10-07), which its edge
 * drags wider or narrower (arrow keys by 10px). Pinned, it docks beside the
 * page, which moves over to make room; otherwise it lies over it.
 */
export const INBOX_WIDTH = 390;
export const INBOX_MIN_WIDTH = 300;
export const INBOX_MAX_WIDTH = 640;
export const INBOX_WIDTH_STEP = 10;

const INBOX_PINNED_KEY = "leafen:inbox-pinned:v1";
const INBOX_WIDTH_KEY = "leafen:inbox-width:v1";

export function clampInboxWidth(width: number) {
  return Math.round(Math.min(INBOX_MAX_WIDTH, Math.max(INBOX_MIN_WIDTH, width)));
}

/** Whether this person keeps the inbox pinned, kept in this browser. */
export function inboxPinned(userId: string) {
  return personValue(INBOX_PINNED_KEY, { userId }) === "true";
}

export function storeInboxPinned(userId: string, pinned: boolean) {
  storePersonValue(INBOX_PINNED_KEY, pinned ? "true" : null, { userId });
}

/** The width this person last gave the inbox in this browser. */
export function inboxWidth(userId: string) {
  const stored = Number(personValue(INBOX_WIDTH_KEY, { userId }));
  return Number.isFinite(stored) && stored > 0 ? clampInboxWidth(stored) : INBOX_WIDTH;
}

export function storeInboxWidth(userId: string, width: number) {
  storePersonValue(INBOX_WIDTH_KEY, width === INBOX_WIDTH ? null : String(clampInboxWidth(width)), { userId });
}

type InboxDock = { docked: boolean; width: number };

let dock: InboxDock = { docked: false, width: INBOX_WIDTH };
const listeners = new Set<() => void>();

/** Whether the open inbox docks beside the page, and how wide; the shell makes room. */
export function setInboxDock(next: InboxDock) {
  if (dock.docked === next.docked && dock.width === next.width) return;
  dock = next;
  for (const listener of listeners) listener();
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

const UNDOCKED: InboxDock = { docked: false, width: INBOX_WIDTH };

export function useInboxDock() {
  return useSyncExternalStore(subscribe, () => dock, () => UNDOCKED);
}
