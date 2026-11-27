// How far in from the window's sides a cell's value box stands at the nearest.
const WINDOW_GAP = 12;

/**
 * How far right (negative: left) the box a table cell's value is written in,
 * standing from `left` and `width` wide, moves to stand inside the window,
 * 12px in from its right edge, as Notion's (seen 2026-11-02: a number's box
 * for a cell running past the window's end sat at 1028..1268), and 12px in
 * from its left edge, where a table rolled sideways has its cell running
 * past the window's start, as Notion's phone (2026-11-25, Safari on an
 * iPhone at 402, at two times in fresh tabs: number cells from -38, -52 and
 * -84, each 240 box at 12, the table left where it was rolled). A box
 * too wide for both keeps its left side in sight. Its cell's box as it opens
 * (fitCellEditor) and the box raised over its view's window
 * (heldEditorOverWindow) both stand by it.
 */
export function cellBoxIntoWindow(left: number, width: number, windowWidth = document.documentElement.clientWidth) {
  return Math.max(Math.min(0, windowWidth - WINDOW_GAP - (left + width)), WINDOW_GAP - left);
}
