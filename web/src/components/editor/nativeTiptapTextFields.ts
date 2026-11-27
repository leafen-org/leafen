import type { Node } from "@tiptap/pm/model";
import type { NativeProjectedBlock } from "./nativeTiptapBinding";

export type NativeTextField = "text" | "caption" | number;
export type NativeProjectedText = { block: NativeProjectedBlock; node: Node; contentFrom: number; field: NativeTextField };
const indexes = new WeakMap<readonly NativeProjectedBlock[], NativeProjectedText[]>();

/** Finds engine text fields without making graph captions/cells Y.Text roots. */
export function nativeTiptapTextFields(blocks: readonly NativeProjectedBlock[]) {
  const cached = indexes.get(blocks);
  if (cached) return cached;
  const fields: NativeProjectedText[] = [];
  for (const block of blocks) block.container.forEach((node, offset) => {
    const contentFrom = block.contentFrom + offset;
    if (node.type.name === "nativeTableCells") node.forEach((cell, cellOffset, index) => {
      fields.push({ block, node: cell, contentFrom: contentFrom + 1 + cellOffset, field: index });
    });
    else if (node.isTextblock) fields.push({ block, node, contentFrom,
      field: node.type.name === "nativeCaption" ? "caption" : "text" });
  });
  indexes.set(blocks, fields);
  return fields;
}
