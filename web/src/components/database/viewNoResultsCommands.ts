import { useContext, useMemo, type RefObject } from "react";
import { DashboardFilteredContext } from "./dashboardWidgetHeight";
import { useDatabaseFilterBar, type DatabaseFilterBarSurface } from "./databaseFilterBar";
import type { ViewNoResultsCommands } from "./DatabaseNoResults";
import type { DatabaseToolbarCommands } from "./databaseToolbarModel";
import { usePageLayoutTabMode } from "./pageLayoutTabEnvironment";

/**
 * What a view its search or filters leave empty offers, as Notion's (seen
 * 2026-11-11): clearing the search, and for "필터 편집하기" a view's own
 * settings at its filters; a dashboard widget's shows the dashboard's
 * filter bar while the dashboard's filters narrow it, and otherwise opens
 * the widget's own filters from its head's "필터". A page layout tab's
 * shows its own bar, its relation's pill on it (Notion's, seen 2026-11-12).
 */
export function useViewNoResultsCommands(toolbarCommands: RefObject<DatabaseToolbarCommands | null>,
  clearSearch: () => void, widgetSlot: HTMLElement | null, ownBar: DatabaseFilterBarSurface): ViewNoResultsCommands {
  // Read above the widget's own: the dashboard's bar.
  const dashboardBar = useDatabaseFilterBar();
  const narrowed = useContext(DashboardFilteredContext);
  const tab = usePageLayoutTabMode() !== null;
  return useMemo(() => ({
    clearSearch,
    openFilterSettings: () => {
      if (tab && !widgetSlot) return ownBar.shown ? undefined : ownBar.toggle();
      if (!widgetSlot) return toolbarCommands.current?.openViewSettings("filter");
      if (narrowed && dashboardBar) return dashboardBar.shown ? undefined : dashboardBar.toggle();
      const trigger = widgetSlot.querySelector<HTMLElement>('[data-database-toolbar-menu="filter"]');
      if (trigger) toolbarCommands.current?.openMenu("filter", trigger);
    },
  }), [clearSearch, dashboardBar, narrowed, ownBar, tab, toolbarCommands, widgetSlot]);
}
