import type { RowMenuPhoneActions } from "./RowMenuPhoneSheet";

/** Which of a page's menu rows show while its lock holds, or doesn't. */
export type LockedPageRows = {
  move: boolean;
  history: boolean;
  undo: boolean;
  import: boolean;
  wiki: boolean;
  connections: boolean;
  /** The phone sheet's font tiles. */
  fonts: boolean;
  duplicate: boolean;
  /** The lock switch. */
  lock: boolean;
  /** "테마와 레이아웃"'s font, small text, full width, outline and layout rows. */
  pageStyle: boolean;
  offline: boolean;
};

/**
 * A locked page's menus, as Notion's (seen 2026-11-16, Chrome 1440 and
 * Android Chrome 411, on a page, a full-page database and a database's page,
 * each locked, then unlocked for the reader alone): while the lock stops the
 * reader's changes, the "…" and its "더 보기", and the phone's "작업" sheet,
 * keep no move, history, undo, import, wiki, connections or font tiles, and
 * "휴지통으로 이동" shows dimmed. Unlocked for the reader alone, they are the
 * unlocked page's, its lock switch on. No menu row unlocks the page for the
 * reader alone or locks it again: the top bar's "잠금" does.
 */
export function lockedPageRows(lockedForMe: boolean): LockedPageRows {
  const shown = !lockedForMe;
  return { move: shown, history: shown, undo: shown, import: shown, wiki: shown, connections: shown, fonts: shown,
    duplicate: true, lock: true, pageStyle: true, offline: true };
}

/**
 * A page's menus while the reader suggests in it, as Notion's (its code: the
 * rows that change the page check that it isn't being suggested in; seen
 * 2026-11-18 in Chrome at 1440 on a page and on a database's page): no
 * duplicate, move, lock, import, connections, offline, fonts, small text,
 * full width, outline or layout — "테마와 레이아웃" keeps the page and inline
 * comments, and goes where nothing is left. History, undo, wiki, export, the
 * notifications and the trash stay.
 */
export function pageMenuRows(lockedForMe: boolean, suggesting: boolean): LockedPageRows {
  const locked = lockedPageRows(lockedForMe);
  if (!suggesting) return locked;
  return { ...locked, move: false, import: false, connections: false, fonts: false, duplicate: false, lock: false, pageStyle: false, offline: false };
}

/**
 * The phone's "작업" sheet as the lock leaves it (lockedPageRows): a locked
 * database's sheet has no lock switch either, as its "…".
 */
export function phoneActionsUnderLock(actions: RowMenuPhoneActions): RowMenuPhoneActions {
  if (!actions.lockedForMe) return actions;
  const rows = lockedPageRows(true);
  return {
    ...actions,
    move: rows.move ? actions.move : undefined,
    onHistory: rows.history ? actions.onHistory : undefined,
    wiki: rows.wiki ? actions.wiki : undefined,
    connections: rows.connections ? actions.connections : undefined,
    lock: actions.kind === "database" ? undefined : actions.lock,
    trashDisabled: true,
  };
}
