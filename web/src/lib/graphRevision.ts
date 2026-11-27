/**
 * Moves the position a page's graph is known to be at to a change the page
 * just saved, when that change follows the position: a change another
 * writer (a person, an automation) made in between isn't shown yet, so the
 * position stays where the page is, and the room's notice of that change
 * still finds the page behind it and reads it again (2026-11-02: an
 * automation's value hid behind the next edit until reload).
 */
export function advanceGraphRevision(revision: { current: number }, cursor: number) {
  if (cursor === revision.current + 1) revision.current = cursor;
}
