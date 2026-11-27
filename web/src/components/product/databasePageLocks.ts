import { useCallback, useRef } from "react";
import type { DatabasePageLock } from "@/components/database/InlineDatabase";
import type { Page } from "@/types";

/**
 * A database page's own lock, for its database's view settings: one object
 * while the page is locked or not, so the database isn't drawn again for a
 * page list read again (2026-11-11: each list refresh drew a table of
 * 100,000 rows again). Setting it changes the page as listed then.
 */
export function useDatabasePageLocks(change: (page: Page, locked: boolean) => void) {
  const latestChange = useRef(change);
  latestChange.current = change;
  const kept = useRef(new Map<string, { page: Page; lock: DatabasePageLock }>());
  return useCallback((page: Page, editable: boolean): DatabasePageLock | undefined => {
    if (page.kind !== "database" || !editable) return undefined;
    const locked = Boolean(page.isLocked);
    const known = kept.current.get(page.id);
    if (known && known.lock.locked === locked) {
      known.page = page;
      return known.lock;
    }
    const entry = { page, lock: { locked, set: (next: boolean) => latestChange.current(entry.page, next) } };
    kept.current.set(page.id, entry);
    return entry.lock;
  }, []);
}
