export const PASTED_URL_MENU_REQUEST = "leafen:pasted-url-menu-request";

export function pastedUrl(value: string) {
  const trimmed = value.trim();
  if (!/^https?:\/\/\S+$/iu.test(trimmed)) return null;
  try {
    return new URL(trimmed).toString();
  } catch {
    return null;
  }
}

/**
 * An address pasted as a link: the block of its line (or the row of its
 * table cell), where the link stands in the editor, and its words when they
 * are not the address (a Markdown link's).
 */
export type PastedUrlMenuRequest = { blockId: string; url: string; text?: string; from: number; to: number };

export function dispatchPastedUrlMenu(request: PastedUrlMenuRequest) {
  window.dispatchEvent(new CustomEvent(PASTED_URL_MENU_REQUEST, { detail: request }));
}

/**
 * Blocks copied from another page were pasted here: the page they went in,
 * the blocks they became, and where they came from (PastedBlocksMenu).
 */
export const PASTED_BLOCKS_MENU_REQUEST = "leafen:pasted-blocks-menu-request";
export type PastedBlocksMenuRequest = { blockIds: string[]; source: { pageId: string; blockIds: string[] } };

export function dispatchPastedBlocksMenu(request: PastedBlocksMenuRequest) {
  window.dispatchEvent(new CustomEvent(PASTED_BLOCKS_MENU_REQUEST, { detail: request }));
}
