import { useSyncExternalStore } from "react";

/** The block whose discussion card is open, and whether it writes a new comment. */
export type OpenCommentCard = { blockId: string; composing: boolean };

let open: OpenCommentCard | null = null;
const listeners = new Set<() => void>();

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
}

/** A block's card says it opened; the card closing clears only itself. */
export function showOpenCommentCard(card: OpenCommentCard) {
  open = card;
  listeners.forEach((listener) => listener());
  return () => {
    if (open !== card) return;
    open = null;
    listeners.forEach((listener) => listener());
  };
}

/** The open card when it belongs to this block, for the block's mark. */
export function useOpenCommentCard(blockId: string) {
  return useSyncExternalStore(subscribe, () => (open?.blockId === blockId ? open : null), () => null);
}
