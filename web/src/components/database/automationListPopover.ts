import type { CSSProperties, RefObject } from "react";
import { placeAround, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import { readsRightToLeft } from "@/lib/readingDirection";
import { toolbarMenuColumn } from "./databaseToolbarModel";
import { toolbarButtonsEnd } from "./useDatabaseToolbarController";

/**
 * Where an inline database's automations open on a desktop, as Notion's
 * (seen 2026-11-15 at 1440, in two tabs): not at the window's right but as
 * a 290px popover where its view settings open — 3px under the toolbar's
 * buttons, from 6px past their end, no nearer than 12px to the window's
 * edge (toolbarMenuColumn) — the page staying as it is. Its own layer is
 * the list's, so this places it only. Null for a database's own page,
 * whose list hangs under its toolbar at the window's right.
 */
export function useAutomationListPopover(panelRef: RefObject<HTMLElement | null>, toolbarRef: RefObject<HTMLElement | null> | undefined): CSSProperties | null {
  const place = usePopoverPlacement(panelRef, toolbarRef ? (size, viewport) => {
    const toolbar = toolbarRef.current;
    const trigger = toolbar?.querySelector<HTMLElement>('[data-database-toolbar-menu="automations"], [data-database-toolbar-menu="view"]') ?? toolbar;
    const rect = popoverRectOf(trigger) ?? { top: 0, bottom: 0, left: viewport.width, right: viewport.width };
    const column = toolbarMenuColumn(rect, "view", viewport.width, trigger ? toolbarButtonsEnd(trigger) : undefined,
      undefined, readsRightToLeft(trigger));
    return placeAround({ ...rect, left: column.left, right: column.left }, size, viewport,
      { gap: column.gap, margin: column.margin, rise: true, cap: viewport.height * 0.8 });
  } : null, [toolbarRef], undefined, false);
  return toolbarRef ? popoverPlacementStyle(place) : null;
}
