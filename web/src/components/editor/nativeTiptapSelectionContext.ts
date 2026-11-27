import type { EditorState } from "@tiptap/pm/state";
import { projectedBlocks } from "./nativeTiptapBinding";
import { nativeTiptapTextFields } from "./nativeTiptapTextFields";
import { fragmentToNativeRichText, nativeOffsetAt } from "./nativeTiptapInline";

/** Native field identity and offsets for product commands; PM owns selection. */
export function nativeTiptapSelectionContext(state: EditorState) {
  const fields = nativeTiptapTextFields(projectedBlocks(state.doc));
  const target = fields.find(field => state.selection.from >= field.contentFrom
    && state.selection.to <= field.contentFrom + field.node.content.size);
  if (!target) return null;
  const rich = fragmentToNativeRichText(target.node.content);
  return { ...target, rich, text: rich.map(span => span.text).join(""),
    start: nativeOffsetAt(target.node.content, state.selection.from - target.contentFrom),
    end: nativeOffsetAt(target.node.content, state.selection.to - target.contentFrom),
  };
}

/**
 * A selection from a text line run on into the simple table's cells below
 * is the line's text only for its toolbar, as Notion's drag there chooses
 * (seen 2026-11-07; a drag itself now ends at the line,
 * nativeTiptapWidgetSelection): the field it starts in, its end at that
 * field's end. Null for any other selection.
 */
export function nativeTiptapSelectionIntoTable(state: EditorState) {
  const fields = nativeTiptapTextFields(projectedBlocks(state.doc));
  const at = (position: number) => fields.find(field => position >= field.contentFrom
    && position <= field.contentFrom + field.node.content.size);
  const start = at(state.selection.from);
  const end = at(state.selection.to);
  if (!start || start.field !== "text" || !end || typeof end.field !== "number") return null;
  const rich = fragmentToNativeRichText(start.node.content);
  return { ...start, rich, text: rich.map(span => span.text).join(""),
    start: nativeOffsetAt(start.node.content, state.selection.from - start.contentFrom),
    end: nativeOffsetAt(start.node.content, start.node.content.size),
  };
}
