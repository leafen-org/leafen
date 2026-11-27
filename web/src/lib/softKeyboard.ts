// On real phones the visual viewport ends above the soft keyboard. It shows
// by what shows of the window being 120px or more shorter than it has been at
// its width: iPhone Safari keeps the window's height and shortens only the
// visual viewport (seen 2026-11-11 at 402: 714 and 384), Android Chrome
// shortens the window with it (iosViewport.ts's resizes-content). The tallest
// height is kept from the app's start, not from an editor's: a page opened
// while the keyboard is up would otherwise take the keyboard's height as the
// window's (2026-11-01: a new page's tools stayed under its line).
const tallest = new Map<number, number>();

function viewportHeight() {
  return window.visualViewport?.height ?? window.innerHeight;
}

function record() {
  const width = window.innerWidth, height = viewportHeight();
  if (height > (tallest.get(width) ?? 0)) tallest.set(width, height);
}

/** Starts keeping the window's tallest height at each width, and on a touch
 * screen how far a page's scroll moved as a keyboard came (keyboardPush). */
export function watchSoftKeyboard() {
  record();
  window.visualViewport?.addEventListener("resize", record);
  window.addEventListener("resize", record);
  if (!window.matchMedia?.("(pointer: coarse)")?.matches) return;
  const listening = { capture: true, passive: true };
  window.visualViewport?.addEventListener("resize", noteKeyboard);
  document.addEventListener("scroll", noteScroll, listening);
  document.addEventListener("selectionchange", notePlace);
  window.addEventListener("touchstart", () => { if (noteKeyboard()) touched = true; }, listening);
}

/**
 * How far a page's scroll (`[data-page-scroll]`) moved its lines up as the
 * soft keyboard came over the line being typed on: the browser brings that
 * line into what shows (Android Chrome scrolls it to the middle of the window
 * the keyboard shortened, seen 2026-11-21 on a Galaxy A90: a line at 653 to
 * 238), where Notion's browser pushes its whole window up behind the
 * keyboard (offsetTop 285 and 329.5) — its lines keep their places in its
 * window, which choose a line's list's side (popoverPlacement). Counted while
 * the keyboard comes, for a second and until a finger touches, from where the
 * scroll rested with the keyboard down; at most the keyboard's height, as far
 * as Notion's window is pushed; none once the keyboard goes.
 */
export function keyboardPush(page: Element | null | undefined) {
  const push = page && noteKeyboard() ? pushes.get(page) : undefined;
  return push && push.came === came ? Math.min(push.by, (tallest.get(window.innerWidth) ?? 0) - viewportHeight()) : 0;
}
const resting = new WeakMap<Element, number>(), pushes = new WeakMap<Element, { by: number; came: number }>();
let up = false, came = 0, touched = false;
function noteKeyboard() {
  const shown = softKeyboardShown();
  if (shown && !up) { came = performance.now(); touched = false; }
  return up = shown;
}
function noteScroll(event: Event) {
  const page = event.target instanceof Element && event.target.matches("[data-page-scroll]") ? event.target : null;
  if (!page) return;
  if (!noteKeyboard()) { resting.set(page, page.scrollTop); return; }
  const was = resting.get(page);
  if (was !== undefined && !touched && performance.now() - came < 1000) pushes.set(page, { by: Math.max(0, page.scrollTop - was), came });
}
// Where a page's scroll rests as its line is chosen with the keyboard down.
function notePlace() {
  const page = noteKeyboard() ? null : document.activeElement?.closest("[data-page-scroll]");
  if (page) resting.set(page, page.scrollTop);
}

/** Whether a soft keyboard takes the window's foot now. */
export function softKeyboardShown() {
  record();
  return viewportHeight() < (tallest.get(window.innerWidth) ?? 0) - 120;
}

/** How much of the window's tallest height a soft keyboard takes now, its suggestion strip with it; none with none up. */
export function softKeyboardHeight() {
  return softKeyboardShown() ? (tallest.get(window.innerWidth) ?? 0) - viewportHeight() : 0;
}

/**
 * Runs `then` once a soft keyboard is up, or after `within` ms with none (a
 * hardware keyboard): a phone lets the keyboard a tap raised go if the caret
 * is moved before it is up (seen 2026-11-09 in iOS Safari).
 */
export function afterSoftKeyboard(then: () => void, within = 800) {
  const viewport = window.visualViewport;
  let done = false;
  const finish = () => {
    if (done) return;
    done = true;
    viewport?.removeEventListener("resize", check);
    window.clearTimeout(timer);
    then();
  };
  const check = () => { if (softKeyboardShown()) finish(); };
  viewport?.addEventListener("resize", check);
  const timer = window.setTimeout(finish, within);
  check();
}

/**
 * The window's height with its browser's bars drawn in their smallest (CSS
 * `100lvh`): iPhone Safari's 754 at 402 wide, where the window is 714 with
 * its bars and 384 over the keyboard (seen 2026-11-13); Android Chrome's
 * 837.3 at 411, where the window is 781.3 (seen 2026-11-14 on a Galaxy A90).
 * A keyboard that shortens the window (Android Chrome, resizes-content)
 * shortens `100lvh` with it, by as much: that is added back, by the window's
 * tallest height since the app started (2026-11-14: the "@" list measured 508
 * there and stood 152 high where Notion's is 251.2).
 */
const large = new Map<number, number>();
export function largeViewportHeight() {
  const width = window.innerWidth, known = large.get(width);
  if (known) return known;
  const probe = document.createElement("div");
  probe.style.cssText = "position:fixed;top:0;width:0;height:100lvh;visibility:hidden;pointer-events:none";
  document.body.appendChild(probe);
  const height = probe.getBoundingClientRect().height || window.innerHeight;
  probe.remove();
  const whole = height + keyboardShortening();
  large.set(width, whole);
  return whole;
}

/**
 * How much a soft keyboard shortens the window now, its suggestion strip
 * with it: Android Chrome's (resizes-content) by the window's tallest height
 * at its width less its height now (seen 2026-11-17 on a Galaxy A90 at 411:
 * 781.3 to 388); none where the keyboard lays over the window (iPhone Safari
 * keeps the window's 714) or none is up.
 */
export function keyboardShortening() {
  if (!softKeyboardShown()) return 0;
  const shortened = (tallest.get(window.innerWidth) ?? 0) - window.innerHeight;
  return shortened >= 120 ? shortened : 0;
}

/**
 * Keeps keyboardShortening on `sheet` as --keyboard-shortening while it is
 * open, written only when it changed and only on the sheet, not the root
 * (visualViewportPlace.ts says why); returns what stops it.
 */
export function followKeyboardShortening(sheet: HTMLElement | null | undefined): () => void {
  if (!sheet || typeof window === "undefined") return () => {};
  let written = "";
  const place = () => {
    const next = `${keyboardShortening()}px`;
    if (next !== written) sheet.style.setProperty("--keyboard-shortening", written = next);
  };
  window.addEventListener("resize", place);
  window.visualViewport?.addEventListener("resize", place);
  place();
  return () => {
    window.removeEventListener("resize", place);
    window.visualViewport?.removeEventListener("resize", place);
    sheet.style.removeProperty("--keyboard-shortening");
  };
}
