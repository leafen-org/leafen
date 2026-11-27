import { createContext, useContext } from "react";

/**
 * Whether the property cell drawn here only shows its value: a locked page's
 * table draws the same cells as when it may be edited, and only this flips
 * (2026-11-13: a lock swapped every cell of a page's tables for another
 * drawing of its value, 1,330 cells and a frame of 200–280 ms; Notion's
 * cells look the same locked). Its parts that edit take no keyboard stop and
 * do not wake; the table's room for them takes no pointer (TableGridRow).
 */
export const CellReadOnlyContext = createContext(false);

export function useCellReadOnly() {
  return useContext(CellReadOnlyContext);
}

/** What a cell's part that edits takes while its cell only shows its value. */
export function readOnlyStop(readOnly: boolean) {
  return readOnly ? { tabIndex: -1, "aria-disabled": true } as const : null;
}
