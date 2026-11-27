/**
 * A page's lock this tab changed, for everyone or for the person alone. The
 * server closes the page's room so its editors reconnect under the new lock
 * (page_lock_policy.go), as it closes the sockets of a page this tab moved to
 * or out of the trash, which turned it read-only or editable
 * (ygo_access_recheck.go); this tab already shows the change, so its own
 * editor's reconnect is no return from being away. It doesn't ask what
 * changed in the workspace, read the room's storage state or the page's
 * comments again, and catches up on the page with one read of what changed
 * since it showed it rather than the whole page (2026-11-13: one lock pressed
 * sent 12 to 19 requests; Notion's sends one). Locking for everyone ends every
 * personal unlock, so the lock isn't read back either.
 */

// Long enough for the change to reach the server and the room to open again.
const FLIP_WAIT_MS = 15_000;

const flips = new Map<string, number>();

/** Notes that this tab changed the page's lock. */
export function noteOwnLockFlip(pageId: string) {
  flips.set(pageId, Date.now() + FLIP_WAIT_MS);
}

/** Whether this tab changed the page's lock a moment ago. */
export function ownLockFlip(pageId: string): boolean {
  const until = flips.get(pageId);
  if (until === undefined) return false;
  if (until >= Date.now()) return true;
  flips.delete(pageId);
  return false;
}

/** Whether this tab changed the page's lock a moment ago; the room that opened again under it ends the note. */
export function takeOwnLockFlip(pageId: string): boolean {
  const own = ownLockFlip(pageId);
  flips.delete(pageId);
  return own;
}
