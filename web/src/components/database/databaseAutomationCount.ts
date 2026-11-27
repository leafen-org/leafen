import { useEffect, useRef, useSyncExternalStore } from "react";
import type { DatabaseAutomationService } from "@/databaseAutomationApi";
import { onAutomationRevision } from "@/lib/automationRevisionEvents";

// How many automations each database has, as its automations' list last
// read them in this window: the toolbar's ⚡ turns blue the moment one is
// made, and grey the moment the last goes, ahead of the page's next read.
// Each is kept with the count the page's read gave when it was noted, so a
// later read that says otherwise is taken over it.
type Noted = { count: number; read: number };
const noted = new Map<string, Noted>();
const listeners = new Set<() => void>();

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
}

/** The count a database's automations' list now holds, beside what the page's read said. */
export function noteDatabaseAutomationCount(databaseId: string, count: number, read: number | undefined) {
  const current = noted.get(databaseId);
  const next = { count, read: read ?? 0 };
  if (current && current.count === next.count && current.read === next.read) return;
  noted.set(databaseId, next);
  for (const listener of listeners) listener();
}

/**
 * How many automations a database has: what its list last held, else what
 * the page's read said. Given its automations (the database's toolbar, there
 * while it is shown), it follows the changes made elsewhere too: as the
 * page's room tells of one, the list is read again and its count noted, so
 * the ⚡, the view settings' "자동화 N" and where "자동화" opens follow at once
 * (lib/automationRevisionEvents.ts).
 */
export function useDatabaseAutomationCount(databaseId: string, read: number | undefined, follow?: DatabaseAutomationService | null) {
  const entry = useSyncExternalStore(subscribe, () => noted.get(databaseId), () => undefined);
  const readRef = useRef(read);
  readRef.current = read;
  const pageId = follow?.pageId;
  useEffect(() => {
    if (!follow || !pageId) return undefined;
    let reading: AbortController | null = null;
    const stop = onAutomationRevision(pageId, () => {
      // A later word asks again; the earlier read's answer is no longer wanted.
      reading?.abort();
      const controller = new AbortController();
      reading = controller;
      follow.list(controller.signal).then((items) => {
        if (!controller.signal.aborted) noteDatabaseAutomationCount(databaseId, items.length, readRef.current);
      }, () => undefined);
    });
    return () => { stop(); reading?.abort(); };
  }, [databaseId, follow, pageId]);
  const base = read ?? 0;
  return entry && entry.read === base ? entry.count : base;
}
