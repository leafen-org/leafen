import type { Page } from "@/types";

/**
 * Whether a page's lock holds what its view shows, for someone it is locked
 * for. A locked page's body takes no edits — its databases' own lock and
 * their rows' own pages aside (rowPageUnderPageLock.ts) — and a full-page
 * database's lock is its database's, which keeps its rows editable. One of
 * its inline databases opened alone (⤢) is a page of its own: it takes
 * edits, its bar without the page's "잠금", its own lock still holding it,
 * as Notion's (seen 2026-11-16 in Chrome, two tabs: opened alone from a
 * locked page, a cell and its name each took a typed letter, "새로 만들기"
 * and "+ 새 페이지" were there, and the bar showed no "잠금").
 */
export function pageLockHoldsBody(page: Pick<Page, "kind">, lockedForMe: boolean, openedAloneBlockId?: string) {
  return lockedForMe && page.kind !== "database" && !openedAloneBlockId;
}
