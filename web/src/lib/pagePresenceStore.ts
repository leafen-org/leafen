import { useSyncExternalStore } from "react";
import { EMPTY_PAGE_PRESENCE_SNAPSHOT } from "./pagePresenceModel";
import type { PagePresenceSnapshot } from "./pagePresenceView";

// Who is on an open page, for what shows them apart from the page's own
// collaboration: the faces beside its lines and a row page's bar.
const snapshots = new Map<string, PagePresenceSnapshot>();
const listeners = new Set<() => void>();

export function setPagePresenceSnapshot(pageId: string, snapshot: PagePresenceSnapshot | null) {
  if (snapshot) snapshots.set(pageId, snapshot);
  else snapshots.delete(pageId);
  for (const listener of listeners) listener();
}

export function usePagePresenceSnapshot(pageId: string | undefined) {
  return useSyncExternalStore(
    (listener) => {
      listeners.add(listener);
      return () => { listeners.delete(listener); };
    },
    () => (pageId ? snapshots.get(pageId) : undefined) ?? EMPTY_PAGE_PRESENCE_SNAPSHOT,
  );
}
