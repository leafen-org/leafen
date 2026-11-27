import { useCallback, useState } from "react";
import type { DatabaseRowPageSnapshot } from "@/databaseRowPageWire.generated";

// How many rows' pages a view keeps as last read: its opened row's, and those its menus read.
const KEPT_ROWS = 16;

type Reads = { latest: DatabaseRowPageSnapshot | null; byRow: ReadonlyMap<string, DatabaseRowPageSnapshot> };
const NONE: Reads = { latest: null, byRow: new Map() };

/**
 * A view's row pages as last read, row by row, as its row page service reads
 * them side by side (databaseRowPageService.ts): its opened row's, and those a
 * row's menu, a comment on it or its updates read. The opened row is drawn
 * from its own read and a menu from its row's, whichever came last (2026-11-22:
 * a right-click on another row put that row's read in the view's one row page,
 * and the open side peek went back to being read, its icon and cover lines
 * gone and its body busy, until its row was read again). `set` takes a read,
 * or none, which lets them all go: no row is opened.
 */
export function useRowPageReads() {
  const [reads, setReads] = useState<Reads>(NONE);
  const set = useCallback((snapshot: DatabaseRowPageSnapshot | null) => {
    setReads((current) => {
      if (!snapshot) return NONE;
      const byRow = new Map(current.byRow);
      byRow.delete(snapshot.row.id);
      byRow.set(snapshot.row.id, snapshot);
      for (const rowId of byRow.keys()) {
        if (byRow.size <= KEPT_ROWS) break;
        byRow.delete(rowId);
      }
      return { latest: snapshot, byRow };
    });
  }, []);
  const of = useCallback((rowId: string) => reads.byRow.get(rowId) ?? null, [reads]);
  return { latest: reads.latest, set, of };
}
