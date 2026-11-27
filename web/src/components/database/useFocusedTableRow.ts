import { useLayoutEffect, useRef, useState } from "react";

/**
 * The row the focus is in, for what a table reads it for (TableGrid): a row
 * kept drawn while its rows are windowed, and raised over the frozen columns'
 * cells (TableGridRow). A table with neither (`read` false) never reads it, so
 * a focus moving in it draws the table no more: a press on a group's
 * "+ 새 페이지" after Escape left a row drew the whole table once more for the
 * focus leaving that row, before the press made its row (2026-11-15). The
 * row is still followed, and given once the table comes to read it.
 */
export function useFocusedTableRow(read: boolean): readonly [string | null, (rowId: string | null) => void] {
  const latest = useRef<string | null>(null);
  const [shown, setShown] = useState<string | null>(null);
  useLayoutEffect(() => {
    if (read) setShown(latest.current);
  }, [read]);
  return [read ? shown : null, (rowId) => {
    latest.current = rowId;
    if (read) setShown(rowId);
  }];
}
