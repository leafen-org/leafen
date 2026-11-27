import { BarChartIcon, DonutChartIcon, HorizontalBarChartIcon, LineChartIcon, NumberChartIcon, type LeafenIconSize } from "@/icons/leafen";
import type { ChartType } from "./chartViewModel";

/** The chart type buttons' pictures, as Notion's five (seen 2026-10-05, drawn to its ink 2026-11-18, chartSettingIcons.tsx). */
export function ChartTypeIcon({ type, size = "regular" }: { type: ChartType; size?: LeafenIconSize }) {
  if (type === "number") return <NumberChartIcon size={size} aria-hidden="true" />;
  if (type === "bar") return <BarChartIcon size={size} aria-hidden="true" />;
  if (type === "horizontal_bar") return <HorizontalBarChartIcon size={size} aria-hidden="true" />;
  if (type === "line") return <LineChartIcon size={size} aria-hidden="true" />;
  return <DonutChartIcon size={size} aria-hidden="true" />;
}
