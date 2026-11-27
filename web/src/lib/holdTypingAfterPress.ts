import { afterNextPaint } from "./afterPress";
import { imeComposing } from "./fieldKeyBoundary";

// Ends the wait of the last holdTypingAfterPress, if one is on.
let waiting: (() => void) | null = null;
// Begins that hold with a letter, as one typed then would.
let typedLetter: ((text: string) => void) | null = null;

/**
 * A letter played from what was held (heldTypingPlayback.ts) while a hold
 * waits to begin is typed as a key typed then: the hold begins and takes it.
 * A letter played goes in with no key of its own, which the wait never heard,
 * so it went into the text the caret was about to leave (2026-11-22: "z" held
 * behind a ⌘Z pressed right after a line was moved, played once the caret was
 * back in the line, went into it, not the title the ⌘Z took the caret to).
 * Says whether it was taken.
 */
export function holdPlayedLetter(text: string) {
  if (!typedLetter) return false;
  typedLetter(text);
  return true;
}

/** Whether a press waits to begin holding what is typed (holdTypingAfterPress). */
export function waitingToHold() {
  return waiting !== null;
}

/** Ends that wait, if one is on. */
export function stopWaitingToHold() {
  waiting?.();
}

/** Whether what is typed goes into this element where it stands: a field, or text being written. */
export function takesTyping(target: EventTarget | null) {
  return target instanceof HTMLInputElement || target instanceof HTMLTextAreaElement || (target instanceof HTMLElement && target.isContentEditable);
}

/**
 * Calls begin once the frame after the press is painted and the field's turn
 * to take the focus after it has passed, or at a key or a letter put in with
 * no key (an input method's, a tool's) that comes before then, unless a field
 * other than the one holding (isHeld) has taken the focus by then: what is
 * typed after it goes there. A letter put in with no key goes, once begun,
 * where add() puts it; the key that begins it, where hold() puts it — a key
 * that is no typing (Escape, an arrow) is held in its turn as those after
 * it, not given to what had the focus. Gives what ends the wait.
 */
export function holdTypingAfterPress(begin: () => void, isHeld: (target: EventTarget | null) => boolean, add: (text: string) => void, hold?: (key: KeyboardEvent) => void) {
  waiting?.();
  const over = new MessageChannel();
  const stop = () => {
    document.removeEventListener("focusin", focused, true);
    document.removeEventListener("keydown", now, true);
    document.removeEventListener("beforeinput", typed, true);
    painted();
    over.port1.onmessage = null;
    over.port1.close();
    if (waiting === stop) { waiting = null; typedLetter = null; }
  };
  // A key's letter goes where the focus is once its keydown is over.
  const now = (event?: Event) => { stop(); begin(); if (event instanceof KeyboardEvent) hold?.(event); };
  const focused = (event: FocusEvent) => { if (!isHeld(event.target) && takesTyping(event.target)) stop(); };
  const typed = (event: InputEvent) => {
    if (!event.isTrusted || imeComposing(event) || event.inputType !== "insertText" || !event.data) return;
    event.preventDefault();
    now();
    add(event.data);
  };
  document.addEventListener("focusin", focused, true);
  document.addEventListener("keydown", now, true);
  document.addEventListener("beforeinput", typed, true);
  over.port1.onmessage = now;
  // A field focused just after the press's first frame holds nothing: this
  // waits one task more.
  const painted = afterNextPaint(() => over.port2.postMessage(null));
  waiting = stop;
  typedLetter = (text) => { now(); add(text); };
  return stop;
}
