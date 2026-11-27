// A peek drawn beside the page rather than over it (DatabaseRowPage, PagePeek).
export const SIDE_PEEK = '[data-mode="side"]:is([data-row-peek-panel], [data-page-peek])';
// A field keeps its own Escape: a search, a cell's editor, the page's text.
const FIELD = 'textarea, select, input:not([type="checkbox"], [type="radio"]), [contenteditable="true"]';

/**
 * Whether an Escape pressed with the focus on `active` — a control of the
 * page that no layer holds (`inLayer` false) — is for the side peek on top,
 * `top`, as one pressed on the page itself is. A side peek stands beside the
 * page, which keeps the focus, and Notion's closes on an Escape nothing on
 * the page beside it takes (seen 2026-11-25 in Chrome at 1440, dark, in two
 * tabs at different times, judged after the key by the screen and one
 * letter: a row's box pressed twice, the row chosen and let go, the focus
 * left on the box, closed the peek; so does the Escape that lets a table's
 * rows go, useTableRowSelectionKeys).
 */
export function escapeForPeekBeside(top: Element | null | undefined, active: Element | null, inLayer: boolean) {
  return !inLayer && active instanceof HTMLElement && !active.isContentEditable && !active.matches(FIELD) && Boolean(top?.matches(SIDE_PEEK));
}
