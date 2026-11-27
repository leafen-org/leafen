// WebKit (Safari, and the Mac app's web view) gives a button or a checkbox
// pressed with the mouse no focus: the press sends the focus to the nearest
// focusable thing around it instead — the page's editor round a block's
// button, or nowhere for a control in a window over the page — so the next
// key went there. Escape after a form question's "필수" switch chose the
// database block instead of closing the question's window, and a page
// button's block stayed chosen after its press (2026-11-08, the test app).
// Chrome gives the pressed control the focus, as Notion does in Chrome and in
// its desktop app; here a mouse press on an enabled button or checkbox that
// no handler kept from the focus (one that keeps a caret where it is calls
// preventDefault) takes it in WebKit too, right after WebKit's own move — to
// the page or to something round the control, never one a handler made. Taken
// in the press itself, with WebKit's move held back, the control drew its
// keyboard ring (2026-11-15).
//
// Nor does WebKit take away a caret or chosen words a press on a control left
// in an editable text, as a database's name: they stay drawn there, and WebKit
// types into them whatever has the focus — a letter typed after ticking two
// rows' boxes renamed the page while the rows stayed chosen (2026-11-17, the
// test app). Chrome draws the caret only where the focus is and types only
// there, so after such a press a caret or chosen words in an editable text
// that doesn't hold the focus are let go. A press that keeps the caret calls
// preventDefault, as the selection toolbar's buttons do, and keeps it here too.

const CONTROL = 'button, input[type="checkbox"], input[type="radio"], input[type="button"], input[type="submit"], input[type="reset"]';
const EDITING_HOST = '[contenteditable=""], [contenteditable="true"], [contenteditable="plaintext-only"]';

let watching = false;

/** Whether this engine leaves a pressed button unfocused: WebKit, not Chrome's or another's. */
function pressLeavesFocus(agent: string) {
  return /AppleWebKit/u.test(agent) && !/Chrome|Chromium|CriOS|Edg|FxiOS|Android/u.test(agent);
}

export function watchWebKitPressFocus() {
  if (watching || typeof window === "undefined" || !pressLeavesFocus(window.navigator.userAgent)) return;
  watching = true;
  // A touch press keeps WebKit's own way: a phone's keyboard and its taps are its own.
  let mouse = false;
  window.addEventListener("pointerdown", (event) => { mouse = event.pointerType === "mouse"; }, true);
  // On the way out, after every handler of the page: one that keeps the focus where it is has said so by then.
  window.addEventListener("mousedown", (event) => {
    if (!mouse || event.button !== 0 || event.defaultPrevented || !(event.target instanceof Element)) return;
    const control = event.target.closest<HTMLButtonElement | HTMLInputElement>(CONTROL);
    if (!control || control.disabled || control === document.activeElement) return;
    window.setTimeout(() => {
      const now = document.activeElement;
      if (control.isConnected && (!now || now === document.body || (now !== control && now.contains(control)))) control.focus({ preventScroll: true });
      letGoOfTextLeftBehind();
    }, 0);
  });
}

/** Lets go of a caret or chosen words in an editable text that doesn't hold the focus. */
function letGoOfTextLeftBehind() {
  const selection = document.getSelection();
  const anchor = selection?.anchorNode;
  if (!selection || !anchor || selection.rangeCount === 0) return;
  const at = anchor instanceof HTMLElement ? anchor : anchor.parentElement;
  if (!at?.isContentEditable) return;
  const host = at.closest(EDITING_HOST);
  const now = document.activeElement;
  if (host && !(now && now !== document.body && host.contains(now))) selection.removeAllRanges();
}
