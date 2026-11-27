import { useSyncExternalStore } from "react";
import type { Page } from "@/types";
import { afterNextPaint } from "./afterPress";

/**
 * The pages' edit times known after the page list: a page's body edited
 * here — its words typed, its blocks made, moved or removed
 * (usePageBodyEditing) — shows it last edited now, by this person, and a
 * list read again that changed a page only in its edit time shows that time
 * (lib/bootstrapSameParts.ts), wherever a page's edit time shows: the top
 * bar's "편집", the sidebar's sections in "최종 편집 일시" order, Home, the
 * library, search, the updates, the menus' feet.
 *
 * The times are kept here, apart from the page list, which stays the same
 * list: a new list, the page's edit time in it, made the app's every page
 * list anew (the pages, the mentions, the moves, the tree), and React looked
 * through the whole page for what reads them (2026-11-15: some 6 ms after
 * each Enter in a 1,031-block page at CPU ×2). What shows an edit time reads
 * it here and alone draws again when it changes (usePageEditedAt,
 * usePagesWithEditTimes, useEditTimesChange).
 *
 * An edit made here is kept once the keys and presses have rested a second,
 * behind the next painted frame: what shows it — a sidebar section in edit
 * order sorts again — is not drawn between the keys of a quick run.
 */
type EditTime = Pick<Page, "updatedAt" | "lastEditedBy">;
export type PageEditTime = Pick<Page, "id" | "updatedAt" | "lastEditedBy">;

// The edit times later than the list's own, by page.
const times = new Map<string, PageEditTime>();
let version = 0;
const listeners = new Set<() => void>();
const subscribe = (listener: () => void) => {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
};
const subscribeNone = () => () => undefined;

const later = (time: string | undefined, than: string | undefined) =>
  Boolean(time) && (!than || Date.parse(time as string) > Date.parse(than));

function keep(edit: PageEditTime): boolean {
  if (!later(edit.updatedAt, times.get(edit.id)?.updatedAt)) return false;
  times.set(edit.id, edit);
  return true;
}

function changed() {
  version += 1;
  for (const listener of [...listeners]) listener();
}

/** Keeps a list read's edit times for the pages it lists as shown (lib/bootstrapSameParts.ts). */
export function notePageEditTimes(edits: readonly PageEditTime[]) {
  let kept = false;
  for (const edit of edits) kept = keep(edit) || kept;
  if (kept) changed();
}

// How long the keys and presses rest before an edit made here is kept.
const EDITED_SHOWN_AFTER_REST_MS = 1000;
const heard = new Map<string, PageEditTime>();
let quiet: ReturnType<typeof setTimeout> | undefined;
let painted: (() => void) | null = null;

function show() {
  quiet = undefined;
  painted = afterNextPaint(() => {
    painted = null;
    window.removeEventListener("keydown", wait, { capture: true });
    window.removeEventListener("pointerdown", wait, { capture: true });
    const edits = [...heard.values()];
    heard.clear();
    notePageEditTimes(edits);
  });
}

function wait() {
  if (!heard.size) return;
  painted?.(); painted = null;
  clearTimeout(quiet);
  quiet = setTimeout(show, EDITED_SHOWN_AFTER_REST_MS);
}

/** Tells what shows edit times that `edit.id`'s body was edited here now. */
export function tellPageEditedHere(edit: PageEditTime) {
  if (!heard.size) {
    window.addEventListener("keydown", wait, { capture: true, passive: true });
    window.addEventListener("pointerdown", wait, { capture: true, passive: true });
  }
  heard.set(edit.id, edit);
  wait();
}

/** A page's last edit: the list's, or a later one known here. */
export function pageEditTime(page: { id?: string } & Partial<EditTime>): Partial<EditTime> {
  const known = page.id ? times.get(page.id) : undefined;
  return known && later(known.updatedAt, page.updatedAt)
    ? { updatedAt: known.updatedAt, lastEditedBy: known.lastEditedBy || page.lastEditedBy }
    : page;
}

/** When a page was last edited, drawn again when that changes. */
export function usePageEditedAt(page: Pick<Page, "id" | "updatedAt">): string {
  return useSyncExternalStore(subscribe, () => pageEditTime(page).updatedAt ?? "");
}

// Each list with its later edit times, for the edit times it was last given.
type EditTimed = { id: string } & EditTime;
const timedLists = new WeakMap<readonly EditTimed[], { version: number; pages: readonly EditTimed[] }>();

/** The pages, each with its later edit time known here: the same list when none has one. */
export function withEditTimes<L extends readonly EditTimed[]>(pages: L): L {
  if (!times.size) return pages;
  const last = timedLists.get(pages);
  if (last?.version === version) return last.pages as L;
  let timed: EditTimed[] | null = null;
  for (let index = 0; index < pages.length; index += 1) {
    const page = pages[index];
    const known = times.get(page.id);
    if (!known || !later(known.updatedAt, page.updatedAt)) continue;
    timed ??= pages.slice();
    // The page as given before, when its time is the same: what draws it stays.
    const before = last?.pages[index];
    timed[index] = before && before.id === page.id && before.updatedAt === known.updatedAt ? before
      : { ...page, updatedAt: known.updatedAt, lastEditedBy: known.lastEditedBy || page.lastEditedBy };
  }
  // The list as given before, when every page of it is.
  const same = timed && last && last.pages !== pages && last.pages.length === timed.length
    && timed.every((page, index) => page === last.pages[index]);
  const result = same ? last.pages : timed ?? pages;
  timedLists.set(pages, { version, pages: result });
  return result as L;
}

/** The pages with their later edit times, drawn again when those change; `enabled` false leaves them. */
export function usePagesWithEditTimes<L extends readonly EditTimed[] | undefined>(pages: L, enabled = true): L {
  const on = enabled && pages !== undefined;
  return useSyncExternalStore(on ? subscribe : subscribeNone, () => (on && pages ? withEditTimes(pages) as L : pages));
}

/** A count of the edit times known here, which draws again as they change; 0 while `enabled` is false. */
export function useEditTimesChange(enabled: boolean): number {
  return useSyncExternalStore(enabled ? subscribe : subscribeNone, () => (enabled ? version : 0));
}

/** A page's later edit time known here, "" for none, drawn again as it changes; none while `pageId` is "". */
export function useEditTimeKnownHere(pageId: string): string {
  return useSyncExternalStore(pageId ? subscribe : subscribeNone, () => (pageId ? times.get(pageId)?.updatedAt ?? "" : ""));
}
