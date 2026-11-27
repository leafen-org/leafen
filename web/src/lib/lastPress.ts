import { fieldEnter } from "./fieldKeyBoundary";
// The control a press began on, whether a finger made it, and whether a key
// came last. WebKit (Safari and the desktop app) leaves the focus where it was
// when a button is clicked, so a window opened from the mouse reads its opener
// from the press rather than from the focus (2026-11-08: closed, its focus went
// to MAIN); and a control given the focus back after a key is drawn with its
// ring there too, as Chrome draws it.

const CONTROL = 'button, a[href], [role="button"], [role="menuitem"], [role="tab"], [role="switch"], input, select, textarea, summary';
// What a press acts on: the controls and every item a press opens, picks or
// ticks by its role — a sidebar page's row, a picker's option, a menu's row, a
// grid's cell or column head. A press on one is its own, as on a button, and
// the keys that led before it lead no more: a focus code gives after it — the
// sidebar row the page it opened gives the caret back to — is the press's,
// ringless (keysLedToPress). Notion's sidebar row takes no ring as its page
// opens, letters typed in a page's text or Escape just before (seen 2026-11-26
// in Chrome at 1440, dark, in two tabs at different times, judged by the
// screen after the press); nor a view's tab after letters typed in the view's
// search, nor a row's "⋯" after a pick in its menu, and a select's option
// picked after letters typed in its search leaves its cell as a pick with
// nothing typed does (seen once each). The row was no control here, and Chrome
// and the desktop app's WebKit rang it. A row of cells is none: it holds the
// view's empty room — a table's sums, its "+ 새 페이지" — where a press after
// letters typed in a pill's window rings "필터" (keysLedToLastPress).
const PRESSED_ON = `${CONTROL}, [role="link"], [role="checkbox"], [role="radio"], [role="menuitemcheckbox"], [role="menuitemradio"], [role="option"], [role="treeitem"], [role="gridcell"], [role="columnheader"]`;
// A control pressed without taking the focus, as Notion's "N개 더 보기" and
// "+" by a database's view tabs (seen 2026-11-10): what it opens comes from
// the focus as it was, which gets it back when that closes (the page, mostly).
const KEEPS_FOCUS = "[data-press-keeps-focus]";
let pressed: { element: HTMLElement; at: number } | null = null;
// A control a shortcut pressed for the key (pressByShortcut), until the next
// key or press.
let keyPressed: HTMLElement | null = null;
// The last key put down, while it is still being handed out (enterBeingPressed).
let keyDown: KeyboardEvent | null = null;
let keyLast = false;
// Where the focus stood as the last key went down: nothing (the page) is null.
let keyFrom: Element | null = null;
// Whether the hand moved the pointer after the last key went down — not the moves a browser makes up as a layer
// appears under a still pointer, which move it nowhere: what opens then, a list beside a menu's row the pointer
// rests on, the pointer opened, not that key (keysFromNothing).
let movedSinceKey = false;
// Whether the keys lead the focus, as Chrome counts them for :focus-visible:
// a key put down with no ⌘, Ctrl or Option, and not a modifier alone; a
// press ends it. A shortcut doesn't, so a control the pointer focused
// takes no ring from ⌘[ (seen 2026-11-13: Chrome's own :focus-visible
// stayed off on a sidebar row after ⌘[, as in Notion).
let keysLead = false;
// Whether keys led until the last press, and that press was outside, on
// nothing a press acts on (PRESSED_ON) or on a window's shade: a field written
// in and then left by a press outside it (keysLedToLastPress).
let keysLedToPress = false;
// A focus code gives now for the last press, as the press's own (focusForLastPress), or as the keys would (focusAsByKeys).
let givenForPress = false, givenForKeys = false;
// The pointer is down: a focus now on what holds the point pressed
// (pressTarget), or inside what the press focused (pressFocused), is the
// press's own; one code gives elsewhere meanwhile is not — "필터" given the
// focus back as the press closes its window, while a hand still holds the
// button down (2026-11-24: a 120 ms press in Chrome and WebKit alike drew it
// with the pointer's mark, where Notion's rings it; a press let go at once,
// as a test's, rang it in Chrome). What the press focused and code lets go
// to nothing at once is the press's no longer: a database's block, a press in
// its empty room focusing it on the way (EditorDropLayer's
// roomPressTakesNoFocus), holds "필터", which Notion rings as that press
// closes a pill's window a value was typed into (seen 2026-11-24 in Chrome).
let pointerDown = false;
let pressTarget: Node | null = null;
let pressFocused: Element | null = null;
// A window's shade: an empty button kept out of Tab's way, which a press
// outside the window lands on.
const SHADE = 'button[tabindex="-1"]:empty';
let finger = false;
// A finger's press that closed a layer as it went down, the element it
// pressed (none where the tap goes nowhere, stopTapAtLayer), and when its
// tap's mouse events stop being its own: once it is lifted, by a second
// (goOnToWhatWasPressed).
let goingOn: { pressed: Element | null; until: number } | null = null;
const MODIFIER_KEYS = new Set(["Meta", "Control", "Alt", "Shift", "CapsLock", "Fn", "AltGraph", "OS"]);

if (typeof document !== "undefined") {
  document.addEventListener("pointerdown", (event) => {
    goingOn = null;
    keyLast = false;
    keyPressed = null;
    const target = event.target instanceof Element ? event.target : null;
    const control = target?.closest<HTMLElement>(CONTROL) ?? null, on = target?.closest(PRESSED_ON) ?? null;
    // A finger's tap leaves no ring, keyboard or not.
    keysLedToPress = keysLead && event.pointerType !== "touch" && (!on || on.matches(SHADE));
    keysLead = false;
    pointerDown = true;
    pressTarget = event.target instanceof Node ? event.target : null;
    pressFocused = null;
    finger = event.pointerType === "touch";
    markKeyFocus(null);
    pressed = control && !control.closest(KEEPS_FOCUS) ? { element: control, at: Date.now() } : null;
  }, true);
  const released = (event: PointerEvent) => {
    pointerDown = false;
    if (goingOn) goingOn = event.type === "pointerup" ? { ...goingOn, until: Date.now() + 1000 } : null;
  };
  document.addEventListener("pointerup", released, true);
  document.addEventListener("pointercancel", released, true);
  // The mouse events and click of a finger's tap come once it is lifted and
  // land on what is under it then: what a layer closed at the press hid
  // takes them unless they are held back (goOnToWhatWasPressed).
  const strayed = (event: MouseEvent) => {
    const going = goingOn, late = going !== null && going.until > 0 && Date.now() > going.until;
    if (late || event.type === "click") goingOn = null;
    if (!going || late) return;
    const { pressed: under } = going, target = event.target;
    const page = under === document.body || under === document.documentElement;
    if (under && (!under.isConnected || !(target instanceof Node) || target === under || target.contains(under) || (!page && under.contains(target)))) return;
    event.preventDefault();
    event.stopImmediatePropagation();
  };
  for (const type of ["mousedown", "mouseup", "click"] as const) document.addEventListener(type, strayed, true);
  document.addEventListener("mousedown", (event) => {
    if (event.target instanceof Element && event.target.closest(KEEPS_FOCUS)) event.preventDefault();
  }, true);
  // While keys lead, the control the focus is on — or is moved to by code: a
  // window's first field, its Tab trap, the one after an action — says so,
  // and takes the keyboard's ring where WebKit gives it no :focus-visible
  // (styles.css).
  document.addEventListener("keydown", (event) => {
    keyDown = event;
    keyPressed = null;
    keyLast = true;
    movedSinceKey = false;
    const from = document.activeElement;
    keyFrom = from && from !== document.body && from !== document.documentElement ? from : null;
    pointerDown = false;
    if (!event.metaKey && !event.ctrlKey && !event.altKey && !MODIFIER_KEYS.has(event.key)) keysLead = true;
    markKeyFocus(document.activeElement);
  }, true);
  document.addEventListener("pointermove", (event) => { if (event.movementX || event.movementY) movedSinceKey = true; },
    { capture: true, passive: true });
  document.addEventListener("focusin", (event) => markKeyFocus(event.target), true);
  document.addEventListener("focusout", (event) => {
    if (pointerDown && event.target === pressFocused && !event.relatedTarget) pressFocused = null;
  }, true);
}

// The one control marked so. A mark on the root instead had the browser
// restyle every control on the page at each switch between keys and the
// pointer (2026-11-12: 41 ms on a page holding two large tables).
let marked: Element | null = null;
// The one the focus is on while the pointer leads (a press came last, not a
// key) says so too, and draws no keyboard ring (styles.css): Chrome's
// :focus-visible rings a control focused by code right after a text field
// held the focus, a press or not — "표" as "새 보기 추가" opens from a press
// on "새 보기" after a search was typed, where Notion's lights it ringless.
// Text being written is not marked so: the page's own editor holds the whole
// page, and its mark's ring, inherited, had the browser restyle every element
// in it as the focus came or went (2026-11-15: 14,300 elements, 156 ms at
// CPU ×2, for a press on a table's "+ 새 페이지" with the caret in the
// page's text); text draws no ring.
let pointerMarked: Element | null = null;
// A focus code gives after a press outside a field keys were typed into
// (keysLedToLastPress) is the keys' too: Notion's toolbar "필터" takes its
// ring as a press on the page closes its pill's window a value was typed
// into (seen 2026-11-12 and twice on 2026-11-15; the pointer's mark below
// took that ring away while its tip showed, 2026-11-15). A press on a
// control ends the keys' lead: "표" above stays ringless.
function markKeyFocus(target: EventTarget | null) {
  const element = target instanceof Element ? target : null;
  if (pointerDown && !pressFocused && element && pressTarget && element.contains(pressTarget)) pressFocused = element;
  const keys = keysLeadNow(element);
  const next = keys ? element : null;
  const pointerNext = keys || (element instanceof HTMLElement && element.isContentEditable) ? null : element;
  if (marked !== next) {
    marked?.removeAttribute("data-key-focus");
    marked = next;
    marked?.setAttribute("data-key-focus", "");
    markHolders(next);
  }
  if (pointerMarked !== pointerNext) {
    pointerMarked?.removeAttribute("data-pointer-focus");
    pointerMarked = pointerNext;
    pointerMarked?.setAttribute("data-pointer-focus", "");
  }
}

// What a row draws while the keys' focus is in it — a sidebar row its "+"
// and "⋯", a block row its handles, a column its outline — it draws by its
// own mark, data-keys-within, which it carries while the keys' mark is on
// an element inside it (not on itself): the same as Chrome's own
// :has(:focus-visible) where the keys lead, and drawn where the browser
// gives the focus no :focus-visible (keysLedFocus), and not where the
// pointer leads and Chrome gives one (data-pointer-focus). Asked of the row
// with :has(), the keys' mark there, which moves with every focus the keys
// lead, had Chrome work out every :has() in the page at each move
// (2026-11-15: 2.5 ms a focus move in a table inside a toggle, 0.4
// without); this mark changes only as the keys' focus enters or leaves
// such a row. A row says so by its class (keysFocusHolder), with the class
// of its own child the focus must be in, where only that child counts, and
// the class of its own child that takes the mark for it, where the row
// holds much: a mark on the row had the browser look through all it holds
// for what the mark changes (a block row holding a large table).
const holderClasses = new Map<string, { part?: string; on?: string }>();
let within: Element[] = [];
const WITHIN = "data-keys-within";
const firstClass = (className?: string) => className?.split(/\s+/u).find(Boolean);

/** Says the elements of this class draw by data-keys-within while the keys'
 *  focus is inside them — inside their child of class `part`, where given —
 *  the mark going on their child of class `on`, where given. */
export function keysFocusHolder(className: string, { part, on }: { part?: string; on?: string } = {}) {
  for (const name of className.split(/\s+/u)) if (name) holderClasses.set(name, { part: firstClass(part), on: firstClass(on) });
}

function markHolders(next: Element | null) {
  const now: Element[] = [];
  if (next && holderClasses.size) {
    for (let child = next, holder = next.parentElement; holder; child = holder, holder = holder.parentElement) {
      for (const name of holder.classList) {
        const kind = holderClasses.get(name);
        if (!kind) continue;
        const { part, on } = kind;
        const bearer = on ? [...holder.children].find((element) => element.classList.contains(on)) : holder;
        if (bearer && (!part || child.classList.contains(part))) now.push(bearer);
        break;
      }
    }
  }
  for (const holder of within) if (!now.includes(holder)) holder.removeAttribute(WITHIN);
  for (const holder of now) if (!within.includes(holder)) holder.setAttribute(WITHIN, "");
  within = now;
}

// Whether the keys lead a focus taken now on this element: the marks' one answer (markKeyFocus).
function keysLeadNow(element: Element | null): boolean {
  return givenForKeys || !givenForPress && (keysLead || (keysLedToPress && !(pointerDown && pressOwns(element))));
}

/** Gives the focus back to an item's own control — a chosen row's title cell,
 *  a list's row, a gallery's card (chosenItemFocus.ts) — as the last press's
 *  own where a press closed its menu: ringless, whatever keys led before the
 *  menu opened or were typed into it, as Notion's (seen 2026-11-25 in Chrome
 *  at 1440, dark, in two tabs at different times, judged after the press by
 *  the screen: a row's title cell ringed by Escape, then ⌘/'s menu, or a right
 *  press's or ⌘/'s menu with letters typed into its search, closed by a press
 *  on the page's room — no ring; a toolbar's "필터" still takes its ring after
 *  a value typed into its pill's window, keysLedToLastPress). After a key, the
 *  keys' as any focus is. */
export function focusForLastPress(element: HTMLElement, options?: FocusOptions) {
  givenForPress = !keyLast;
  try { element.focus(options); } finally { givenForPress = false; }
}

/** Gives the focus as the keys would, a press though it came last: the keyboard's ring and a control's tip
 *  (iconTips.ts), as Notion's page "⋯" takes them back as a press on its export window's shade closes it (seen
 *  2026-11-26 in Chrome at 1440, two tabs). */
export function focusAsByKeys(element: HTMLElement, options?: FocusOptions) {
  givenForKeys = true;
  try { element.focus({ ...options, focusVisible: true } as FocusOptions); } finally { givenForKeys = false; }
}

/** Whether a focus being given now is given as the keys would (focusAsByKeys). */
export const focusGivenAsByKeys = () => givenForKeys;

// Whether a focus while the pointer is down is the press's own (pointerDown):
// on what holds the point pressed, or inside what the press focused — not
// anything inside what was pressed, which may be the page around the toolbar
// (2026-11-24, in the desktop app: a press on the page's empty room under a
// view, the page's scroll box, holds "필터").
function pressOwns(element: Element | null) {
  if (!element || !pressTarget) return true;
  return element.contains(pressTarget) || Boolean(pressFocused?.contains(element));
}

/** Whether the keys led the focus to this element, as its ring shows: the
 *  keys' own mark, which every :focus-visible rule takes too
 *  (postcss.config.mjs), not the browser's :focus-visible alone — WebKit
 *  gives none to the opener a press outside gives the focus back to after
 *  typing (keysLedToLastPress), and Chrome's on a control code focused after
 *  a press, the pointer leading, draws no ring (data-pointer-focus). What a
 *  focus brings or gives back by the keys asks this — a tip, the focus back
 *  after a window closes — so it follows the ring wherever it shows. */
export function keysLedFocus(element: Element | null | undefined): boolean {
  return Boolean(element?.matches(":focus")) && keysLeadNow(element ?? null);
}

// Where the focus is kept on purpose: text being edited, a control, or a
// widget that moves its own highlight (a menu, a list, a grid).
const KEEPER = `${CONTROL}, [contenteditable], [role="menu"], [role="listbox"], [role="grid"], [role="tree"], [aria-activedescendant]`;

/** The control a window opened just now came from: the focused one, or —
 *  where the focus stayed on what holds it (WebKit's click) — the control
 *  pressed in that moment inside it, unless a key came after that press: the
 *  keys opened it then, from the focus as it stands. Keys put down with the
 *  focus on nothing open it from nothing (keysFromNothing), even where their
 *  press focused a control on its way, or a window names one to go back to:
 *  Notion's advanced filter window gives the focus to nothing when a list or
 *  menu its keys opened closes, a press on its pill just before (seen
 *  2026-11-24 at 1685 in Chrome, dark, dpr 1, in two tabs at different
 *  times: ↓ Enter on a rule's property and on "+ 필터 규칙 추가", then
 *  Escape, the focus on nothing, the next letter landing nowhere, the
 *  control lit). */
export function openerOf(focused: HTMLElement | null): HTMLElement | null {
  if (keysFromNothing()) return null;
  if (focused && (focused.matches(KEEPER) || focused.isContentEditable)) return focused;
  const control = pressed && !keyLast && pressed.element.isConnected && Date.now() - pressed.at < 1500 ? pressed.element : null;
  return control && (!focused || focused.contains(control)) ? control : focused;
}

/** The control a window, panel or menu opening now comes from, for a place
 *  that keeps its own to give the focus back to: the focus as openerOf reads
 *  it, else wherever the focus is. */
export function focusedOpener(): HTMLElement | null {
  if (typeof document === "undefined") return null;
  const active = document.activeElement instanceof HTMLElement ? document.activeElement : null;
  return openerOf(active === document.body ? null : active) ?? active;
}

/** Whether this control opened something from a press that left the focus
 *  where it was (KEEPS_FOCUS), or a shortcut's (pressByShortcut): that goes
 *  back to the focus, not to it. */
export function keptFocusWhenPressed(control: Element | null | undefined, focused: Element | null) {
  return Boolean(control && control !== focused && (control === keyPressed || control.closest(KEEPS_FOCUS)));
}

/** The control the last press began on, unless it kept the focus where it was (KEEPS_FOCUS). */
export function lastPressedControl(): HTMLElement | null {
  return pressed?.element ?? null;
}

/** Presses a control for a shortcut, as ⌘⇧O presses "공유": what it opens
 *  gives the focus back where the key was put down when it closes, not to
 *  the control, as Notion's (seen 2026-11-13: its share window opened by
 *  ⌘⇧O and closed by Escape left the caret where it was in the page, no
 *  ring or tip on "공유"; opened by a press, the focus went to "공유"). */
export function pressByShortcut(control: HTMLElement) {
  keyPressed = control;
  control.click();
}

/** The Enter being put down now, while its handlers still run — not one
 *  being composed into a word — whose press of the focused control would
 *  follow unless it is held back. */
export function enterBeingPressed(): KeyboardEvent | null {
  return keyDown && keyDown.eventPhase !== Event.NONE && fieldEnter(keyDown) ? keyDown : null;
}

/** Whether the last thing done was a key rather than a press. */
export function keyCameLast(): boolean {
  return keyLast;
}

/** Whether the last thing done was a key put down with the focus on nothing:
 *  a window's own keys pressing a control they lit, as the advanced filter
 *  window's ↓ and Enter (openerOf) — the press of a select focusing its
 *  button on the way (react-aria's) — or a shortcut. The hand moving the
 *  pointer after it is the last thing done then: a list the pointer opens
 *  beside a row of a menu that shortcut opened comes from its row, and gives
 *  the keys back to that menu as it closes, as Notion's block menu does
 *  (measured 2026-11-27 in Chrome at 1440 in two tabs at different times,
 *  after the action only, no script in the page before it: ⌘/ from a chosen
 *  block, the pointer resting on "색 ›" or "전환 ›", Escape closed the list
 *  alone with the menu's search ringed, "q" typed then landing in it, and a
 *  second Escape closed the menu, the block still chosen). */
export function keysFromNothing(): boolean {
  return keyLast && keyFrom === null && !movedSinceKey;
}

/** Whether keys led until the last press came and it landed outside, on no
 *  control or on a window's shade, as a value typed into a window's field
 *  before a press outside closed it: its opener takes the focus back with
 *  the keyboard's ring, as Notion's filter does (seen 2026-11-12 and twice
 *  on 2026-11-15 in Chrome; no ring when nothing was typed, seen once). */
export function keysLedToLastPress(): boolean {
  return keysLedToPress;
}

/** Whether the last press was a finger's: on a phone the mouse events a tap
 *  makes come after its pointer's, and carry no pointer type of their own. */
export function pressedByFinger(): boolean {
  return finger;
}

/**
 * A finger's press closing a layer as it goes down, the press going on to the
 * page (overlayLayer's pressGoesOn): its tap reaches only what it pressed. Its
 * mouse events and click come once the finger is lifted and land on what is
 * under it then (Android Chrome), so what the layer hid and gives back there
 * would take them (2026-11-21 on a Galaxy A90: a tap just under a line, the
 * "@" list open over it, closed the list and pressed the phone tools' 💬 as
 * they came back, opening a comment). They are held back from anything else;
 * on the page under it they go on — the caret to the words tapped, a new line
 * under the body — as Notion's phone does (seen 2026-11-10 and 2026-11-11 in
 * iPhone Safari and Android Chrome).
 */
export function goOnToWhatWasPressed(event: PointerEvent) {
  goingOn = event.pointerType !== "mouse" && event.target instanceof Element ? { pressed: event.target, until: 0 } : null;
}

/**
 * The finger's press that just closed a layer (goOnToWhatWasPressed) goes no
 * further: its mouse events and click are held back from everything, the
 * page's words under it too, so the caret and the keyboard stay as they were
 * — a phone's "@" or "[[" list, as Notion's Android app closes it (seen
 * 2026-11-21 on a Galaxy A90, twice: a tap outside closed the list alone, and
 * a letter typed then went after its "@").
 */
export function stopTapAtLayer() {
  if (goingOn) goingOn = { ...goingOn, pressed: null };
}
