import { pressOnPageRoom } from "../editor/pageBodyFrame";

/**
 * A side peek's press beside it (a row's page or a page's): only a press on
 * the page's room — its frame's empty space beside, above and below its head
 * and blocks, or on a database's own page its table's room under the rows —
 * closes it, once that press's click has done its own work, so the page
 * doesn't move under the pointer while it is down. A press on what the page
 * shows (its title, text and blocks, a database's title, cells, headers and
 * buttons — a row's "열기" turning the peek to that row) or the sidebar leaves
 * it open, as Notion's (seen 2026-11-12: the page's title or another row's
 * title took the caret with the peek open; the margin beside a line, the room
 * under the last block or under a database page's rows closed it). A press on
 * the top bar's room goes by where the caret is (caretStaysOnBar).
 */
export function sidePeekPressBeside(onClose: () => void) {
  return { onOutsidePress: onClose, pressGoesOn: true, closesAfterClick: true, keepsPress: keptBeside } as const;
}

function keptBeside(target: Element): boolean {
  if (target.closest("[data-app-main] > header") && !target.closest(BAR_CONTROL)) return caretStaysOnBar();
  return !pressOnPageRoom(target) && !(target.matches("[data-table-scroll]") && target.closest('[data-database-page="true"]'));
}

const BAR_CONTROL = "button, a[href], input, select, textarea, [role='button'], [role='link'], [contenteditable='true']";

// A page's text: its blocks, and its title, which Notion's page takes with its
// blocks — not the title of the page the side peek shows or a database page's
// name, each a field of its own there.
const PAGE_TEXT = "[data-native-editor-content], [data-page-title-editor]:not([data-database-page-name])";
const SIDE_PEEK = "[data-row-peek-panel], [data-page-peek]";

/**
 * The top bar takes no caret (user-select: none): a press on its room leaves
 * the caret in a field of its own — the side peek's title, a comment, a
 * database's name — and the peek with it, however many times it is pressed;
 * with the caret in a page's text, which that press takes it from, or nowhere
 * (a press on the page's head room drops it), the press closes the peek, as
 * Notion's (seen 2026-11-12: with the caret in the peek's title, its comment,
 * the page's comment or a database page's name, the peek stayed; in the page's
 * text or title, in the peek's text, or after a press on the page's head room,
 * it closed — a press on the sidebar in between changed nothing).
 */
function caretStaysOnBar(): boolean {
  const selection = window.getSelection();
  const node = selection && selection.rangeCount > 0 ? selection.anchorNode : null;
  const at = node instanceof Element ? node : node?.parentElement ?? null;
  if (!at?.closest("[contenteditable='true']")) return false;
  const text = at.closest(PAGE_TEXT);
  return !text || (text.matches("[data-page-title-editor]") && Boolean(text.closest(SIDE_PEEK)));
}
