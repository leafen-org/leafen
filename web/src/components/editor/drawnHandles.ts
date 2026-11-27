import { useEffect, useState, type RefObject } from "react";
import { flushSync } from "react-dom";
import { keyCameLast, lastPressedControl } from "@/lib/lastPress";
import { caretMenuOpen, watchCaretMenus } from "@/lib/pageHoverShield";
import { raisesKeyboard, touchScreen } from "@/lib/screenKind";

/**
 * Which block rows draw their handles — the "+ ⋮⋮" beside the block and its
 * "💬+" past the line — as Notion's draws them for the block the pointer is
 * at and none at rest (seen two ways, 2026-11-15: its page in Chrome had no
 * "+" or "⋮⋮" until a block was pointed, then one of each; its web client
 * draws one hover menu for the pointed block, both buttons out of the Tab
 * order). Every block of a long page held them unseen, two of the page's
 * six positioned boxes a block, which the browser went through at each
 * pointer test (2026-11-15: 5–8 ms twice at an Enter on a 1,031-block page
 * at CPU ×2).
 *
 * Here a row draws them while the pointer is at its height in its editor,
 * or beside it, and while the focus is in it; when and where they show
 * stays with their CSS (EditorBlockHandles), so the pointer finds them
 * where it did, and the moment the pointer comes to a row's height its
 * handles are there before it is pressed. A touch screen draws every row's,
 * which its own rules show by the caret and by a focus a person put in the
 * row (watchPersonFocus).
 *
 * While a menu at the caret ("/" or "+") is open no row draws them, the
 * pointer and the focus aside, and an awake handle's row puts them away
 * (useHandlesAwayUnderCaretMenu), as Notion's (pageHoverShield.ts).
 */

/** How far from a row's box its handles may stand: 8px over it (a divider's), 28px from its top, 96px before it (a column list's). */
const BAND = 16;
/** How far beside an editor the pointer still brings a row's handles: 150px before its blocks or past its column (useBlockHoverReach). */
const ROOM = 160;
const BLOCK_VIEW = "node-nativeBlock";
/** What a column's view holds (NativeTiptapColumnView). */
const COLUMN_VIEW = "data-native-column";

type Watch = { root: Element | null; setters: Set<(drawn: boolean) => void> };
const watched = new Map<HTMLElement, Watch>();
const roots = new Map<Element, number>();
let drawn = new Set<HTMLElement>();
let pointer: { x: number; y: number } | null = null;
let tracking = false;
let again = 0;
let fallback = 0;
let later = 0;
let resized: ResizeObserver | null = null;
/** The rows found as a pointer move began, drawn once every part of the page has had it. */
let pending: Set<HTMLElement> | null = null;

/** Whether the row draws its handles now. */
export function useHandlesDrawn(row: RefObject<HTMLElement | null>) {
  const [shown, setShown] = useState(touchScreen);
  useEffect(() => {
    watchPersonFocus();
    const element = row.current;
    if (!element || touchScreen()) return;
    const watch = watched.get(element) ?? watchRow(element);
    watch.setters.add(setShown);
    if (drawn.has(element)) setShown(true);
    track();
    // A row drawn under the pointer, as a new line, is found as the frame it came in is shown.
    redrawLater();
    return () => {
      watch.setters.delete(setShown);
      if (watch.setters.size) return;
      watched.delete(element);
      drawn.delete(element);
      if (watch.root) {
        const left = (roots.get(watch.root) ?? 1) - 1;
        if (left) roots.set(watch.root, left);
        else {
          roots.delete(watch.root);
          resized?.unobserve(watch.root);
        }
      }
    };
  }, [row]);
  return shown;
}

/** An awake handle's row puts its handles away while a menu at the caret is open, as no row draws them then. */
export function useHandlesAwayUnderCaretMenu(row: RefObject<HTMLElement | null>) {
  useEffect(() => {
    const element = row.current;
    if (!element || touchScreen()) return undefined;
    const mark = () => element.toggleAttribute("data-caret-menu-open", caretMenuOpen());
    mark();
    const release = watchCaretMenus(mark);
    return () => {
      release();
      element.removeAttribute("data-caret-menu-open");
    };
  }, [row]);
}

/** A block's row, as the focus and a press find it (BlockFrame's). */
const ROW = "[data-block-id] > [role='group'][data-type]";
/** A control a press on leaves the handles where they were: a toggle's arrow (NativeTiptapTextView). */
const LEAVES_HANDLES = "[data-leaves-handles]";
let personFocus: HTMLElement[] = [];
let held: HTMLElement[] = [];
let shownAtPress = false;
let watchingFocus = false;

/**
 * On a touch screen a focus in a block's row shows its "+ ⋮⋮" only when a
 * person put it there (data-person-focus, EditorBlockHandles.module.css): a
 * field of the block's own holding the caret, a database's name, or — while
 * a row showed its handles as the press came — the control a press landed
 * on, focused by that press. A focus code gives back as a window, a preview
 * or the block's menu closes — to a file's row, to its "⋮⋮" — is no press,
 * and the row stays bare, as Notion's iPad (seen 2026-11-19 in iPad Safari at
 * 834: after its docx and xlsx previews and its block menu closed, no "+ ⋮⋮"
 * beside the block). Each focus marks the rows it stands in, or none; a press
 * outside a marked row takes its mark away (a menu closed by a press on no
 * control leaves the focus where it was).
 *
 * A press on a toggle's arrow leaves the handles where they were, the arrow
 * focused all the same: the rows showing them as it came keep them
 * (data-handle-held) until the next press or focus elsewhere, and the
 * toggle's own stay put away. A control pressed while no row showed them —
 * a to-do's box on a page just opened — brings none out. Seen in Notion's
 * iPad Safari at 834, 2026-11-19, no script before the presses, in four
 * page loads: after a press on a line's text, or on a to-do's box with the
 * handles out, a press on a toggle's arrow left the "+ ⋮⋮" beside that line
 * or to-do (its focus on the arrow, read after); on a page just opened a
 * press on a to-do's box, then on the arrow, showed none.
 */
function watchPersonFocus() {
  if (watchingFocus || typeof document === "undefined") return;
  watchingFocus = true;
  const mark = (rows: HTMLElement[]) => {
    for (const row of personFocus) if (!rows.includes(row)) row.removeAttribute("data-person-focus");
    for (const row of rows) if (!personFocus.includes(row)) row.setAttribute("data-person-focus", "");
    personFocus = rows;
  };
  const hold = (rows: HTMLElement[]) => {
    for (const row of held) if (!rows.includes(row)) row.removeAttribute("data-handle-held");
    for (const row of rows) if (!held.includes(row)) row.setAttribute("data-handle-held", "");
    held = rows;
  };
  document.addEventListener("focusin", (event) => {
    const target = event.target instanceof HTMLElement ? event.target : null;
    if (target?.closest(LEAVES_HANDLES)) return;
    hold([]);
    const pressed = lastPressedControl();
    const rows: HTMLElement[] = [];
    if (target && touchScreen() && (raisesKeyboard(target) || (shownAtPress && !keyCameLast() && pressed !== null && pressed.contains(target)))) {
      for (let row = target.closest<HTMLElement>(ROW); row; row = row.parentElement?.closest<HTMLElement>(ROW) ?? null) rows.push(row);
    }
    mark(rows);
  }, true);
  document.addEventListener("pointerdown", (event) => {
    if (!touchScreen()) return;
    const target = event.target instanceof Element ? event.target : null;
    const showing = rowsShowingHandles();
    shownAtPress = showing.length > 0;
    if (target?.closest(LEAVES_HANDLES)) {
      hold(showing);
      return;
    }
    hold([]);
    if (!personFocus.length) return;
    mark(personFocus.filter((row) => target && row.contains(target)));
  }, true);
}

/** The rows showing their "+ ⋮⋮" on a touch screen by the person's rules (EditorBlockHandles.module.css), read from the focus and the caret, not the page. */
function rowsShowingHandles(): HTMLElement[] {
  const rows = new Set(held);
  for (const row of personFocus) if (row.matches(":focus-within")) rows.add(row);
  const anchor = document.getSelection()?.anchorNode;
  const at = anchor instanceof Element ? anchor : anchor?.parentElement;
  const caretLine = at?.closest("[data-caret-block='true']")?.closest<HTMLElement>(ROW);
  if (caretLine) rows.add(caretLine);
  const kept = at?.closest<HTMLElement>(`${ROW}[data-caret-kept-in='true']`);
  if (kept) rows.add(kept);
  return [...rows];
}

function watchRow(row: HTMLElement): Watch {
  const root = row.closest("[data-editor-root]:not([data-editor-nested])");
  const watch: Watch = { root, setters: new Set() };
  watched.set(row, watch);
  if (root && !roots.has(root)) {
    // Blocks coming, going or growing move others under a still pointer.
    resized ??= new ResizeObserver(redrawLater);
    resized.observe(root);
  }
  if (root) roots.set(root, (roots.get(root) ?? 0) + 1);
  return watch;
}

function track() {
  if (tracking) return;
  tracking = true;
  // The rows are found as the move reaches the page, before anything in it
  // changes it, and drawn as the move leaves it: what reads the page on the
  // way (useBlockHoverReach, trackBlockEnd) reads it as laid out, and the
  // new handles are there before a press can follow. One that never leaves
  // it, its way stopped, draws them by the next frame.
  document.addEventListener("pointermove", (event) => {
    // A press held (a drag, a choice being drawn) keeps the rows it began with.
    if (event.pointerType !== "mouse" || event.buttons) return;
    pointer = { x: event.clientX, y: event.clientY };
    pending = measure();
    if (!fallback) fallback = window.requestAnimationFrame(() => { fallback = 0; drawPending(); });
  }, { capture: true, passive: true });
  window.addEventListener("pointermove", drawPending, { passive: true });
  document.addEventListener("pointerout", (event) => {
    if (event.relatedTarget || event.pointerType !== "mouse") return;
    pointer = null;
    draw(measure());
  }, true);
  // The page moving under a still pointer brings other rows to it.
  document.addEventListener("scroll", () => { if (pointer) redrawSoon(); }, { capture: true, passive: true });
  document.addEventListener("focusin", redrawSoon, true);
  document.addEventListener("focusout", redrawSoon, true);
  watchCaretMenus(redrawSoon);
}

function redrawSoon() {
  if (again) return;
  again = window.requestAnimationFrame(() => { again = 0; draw(measure()); });
}

/**
 * Finds the rows again in the next frame, as the page changed under a still
 * pointer; while keys put the handles away (typingFade), the pointer's next
 * move finds them. Read as that frame begins, the page is laid out once for
 * the frame and the reading: read in a task after a frame, it laid the page
 * out again for what changed meanwhile (2026-11-15: a new table row's title
 * taking the focus, 30 ms at CPU ×2 just after a table's "+ 새 페이지").
 */
function redrawLater() {
  if (later || !pointer) return;
  later = window.requestAnimationFrame(() => {
    later = 0;
    if (pointer && !document.documentElement.dataset.typing) draw(measure());
  });
}

function drawPending() {
  if (pending) draw(pending);
}

function measure() {
  const next = new Set<HTMLElement>();
  if (caretMenuOpen()) return next;
  if (pointer) for (const root of roots.keys()) rowsAt(root, pointer.x, pointer.y, next);
  // The rows the focus is in: a control the keyboard reached shows its block's handles.
  for (let row = document.activeElement?.closest<HTMLElement>(ROW); row; row = row.parentElement?.closest<HTMLElement>(ROW)) next.add(row);
  return next;
}

function draw(next: Set<HTMLElement>) {
  pending = null;
  const changed: [HTMLElement, boolean][] = [];
  for (const row of next) if (!drawn.has(row) && watched.has(row)) changed.push([row, true]);
  for (const row of drawn) if (!next.has(row)) changed.push([row, false]);
  drawn = new Set([...next].filter((row) => watched.has(row)));
  if (!changed.length) return;
  // Drawn before the pointer's next press is tested against the page.
  flushSync(() => { for (const [row, on] of changed) watched.get(row)?.setters.forEach((set) => set(on)); });
}

/** The rows of the editor's blocks at the pointer's height, at every depth, beside it or not. */
function rowsAt(root: Element, x: number, y: number, out: Set<HTMLElement>) {
  if (!root.isConnected) return;
  const box = root.getBoundingClientRect();
  if (x < box.left - ROOM || x > box.right + ROOM || y < box.top - BAND || y > box.bottom + BAND) return;
  for (const list of listsIn(root)) groupsAt(list, y, out);
}

/**
 * Block lists held in an element, not in its blocks: the elements whose
 * children are blocks' views. A list is known by its first block, and the
 * blocks after it are not looked through (a long page's thousand).
 */
function listsIn(element: Element, lists: Element[] = []) {
  for (let child = element.firstElementChild; child; child = child.nextElementSibling) {
    if (child.classList.contains(BLOCK_VIEW)) {
      lists.push(element);
      return lists;
    }
    listsIn(child, lists);
  }
  return lists;
}

/** Blocks with nothing of their own inside, which are not looked into. */
const NO_BLOCKS_INSIDE = new Set(["inline_database", "child_database", "table", "image", "video", "audio", "pdf", "file",
  "embed", "bookmark", "code", "equation", "divider"]);

/**
 * A column's view draws no box of its own (display: contents, so its column
 * is a flex item of the column list) and has no row: the rows are its
 * blocks', looked through in each of the list's columns, which stand side by
 * side, not one under another (2026-11-16: no block in a column drew its
 * handles, the columns' empty boxes skipped).
 */
function columnsAt(list: Element, y: number, out: Set<HTMLElement>) {
  for (const view of list.children) {
    const column = view.firstElementChild;
    if (!(column instanceof HTMLElement) || !column.hasAttribute(COLUMN_VIEW)) continue;
    const rect = column.getBoundingClientRect();
    if (rect.height === 0 || rect.top - BAND > y || rect.bottom + BAND < y) continue;
    for (const inner of listsIn(column)) groupsAt(inner, y, out);
  }
}

function groupsAt(list: Element, y: number, out: Set<HTMLElement>) {
  const views = list.children;
  if (views[0]?.firstElementChild?.hasAttribute(COLUMN_VIEW)) {
    columnsAt(list, y, out);
    return;
  }
  let low = 0;
  let high = views.length;
  while (low < high) {
    const middle = (low + high) >> 1;
    if (views[middle].getBoundingClientRect().bottom + BAND < y) low = middle + 1;
    else high = middle;
  }
  for (let index = low; index < views.length; index += 1) {
    const view = views[index];
    const rect = view.getBoundingClientRect();
    if (rect.top - BAND > y) break;
    if (!view.classList.contains(BLOCK_VIEW) || rect.height === 0) continue;
    const group = view.querySelector<HTMLElement>("[data-block-id][role='group']");
    const row = group?.firstElementChild;
    if (!group || !(row instanceof HTMLElement)) continue;
    out.add(row);
    if (!NO_BLOCKS_INSIDE.has(group.dataset.blockType ?? "")) for (const inner of listsIn(group)) groupsAt(inner, y, out);
  }
}
