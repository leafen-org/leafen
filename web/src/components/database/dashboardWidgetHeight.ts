import { createContext, useContext } from "react";

/** A dashboard widget's card height: a chart in it fills the card, as Notion's (seen 2026-11-03). */
export const DashboardWidgetHeightContext = createContext<number | null>(null);

/** Sent on a widget's head slot to open its view's settings ("view") or its new view's ("newView"). */
export const DASHBOARD_WIDGET_SETTINGS_EVENT = "leafen-dashboard-widget-settings";

/** A view whose rows something outside it narrows — a dashboard widget's by its dashboard's filters, a page's relation tab by its relation: an empty one says so, as a filtered view does. */
export const DashboardFilteredContext = createContext(false);

/** A widget shown while its dashboard is in Edit mode: an empty one offers "필터 편집하기" alone, as Notion's (seen 2026-11-11). */
export const DashboardWidgetEditingContext = createContext(false);

/**
 * A dashboard widget's view out of Edit mode, where its table's head ends
 * with no "+ 속성 추가", as Notion's (seen 2026-11-11; in Edit mode, where the
 * widget's settings are offered, its head has it as any table's).
 */
export function useDashboardWidgetViewing(): boolean {
  const inWidget = useContext(DashboardWidgetHeightContext) !== null;
  const editing = useContext(DashboardWidgetEditingContext);
  return inWidget && !editing;
}
