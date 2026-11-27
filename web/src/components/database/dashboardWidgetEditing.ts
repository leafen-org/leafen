import type { GraphBlock, GraphDataSource, GraphView } from "@/contentGraph";
import type { NativeDatabaseViewProductAdapter } from "@/lib/nativeDatabaseViewProductAdapter";
import { databaseBlockMembers, databaseListedOwnSourceIds } from "@/lib/nativeDataSourceManagement";
import { blockDataSourceName } from "@/lib/dataSourceGraphName";
import type { DashboardWidgetEditing } from "./DatabaseDashboardWidgets";
import { layoutDefaults } from "./viewLayoutDefaults";

/**
 * What an editor does to a dashboard's widgets, through the database's
 * adapter: its sources, the views a new widget can take after (the
 * database's tabs and widgets, dashboards aside), and the widget writes.
 * A dashboard asks a new widget's source first, as Notion's (seen
 * 2026-11-12), unless its database holds a source of its own and shows no
 * other: a view block's — "/대시보드" — stands on an unnamed source that is
 * none to chart. A source chosen there that the database neither holds nor
 * links is linked with its widget; a widget's "←" turns it to another source
 * as its settings' "데이터베이스" does.
 */
export function dashboardWidgetEditing({ block, dashboard, dataSources, views, adapter }: {
  block: GraphBlock;
  dashboard: GraphView;
  dataSources: readonly GraphDataSource[];
  views: readonly GraphView[];
  adapter: NativeDatabaseViewProductAdapter;
}): DashboardWidgetEditing {
  const members = databaseBlockMembers(block);
  const linked = members.linked.map(({ dataSourceId }) => dataSourceId);
  const own = databaseListedOwnSourceIds(block, views);
  // Each by its name, an untitled one by how many name its database (blockDataSourceName).
  const sources = [...own, ...linked].map((id) => {
    const source = dataSources.find((candidate) => candidate.id === id);
    return { id, title: source?.title ?? "", name: blockDataSourceName(block, source ?? { id, title: "" }) };
  });
  return {
    sources,
    asksSource: Boolean(adapter.dataSources) && (sources.length !== 1 || own.length === 0),
    views: views.filter((view) => view.type !== "dashboard"),
    add: (dataSourceId, type, placement) => adapter.addDashboardWidget(block, dashboard, dataSourceId, type, placement, () => true),
    addFrom: (candidate, placement) => members.own.includes(candidate.dataSourceId) || linked.includes(candidate.dataSourceId)
      ? adapter.addDashboardWidget(block, dashboard, candidate.dataSourceId, "chart", placement, () => true)
      : adapter.addLinkedDashboardWidget?.(block, dashboard, candidate, "chart", placement, () => true) ?? Promise.resolve(null),
    switchSource: async (widget, candidate) => widget.dataSourceId === candidate.dataSourceId
      || Boolean(await adapter.dataSources?.switchView(block, widget.id, candidate, layoutDefaults(widget.type, []))),
    update: (widget) => adapter.updateViews([widget], () => true),
    retype: (widget, type) => adapter.retypeDashboardWidget(widget, type, () => true),
    duplicate: (widget) => void adapter.duplicateDashboardWidget(block, dashboard, widget, () => true),
    remove: async (widgetId) => { await adapter.removeDashboardWidget(dashboard, widgetId, () => true); },
    arrange: (layout) => adapter.arrangeDashboard(dashboard, layout, () => true),
  };
}
