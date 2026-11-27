import type { Slice } from "@tiptap/pm/model";
import { isBlockType } from "@/lib/blockSemantics";
import type { BlockContent } from "@/lib/types";
import { clipboardHtmlFragment } from "@/lib/clipboardHtml";
import { EDITOR_PASTE_BOUNDS } from "./editorClipboardPolicy";
import {
  admitPastedBlock, admitStandaloneSpans, assertMarkdownPasteChildren,
  assertMarkdownPasteDom, assertMarkdownPasteHtml, consumeMarkdownPasteInput,
  createMarkdownPasteContext,
} from "./markdownPasteBudget";
import { fragmentToNativeRichText } from "./nativeTiptapInline";

/** Admission runs before ProseMirror parses clipboard HTML, including code. */
export function admitNativeTiptapClipboard(data: DataTransfer) {
  const context = createMarkdownPasteContext(EDITOR_PASTE_BOUNDS);
  consumeMarkdownPasteInput(data.getData("text/plain"), context);
  const html = data.getData("text/html");
  if (html) {
    assertMarkdownPasteHtml(html, context);
    const parsed = clipboardHtmlFragment(html, context.bounds.richtext.maxHtmlCharacters);
    assertMarkdownPasteDom(parsed, context);
  }
  const bounds = EDITOR_PASTE_BOUNDS.clipboard;
  let total = 0;
  if (data.types.length > bounds.maxEntries) throw new RangeError("Clipboard entries exceed their bound.");
  for (const type of data.types) {
    const value = data.getData(type);
    total += type.length + value.length;
    if (value.length > bounds.maxEntryCharacters || total > bounds.maxTotalEntryCharacters) {
      throw new RangeError("Clipboard payload exceeds its bound.");
    }
  }
}

/** PM slices use the same native import budgets as HTML/Markdown imports. */
export function admitNativeTiptapSlice(slice: Slice) {
  const context = createMarkdownPasteContext(EDITOR_PASTE_BOUNDS);
  const stack = Array.from({ length: slice.content.childCount }, (_, index) => ({ node: slice.content.child(index), depth: 0 }));
  while (stack.length) {
    const { node, depth } = stack.pop()!;
    if (node.type.name === "nativeBlock") {
      if (!isBlockType(node.attrs.kind)) throw new RangeError("Clipboard block kind is unsupported.");
      const props = node.attrs.props;
      if (Object.keys(props).some(key => key === "__proto__" || key === "constructor" || key === "prototype")) {
        throw new RangeError("Clipboard block properties are unsupported.");
      }
      admitPastedBlock({ type: node.attrs.kind, content: props as BlockContent }, depth, context);
    }
    if (node.isTextblock) {
      admitStandaloneSpans(fragmentToNativeRichText(node.content), context);
      continue;
    }
    if (node.type.name === "nativeChildren") assertMarkdownPasteChildren(node.childCount, context);
    if (node.type.name === "nativeTableCells" && node.childCount > context.bounds.maxTableColumns) {
      throw new RangeError("Clipboard table columns exceed their bound.");
    }
    for (let index = node.childCount - 1; index >= 0; index--) {
      stack.push({ node: node.child(index), depth: depth + (node.type.name === "nativeBlock" ? 1 : 0) });
    }
  }
}
