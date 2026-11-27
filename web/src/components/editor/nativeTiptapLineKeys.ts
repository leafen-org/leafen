import { Extension } from "@tiptap/core";
import { TextSelection } from "@tiptap/pm/state";
import { imeComposing } from "@/lib/fieldKeyBoundary";

/**
 * Home and End go to the start and end of the caret's line on screen, with
 * Shift widening the selection there, as Notion's do on every system: macOS
 * leaves them to scroll the page and keeps the caret where it was (seen
 * 2026-10-30, Notion's End took the caret to the end of a wrapped line).
 * Every text engine (page body, plain fields, table cells, formulas) carries
 * this one rule.
 */
export const NativeTiptapLineKeys = Extension.create({
  name: "nativeLineKeys",
  addKeyboardShortcuts() {
    const move = (forward: boolean, extend: boolean) => () => {
      const { view, state } = this.editor;
      if (!this.editor.isEditable || imeComposing(view) || !view.hasFocus() || !(state.selection instanceof TextSelection)) return false;
      const selection = view.dom.ownerDocument.getSelection();
      if (!selection?.rangeCount || typeof selection.modify !== "function") return false;
      selection.modify(extend ? "extend" : "move", forward ? "forward" : "backward", "lineboundary");
      reveal(view.dom, selection);
      return true;
    };
    return {
      Home: move(false, false), End: move(true, false),
      "Shift-Home": move(false, true), "Shift-End": move(true, true),
    };
  },
});

/**
 * Scrolls the boxes around the caret, nearest first, until it shows, as
 * the browser does for its own caret moves: selection.modify scrolls
 * nothing, and End on a long line of code with wrapping off left its end
 * out of sight (Notion's box scrolls to it, seen 2026-11-11).
 */
function reveal(root: HTMLElement, selection: Selection) {
  const focus = selection.focusNode;
  if (!focus || !root.contains(focus)) return;
  const at = root.ownerDocument.createRange();
  at.setStart(focus, selection.focusOffset);
  at.collapse(true);
  for (let box = focus instanceof Element ? focus : focus.parentElement; box && box !== root.ownerDocument.body; box = box.parentElement) {
    const style = getComputedStyle(box);
    const across = /auto|scroll/u.test(style.overflowX) && box.scrollWidth > box.clientWidth;
    const down = /auto|scroll/u.test(style.overflowY) && box.scrollHeight > box.clientHeight;
    if (!across && !down) continue;
    const caret = at.getClientRects()[0] ?? at.getBoundingClientRect(), frame = box.getBoundingClientRect();
    if (!caret.width && !caret.height && !caret.left && !caret.top) return;
    // Across, a caret out of sight comes to the middle, as the browser's does (End shows a line's end, Home its start).
    const left = frame.left + box.clientLeft, right = left + box.clientWidth;
    const top = frame.top + box.clientTop, bottom = top + box.clientHeight;
    if (across && (caret.left < left || caret.right > right)) box.scrollLeft += caret.left - (left + box.clientWidth / 2);
    if (down) {
      if (caret.top < top) box.scrollTop -= top - caret.top;
      else if (caret.bottom > bottom) box.scrollTop += caret.bottom - bottom;
    }
  }
}
