import { useEffect, useSyncExternalStore } from "react";

// A block can be drawn twice: an inline database opened alone in a side or
// center peek is drawn there and on the page under it, as Notion's is (seen
// 2026-11-09). What is asked of the block — a window it opens, the place a
// window is found from — goes to one of them, the copy that shows: the one
// drawn alone while it is, as pressing the page beside Notion's peek closes
// the peek first.

// The copies drawn alone by block (a peek's, or the database's own page's,
// where no other is drawn), counted as they come and go.
const drawnAlone = new Map<string, number>();
const listeners = new Set<() => void>();
let version = 0;

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
}

function changed() {
  version += 1;
  for (const listener of listeners) listener();
}

/** Whether this copy of the block, drawn alone or on its page, is the one that shows. */
export function shownBlockCopy(blockId: string, alone: boolean) {
  return alone || !drawnAlone.has(blockId);
}

/**
 * Whether this copy of the block is the one that shows, kept up as a copy
 * drawn alone comes and goes; a copy drawn alone counts itself in.
 */
export function useShownBlockCopy(blockId: string | undefined, alone: boolean) {
  useEffect(() => {
    if (!blockId || !alone) return undefined;
    drawnAlone.set(blockId, (drawnAlone.get(blockId) ?? 0) + 1);
    changed();
    return () => {
      const left = (drawnAlone.get(blockId) ?? 1) - 1;
      if (left > 0) drawnAlone.set(blockId, left); else drawnAlone.delete(blockId);
      changed();
    };
  }, [alone, blockId]);
  useSyncExternalStore(subscribe, () => version, () => version);
  return !blockId || shownBlockCopy(blockId, alone);
}

/** The element of the block's copy that shows: the one a peek draws alone, else the page's. */
export function shownBlockElement(blockId: string): HTMLElement | null {
  if (typeof document === "undefined") return null;
  const id = CSS.escape(blockId);
  return document.querySelector<HTMLElement>(`[data-inline-database-peek="${id}"] [id="block-${id}"]`)
    ?? document.getElementById(`block-${blockId}`);
}

/** Whether a peek draws the block alone (a database opened in a side or center peek). */
export function blockPeekedAlone(blockId: string) {
  return typeof document !== "undefined" && document.querySelector(`[data-inline-database-peek="${CSS.escape(blockId)}"]`) !== null;
}
