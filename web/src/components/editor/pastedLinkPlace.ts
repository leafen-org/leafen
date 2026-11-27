import type { Editor } from "@tiptap/core";
import type { TextSpan } from "@/lib/types";
import { projectedBlocks } from "./nativeTiptapBinding";
import { nativeTiptapTextFields } from "./nativeTiptapTextFields";
import type { PastedLinkPlace } from "./PastedLinkMenu";

/**
 * The line's text or table cell holding a pasted link where it was pasted,
 * while it still reads as the link's words (its address, or a Markdown
 * link's name); never a caption.
 */
export function pastedLinkField(editor: Editor, from: number, to: number, words: string) {
  if (editor.isDestroyed || from > to || to > editor.state.doc.content.size) return null;
  const field = nativeTiptapTextFields(projectedBlocks(editor.state.doc)).find(field => field.field !== "caption"
    && from >= field.contentFrom && to <= field.contentFrom + field.node.content.size);
  return field && editor.state.doc.textBetween(from, to, "\n", node => node.attrs.text ?? "") === words ? field : null;
}

/**
 * Where a pasted address stands, for the choice its menu makes
 * (PastedLinkMenu): `range` is the link's place, which the menus keep
 * where it is as the page changes, and `write` puts spans in its place.
 */
export function pastedLinkPlace(editor: Editor, range: { occurrenceId: string; from: number; to: number }, words: string,
  write: (spans: TextSpan[]) => boolean): PastedLinkPlace {
  const find = () => {
    const field = pastedLinkField(editor, range.from, range.to, words);
    return field?.block.id !== range.occurrenceId ? null
      : { wholeLine: field.field === "text" && range.to - range.from === field.node.content.size };
  };
  return { find, write: spans => find() !== null && write(spans) };
}
