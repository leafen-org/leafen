import { useLayoutEffect, useRef, type RefObject } from "react";

// The box a database shows its chosen view in, named for that view
// (DatabaseView), which the view's tab controls (aria-controls).
const PANEL = "database-view-panel-";

/** The id of the box showing the view `viewId`. */
export function viewPanelId(viewId: string) {
  return PANEL + viewId;
}

/** The view shown in the box `element` stands in, or null outside a database's views. */
export function viewShownAround(element: Element): string | null {
  return element.closest(`[id^="${PANEL}"]`)?.id.slice(PANEL.length) ?? null;
}

/**
 * Runs `letGo` once the box the element of `ref` stands in shows another
 * view than when it was last drawn — a view's tab pressed, one chosen in
 * "N개 더 보기" — before the new view is painted: what a table chose there,
 * its chosen cell (useChosenCellOutlivesRow) and its range of cells, a
 * timeline's table's too (useTableCellRange), stays behind with the view it
 * was chosen in, the new view choosing nothing, as Notion's.
 */
export function useViewSwitchLetsGo(ref: RefObject<HTMLElement | null>, letGo: () => void) {
  const view = useRef<string | null | undefined>(undefined);
  const latest = useRef(letGo);
  latest.current = letGo;
  useLayoutEffect(() => {
    const shown = ref.current ? viewShownAround(ref.current) : null;
    if (view.current === shown) return;
    const another = view.current !== undefined;
    view.current = shown;
    if (another) latest.current();
  });
}
