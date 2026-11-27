// What was typed while a new page or a line was coming, held in a field of
// its own (pageTitleFocus.ts), played into the place that has opened.

import { createFocusManager } from "@react-aria/focus";
import { chordLetter, KEYBOARD_SHORTCUTS, matchesKeyboardShortcut, primaryModifier } from "./keyboardShortcuts";
import { fieldEnter, imeContinuing } from "./fieldKeyBoundary";
import { holdPlayedLetter } from "./holdTypingAfterPress";

/** A key pressed among what is held that is no typing — the app's shortcut, Escape, Tab, an arrow (heldKey) — sent on in its turn. */
export type HeldKey = { readonly keydown: KeyboardEventInit };

/** A piece of what was held: text as typed, a paste, or a key. */
export type HeldPiece = string | DataTransfer | HeldKey;

function pressEnter() {
  const active = document.activeElement;
  // Into the field that holds what is typed, while the place it is for opens.
  if (active instanceof HTMLElement && active.dataset.newPageTypingHold) document.execCommand("insertText", false, "\n");
  else active?.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", code: "Enter", keyCode: 13, which: 13, bubbles: true, cancelable: true }));
}

function pasteIntoFocus(data: DataTransfer) {
  const event = new Event("paste", { bubbles: true, cancelable: true });
  Object.defineProperty(event, "clipboardData", { value: data });
  document.activeElement?.dispatchEvent(event);
}

/**
 * Sends a held key to what has the focus now, as it was pressed there: what
 * answers it there does its work, and a key nothing there took does what the
 * browser does with it — Tab moves the focus on, an arrow moves the caret,
 * Backspace and Delete take a letter (keyDefault) — which a made key doesn't
 * on its own.
 */
function pressHeldKey({ keydown }: HeldKey) {
  const key = new KeyboardEvent("keydown", keydown);
  // A Mac keymap reads ⇧⌘'s letter from the key's number (w3c-keyname), which WebKit leaves out of a made key.
  Object.defineProperty(key, "keyCode", { value: keydown.keyCode });
  Object.defineProperty(key, "which", { value: keydown.which });
  const target = document.activeElement ?? document.body;
  // Into a field holding again (a title's Enter, its body still opening), it is held there in its turn (heldKey).
  pressing = true;
  try { target.dispatchEvent(key); } finally { pressing = false; }
  if (!key.defaultPrevented) keyDefault(keydown, target);
}

/**
 * The app's own shortcut — ⌘⇧P's move window, ⌘P's or ⌘K's search, ⌘B on
 * the line — pressed while what was typed before it is still on its way,
 * held or playing: it waits its turn behind that as a piece of what is held,
 * so what was typed before it goes where it was typed and only what follows
 * reaches what it opens, as each key goes in its turn on Notion's
 * (2026-11-18: "Block to move" typed right after a press below the page's
 * lines, then ⌘⇧P, read "Bl" in the line and "ock to move" in the move
 * window's search). A chord the browser or the system answers — copy,
 * paste, reload — is none of these and goes on at once, as only a key a
 * person pressed does its work.
 */
export function heldShortcut(key: KeyboardEvent): HeldKey | null {
  if (!(key.isTrusted || pressing) || imeContinuing(key) || !(key.metaKey || key.ctrlKey || key.altKey)) return null;
  if (!KEYBOARD_SHORTCUTS.some(({ id }) => matchesKeyboardShortcut(id, key))) return null;
  return keyOf(key);
}

function keyOf({ key, code, keyCode, which, metaKey, ctrlKey, altKey, shiftKey }: KeyboardEvent): HeldKey {
  return { keydown: { key, code, keyCode, which, metaKey, ctrlKey, altKey, shiftKey, bubbles: true, cancelable: true } };
}

// Keys that are no typing but are the place's to answer: Escape ends its
// editing, Tab goes on from it, an arrow moves its caret.
const PLACE_KEYS = new Set(["Escape", "Tab", "ArrowUp", "ArrowDown", "ArrowLeft", "ArrowRight"]);

/**
 * A key pressed while what is typed is held for a place still coming (a new
 * row's title, a new page's, a line), held to go there in its turn as
 * Notion's place, open at once, takes each key in its turn: the app's
 * shortcut after something held (heldShortcut), the place's own first too
 * (placeShortcut), ⌘A, ⌘Z and ⌘⇧Z always; Escape, Tab or an arrow, held
 * or not — the field holding would drop Escape, lose the focus at Tab, move
 * only its own caret at an arrow; and Backspace or Delete after one of those,
 * which takes the place's letter, not the field's (2026-11-18: "Peek row"
 * then Escape typed while a table's new row was being made, its title took
 * the letters a quarter second on and stayed open, its editor lying over the
 * row's "열기"). What was held before it is "none", "typed" — text, a paste,
 * a shortcut — or a "key" of these last. A key played into a field holding
 * again (pressHeldKey) is held there as one pressed there.
 */
export function heldKey(key: KeyboardEvent, before: "none" | "typed" | "key"): HeldKey | null {
  if (!(key.isTrusted || pressing) || imeContinuing(key)) return null;
  if (choosesAll(key) || stepsHistory(key)) return keyOf(key);
  if (key.metaKey || key.ctrlKey || key.altKey) return before === "none" ? placeShortcut(key) : heldShortcut(key);
  if (!PLACE_KEYS.has(key.key) && !(before === "key" && (key.key === "Backspace" || key.key === "Delete"))) return null;
  return keyOf(key);
}

/**
 * ⌘A (Ctrl+A off a Mac) is the place's too, first or not: it chooses the
 * words of the place that opens, as Notion's open at once would, and what is
 * typed after takes their place (2026-11-22: ⌘A right after a comment's
 * "편집하기", its box some frames from the caret, chose the held field's
 * nothing, and the words typed went after the comment's own).
 */
function choosesAll(key: KeyboardEvent) {
  return (key.metaKey !== key.ctrlKey) && !key.altKey && !key.shiftKey && (key.code === "KeyA" || key.key.toLowerCase() === "a");
}

/**
 * The place's own shortcut is the place's first or not: one the page's
 * editor answers where the caret is — the shortcut table's "editor" ones
 * (keyboardShortcutTable.ts: ⌘B, ⌘I, ⌘U, ⌘E, ⌘⇧S, ⌘D, ⌘⇧P, ⌥⇧ and a digit…)
 * — held to be pressed there in its turn. Let through, the field holding
 * took it, and it went nowhere: the words typed next came plain. The app's
 * own shortcuts (the table's "global" and "page" ones) pressed first are
 * answered at once by the window, as with nothing held.
 */
function placeShortcut(key: KeyboardEvent) {
  return KEYBOARD_SHORTCUTS.some(({ id, scope }) => scope === "editor" && matchesKeyboardShortcut(id, key)) ? heldShortcut(key) : null;
}

/**
 * ⌘Z and ⌘⇧Z (Ctrl+Z, Ctrl+Y off a Mac) are the page's, first or not: they
 * step its history where the place that opens is, in their turn, a step still
 * on its way to the server taken back once it is there. Let through, the
 * browser stepped its own history of the document from the field holding,
 * which took back the last words typed in the page's text (2026-11-22: ⌘Z
 * right after a line was moved last on its own page left the move and turned
 * "three1" into "t", the letter typed next going after it).
 */
function stepsHistory(key: KeyboardEvent) {
  const letter = chordLetter(key);
  return primaryModifier(key) && !key.altKey && (letter === "z" || (letter === "y" && !key.shiftKey));
}

/**
 * What the browser does with a key that nothing took: Tab moves the focus to
 * the next of what takes it (⇧ the one before; a real Tab in a new row's
 * title went on to the row's "열기", 2026-11-18); in text an arrow moves the
 * caret (⇧ chooses on), Backspace and Delete take a letter.
 */
function keyDefault({ key, shiftKey }: KeyboardEventInit, target: Element) {
  if (key === "Tab") {
    const order = createFocusManager({ current: document.body });
    if (shiftKey) order.focusPrevious({ tabbable: true }); else order.focusNext({ tabbable: true });
    return;
  }
  if (!canTakeTyping(target)) return;
  if ((key === "a" || key === "A") && !shiftKey) {
    if (target instanceof HTMLInputElement || target instanceof HTMLTextAreaElement) target.select(); else document.execCommand("selectAll", false);
    return;
  }
  if (key === "Backspace" || key === "Delete") { document.execCommand(key === "Backspace" ? "delete" : "forwardDelete", false); return; }
  const line = key === "ArrowUp" || key === "ArrowDown";
  if (!line && key !== "ArrowLeft" && key !== "ArrowRight") return;
  if (!(target instanceof HTMLInputElement || target instanceof HTMLTextAreaElement)) {
    document.getSelection()?.modify(shiftKey ? "extend" : "move", line ? (key === "ArrowUp" ? "backward" : "forward") : key === "ArrowLeft" ? "left" : "right", line ? "line" : "character");
    return;
  }
  // A field's own caret: a letter on, or to its start or end by line, the way it reads.
  const start = target.selectionStart ?? 0, end = target.selectionEnd ?? 0, length = target.value.length;
  const ahead = line ? key === "ArrowDown" : (key === "ArrowRight") !== (getComputedStyle(target).direction === "rtl");
  const from = target.selectionDirection === "backward" ? end : start;
  const to = line ? (ahead ? length : 0) : start !== end && !shiftKey ? (ahead ? end : start)
    : Math.min(length, Math.max(0, (target.selectionDirection === "backward" ? start : end) + (ahead ? 1 : -1)));
  if (shiftKey) target.setSelectionRange(Math.min(from, to), Math.max(from, to), to < from ? "backward" : "forward");
  else target.setSelectionRange(to, to);
}

/** Sends on the shortcuts among what was held and is let go, where the focus is now. */
export function pressHeldKeys(pieces: readonly HeldPiece[]) {
  for (const piece of pieces) if (isHeldKey(piece)) pressHeldKey(piece);
}

function isHeldKey(piece: HeldPiece): piece is HeldKey {
  return typeof piece !== "string" && !(piece instanceof DataTransfer);
}

/**
 * Plays what was held into what has the focus now, as it was made there: a
 * line break is an Enter, a paste the paste that place takes (a title's rule,
 * titlePaste.ts), a shortcut the key pressed there. Enter may send the caret
 * on (a title's to the body), and what follows goes along, into the field
 * again while the body opens. Each key takes a turn of its own, as typed keys
 * do, so what one sets off — a Markdown shortcut turning the line into a
 * list, "/" opening its menu — has happened before the next goes in; keys
 * typed meanwhile wait behind what was held (2026-11-07: on a busy machine,
 * letters played a microtask apart went in before "- " had turned its line
 * into a list item, and "목록" read "록" with "목" at the page's end, as
 * "/google maps" had read "/oogle mapsg").
 */
export function playHeldTyping(pieces: readonly HeldPiece[]) {
  const steps: HeldStep[] = [];
  // Into a hold waiting to begin (a caret about to move, a step played before it asked), as a letter typed then.
  const letter = (text: string): HeldStep => Object.assign(() => { if (!holdPlayedLetter(text)) document.execCommand("insertText", false, text); },
    { settles: !/^[\p{L}\p{N}]$/u.test(text) });
  for (const piece of pieces) {
    if (typeof piece !== "string") {
      steps.push(isHeldKey(piece) ? () => pressHeldKey(piece) : () => pasteIntoFocus(piece));
      continue;
    }
    piece.split("\n").forEach((line, index) => {
      if (index) steps.push(pressEnter);
      // Letter by letter, as typed: a Markdown shortcut takes effect where it
      // was typed (2026-11-05: "- q1" typed right after a press below the
      // page's lines stayed text, and Notion's made a bulleted line).
      for (const one of Array.from(line)) steps.push(letter(one));
    });
  }
  // Held again while this plays (a title's Enter opening the body), what was
  // held then goes before what is left to play.
  if (playing) {
    playing.unshift(...steps);
    return;
  }
  const queue = steps;
  playing = queue;
  // While it plays nothing typed reaches the page on its own: a key or text
  // that came in meanwhile — a Tab, an Enter, the app's own shortcut
  // (heldShortcut), letters put in with no key, as an input method or a tool
  // puts them — waits its turn behind (2026-11-07: an Enter the page took at
  // once and again from the queue, and words put in mid-line, read "DB
  // 부모인라인 글" with "/" left alone on the next line).
  let ours = false;
  const run = (step: HeldStep) => { ours = true; try { step(); } finally { ours = false; } };
  const behind = (key: KeyboardEvent) => {
    if (ours || !key.isTrusted || imeContinuing(key)) return;
    const shortcut = heldShortcut(key);
    if (!shortcut && (key.metaKey || key.ctrlKey || key.altKey || key.key === "Shift" || key.key === "CapsLock")) return;
    key.preventDefault();
    key.stopImmediatePropagation();
    if (fieldEnter(key) && !shortcut) queue.push(pressEnter);
    else if (Array.from(key.key).length === 1 && !shortcut) queue.push(letter(key.key));
    // Any other key as it was pressed where the focus is in its turn (pressHeldKey): Backspace, Escape, Tab, an arrow.
    else { const held = shortcut ?? keyOf(key); queue.push(() => pressHeldKey(held)); }
  };
  const behindText = (input: InputEvent) => {
    if (ours || !input.isTrusted || imeContinuing(input) || input.inputType !== "insertText" || !input.data) return;
    input.preventDefault();
    input.stopImmediatePropagation();
    queue.push(letter(input.data));
  };
  document.addEventListener("keydown", behind, true);
  document.addEventListener("beforeinput", behindText, true);
  // A letter or digit goes on in the next turn; after a space, a mark, an
  // Enter, a paste or a shortcut — what may turn the line or open a menu or a
  // window — a frame passes first, for the page to show what it set off, or a
  // tenth of a second where frames come slower (a busy machine, a window in
  // the background): the keys were typed together, and a pause of half a
  // second between two of them would part them into steps of ⌘Z, the page's
  // and a title's alike, where Notion's takes back the run typed at once
  // (2026-11-16: "body first" typed right after a press under a new page's
  // title came back as "body" and " first", one ⌘Z each).
  const next = () => {
    const step = queue.shift();
    if (step) run(step);
    if (queue.length) {
      if (step?.settles === false) window.setTimeout(next, 0);
      else afterFrameOrSoon(next);
      return;
    }
    document.removeEventListener("keydown", behind, true);
    document.removeEventListener("beforeinput", behindText, true);
    playing = null;
  };
  next();
}

function canTakeTyping(target: Element) {
  if (target instanceof HTMLInputElement || target instanceof HTMLTextAreaElement) return true;
  const anchor = document.getSelection()?.anchorNode;
  return target instanceof HTMLElement && target.isContentEditable && Boolean(anchor && target.contains(anchor));
}

/**
 * Plays what was held into the field that has just taken the focus, once its
 * caret is there. A field's caret comes with its focus, in the same step, so
 * what was held goes in before any key that follows; one whose caret comes
 * later takes it a few frames on at most, and the letters and shortcuts
 * typed meanwhile go after what was held (2026-11-06: a press below a busy
 * page's lines, "/" held, "g" went in first and "/google maps" read
 * "/oogle mapsg").
 */
export function playHeldTypingOnceReady(pieces: HeldPiece[]) {
  let frames = 0;
  const add = (text: string) => {
    const last = pieces.at(-1);
    if (typeof last === "string") pieces[pieces.length - 1] = last + text;
    else pieces.push(text);
  };
  const after = (key: KeyboardEvent) => {
    if (playing || !key.isTrusted || imeContinuing(key)) return;
    const last = pieces.at(-1);
    // A Backspace after letters takes the last of them, as in the field that held them.
    const erases = key.key === "Backspace" && typeof last === "string" && last !== "" && !(key.metaKey || key.ctrlKey || key.altKey);
    const held = erases ? null : heldKey(key, last !== undefined && isHeldKey(last) ? "key" : "typed");
    const text = held || key.metaKey || key.ctrlKey || key.altKey ? ""
      : fieldEnter(key) ? "\n" : Array.from(key.key).length === 1 ? key.key : "";
    if (!held && !text && !erases) return;
    key.preventDefault();
    key.stopImmediatePropagation();
    if (held) pieces.push(held);
    else if (erases) pieces[pieces.length - 1] = Array.from(last).slice(0, -1).join("");
    else add(text);
  };
  // Letters put in with no key (an input method's, a tool's) wait as well.
  const afterText = (input: InputEvent) => {
    if (playing || !input.isTrusted || imeContinuing(input) || input.inputType !== "insertText" || !input.data) return;
    input.preventDefault();
    input.stopImmediatePropagation();
    add(input.data);
  };
  document.addEventListener("keydown", after, true);
  document.addEventListener("beforeinput", afterText, true);
  readying += 1;
  let waiting = true;
  const stop = () => {
    waiting = false;
    atControl.delete(play);
    document.removeEventListener("keydown", after, true);
    document.removeEventListener("beforeinput", afterText, true);
    readying -= 1;
  };
  const play = () => { if (waiting) { stop(); playHeldTyping(pieces); } };
  atControl.add(play);
  const type = () => {
    if (!waiting) return;
    const focus = document.activeElement;
    const ready = !!focus && canTakeTyping(focus);
    if (ready) play();
    else if (frames++ >= 20) stop();
    else window.requestAnimationFrame(type);
  };
  queueMicrotask(type);
}

// What waits for a caret (playHeldTypingOnceReady), to go now to a control the press opened (playHeldTypingAtControl).
const atControl = new Set<() => void>();

/**
 * The place a press opened has taken the focus and is a control, not a field
 * to write in — a window opened on its tab, as a picture's "바꾸기" on
 * "업로드": what was held for it goes there now, its keys pressed there
 * (Escape closing the window, Tab going on), its letters going nowhere, as
 * on Notion's window, there at the press. Waiting
 * for a caret that never comes to a control, they went nowhere at all
 * (2026-11-26: an Escape pressed as that window opened left it open, the
 * page's shield under it taking the next press).
 */
export function playHeldTypingAtControl() {
  for (const play of [...atControl]) play();
}

type HeldStep = (() => void) & { settles?: boolean };

/** Calls then once, after the next frame or a tenth of a second, whichever comes first. */
function afterFrameOrSoon(then: () => void) {
  let done = false;
  const go = () => { if (!done) { done = true; window.setTimeout(then, 0); } };
  const timer = window.setTimeout(go, 100);
  window.requestAnimationFrame(() => { window.clearTimeout(timer); go(); });
}

// The keys being played (playHeldTyping), which keys typed meanwhile wait behind.
let playing: HeldStep[] | null = null;
// Pieces taken from the field that held them, waiting for their place's caret (playHeldTypingOnceReady).
let readying = 0;
// Whether a held key is being sent on (pressHeldKey): a field holding again holds it as pressed there (heldKey).
let pressing = false;

/** Whether something typed is still on its way to its place: held in the field that holds it, or being played. */
function typingOnItsWay() {
  const active = document.activeElement;
  return playing !== null || readying > 0
    || (active instanceof HTMLTextAreaElement && Boolean(active.dataset.newPageTypingHold) && active.value !== "");
}

/**
 * Calls then once what was typed before has reached its place: what the
 * desktop app's menu asks for (desktopMenuCommands.ts — ⇧⌘K's search,
 * Settings, Undo) waits as a shortcut pressed in the page does (heldShortcut).
 */
export function afterHeldTyping(then: () => void) {
  if (typingOnItsWay()) afterFrameOrSoon(() => afterHeldTyping(then));
  else then();
}
