import { onRoomReturn } from "./commentRefreshEvents";
import { onGraphReaderWord } from "./graphReaderEvents";

/**
 * Follows a row's page changing elsewhere — its values, or its lock set or
 * lifted on another device — as its own page or a view's peek of it shows
 * it: the room's word that its row changed (lib/graphReaderEvents.ts), or
 * its room back after a break, when a word may have gone unheard
 * (commentRefreshEvents.ts announceRoomReturn).
 */
export function onRowChanged(rowId: string, listener: () => void): () => void {
  const stopWords = onGraphReaderWord("rows", rowId, listener);
  const stopReturns = onRoomReturn(rowId, listener);
  return () => {
    stopWords();
    stopReturns();
  };
}
