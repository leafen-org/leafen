import type { NativeProjectedBlock } from "./nativeTiptapBinding";
import { fragmentToNativeRichText } from "./nativeTiptapInline";

/** Captions/cells remain native graph fields, rather than new Y.Text roots. */
export function nativeTiptapGraphProps(block: NativeProjectedBlock): Record<string, unknown> {
  const props = { ...block.container.attrs.props };
  if (block.node.type.name === "nativeTableCells") props.cells = Array.from({ length: block.node.childCount },
    (_, index) => fragmentToNativeRichText(block.node.child(index).content));
  block.container.forEach(node => {
    if (node.type.name === "nativeCaption") props.caption = fragmentToNativeRichText(node.content);
  });
  return props;
}

export function nativeTiptapGraphFieldsEqual(a: NativeProjectedBlock, b: NativeProjectedBlock) {
  if (a.node.type.name === "nativeTableCells" && !a.node.eq(b.node)) return false;
  const caption = (block: NativeProjectedBlock) => {
    let result = null as typeof block.node | null;
    block.container.forEach(node => { if (node.type.name === "nativeCaption") result = node; });
    return result;
  };
  const first = caption(a), last = caption(b);
  return first === last || !!first && !!last && first.eq(last);
}
