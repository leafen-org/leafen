// A control drawn as an icon alone is named in Notion's dark tip, not the
// browser's own title tip (2026-10-28, beside Notion and Hermes desktop: a
// white system tip a second late on every icon button). One boundary for
// every such control: pointing at it, or the keyboard focusing it, takes its
// `title` — kept as its accessible name — and shows the tip below it, above
// when below has no room by the dark tip's edge rule (tipPlacement.ts): the
// keyboard's at once, the pointer's once it has rested there Notion's wait —
// 150 ms, or what the control's `data-tip-wait` names — fading in and out as
// Notion's (tipTiming.ts). A control with words of its own needs no tip and
// loses the title, unless it names one itself in `data-icon-tip`; a title
// reading its words whole where they are cut stays, as cut-off text keeps its
// whole in a title (overflowTitles.ts).
// `data-tip-keys` adds a shortcut
// on a dimmed line under the name, as Notion's tips do (seen 2026-10-28). A
// name may run over lines and set words in bold, written with "\n" and
// "**", as a table's "+" bars (seen 2026-10-31). The
// keyboard shows a tip on a control Tab moved to, and on one focus came back
// to as Escape closed the menu it opened, as Enter chose in the window it
// opened (Notion's move window, seen 2026-11-06), or with the keyboard's ring
// as a press outside closed the window keys were typed into (Notion's "필터"
// after a value typed into its pill, seen 2026-11-15) — not a panel beside the page
// (Notion, seen 2026-11-01: the page "⋯", a database's filter and sort tip
// then; its view settings, inbox and comments don't) — nor ever on a touch
// screen; or on one marked `data-tip-on-focus`, which a window focuses as it
// opens and Notion tips then (its teamspace window's "…", seen 2026-10-28).

import styles from "@/components/IconTipButton.module.css";
import { focusGivenAsByKeys, keysLedFocus, keysLedToLastPress } from "./lastPress";
import { topLayerTakesEscape } from "./overlayLayer";
import { readsRightToLeft } from "./readingDirection";
import { TIP_EDGE, tipFitsAbove, tipFitsBelow } from "./tipPlacement";
import { fadeTipOut, tipWaitOf } from "./tipTiming";
import { fieldEnter, imeComposing } from "./fieldKeyBoundary";

// A picture that names itself in a tip with a larger copy of it, as Notion's
// workspace emoji in text (seen 2026-11-07): `data-tip-image` is the picture.
const PICTURE = "data-tip-image";
// A dimmed word after the name on the same line (Notion's "(삭제됨)" after a
// deleted emoji's name, seen 2026-11-07): `data-tip-note`.
const NOTE = "data-tip-note";
const CONTROL = `button, a[href], summary, [role=button], [role=tab], [role=menuitem], [role=option], [role=switch], [role=checkbox], [role=radio], [${PICTURE}], [${NOTE}]`;
const NAME = "data-icon-tip";
const QUIET = "data-tip-quiet";
// A control the keyboard's focus tips and the pointer doesn't, as Notion's "입력 값 지우기" (seen 2026-11-17 under the
// pointer, no tip; 2026-11-26 Tab to it, its tip, in two tabs): `data-tip-keys-only`.
const KEYS_ONLY = "data-tip-keys-only";
// A sentence that wraps at 200px, as Notion's tip on its top bar's "오프라인"
// (seen 2026-11-14: 216 wide with its 8px sides): `data-tip-wrap`.
const WRAP = "data-tip-wrap";
// A sentence as wide as its words up to 300px, then wrapped, as Notion's tip
// on a red automation card's head (seen 2026-11-21: 300 wide, two lines): `data-tip-wide`.
const WIDE = "data-tip-wide";
// `data-tip-under` names a part of the control the tip goes 6px under, still
// centred on the whole control: Notion's tip on a red automation card's head
// stands under the head's 20px row, centred on the card (seen 2026-11-21 in a
// database button's editor and a database automation's).
const UNDER = "data-tip-under";
// Our own accessible name, kept up with the title it came from.
const LABEL = "data-icon-tip-label";
const GAP = 6;
const MARGIN = 8;
// A tip under a control by the window's side keeps 4px from it, as Notion's
// (seen 2026-11-13, 1440: the sidebar's close at 4.2, a peek's "…" ending at 1436.6).
const SIDE_MARGIN = TIP_EDGE;

let tip: HTMLSpanElement | null = null;
let owner: HTMLElement | null = null;
// The control the pointer rests on while its tip's wait runs.
let pending: HTMLElement | null = null;
let waiting = 0;
let watching = false;
let checking = 0;
// The last key was Tab: the focus that follows was moved there by hand.
let tabbed = false;
// The tip up came with a control Tab moved to: it takes the next Escape alone, the menu or window under it staying
// (Notion's, seen 2026-11-26 in Chrome 1440 in two tabs: a page "⋯"'s search, words typed, Tab to its "입력 값 지우기"
// tipped; Escape took the tip, the menu stayed, a second Escape closed it; so a restore panel over a page's history,
// Tab onto a control under it — the first Escape did nothing seen, the second closed the panel).
let tipTabbed = false;
// The last key was Escape in a menu (not a panel beside the page): the focus
// that follows came back to what opened it.
let escapedMenu = false;
// The last key was typed into a panel beside the page, not a window over it.
let typedInPanel = false;
// When the focus last came in, on the events' clock: a focus given while a
// key is on its way comes after that key's time.
let focusedAt = -Infinity;
// A panel beside the page: the inbox, the comments, a view's settings. A
// page shown in a peek, side, center or whole, is a page and no panel: its top
// bar's "⋯" tips as its menu closes on Escape, as the page's own does
// (Notion's side peek, seen 2026-11-14 and 2026-11-16 in Chrome at 1440, the
// pointer still in the bar, no script before the key).
const PANEL = "aside:not([data-mode]), [data-docked-panel]";

// Words of its own: a letter at least — an unread count beside an icon isn't,
// nor a glyph drawn in letters and hidden from readers (the text menu's "B"),
// nor a name kept for readers alone, clipped out of sight (a database
// toolbar's "필터" and "정렬", which Notion tips — seen 2026-11-01).
function words(element: HTMLElement) {
  if (!/\p{L}/u.test(element.innerText ?? element.textContent ?? "")) return false;
  const walker = document.createTreeWalker(element, NodeFilter.SHOW_TEXT);
  for (let node = walker.nextNode(); node; node = walker.nextNode()) {
    const hidden = node.parentElement?.closest("[aria-hidden='true']");
    if (hidden && element.contains(hidden)) continue;
    const box = node.parentElement?.getBoundingClientRect();
    if (box && box.width <= 1 && box.height <= 1) continue;
    if (/\p{L}/u.test(node.textContent ?? "")) return true;
  }
  return false;
}

// Whether the title reads whole words its control cuts — clipped by a box of
// their own, with an ellipsis or without — as overflowTitles.ts gives cut-off
// text its whole (2026-11-18: a settings item's German "Registrierung und
// Anmeldung", cut, lost the title it had just been given and showed nothing).
function readsCutWords(element: HTMLElement, title: string) {
  const whole = title.replace(/\s+/gu, " ").trim();
  const walker = document.createTreeWalker(element, NodeFilter.SHOW_TEXT);
  for (let node = walker.nextNode(); node; node = walker.nextNode()) {
    if (!/\p{L}/u.test(node.textContent ?? "")) continue;
    for (let box = node.parentElement; box && element.contains(box); box = box.parentElement) {
      const text = box.textContent?.replace(/\s+/gu, " ").trim() ?? "";
      if (text && whole.includes(text) && box.scrollWidth > box.clientWidth + 1 && getComputedStyle(box).overflowX !== "visible") return true;
      if (box === element) break;
    }
  }
  return false;
}

// Moves a control's title into the tip's name; false when it has words,
// whose title goes unless it reads them whole where they are cut.
function take(element: HTMLElement) {
  const title = element.getAttribute("title");
  if (title === null) return element.hasAttribute(NAME);
  if (words(element)) {
    if (!readsCutWords(element, title)) element.removeAttribute("title");
    element.removeAttribute(NAME);
    return false;
  }
  element.removeAttribute("title");
  element.setAttribute(NAME, title);
  const named = element.hasAttribute("aria-label") && !element.hasAttribute(LABEL);
  if (!named && !element.hasAttribute("aria-labelledby")) {
    element.setAttribute("aria-label", title);
    element.setAttribute(LABEL, "");
  }
  return true;
}

// A mark that isn't a control but is named in a title and has no words of
// its own — an ⓘ, a badge, a status icon, a person's picture — is named in
// the tip too, as Notion's are (2026-11-06: settings' ⓘ showed the browser's
// white tip a second late). Words keep their title: it names them whole
// where they are cut. A frame's or a field's title is its name, not a tip,
// and a stretch holding controls (a block's gutter) isn't a mark.
const NOT_A_MARK = "iframe, input, textarea, select";

function controlOf(target: EventTarget | null) {
  const element = target instanceof Element ? target.closest<HTMLElement>(`[title], [${NAME}]`) : null;
  if (!element || element.hasAttribute("data-overflow-title")) return null;
  if (!element.matches(CONTROL)) {
    if (element.hasAttribute(NAME)) return element;
    if (element.matches(NOT_A_MARK) || element.querySelector(CONTROL) || words(element)) return null;
    // Named for readers as the picture it is.
    if (!element.hasAttribute("role") && element.tagName !== "IMG") element.setAttribute("role", "img");
  }
  return take(element) ? element : null;
}

function hide() {
  if (waiting) window.clearTimeout(waiting);
  waiting = 0;
  pending = null;
  fadeTipOut(tip);
  tip = null;
  owner = null;
  tipTabbed = false;
  if (checking) window.clearInterval(checking);
  checking = 0;
}

function show(control: HTMLElement) {
  const name = control.getAttribute(NAME);
  const note = control.getAttribute(NOTE);
  // A note may stand alone, for a mark with no name to give.
  if (!name && !note) return;
  if (owner === control && tip) return;
  hide();
  owner = control;
  tip = document.createElement("span");
  tip.className = styles.tip;
  tip.setAttribute("role", "tooltip");
  tip.setAttribute("data-icon-tip-shown", "");
  const box = tip;
  (name ?? "").split("\n").forEach((text, _, lines) => {
    const line = lines.length > 1 ? document.createElement("span") : box;
    if (line !== box) line.className = styles.tipLine;
    text.split(/\*\*(.+?)\*\*/).forEach((part, at) => {
      if (!part) return;
      if (at % 2) {
        const bold = document.createElement("strong");
        bold.textContent = part;
        line.append(bold);
      } else line.append(part);
    });
    if (line !== box) box.append(line);
  });
  if (note) {
    const dim = document.createElement("span");
    dim.className = styles.tipNote;
    dim.textContent = name ? ` ${note}` : note;
    tip.append(dim);
  }
  // The picture on a white tile over its name, as Notion's (seen 2026-11-07:
  // 48px tile, 40px picture, the name in 12px 500 under it).
  const picture = control.getAttribute(PICTURE);
  if (picture) {
    tip.classList.add(styles.tipPicture);
    const tile = document.createElement("span");
    tile.className = styles.tipTile;
    const image = document.createElement("img");
    image.src = picture;
    image.alt = "";
    tile.append(image);
    tip.prepend(tile);
  }
  // `data-tip-quiet` draws it as a small menu, grey words on the menus'
  // ground, as Notion's tip on a template's "@지금" (seen 2026-11-09).
  if (control.hasAttribute(QUIET)) tip.classList.add(styles.tipQuiet);
  if (control.hasAttribute(WRAP)) tip.classList.add(styles.tipWrap);
  if (control.hasAttribute(WIDE)) tip.classList.add(styles.tipWide);
  const keys = control.getAttribute("data-tip-keys");
  if (keys) {
    const line = document.createElement("span");
    line.className = styles.tipKeys;
    line.textContent = keys;
    tip.append(line);
  }
  tip.style.visibility = "hidden";
  document.body.append(tip);
  const whole = control.getBoundingClientRect();
  const under = control.getAttribute(UNDER);
  const part = under ? control.querySelector(under)?.getBoundingClientRect() : undefined;
  const rect = part ? new DOMRect(whole.x, part.y, whole.width, part.height) : whole;
  const size = tip.getBoundingClientRect();
  const roomBelow = tipFitsBelow(rect, size.height);
  // `data-tip-side="start"` lines it up with the control's start, under it
  // while there is room, as Notion's on a template's mention (seen
  // 2026-11-09: 6px under its words, two thirds down the window).
  const start = control.getAttribute("data-tip-side") === "start";
  // "start" and "end" are the control's reading direction's: its right and
  // left where it reads from the right.
  const rtl = readsRightToLeft(control);
  // `data-tip-side="end"` sets it beside the control, its top just over the
  // control's, as Notion's tip on a private page's place (seen 2026-10-28).
  const beside = control.getAttribute("data-tip-side") === "end"
    && (rtl ? rect.left - GAP - size.width >= MARGIN : rect.right + GAP + size.width <= window.innerWidth - MARGIN);
  // `data-tip-side="top"` sets it above while there is room, as Notion's tips
  // on a menu row's "⋯" (the template menu's, seen 2026-10-30) and on a name
  // field's ⓘ in a menu's head (describeButton.ts, measured 2026-11-18).
  const above = control.getAttribute("data-tip-side") === "top" && tipFitsAbove(rect, size.height);
  const top = beside ? Math.max(0, rect.top - GAP)
    : roomBelow && !above ? rect.bottom + GAP : Math.max(MARGIN, rect.top - GAP - size.height);
  const left = beside ? (rtl ? rect.left - GAP - size.width : rect.right + GAP)
    : start ? Math.min(Math.max(SIDE_MARGIN, rtl ? rect.right - size.width : rect.left), window.innerWidth - SIDE_MARGIN - size.width)
    : Math.min(Math.max(SIDE_MARGIN, rect.left + rect.width / 2 - size.width / 2), window.innerWidth - SIDE_MARGIN - size.width);
  tip.style.top = `${Math.round(top)}px`;
  tip.style.left = `${Math.round(left)}px`;
  tip.style.visibility = "";
  // A control taken away under the pointer takes its tip with it.
  checking = window.setInterval(() => { if (!owner?.isConnected) hide(); }, 250);
}

// The pointer's tip shows once it has rested on the control the control's
// wait; the tip up goes at once, and the next control waits all its time.
function point(control: HTMLElement) {
  if ((owner === control && tip) || pending === control || control.hasAttribute(KEYS_ONLY)) return;
  hide();
  const wait = tipWaitOf(control);
  if (wait === 0) { show(control); return; }
  pending = control;
  waiting = window.setTimeout(() => {
    waiting = 0;
    pending = null;
    if (control.isConnected) show(control);
  }, wait);
}

// A tip comes as the pointer moves onto its control, not as the control
// comes under a still pointer: a window closed over the page's "…" left its
// tip up (2026-11-05; Notion's shows none until the pointer moves). The
// browser sends move events with the pointer where it was then too.
let lastPoint: { x: number; y: number } | null = null;
// The pointer went into a frame (an app's, a document's) or off the page,
// where its moves don't come here: the first move back only says where it is
// now. A document app's frame closed over the page's "…" left its tip up
// (2026-11-06), the pointer having stood still on the frame's close.
let arriving = false;
function pointed(event: PointerEvent) {
  if (event.pointerType === "touch") return;
  const still = lastPoint !== null && lastPoint.x === event.clientX && lastPoint.y === event.clientY;
  lastPoint = { x: event.clientX, y: event.clientY };
  if (still) return;
  if (arriving) {
    arriving = false;
    return;
  }
  const control = controlOf(event.target);
  const near = owner ?? pending;
  if (control) point(control);
  else if (near && !(event.target instanceof Node && near.contains(event.target))) hide();
}

function left(event: PointerEvent) {
  const next = event.relatedTarget;
  if (event.pointerType !== "touch" && (next === null || next instanceof HTMLIFrameElement)) arriving = true;
  // Only the pointer leaving the tip's own control takes it: a tip the keys
  // brought stays as what lies under a still pointer changes, as Notion's
  // on the table's "+" after Enter chose a type (seen 2026-11-17 in Chrome:
  // the new column come under the pointer in one tab, the pointer away and
  // the table rolled in another).
  const near = owner ?? pending;
  if (!near || !(event.target instanceof Node && near.contains(event.target))) return;
  if (!(next instanceof Node && near.contains(next))) hide();
}

function focused(event: FocusEvent) {
  focusedAt = event.timeStamp;
  const control = controlOf(event.target);
  const byKeys = tabbed || escapedMenu || focusGivenAsByKeys() || (keysLedToLastPress() && !typedInPanel);
  if (control && ((byKeys && keysLedFocus(control)) || control.hasAttribute("data-tip-on-focus"))) {
    show(control);
    tipTabbed = tabbed && tip !== null;
  }
}

function keyed(event: KeyboardEvent) {
  if (event.key === "Escape" && tipTabbed && tip && !imeComposing(event)) {
    hide();
    event.preventDefault(); event.stopImmediatePropagation();
    return;
  }
  tabbed = event.key === "Tab";
  typedInPanel = event.target instanceof Element && Boolean(event.target.closest(PANEL));
  escapedMenu = (event.key === "Escape" || fieldEnter(event)) && !typedInPanel;
  hide();
  // A menu may hand the focus back while the key is still on its way here.
  // Escape with nothing open only rings a control the focus already sat on
  // — given back by a press that closed what it opened — and brings no tip,
  // as Notion's (seen 2026-11-17 in Chrome, two tabs: the table's "+" after a
  // type pressed or a press elsewhere, then Escape — the ring alone); so does
  // Escape with only what it leaves open, an open margin card's "⋯" ringed
  // keeping no tip (seen 2026-11-26 in Chrome at 1440, two tabs).
  if (escapedMenu && event.key === "Escape") {
    const menuOpen = topLayerTakesEscape(), keyAt = event.timeStamp;
    window.setTimeout(() => {
      const control = controlOf(document.activeElement);
      if (escapedMenu && control && keysLedFocus(control) && (menuOpen || focusedAt >= keyAt)) show(control);
    }, 0);
  }
}

function blurred(event: FocusEvent) {
  // A menu's virtual focus (Aria's aria-activedescendant) tells its rows' moves with focus events of its own while the
  // focus stays where it is: the tip stays with it (a page "⋯"'s "입력 값 지우기" lost its tip as Tab reached it).
  if (owner && event.target === owner && event.isTrusted) hide();
}

// A title the page sets again on a control already taken (its name changed)
// is taken at once, its accessible name with it.
function retitled(records: MutationRecord[]) {
  for (const record of records) {
    const element = record.target;
    if (element instanceof HTMLElement && element.hasAttribute(NAME) && element.hasAttribute("title")) {
      const title = element.getAttribute("title") ?? "";
      element.removeAttribute("title");
      element.setAttribute(NAME, title);
      if (element.hasAttribute(LABEL)) element.setAttribute("aria-label", title);
      if (owner === element && tip) tip.firstChild!.textContent = title;
    }
  }
}

/** Names icon-only controls and marks in the dark tip, everywhere in the app. */
export function watchIconTips() {
  if (watching || typeof document === "undefined") return;
  watching = true;
  document.addEventListener("pointermove", pointed, { capture: true, passive: true });
  document.addEventListener("pointerout", left, { capture: true, passive: true });
  document.addEventListener("focusin", focused, true);
  document.addEventListener("focusout", blurred, true);
  // A press takes the tip, but a quiet one's: pressing a template's mention
  // keeps saying what it becomes, as Notion's does (seen 2026-11-09).
  document.addEventListener("pointerdown", (event) => {
    tabbed = false; escapedMenu = false;
    if (!(owner?.hasAttribute(QUIET) && event.target instanceof Node && owner.contains(event.target))) hide();
  }, true);
  document.addEventListener("keydown", keyed, true);
  // A scroll that moves its control takes the tip; the sidebar scrolling to a
  // page just moved leaves the top bar's (2026-11-06).
  window.addEventListener("scroll", (event) => {
    const moved = event.target, near = owner ?? pending;
    if (!near || !(moved instanceof Node) || moved === document || moved.contains(near)) hide();
  }, { capture: true, passive: true });
  window.addEventListener("resize", hide);
  new MutationObserver(retitled).observe(document.documentElement, { attributes: true, attributeFilter: ["title"], subtree: true });
}
