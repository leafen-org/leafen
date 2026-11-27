import { useEffect, useSyncExternalStore } from "react";

// The pages this tab shows locked for its person right now: a page view or
// a page's peek (usePageLockState.ts) and a database page
// (DatabaseRowPage.tsx) say so while they show it. A database in such a page
// reads it here to tell what the page's lock stops — its table and views —
// from what the lock leaves to its rows' own pages and its own lock
// (rowPageUnderPageLock.ts); the page's text this device couldn't send is
// asked of the server by it (refusedTextLetGo.ts).

const shown = new Map<string, number>();
const listeners = new Set<() => void>();

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
}

function tell() {
  for (const listener of listeners) listener();
}

/** Says, while the caller shows it, that the page is locked for the person. */
export function useShowPageLockedForMe(pageId: string, lockedForMe: boolean) {
  useEffect(() => {
    if (!pageId || !lockedForMe) return undefined;
    shown.set(pageId, (shown.get(pageId) ?? 0) + 1);
    tell();
    return () => {
      const left = (shown.get(pageId) ?? 1) - 1;
      if (left > 0) shown.set(pageId, left);
      else shown.delete(pageId);
      tell();
    };
  }, [lockedForMe, pageId]);
}

/** Whether a view shows the page locked for the person. */
export function usePageLockedForMe(pageId: string) {
  return useSyncExternalStore(subscribe, () => Boolean(pageId) && shown.has(pageId), () => false);
}
