import { holdTypingAfterPress, stopWaitingToHold, takesTyping, waitingToHold } from "./holdTypingAfterPress";
import { type HeldKey, type HeldPiece, heldKey, playHeldTyping, playHeldTypingOnceReady, pressHeldKeys } from "./heldTypingPlayback";
import { fullPageDatabasePage } from "./inlineDatabaseAddress";
import { shutBehindModal } from "./modalFocus";
import { touchScreen } from "./screenKind"; import { dropTapWhileOpening } from "./tapWhileOpening"; import { noteTitleFocusAsked } from "./titleFocusAsked";

let pending: { pageId: string; cancel: () => void } | null = null;

/**
 * Whether a view's "+ 새 페이지" makes its row by opening the row's own page,
 * its empty title taking the caret, rather than a row or card to name in
 * place: on a touch screen, a phone's or a tablet's, as Notion's (seen
 * 2026-11-14 in iPad Safari 834: a table's, a board column's and a list's
 * "+ 새 페이지" opened the new row's whole page, its title focused; a
 * phone's table the same at 402).
 */
export function newRowOpensItsPage() {
  return touchScreen();
}

/**
 * A newly made page takes title focus once, when its own header mounts. What
 * was held for it and isn't taken — a press elsewhere, before or after the
 * page was made, or the page left before its header came — goes to the page's
 * keeper when it has one (keepUntakenTyping: a view waiting for its new row
 * keeps it as the row's title), rather than nowhere (2026-11-07: on a stalled
 * network a new row's page waited for the server, and its typed title went
 * once the person pressed elsewhere or the page was left).
 */
export function requestPageTitleFocus(pageId: string, keep?: (text: string) => void) {
  pending?.cancel();
  // Its body was pressed while it was being made: the caret goes there.
  if (bodyPressedWhileMaking) { noteTitleFocusAsked(pageId); return; }
  // Pressed elsewhere while it was being made: what was held goes to the
  // page's keeper, now or once it comes; without one the page's title takes it.
  if (held && heldLeft && !giveHeldTo(pageId, keep)) untaken = { pageId, keep };
  const cancel = () => {
    window.removeEventListener("leafen:routechange", onRoute);
    window.removeEventListener("popstate", onRoute);
    document.removeEventListener("pointerdown", leave, true);
    if (pending?.cancel === cancel) pending = null;
  };
  const leave = (event: PointerEvent) => {
    if (event.target === held) return;
    cancel(); noteTitleFocusAsked(pageId);
    if (held && !giveHeldTo(pageId, keep)) untaken = { pageId, keep };
  };
  const onRoute = () => {
    const url = new URL(window.location.href);
    // A full-page database's page shows at its database's address, and peeks by it.
    const named = url.searchParams.get("p") ?? url.pathname.split("/")[2];
    const shown = named && (fullPageDatabasePage(named) ?? named);
    if (shown !== pageId) {
      cancel();
      if (holdingTyping() && !giveHeldTo(pageId, keep)) dropHeldTyping();
    }
  };
  pending = { pageId, cancel };
  window.addEventListener("leafen:routechange", onRoute);
  window.addEventListener("popstate", onRoute);
  document.addEventListener("pointerdown", leave, true);
  // A page that opened while it was being made already shows its header,
  // which takes the request now.
  window.dispatchEvent(new CustomEvent(PAGE_TITLE_FOCUS_EVENT, { detail: pageId }));
}

// Where what was typed for a page goes when the page doesn't take it, by
// page id; and the page whose held typing waits for its keeper to come.
const keepers = new Map<string, (text: string) => void>();
let untaken: { pageId: string; keep?: (text: string) => void } | null = null;

/** Gives what is held to the page's keeper, when it has one, and says whether it did. */
function giveHeldTo(pageId: string, keep?: (text: string) => void) {
  const kept = keep ?? keepers.get(pageId);
  if (!held || !kept) return false;
  const text = held.value;
  dropHeldTyping();
  if (text.trim()) kept(text);
  return true;
}

/** Keeps what was typed for this page and not taken by it with keep, until the returned stop. */
export function keepUntakenTyping(pageId: string, keep: (text: string) => void): () => void {
  keepers.set(pageId, keep);
  if (untaken?.pageId === pageId && held) giveHeldTo(pageId, untaken.keep ?? keep);
  return () => {
    if (keepers.get(pageId) === keep) keepers.delete(pageId);
  };
}

/** Sent when a page asks for its title's focus, its id in `detail`. */
export const PAGE_TITLE_FOCUS_EVENT = "leafen:pagetitlefocus";

/** Whether the page is asked to take its title's focus, the ask left for consumePageTitleFocus. */
export function pageTitleFocusAsked(pageId: string) {
  return pending?.pageId === pageId;
}

export function consumePageTitleFocus(pageId: string) {
  if (pending?.pageId !== pageId) return false;
  pending.cancel(); noteTitleFocusAsked(pageId);
  return true;
}

// How long a page's title is waited for while its page is asked to take the
// caret: a page from the add window reads as read-only while it loads, and a
// new database's name until its view has come (2026-11-09: an empty database
// from the add window gave up after 30 frames and typing went nowhere).
const TITLE_WAIT_MS = 10_000;

/**
 * Calls `then` with the page's title once it can take the caret — editable, shown (not
 * in a head hidden as its body is first read, usePageBodyReading), not behind a closing
 * window — while that page is still asked for it (requestPageTitleFocus). Returns what stops the wait.
 */
export function whenPageTitleCanTakeFocus(pageId: string, title: () => HTMLElement | null, then: (title: HTMLElement) => void) {
  const started = performance.now();
  let frame = 0;
  const check = () => {
    const element = title();
    if (element?.isContentEditable && getComputedStyle(element).visibility === "visible" && !shutBehindModal(element)) { then(element); return; }
    if (pending?.pageId === pageId && performance.now() - started < TITLE_WAIT_MS) frame = window.requestAnimationFrame(check);
  };
  check();
  return () => window.cancelAnimationFrame(frame);
}

// What is typed while a new page is being made waits in a field of its own
// and goes into the new page's title once it opens, as Notion's new page
// takes typing at once (2026-10-16: typing right after "new page" went into
// the page being left, replacing a block selected there, or lost its first
// words). The field takes IME composition like any text field.
//
// It stands in the top layer, which a modal window's isolation passes over:
// made inert by a center peek opening for the new page, it lost the focus,
// what came next landed on nothing or on the window's first button, and an
// Enter there left for the full page with the typing (2026-11-04).
let held: HTMLTextAreaElement | null = null;
// Whether a press went elsewhere while something was held.
let heldLeft = false;
function noteLeftHeld(event: PointerEvent) { if (held && event.target !== held) heldLeft = true; }
// What a paste into the field brought, or a key pressed there that is no
// typing (heldKey: a shortcut, Escape, Tab, an arrow), and where among what
// was typed: a paste can't be read once its event is over.
let heldPastes: { at: number; data: DataTransfer | HeldKey }[] = [];
// Ends the wait of the last holdTypingUntilFocus, if one is on, and asks
// again for the field it waits for.
let stopWaitingForFocus: (() => void) | null = null;
let askAgain: (() => void) | null = null;
// How often what waits for a field asks again besides when the page's body
// says it can take the caret (pageBodyReady): for what draws in the body
// after it, a database's cell.
const ASK_AGAIN_MS = 500;

/**
 * The opened page's body can take the caret now, its lines drawn (Editor):
 * what was pressed or typed for it while it opened goes there in this frame,
 * not at the next beat of a wait (2026-11-13: asked every 150 ms, a press on
 * a page still opening put its caret down up to 0.15 s after the body
 * showed, on a NAS-class server).
 */
export function pageBodyReady() {
  askAgain?.();
}
// Whether a syllable is being composed in the field.
let composing = false;
// How long a syllable left half made holds up the title or body taking over.
const COMPOSITION_PAUSE_MS = 800;

function keepPaste(event: ClipboardEvent) {
  if (!held || !event.clipboardData) return;
  event.preventDefault();
  const data = new DataTransfer();
  for (const type of event.clipboardData.types) {
    if (type !== "Files") data.setData(type, event.clipboardData.getData(type));
  }
  heldPastes.push({ at: held.selectionStart, data });
}

function keepHeldKey(event: KeyboardEvent) {
  const last = heldPastes.at(-1), keyLast = last && !(last.data instanceof DataTransfer) && last.at >= (held?.value.length ?? 0);
  const key = held ? heldKey(event, keyLast ? "key" : held.value || heldPastes.length ? "typed" : "none") : null;
  if (!key || !held) return;
  event.preventDefault(); event.stopImmediatePropagation();
  heldPastes.push({ at: held.selectionStart, data: key });
}

/**
 * Starts holding what is typed until the new page's title takes it; `focus`
 * false holds it without taking the focus, so a touch screen's keyboard
 * stays down (holdTypingWhileRowOpens).
 */
export function holdTypingForNewPage(focus = true) {
  if (typeof document === "undefined") return;
  stopWaitingForFocus?.();
  stopWaitingToHold();
  if (!held) {
    held = document.createElement("textarea");
    held.setAttribute("aria-hidden", "true");
    held.tabIndex = -1;
    held.dataset.newPageTypingHold = "true";
    held.dataset.reactAriaTopLayer = "true";
    Object.assign(held.style, { position: "fixed", left: "-10000px", top: "0", width: "1px", height: "1px", opacity: "0" });
    held.addEventListener("paste", keepPaste);
    held.addEventListener("keydown", keepHeldKey);
    held.addEventListener("compositionstart", () => { composing = true; });
    held.addEventListener("compositionend", () => { composing = false; });
    document.body.append(held);
  }
  held.value = "";
  heldPastes = [];
  heldLeft = false;
  untaken = null;
  document.addEventListener("pointerdown", noteLeftHeld, true);
  if (focus) held.focus({ preventScroll: true });
}

/**
 * Holds what is typed while make() makes a page that opens at its title
 * (2026-10-29: a sidebar page's "+" or a database's "새로 만들기" lost what was
 * typed before the page opened); when make() settles without asking for a
 * title (requestPageTitleFocus), what was held is let go.
 */
export async function holdTypingWhile<T>(make: () => Promise<T>, focus = true): Promise<T> {
  const before = pending;
  holdTypingForNewPage(focus);
  const field = held;
  if (!making) madeFrom = window.location.href;
  making += 1;
  try {
    return await make();
  } finally {
    making -= 1;
    if (!making) {
      bodyPressedWhileMaking = false;
      // A body pressed meanwhile is asked now that it is the page's.
      askAgain?.();
    }
    if (field && held === field && !stopWaitingForFocus && !untaken && (!pending || pending === before)) dropHeldTyping();
  }
}

/**
 * Holds typing while make() makes a database's row that opens at its page's
 * title. On a touch screen nothing takes the focus at the press, so the
 * keyboard stays down and the title takes the caret once the page opens, as
 * Notion's (seen 2026-11-14 in iPhone Safari at 402pt and on an iPad at 834:
 * "+ 새 페이지" left the new row's empty title focused, the screen its whole
 * height); a pointer's screen holds what is typed as for any new page.
 */
export function holdTypingWhileRowOpens<T>(make: () => Promise<T>): Promise<T> {
  return holdTypingWhile(make, !touchScreen());
}

// Pages being made right now (holdTypingWhile).
let making = 0;
// Whether what stood over the page being made was pressed meanwhile
// (pressPageBeingMade), and the address shown as the making began.
let bodyPressedWhileMaking = false;
let madeFrom = "";

/**
 * A press on what stands over a page being made at its title
 * (holdTypingWhile) — the add window, still making it — is a press on the
 * page's body, as on Notion's, whose window is gone by then: the body takes
 * the caret there once the page opens, not the title, and what is typed
 * meanwhile waits for it (2026-11-13: a press on the window in that moment
 * took the focus from what was held, and the typing after it went nowhere).
 * Says whether a page was being made.
 */
export function pressPageBeingMade(point: { x: number; y: number }) {
  if (!making) return false;
  bodyPressedWhileMaking = true;
  pending?.cancel();
  holdTypingForBody(point, madeFrom);
  return true;
}

/**
 * Asked of the opened page's editor, with the point pressed while it opened:
 * the caret to its line there, or to the page's end below its lines.
 */
export const PAGE_BODY_PRESS_EVENT = "leafen:pagebodypress";

/**
 * The frame of the page opened in the app: the main's, or that of a row's page
 * opened full, which stands over the main outside it in the document (a
 * portal) while React passes its presses through the main's.
 */
export const OPENED_PAGE_FRAME = '[data-app-main], [data-row-peek-panel][data-mode="full"]';

// The opened page's frame a press landed in: a full row page's own, else the main's.
const pressedFrame = (main: Element, target: EventTarget | null) =>
  (target instanceof Element ? target.closest(OPENED_PAGE_FRAME) : null) ?? main;

/** Whether a press in the page's frame comes before the page's own editor can take it. */
function pageStillOpening(frame: Element) {
  return making > 0 || (window.location.pathname.startsWith("/p/")
    && (!frame.querySelector("[data-editor-root]") || Boolean(frame.querySelector('[aria-busy="true"][data-loading="true"]'))));
}

// The press on the page's frame that came before the page could take it.
let openingPress: PointerEvent | null = null;

/**
 * Notes a press on the page's frame, and whether it came before the page could
 * take it — asked of the frame pressed, as a full row page's editor stands in
 * its own (2026-11-12: the main, holding none, took every press on a full row
 * page's room as one made while it opened, and a press below its body, beside
 * its lines or by its head put no caret down).
 */
export function notePageFramePress(main: Element, event: PointerEvent) {
  openingPress = event.button === 0 && pageStillOpening(pressedFrame(main, event.target)) ? event : null;
}

/**
 * Whether this press came before the page could take it: it is the opened
 * page's (takePageFramePress), not for what shows meanwhile (2026-11-10: a
 * body still being read took it as a press below its lines and made a line
 * above the page's first, its caret nowhere).
 */
export function pressedWhileOpening(event: Event) {
  return openingPress !== null && openingPress === event;
}

/** The click of a press noted on the page's frame: one nothing took on a page still opening is the opened page's, but a tap (dropTapWhileOpening) unless a page is being made. */
export function takePageFramePress(main: Element, target: EventTarget) {
  const press = openingPress;
  openingPress = null;
  const active = document.activeElement;
  if (!press || (active !== main && active !== pressedFrame(main, target) && active !== document.body) || !(target instanceof Element)
    || target.closest('button, a, input, textarea, select, [role="button"], [contenteditable="true"]') || (!making && dropTapWhileOpening(press))) return;
  holdTypingForBody({ x: press.clientX, y: press.clientY });
}

/**
 * A press on a page still opening went nowhere, with what was typed next
 * (2026-11-05, right after "new page"), or to the page's end, far below where
 * it was pressed (2026-11-10): what is typed waits, and the body takes the
 * caret where it was pressed as soon as it is the opened page's — Notion's
 * first press on a page just opened puts it there (seen 2026-11-10).
 */
function holdTypingForBody(point: { x: number; y: number }, leaving = making > 0 ? window.location.href : null) {
  // While a page is being made the one showing is being left: it isn't asked.
  const ask = () => {
    if (!making && window.location.href !== leaving) window.dispatchEvent(new CustomEvent(PAGE_BODY_PRESS_EVENT, { detail: point }));
  };
  holdTypingUntilFocus(held?.value ?? "", null, ask);
  const field = held;
  ask();
  afterFocusWait(() => { if (field && held === field) dropHeldTyping(); });
}

/** Whether what is typed is being held for a page opening now, or is to be once the press is over. */
export function holdingTyping() {
  return waitingToHold() || (held !== null && document.activeElement === held);
}

/** Whether a syllable is being made in what is held. */
export function holdingComposition() { return composing && holdingTyping(); }

/** Whether this is the field holding what is typed, with nothing in it yet: no letter, paste, key or syllable. */
export function holdsNothingYet(target: EventTarget | null) { return held !== null && target === held && !held.value && !heldPastes.length && !composing; }

/**
 * While a syllable is being made in what is held, says so and calls then when
 * it is done, or left half made for a moment: moving the focus now would cut
 * it (ㅂ here and ㅗ there, 2026-11-04).
 */
export function waitForHeldComposition(then: () => void) {
  if (!holdingComposition() || !held) return false;
  const field = held;
  const go = () => {
    field.removeEventListener("compositionend", go);
    window.clearTimeout(timer);
    then();
  };
  const timer = window.setTimeout(go, COMPOSITION_PAUSE_MS);
  field.addEventListener("compositionend", go);
  return true;
}

/** Calls then now, or once a syllable being made in what is held is done. */
export function afterHeldComposition(then: () => void) {
  if (!waitForHeldComposition(then)) then();
}

/** What was held, in the order it came, and holding stops. */
function takeHeldTyping(): HeldPiece[] {
  stopWaitingForFocus?.();
  const text = held?.value ?? "", pieces: HeldPiece[] = [];
  let from = 0;
  for (const { at, data } of heldPastes) {
    if (at > from) pieces.push(text.slice(from, at));
    pieces.push(data);
    from = Math.max(from, at);
  }
  if (from < text.length) pieces.push(text.slice(from));
  held?.remove();
  held = null;
  heldPastes = [];
  composing = false;
  heldLeft = false;
  untaken = null;
  document.removeEventListener("pointerdown", noteLeftHeld, true);
  return pieces;
}

/** What was held is let go: the page it was for won't open. A shortcut pressed after it is sent on. */
export function dropHeldTyping() {
  stopWaitingToHold();
  pressHeldKeys(takeHeldTyping());
}

// How long a field that was to take the focus is waited for, and how long at
// most while a page body it is to be in is still being read.
const FOCUS_WAIT_MS = 5000;
const BODY_READ_WAIT_MS = 30000;

/**
 * Calls then when a field has had its time to take the focus. A body still
 * being read, or still coming (stillComing), has none to give it yet, for as
 * long as a busy machine takes it (2026-11-05: past a few seconds a title's
 * Enter was lost, and what was typed after it went into the title; 2026-11-06:
 * so it was while a new page's body hadn't come at all). Gives what cancels
 * the wait.
 */
function afterFocusWait(then: () => void, stillComing?: () => boolean) {
  const started = Date.now();
  let timer = 0;
  const check = () => {
    const reading = document.querySelector('[aria-busy="true"][data-loading="true"]') || stillComing?.();
    if (reading && Date.now() - started < BODY_READ_WAIT_MS) timer = window.setTimeout(check, FOCUS_WAIT_MS);
    else then();
  };
  timer = window.setTimeout(check, FOCUS_WAIT_MS);
  return () => window.clearTimeout(timer);
}

/**
 * Holds what is typed until another field takes focus, then types it there:
 * a title's Enter goes to the body, which may still be opening (2026-10-29:
 * typing right after it stayed in the title). It starts with what is given;
 * meanwhile it asks again for the field to take focus (retry), and when
 * nothing does within a few seconds, what was held goes back to the field it
 * came from, at its end.
 */
function holdTypingUntilFocus(start = "", from: HTMLElement | null = null, retry?: () => void, stillComing?: () => boolean) {
  holdTypingForNewPage();
  const field = held;
  if (!field) return;
  field.value = start;
  const stop = () => {
    document.removeEventListener("focusin", deliver, true);
    field.removeEventListener("compositionend", ask);
    cancelWait();
    window.clearInterval(again);
    if (stopWaitingForFocus === stop) stopWaitingForFocus = null;
    if (askAgain === ask) askAgain = null;
  };
  const deliver = (event: FocusEvent) => {
    if (event.target === field) return;
    const pieces = takeHeldTyping();
    if (pieces.length) playHeldTypingOnceReady(pieces);
  };
  // Asked again once the body can take it (pageBodyReady), once a syllable
  // being made is done, and at a slow beat besides.
  const ask = () => { if (document.activeElement === field && !composing) retry?.(); };
  const again = retry ? window.setInterval(ask, ASK_AGAIN_MS) : 0;
  if (retry) field.addEventListener("compositionend", ask);
  const cancelWait = from ? afterFocusWait(() => {
    const back = document.activeElement === field && from.isConnected ? from : null;
    const pieces = takeHeldTyping();
    if (back) {
      back.focus();
      document.getSelection()?.selectAllChildren(back);
      document.getSelection()?.collapseToEnd();
      playHeldTyping(pieces);
    }
  }, stillComing) : () => {};
  document.addEventListener("focusin", deliver, true);
  stopWaitingForFocus = stop;
  askAgain = retry ? ask : null;
}

/**
 * Holds what is typed while make() makes something whose own field then takes
 * the focus (a table's new row's title); it is typed there. When make() brings
 * nothing, what was held goes where back() puts the focus, if it does; else,
 * and when no field takes it within a few seconds, it is let go. Holding
 * begins at once (a letter put in with no key reaches only a field that can be
 * written in), but for a field made and focused in the press itself (madeNow:
 * a list's new row made on this device) and for text being written when it is
 * pressed (the page's, which a table's "+ 새 페이지" leaves the focus in),
 * which keeps the focus: then only at a key or a letter put in with no key, or
 * once the press's first frame is over with no field focused (holdTypingAfterPress;
 * focusing the field that holds in the press was a page-wide style pass, 2026-11-13,
 * and had the page's editor let go of the focus, 2026-11-15). Where make()'s
 * result says its place in that same text has the caret (inPlace: no focus moves
 * there), holding not begun by then doesn't (2026-11-16: "/콜아웃" Enter, its line
 * drawn a frame on, had the caret, and a hold begun after it took the focus away).
 */
export async function holdTypingForField<T>(make: () => Promise<T>, back?: () => void, madeNow = false, inPlace = false): Promise<T> {
  let field: HTMLTextAreaElement | null = null;
  const begin = () => { holdTypingUntilFocus(); field = held; };
  const stopWaiting = madeNow || takesTyping(document.activeElement)
    ? holdTypingAfterPress(begin, (target) => target === held, (text) => { if (held) held.value += text; }, keepHeldKey) : (begin(), () => {});
  const letGo = () => { if (field && held === field) dropHeldTyping(); };
  try {
    const made = await make();
    if (made) { if (inPlace) stopWaiting(); window.setTimeout(letGo, FOCUS_WAIT_MS); } else {
      stopWaiting();
      if (field && held === field) back?.();
      letGo();
    }
    return made;
  } catch (error) {
    stopWaiting();
    letGo();
    throw error;
  }
}

/**
 * A new page's title has taken focus with its caret at the end: what was held
 * while the page was made goes into it as typed there.
 */
export function typeHeldTypingIntoTitle() {
  playHeldTyping(takeHeldTyping());
}

/**
 * Where a title sends the caret in its body: the first line ("start", an
 * arrow key), the first line selected ("select", select-all), or a new first
 * line ("new", Enter — Notion's, seen 2026-11-01: typed above the lines
 * already there), made with no caret in a body kept unseen ("unseen",
 * pageBodyFrame.ts). "again" is that Enter asking once more while what is
 * typed waits for the body: it goes to the line the Enter made, made now only
 * if the body had none to give it then (2026-11-07: on a busy machine each
 * ask made one more empty line, a letter in one and the rest in another).
 */
export type BodyFocus = "start" | "select" | "new" | "again" | "unseen";

/**
 * A title's Enter goes to the body of its page, which may still be opening:
 * what is typed meanwhile (after what is given) waits for it, as long as the
 * body hasn't come or is still being read.
 */
export function enterBodyFromTitle(title: HTMLElement | null, focusBody: (selection: BodyFocus) => void, typed = "", pageId?: string) {
  const bodyComing = () => {
    if (!pageId) return false;
    const body = document.querySelector(`[data-editor-page="${CSS.escape(pageId)}"]`);
    return !body || body.getAttribute("data-loading") === "true";
  };
  holdTypingUntilFocus(typed, title, () => focusBody("again"), bodyComing);
  focusBody("new");
}

/**
 * On a phone, a page just made scrolls its title up to where the options over
 * it stood, as Notion's (seen 2026-11-01: 40px, the title just under the top
 * bar and "아이콘 추가 · 커버 추가 · 댓글 추가" scrolled away above it).
 */
export function scrollPastPageOptionsOnPhone(title: HTMLElement) {
  if (!title.closest('[data-phone="true"]')) return;
  const scroller = title.closest<HTMLElement>("[data-page-scroll]");
  const options = scroller?.querySelector<HTMLElement>("[data-page-header-controls]");
  if (!scroller || !options) return;
  scroller.scrollTop += title.getBoundingClientRect().top - options.getBoundingClientRect().top;
}
