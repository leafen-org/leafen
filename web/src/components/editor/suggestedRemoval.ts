import type { GraphBlock } from "@/contentGraph";
import { SUGGESTING_BLOCK_TYPES } from "./suggestingInputModel";

/**
 * The blocks a removal suggests away, from those chosen, in the page's
 * order: text blocks that stand, as Notion's suggesting mode takes them
 * (seen 2026-11-18 in Chrome, a page and a database row's side peek: "/"
 * "삭제", a block's "⋮⋮" "삭제" and Backspace over chosen blocks strike each
 * line's words under one suggestion, an empty line among them; "삭제" on a
 * divider does nothing). A block suggested in is left out: its own
 * suggestion is rejected instead (page_suggestion_removals.go).
 */
export function suggestedRemovalBlocks(blocks: readonly GraphBlock[]) {
  return blocks.filter((block) => SUGGESTING_BLOCK_TYPES.has(block.type)
    && !(typeof block.content.suggestionId === "string" && block.content.suggestionId)).map((block) => block.id);
}
