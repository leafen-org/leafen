import { createContext } from "react";

/** The originals whose content is shown around a block. */
export const SyncedOriginalTrail = createContext<ReadonlySet<string>>(new Set());

/**
 * Whether the blocks around are a synced block's content shown again, in a
 * copy on the original's page; they keep the page's anchors to the original.
 */
export const SyncedMirror = createContext(false);
