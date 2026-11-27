import { afterNextPaint } from "@/lib/afterPress";

// Fields whose caret lives inside them, out of the document's own selection.
const TEXT_FIELD = 'textarea, input:not([type="checkbox"]):not([type="radio"]):not([type="button"]):not([type="submit"]):not([type="reset"]):not([type="range"]):not([type="color"]):not([type="file"])';

/**
 * A caret left in a table's rows by a field the focus left — a cell's
 * editor given up for a chosen cell or a button, or a new row's title taken
 * away with its row (Escape) — is let go: the browser works out where such a
 * caret stands, among rows not laid out out of sight (content-visibility),
 * each time it lays the page out, ~17 ms each at CPU ×2 for a grouped table
 * of 30 rows by 40 properties, several times in each press after it (a
 * table's "+ 새 페이지": 340 → 180 ms, 2026-11-15). The field that takes the
 * focus next puts its own caret; a stretch of words someone chose stays.
 * A text field in the rows keeps its caret: WebKit (Safari and the desktop
 * app) tells the caret of a focused <input> or <textarea> as standing in the
 * field's parent, beside it, and letting that go took the field's caret
 * away, so a number cell's editor opened by a press took no keys until it
 * was pressed again (the desktop app, 2026-11-16).
 * Stopping it drops a look still to come. Returns what stops it.
 */
export function dropCaretsLeftInRows(list: HTMLElement): () => void {
  let queued: (() => void) | null = null;
  const check = () => {
    queued = null;
    const selection = window.getSelection();
    const anchor = selection?.anchorNode;
    if (!selection || !selection.isCollapsed || !anchor || !list.contains(anchor)) return;
    const active = document.activeElement;
    if (active && active !== document.body && (active.contains(anchor) || active.matches(TEXT_FIELD))) return;
    selection.removeAllRanges();
  };
  // Once the focus has moved and the frame after it is painted: the next field may still take it,
  // and reading where the caret stands lays the page out, which it then is.
  const soon = () => {
    queued ??= afterNextPaint(check);
  };
  list.addEventListener("focusin", soon);
  list.addEventListener("focusout", soon);
  return () => {
    list.removeEventListener("focusin", soon);
    list.removeEventListener("focusout", soon);
    queued?.();
  };
}
