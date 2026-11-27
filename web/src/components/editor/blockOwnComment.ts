import { openBlockComment } from "@/components/comments/commentPanelEvents";
import type { GraphBlock } from "@/contentGraph";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";

/**
 * A block's own "댓글" — its menu's, its handle's, its mark's, ⌘⇧M with the
 * caret in it, a phone's tools — on its words (openBlockComment). A callout's
 * words are the line it opens with, which Leafen keeps as its first child, as
 * Notion's discussion on a callout carries its text (seen 2026-11-22:
 * text-context "알림 글").
 */
export function commentOnOwnBlock(adapter: Pick<NativeEditorGraphAdapter, "pageId" | "graph" | "text">, block: GraphBlock, unmarked = false) {
  const opening = block.type === "callout" ? adapter.graph.blocks.find((candidate) => candidate.parentId === block.id) : undefined;
  openBlockComment(adapter.pageId, block.id, adapter.text(opening ?? block), unmarked, block.type);
}
