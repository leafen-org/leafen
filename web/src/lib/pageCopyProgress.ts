import { useSyncExternalStore } from "react";

// A copy being made opens at once, as Notion's does (seen 2026-11-05: its page
// after 1.6 seconds, the top bar counting, "duplicated" for a moment at the end):
// what is known of it, by its page. The notice is for the tab that asked (seen
// the same day: it does not come back with a reload, while the count does). A
// quick copy says nothing at all (seen the same day).

const copies = new Map<string, number>();
const asked = new Set<string>();
// Pages whose body's own databases are copied into them after the rest, for
// a copy this tab asked for (lib/pageDatabaseCopies.ts).
const filling = new Set<string>();
// Copies asked for here that have not lasted long enough to count in the bar.
const waiting = new Map<string, number>();
const listeners = new Set<() => void>();
const noCopy = -1;
const COUNTS_AFTER_MS = 2_500;
const DONE_SHOWN_MS = 3_000;

/** The percent that says the copy is done; those under way stop short of it. */
export const pageCopyDone = 100;

/** The followed state of a page's own access — a hold on it, a copy's count — heard by every screen that waits on it (followAcrossTabs). */
export const pageAccessTopic = (userId: string, pageId: string) => `page-access:${userId}:${pageId}`;

// Copies' tops read again here for their body written while their databases
// are still to come (page_duplication_top_first.go pageCopyTopWritten).
const topsReadAgain = new Set<string>();

/**
 * Whether a screen held by a copy reads the page again now: once, when the
 * copy's top says its body is written before the copy is whole, so its text
 * shows then and not only after its databases (2026-11-15, NAS condition: a
 * top with a database of its own, written at 4.5 s, showed its text at 38 s).
 */
export function copyTopReadAgain(pageId: string, written: boolean | undefined): boolean {
  if (!written || topsReadAgain.has(pageId)) return false;
  topsReadAgain.add(pageId);
  return true;
}

function changed() {
  for (const listener of listeners) listener();
}

function stopWaiting(pageId: string) {
  window.clearTimeout(waiting.get(pageId));
  waiting.delete(pageId);
}

export function setPageCopyProgress(pageId: string, percent: number | null): void {
  if (percent === null) {
    stopWaiting(pageId);
    copies.delete(pageId);
    asked.delete(pageId);
  } else if (!waiting.has(pageId) && copies.get(pageId) !== pageCopyDone) copies.set(pageId, percent);
  changed();
}

/** A copy this tab asked for, which it says is under way until it ends. */
export function askPageCopy(pageId: string): void {
  asked.add(pageId);
  waiting.set(pageId, window.setTimeout(() => {
    waiting.delete(pageId);
    setPageCopyProgress(pageId, 0);
  }, COUNTS_AFTER_MS));
  changed();
}

/** The copy is whole: says so for a moment, if it was being shown at all. */
export function endPageCopy(pageId: string): void {
  stopWaiting(pageId);
  asked.delete(pageId);
  if (!copies.has(pageId) || copies.get(pageId) === pageCopyDone) {
    changed();
    return;
  }
  copies.set(pageId, pageCopyDone);
  changed();
  window.setTimeout(() => {
    if (copies.get(pageId) === pageCopyDone) setPageCopyProgress(pageId, null);
  }, DONE_SHOWN_MS);
}

export function pageCopyAsked(pageId: string): boolean {
  return asked.has(pageId);
}

export function pageCopyUnderWay(pageId: string): boolean {
  return waiting.has(pageId) || (copies.has(pageId) && copies.get(pageId) !== pageCopyDone);
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
}

/** How far the page's copy has come, in percent; null if none is being made. */
export function usePageCopyProgress(pageId: string | null | undefined): number | null {
  const percent = useSyncExternalStore(subscribe, () => (pageId ? copies.get(pageId) ?? noCopy : noCopy), () => noCopy);
  return percent === noCopy ? null : percent;
}

/** Says a page's own databases are being copied into it for a copy asked for here, or no longer. */
export function setPageFilling(pageId: string, on: boolean): void {
  if (on === filling.has(pageId)) return;
  if (on) filling.add(pageId);
  else filling.delete(pageId);
  changed();
}

/** Whether the page's own databases are being copied into it for a copy asked for here. */
export function usePageFilling(pageId: string | null | undefined): boolean {
  return useSyncExternalStore(subscribe, () => Boolean(pageId) && filling.has(pageId!), () => false);
}

/** Whether a copy asked for here is being made, a page's or its databases'. */
export function useAnyPageCopy(): boolean {
  return useSyncExternalStore(subscribe, () => asked.size > 0 || filling.size > 0, () => false);
}
