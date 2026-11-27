// While a window or menu floats over the page — a popover placed beside what
// opened it, which a press beside it closes — nothing under it answers the
// pointer, as under Notion's overlay: a backdrop as big as the window, just
// under the menu, takes the pointer — hover, press and wheel. Seen
// 2026-11-12: a database title's "+ ⋯", a board card's "✎ ⋯", its group's
// "⋯ +" and a block's handle stay away under the linked view's "새 보기"
// window and a view's "⋯" menu, and come back once it closes; a side peek's
// own page takes none under a menu opened in it, its property's menu
// covering the peek and the page beside it; under a select cell's list, the
// view's settings, a block's "⋯", the filter's property list, the page's "⋯",
// the template menu and a view title's "⋯", the page and the sidebar keep
// their place under the wheel (the same wheel rolls them 100px with the menu
// closed), and the search window under its "생성자" list is covered too. A
// press beside it closes it and does nothing more, as there. A menu at the
// caret shields the page from the wheel alone (holdWheelForCaretMenu).
// What the layer counts as its own (its opener, a bar it belongs to, the text
// it was typed from) and the layer itself, when drawn under the backdrop, are
// cut out of it and still answer — the layer whole, with all it draws past its
// own box: a menu placed under the button holding it, inside a window under the
// backdrop (the shortcuts window's "더 보기…", 2026-11-27: its rows took no press).
// Every menu, picker, card or small window a press beside closes takes the
// page so (`floats`), drawn with usePopoverPlacement or not, as Notion's,
// seen 2026-11-26 in Chrome, two tabs each, judged after the press: the page
// head's "인증 추가" menu, a "페이지 링크" picker, an empty image block's
// window, a new comment's card under a block or a row — a press on a line, a
// to-do's box or the top bar only closed it. A menu at the caret (":" emoji,
// "/", "+", a paste's menu) and a card reading a block's threads let the
// press go on, the caret where it was pressed (the same day for ":").
//
// Opening or closing a menu changes this one element's look. Marking the
// page for its styles to read restyled all of it each time, some 100–145 ms on
// a page with a table of 30 rows by 40 properties (2026-11-12, on this Mac).

import { afterNextPaint } from "./afterPress";
import { overlayRoot } from "./overlayRoot";

/**
 * Under which layer the backdrop goes (its z-index), the layers it shields
 * the page for, what still answers through it (their own parts, in the page
 * or under it) and which of those are layers themselves, cut out with all
 * they draw (drawnBoxes).
 */
export type PageShield = { zIndex: number; layers: ReadonlyArray<Element>; open: ReadonlyArray<Element>; own: ReadonlyArray<Element> };
type Hole = { part: Element; top: number; bottom: number; start: number; end: number };

let backdrop: HTMLElement | null = null;
let wanted: PageShield | null = null;
let drawn: PageShield | null = null;
let holes: Hole[] = [];
let cancelTurn: (() => void) | null = null;
let holesFrame = 0;
let watched: (() => void) | null = null;

// Whether a pointer is down: the window hears it before the page's own
// handlers, a layer closing on its release included. A close made while it
// was down lets presses through from its release on (passThrough); the
// release itself, already aimed at the backdrop, ends there with its click.
let pressing = false;
if (typeof window !== "undefined") {
  window.addEventListener("pointerdown", () => { pressing = true; }, { capture: true, passive: true });
  for (const type of ["pointerup", "pointercancel"]) window.addEventListener(type, () => {
    pressing = false;
    if (!wanted && drawn && !closed) { closed = true; passThrough(); }
  }, { capture: true, passive: true });
}

// Closed, the backdrop is still drawn until the next paint (syncPageShield)
// but takes no pointer: a press right after a menu closes — Escape, a press
// beside it, a choice in it — goes to what is under it, as with nothing open.
// Seen 2026-11-21 (gate lap 6): a row's right press just after Escape closed
// its menu opened none (the press landed on the backdrop), and a press at a
// line's end soon after its "@" menu closed left the focus on the body, the
// "@" typed next going in later on another line.
let closed = false;

// A drag begun in a layer goes through the backdrop to the page under it,
// the layer left open: a calendar's "날짜 없음" row dropped on a day, as
// Notion's (seen 2026-11-15). The browser's own drag finds its drop targets
// itself, so the backdrop takes no pointer while it goes; pointer events come
// back once it ends, its source perhaps gone from the page (the row it
// dated). A drag the pointer's own events carry finds them by what is under
// the pointer, the backdrop passed over (underDrag), the page under it still
// shown as under a menu.
let dragging = false;
if (typeof window !== "undefined") {
  window.addEventListener("dragstart", () => { dragging = true; passThrough(); }, { capture: true, passive: true });
  for (const type of ["dragend", "drop", "pointermove", "pointerdown"]) window.addEventListener(type, () => {
    if (dragging) { dragging = false; passThrough(); }
  }, { capture: true, passive: true });
}
function passThrough() {
  if (!backdrop) return;
  const value = dragging || closed ? "none" : "";
  if (backdrop.style.pointerEvents !== value) backdrop.style.pointerEvents = value;
}

/**
 * What a drag the pointer's own events carry (pointerDrag, a list's rows)
 * finds at a point: the top element there, the page's backdrop passed over,
 * as the browser's own drag finds it (passThrough). The copy it carries
 * takes no pointer and is never found.
 */
export function underDrag(x: number, y: number): Element | null {
  const top = document.elementFromPoint(x, y);
  if (!top || top !== backdrop) return top;
  return document.elementsFromPoint(x, y).find((element) => element !== backdrop) ?? null;
}

function backdropElement(): HTMLElement {
  if (backdrop?.isConnected) return backdrop;
  backdrop = document.createElement("div");
  backdrop.setAttribute("data-page-shield", "");
  backdrop.setAttribute("aria-hidden", "true");
  Object.assign(backdrop.style, { display: "none", position: "fixed", inset: "0", background: "transparent",
    cursor: "default", userSelect: "none" });
  backdrop.style.setProperty("-webkit-user-select", "none");
  overlayRoot().appendChild(backdrop);
  return backdrop;
}

/**
 * Shields the page under the floating layers on top while `shield` is given.
 * It comes and goes after the frame that draws the window opening or
 * closing: the press that opened it ends where it began. Closed, it covers
 * the whole page meanwhile but takes no pointer (closed above), unless a
 * press made the close: that press ends on it, or on a layer's own part it is
 * still down on, and the next press goes through. A layer opened or closed
 * over others changes it at once. Returns the backdrop while one is wanted,
 * for a window under the menu to keep it pressable (modal isolation).
 */
export function syncPageShield(shield: PageShield | null): HTMLElement | null {
  if (typeof document === "undefined") return null;
  wanted = shield;
  if (drawn) {
    const was = drawn;
    // Closed, the whole page waits under it, unless a press goes on to its
    // own part (a pointer still down on its opener).
    const open = shield?.open ?? (pressing ? was.open.filter((part) => !was.layers.some((layer) => layer.contains(part))) : []);
    if (open.length !== was.open.length || open.some((part, index) => part !== was.open[index])) {
      // What went closes at once, from the boxes already read; what came
      // opens once the frame is laid out.
      holes = holes.filter((hole) => open.includes(hole.part));
      clip();
      watch(shield ? open : []);
      if (shield) again();
    }
    drawn = shield ? shield : { ...was, open };
    if (shield) place(shield);
    else if (!pressing && !closed) { closed = true; passThrough(); }
  }
  if (Boolean(wanted) !== Boolean(drawn)) cancelTurn ??= afterNextPaint(() => {
    cancelTurn = null;
    if (wanted && !drawn) {
      drawn = wanted;
      place(wanted);
      watch(wanted.open);
      cutHoles();
    } else if (!wanted && drawn) hide();
  });
  return shield ? backdropElement() : null;
}

function place(shield: PageShield) {
  const element = backdropElement();
  const zIndex = String(shield.zIndex);
  if (element.style.zIndex !== zIndex) element.style.zIndex = zIndex;
  if (closed) { closed = false; passThrough(); }
  if (element.style.display === "none") element.style.display = "block";
}

function hide() {
  drawn = null;
  holes = [];
  watch([]);
  if (backdrop) {
    backdrop.style.display = "none";
    backdrop.style.clipPath = "";
  }
}

function again() {
  if (!holesFrame) holesFrame = requestAnimationFrame(() => { holesFrame = 0; cutHoles(); });
}

// A part that still answers moves with the page (a scroll it takes itself,
// the window's size, its own size as text is typed into it): its hole follows,
// as does one a layer draws past its own box (drawnBoxes), watched once cut.
let resizing: ResizeObserver | null = null;
const sizeWatched = new Set<Element>();
function watch(open: ReadonlyArray<Element>) {
  watched?.();
  watched = null;
  cancelAnimationFrame(holesFrame);
  holesFrame = 0;
  sizeWatched.clear();
  if (!open.length) return;
  const resize = new ResizeObserver(again);
  resizing = resize;
  for (const part of open) resize.observe(part);
  document.addEventListener("scroll", again, { capture: true, passive: true });
  window.addEventListener("resize", again, { passive: true });
  watched = () => {
    resize.disconnect();
    resizing = null;
    document.removeEventListener("scroll", again, { capture: true });
    window.removeEventListener("resize", again);
  };
}

function cutHoles() {
  if (!drawn) return;
  const width = window.innerWidth, height = window.innerHeight, own = drawn.own;
  holes = drawn.open.filter((part) => part.isConnected).flatMap((part) => drawnBoxes(part, own.includes(part)).map((box) => (
    { part, top: Math.max(0, Math.floor(box.top)), bottom: Math.min(height, Math.ceil(box.bottom)),
      start: Math.max(0, Math.floor(box.left)), end: Math.min(width, Math.ceil(box.right)) })))
    .filter((hole) => hole.bottom > hole.top && hole.end > hole.start);
  clip();
}

const within = (box: DOMRect, at: DOMRect) =>
  box.left >= at.left - 0.5 && box.right <= at.right + 0.5 && box.top >= at.top - 0.5 && box.bottom <= at.bottom + 0.5;

// A layer's own element (`whole`) is cut out with all it draws: its box and
// each box drawn in it past those (a menu placed under the button holding it,
// not its rows within the menu), what takes no pointer left under the
// backdrop; inside what clips what it holds (a list that scrolls) nothing is
// drawn past that. A box still growing in as it opens (popoverAppear.ts) is
// cut again once it has grown: its size then is no resize.
function drawnBoxes(part: Element, whole: boolean): DOMRect[] {
  const boxes = [part.getBoundingClientRect()];
  if (!whole) return boxes;
  const cut: Element[] = [part];
  const visit = (element: Element) => {
    for (const child of element.children) {
      const style = getComputedStyle(child);
      if (style.display === "none") continue;
      const box = child.getBoundingClientRect();
      if (box.width > 0 && box.height > 0 && style.visibility !== "hidden" && style.pointerEvents !== "none"
        && !boxes.some((at) => within(box, at))) {
        boxes.push(box);
        cut.push(child);
        if (!sizeWatched.has(child)) { sizeWatched.add(child); resizing?.observe(child); }
      }
      if (style.overflowX === "visible" && style.overflowY === "visible") visit(child);
    }
  };
  visit(part);
  for (const element of cut) cutAgainOnceGrown(element);
  return boxes;
}

const growing = new WeakSet<Animation>();
function cutAgainOnceGrown(element: Element) {
  if (typeof element.getAnimations !== "function") return;
  for (const animation of element.getAnimations()) {
    if (animation.playState === "finished" || growing.has(animation)) continue;
    growing.add(animation);
    animation.finished.then(again, again);
  }
}

// A menu at the caret — "/" typed in a line, "+" pressed beside one — keeps
// the wheel alone from the page: the page under it answers the pointer as
// with nothing open, as Notion's (measured 2026-11-16, first account, Chrome
// 1440, two tabs at different times, no script before an action and one
// letter typed after it): a row takes the hover (a page block's tint and its
// card, over the menu), a press beside it closes it and goes on (the caret
// where the words were pressed, at the line's end for the room beside them),
// and the wheel over the words or beside them leaves the page where it is,
// while the sidebar still scrolls under it. No block shows its "+ ⋮⋮" or
// "💬+" meanwhile (drawnHandles.ts). Nothing in the page is marked for it;
// what is drawn over the page (the menu, the card) still takes the wheel.
// Each kind of layer says which it holds (overlayKinds.ts holdUnderKind): a
// menu at the caret both — but ":"'s emoji menu, under which a block's
// handles still come to the pointer — and the text selection's toolbar the
// handles alone, the wheel rolling the page and the toolbar following the
// words (Notion's, 2026-11-26, two tabs each).
// Keyed by the layer (its element, or its ref while it is drawn yet to be).
const wheelHeld = new Map<object, number>(), handlesAway = new Map<object, number>();
const caretMenuWatchers = new Set<() => void>();
const count = (held: Map<object, number>, layer: object, by: number) => {
  const next = (held.get(layer) ?? 0) + by;
  if (next > 0) held.set(layer, next); else held.delete(layer);
};

function holdWheel(event: WheelEvent) {
  const target = event.target instanceof Element ? event.target : null;
  if (event.ctrlKey || (target && (overlayRoot().contains(target) || target.closest("[data-app-sidebar]")))) return;
  event.preventDefault();
}

/** While `layer` is drawn, keeps the wheel from the page under it and its blocks' handles away, as asked; returns the release. */
export function holdPageUnder(layer: object, { wheel, handles }: { wheel: boolean; handles: boolean }): () => void {
  const rolling = !wheelHeld.size;
  if (wheel) count(wheelHeld, layer, 1);
  if (handles) count(handlesAway, layer, 1);
  if (rolling && wheelHeld.size) window.addEventListener("wheel", holdWheel, { capture: true, passive: false });
  if (handles) for (const watcher of caretMenuWatchers) watcher();
  let held = true;
  return () => {
    if (!held) return;
    held = false;
    if (wheel) count(wheelHeld, layer, -1);
    if (handles) count(handlesAway, layer, -1);
    if (wheel && !wheelHeld.size) window.removeEventListener("wheel", holdWheel, { capture: true });
    if (handles) for (const watcher of caretMenuWatchers) watcher();
  };
}

/** Keeps the page from the wheel and its handles away while `menu`, a menu at the caret, is drawn; returns the release. */
export function holdWheelForCaretMenu(menu: Element): () => void {
  return holdPageUnder(menu, { wheel: true, handles: true });
}

/** Whether a layer keeping the blocks' handles away is drawn (a menu at the caret, the selection's toolbar). */
export function caretMenuOpen() {
  return handlesAway.size > 0;
}

/** Hears a menu at the caret open or close; returns the release. */
export function watchCaretMenus(watcher: () => void): () => void {
  caretMenuWatchers.add(watcher);
  return () => { caretMenuWatchers.delete(watcher); };
}

// The backdrop is cut to the window less the holes: row bands between their
// edges, each band's stretches left between the holes that cross it.
function clip() {
  const element = backdrop;
  if (!element) return;
  if (!holes.length) {
    element.style.clipPath = "";
    return;
  }
  const width = window.innerWidth, height = window.innerHeight;
  const edges = [...new Set([0, height, ...holes.flatMap((hole) => [hole.top, hole.bottom])])].sort((a, b) => a - b);
  let path = "";
  for (let index = 0; index < edges.length - 1; index++) {
    const top = edges[index], bottom = edges[index + 1];
    const cut = holes.filter((hole) => hole.top <= top && hole.bottom >= bottom).sort((a, b) => a.start - b.start);
    let from = 0;
    for (const hole of [...cut, { start: width, end: width }]) {
      if (hole.start > from) path += `M${from} ${top}H${hole.start}V${bottom}H${from}Z`;
      from = Math.max(from, hole.end);
    }
  }
  element.style.clipPath = path ? `path("${path}")` : "inset(50%)";
}
