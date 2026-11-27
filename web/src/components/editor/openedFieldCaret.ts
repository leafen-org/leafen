import { fieldAsleep } from "@/lib/sleepingFields";
import { touchScreen } from "@/lib/screenKind";
import { holdingTyping } from "@/lib/pageTitleFocus";
import { playHeldTypingAtControl } from "@/lib/heldTypingPlayback";
import { caretOnceAnswered, type CaretLanding } from "./focus";

/**
 * A field to write in that a press — a key or a click — opens takes the caret
 * once it is drawn, and what is typed from the press until then is held for
 * it and typed there (caretOnceAnswered), as Notion's field, there at the
 * press, takes it: focused a frame on with nothing held, the first keys went
 * where the caret had been — the page's text, a code block, a table's cell —
 * or nowhere (2026-11-22). Every field a press opens and gives the caret a
 * frame or more on goes through here (scripts/check-late-field-focus.mjs).
 */

/**
 * A press opens `target`'s field, drawn by the next frame: it takes the
 * caret there — its words chosen with `select`, what is held then taking
 * their place; the page left where it is without `scroll`; put by the field
 * itself with `place` — and what was typed meanwhile is typed into it. Gives what
 * calls it off; a field never there gives what was held back where it was.
 * One drawn later — after a menu closing gives the focus back a frame on, or
 * once an answer comes — is looked for from `from` frames on, each frame, up
 * to `frames` looks, what is typed held all the while.
 * On a touch screen nothing takes the focus at the press and nothing is held —
 * its keyboard stays down till the field has it, a frame on, as a new row's
 * title does (holdTypingWhileRowOpens): the hold's own field, focused in the
 * tap, would bring it up.
 */
export function focusOpenedField(target: () => HTMLElement | null | undefined,
  { select = false, scroll = true, place, from = 1, frames = 1 }:
  { select?: boolean; scroll?: boolean; place?: () => void; from?: number; frames?: number } = {}): () => void {
  const land = touchScreen() ? null : caretOnceAnswered();
  let waits = from, looks = frames;
  const look = () => {
    frame = 0;
    if (--waits > 0) { frame = window.requestAnimationFrame(look); return; }
    const field = target() ?? null;
    if (!field?.isConnected && --looks > 0) { frame = window.requestAnimationFrame(look); return; }
    if (!land) { if (field?.isConnected) placeOwn(field, select, scroll, place); return; }
    // A field that puts its caret itself (a formula's, at a character) does, the held keys going in as it takes the focus.
    if (place && field?.isConnected) { place(); land(field.contains(document.activeElement) ? "kept" : null); return; }
    landInField(land, field, select, scroll);
  };
  let frame = window.requestAnimationFrame(look);
  return () => {
    if (!frame) return;
    window.cancelAnimationFrame(frame);
    frame = 0;
    land?.(null);
  };
}

// A field takes the caret asked for: an editor's through the caret's own
// place (placeCaret), a plain input focused here, the held keys going in as
// it takes the focus; the page left where it is without `scroll`. A control
// the press opened on instead (a window's tab) takes the held keys at once
// (playHeldTypingAtControl).
function landInField(land: CaretLanding, field: HTMLElement | null, select: boolean, scroll = true) {
  if (!field?.isConnected) { land(null); return; }
  if (field.isContentEditable || fieldAsleep(field)) {
    land(field.contains(document.activeElement) && !holdingTyping() ? "kept" : { field, position: select ? "select" : "end", scroll });
    return;
  }
  placeOwn(field, select, scroll);
  const focused = field.contains(document.activeElement);
  if (focused && !(field instanceof HTMLInputElement || field instanceof HTMLTextAreaElement)) playHeldTypingAtControl();
  land(focused ? "kept" : null);
}

// The field's own focus: its words chosen, or the caret where it puts it.
function placeOwn(field: HTMLElement, select: boolean, scroll: boolean, place?: () => void) {
  if (place) place();
  else if (select && (field instanceof HTMLInputElement || field instanceof HTMLTextAreaElement)) field.select();
  else field.focus({ preventScroll: !scroll });
}

/**
 * A new comment's box — opened by ⌘⇧M, a block's "댓글", the chosen words'
 * or table cells' — takes the caret once it is drawn, and what is typed from
 * the press until then is held for it and typed there (caretOnceAnswered), as
 * Notion's, where what is typed right after ⌘⇧M goes into the comment
 * (2026-11-22: the box took the caret a frame after it was drawn, and the
 * first letters went into the code block or the table cell it was opened
 * from). A box opened with nothing asked takes the caret as before.
 */
// The caret the press asked for, until its box is drawn.
let asked: CaretLanding | null = null;
// How long a press's ask waits for its box; one never drawn gives what was held back.
const BOX_WAIT_MS = 1000;

/** A press opens a new comment's box: what is typed until the box has the caret waits for it. */
export function askCaretForNewComment() {
  // A touch screen holds nothing, its keyboard down till the box has the caret (focusOpenedField).
  if (touchScreen()) return;
  // Asked again before its box came: the newer ask has the hold, the older goes nowhere.
  const land = caretOnceAnswered(), previous = asked;
  asked = land;
  previous?.(null);
  window.setTimeout(() => {
    if (asked !== land) return;
    asked = null;
    land(null);
  }, BOX_WAIT_MS);
}

/**
 * A new comment's box is drawn in `box`: its field takes the caret asked for
 * and what was held is typed there — kept where it is when the field took
 * the caret on its own and what was held is in. With none asked, `otherwise`.
 */
export function giveNewCommentCaret(box: HTMLElement | null, otherwise: () => void) {
  const land = asked;
  if (!land) { otherwise(); return; }
  asked = null;
  // The new comment's own box (CommentComposer), not a thread's reply box beside it.
  const field = box?.querySelector<HTMLElement>("[data-comments-composer] [data-comment-field]") ?? null;
  if (!field) { land(null); otherwise(); return; }
  landInField(land, field, false);
}
