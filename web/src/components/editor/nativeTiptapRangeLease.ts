import type { Slice } from "@tiptap/pm/model";
import type { EditorState, Transaction } from "@tiptap/pm/state";
import type * as Y from "yjs";
import { nativeTiptapRangeSelection, projectedBlocks, resolveNativeTiptapSelection, type NativeRelativeSelection } from "./nativeTiptapBinding";
import { nativeTiptapTextFields, type NativeTextField } from "./nativeTiptapTextFields";

/** A delayed product control can edit only the unchanged native field it opened. */
export type NativeTiptapRangeLease = {
  from: number;
  to: number;
  occurrence: string;
  field: NativeTextField;
  expected: Slice;
  relative?: NativeRelativeSelection;
};

export function captureNativeTiptapRange(state: EditorState, document: Y.Doc | null, from: number, to: number): NativeTiptapRangeLease | null {
  const field = nativeTiptapTextFields(projectedBlocks(state.doc)).find(target =>
    from >= target.contentFrom && to <= target.contentFrom + target.node.content.size);
  if (!field || from > to) return null;
  return { from, to, occurrence: field.block.id, field: field.field, expected: state.doc.slice(from, to),
    relative: document ? nativeTiptapRangeSelection(state, document, from, to) : undefined };
}

export function mapNativeTiptapRange(lease: NativeTiptapRangeLease, transaction: Transaction) {
  lease.from = transaction.mapping.map(lease.from, 1);
  lease.to = transaction.mapping.map(lease.to, -1);
}

export function resolveNativeTiptapRange(state: EditorState, document: Y.Doc | null, lease: NativeTiptapRangeLease) {
  const relative = lease.relative && document ? resolveNativeTiptapSelection(state.doc, document, lease.relative) : null;
  if (lease.relative && !relative) return null;
  const from = relative?.anchor ?? lease.from, to = relative?.head ?? lease.to;
  if (from > to || from < 0 || to > state.doc.content.size) return null;
  const field = nativeTiptapTextFields(projectedBlocks(state.doc)).find(target =>
    target.block.id === lease.occurrence && target.field === lease.field
    && from >= target.contentFrom && to <= target.contentFrom + target.node.content.size);
  if (!field || !state.doc.slice(from, to).eq(lease.expected)) return null;
  return { from, to };
}
