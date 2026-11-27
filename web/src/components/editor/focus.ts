import { afterNextPaint } from "@/lib/afterPress";
import { holdTypingForField, holdingComposition, holdingTyping, waitForHeldComposition } from "@/lib/pageTitleFocus";
import { queryPlaced } from "@/lib/popoverLayer";
import { liveField } from "@/lib/sleepingFields";
import { childrenScope } from "./blockOccurrences";
import { focusNativeTiptapBlock, focusNativeTiptapControl, nativeTiptapEditableElement, refocusNativeTiptapView,
  selectNativeTiptapElement, independentTiptapSelection } from "./nativeTiptapFocus";

// Titles register their native identity; every editable caret belongs to
// a committed engine view. The registry only locates that view’s mount.
const registry = new Map<string, HTMLElement[]>();
type CaretPos = "start" | "end" | "select" | number;

export function registerEditable(key: string, element: HTMLElement | null): () => void {
  if (!element) return () => {};
  const elements = registry.get(key) ?? [];
  if (!elements.includes(element)) registry.set(key, [...elements, element]);
  return () => {
    const remaining = (registry.get(key) ?? []).filter(candidate => candidate !== element);
    if (remaining.length) registry.set(key, remaining); else registry.delete(key);
  };
}

export function getEditable(key: string, scope?: string): HTMLElement | undefined {
  return nativeTiptapEditableElement(key, scope) ?? registry.get(key)?.find(element => element.isConnected);
}

function cssEscape(value: string) { return CSS.escape(value); }

/**
 * Puts the caret in a field; one drawn before its editor is made has it made
 * now (liveField). Without `scroll` the page stays where it is: a key typed
 * there brings the field into view, as typing anywhere does.
 */
export function placeCaret(element: HTMLElement, position: CaretPos = "end", scroll = true) {
  return selectNativeTiptapElement(liveField(element), position, scroll);
}

// The caret goes where it was asked for last: an ask still waiting for its
// line to come gives up once a newer one is made (2026-11-09: a title's
// Enter asked for its new first line again while the page was busy, and the
// first ask, its line come at last, took the caret back to the line's start
// in the middle of what was typed after the Enter, "body words" reading
// "wordsbody ").
let latestAsk = 0;
// The last ask that has had its place, or given up.
let settledAsk = 0;
// The ask that leaves the page where it is (focusTitleStartInPlace).
let inPlaceAsk = 0;
// Ends the hold of what is typed while the caret waits for its place, saying
// whether the caret came.
let endCaretWait: ((placed: boolean) => void) | null = null;
// How long what is typed is held while nothing asks for the caret.
const CARET_WAIT_MS = 5000;

/**
 * The caret is to go to a place not drawn yet — a line just made, a field a
 * press shows: what is typed from now until it has come is held, and typed
 * there once it has, as in a line typed straight on (2026-11-09: an empty
 * tab's, toggle's or synced block's first line and a caption lost their
 * first keys; 2026-11-10: "def" typed right after a "/" menu's Enter read
 * "abc de" in the line above and "f" in the heading it made, and "/" typed
 * right after Escape on a new page's "시작하기" went nowhere). When the caret
 * doesn't come — its place never shown, or nothing asks for it within a few
 * seconds — what was held goes back where the caret was. A caret that comes
 * in the text that had the focus all along (the page's, a frame after a "/"
 * menu's Enter) moves no focus: holding not begun by then doesn't begin
 * (2026-11-16: "/콜아웃", "/동기화" and "/mermaid" each lost the letters typed
 * next, the hold begun after the caret came taking the focus from it).
 */
export function holdTypingForCaret() {
  if (holdingTyping()) return;
  endCaretWait?.(true);
  const back = document.activeElement;
  void holdTypingForField(() => new Promise<boolean>((resolve) => {
    const end = (placed: boolean) => {
      window.clearTimeout(timer);
      if (endCaretWait === end) endCaretWait = null;
      resolve(placed);
    };
    const timer = window.setTimeout(() => end(false), CARET_WAIT_MS);
    endCaretWait = end;
  }), () => giveCaretBack(back), false, true);
}

function giveCaretBack(back: Element | null) {
  if (!(back instanceof HTMLElement) || !back.isConnected || back === document.body) return;
  if (!refocusNativeTiptapView(back)) back.focus({ preventScroll: true });
}

function settle(ask: number, placed: boolean) {
  settledAsk = ask;
  endCaretWait?.(placed);
}

/**
 * Puts the caret in a line; one not drawn yet takes it once it is, a frame or
 * a few on, what is typed meanwhile waiting for it (holdTypingForCaret).
 */
export function focusEditable(key: string, position: CaretPos = "end", attempts = 12, scope?: string): boolean {
  const ask = ++latestAsk;
  if (askFocus(ask, key, position, attempts, scope)) return true;
  // From after this step: what asked may still be using the focus.
  if (attempts > 0) queueMicrotask(() => { if (ask === latestAsk && settledAsk !== ask) holdTypingForCaret(); });
  return false;
}

/**
 * Puts the caret at the start of a page's title and leaves the page where it
 * is, far down as it may be: the title comes into view with the first key
 * typed there, not with the caret. Notion's after its new database panel's
 * Escape (DatabaseStartPanel), measured twice on 2026-11-16 in two Chrome
 * tabs at 1440, dark, with no script in the page before the key, on a long
 * page (a line, 40 empty lines, a line, then the database): after Escape the
 * database was still in view, and one letter typed went in before the
 * title's first letter, the page moving up to the title only with it.
 */
export function focusTitleStartInPlace(pageId: string): boolean {
  inPlaceAsk = latestAsk + 1;
  return focusEditable(`title:${pageId}`, "start");
}

/**
 * A press on an empty block's hint makes its first line, which takes the
 * caret there, where the block was pressed — an empty tab's, toggle's or
 * synced block's, drawn itself or inside a synced copy that draws its
 * original again on the same page: `at` is the pressed block's occurrence
 * (blockOccurrences), `drawsOriginal` that it is such a copy, the line made
 * its original's (2026-11-18: an empty copy's "내용 추가" on its original's
 * page put the caret in the original, far from the press on a long page).
 */
export function focusMadeLine(make: () => { id: string } | null | undefined, at: string, drawsOriginal = false) {
  const line = make();
  if (line) focusEditable(line.id, "start", 12, childrenScope(at, drawsOriginal));
}

/**
 * Where a caret put off goes, named then (focusNextFrame, caretOnceAnswered):
 * a line or a page's title by its key, the start of a page's title with the
 * page left where it is (focusTitleStartInPlace), or a field drawn.
 */
// A field's caret leaves the page where it is with `scroll` false.
export type CaretNextFrame = { key: string; position: CaretPos } | { titleStart: string } | { field: HTMLElement; position: CaretPos; scroll?: boolean };

function goTo(ask: number, then: CaretNextFrame) {
  if ("titleStart" in then) focusTitleStartInPlace(then.titleStart);
  else if ("field" in then) {
    // A syllable being made in what is held is let finish first, as askFocus does.
    const { field, position, scroll = true } = then;
    const place = () => { if (ask === latestAsk) settle(ask, field.isConnected && placeCaret(field, position, scroll)); };
    if (!waitForHeldComposition(place)) place();
  } else focusEditable(then.key, then.position);
}

/**
 * Moves the caret once the next frame has drawn what was just changed — a
 * menu or window put away, a line made or turned into another, a page's text
 * drawn again — to where `to` says then, unless `unless` says then that it
 * needn't; with nowhere to go it stays where it was. What is typed meanwhile
 * waits for it and is typed there (holdTypingForCaret), as when the caret is
 * asked for at once: a caret put off a frame without it lost the keys typed
 * in that frame (2026-11-21: the letter typed right after an inline
 * database's "비어 있는 새 데이터 소스" went nowhere, its title-start caret
 * a frame on). Every caret move put off a frame goes through here.
 */
export function focusNextFrame(to: () => CaretNextFrame | null, unless?: () => boolean) {
  const ask = ++latestAsk;
  queueMicrotask(() => { if (ask === latestAsk && !unless?.()) holdTypingForCaret(); });
  requestAnimationFrame(() => {
    if (ask !== latestAsk) return;
    if (unless?.()) { settle(ask, true); return; }
    const then = to();
    if (!then) settle(ask, false);
    else goTo(ask, then);
  });
}

/**
 * Sends a caret asked for at an action's start where it goes once the action
 * has its answer (caretOnceAnswered): a place, nowhere (null), or "kept" —
 * something else has the caret by then, where what was held went.
 */
export type CaretLanding = (to: CaretNextFrame | "kept" | null, frameOn?: boolean) => void;

// Presses on the page, counted: a caret waiting for an answer gives way to a press made meanwhile.
let presses = 0;
let countingPresses = false;

/**
 * A caret to move once an action begun now has its answer — the server's, as
 * a block move's or its ⌘Z's, or a window's question: what this answers sends
 * it where it goes then (`to`, as focusNextFrame's; `frameOn` once the next
 * frame has drawn what the answer changed), or nowhere (null). What is typed
 * from the action's start until then waits for it and is typed there
 * (holdTypingForCaret), as on Notion's, whose caret is there at the press:
 * moved only once the answer came, what was typed meanwhile went into the
 * line being moved or taken away, or the window still up (2026-11-21: after
 * "/삭제", a block's "옮기기" or "페이지 위치:", and a move's ⌘Z). With
 * nowhere to go, what was held goes back where the caret was; so it does when
 * the answer is that something else has the caret ("kept") but what is typed
 * is still held, nothing having taken it (2026-11-22: a new page's title had
 * the caret, the empty page's ask took it into the hold, and what was typed
 * stayed there). A press on the page meanwhile has the caret, and so has a
 * caret asked for since: this one then goes nowhere. So it does, nothing held,
 * when it is asked for after the person's own press or key — by a read or an
 * answer — while what they opened or moved into has the keys: a field to write
 * in, a menu or a window outside `place`, where the caret is asked into (or the
 * window the focus was in as it was asked); 2026-11-22: a side peek's row,
 * read again as another row's right-click menu opened, took the menu's focus
 * into the hold, and Escape left the menu open, what was typed in its search
 * going nowhere. Every caret move put off behind an answer goes through here,
 * as one put off a frame goes through focusNextFrame.
 */
export function caretOnceAnswered(place?: () => Element | null | undefined): CaretLanding {
  if (!countingPresses) {
    countingPresses = true;
    document.addEventListener("pointerdown", (event) => { if (event.isTrusted) presses += 1; }, { capture: true, passive: true });
  }
  const ask = ++latestAsk, pressed = presses;
  const own = personInputNow(), from = document.activeElement;
  let given = false;
  queueMicrotask(() => {
    if (ask !== latestAsk || settledAsk === ask || presses !== pressed) return;
    if (!own && keysTakenElsewhere(place?.() ?? from?.closest(WINDOW) ?? null)) {
      given = true;
      settledAsk = ask;
      return;
    }
    holdTypingForCaret();
  });
  let sent = false;
  return (to, frameOn = false) => {
    if (sent) return;
    sent = true;
    if (given || ask !== latestAsk) return;
    if (presses !== pressed) settle(ask, true);
    // "Kept" while what is typed is still held: nothing else took the caret,
    // and what was held goes back where it was, as with nowhere to go.
    else if (to === "kept") settle(ask, !holdingTyping());
    else if (!to) settle(ask, false);
    else if (frameOn) requestAnimationFrame(() => { if (ask === latestAsk) goTo(ask, to); });
    else goTo(ask, to);
  };
}

// A window over the page, and a menu or list a press opened: the keys go there.
const WINDOW = '[role="dialog"], [role="alertdialog"], [aria-modal="true"]';
const OPENED = `${WINDOW}, [role="menu"], [role="menubar"], [role="listbox"]`;

/**
 * Whether the keys are with something the person has open outside `place`: a
 * field to write in that isn't a page's own text, or a menu or window. The
 * page's text and its controls leave them to a caret asked for.
 */
function keysTakenElsewhere(place: Element | null) {
  const active = document.activeElement;
  if (!(active instanceof HTMLElement) || active === document.body || place?.contains(active)) return false;
  return plainField(active) || Boolean(active.closest(OPENED));
}

function plainField(element: Element) {
  return element instanceof HTMLTextAreaElement || element instanceof HTMLSelectElement
    || element instanceof HTMLInputElement && !["checkbox", "radio", "button", "submit", "reset", "range", "color", "file", "image"].includes(element.type);
}

// The person's own press, key or paste while it is being handled: a caret asked
// for then is that input's own — its menu's, its field's — whatever has the
// keys; one asked for after it, by a read or an answer, gives way to them.
let input: Event | null = null;
if (typeof window !== "undefined") {
  for (const type of ["pointerdown", "pointerup", "mousedown", "mouseup", "click", "dblclick", "contextmenu", "keydown", "keyup",
    "beforeinput", "input", "paste", "cut", "drop", "compositionend", "touchstart", "touchend"]) {
    window.addEventListener(type, (event) => { if (event.isTrusted) input = event; }, { capture: true, passive: true });
  }
}
function personInputNow() {
  return input !== null && input.eventPhase !== Event.NONE;
}

/**
 * Whether the field an action was begun from — a comment's box as it sends —
 * may take the caret back once the action has its answer: not after a press
 * on the page or a caret asked for since (caretOnceAnswered's rule), nor while
 * a field to write in has it, the person writing on elsewhere (2026-11-22: an
 * edit opened from a comment's "…" right after it was sent lost its letters
 * to the new comment's box as the send answered, and its Enter saved
 * nothing). Nothing typed is held: the field with the caret keeps it.
 */
export function caretMayComeBack(): () => boolean {
  if (!countingPresses) {
    countingPresses = true;
    document.addEventListener("pointerdown", (event) => { if (event.isTrusted) presses += 1; }, { capture: true, passive: true });
  }
  const ask = latestAsk, pressed = presses;
  return () => ask === latestAsk && presses === pressed && !writingField(document.activeElement);
}

// Keys and words put in on the page, counted: the person's own key (an arrow,
// a letter) or words played where they typed them (heldTypingPlayback.ts).
let typings = 0;
let countingTypings = false;

/**
 * Whether the caret an action leaves nowhere once it has its answer — a ⌘Z
 * taking blocks back from another page, which leaves none, as Notion's —
 * may go then: not after a press on the page, a key or words typed, or a
 * caret asked for since (caretOnceAnswered's rule), what the person did
 * meanwhile keeping it where they did it (2026-11-23: a line sent to another
 * page with "/페이지 위치:", then ⌘Z and a letter at once — the letter went
 * before the title, where the ⌘Z had the caret, and the ⌘Z's answer, a moment
 * on, took the caret away from under it, the next keys going nowhere).
 */
export function caretMayGoOnceAnswered(): () => boolean {
  if (!countingPresses) {
    countingPresses = true;
    document.addEventListener("pointerdown", (event) => { if (event.isTrusted) presses += 1; }, { capture: true, passive: true });
  }
  if (!countingTypings) {
    countingTypings = true;
    document.addEventListener("keydown", (event) => { if (event.isTrusted) typings += 1; }, { capture: true, passive: true });
    document.addEventListener("input", () => { typings += 1; }, { capture: true, passive: true });
  }
  const ask = latestAsk, pressed = presses, typed = typings;
  return () => ask === latestAsk && presses === pressed && typings === typed;
}

function writingField(element: Element | null) {
  return element instanceof HTMLElement && (element.isContentEditable || element.getAttribute("role") === "textbox"
    || element instanceof HTMLTextAreaElement
    || element instanceof HTMLInputElement && !["checkbox", "radio", "button", "submit", "range", "color"].includes(element.type));
}

/**
 * The caret goes where `to` says once the frame after now is painted — a
 * field drawn before its editor is made (sleepingFields), a page or window
 * just opened or closed — what is typed meanwhile waiting for it there, a
 * press meanwhile keeping its own focus (caretOnceAnswered): put off with
 * nothing held, a letter typed at once went nowhere (2026-11-21: after a
 * row's "열기", and after the page layout builder's "취소"; a key there called
 * the caret off). `place` is where the caret goes (caretOnceAnswered). Gives
 * what calls it off, what was held going back.
 */
export function focusAfterPaint(to: () => CaretNextFrame | "kept" | null, place?: () => Element | null | undefined): () => void {
  const land = caretOnceAnswered(place);
  const stop = afterNextPaint(() => land(to()));
  return () => { stop(); land(null); };
}

/** Puts the caret in a line once the next frame has drawn what was just changed (focusNextFrame). */
export function focusEditableNextFrame(key: string, position: CaretPos = "end", unless?: () => boolean) {
  focusNextFrame(() => ({ key, position }), unless);
}

function askFocus(ask: number, key: string, position: CaretPos, attempts: number, scope?: string): boolean {
  if (ask !== latestAsk || settledAsk === ask) return false;
  // A syllable being made in what is held is let finish first, wherever the
  // caret was asked for: moved now it is cut (2026-11-05: a press on a new page's
  // body, with "본문" typed as the line it made came up, gave "ㅂㅗㄴ문").
  if (waitForHeldComposition(() => askFocus(ask, key, position, attempts, scope))) return false;
  const placed = focusNativeTiptapBlock(key, position, scope) || placeExternal(key, position, ask !== inPlaceAsk);
  if (placed || attempts <= 0) settle(ask, placed);
  else askFocusNextFrame(ask, key, position, attempts - 1, scope);
  return placed;
}

function placeExternal(key: string, position: CaretPos, scroll: boolean) {
  const external = registry.get(key)?.find(element => element.isConnected);
  return Boolean(external && placeCaret(external, position, scroll));
}

// A title's Enter comes here with what is typed held (pageTitleFocus.ts): a
// syllable being made there is let finish.
function askFocusNextFrame(ask: number, key: string, position: CaretPos, attempts: number, scope?: string) {
  requestAnimationFrame(() => {
    if (holdingComposition()) askFocusNextFrame(ask, key, position, attempts, scope);
    else askFocus(ask, key, position, attempts, scope);
  });
}

function blockOwnRow(blockId: string) {
  const group = document.querySelector<HTMLElement>(
    `[data-block-id="${cssEscape(blockId)}"]`
  );
  return group?.querySelector<HTMLElement>(":scope > [data-type]") ?? group;
}

/** Focuses a block's own control; one not drawn yet once it is, telling `settled` whether it came. */
export function focusBlockControl(
  blockId: string,
  selector: string,
  attempts = 12,
  settled?: (placed: boolean) => void
): boolean {
  if (placeBlockControl(blockId, selector)) { settled?.(true); return true; }
  if (attempts <= 0) { settled?.(false); return false; }
  requestAnimationFrame(() => focusBlockControl(blockId, selector, attempts - 1, settled));
  return false;
}

function placeBlockControl(blockId: string, selector: string) {
  if (focusNativeTiptapControl(blockId, selector)) return true;
  // A control in a window the block opened over the page (an empty
  // picture's link field) is found there, drawn on the page's body.
  const row = blockOwnRow(blockId);
  const target = row ? queryPlaced<HTMLElement>(row, selector) : null;
  if (target) {
    target.focus();
    if (
      target instanceof HTMLInputElement ||
      target instanceof HTMLTextAreaElement
    ) {
      target.select();
      return true;
    } else if (target.isContentEditable) {
      if (placeCaret(target, "end")) return true;
    } else if (document.activeElement === target) {
      return true;
    }
    // A caption wrapper may mount before its engine text field. A focus()
    // call on that non-focusable wrapper is not success; retry the owner.
  }
  return false;
}

/**
 * A block's field a press shows, such as its caption, takes the caret: engine
 * focus is synchronous once mounted; one not mounted yet takes it once it is,
 * what is typed meanwhile waiting for it (holdTypingForCaret).
 */
export function focusBlockControlSettled(blockId: string, selector: string, attempts = 12) {
  if (focusBlockControl(blockId, selector, 0)) return;
  holdTypingForCaret();
  const end = endCaretWait;
  focusBlockControl(blockId, selector, attempts, (placed) => end?.(placed));
}

/**
 * A finger's tap whose caret the page put down as the finger lifted — on the
 * page's last line or a new one under its blocks, its first line, or the line
 * beside the tap — keeps it: the browser's own tap, the mousedown it sends
 * after the lift, moved the focus to what it hit, which in the page's room is
 * no field (2026-11-16: under a page of user HTML blocks the focus went to the
 * page and what was typed next went nowhere; under app blocks its hit found
 * the last line by chance). Held back until that tap's click, or a moment on
 * where none comes.
 */
export function keepTapCaret() {
  const hold = (event: MouseEvent) => event.preventDefault();
  const done = () => {
    window.clearTimeout(timer);
    window.removeEventListener("mousedown", hold, true);
    window.removeEventListener("click", done, true);
  };
  const timer = window.setTimeout(done, TAP_CLICK_MS);
  window.addEventListener("mousedown", hold, true);
  window.addEventListener("click", done, true);
}
// How long after a finger lifts its tap's mousedown and click may still come.
const TAP_CLICK_MS = 500;

export function isEditableFullySelected(element: HTMLElement): boolean {
  const selection = independentTiptapSelection(element);
  return Boolean(selection && selection.length > 0 && selection.start === 0 && selection.end === selection.length);
}

export function selectEditableContents(element: HTMLElement) { placeCaret(element, "select"); }

export function isCaretAtEnd(element: HTMLElement): boolean {
  const selection = independentTiptapSelection(element);
  return Boolean(selection?.collapsed && selection.end === selection.length);
}
