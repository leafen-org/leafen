// How an update's changed line shows by its block's kind, the workspace's
// updates and a page's updates tab alike, as Notion's (seen 2026-11-17 by
// workers w1645 and w1658, two tabs each, first account, test workspace;
// drawn by updates/UpdateLine.tsx). The server keeps the same kinds for the
// workspace's updates (workspace_activity_block_lines.go).

/**
 * The blocks whose made line shows with no words: a button's, a
 * breadcrumb's, an image's and a synced block's an empty band — a button's
 * name not shown even where it has one — a bookmark's an empty slot with no
 * band, a callout's its box. A divider, a table of contents and a line with
 * no words show none.
 */
const BARE_KINDS = new Set(["button", "breadcrumb", "image", "synced_block", "bookmark", "callout"]);

/** Boxed under their kind's name (updates/UpdateBlockBox). */
const BOXED_KINDS = new Set(["code", "equation", "quote", "callout"]);

/** Whether a block made in an update is a line though it has no words. */
export const updateLineBare = (blockType: string | undefined) => BARE_KINDS.has(blockType ?? "");

/** Whether a made bare block shows no words even where it has some: all but a callout, whose words are its box's. */
export const updateLineWordless = (blockType: string | undefined) => updateLineBare(blockType) && blockType !== "callout";

/**
 * Whether a line is its box, with no band or padding of its own: a code
 * block's, an equation's, a quote's and a callout's, and a deleted button's,
 * boxed under "버튼" with its workflow's steps (seen 2026-11-17, two pages).
 */
export const updateLineBoxed = (blockType: string | undefined, change: string) =>
  BOXED_KINDS.has(blockType ?? "") || (blockType === "button" && change === "deleted");

/** Whether a line is an empty slot with no band: a made bookmark's. */
export const updateLineBlank = (blockType: string | undefined) => blockType === "bookmark";
