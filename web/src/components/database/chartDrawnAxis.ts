import type { GraphView } from "@/contentGraph";
import type { DbProperty, DbView } from "@/lib/types";
import { chartFirstAxis } from "./chartGroupingConfiguration";
import { isChartGroupableProperty } from "./chartViewBuckets";
import { recoveredNotionChartConfig } from "./chartViewModel";

/**
 * The property a chart's X axis runs along as ChartView draws it: its own
 * (chartGroupBy), else the one read from the view's configuration, else the
 * first by Notion's rule.
 */
export function chartDrawnAxisId(view: GraphView, properties: readonly DbProperty[]): string | undefined {
  const groupable = properties.filter(isChartGroupableProperty);
  const own = groupable.find((property) => property.id === view.config.chartGroupBy);
  const recovered = recoveredNotionChartConfig(view as unknown as DbView, properties).groupById;
  return (own ?? groupable.find((property) => property.id === recovered) ?? chartFirstAxis(groupable)?.property ?? groupable[0])?.id;
}

/**
 * What a chart's view-wide grouping (DatabaseGroupMenu) writes besides, so the
 * chart keeps drawing along the axis it drew: grouping named there leaves the
 * bars as they are, as Notion's (seen 2026-11-18), though a chart with no axis
 * of its own reads one from the view's configuration.
 */
export function chartAxisKept(view: GraphView, properties: readonly DbProperty[]): Record<string, unknown> {
  if (view.type !== "chart" || typeof view.config.chartGroupBy === "string") return {};
  const drawn = chartDrawnAxisId(view, properties);
  return drawn ? { chartGroupBy: drawn } : {};
}
