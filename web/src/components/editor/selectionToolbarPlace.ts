import type { EditorState } from "@tiptap/pm/state";
import type { EditorView } from "@tiptap/pm/view";
import { type RefObject, useMemo } from "react";
import type { PopoverRect } from "@/lib/popoverPlacement";
import { readsRightToLeft } from "@/lib/readingDirection";
import { projectedBlocks } from "./nativeTiptapBinding";
import { nativeTiptapSelectionIntoTable } from "./nativeTiptapSelectionContext";
import { nativeTiptapTextFields } from "./nativeTiptapTextFields";
import { selectionRanOutOfLine } from "./nativeTiptapWidgetSelection";
import type { SelectionToolbarPage } from "./SelectionToolbarView";

/**
 * Where the chosen text is drawn, as Notion's panel measures it (read and
 * seen 2026-11-09): the box of its lines, and its right edge where the
 * farthest of them ends, whichever way it was chosen — a line it runs out of
 * into the next block ends at that line's right edge. So does a selection run
 * on into a simple table's cells below, whose box is the line's, and a drag
 * let go past its line, which ends at the line (selectionRanOutOfLine) —
 * but in the first panel after the page opens, which counts only its words,
 * as Notion's (seen 2026-11-13 at 1440×723: a line dragged on into a table,
 * a code block, a divider or a page under it; the first panel by the words,
 * every later one out by the column — `countsRunOn`).
 * With it, the page it is in: the column's end edge (the top-level
 * block's box), the scrolling area's end edge and top, and the text itself,
 * whose scrolling area places the panel again as it scrolls.
 */
export function chosenTextPlace(view: EditorView, state: EditorState, countsRunOn: boolean): { rect: DOMRect; page: SelectionToolbarPage } {
  const { from, $from } = state.selection;
  const intoTable = nativeTiptapSelectionIntoTable(state);
  const to = intoTable ? intoTable.contentFrom + intoTable.node.content.size : state.selection.to;
  const ranOut = Boolean(intoTable) || (countsRunOn && selectionRanOutOfLine(view, state.selection));
  let left = Infinity, top = Infinity, right = -Infinity, bottom = -Infinity;
  const fields = nativeTiptapTextFields(projectedBlocks(state.doc))
    .filter(field => field.contentFrom <= to && field.contentFrom + field.node.content.size >= from);
  // The first and the last bound it; the lines between end at the same edge.
  for (const field of new Set([fields[0], fields.at(-1)])) {
    if (!field) continue;
    const start = field.contentFrom, end = start + field.node.content.size;
    try {
      const range = document.createRange(), head = view.domAtPos(Math.max(from, start)), tail = view.domAtPos(Math.min(to, end));
      range.setStart(head.node, head.offset); range.setEnd(tail.node, tail.offset);
      for (const line of range.getClientRects()) {
        if (line.height <= 0) continue;
        left = Math.min(left, line.left); top = Math.min(top, line.top); right = Math.max(right, line.right); bottom = Math.max(bottom, line.bottom);
      }
      const box = view.domAtPos(start).node;
      const text = (box instanceof Element ? box : box.parentElement)?.closest("[data-rt-editable]");
      if ((to > end || (ranOut && field === fields.at(-1))) && text) right = Math.max(right, text.getBoundingClientRect().right);
    } catch { /* A field not drawn yet counts for nothing. */ }
  }
  if (!(bottom > top)) {
    const end = view.coordsAtPos(to, -1);
    ({ left, top, bottom } = end); right = Math.max(right, end.right);
  }
  const block = $from.depth > 0 ? view.nodeDOM($from.before(1)) : null;
  return { rect: new DOMRect(left, top, right - left, bottom - top), page: pageAround(view.dom, block instanceof Element ? block : null) };
}

/** The page around text: the column's end edge (the top-level block's box), the scrolling area's end edge and top, and the text. */
function pageAround(owner: HTMLElement, block: Element | null): SelectionToolbarPage {
  const area = owner.closest("[data-page-scroll]")?.getBoundingClientRect();
  const columnBox = (block ? block.querySelector("[data-block-id]") ?? block : owner).getBoundingClientRect();
  // The column's and the area's end edges: their left read from the right.
  const rtl = readsRightToLeft(owner);
  return { column: rtl ? columnBox.left : columnBox.right, right: rtl ? area?.left ?? 0 : area?.right ?? window.innerWidth, top: area?.top ?? 0, owner };
}

/**
 * The page around a field of a block in a page's text — an inline
 * database's cell —, whose chosen words' panel keeps to the page's column as
 * the page's own words' does: out beside it when the area has room, its top
 * 2.5px over the words', as Notion's (seen 2026-11-16 in Chrome at 1440×723,
 * twice, a word double-clicked in an inline table's text cell, the cell in the
 * middle and at the table's right end: the panel at the column's edge 1215
 * plus 16, 376.5 for words from 379). None for a field elsewhere (a
 * full-page database's, a row page's).
 */
export function fieldPageAround(field: HTMLElement): SelectionToolbarPage | undefined {
  const content = field.closest("[data-native-editor-content]");
  let block: Element | null = field;
  while (content && block && block.parentElement !== content) block = block.parentElement;
  return content && block ? pageAround(field, block) : undefined;
}

/**
 * A box measured where it is each time it is read, one reading serving the
 * reads of one turn; a measure that finds nothing, or fails, keeps the last box.
 */
export function liveBox(measure: () => DOMRect | null, first: DOMRect): DOMRect {
  let last = first, fresh = false;
  const at = () => {
    if (fresh) return last;
    fresh = true; queueMicrotask(() => { fresh = false; });
    try { last = measure() ?? last; } catch { /* A place the page no longer draws keeps its last. */ }
    return last;
  };
  return { get x() { return at().x; }, get y() { return at().y; }, get width() { return at().width; }, get height() { return at().height; },
    get left() { return at().left; }, get top() { return at().top; }, get right() { return at().right; }, get bottom() { return at().bottom; },
    toJSON: () => at().toJSON() };
}

/**
 * The chosen text's box, read where it is as the panel's place reads it: the
 * page scrolled under the panel, it is placed again at its words by the same
 * rule, as Notion's (seen 2026-11-15 at 1440×723: wheeled 95px, the panel
 * stood again 16px under the words, or over them by the window's foot).
 */
export function liveChosenTextBox(view: EditorView, first: DOMRect, countsRunOn: () => boolean): DOMRect {
  return liveBox(() => view.isDestroyed || view.state.selection.empty ? null : chosenTextPlace(view, view.state, countsRunOn()).rect, first);
}

/**
 * Whether the chosen text is wholly out of the window, the panel then put
 * out of sight until it comes back, as Notion's (seen 2026-11-15 at 1440×723:
 * gone once the words' foot passed the window's top, or their top its foot;
 * there again, at its place, as they came back in).
 */
export function chosenTextAway(box: DOMRect) {
  return box.bottom < 0 || box.top > window.innerHeight;
}

/**
 * Where a list of the panel opens from, read once as it opens and kept while
 * it stays open: the page scrolled under it, the panel goes on following its
 * words and the list stands where it opened, as Notion's color list (seen
 * 2026-11-16 in Chrome at 1440×723, two tabs: wheeled 100px beside the page,
 * the panel 100px up with the words and the list at its place, over it) and
 * its type list until the pointer has left it (seen once the same day).
 * The first of the openers drawn counts.
 */
export function useOpenerAtOpen(open: boolean, ...openers: RefObject<Element | null>[]): () => PopoverRect | null {
  // A new holder for each opening.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const held = useMemo(() => ({ rect: null as PopoverRect | null }), [open]);
  return () => {
    const opener = openers.map(ref => ref.current).find(Boolean);
    if (!held.rect && opener) {
      const { top, bottom, left, right } = opener.getBoundingClientRect();
      held.rect = { top, bottom, left, right, contextElement: opener };
    }
    return held.rect;
  };
}
