import { useSyncExternalStore } from "react";
import { personValue, storePersonValue } from "./browserStorage";

// The tab a database's pages show under a tabbed layout, as Notion's (seen
// 2026-11-12, its local preference by the database): one for all the
// database's pages, kept on this browser for the person — a tab chosen on one
// page shows on the others and after a reload — and, as the layout builder is
// applied, the tab it showed last, which is the tab it opens on. Nothing
// kept is the content tab. With no one signed in, it lasts for the visit.
const NAME = "leafen:database-page-tab";
const visit = new Map<string, string | null>();
const listeners = new Set<() => void>();

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
}

/** The tab the database's pages show, by its data source; null for the content tab. */
export function shownDatabasePageTab(dataSourceId: string): string | null {
  if (visit.has(dataSourceId)) return visit.get(dataSourceId) ?? null;
  return personValue(NAME, { part: dataSourceId }) || null;
}

export function showDatabasePageTab(dataSourceId: string, tabId: string | null): void {
  visit.set(dataSourceId, tabId);
  storePersonValue(NAME, tabId, { part: dataSourceId });
  for (const listener of listeners) listener();
}

export function useShownDatabasePageTab(dataSourceId: string | undefined): string | null {
  return useSyncExternalStore(subscribe, () => dataSourceId ? shownDatabasePageTab(dataSourceId) : null);
}
