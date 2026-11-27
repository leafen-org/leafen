// On a phone, putting the soft keyboard down ends the editing of the page's
// text: the caret leaves the page's body or title, and the caret's tools
// (NativePhoneToolbar) go with it, as Notion's Android app (seen 2026-11-21
// on a Galaxy A90, w2270, twice: with the keyboard put down the caret and the
// tool bar went, the page's own foot bar coming back). Android Chrome keeps
// the field focused with its keyboard down, its caret and Leafen's tools
// still drawn; iPhone Safari lets the field go itself. Only the page's own
// text is let go: a field in a window or sheet, and a database's name in the
// page (richCellCaretKept.ts), keep their focus as before. A field the
// page put the keyboard away from itself (`virtualkeyboardpolicy` manual: a
// strip's list in the keyboard's place, stripPanelState.ts) keeps it too.
import { phoneScreen } from "./screenKind";
import { softKeyboardShown } from "./softKeyboard";

const PAGE_TEXT = "[data-native-editor-content], [data-page-title-editor]:not([data-database-page-name])";

/** Starts letting the page's text go when a phone's keyboard is put down. */
export function watchKeyboardDownEndsEditing() {
  const shown = window.visualViewport;
  if (!shown) return;
  // The keyboard is told by the window's height at one width and one zoom:
  // a turned phone or a pinch changes the height with no keyboard moving.
  let up = false, width = window.innerWidth, scale = shown.scale;
  shown.addEventListener("resize", () => {
    const now = softKeyboardShown(), same = window.innerWidth === width && Math.abs(shown.scale - scale) < 0.01;
    const wasUp = up;
    up = now; width = window.innerWidth; scale = shown.scale;
    if (!wasUp || now || !same || !phoneScreen()) return;
    const active = document.activeElement;
    if (active instanceof HTMLElement && active.isContentEditable && active.closest(PAGE_TEXT)
      && active.getAttribute("virtualkeyboardpolicy") !== "manual") {
      window.getSelection()?.removeAllRanges();
      active.blur();
    }
  });
}
