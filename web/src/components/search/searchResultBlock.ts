import { blockAnchor } from "@/lib/blockAddress";
import type { SearchHit } from "./searchModel";

/**
 * A result found by a line of its page's body opens at that line's block,
 * chosen, which the server names (workspace_search_block.go), as Notion's
 * opens a body match at "#<block>" with the block chosen (seen 2026-11-11) —
 * a database item's and a template's too, full as its page: Notion's opens an
 * item found by its body at its own address and that block, chosen, whether
 * searched from another page or from its database's table or board with an
 * item open in a side or a center peek (seen 2026-11-22, four times). A
 * person's page in People opens as their profile, and a line found in a file
 * block's document opens at that file (hit.file).
 */
export function opensAtBlock(hit: SearchHit) {
  return !hit.database && !hit.row?.person && !hit.file?.blockId && hit.match === "body" && Boolean(hit.preview);
}

/** A result's block as asked: known once found, "" for none (its top). */
export type SearchBlockRead = { found?: string; reading: Promise<string> };

/** A result that opens at its page's top. */
export const SEARCH_PAGE_TOP: SearchBlockRead = { found: "", reading: Promise.resolve("") };

/** The address's part that opens a page at a block, none for its top or the page itself (blockAddress.ts). */
export function searchBlockAnchor(pageId: string, blockId: string) {
  return blockAnchor(pageId, blockId);
}

/**
 * Opens a result at its line's block: now when it is known, else once it is
 * — unless the reader went elsewhere meanwhile (a new tab opens it anyway).
 */
export function openAtSearchBlock(block: SearchBlockRead, newTab: boolean, open: (blockId: string) => void) {
  if (block.found !== undefined) {
    open(block.found);
    return;
  }
  const from = window.location.href;
  void block.reading.then((blockId) => {
    if (newTab || window.location.href === from) open(blockId);
  });
}
