import type { GraphBlock } from "@/contentGraph";
import { editorBlockChildren, insertEditorBlock } from "./editorGraphModel";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";

/**
 * The line a page with none makes for the caret: its first, an empty
 * paragraph, made outside the page's history, as Notion's empty page holds
 * one before anything is written (seen 2026-11-16 in Chrome at 1440, twice:
 * a click under a new page's title, "body first" typed, then ⌘Z three times
 * — the first takes the words, and the line stays through the other two).
 * A page with lines makes a click's line a step of its own, which ⌘Z takes
 * away, as Notion's does. None when the page has lines or can't be written.
 */
export function pageFirstLine(adapter: NativeEditorGraphAdapter): GraphBlock | null {
  if (editorBlockChildren(adapter.graph, null).length) return null;
  const made = insertEditorBlock(adapter.graph, null, "paragraph");
  adapter.restoreGraph(made.graph, made.operations);
  return adapter.graph.blocks.some((block) => block.id === made.block.id) ? made.block : null;
}
