import { useSyncExternalStore } from "react";
import { imeComposing } from "./fieldKeyBoundary";
// Which input leads a menu's rows, the keys or the pointer: a row lit while
// the keys lead shows the keyboard ring, one lit while the pointer leads only
// the shade (NativeMenuItem, NativeActionMenuItem). The keys that move or
// choose lead — the arrows, Enter, Home, End, Page Up and Down — and so
// does a press of the pointer. Tab, which goes round a menu's own stops (its
// search, the search's "입력 값 지우기", the foot's person: menuTabStops.ts)
// and never its rows, leaves the lead as it was: a row a search lights after
// it shows no ring, the field keeping its own (Notion's block menu, seen
// 2026-11-26 in Chrome 1440 in two tabs: Tab, and "q" lit "인용" grey).
// Keys that type (a letter, a digit, a space, Backspace or Delete, a
// composition's keys) and Escape lead nothing: the rows
// keep the look the last lead gave them, though the browser and Aria count
// those keys as the keyboard's. As Notion's (seen 2026-11-16 at 1440 in
// Chrome, judged by the screen: a page's "⋯" pressed open and "link" typed,
// "링크 복사" grey with no ring, ↓ first and then "link" ringed; a block's
// "⋮⋮" pressed open the same, ↓ first ringed; a block menu opened by ⌘/ after
// a letter typed in the text, then "link", grey; a list beside the page's
// "⋯" that the pointer opened, closed by Escape, then "링크" typed, grey —
// Escape leaving the rows as the lead before it left them: "테마와 레이아웃 ›"
// pressed open, a switch in it pressed, Escape, "k", the row grey).
const LEADING_KEYS = new Set(["ArrowUp", "ArrowDown", "ArrowLeft", "ArrowRight", "Enter", "Home", "End", "PageUp", "PageDown"]);
// In a list or menu that holds the keys itself, no field to type in, a letter
// or Backspace leads as the arrows do: its lit row takes the ring, nothing
// typed anywhere, as Notion's (measured 2026-11-27 in Chrome at 1440, dpr 1,
// in two tabs at different times, judged by the screen after the key, no
// script before it: a column menu's "정렬 ›" list the pointer opened, "z" and
// Backspace ringed "A → Z로 정렬"; the column menu holding the keys after its
// list closed, "q" ringed the row the pointer lit; the title column's menu,
// a row lit by the pointer, "q" ringed it; a page "⋯"'s "알림 받기 ›" list,
// "q" ringed its checked row, its search left empty). Typed in a field — a
// menu's search, a column's name — they lead nothing, as above.
const TYPING_KEYS = new Set(["Backspace", "Delete"]);
const typingKey = (event: KeyboardEvent) => (event.key.length === 1 && event.key !== " " || TYPING_KEYS.has(event.key))
  && !event.altKey && !imeComposing(event);
// Whether the focus is a list or menu itself (or a row of one, or the box one is drawn in), not a field or the page,
// in a menu with no search of its own: a row that holds the focus in one (an Escape giving it back) keeps its look.
function holdsMenuKeys(target: EventTarget | null) {
  if (!(target instanceof HTMLElement) || target === document.body || target.isContentEditable) return false;
  if (target instanceof HTMLInputElement || target instanceof HTMLTextAreaElement || target instanceof HTMLSelectElement) return false;
  const menu = target.closest("[data-native-menu]") ?? target.querySelector("[data-native-menu]");
  return menu !== null && !menu.querySelector("[data-native-menu-search], [data-block-menu-search]");
}
// Rows draw their ring from the lead as they are drawn (NativeMenuItem, NativeActionMenuItem): a key that turns the
// lead to the keys without moving a highlight draws them again (useMenuInputLead).
let leadDrawn = 0;
const leadListeners = new Set<() => void>();
function leadChanged() {
  leadDrawn += 1;
  for (const listener of leadListeners) listener();
}

let keysLead = false, lastKeyAt = 0, lastPointerMove = 0;
// Keys came since the pointer last led, none of them leading.
let quietKeys = false;
// The last key was Escape.
let escaped = false;
// A shortcut (⌘ or Ctrl with a key) came outside the menus after the keys
// last led: the keys were the page's, and a menu the shortcut opens is not
// led by them (see below).
let shortcutAfterKeys = false;
// Inside a menu: its rows, or the search or name field it holds (both drawn
// within the element `data-native-menu` marks).
const inMenu = (target: EventTarget | null) => target instanceof Element && target.closest("[data-native-menu]") !== null;
// The menu the last press came in, if any.
let pressedMenu: Element | null = null;
if (typeof document !== "undefined") {
  document.addEventListener("keydown", (event) => {
    if (["Shift", "Meta", "Control", "Alt"].includes(event.key)) return;
    if (event.metaKey || event.ctrlKey) {
      if (keysLead && !inMenu(event.target)) shortcutAfterKeys = true;
      return;
    }
    escaped = event.key === "Escape";
    if (event.key === "Tab") return;
    if (typingKey(event) && holdsMenuKeys(event.target)) {
      const was = keysLead && !quietKeys;
      keysLead = true; quietKeys = false; shortcutAfterKeys = false; lastKeyAt = performance.now();
      if (!was) leadChanged();
      return;
    }
    if (imeComposing(event) || !LEADING_KEYS.has(event.key)) {
      if (!keysLead) quietKeys = true;
      return;
    }
    keysLead = true; quietKeys = false; shortcutAfterKeys = false; lastKeyAt = performance.now();
  }, true);
  document.addEventListener("pointerdown", (event) => {
    keysLead = false; quietKeys = false; escaped = false; shortcutAfterKeys = false;
    pressedMenu = event.target instanceof Element ? event.target.closest('[role="menu"]') : null;
  }, true);
  // Keys that led before a shortcut opened a menu — End or Enter in the
  // text, then ⌘/ — lead nothing in it: as the focus comes into it, its rows
  // take the pointer's look, a row its search lights shaded with no ring,
  // until a key that leads comes in it. As Notion's block menu (seen
  // 2026-11-25 at 1440 in Chrome, dpr 2: End, ⌘/, "제목" shaded "제목1", in
  // three tabs; so after Enter made the line, and after ↑ moved a block
  // selection; ↓ first, then "제목", ringed it; ↓, "블록", ⌘A, "제목" kept
  // the ring, in two tabs — a shortcut inside the menu ends nothing). A key
  // that leads and opens a menu itself (Enter, ↓ or → on its row or button)
  // still leads its rows; a menu the pointer opens starts unled anyway, its
  // press ending the lead.
  document.addEventListener("focusin", (event) => {
    if (!shortcutAfterKeys || !inMenu(event.target)) return;
    shortcutAfterKeys = false; keysLead = false; quietKeys = true;
  }, true);
  // When the pointer last moved by the person's hand — not the moves a browser
  // makes up when a menu appears under a still pointer, which move it nowhere.
  document.addEventListener("pointermove", (event) => { if (event.movementX || event.movementY) lastPointerMove = performance.now(); },
    { capture: true, passive: true });
}

/** Whether a key that leads came after the pointer's last press. */
export function keysLeadRows() {
  return keysLead;
}

/** When the last key that leads came. */
export function lastLeadingKeyAt() {
  return lastKeyAt;
}

/** When the pointer last moved by hand. */
export function lastPointerMoveAt() {
  return lastPointerMove;
}

/** Whether the keys lead now: a key that leads came after the pointer's last press and its last move by hand. */
export function keysLeadNow() {
  return keysLead && lastKeyAt > lastPointerMove;
}

/**
 * Whether keys came since the pointer last led, none of them leading (typing,
 * Escape): the rows keep the pointer's look — shaded, no ring — though Aria's
 * focus-visible now says keys.
 */
export function pointerKeptLead() {
  return quietKeys;
}

/**
 * Whether an Escape the pointer led just closed a list beside `menu` that
 * the pointer had pressed in: its row takes the focus back and keeps it, a
 * letter typed then going nowhere, as Notion's page "⋯" does (seen
 * 2026-11-16 at 1440 in Chrome, in two tabs twenty minutes apart: "테마와
 * 레이아웃 ›" pressed open, a switch in it pressed twice, Escape — the focus
 * on the row, "k" in no field). One only passed over, or opened by the keys,
 * gives its letters to the menu's search (the same page, the pointer resting
 * in the list without a press; a block menu's "전환 ›" so; → then Escape).
 */
export function escapeLeftPressedList(menu: Element) {
  return escaped && quietKeys && pressedMenu !== null && pressedMenu !== menu && !menu.contains(pressedMenu);
}

/** Draws a menu row again when a key turns the lead to the keys without moving its highlight (a letter typed in a list
 *  that holds the keys itself). */
export function useMenuInputLead() {
  return useSyncExternalStore((listener) => { leadListeners.add(listener); return () => { leadListeners.delete(listener); }; },
    () => leadDrawn, () => 0);
}
