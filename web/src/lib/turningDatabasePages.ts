import { useSyncExternalStore } from "react";

// A full-page database turning inline is a page on the server for a moment
// before its database leaves it: the page's kind changes first, for the way
// to the database its links keep. Meanwhile the page's link still reads as
// the database it is, rather than "새 페이지" with a page's icon, as Notion's
// does (2026-11-11).
const turning = new Set<string>();
const listeners = new Set<() => void>();
const tell = () => { for (const listener of listeners) listener(); };
const follow = (listener: () => void) => {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
};

/** Runs a full-page database's turn inline, its page read as a database all the while. */
export async function whileDatabasePageTurns<Value>(pageId: string, task: () => Promise<Value>): Promise<Value> {
  turning.add(pageId);
  tell();
  try {
    return await task();
  } finally {
    turning.delete(pageId);
    tell();
  }
}

/** Whether the page is a full-page database turning inline now. */
export function useDatabasePageTurning(pageId: string | null | undefined): boolean {
  return useSyncExternalStore(follow, () => Boolean(pageId) && turning.has(pageId!), () => false);
}
