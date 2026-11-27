import { appleTouchDevice } from "./iosViewport";
import { phoneScreen } from "./screenKind";
import { typePhoneSheet } from "./phoneWindowType";
import { followSheetDrag, type SheetDragFollow } from "@/components/shell/sheetBodyDrag";
import "./phoneWindowSheet.css";

// The grip's band at a sheet's top: 6px, the 5px grip, 6px (phoneWindowSheet.css).
const GRIP_BAND = 17;
// A press this far down the sheet, off its controls, drags it as its grip does.
const DRAG_HEAD = 56;
const CONTROLS = "button, a, input, textarea, select, label, [contenteditable='true'], [role='button'], [role='combobox']";

/**
 * How a window stands on a phone: a sheet rising from the screen's foot, the
 * whole screen, or a viewer — what it shows alone over the dark shade, drawn
 * its own way.
 */
export type PhoneWindowForm = "sheet" | "screen" | "viewer";

// Each window's phone form, decided here for every window: a sheet unless
// Notion's phone draws it otherwise, named by the mark the window carries.
const PHONE_FORMS: Readonly<Record<string, PhoneWindowForm>> = {
  // A page's "⋯ › 내보내기" covers the screen on both phones, in either theme
  // (seen 2026-11-17: Galaxy A90 Chrome 411 light and iPhone Safari 402 dark,
  // twice): "내보내기" in its head with a blue "닫기", grouped rows, a blue
  // "내보내기" row (PageExportPhone.tsx lays them).
  "data-page-export-dialog": "screen",
  // An embed's "전체 화면" and a Mermaid diagram's are the thing alone over
  // the windows' shade, the screen's width, in the middle with no head or
  // edge (seen 2026-11-17: iPhone Safari 402 dark, 402 × 367.5 and 402 ×
  // 497.9; Galaxy A90 Chrome 411 light, 411 × 366.3 and 411 × 509).
  "data-block-media-preview": "viewer",
  "data-mermaid-lightbox": "viewer",
};

/** The phone form of the window carrying this mark. */
export function phoneWindowForm(mark: string): PhoneWindowForm {
  return PHONE_FORMS[mark] ?? "sheet";
}

function formOf(root: HTMLElement): PhoneWindowForm {
  for (const mark of Object.keys(PHONE_FORMS)) if (root.hasAttribute(mark)) return PHONE_FORMS[mark];
  return "sheet";
}

/** Whether a box paints a ground of its own, as every window does; a box that only lays one out paints none. */
function paintsGround(style: CSSStyleDeclaration) {
  return !/^transparent$|[,/]\s*0\)$/.test(style.backgroundColor);
}

/** Whether a box covers the whole screen, as a phone's own screens do (settings, search, the inbox). */
function coversScreen(rect: DOMRect) {
  return rect.top <= 1 && rect.left <= 1 && rect.right >= window.innerWidth - 1 && rect.bottom >= window.innerHeight - 1;
}

/**
 * A window on a phone rises from the screen's foot as a sheet, as Notion's
 * phone does with its windows (seen 2026-11-12 at 402pt, iPhone Safari:
 * "팀스페이스 만들기" from 10pt under the screen's top to its foot, full
 * width, its top corners 12pt round, a 38pt grip 6pt down and the window's
 * own content under it, its ⓧ 27pt down; 2026-11-13: the developer portal's
 * "새 토큰" the same, dragged down it closes). Which, by its shape alone, in
 * either theme: a modal dialog with a ground of its own that doesn't already
 * cover the screen, fixed to the screen or held by what covers it, and isn't
 * one of the phone's own sheets, which their own styles lay on the page, held
 * by the body (phoneBarGround.ts) — PhoneSheet's, and a new comment's at the
 * foot of what shows (PhoneCommentSheet.module.css; 2026-11-17, Galaxy A90:
 * taken for a window, it rose to the whole screen over the dimmed page and
 * the line commented on). Its ground's colour says nothing: windows paint
 * theirs in several (2026-11-17: told by the window token's, the add window
 * was a sheet in the dark theme alone, the new comment's sheet in the light
 * alone).
 * A short confirmation (`alertdialog`) stays a card in the middle, as
 * Notion's phone asks before deleting forever (seen 2026-11-13). The
 * sheet is laid on the page, as a phone's sheets are (phoneBarGround.ts), so
 * it stands only where what holds it covers the screen. Its look is
 * phoneWindowSheet.css, its type the phone's (phoneWindowType.ts); a drag
 * down from its grip or head lands as every sheet's does (followSheetDrag):
 * let go far or fast enough it closes as an Escape would (`close`), short of
 * that it comes back. Returns what undoes the drag's listeners and the
 * phone's type.
 *
 * A window Notion's phone draws otherwise (PHONE_FORMS) is no sheet: one
 * over the whole screen is laid there the same way, square on the phone
 * screens' soft ground with no grip (phoneWindowSheet.css), its content the
 * window's own phone screen in the phone's sizes; a viewer keeps its own
 * drawing over its shade.
 */
export function sheetPhoneWindow(root: HTMLElement, close: (() => void) | undefined): (() => void) | undefined {
  if (!phoneScreen() || root.getAttribute("role") !== "dialog" || root.getAttribute("aria-modal") !== "true") return undefined;
  const form = formOf(root);
  if (form === "viewer") {
    root.dataset.phoneWindow = form;
    return undefined;
  }
  // What holds it once absolute: the nearest box placed itself.
  let holder = root.parentElement;
  while (holder && holder !== document.body && getComputedStyle(holder).position === "static") holder = holder.parentElement;
  const style = getComputedStyle(root), onPage = style.position === "absolute" && (!holder || holder === document.body);
  if (onPage || root.closest("[data-phone-sheet]") || !paintsGround(style)) return undefined;
  if (holder && holder !== document.body && !coversScreen(holder.getBoundingClientRect())) return undefined;
  if (form === "screen") {
    root.dataset.phoneWindow = form;
    return undefined;
  }
  if (coversScreen(root.getBoundingClientRect())) return undefined;
  root.dataset.phoneWindow = "sheet";
  root.dataset.apple = appleTouchDevice() ? "true" : "false";
  const untype = typePhoneSheet(root), undrag = close ? dragToClose(root, close) : undefined;
  return () => { untype(); undrag?.(); };
}

function dragToClose(root: HTMLElement, close: () => void) {
  let drag: { pointer: number; from: number; follow: SheetDragFollow; moving: boolean } | null = null;
  const reset = () => {
    root.style.removeProperty("--phone-window-drag");
    root.style.transition = "";
  };
  const down = (event: PointerEvent) => {
    if (event.button !== 0 || drag) return;
    const top = root.getBoundingClientRect().top;
    const inBand = event.clientY - top < GRIP_BAND;
    const onHead = event.clientY - top < DRAG_HEAD && root.scrollTop === 0
      && !(event.target instanceof Element && event.target.closest(CONTROLS));
    if (!inBand && !onHead) return;
    drag = { pointer: event.pointerId, from: event.clientY, follow: followSheetDrag(Date.now()), moving: false };
  };
  const move = (event: PointerEvent) => {
    if (!drag || drag.pointer !== event.pointerId) return;
    const by = event.clientY - drag.from;
    if (!drag.moving) {
      if (Math.abs(by) < 8) return;
      drag.moving = true;
      root.style.transition = "none";
    }
    drag.follow.move(by);
    root.style.setProperty("--phone-window-drag", `${Math.max(0, by)}px`);
  };
  const end = (event: PointerEvent, letGo: boolean) => {
    const current = drag;
    if (!current || current.pointer !== event.pointerId) return;
    drag = null;
    reset();
    if (!letGo || !current.moving) current.follow.stop();
    else if (current.follow.lands(root) === "closed") close();
  };
  const up = (event: PointerEvent) => end(event, true);
  const cancel = (event: PointerEvent) => end(event, false);
  // A finger pulling the sheet down from its grip or head moves the sheet,
  // not what scrolls in it (touch-action can't name a part of a box).
  const holdScroll = (event: TouchEvent) => {
    const touch = event.touches[0];
    if (drag && touch && (drag.moving || touch.clientY > drag.from) && event.cancelable) event.preventDefault();
  };
  root.addEventListener("pointerdown", down);
  window.addEventListener("pointermove", move);
  window.addEventListener("pointerup", up);
  window.addEventListener("pointercancel", cancel);
  root.addEventListener("touchmove", holdScroll, { passive: false });
  return () => {
    root.removeEventListener("pointerdown", down);
    window.removeEventListener("pointermove", move);
    window.removeEventListener("pointerup", up);
    window.removeEventListener("pointercancel", cancel);
    root.removeEventListener("touchmove", holdScroll);
  };
}
