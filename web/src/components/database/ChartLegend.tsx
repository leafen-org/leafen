import { useId } from "react";
import chartStyles from "./chartView.module.css";

/** A label's width as the chart draws it, 12px in the page's typeface, to the part of a pixel. */
let labelMeasure: CanvasRenderingContext2D | null | undefined;
export function chartTextWidth(text: string) {
  if (labelMeasure === undefined) {
    labelMeasure = typeof document === "undefined" ? null : document.createElement("canvas").getContext("2d");
    if (labelMeasure) labelMeasure.font = `12px ${getComputedStyle(document.body).fontFamily}`;
  }
  return labelMeasure ? labelMeasure.measureText(text).width : text.length * 8;
}
/**
 * How tall a 12px line of a chart's labels stands in Notion's own typeface, the system's, as this screen draws it:
 * its font's ascent and descent each to the device pixel — 15 on a screen of one pixel to a pixel and 14 on one of two
 * (seen 2026-11-12, Notion's chart room under its plot 1 taller on the one) — by which Notion's chart lays out what
 * goes under its plot.
 */
const NOTION_FONT = 'ui-sans-serif, -apple-system, system-ui, "Segoe UI Variable Display", "Segoe UI", Helvetica, "Apple Color Emoji", Arial, sans-serif';
let notionLine: { ratio: number; height: number } | undefined;
export function chartNotionLine() {
  if (typeof document === "undefined") return 15;
  const ratio = window.devicePixelRatio || 1;
  if (notionLine?.ratio !== ratio) {
    const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
    svg.setAttribute("style", "position:fixed;inset-inline-start:0;inset-block-start:0;visibility:hidden;pointer-events:none");
    const text = document.createElementNS("http://www.w3.org/2000/svg", "text");
    text.setAttribute("style", `font:12px ${NOTION_FONT}`);
    text.textContent = "Ag";
    svg.appendChild(text);
    document.body.appendChild(svg);
    const height = text.getBBox().height;
    svg.remove();
    notionLine = { ratio, height: height > 0 ? height : 15 };
  }
  return notionLine.height;
}

/** A label's width in whole pixels, rounded up. */
export function chartLabelWidth(text: string) {
  return Math.ceil(chartTextWidth(text));
}

/** A legend row under the one before it, when the row above is full. */
export const LEGEND_ROW = 18;

// Each item's mark takes 16 across and its name starts 4 after it; items sit
// 20 apart inside 8 of the legend's own room on each side, the legend in the
// middle of the chart (Notion's, seen 2026-11-08: "진행 중 · 시작 전 · 완료"
// 207 wide from 243 in a 692 chart, "날짜" alone 57 from 318).
const MARK = 16, NAME_GAP = 4, ITEM_GAP = 20, PAD = 8;

export interface ChartLegendEntry { key: string; label: string; fill: string }

/**
 * Where each entry of a legend under a chart sits: in one row while they fit
 * across the chart less its 10 on each side, then in the rows after it, each
 * row starting where the first does.
 */
export function chartLegendRows(entries: readonly ChartLegendEntry[], width: number) {
  const room = width - 20 - PAD * 2;
  const rows: { entry: ChartLegendEntry; x: number }[][] = [];
  let x = 0, widest = 0;
  for (const entry of entries) {
    const itemWidth = MARK + NAME_GAP + chartLabelWidth(entry.label);
    if (!rows.length || (x > 0 && x + itemWidth > room)) { rows.push([]); x = 0; }
    rows[rows.length - 1].push({ entry, x });
    widest = Math.max(widest, x + itemWidth);
    x = Math.round(x + itemWidth + ITEM_GAP);
  }
  const left = Math.round((width - widest - PAD * 2) / 2) + PAD;
  return rows.map((row) => row.map((item) => ({ ...item, x: left + item.x })));
}

/**
 * One legend entry from its left edge, its mark in the middle of its row: a
 * rounded 8px square, or for a line a stroke as the line draws 15 across (and
 * its point, when the line has only one) over a 3 high band of its area's
 * fade, 0.1 to nothing, while the line fills under it (Notion's, seen
 * 2026-11-12), its name in 12px beside it.
 */
export function ChartLegendItem({ entry, x, y, line }: {
  entry: ChartLegendEntry; x: number; y: number;
  line?: { point: boolean; area?: boolean };
}) {
  const fade = useId().replace(/:/g, "");
  return <g data-chart-legend={entry.key}>
    {line
      ? <>
        {line.area && <>
          <defs><linearGradient id={fade} x1="0" x2="0" y1="0" y2="1">
            <stop offset="0" style={{ stopColor: entry.fill, stopOpacity: 0.1 }} /><stop offset="1" style={{ stopColor: entry.fill, stopOpacity: 0 }} />
          </linearGradient></defs>
          <rect x={x + 0.625} y={y} width={MARK - 1.25} height={3} fill={`url(#${fade})`} data-chart-legend-area />
        </>}
        <line className={chartStyles.line} x1={x + 0.625} x2={x + MARK - 0.625} y1={y} y2={y} style={{ stroke: entry.fill }} />
        {line.point && <circle className={chartStyles.isolatedPoint} cx={x + MARK / 2} cy={y} r={3} style={{ fill: entry.fill }} />}
      </>
      : <rect x={x + 4} y={y - 4} width={8} height={8} rx={2} style={{ fill: entry.fill }} />}
    <text className={chartStyles.legendText} x={x + MARK + NAME_GAP} y={y - 0.5} dominantBaseline="central">{entry.label}</text>
  </g>;
}
