import type { GraphView } from "@/contentGraph";
import type { DbProperty, DbView, Page, ViewConfig } from "@/lib/types";
import { nativeBoardQueryBounds } from "./boardViewModel";
import { applyView, effectiveFilterGroup } from "./query";
import { filterForSource } from "./dashboardMultiSourceFilter";

export { dashboardFilterProperties, dashboardFilterSources, DashboardFilterSourcesContext } from "./dashboardFilterSources";

/**
 * A dashboard's own filters, set from "필터" in its toolbar, narrow every
 * widget showing the source of the property each filters, the widgets' own
 * filters kept apart, as Notion's (seen 2026-11-05: a pill under the toolbar,
 * "초기화 · 모두에게 저장" while it is the viewer's own, the table widget then
 * "필터 편집하기 · + 새 페이지" and the chart empty; 2026-11-17: its filters
 * name the properties of each widget source, dashboardFilterSources.ts; one
 * over several sources' properties narrows each one's widgets by its own,
 * dashboardMultiSourceFilter.ts).
 * Answers the rows of the given source (the dashboard's own unless named)
 * the dashboard keeps, or null while none of its filters is on that source.
 */
export function dashboardKeptRowIds(
  dashboard: GraphView,
  rows: readonly { id: string }[],
  snapshot: { pagesById: Readonly<Record<string, unknown>>; propertiesByDatabase: Readonly<Record<string, readonly DbProperty[] | undefined>>; currentUserId: string },
  currentPageId?: string,
  dataSourceId = dashboard.dataSourceId,
): ReadonlySet<string> | null {
  const { quickFilters, filters, filterConjunction, filterGroup } = dashboard.config as ViewConfig;
  const props = [...snapshot.propertiesByDatabase[dataSourceId] ?? []];
  // A filter over several sources' properties reads this source's own (dashboardMultiSourceFilter.ts).
  const every = Object.values(snapshot.propertiesByDatabase).flatMap((list) => list ?? []);
  const own = quickFilters?.flatMap((filter) => filterForSource(filter, props, every) ?? []);
  const config: ViewConfig = { quickFilters: own, filters, filterConjunction, filterGroup };
  const bounds = nativeBoardQueryBounds;
  if (!effectiveFilterGroup(config, new Map(props.map((property) => [property.id, property])), bounds)) return null;
  const pagesById = snapshot.pagesById as Record<string, Page>;
  const pages = rows.flatMap(({ id }) => pagesById[id] ?? []);
  const view = { id: dashboard.id, databaseId: dataSourceId, name: dashboard.name, type: "table", position: 0, config } as DbView;
  const kept = applyView(pages, props, view, pagesById, {
    currentUserId: snapshot.currentUserId, currentPageId, propsByDb: snapshot.propertiesByDatabase as Record<string, DbProperty[]>,
  }, bounds);
  return new Set(kept.map(({ id }) => id));
}
