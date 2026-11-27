import { useState, type PointerEvent } from "react";
import type { ListState } from "react-stately";

// Hover moves a highlight only when the pointer really moves, as in Notion
// (2026-10-16: a slash menu opened under a pointer left where a click put
// the caret highlighted the row under it, and Enter picked that row). A menu
// that opens, filters or scrolls under a still pointer gets the browser's
// enter and move events too; those keep the pointer where it was.

let last: { x: number; y: number } | null = null;
let movedEvent: Event | null = null;
// Where the pointer was before its last real move.
let movedFrom: { x: number; y: number } | null = null;
let watching = false;

// A wheel rolls the page under a still pointer, and Notion's hover tools go
// with it: no property of a database page shows its handle after one, not
// even the one now under the pointer, until the pointer moves (seen
// 2026-11-16 in Chrome at 1440 in a side peek, in two tabs at different
// times: after the wheel, twice, no line showed one; a 2px move brought it
// on the line under the pointer). A table's rows keep their "+ ⋮⋮ ☐" away
// so too, while the row now under the pointer shows its "열기" and a board's
// card its tools, as :hover does (seen the same day at 1440 in two tabs).
// The root carries `data-pointer-still` meanwhile for the tools' styles; it
// names few elements, so marking it restyles little.
let wheeled = false;
function wheel(event: WheelEvent) {
  if (wheeled || event.ctrlKey) return;
  wheeled = true;
  document.documentElement.dataset.pointerStill = "";
}

function remember(event: globalThis.PointerEvent) {
  const moved = event.type === "pointermove" && last !== null && (last.x !== event.clientX || last.y !== event.clientY);
  if (moved) { movedEvent = event; movedFrom = last; }
  if (wheeled && (moved || event.type === "pointerdown")) {
    wheeled = false;
    delete document.documentElement.dataset.pointerStill;
  }
  last = { x: event.clientX, y: event.clientY };
}

/** Starts telling a pointer that moves from one the page moved under. */
export function watchPointerMoves() {
  if (watching || typeof window === "undefined") return;
  watching = true;
  // On the window, before anything below it reads the event.
  window.addEventListener("pointerdown", remember, { capture: true, passive: true });
  window.addEventListener("pointermove", remember, { capture: true, passive: true });
  window.addEventListener("wheel", wheel, { capture: true, passive: true });
}
watchPointerMoves();

/** Where the pointer last was in the window, as its last move or press said. */
export function pointerAt(): { x: number; y: number } | null {
  watchPointerMoves();
  return last;
}

/** Whether this move took the pointer somewhere new (never a touch). */
export function pointerMoved(event: PointerEvent<Element>) {
  watchPointerMoves();
  return event.pointerType !== "touch" && event.nativeEvent === movedEvent;
}

/** Whether the pointer came into this element only as the page moved under
 * it, still where it last was (a menu opening, filtering or scrolling
 * there). The browser tells such an enter before the move it makes up. */
export function pointerStill(event: Pick<globalThis.PointerEvent, "pointerType" | "clientX" | "clientY">) {
  watchPointerMoves();
  return event.pointerType !== "touch" && last !== null && last.x === event.clientX && last.y === event.clientY;
}

/**
 * Whether the pointer has moved onto an element (or pressed it) and not left
 * it since — never one the page brought under a still pointer, which the
 * browser's own :hover counts — with the handlers that tell it, spread on the
 * element. A block's hover tools wait for this and :hover both, as Notion's
 * do (CodeBlock): a page scrolled under a still pointer tells no leave.
 */
export function usePointedByMove() {
  const [pointed, setPointed] = useState(false);
  return [pointed, {
    onPointerMove: (event: PointerEvent<Element>) => { if (!pointed && pointerMoved(event)) setPointed(true); },
    onPointerDown: (event: PointerEvent<Element>) => { if (!pointed && event.pointerType !== "touch") setPointed(true); },
    onPointerLeave: () => { if (pointed) setPointed(false); },
  }] as const;
}

// After the arrow keys move a list's highlight, Notion's pointer takes it
// back only as it goes into another row (seen 2026-11-14 in the "/" menu,
// search, the relation, select, link, page and people lists, the property
// type menus and the emoji grid: ↓↓ ringed a row; the pointer's first move left the ring where it
// was, only grounding the row it landed on; moving on into the next row took
// the highlight there, the ring gone, and ↓ went on from it). A list's keys
// say so here (`keysMovedHighlight`); its first pointer move after them
// marks the row the pointer is on, and the highlight moves only once the
// pointer is in another row, or comes back into the list. A list that came
// up under a still pointer is held so too: the pointer's moves inside the
// row it came up on take nothing (seen 2026-11-14 at 1440 in the "/" menu
// and a row page's property type list: its row only shaded, moved within it
// still, ↓ went on from the lit row or lit the first).
const keysHeld = new WeakMap<Element, { row?: string | null }>();
const pointerCameIn = new WeakSet<Element>();

function hold(list: Element, held: { row?: string | null }) {
  keysHeld.set(list, held);
  // Out of the list and back, the pointer comes into a row again.
  list.addEventListener("pointerleave", () => { if (keysHeld.get(list) === held) held.row = null; }, { once: true });
}

/** Says the keys just moved this list's highlight: the pointer leaves it there until it moves into another row. */
export function keysMovedHighlight(list: Element | null | undefined) {
  if (list) keysHeld.set(list, {});
}

/** The row (its `data-key`) the pointer's move over a list (the handler's
 * element) takes the highlight to, or null: after the keys only once it
 * moves into another row, and only a row `canTake` allows. A list keeping
 * its own highlight (a type menu, a grid) moves it there. */
export function pointerTakenRow(event: PointerEvent<HTMLElement>, canTake: (key: string) => boolean = () => true) {
  if (!pointerMoved(event)) return null;
  const list = event.currentTarget;
  const row = event.target instanceof Element ? event.target.closest<HTMLElement>("[data-key]") : null;
  const key = row && list.contains(row) ? row.dataset.key ?? null : null;
  // Its first move starting inside it: it came up under the pointer, held on the row there.
  // (A list may be drawn as its rows alone, `display: contents`, with no box of its own.)
  if (!pointerCameIn.has(list)) {
    pointerCameIn.add(list);
    const from = movedFrom, at = from ? document.elementFromPoint(from.x, from.y) : null;
    const under = at?.closest<HTMLElement>("[data-key]");
    if (at && list.contains(at) && !keysHeld.has(list)) hold(list, { row: under && list.contains(under) ? under.dataset.key ?? null : null });
  }
  const held = keysHeld.get(list);
  if (held) {
    if (held.row === undefined) {
      held.row = key;
      hold(list, held);
      return null;
    }
    if (key === held.row) return null;
    held.row = key;
  }
  if (key === null || !canTake(key)) return null;
  keysHeld.delete(list);
  return key;
}

/** A list's move handler that highlights the row the pointer moved onto,
 * after the keys only once it moves into another row; `onTake` hears when
 * the pointer takes the highlight. */
export function movedPointerHover<T>(state: ListState<T>, onTake?: () => void) {
  watchPointerMoves();
  return (event: PointerEvent<HTMLElement>) => {
    const key = pointerTakenRow(event, (row) => Boolean(state.collection.getItem(row)) && !state.disabledKeys.has(row));
    if (key === null) return;
    const manager = state.selectionManager;
    onTake?.();
    if (manager.isFocused && manager.focusedKey === key) return;
    manager.setFocused(true);
    manager.setFocusedKey(key);
  };
}
