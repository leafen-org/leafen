import type { EditorState } from "@tiptap/pm/state";
import { commentQuoteCellField, type CommentQuotePlaces } from "@/components/comments/commentQuotePlaces";
import { projectedBlocks } from "./nativeTiptapBinding";
import { nativeTiptapTextFields } from "./nativeTiptapTextFields";
import { fragmentToNativeRichText, nativeOffsetAt } from "./nativeTiptapInline";

/** Chosen words a comment or a reaction is left on: the first block's part, and where the rest lies. */
export type ChosenCommentText = { blockId: string; start: number; end: number; quote: string; places: CommentQuotePlaces };

/**
 * The chosen words a new comment or reaction quotes and marks, as Notion's
 * (seen 2026-11-09): in a block's text, in a media block's caption, or across
 * blocks — then one thread whose block is the first and whose quote has a
 * line for each block. Words chosen in one cell of a simple table are its
 * row's, marked in that cell, as Notion's (seen 2026-11-22 in Chrome at 1440,
 * a part of a cell's words and all of them alike: the panel under them took
 * "댓글", the thread its row's with the words its text-context). Cells take
 * none of a selection run on into them from other lines, which is those
 * lines' words, nor of one across cells, which Notion makes a range of cells.
 */
export function chosenCommentText(state: EditorState): ChosenCommentText | null {
  const { from, to } = state.selection;
  if (from >= to) return null;
  const parts: { blockId: string; field?: "caption"; start: number; end: number; text: string }[] = [];
  const cells: (typeof parts[number] & { cell: number })[] = [];
  for (const field of nativeTiptapTextFields(projectedBlocks(state.doc))) {
    const size = field.node.content.size;
    if (field.contentFrom + size <= from || field.contentFrom >= to) continue;
    const start = nativeOffsetAt(field.node.content, Math.max(from, field.contentFrom) - field.contentFrom);
    const end = nativeOffsetAt(field.node.content, Math.min(to, field.contentFrom + size) - field.contentFrom);
    if (end <= start) continue;
    const text = fragmentToNativeRichText(field.node.content).map(span => span.text).join("").slice(start, end);
    if (typeof field.field === "number") cells.push({ blockId: field.block.nativeId, cell: field.field, start, end, text });
    else parts.push({ blockId: field.block.nativeId, ...(field.field === "caption" ? { field: "caption" as const } : {}), start, end, text });
  }
  if (!parts.length && cells.length === 1) {
    const [{ blockId, cell, start, end, text }] = cells;
    return { blockId, start, end, quote: text, places: { quoteField: commentQuoteCellField(cell) } };
  }
  const [first, ...rest] = parts;
  if (!first) return null;
  return { blockId: first.blockId, start: first.start, end: first.end, quote: parts.map(part => part.text).join("\n"),
    places: { ...(first.field ? { quoteField: first.field } : {}),
      ...(rest.length ? { quoteBlocks: rest.map(({ blockId, field, start, end }) => ({ blockId, ...(field ? { field } : {}), start, end })) } : {}) } };
}
