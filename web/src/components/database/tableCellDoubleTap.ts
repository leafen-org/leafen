// How long after a finger's tap a second one is its double tap (Android's and
// Chrome's double-tap time), and how far a finger may move and still tap.
const DOUBLE_TAP_MS = 300;
const TAP_SLOP = 10;
const SECOND_TAP_REACH = 40;

// The last tap on a title or text cell: the cell, when the finger lifted and where.
let lastTap: { cell: Element; at: number; x: number; y: number } | null = null;
let pressed: { cell: Element; x: number; y: number } | null = null;

function touchStarted(event: TouchEvent) {
  const cell = event.currentTarget as HTMLElement, touch = event.touches[0];
  const tap = lastTap;
  lastTap = null;
  pressed = null;
  if (!touch || event.touches.length > 1 || cell.dataset.cellTyping !== "text") return;
  if (tap?.cell === cell && performance.now() - tap.at <= DOUBLE_TAP_MS
    && Math.hypot(touch.clientX - tap.x, touch.clientY - tap.y) <= SECOND_TAP_REACH && event.cancelable) {
    event.preventDefault();
    return;
  }
  if (!(event.target instanceof Element && event.target.closest("button, a[href]"))) pressed = { cell, x: touch.clientX, y: touch.clientY };
}

function touchEnded(event: TouchEvent) {
  const touch = event.changedTouches[0], press = pressed;
  pressed = null;
  if (press?.cell === event.currentTarget && touch && Math.hypot(touch.clientX - press.x, touch.clientY - press.y) <= TAP_SLOP) {
    lastTap = { cell: press.cell, at: performance.now(), x: touch.clientX, y: touch.clientY };
  }
}

/**
 * A ref for a table's cell (TableGridRow, TimelineTableCell): on a title or
 * text cell a finger's quick second tap — a double tap — does what one tap
 * does, the cell written with its caret at the end of its words, the keyboard
 * up, no word chosen and no row menu, as Notion's phone (seen 2026-11-24 in
 * Chrome on a Galaxy A90 at 411, a title's cell and a text's, in two ways —
 * taps 90 ms apart from the device's input and touches 120 ms apart from
 * outside the page, in two tabs, nothing in the page before them; a letter
 * typed then went at the words' end). Chrome read the second tap as a double
 * tap on the editor the first had woken: it chose the word there, and the
 * context menu that choice sends opened the row's menu, choosing the row and
 * dropping the keyboard. So the second touch is let go before Chrome reads it,
 * its touchstart cancelled. The listener that cancels it stands on the cell
 * from the start: Chrome sends a touch no listener can cancel where none stood
 * as it came, and one set at the first tap's lift was still unknown to it a
 * frame later half the time (2026-11-24, the A90: the second touch came 44 ms
 * on, uncancellable). A touch begun there that is no second tap goes on as
 * ever, a scroll too; panning a table from its title or text cells waits for
 * this short look before it starts.
 */
export function secondTapStaysATap(cell: HTMLElement | null) {
  if (!cell || cell.dataset.cellTyping !== "text") return;
  cell.addEventListener("touchstart", touchStarted, { passive: false });
  cell.addEventListener("touchend", touchEnded, { passive: true });
  return () => {
    cell.removeEventListener("touchstart", touchStarted);
    cell.removeEventListener("touchend", touchEnded);
  };
}
