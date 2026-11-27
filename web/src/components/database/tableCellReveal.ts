import { inlineSign } from "@/lib/readingDirection";

// How far in from the table's end Notion stands a cell it rolls the table to (from its start too, a phone's opened cell).
const END_ROOM = 64;

/**
 * Chooses a table's cell from the keys, the table rolled sideways as Notion's
 * (seen 2026-11-16 at 1440 in Chrome, twice in a table of 16 columns, and by
 * the OP-03-a audit in another tab): for a cell starting in the table's last
 * 64px or past its end, until its end stands 64px in from it (in a table
 * ending at 1440, a 200px column from 1454 to 1176–1375, then the next from
 * 1376 to 1177–1376; one from 1254 or 1349, partly shown, not at all); for one
 * wholly before its start, until its start meets it (the title at 269 of a
 * table from 270; one at 177–376 not at all). Frozen columns are the start of
 * what rolls, and a frozen cell rolls nothing. Up and down, the page goes as
 * the browser's own focus took it: a cell not wholly in view comes to the
 * middle.
 */
export function chooseCellByKey(cell: HTMLElement) {
  const scroller = cell.closest<HTMLElement>("[data-table-scroll]");
  cell.focus({ preventScroll: true });
  if (!wholeInViewDown(cell)) {
    const along = scroller?.scrollLeft;
    cell.scrollIntoView({ block: "center", inline: "nearest" });
    if (scroller && along !== undefined) scroller.scrollLeft = along;
  }
  if (!scroller || cell.dataset.frozen === "true") return;
  const { at, roomStart, roomEnd, cellStart, cellEnd, roll } = alongRoom(scroller, cell);
  if (at(cellStart) >= at(roomEnd) - END_ROOM) roll(at(cellEnd) - (at(roomEnd) - END_ROOM));
  else if (at(cellEnd) <= at(roomStart)) roll(at(cellStart) - (at(roomStart) - 1));
}

/**
 * Rolls a phone's table sideways as a finger opens a cell's value box in it,
 * as Notion's phone does (2026-11-25, Safari on an iPhone at 402, number and
 * text cells, at 20:3x and 20:5x): a cell ending in the table's first 64px
 * or before it, its 1px end line aside, rolls until its start stands 64px in
 * from the table's start (in a table from -6, a 200px cell from -141 to -195
 * to 58; from -140, its end 59 without its line, not at all, nor from -38 to
 * -130); one starting in its last 64px or past its end rolls
 * until its end, its line aside, stands 64px in from the table's end (in a
 * table ending at 396, a cell from 332, 333 or 340 to 133–333; from 331 or
 * 320 not at all). A cell showing more stays where it is, its box kept inside
 * the window (cellBoxInWindow.ts). Frozen columns are the start of what rolls,
 * and a frozen cell rolls nothing.
 */
export function rollTableToOpenedCell(cell: HTMLElement) {
  const scroller = cell.closest<HTMLElement>("[data-table-scroll]");
  if (!scroller || cell.dataset.frozen === "true") return;
  const { at, sign, roomStart, roomEnd, cellStart, cellEnd, roll } = alongRoom(scroller, cell);
  const lineEnd = cellEnd - sign;
  if (at(cellStart) >= at(roomEnd) - END_ROOM) roll(at(lineEnd) - (at(roomEnd) - END_ROOM));
  else if (at(lineEnd) <= at(roomStart) + END_ROOM) roll(at(cellStart) - (at(roomStart) + END_ROOM));
}

/**
 * Along the reading direction: where a table's room starts (past any frozen
 * columns) and ends, where its cell starts and ends, and how to roll the
 * table on by so much.
 */
function alongRoom(scroller: HTMLElement, cell: HTMLElement) {
  const sign = inlineSign(scroller);
  const [left, right] = tableRoom(scroller);
  const frozen = cell.parentElement?.querySelector<HTMLElement>(":scope > [data-frozen-edge=\"true\"]")?.getBoundingClientRect();
  const box = cell.getBoundingClientRect();
  return {
    sign,
    at: (x: number) => x * sign,
    roomStart: sign > 0 ? Math.max(left, frozen?.right ?? left) : Math.min(right, frozen?.left ?? right),
    roomEnd: sign > 0 ? right : left,
    cellStart: sign > 0 ? box.left : box.right,
    cellEnd: sign > 0 ? box.right : box.left,
    roll: (by: number) => { if (by) scroller.scrollLeft += by * sign; },
  };
}

/**
 * Where a table's room starts and ends across the window: its scroller's
 * inside. A table on its database's own page scrolls sideways in the page
 * itself in Notion, whose box takes in the page's own up-and-down bar (seen
 * 2026-11-16 at 1440, twice in different tabs: a page box 270–1440 with a
 * 15px bar, the next column rolled to 1177–1376, its end 64 in from 1440, not
 * from the bar at 1425); here the table has its own scroller inside the
 * page's, ending at the bar, so where it runs to the page's edge on a side,
 * its room runs on to the page's box there, over the bar.
 */
function tableRoom(scroller: HTMLElement): [number, number] {
  const box = scroller.getBoundingClientRect();
  let left = box.left + scroller.clientLeft, right = left + scroller.clientWidth;
  const page = scroller.closest<HTMLElement>('[data-database-page="true"]')?.closest<HTMLElement>("[data-page-scroll]")
    ?? null;
  if (!page) return [left, right];
  const outer = page.getBoundingClientRect();
  const inner = outer.left + page.clientLeft;
  if (Math.abs(left - inner) < 1) left = outer.left;
  if (Math.abs(right - (inner + page.clientWidth)) < 1) right = outer.right;
  return [left, right];
}

// Whether a cell shows whole up and down, in the window and in every box
// around it that scrolls up and down.
function wholeInViewDown(cell: HTMLElement) {
  const box = cell.getBoundingClientRect();
  let top = 0, bottom = window.innerHeight;
  for (let node = cell.parentElement; node; node = node.parentElement) {
    if (!/(auto|scroll)/u.test(getComputedStyle(node).overflowY) || node.scrollHeight <= node.clientHeight) continue;
    const room = node.getBoundingClientRect();
    top = Math.max(top, room.top + node.clientTop);
    bottom = Math.min(bottom, room.top + node.clientTop + node.clientHeight);
  }
  return box.top >= top && box.bottom <= bottom;
}
