import { useLayoutEffect, useRef } from "react";
import { lessMotion } from "./sidePeekMotion";

/**
 * How a list beside a menu's row goes while its menu stays — left for another
 * row (useSubmenuSettledClose, PageMenu useSubmenuHover), or closed by Escape
 * or ←: it fades out over BESIDE_LEAVE_MS, fast then slow (CSS `ease`),
 * taking no presses, and is gone at the end. Gone with its menu — a press
 * outside, a row chosen, Escape on the menu — it goes at once with it. As
 * Notion's (seen 2026-11-25 in Chrome, two tabs: a block menu's "전환 ›" and
 * "색 ›" and a page "⋯"'s "알림 받기 ›" — an opacity transition of 200 ms
 * `ease` with pointer-events none, out of the page 210–222 ms after it began;
 * a press outside or a row chosen took the list and the menu at once).
 */
export const BESIDE_LEAVE_MS = 200;

// What a leaving copy keeps of what draws it, past its classes, style and drawing (an icon's own attributes): the
// light, ring, marks and colours of its rows. Whatever names it (ids, roles, its other data) goes, so nothing that
// looks for the list or its rows finds the copy, and nothing reads it out.
const LOOK = new Set(["aria-checked", "aria-expanded", "aria-disabled", "aria-selected", "aria-current", "aria-pressed",
  "data-focused", "data-focus-visible", "data-lit-by", "data-color", "data-chosen", "data-active", "data-moved-by", "data-side",
  "data-edge-fade", "data-font", "data-font-sample", "data-open", "data-pointer-passing", "data-phone-menu", "data-phone-menu-list"]);
const NAMES = new Set(["id", "role", "tabindex", "name", "for", "href", "title", "autofocus"]);

function lookOnly(element: Element) {
  for (const name of element.getAttributeNames()) {
    if (LOOK.has(name)) continue;
    if (NAMES.has(name) || name.startsWith("aria-") || name.startsWith("data-")) element.removeAttribute(name);
  }
}

/** How far a list's parts are scrolled, by their place among its elements (-1 for the list itself). */
export type BesideListScrolls = Map<number, number>;

function scrollsOf(list: HTMLElement): BesideListScrolls {
  const scrolls: BesideListScrolls = new Map();
  if (list.scrollTop) scrolls.set(-1, list.scrollTop);
  list.querySelectorAll("*").forEach((element, index) => { if (element.scrollTop) scrolls.set(index, element.scrollTop); });
  return scrolls;
}

// Whether its row still shows: its menu stays.
function shown(row: Element | null) {
  return Boolean(row?.isConnected) && (row?.checkVisibility?.({ opacityProperty: true, visibilityProperty: true }) ?? true);
}

/**
 * Leaves a copy of `list` where it was, fading out, once the list itself is
 * gone (BESIDE_LEAVE_MS) — `scrolled` says how far its parts were scrolled
 * where it is already out of the page. Its row gone too by the end of the
 * pass, the copy goes at once.
 */
export function leaveBeside(list: HTMLElement, row: Element | null, scrolled: BesideListScrolls) {
  if (lessMotion() || !shown(row)) return;
  const live = list.isConnected;
  const scrolls = live ? scrollsOf(list) : scrolled;
  const from = live ? Number(getComputedStyle(list).opacity) || 1 : 1;
  const copy = list.cloneNode(true) as HTMLElement;
  // Fields keep what was typed in them (a property, not an attribute).
  const fields = list.querySelectorAll<HTMLInputElement | HTMLTextAreaElement>("input, textarea");
  copy.querySelectorAll<HTMLInputElement | HTMLTextAreaElement>("input, textarea").forEach((field, index) => {
    if (fields[index]) field.value = fields[index].value;
  });
  lookOnly(copy);
  copy.querySelectorAll("*").forEach(lookOnly);
  copy.setAttribute("aria-hidden", "true");
  copy.inert = true;
  copy.style.pointerEvents = "none";
  copy.dataset.besideLeaving = "";
  if (live) list.after(copy); else document.body.append(copy);
  const parts = copy.querySelectorAll("*");
  for (const [index, top] of scrolls) {
    const part = index < 0 ? copy : parts[index];
    if (part) part.scrollTop = top;
  }
  // Read once the pass that closed it is done: closed with its menu, it goes with it before it is drawn again.
  queueMicrotask(() => {
    if (!shown(row)) { copy.remove(); return; }
    const done = () => copy.remove();
    const fade = copy.animate([{ opacity: from }, { opacity: 0 }], { duration: BESIDE_LEAVE_MS, easing: "ease", fill: "forwards" });
    void fade.finished.then(done, done);
    window.setTimeout(done, BESIDE_LEAVE_MS + 150);
  });
}

// The lists beside a menu's row drawn now (useBesideListLeaving), for what takes them apart from other popovers.
const besideLists = new WeakSet<Element>();

/** Whether `element` is, or is in, a list open beside a menu's row: it only fades in as it opens (popoverAppear.ts). */
export function isBesideList(element: Element | null | undefined) {
  for (let at = element; at; at = at.parentElement) if (besideLists.has(at)) return true;
  return false;
}

// The row each list beside a menu's row keeps lit since its light went out of it (besideLightHeld).
const heldLight = new WeakMap<Element, Element>();

/**
 * Whether `row`, in a list beside a menu's row, stays lit though the list's
 * own light has gone out of it to its menu — the pointer come onto another
 * row there, an Escape or ← giving its row the focus back —, while the list
 * waits to close and as it fades, till the light comes back into the list.
 * Notion's keeps it so (seen 2026-11-25 in Chrome at 1440, dpr 2: a block
 * menu's "전환 ›" with "텍스트" lit and a page "⋯"'s "알림 받기 ›" with "답글과
 * @멘션" lit, the pointer moved onto "블록 링크 복사" and "휴지통으로 이동": the
 * row stayed lit through the 125 ms wait and the fade till the list went,
 * the menu lighting the pointer's row at once and the list's row till the
 * list began to fade).
 */
export function besideLightHeld(row: Element | null | undefined) {
  for (let at = row?.parentElement; at; at = at.parentElement) if (besideLists.has(at)) return heldLight.get(at) === row;
  return false;
}

/**
 * A list beside a row fades out as it closes with its menu staying
 * (leaveBeside): `list` gives its element while `open`, `row` its row. The
 * list may be out of the page by the time its owner hears it closed (one
 * drawn by the owner's child): it is noted as drawn, with how far its parts
 * are scrolled. Its lit row stays lit as its light leaves it (besideLightHeld).
 */
export function useBesideListLeaving(open: boolean, list: () => Element | null, row: () => Element | null) {
  const seen = useRef<{ list: HTMLElement; scrolled: BesideListScrolls } | null>(null);
  const listOf = useRef(list), rowOf = useRef(row);
  listOf.current = list; rowOf.current = row;
  useLayoutEffect(() => {
    const element = open ? listOf.current() : null;
    if (element instanceof HTMLElement) besideLists.add(element);
    if (element instanceof HTMLElement && seen.current?.list !== element) seen.current = { list: element, scrolled: scrollsOf(element) };
  });
  useLayoutEffect(() => {
    if (!open) return undefined;
    const note = (event: Event) => {
      const at = seen.current, part = event.target;
      if (!at || !(part instanceof Element) || !at.list.contains(part)) return;
      at.scrolled.set(part === at.list ? -1 : Array.prototype.indexOf.call(at.list.querySelectorAll("*"), part), part.scrollTop);
    };
    // The light (the focus, or Aria's highlight named by it) leaving the list for outside it, its lit row is held lit; coming
    // back into it, its own light shows again. Heard before the menus' own handlers, which put the list's light out.
    const left = (event: FocusEvent) => {
      const at = seen.current, from = event.target, to = event.relatedTarget;
      if (!at || !(from instanceof Node) || !at.list.contains(from) || to instanceof Node && at.list.contains(to)) return;
      const lit = at.list.querySelector('[data-focused="true"]');
      if (lit) heldLight.set(at.list, lit);
    };
    const came = (event: FocusEvent) => {
      const at = seen.current;
      if (at && event.target instanceof Node && at.list.contains(event.target)) heldLight.delete(at.list);
    };
    document.addEventListener("scroll", note, true);
    document.addEventListener("focusout", left, true);
    document.addEventListener("focusin", came, true);
    return () => {
      document.removeEventListener("scroll", note, true);
      document.removeEventListener("focusout", left, true);
      document.removeEventListener("focusin", came, true);
      const at = seen.current;
      seen.current = null;
      if (at) leaveBeside(at.list, rowOf.current(), at.scrolled);
    };
  }, [open]);
}
