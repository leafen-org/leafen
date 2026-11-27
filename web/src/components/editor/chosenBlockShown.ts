import { scrollIntoViewNearest } from "@/lib/scrollIntoViewNearest";
import { occurrenceBlockId } from "./blockOccurrences";

/**
 * The room a block the arrows or ⌘↑ ⌘↓ chose, or stretched the choice to,
 * keeps from its scroller's edges: the page follows it till its wash is 66
 * from the top or the foot, as Notion's (seen 2026-11-12 in a 723-high
 * window: ↓, ↑ and Shift+↓ leave the wash's foot at 657 and its top at 110);
 * the wash stands 2 inside the block.
 */
export const CHOSEN_BLOCK_ROOM = 64;

/** Where a chosen block is drawn: a block a synced copy draws again, there (blockOccurrences). */
export function chosenBlockDrawn(root: Element | null, at: string) {
  return root?.querySelector<HTMLElement>(`[data-native-occurrence="${CSS.escape(at)}"] [data-block-id]`)
    ?? root?.querySelector<HTMLElement>(`[data-block-id="${CSS.escape(occurrenceBlockId(at))}"]`) ?? null;
}

// The block a key moved the choice to shows; one taller than the window that Shift+↓ stretched it to, its foot (seen 2026-11-14).
export function showChosen(root: Element | null, at: string | undefined, foot = false) {
  if (at) scrollIntoViewNearest(chosenBlockDrawn(root, at), CHOSEN_BLOCK_ROOM, CHOSEN_BLOCK_ROOM, foot);
}
