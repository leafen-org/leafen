import { createContext } from "react";

/**
 * The originals whose content is shown around a block, outermost first: the
 * open page's own ID, then each original a copy shows on the way in; empty
 * on the open page itself.
 */
export const SyncedOriginalTrail = createContext<readonly string[]>([]);

/**
 * How many times a page's body is shown around a block, the open page's own
 * counted: a copy of the open page's body on that page shows it a second
 * time (pageId is the page the block stands on there).
 */
export function syncedPageShownAround(trail: readonly string[], pageId: string, originalPageId: string) {
  return (trail.length ? trail : [pageId]).filter((id) => id === originalPageId).length;
}

/**
 * Whether the blocks around are a synced block's content shown again, in a
 * copy on the original's page; they keep the page's anchors to the original.
 */
export const SyncedMirror = createContext(false);
