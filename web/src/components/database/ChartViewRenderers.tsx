import { useRef, useState, type PointerEvent as ReactPointerEvent } from "react";
import { i18next } from "@/i18n";
import { sizeAtChartHeight, truncatedLabel } from "./chartGeometry";
import { ChartLegendItem, LEGEND_ROW, chartLegendRows } from "./ChartLegend";
import { ChartTip } from "./ChartTip";
import { chartInkColor } from "./chartViewPalette";
import type { ChartCategory } from "./chartViewSeries";
import type { ChartSettings } from "./chartViewSettings";
import chartStyles from "./chartView.module.css";
import { useChartWidth } from "./useChartWidth";

function tt(key: string): string {
  return i18next.t(`chartView:${key}`) as string;
}

// A number chart's title and value at each of Notion's heights (seen
// 2026-11-08: 16·20·24·32 and 48·64·80·96 at 300·400·600·800 high).
const NUMBER_TITLE = [[300, 16], [400, 20], [600, 24], [800, 32]] as const;
const NUMBER_VALUE = [[300, 48], [400, 64], [600, 80], [800, 96]] as const;

/**
 * A number chart as Notion's (seen 2026-10-05): its title over one large
 * value, in its color or the color the first rule that holds gives, in the
 * middle of a box as tall as the other charts (seen 2026-11-08: 692×400 at
 * "중간" in a page's text, the title 20px over the 64px value, both 600).
 */
export function NumberChart({ value, title, color, height, ariaLabel }: {
  value: string; title?: string; color: string; height: number; ariaLabel: string;
}) {
  return (
    <div className={chartStyles.numberChart} role="img" aria-label={`${ariaLabel}: ${value}`} data-chart-number
      style={{ height }}>
      {title !== undefined && <span className={chartStyles.numberTitle} data-chart-number-title
        style={{ fontSize: Math.round(sizeAtChartHeight(height, NUMBER_TITLE)) }}>{title}</span>}
      <span className={chartStyles.numberValue}
        style={{ color: chartInkColor(color), fontSize: Math.round(sizeAtChartHeight(height, NUMBER_VALUE)) }}>{value}</span>
    </div>
  );
}

function arcPath(cx: number, cy: number, radius: number, start: number, end: number) {
  const x1 = cx + radius * Math.cos(start), y1 = cy + radius * Math.sin(start);
  const x2 = cx + radius * Math.cos(end), y2 = cy + radius * Math.sin(end);
  return `M ${x1} ${y1} A ${radius} ${radius} 0 ${end - start > Math.PI ? 1 : 0} 1 ${x2} ${y2}`;
}

// Notion's ring and the words in its middle at each of its heights (seen
// 2026-11-08 at 300·400·600·800 high): the ring 126·252·360·468 across with
// a hole 85% of that; the total 48·72·84·108px less 5 for each of its
// letters ("4" 67px, "12" 62px, "1234567" 37px at 400), 600, in a line 13
// taller; its label 12·12·14·16px in a line 3 taller; the two together in
// the ring's middle.
const RING = [[300, 126], [400, 252], [600, 360], [800, 468]] as const;
const CENTER_TOTAL = [[300, 48], [400, 72], [600, 84], [800, 108]] as const;
const CENTER_LABEL = [[300, 12], [400, 12], [600, 14], [800, 16]] as const;

/**
 * A donut as Notion's draws one (seen 2026-10-05): a thin ring of the slices
 * from the top clockwise, each slice's value and share ("2 (50%)") or name
 * outside it on a short line, the total and "합계" in the middle, and the
 * legend under it (or beside it, or none), all in the one picture "다른
 * 이름으로 차트 저장" saves. With no slice — no row, or every value 0 — only
 * "데이터 없음" in its middle, as Notion's (seen 2026-10-11). A slice
 * hovered is drawn a tenth lighter with its tip over the pointer, "Done ·
 * 2 (40%)"; clicked, its rows open (Notion's, seen 2026-11-11).
 */
export function DonutChart({ categories, settings, height, width: fixedWidth, centerValue, ariaLabel, onOpen }: {
  categories: ChartCategory[]; settings: ChartSettings; height: number; centerValue?: string; ariaLabel: string;
  /** A width of its own, as its saved picture's; otherwise as wide as its box. */
  width?: number;
  /** Opens a slice's rows, where they open. */
  onOpen?(category: ChartCategory): void;
}) {
  const wrapRef = useRef<HTMLDivElement>(null);
  const svgRef = useRef<SVGSVGElement>(null);
  const [hovered, setHovered] = useState<{ key: string; x: number; y: number } | null>(null);
  const width = useChartWidth(wrapRef, fixedWidth);
  const listed = settings.hideNegative ? categories.filter((category) => category.value >= 0) : categories;
  const positive = listed.filter((category) => category.value > 0);
  const total = positive.reduce((sum, category) => sum + category.value, 0);
  const side = settings.legend === "side";
  const caption = Boolean(settings.showCaption && settings.caption);
  // A group at 0 draws no slice and leaves the legend too, its X axis showing empty groups or not (Notion's, seen
  // 2026-11-18 in Chrome, three databases: a select's option and the group for no value at 0 left out).
  const legendEntries = listed.filter((category) => category.value !== 0)
    .map((category) => ({ key: category.key, label: truncatedLabel(category.label, 12), fill: category.fill }));
  // At 크게 and 아주 크게 (600 and 800 high) Notion draws the ring with no legend under it (seen 2026-11-08).
  const legendUnder = settings.legend !== "off" && !side && height < 600;
  const legendRows = legendUnder && positive.length > 0 ? chartLegendRows(legendEntries, width) : [];
  // The picture is the chart's height, the ring in the middle of what the
  // legend's rows and the caption leave above them: the last row's marks 21
  // over the bottom, the ring's room 8 under the top and 29 over the first
  // row's, as Notion's (seen 2026-11-08 at 400 high: marks around 379, the
  // ring around 179).
  const captionY = height - 12;
  const legendY = height - 21 - (caption ? 26 : 0) - Math.max(0, legendRows.length - 1) * LEGEND_ROW;
  const plotBottom = legendUnder ? legendY - 29 : caption ? captionY - 20 : height - 8;
  const ringRadius = Math.max(0, Math.min(sizeAtChartHeight(height, RING), plotBottom - 8, width - 20)) / 2;
  const stroke = ringRadius * 0.15, radius = ringRadius - stroke / 2;
  const cx = side ? Math.max(radius + 90, width / 2 - 80) : width / 2, cy = (8 + plotBottom) / 2;
  const totalSize = Math.max(1, Math.round(sizeAtChartHeight(height, CENTER_TOTAL)) - 5 * (centerValue?.length ?? 0));
  const labelSize = Math.round(sizeAtChartHeight(height, CENTER_LABEL));
  const totalLine = settings.hideCenterValue ? 0 : totalSize + 13, labelLine = settings.hideCenterLabel ? 0 : labelSize + 3;
  const centerTop = cy - (totalLine + labelLine) / 2;
  let angle = -Math.PI / 2;
  const slices = positive.map((category) => {
    const sweep = (category.value / total) * Math.PI * 2;
    const slice = { category, start: angle, end: angle + sweep };
    angle += sweep;
    return slice;
  });
  // Its share to one decimal where it has one, "3 (37.5%)", as Notion's (seen 2026-11-02).
  const share = (category: ChartCategory) => total > 0 ? `${Math.round((category.value / total) * 1000) / 10}%` : "0%";
  const labelText = (category: ChartCategory) => settings.donutLabels === "name" ? category.label
    : settings.donutLabels === "name_and_value" ? `${category.label} ${category.formatted} (${share(category)})`
      : `${category.formatted} (${share(category)})`;
  const point = (key: string) => (event: ReactPointerEvent<SVGElement>) => {
    const box = svgRef.current?.getBoundingClientRect();
    if (box) setHovered({ key, x: event.clientX - box.left, y: event.clientY - box.top });
  };
  const slice = (category: ChartCategory) => ({
    "data-chart-segment": category.key, "data-opens": onOpen ? "" : undefined, className: chartStyles.slice, fill: "none", strokeWidth: stroke,
    style: { stroke: hovered?.key === category.key ? lighter(category.fill) : category.fill, strokeOpacity: category.shade },
    onPointerMove: point(category.key), onClick: onOpen ? () => onOpen(category) : undefined,
  });
  const tipped = hovered && positive.find((category) => category.key === hovered.key);
  return (
    <div ref={wrapRef} className={chartStyles.canvasWrap} data-chart-picture>
      <svg ref={svgRef} className={chartStyles.canvas} width={width} height={height} viewBox={`0 0 ${width} ${height}`} role="img"
        aria-label={ariaLabel} data-chart-svg data-chart-donut onPointerLeave={() => setHovered(null)}>
        {slices.length === 0 && <text className={chartStyles.noData} x={width / 2} y={cy + 5} textAnchor="middle" data-chart-no-data>{tt("noData")}</text>}
        {slices.map(({ category, start, end }) => slices.length === 1
          ? <circle key={category.key} cx={cx} cy={cy} r={radius} {...slice(category)} />
          : <path key={category.key} d={arcPath(cx, cy, radius, start, end)} {...slice(category)} />)}
        {slices.length > 1 && slices.map(({ category, start }) => (
          <line key={`gap:${category.key}`} className={chartStyles.donutGap} x1={cx + (radius - stroke / 2) * Math.cos(start)}
            y1={cy + (radius - stroke / 2) * Math.sin(start)} x2={cx + (radius + stroke / 2) * Math.cos(start)} y2={cy + (radius + stroke / 2) * Math.sin(start)} />
        ))}
        {settings.donutLabels !== "none" && slices.map(({ category, start, end }) => {
          const middle = (start + end) / 2, cos = Math.cos(middle), sin = Math.sin(middle);
          const inner = radius + stroke / 2 + 4, outer = radius + stroke / 2 + 18;
          const right = cos >= 0;
          return <g key={`label:${category.key}`} data-chart-label={category.key}>
            <polyline className={chartStyles.donutLeader} fill="none"
              points={`${cx + inner * cos},${cy + inner * sin} ${cx + outer * cos},${cy + outer * sin} ${cx + outer * cos + (right ? 10 : -10)},${cy + outer * sin}`} />
            <text className={chartStyles.donutLabel} x={cx + outer * cos + (right ? 14 : -14)} y={cy + outer * sin + 4}
              textAnchor={right ? "start" : "end"}>{labelText(category)}</text>
          </g>;
        })}
        {slices.length > 0 && centerValue !== undefined && !settings.hideCenterValue && (
          <text className={chartStyles.donutTotal} x={cx} y={centerTop + totalLine / 2} textAnchor="middle" dominantBaseline="central"
            style={{ fontSize: totalSize }}>{centerValue}</text>)}
        {slices.length > 0 && centerValue !== undefined && !settings.hideCenterLabel && (
          <text className={chartStyles.donutTotalLabel} x={cx} y={centerTop + totalLine + labelLine / 2} textAnchor="middle" dominantBaseline="central"
            style={{ fontSize: labelSize }}>{settings.centerLabel || tt("total")}</text>)}
        {/* Under the ring in rows, in the middle, or down its right side. */}
        {legendRows.flatMap((row, index) => row.map(({ entry, x }) => (
          <ChartLegendItem key={`legend:${entry.key}`} entry={entry} x={x} y={legendY + index * LEGEND_ROW} />
        )))}
        {slices.length > 0 && side && legendEntries.map((entry, index) => (
          <ChartLegendItem key={`legend:${entry.key}`} entry={entry} x={cx + radius + stroke + 136}
            y={cy - legendEntries.length * 10 + index * 20} />
        ))}
        {caption && <text className={chartStyles.caption} x={width / 2} y={captionY} textAnchor="middle"
          data-chart-caption>{settings.caption}</text>}
      </svg>
      {tipped && hovered && <ChartTip anchor={{ x: hovered.x, y: hovered.y, place: "above", gap: 24 }} width={width} height={height}
        rows={[{ key: tipped.key, label: tipped.label, value: `${tipped.formatted} (${share(tipped)})`, fill: tipped.fill, shade: tipped.shade }]}
        opens={Boolean(onOpen)} />}
    </div>
  );
}

/** A slice's color a tenth lighter, as Notion's hovered slice (seen 2026-11-11: 114,188,143 → 139,213,168). */
function lighter(fill: string) {
  const match = /^rgba?\((\d+),\s*(\d+),\s*(\d+)(,\s*[\d.]+)?\)$/u.exec(fill);
  if (!match) return fill;
  const [r, g, b] = match.slice(1, 4).map((channel) => Math.min(255, Math.round(Number(channel) + 25.5)));
  return match[4] ? `rgba(${r}, ${g}, ${b}${match[4]})` : `rgb(${r}, ${g}, ${b})`;
}
