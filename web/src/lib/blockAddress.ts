import { pageAddressId } from "@/lib/inlineDatabaseAddress";

/**
 * Block addresses — a page's address at one of its blocks (`#block-<id>`) —
 * in one rule: an address names a block only when the block is on the page
 * it opens and is not that page itself. A full-page database's own block is
 * its page (its ID is the page's, or the address's the page goes by after
 * turning inline and back, pageAddressId), so an address at it opens the
 * page with nothing chosen, as Notion's opens a link to a full-page database,
 * and its own address with its ID after `#`, unchosen (seen 2026-11-22: link
 * block, mention, and /p/<db>#<db> pressed or loaded).
 */

const plainId = (id: string) => id.replaceAll("-", "").toLowerCase();

/** Whether two IDs name the same page or block, written with dashes (`#block-`) or without (Notion's `#<32 hex>`). */
export function sameAddressId(left: string, right: string) {
  return Boolean(left) && plainId(left) === plainId(right);
}

const BLOCK_ANCHOR = /^#block-([A-Za-z0-9_-]{1,128})$/u;
// Notion's form, the block's ID without its dashes, as page Markdown writes a
// block it can't hold (`/p/<page>#<block>`, 2026-11-10).
const NOTION_BLOCK_ANCHOR = /^#([0-9a-f]{32})$/iu;

/** The block an address's block link (`#block-<id>`, or Notion's `#<32 hex>`) names. */
export function blockLinkTarget(hash: string): string | null {
  const own = BLOCK_ANCHOR.exec(hash)?.[1];
  if (own) return own;
  const hex = NOTION_BLOCK_ANCHOR.exec(hash)?.[1]?.toLowerCase();
  return hex ? `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}` : null;
}

/** Whether a block ID names the page itself (its own ID, or the address its full-page database goes by). */
export function blockIsPageItself(pageId: string, blockId: string) {
  return sameAddressId(blockId, pageId) || sameAddressId(blockId, pageAddressId(pageId));
}

/** The anchor a page's address takes for a block on it: `#block-<id>`, none for no block or the page itself. */
export function blockAnchor(pageId: string, blockId: string | null | undefined) {
  return blockId && !blockIsPageItself(pageId, blockId) ? `#block-${encodeURIComponent(blockId)}` : "";
}
