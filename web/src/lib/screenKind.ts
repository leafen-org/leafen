// What kind of screen the app is on, decided here once for every screen and
// rule (2026-11-13: the touch judgement was spread over 31 media rules and
// three scripts, and a tablet or an opened fold, touch screens wider than a
// phone's, showed every line's "+ ⋮⋮" as no pointer can bring them, where
// Notion's iPad shows none).
//
// - A touch screen has no pointer to hover with, at any width: a phone's, a
//   tablet's, an opened fold's. What a pointer's hover brings out on a
//   desktop, a touch screen shows by its own rule.
// - A phone is a touch screen narrower than 600px, or one whose screen is
//   under 600px on its shorter side however it is held: it takes the phone's
//   layout (sheets, a drawer sidebar, 24px margins), turned on its side too,
//   as Notion's iPhone keeps its phone bar at 874 wide (seen 2026-11-16 in
//   iPhone Safari; the screen's size does not change with a soft keyboard,
//   where the window's height would). A desktop window as narrow keeps the
//   desktop's (seen in Notion 2026-11-01 at 480 and 560).
//
// The body carries both marks from the app's start, on every screen (the
// app's frame, a public page, sign-in), so a style reads
// `[data-touch="true"]` or `[data-phone="true"]` and sheets, windows and
// menus drawn into the body are reached as the frame's own parts are. Only
// the rules drawn before the script runs (index.html's bars, the body's
// ground in globals.css) keep the same queries as media rules.
import { appleTouchDevice } from "./iosViewport";

export const TOUCH_SCREEN_QUERY = "(hover: none) and (pointer: coarse)";
export const PHONE_WIDTH_QUERY = "(max-width: 599px)";
/** A screen a phone's size, held upright or on its side. */
export const PHONE_SCREEN_QUERY = "(max-device-width: 599px), (max-device-height: 599px)";

// Each query's list, asked for once: a new one is read and made again at
// each ask, and the screen is asked as each block's handles are drawn and at
// each key (2026-11-15: 0.4 ms for six new blocks of a paste at CPU ×2). A
// list answers as the screen is now; a window whose asking changed (a
// stand-in screen) is asked again.
const lists = new Map<string, MediaQueryList>();
let askedOf: Window["matchMedia"] | null = null;

function matches(query: string) {
  if (typeof window === "undefined" || !window.matchMedia) return false;
  if (askedOf !== window.matchMedia) {
    lists.clear();
    askedOf = window.matchMedia;
  }
  let list = lists.get(query);
  if (!list) lists.set(query, list = window.matchMedia(query));
  return list.matches === true;
}

/** Whether this is a touch screen, of any width. */
export function touchScreen() {
  return matches(TOUCH_SCREEN_QUERY);
}

/** Whether this is a phone: a touch screen narrower than 600px, or a phone's screen held on its side. */
export function phoneScreen() {
  return touchScreen() && (matches(PHONE_WIDTH_QUERY) || matches(PHONE_SCREEN_QUERY));
}

/** Whether the app's frame takes its narrow layout: a window narrower than 600px, or a phone held either way. */
export function narrowScreen() {
  return matches(PHONE_WIDTH_QUERY) || phoneScreen();
}

/**
 * Whether a field may take the focus as its screen or window opens: not on a
 * touch screen, where that would raise the keyboard before anything was
 * pressed. There the field waits for a tap, as Notion's phone and iPad
 * windows open with the keyboard down (seen 2026-11-10 and 2026-11-12) — a
 * sign-in or first-run screen as much as a window (2026-11-14: the code
 * screen a device code's link opens raised a tablet's keyboard), and a menu's
 * or picker's search as much as a window's field (2026-11-14: Notion's iPad
 * pickers open with no focus in their search, 834 wide). A pointer's screen
 * opens with its field focused.
 */
export function fieldFocusOnOpen() {
  return !touchScreen();
}

/** Whether focusing this control raises a touch screen's keyboard: a field to type in. */
export function raisesKeyboard(control: Element | null | undefined) {
  if (control instanceof HTMLTextAreaElement) return !control.readOnly;
  if (control instanceof HTMLInputElement) return !control.readOnly && !/^(checkbox|radio|button|submit|reset|range|color|file|image)$/.test(control.type);
  return control instanceof HTMLElement && control.isContentEditable;
}

/** The screen as one word, as an app's frame is told it (its own width may be less than the window's). */
export function screenKind(): "phone" | "touch" | "desktop" {
  if (!touchScreen()) return "desktop";
  return phoneScreen() ? "phone" : "touch";
}

/** Calls `onChange` whenever the screen turns touch or not, phone or not. */
export function followScreenKind(onChange: () => void) {
  const queries = [TOUCH_SCREEN_QUERY, PHONE_WIDTH_QUERY, PHONE_SCREEN_QUERY].map((query) => window.matchMedia(query));
  queries.forEach((query) => query.addEventListener("change", onChange));
  return () => queries.forEach((query) => query.removeEventListener("change", onChange));
}

function mark(name: string, on: boolean) {
  if (on) document.body.setAttribute(name, "true");
  else document.body.removeAttribute(name);
}

/**
 * Marks the body as a touch screen and a phone where they are, from now on,
 * and as an iPhone's or iPad's (`data-apple-touch`, as the app's frame is
 * marked), whose page foot differs from other phones' as Notion's does
 * (PageView .doc).
 */
export function markScreenKind() {
  mark("data-apple-touch", appleTouchDevice());
  const update = () => {
    mark("data-touch", touchScreen());
    mark("data-phone", phoneScreen());
  };
  update();
  followScreenKind(update);
}
