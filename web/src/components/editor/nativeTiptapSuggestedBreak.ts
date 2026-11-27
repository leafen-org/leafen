import { TextSelection, type EditorState, type Transaction } from "@tiptap/pm/state";
import { nativeProjectionMeta } from "./nativeTiptapBinding";
import { nativeOffsetAt } from "./nativeTiptapInline";
import { lineAboveProps } from "./nativeTiptapKeys";
import type { nativeTiptapSelectionContext } from "./nativeTiptapSelectionContext";

type Selected = NonNullable<ReturnType<typeof nativeTiptapSelectionContext>>;

/** A break drawn in suggesting mode: the new line's text length, and where the line it came from now ends (a split's). */
export type DrawnSuggestedBreak = { tr: Transaction; length: number; cutAt: number | null };

/**
 * Enter in suggesting mode draws its new line at once, as Notion's does
 * (seen 2026-11-15 in Chrome at 1440, two tabs: Enter at a line's end, then
 * two letters typed at once, made a new line holding them, in the same card
 * as the line's other suggested text; judged by the letters typed after),
 * before the page holds the suggested block: what is typed next goes into
 * it, not onto the end of the line before it. The line has the block's id,
 * so the page's own drawing of the block takes its place; nothing is written
 * from it (a projection's transaction). At the line's end the new line goes
 * after the block, or first in an open toggle's children; before the end a
 * break splits the line, the text after it moving to the new line, which the
 * caret goes to the start of; at the start the new empty line goes before
 * it, the caret staying. Null where nothing is drawn: a range chosen, a
 * synced copy's line.
 */
export function drawSuggestedBreak(
  state: EditorState, selected: Selected, newBlockId: string, next: { type: string; firstChild: boolean }, splitType: string | null,
): DrawnSuggestedBreak | null {
  const { block, node, contentFrom } = selected;
  if (!state.selection.empty || block.id !== block.nativeId) return null;
  const { schema } = state;
  const from = state.selection.from, containerStart = contentFrom - 2, containerEnd = containerStart + block.container.nodeSize;
  const line = (type: string, text = node.content.cut(0, 0), props: Record<string, unknown> = {}) => schema.nodes.nativeBlock.create(
    { id: newBlockId, sourceId: null, childrenSourceId: null, projectionStatus: null, kind: type, props },
    schema.nodes.nativeText.create(null, text),
  );
  const tr = state.tr.setMeta(nativeProjectionMeta, true).setMeta("addToHistory", false);
  if (from === contentFrom + node.content.size) {
    const children = block.container.lastChild?.type.name === "nativeChildren" ? block.container.lastChild : null;
    if (!next.firstChild) tr.insert(containerEnd, line(next.type)).setSelection(TextSelection.create(tr.doc, containerEnd + 2));
    else if (children) {
      const at = containerEnd - children.nodeSize;
      tr.insert(at, line(next.type)).setSelection(TextSelection.create(tr.doc, at + 2));
    } else {
      tr.insert(containerEnd - 1, schema.nodes.nativeChildren.create(null, line(next.type)))
        .setSelection(TextSelection.create(tr.doc, containerEnd + 2));
    }
    return { tr, length: 0, cutAt: null };
  }
  if (!splitType) return null;
  if (from === contentFrom) {
    // The line above is the one Enter leaves there while editing (a to-do as checked as it).
    tr.insert(containerStart, line(splitType, undefined, lineAboveProps(splitType, block.container.attrs.props)));
    return { tr, length: 0, cutAt: null };
  }
  const offset = from - contentFrom, end = contentFrom + node.content.size;
  const tail = node.content.cut(offset);
  const length = nativeOffsetAt(node.content, node.content.size) - selected.start;
  tr.delete(from, end);
  const at = tr.mapping.map(containerEnd);
  tr.insert(at, line(splitType, tail)).setSelection(TextSelection.create(tr.doc, at + 2));
  return { tr, length, cutAt: selected.start };
}
