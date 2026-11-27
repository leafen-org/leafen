import { createContext, useContext, useRef } from "react";
import type { Page } from "@/lib/types";

/**
 * The rows an automation may set a relation to, by the related database: the
 * rows its view already reads, as a relation cell offers them.
 */
export const AutomationRelationRowsContext = createContext<((dataSourceId: string) => Page[]) | null>(null);

export function useAutomationRelationRows() {
  return useContext(AutomationRelationRowsContext);
}

/**
 * A related database's rows as an automation lists them (its value window,
 * a condition's pages), kept as the same list while they are the same pages
 * in the same order: the view reads them anew each time it is asked, and a
 * list new at each drawing sorted and drew a window of 5,000 pages again at
 * each move of the pointer (2026-11-22: 161 ms a row). None while `wanted`
 * is false or nothing provides them.
 */
export function useAutomationRelationPages(dataSourceId: string | undefined, wanted = true): Page[] | undefined {
  const rowsOf = useAutomationRelationRows();
  const read = wanted && dataSourceId && rowsOf ? rowsOf(dataSourceId) : undefined;
  const kept = useRef<Page[] | undefined>(undefined);
  const before = kept.current;
  if (!read || !before || before.length !== read.length || read.some((page, index) => page !== before[index])) kept.current = read;
  return kept.current;
}
