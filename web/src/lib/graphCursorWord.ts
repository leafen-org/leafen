/**
 * The furthest position of the page's blocks the server has told the room
 * about, told to those following it without drawing the page again: each
 * word drew the whole page, some 0.23 seconds with a table of 100,000 rows,
 * for every edit another session made (2026-11-11).
 */
export type GraphCursorWord = {
  current(): number;
  subscribe(listener: () => void): () => void;
};

export function createGraphCursorWord(): GraphCursorWord & { tell(cursor: number): void } {
  let cursor = 0;
  const listeners = new Set<() => void>();
  return {
    current: () => cursor,
    subscribe(listener) {
      listeners.add(listener);
      return () => { listeners.delete(listener); };
    },
    tell(next) {
      if (next === cursor) return;
      cursor = next;
      for (const listener of [...listeners]) listener();
    },
  };
}

/** The block graph position the server's own awareness state tells the room, if it tells one. */
export function announcedGraphCursor(state: unknown) {
  const cursor = (state as { leafenGraph?: { cursor?: unknown } } | null)?.leafenGraph?.cursor;
  return typeof cursor === "number" && Number.isSafeInteger(cursor) && cursor > 0 ? cursor : 0;
}
