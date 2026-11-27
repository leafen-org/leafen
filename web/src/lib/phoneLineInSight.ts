// The line typed or commented on in a phone's page stays in sight over what
// stands at the foot of what shows of the page: the editing tools there over
// the soft keyboard (NativePhoneToolbar), the sheet a new comment is written
// in (phoneCommentSheet.ts), or that foot itself while the window is put back
// (phoneWindowHold.ts). The page's own scroll brings it there; the window
// stays where it is, under its top bar.

/** How far over what stands under it the line stands: the tools stand this far under the caret's line. */
export const LINE_GAP = 10;

/** The scroll in the page that holds `element`, short of the document (a phone's doesn't scroll), or of `page`. */
export function pageScrollOf(element: Element, page?: Element): HTMLElement | null {
  for (let node: Element | null = element; node && node !== document.body; node = node.parentElement) {
    if (node instanceof HTMLElement && /(auto|scroll)/u.test(getComputedStyle(node).overflowY) && node.scrollHeight > node.clientHeight) return node;
    if (node === page) break;
  }
  return null;
}

type LineOptions = {
  /** How far over `over` it stands. */
  gap?: number;
  /** Where the line begins on screen: a line taller than the room keeps that in sight instead. */
  top?: number;
  page?: Element;
};

/**
 * Scrolls the page holding `element` up so the line ending at `bottom`
 * stands `gap` over `over` (both places on screen), where it lies lower.
 * Returns how far it scrolled.
 */
export function bringLineOver(element: Element, bottom: number, over: number, options: LineOptions = {}) {
  const { gap = LINE_GAP, top, page } = options;
  const scroller = pageScrollOf(element, page);
  if (!scroller) return 0;
  let by = bottom + gap - over;
  if (top !== undefined) by = Math.min(by, top - scroller.getBoundingClientRect().top);
  if (by < 0.5) return 0;
  const was = scroller.scrollTop;
  scroller.scrollTop = was + by;
  return scroller.scrollTop - was;
}

/**
 * Where the line typed in ends on screen: the caret's, or an empty line's, in
 * an editable page; a field's first line.
 */
export function caretLineBottom(field: HTMLElement) {
  const selection = window.getSelection();
  const at = selection?.focusNode;
  if (selection && field.isContentEditable && at && field.contains(at)) {
    const range = document.createRange();
    range.setStart(at, selection.focusOffset);
    const caret = range.getClientRects()[0];
    if (caret) return caret.bottom;
    const line = at instanceof HTMLElement ? at : at.parentElement;
    if (line) return line.getBoundingClientRect().bottom;
  }
  const box = field.getBoundingClientRect();
  return Math.min(box.bottom, box.top + 48);
}
