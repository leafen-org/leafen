import { listScroller } from "./dragReorderLive";
import { letGoCarried } from "./carriedChoice";

type Side = "before" | "after";
type Input = { clientX: number; clientY: number };
type Axis = "vertical" | "horizontal";

/** What a dragged row does while the pointer carries it: its rows move live, or its list shows a line. */
export type PointerReorderMove = {
  place(input: Input): void;
  /** Where the row lands if let go now. */
  landing(): { target: string; side: Side } | null;
  /** Every row back as the list draws it. */
  end(): void;
};

/** A list whose rows a mouse or a pen drags itself (useDragReorder). */
export type PointerReorderList = {
  axis: Axis;
  /** The row a press on this element would drag, if it may move now. */
  rowPressed(target: Element): string | null;
  /** The row's drag starts from where it was pressed: what carries it, and the row (whose list may scroll). */
  begin(source: string, start: Input): { move: PointerReorderMove; row: HTMLElement } | null;
  started(source: string): void;
  /** The drag is over: where the row lands, or null when it was put back (Escape) or lost. */
  ended(source: string, landing: { target: string; side: Side } | null, at: Input): void;
};

/**
 * How far a press goes before its row goes with it, as Notion's property
 * list (seen 2026-11-16 in Chrome: its row stayed put with the pointer 6.5px
 * down and went with it from 7px).
 */
export const DRAG_START = 7;
/** How near a scrolling list's edge the pointer scrolls it, and how fast at most (px a frame); not measured against Notion. */
const EDGE = 40;
const EDGE_SPEED = 14;

/**
 * Said on a row as a drag of it by the pointer ends, moved or put back, as
 * the browser's own drag says "dragend" on its source.
 */
export const POINTER_REORDER_END = "leafenreorderend";

// A press already taken by one list's rows, for a list drawn inside another's
// (or by another drag the pointer follows, pointerDrag).
const taken = new WeakSet<Event>();
export const pressTaken = (event: Event) => taken.has(event);
export const takePress = (event: Event) => { taken.add(event); };

/**
 * Heard as a press goes down, before the browser focuses what it pressed (a
 * handle, a card): the drag it starts takes that focus away again, to the
 * page's body. Notion's handles take no focus: after pressing and dragging a
 * list row's handle a letter typed went nowhere and the page's body had the
 * focus (seen 2026-11-17 00:4x and 03:2x in Chrome, two tabs, read after the
 * letter; the second time the view's tab had the focus before the press, as
 * a pressed view tab keeps it in Notion). A handle or card the press left
 * focused kept showing its tools once the row or card had gone from under
 * the pointer — dropped or put back with ⌘Z — and a view tab given the focus
 * back took the next key. Only a menu or window whose own field or frame had
 * the focus gets it back, so its keys (Escape, the arrows, typing to search)
 * stay with it while its rows are dragged.
 */
export function focusBeforePress(press: PointerEvent) {
  const before = document.activeElement;
  return () => {
    const now = document.activeElement, pressed = press.target;
    // Only what the press itself focused: the body holds every press and is left as it is.
    if (!(now instanceof HTMLElement) || now === document.body || !(pressed instanceof Node) || !now.contains(pressed)) return;
    if (before instanceof HTMLElement && before !== now && before.isConnected && keepsKeys(before, pressed)) before.focus({ preventScroll: true });
    else now.blur();
  };
}

const menuLayer = '[role="dialog"], [role="menu"], [role="listbox"], [data-popover-layer]';
function keepsKeys(before: HTMLElement, pressed: Node) {
  const layer = before.matches(menuLayer) ? before : before.closest(menuLayer);
  return Boolean(layer?.contains(pressed)) && before.matches(`input, textarea, select, ${menuLayer}`);
}

/**
 * A list's rows dragged by the pointer's own events, as Notion drags the
 * rows of its menus' lists, its page's properties and its table's heads
 * (seen 2026-11-16 in Chrome, its lists and its page's properties): a press
 * on a row's grip moves the row once the pointer has gone DRAG_START, and
 * however long the pointer rests the row goes with it — live, the rows
 * making way (dragReorderLive), or as a copy over a list that shows a line
 * (dragReorderLine). Letting go puts it where the list shows it landing —
 * a live list's beside the list too; Escape puts it back and leaves the menu
 * open. The browser's own drag (HTML drag and drop) is not used here: in
 * WebKit, the desktop app's window, its drop was lost at times — let go
 * quickly (seen 2026-11-12) or after the pointer rested still a moment
 * (seen 2026-11-16) — and the list went back. A finger keeps the browser's
 * drag, which its long press starts (useDragReorder).
 */
export function listenPointerReorder(list: () => PointerReorderList) {
  let stop: (() => void) | null = null;
  const pressed = (event: PointerEvent) => {
    // Heard on the window as it starts, before the page's own handlers, which
    // may keep it from the window (a tab's press, as Aria's buttons do); a
    // press one of them took for itself (a column's resize border) drags
    // nothing, as its first move finds (follow).
    if (stop || taken.has(event) || event.defaultPrevented || event.button !== 0 || event.pointerType === "touch"
      || event.ctrlKey || event.metaKey || !(event.target instanceof Element)) return;
    const source = list().rowPressed(event.target);
    if (!source) return;
    taken.add(event);
    stop = follow(list, source, event, focusBeforePress(event), () => { stop = null; });
  };
  window.addEventListener("pointerdown", pressed, true);
  return () => {
    window.removeEventListener("pointerdown", pressed, true);
    stop?.();
  };
}

function follow(list: () => PointerReorderList, source: string, press: PointerEvent, giveFocusBack: () => void, done: () => void) {
  const start: Input = { clientX: press.clientX, clientY: press.clientY };
  const { axis } = list();
  const client = axis === "vertical" ? "clientY" : "clientX";
  const listening = new AbortController();
  const options = { capture: true, signal: listening.signal };
  let moving: PointerReorderMove | null = null;
  let scroller: HTMLElement | null = null;
  let last = start;
  let frame = 0;
  let over = false;

  const finish = (landing: boolean) => {
    if (over) return;
    over = true;
    window.cancelAnimationFrame(frame);
    listening.abort();
    done();
    if (!moving) return;
    const at = moving.landing();
    moving.end();
    list().ended(source, landing ? at : null, last);
  };
  // The click a drag's release makes opens nothing (a head's menu, a handle's).
  const swallowClick = () => {
    const swallow = (event: MouseEvent) => { event.stopImmediatePropagation(); event.preventDefault(); };
    window.addEventListener("click", swallow, { capture: true });
    window.setTimeout(() => window.removeEventListener("click", swallow, { capture: true }), 0);
  };
  // Near a scrolling list's edge the list scrolls, the row going with it.
  const scroll = () => {
    frame = 0;
    if (!moving || !scroller) return;
    const box = scroller.getBoundingClientRect();
    const [from, to] = axis === "vertical" ? [box.top, box.bottom] : [box.left, box.right];
    const at = last[client];
    const step = at < from + EDGE ? -Math.min(1, (from + EDGE - at) / EDGE) : at > to - EDGE ? Math.min(1, (at - to + EDGE) / EDGE) : 0;
    if (!step) return;
    const before = axis === "vertical" ? scroller.scrollTop : scroller.scrollLeft;
    if (axis === "vertical") scroller.scrollTop += step * EDGE_SPEED;
    else scroller.scrollLeft += step * EDGE_SPEED;
    if ((axis === "vertical" ? scroller.scrollTop : scroller.scrollLeft) === before) return;
    moving.place(last);
    frame = window.requestAnimationFrame(scroll);
  };

  // The browser's own drag of the row (draggable for a finger) and a text
  // selection give way to this one while the button is down.
  window.addEventListener("dragstart", (event) => event.preventDefault(), options);
  window.addEventListener("selectstart", (event) => event.preventDefault(), options);
  // The pointer at a point: once it has gone DRAG_START from the press the row moves, and goes with it.
  // Whether the row is moving after.
  const moveTo = (event: PointerEvent) => {
    last = { clientX: event.clientX, clientY: event.clientY };
    if (!moving) {
      if (Math.hypot(last.clientX - start.clientX, last.clientY - start.clientY) < DRAG_START) return false;
      if (press.defaultPrevented) { finish(false); return false; }
      const lists = list(), begun = lists.begin(source, start);
      if (!begun) { finish(false); return false; }
      moving = begun.move;
      scroller = listScroller(begun.row, axis);
      giveFocusBack();
      lists.started(source);
    }
    moving.place(last);
    return true;
  };
  window.addEventListener("pointermove", (event) => {
    if (event.pointerId === press.pointerId && moveTo(event) && !frame) frame = window.requestAnimationFrame(scroll);
  }, options);
  // The release is the pointer's last move: the row lands where it is let go, though no move came before
  // it — a quick hand's moves can all go to the browser's own drag start, which gives way here (as a
  // calendar's "날짜 없음" row's did, 2026-11-18, pointerDrag).
  window.addEventListener("pointerup", (event) => {
    if (event.pointerId !== press.pointerId) return;
    if (moveTo(event)) swallowClick();
    finish(true);
  }, options);
  window.addEventListener("pointercancel", (event) => { if (event.pointerId === press.pointerId) finish(false); }, options);
  // The window losing the focus, not the press moving it from a field to the grip.
  window.addEventListener("blur", (event) => { if (event.target === window) finish(false); }, options);
  window.addEventListener("keydown", (event) => {
    if (event.key !== "Escape" || !moving) return;
    // The menu the list is in stays open, as Notion's (seen 2026-11-16 in Chrome); a view's rows let their choice go (carriedChoice.ts).
    event.preventDefault();
    event.stopImmediatePropagation();
    window.addEventListener("pointerup", swallowClick, { capture: true, once: true });
    letGoCarried();
    finish(false);
  }, options);
  return () => finish(false);
}
