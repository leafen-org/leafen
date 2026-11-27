import { focusEditable } from "./focus";
import { placeNativeTiptapCaretAt } from "./nativeTiptapFocus";

type Point = { x: number; y: number };

/** The collapsed range a point on screen falls in, by the browser's own caret placement. */
function caretRangeAt(x: number, y: number): Range | null {
  const position = (document as Document & { caretPositionFromPoint?: (x: number, y: number) => { offsetNode: Node; offset: number } | null })
    .caretPositionFromPoint?.(x, y);
  if (position) {
    const range = document.createRange();
    range.setStart(position.offsetNode, position.offset);
    range.collapse(true);
    return range;
  }
  return document.caretRangeFromPoint?.(x, y) ?? null;
}

/**
 * Puts the caret in a line's text from a point beside it: at the place in the
 * text across from the point, so at a line's end right of it and at its start
 * left of it, on the line nearest the point's height — a click in the page's
 * margin beside a line, as Notion's (2026-10-16; measured once 2026-11-15 in
 * Chrome 1440 with no script before the press: a letter typed after a press
 * right of a line went at its end, far left at its start), and a finger's tap anywhere
 * on a line's block on a phone (2026-11-09).
 */
export function caretBesideLine(field: HTMLElement, point: Point): boolean {
  const rect = field.getBoundingClientRect();
  const x = Math.min(Math.max(point.x, rect.left + 1), rect.right - 1);
  const y = Math.min(Math.max(point.y, rect.top + 1), rect.bottom - 1);
  const caret = caretRangeAt(x, y);
  if (!caret || !field.contains(caret.startContainer)) {
    const blockId = field.closest<HTMLElement>("[data-block-id]")?.dataset.blockId;
    const rtl = getComputedStyle(field).direction === "rtl";
    return blockId ? focusEditable(blockId, (rtl ? point.x > rect.right : point.x < rect.left) ? "start" : "end") : false;
  }
  // A line of the editor takes it through the editor's own selection; a
  // field of no editor, as the browser places it.
  if (placeNativeTiptapCaretAt(field, caret.startContainer, caret.startOffset)) return true;
  field.closest<HTMLElement>('[contenteditable="true"]')?.focus({ preventScroll: true });
  const selection = window.getSelection();
  selection?.removeAllRanges();
  selection?.addRange(caret);
  return true;
}
