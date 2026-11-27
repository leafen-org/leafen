import type { Node, Schema } from "@tiptap/pm/model";
import type { GraphBlock } from "@/contentGraph";
import { TEXT_BLOCK_TYPES } from "@/lib/blockSemantics";
import { sanitizeTextSpans } from "@/lib/textOperations";
import type { TextSpan } from "@/lib/types";
import { nativeRichTextToFragment } from "./nativeTiptapInline";
import { blockCaptionShown, blockCaptionSpans } from "./blockCaptionModel";

/** The same native field projection is used for stored blocks and paste. */
export function nativeTiptapBody(block: Pick<GraphBlock, "type" | "content">, rich: readonly TextSpan[], schema: Schema, iconOnly = false): Node[] {
  const cells = Array.isArray(block.content.cells) ? block.content.cells : [[]];
  const body = block.type === "table_row" ? schema.nodes.nativeTableCells.create(null,
    (cells.length ? cells : [[]]).map(cell => schema.nodes.nativeTableCell.create(null,
      nativeRichTextToFragment(sanitizeTextSpans(cell) ?? [], schema)))) : block.type === "code"
    ? schema.nodes.nativeCode.create(null, nativeRichTextToFragment(rich, schema, true))
    : TEXT_BLOCK_TYPES.has(block.type) && !iconOnly
      ? schema.nodes.nativeText.create(null, nativeRichTextToFragment(rich, schema))
      : schema.nodes.nativeWidget.create();
  const content = [body];
  if (blockCaptionShown(block as GraphBlock)) content.push(schema.nodes.nativeCaption.create(null,
    nativeRichTextToFragment(blockCaptionSpans(block as GraphBlock), schema)));
  return content;
}
