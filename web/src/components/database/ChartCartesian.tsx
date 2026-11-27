import { useId, useRef, useState, type ReactNode } from "react";
import { i18next } from "@/i18n";
import { chartBarShares, chartDrawnGroupStyle, chartValueAxis, linePath, stackedParts } from "./chartGeometry";
import { chartCategoryAxis } from "./chartCategoryAxis";
import { CHART_FLAT_NAMES, CHART_SIDE_NAME_WIDTH, CHART_TURNED_BASELINE, chartCategoryNames, chartNameWithin, chartSideNameStep } from "./chartCategoryNames";
import { chartTickNames, chartTimeAxis } from "./chartTimeAxis";
import { chartTimeTickName } from "./chartViewBuckets";
import { chartLineColor, chartReferenceLineColor } from "./chartViewPalette";
import type { ChartCategory, ChartData } from "./chartViewSeries";
import type { ChartSettings } from "./chartViewSettings";
import chartStyles from "./chartView.module.css";
import { useChartWidth } from "./useChartWidth";
import { ChartLegendItem, LEGEND_ROW, chartLabelWidth, chartLegendRows, chartNotionLine, chartTextWidth } from "./ChartLegend";
import { ChartTip, type ChartTipAnchor } from "./ChartTip";
import { CHART_FACES_TALLER, ChartPersonFaces } from "./ChartPersonFaces";

// Hangul, kana and Han: letters whose line, written out as text, stands taller than a Latin one's.
const EAST_ASIAN_LETTERS = /[\u1100-\u11ff\u3040-\u30ff\u3130-\u318f\u3400-\u9fff\uac00-\ud7af\uf900-\ufaff]/;
/** The one line a range holding none of the values draws instead, named in English in every language as Notion's (seen 2026-11-12). */
const NO_DATA_SERIES = "No data";

/**
 * Words over a chart with nothing to draw, as Notion's (seen 2026-11-12, its chart-empty-state): 14px in the
 * secondary color on the page's, 8 around them, in the middle of a box 69 in and 10 down, to 10 from the end
 * and 46 shorter than the chart — over sideways bars 40 in and 85 shorter — whatever the plot.
 */
function ChartMessage({ text, horizontal, width, height }: { text: string; horizontal: boolean; width: number; height: number }) {
  const [x, shorter] = horizontal ? [40, 85] : [69, 46];
  const middle = { x: x + (width - x - 10) / 2, y: 10 + (height - shorter) / 2 };
  const textWidth = chartTextWidth(text) * 14 / 12;
  return <g data-chart-no-data>
    <rect className={chartStyles.noDataBack} x={middle.x - textWidth / 2 - 8} y={middle.y - 17} width={textWidth + 16} height={34} />
    <text className={chartStyles.noData} x={middle.x} y={middle.y + 5} textAnchor="middle">{text}</text>
  </g>;
}

export type ChartAxisNames = { x: string; y: string };

/**
 * A column, bar or line chart as Notion's draws one (seen 2026-10-05): its
 * value axis with grid lines, each group's bar or point (stacked, as shares
 * or side by side when the Y axis groups), value labels, reference lines,
 * the axes' names, a legend of the stacked groups and the caption, all in
 * the one picture "다른 이름으로 차트 저장" saves. With no group left to draw
 * — every one at 0 and "0 값 생략" on, say, under a filter no row meets — it
 * keeps its frame on a 0–60 scale, both axes named and "데이터 없음" in the
 * middle, as Notion's (seen 2026-10-11). A range set that holds none of the
 * values draws one unnamed group with nothing in it: its value axis, no
 * names, "축 범위로 인해 데이터 없음" over bars, and a line's legend "No data"
 * with no words (Notion's, seen 2026-11-12). Bars, lines and their areas run
 * past a range they leave and are cut at the plot's edges, bars standing on
 * zero or the range's nearer end; points and value labels outside it are not
 * drawn. A group hovered anywhere in its slot has the slot banded and its tip
 * over it; clicked, its rows open (Notion's, seen 2026-11-11).
 */
export function ChartCartesian({ data, kind, settings, height, width: fixedWidth, formatTick, formatCategoryTick = String, includeZero, counts = false, wholeSteps = false, percentFormat = false, axisNames, ariaLabel, lineTheme, onOpen }: {
  data: ChartData;
  kind: "bar" | "horizontal_bar" | "line";
  settings: ChartSettings;
  height: number;
  /** A width of its own, as its saved picture's; otherwise as wide as its box. */
  width?: number;
  formatTick(value: number): string;
  formatCategoryTick?(value: number): string;
  includeZero: boolean;
  /** The values count rows, stepping by whole numbers. */
  counts?: boolean;
  /** A count of rows or of unique values, a range set for it never stepping by a fraction. */
  wholeSteps?: boolean;
  /** The values show as percents, a range set for them stepping by fractions where it needs. */
  percentFormat?: boolean;
  axisNames: ChartAxisNames;
  ariaLabel: string;
  /** The color one line draws in, under its theme. */
  lineTheme: string;
  /** Opens a group's rows, where they open. */
  onOpen?(category: ChartCategory): void;
}) {
  const gradientId = useId().replace(/:/g, "");
  const [hovered, setHovered] = useState<number | null>(null);
  const wrapRef = useRef<HTMLDivElement>(null);
  const width = useChartWidth(wrapRef, fixedWidth);
  const { categories, stacked } = data;
  const style = chartDrawnGroupStyle(kind, settings.groupStyle);
  const percent = stacked && style === "percent";
  const singleFill = chartLineColor(lineTheme);
  const holdsNone = categories.length > 0 && chartValueAxis(categories, stacked, kind, settings, { includeZero, counts, wholeSteps, percentFormat }).holdsNone;
  // The groups drawn, named and hovered, and the slots along the categories they take: one empty one where none is.
  const groups = holdsNone ? [] : categories;
  const slots = Math.max(1, groups.length);
  // A line of its own is named by its X axis's property under it ("— 상태", "— 날짜"), as Notion's (seen 2026-11-06).
  const singleLine = kind === "line" && !stacked && categories.length > 0 && settings.legend !== "off";
  const legendGroups = holdsNone ? kind === "line" && settings.legend !== "off" ? [{ key: "no-data", label: NO_DATA_SERIES, hidden: false, fill: singleFill }] : []
    : stacked && settings.legend !== "off" ? data.stackGroups.filter((group) => !group.hidden)
      : singleLine ? [{ key: "values", label: axisNames.x, hidden: false, fill: singleFill }] : [];
  const legendRows = chartLegendRows(legendGroups, width);
  // X and Y are the screen's axes, as Notion's: a horizontal bar chart's X
  // is its values', under the plot (seen 2026-10-10).
  const empty = categories.length === 0;
  const showX = empty || settings.axisLabels === "x_axis" || settings.axisLabels === "both";
  const showY = empty || settings.axisLabels === "y_axis" || settings.axisLabels === "both";
  const horizontal = kind === "horizontal_bar";
  // People along a column or line chart's X axis stand as their faces (ChartPersonFaces).
  const personFaces = Boolean(data.personAxis) && !horizontal && !empty && !holdsNone;
  // The value axis names its ticks as the values read, shares' too without a percent sign (Notion's, seen 2026-11-12: 0, 25, … 100).
  const tickText = formatTick;
  const grid = settings.gridLines;
  const valueLines = horizontal ? grid === "vertical" || grid === "both" : grid === "horizontal" || grid === "both";
  const categoryLines = horizontal ? grid === "horizontal" || grid === "both" : grid === "vertical" || grid === "both";

  // The plot, then under it the categories' or values' names, the X axis's
  // name, the legend and the caption, all inside the chart's height: the plot
  // gives up the room they take.
  // Up the side the values take 10 and their widest label and 8, to the
  // nearest pixel as Highcharts rounds the plot's edges, 30 more for the Y
  // axis's name; the plot runs to 10 from the end and from 20 under the top,
  // as Notion's (seen 2026-11-05: right 952 in a 962 chart; 2026-11-12: 31 for
  // "10" 13.13 wide, 25 for "5" 7.4 wide, 56 with axis names, top 20 by bars,
  // sideways bars and lines alike). A bar chart's side holds its groups'
  // names the same way: 10, the widest and 8 (Notion's, seen 2026-11-12: 67
  // for "Kind 없음" 49.39 wide). The page's own typeface draws digits a little
  // narrower and Hangul a little wider than Notion's, the edge moving with it.
  const top = 20;
  const caption = Boolean(settings.showCaption && settings.caption);
  // The value axis, its range's steps by the room the plot leaves it: up a column or line chart's side first as
  // if its groups' names lie flat, again if they turn and take more; along a bar chart's foot once its side is known.
  const valueAxisAlong = (length: number) => chartValueAxis(categories, stacked, kind, settings, { includeZero, counts, wholeSteps, percentFormat, length });
  // Each line under the plot by how far under its bottom it falls, as Notion's, whose chart makes room for a line
  // of its labels as tall as its typeface's line, `line`: 15 on a screen of one pixel to a pixel (chartNotionLine;
  // seen 2026-11-12 at 400 high, by bars, sideways bars and lines; 14 on one of two, by a plot 1 lower): the names'
  // baseline 20 under it, the X axis's name 16 and a line under them, a legend row's middle 17 and a line under what
  // is over it and each next row 18, the caption 15 and a line under the names or 40 under the legend, and 3 and a
  // line from the last baseline to the bottom, 28 from a legend's middle — 38 by names alone (36.5 and their room
  // when they turn). Days' and numbers' names, which Notion writes out as text and not drawn, stand a line 1 taller
  // where one has Korean in it: "2026년 1월", "점수 없음" and "수식 없음" by a plot 1 higher than "5" and "10" along
  // numbers or any select's or relation's names, Korean or not (seen 2026-11-12); under sideways bars the values'
  // names stand there instead, as under any other.
  // With no names under it, as where a range holds none of the values, the plot runs to 15 from the bottom, 5 more
  // under a filled line for its ticks, whatever its labels' line (Notion's, seen 2026-11-12: 385, and 338 over its
  // legend, at 400 high).
  function plotFoot(namesTaller: number) {
    const line = chartNotionLine();
    const namesTallerByOne = (data.dateAxis || data.numberNames) && !namesTaller && !horizontal
      && groups.some((category) => EAST_ASIAN_LETTERS.test(category.label));
    const namesBase = holdsNone && !horizontal ? (kind === "line" && !settings.hideLineFill ? 5 : 0) + 12 - line
      : 20 + namesTaller + (namesTallerByOne ? 1 : 0) + (personFaces ? CHART_FACES_TALLER : 0);
    const xNameUnder = namesBase + 16 + line;
    const legendUnder = (showX ? xNameUnder : namesBase) + 17 + line;
    const lastLegend = legendUnder + (legendRows.length - 1) * LEGEND_ROW;
    const captionUnder = legendRows.length ? lastLegend + 40 : (showX ? xNameUnder : namesBase) + 15 + line;
    const below = caption ? captionUnder + 3 + line : legendRows.length ? lastLegend + 28
      : showX ? xNameUnder + 3 + line : namesTaller ? 36.5 + namesTaller : namesBase + 3 + line;
    return { xNameUnder, legendUnder, captionUnder, below };
  }
  // A line across days runs from the plot's one edge to the other, as Notion's (seen 2026-11-06); across groups, from their middles.
  const edgeToEdge = kind === "line" && data.dateAxis && groups.length > 1;
  // A column or line chart's group names, which turn and take more room under the plot where they do not fit
  // (Notion's, seen 2026-11-12), along the axis its values' side leaves: 18 and their widest name.
  const flatAxis = valueAxisAlong(height - top - plotFoot(0).below);
  const flatLeft = Math.round(18 + Math.max(...flatAxis.ticks.map((tick) => chartTextWidth(tickText(tick))))) + (showY ? 30 : 0);
  const flatLength = width - 10 - flatLeft;
  const flatSlot = flatLength / slots;
  const categoryNames = horizontal || data.numericAxis || data.timeAxis || empty || holdsNone || personFaces ? null
    : chartCategoryNames(categories.map((category) => category.label), {
      slot: flatSlot, length: flatLength, height, date: Boolean(data.dateAxis),
      center: (index) => edgeToEdge ? flatLeft + flatLength * index / (categories.length - 1) : flatLeft + flatSlot * index + flatSlot / 2,
    });
  const namesTaller = categoryNames?.turned ? categoryNames.room - CHART_FLAT_NAMES : 0;
  const { xNameUnder, legendUnder, captionUnder, below } = plotFoot(namesTaller);
  const total = height;
  const plotBottom = height - below;
  const rowHeight = (plotBottom - top) / slots;
  const xNameY = plotBottom + xNameUnder;
  const legendY = plotBottom + legendUnder;
  const captionY = plotBottom + captionUnder;
  // Lines and bars' edges land on whole pixels as Notion's chart draws them (seen 2026-11-12): across, in the
  // middle of the pixel under their place, the plot's top edge just over it (19.5, 277.5, 363.5 for 20, 277.25,
  // 363); down, in the middle of the pixel before it (25.5, 91.5 for 26, 92).
  const crispY = (y: number) => y <= top ? top - 0.5 : Math.round(y) + 0.5;
  const crispX = (x: number) => Math.round(x) - 0.5;
  // A number's or a date property's groups along a continuous axis, by their values or their times.
  const axisValue = (category: ChartCategory) => category.numericValue ?? category.timeValue;
  const continuousAxis = (length: number) => holdsNone ? null : data.numericAxis
    ? chartCategoryAxis(categories.flatMap((category) => category.numericValue === undefined ? [] : [category.numericValue]),
      { descending: data.numericAxis.descending, line: kind === "line", length })
    : data.timeAxis ? chartTimeAxis(categories.flatMap((category) => category.timeValue === undefined ? [] : [category.timeValue]),
      { line: kind === "line", length }) : null;
  // A tick along time or a number is named as its groups are.
  const tickName = (tick: number) => data.timeAxis ? chartTimeTickName(tick, data.timeAxis.unit) : formatCategoryTick(tick);
  // A horizontal bar chart's side holds its groups' names, or along time or a number its ticks' (Notion's, seen
  // 2026-11-12): every step-th group's, each 250 at the most.
  const sideAxis = horizontal ? continuousAxis(plotBottom - top) : null;
  const sideStep = horizontal ? chartSideNameStep(rowHeight, groups.length) : 1;
  const sideNames = groups.map((category, index) => horizontal && index % sideStep === 0
    ? chartNameWithin(category.label, CHART_SIDE_NAME_WIDTH, Boolean(data.dateAxis)) : null);
  const sideLabels = sideAxis ? sideAxis.ticks.map(tickName) : sideNames.flatMap((name) => name === null ? [] : [name]);
  // With no group named beside them, where a range holds none of the values, sideways bars start 10 in (Notion's, seen 2026-11-12).
  const left = !horizontal ? flatLeft
    : (holdsNone ? 10 : !sideLabels.length ? 150 : Math.round(18 + Math.max(...sideLabels.map(chartTextWidth)))) + (showY ? 30 : 0);
  const right = width - 10;
  const { ticks, min, max } = horizontal ? valueAxisAlong(right - left) : namesTaller ? valueAxisAlong(plotBottom - top) : flatAxis;
  // A line down the plot's far edge falls just past it, as one at its near edge does (Notion's, seen 2026-11-12: 25.5 and 952.5 for 26 and 952).
  const lineX = (x: number) => x >= right ? Math.round(right) + 0.5 : crispX(x);
  const clamp = (value: number) => Math.min(max, Math.max(min, value));
  const inRange = (value: number) => value >= min && value <= max;
  // Where a value falls along the value axis, past its ends too (bars and lines run on, cut at the plot's edges); `along` keeps it inside.
  const place = (value: number) => horizontal
    ? left + ((value - min) / (max - min)) * (right - left)
    : plotBottom - ((value - min) / (max - min)) * (plotBottom - top);
  const along = (value: number) => place(clamp(value));
  // Under sideways bars each value is named under its tick, a name that would run past the chart's 10 at either
  // end pushed in to end there (Notion's, seen 2026-11-12: "100" 20.69 wide at 941.66, "4" at 948.22 by 952).
  const valueNames = horizontal
    ? chartTickNames(ticks.map((tick) => ({ at: along(tick), text: tickText(tick) })), { length: right - left, start: 10, end: width - 10 }) : null;
  const zero = along(includeZero || min > 0 ? Math.max(min, Math.min(0, max)) : min);
  const categoryAxis = horizontal ? sideAxis : continuousAxis(right - left);
  const categoryAt = (value: number) => horizontal ? top + categoryAxis!.position(value) * (plotBottom - top) : left + categoryAxis!.position(value) * (right - left);
  const slot = categoryAxis ? categoryAxis.interval * (horizontal ? plotBottom - top : right - left)
    : horizontal ? rowHeight : (right - left) / slots;
  // A group's bar, by groups and along time alike (Notion's, seen 2026-11-12: 46 a day → 15, 23 a month → 8).
  const single = chartBarShares(slot, 1);
  const thickness = single.size;
  const center = (index: number) => categoryAxis && axisValue(groups[index]) !== undefined ? categoryAt(axisValue(groups[index])!)
    : edgeToEdge ? left + (right - left) * index / (groups.length - 1)
    : horizontal ? top + (plotBottom - top) * (index + 0.5) / slots : left + slot * index + slot / 2;
  // Under a line from edge to edge the first name starts 9 in and the last ends at the last point (Notion's, seen 2026-11-06).
  const nameAt = (index: number, text: string) => {
    if (!edgeToEdge) return center(index);
    const half = chartLabelWidth(text) / 2;
    return Math.min(right - half, Math.max(9 + half, center(index)));
  };
  // A group's slot along the categories, which its band fills and its hover and click take: a
  // bar's slot, or a line's step from point to point, inside the plot (Notion's, seen 2026-11-11);
  // along time a line's point takes all up to halfway to the next (seen 2026-11-12).
  const slotOf = (index: number) => {
    const size = edgeToEdge && !categoryAxis ? (right - left) / (groups.length - 1) : horizontal && !categoryAxis ? rowHeight : slot;
    const [low, high] = horizontal ? [top, plotBottom] : [left, right];
    const nearest = kind === "line" && data.timeAxis && groups.length > 1;
    const start = nearest ? index > 0 ? (center(index - 1) + center(index)) / 2 : low : Math.max(low, center(index) - size / 2);
    const end = nearest ? index < groups.length - 1 ? (center(index) + center(index + 1)) / 2 : high : Math.min(high, center(index) + size / 2);
    return horizontal ? { x: left, y: start, width: right - left, height: end - start } : { x: start, y: top, width: end - start, height: plotBottom - top };
  };

  // Along time or a number each tick is named under it and kept inside the chart, or beside it.
  const tickNames = categoryAxis ? horizontal
    ? categoryAxis.ticks.map((tick) => ({ tick, x: left - 8, text: tickName(tick) }))
    : chartTickNames(categoryAxis.ticks.map((tick) => ({ at: categoryAt(tick), text: tickName(tick) })), { length: right - left, start: 10, end: width - 10 })
      .map((name, index) => ({ tick: categoryAxis.ticks[index], ...name })) : null;

  // A bar `share` wide around `at`: from its exact start to the pixel, then its rounded-up width on —
  // across a column chart left to right, down a sideways one bottom to top (Notion's, seen 2026-11-12).
  // It stands on zero, or on the range's nearer end where zero is outside it, and runs to its value
  // wherever that is; one at that end has no length (Notion's, seen 2026-11-12).
  function bar(key: string, at: number, share: { exact: number; size: number }, from: number, to: number, fill: string, title: string, shade?: number) {
    const crisp = horizontal ? crispX : crispY;
    const base = from === 0 ? clamp(0) : from;
    const start = crisp(place(base)), end = crisp(place(to));
    const length = Math.abs(end - start);
    const visible = from === to || base === to ? 0 : Math.max(length, 1);
    const shape = horizontal
      ? { x: Math.min(start, end), y: crispY(at + share.exact / 2) - share.size, width: visible, height: share.size }
      : { x: crispX(at - share.exact / 2), y: Math.min(start, end), width: share.size, height: visible };
    return <rect key={key} data-chart-bar={key} aria-label={title} {...shape} rx={2} style={{ fill, fillOpacity: shade }} />;
  }

  // A value outside the range has no label (Notion's, seen 2026-11-12). Its label is a box of a line of Notion's
  // typeface and 5 around it (chartNotionLine), its baseline 17 under its top: over a column the box ends 1 under the
  // bar's top, its top to the pixel (Notion's, seen 2026-11-12 at 15 a line: 169 over 175.5, 246 over 253.25); beside a bar
  // in the middle of the bar as drawn, its baseline 4.5 under that middle (68 by 63.5).
  function valueLabel(key: string, at: number, share: { exact: number; size: number }, value: number, text: string) {
    if (!inRange(value)) return null;
    const point = along(value);
    const line = chartNotionLine();
    return horizontal
      ? <text key={`label:${key}`} className={chartStyles.dataLabel} data-chart-label={key} x={Math.max(point, zero) + 6}
        y={crispY(at + share.exact / 2) - share.size / 2 + 12 - line / 2}>{text}</text>
      : <text key={`label:${key}`} className={chartStyles.dataLabel} data-chart-label={key} x={at} y={Math.round(Math.min(point, zero) - line - 9) + 17}
        textAnchor="middle">{text}</text>;
  }

  // Bars, lines and their areas, cut at the plot's edges; a line's points, drawn inside the range alone, are not cut.
  const marks: ReactNode[] = [];
  const points: ReactNode[] = [];
  const labels: ReactNode[] = [];
  // A line with a single point has no line to draw: its point is drawn, as is
  // a point in its legend (Notion's, seen 2026-11-06).
  const isolated = kind === "line" && groups.length === 1;
  if (kind === "line") {
    const series = stacked
      ? data.stackGroups.filter((group) => !group.hidden).map((group, index) => ({
        key: group.key, fill: group.fill,
        points: groups.map((category, at) => {
          const part = stackedParts(category, style)[index];
          return { x: center(at), y: place(part?.to ?? 0), value: part?.to ?? 0, text: percent ? `${Math.round(part?.to ?? 0)}%` : part?.segment.formatted ?? "" };
        }),
      }))
      : [{ key: "values", fill: singleFill, points: groups.map((category, at) => ({ x: center(at), y: place(category.value), value: category.value, text: category.formatted })) }];
    series.forEach((line, lineIndex) => {
      if (!line.points.length) return;
      const path = linePath(line.points, settings.smoothLine);
      // "그라데이션 영역": the area under the line fades from 0.1 to nothing, as Notion's (seen 2026-11-08).
      if (!settings.hideLineFill && line.points.length) {
        marks.push(<defs key={`gradient:${line.key}`}><linearGradient id={`${gradientId}-${lineIndex}`} x1="0" x2="0" y1="0" y2="1">
          <stop offset="0" style={{ stopColor: line.fill, stopOpacity: 0.1 }} /><stop offset="1" style={{ stopColor: line.fill, stopOpacity: 0 }} />
        </linearGradient></defs>);
        marks.push(<path key={`area:${line.key}`} className={chartStyles.lineArea} fill={`url(#${gradientId}-${lineIndex})`}
          d={`${path} L ${line.points[line.points.length - 1].x} ${zero} L ${line.points[0].x} ${zero} Z`} />);
      }
      marks.push(<path key={`line:${line.key}`} className={chartStyles.line} d={path} style={{ stroke: line.fill }} data-chart-line={line.key} />);
      line.points.forEach((point, index) => {
        if (!inRange(point.value)) return;
        const lit = !isolated && hovered === index;
        points.push(<circle key={`point:${line.key}:${index}`} className={isolated ? chartStyles.isolatedPoint : lit ? chartStyles.hoverPoint : chartStyles.point}
          data-chart-point={groups[index].key} cx={point.x} cy={point.y} r={isolated || lit ? 3 : 3.5} style={{ fill: line.fill }} />);
        // Lines each label their own points; shares are not labelled.
        if (settings.showDataLabels && (!stacked || style === "side_by_side")) {
          labels.push(<text key={`label:${line.key}:${index}`} className={chartStyles.dataLabel} data-chart-label={groups[index].key}
            x={point.x} y={point.y - 8} textAnchor="middle">{point.text}</text>);
        }
      });
    });
  } else {
    groups.forEach((category, index) => {
      const at = center(index);
      if (!stacked) {
        marks.push(bar(category.key, at, single, 0, category.value, category.fill, `${category.label}: ${category.formatted}`, category.shade));
        if (settings.showDataLabels) labels.push(valueLabel(category.key, at, single, category.value, category.formatted));
        return;
      }
      const parts = stackedParts(category, style);
      // Side by side each stacked group takes its share of the group's room, the first leftmost or topmost.
      const share = style === "side_by_side" ? chartBarShares(slot, parts.length) : single;
      parts.forEach((part, partIndex) => {
        const offset = style === "side_by_side" ? share.offset(partIndex) : 0;
        const text = percent ? `${Math.round(part.to - part.from)}%` : part.segment.formatted;
        marks.push(bar(`${category.key}:${part.segment.key}`, at + offset, share, part.from, part.to, part.segment.fill,
          `${category.label} · ${part.segment.label}: ${text}`));
        if (settings.showDataLabels && style === "side_by_side") labels.push(valueLabel(`${category.key}:${part.segment.key}`, at + offset, share, part.to, text));
      });
      if (settings.showDataLabels && style === "normal") labels.push(valueLabel(category.key, at, single, category.value, category.formatted));
    });
  }

  // The hovered group's tip: a single group centered over its bar's top, 15 over it, or over its
  // point, 24 over it; stacked groups beside their top, each part that holds a value under the
  // group's name (Notion's, seen 2026-11-11).
  const tipped = hovered !== null ? groups[hovered] : undefined;
  let tip: ReactNode = null;
  if (tipped && hovered !== null) {
    const reach = stacked ? Math.max(0, ...stackedParts(tipped, style).map((part) => part.to)) : tipped.value;
    const end = horizontal ? Math.max(along(reach), zero) : Math.min(along(reach), zero);
    const anchor: ChartTipAnchor = horizontal ? { x: end, y: center(hovered) - thickness / 2, place: "above", gap: 15 }
      : stacked ? { x: center(hovered), y: along(reach), place: "side", gap: 14 }
        : { x: center(hovered), y: kind === "line" ? along(reach) : end, place: "above", gap: kind === "line" ? 24 : 15 };
    const rows = stacked
      ? tipped.segments.filter((segment) => segment.value !== 0).map((segment) => ({ key: segment.key, label: segment.label, value: segment.formatted, fill: segment.fill }))
      : [{ key: tipped.key, label: tipped.label, value: tipped.formatted, fill: kind === "line" ? singleFill : tipped.fill, shade: tipped.shade }];
    tip = <ChartTip anchor={anchor} title={stacked ? tipped.label : undefined} rows={rows} opens={Boolean(onOpen)} width={width} height={total} />;
  }

  return (
    <div ref={wrapRef} className={chartStyles.canvasWrap} data-chart-picture>
      <svg className={chartStyles.canvas} width={width} height={total} viewBox={`0 0 ${width} ${total}`} role="img" aria-label={ariaLabel}
        data-chart-svg data-chart-category-axis={categoryAxis ? data.timeAxis ? "time" : "number" : "category"} onPointerLeave={() => setHovered(null)}>
        {ticks.map((tick, index) => {
          const point = along(tick);
          const line = tick === 0 ? chartStyles.axisLine : valueLines ? chartStyles.gridline : undefined;
          return <g key={tick}>
            {line && (horizontal
              ? <line className={line} x1={lineX(point)} x2={lineX(point)} y1={top} y2={plotBottom} />
              : <line className={line} x1={left} x2={right} y1={crispY(point)} y2={crispY(point)} />)}
            {/* Up the side a tick's name stands 5 under the pixel its tick falls in (Notion's, seen 2026-11-12: 207 for
                202.5, 102 for 97.75, 336 for 331), the part of a pixel left off as near as reckoning gets. */}
            {horizontal
              ? <text className={chartStyles.tickText} x={valueNames![index].x} y={plotBottom + 20} textAnchor="middle">{valueNames![index].text}</text>
              : <text className={chartStyles.tickText} x={left - 8} y={Math.floor(point + 1e-6) + 5} textAnchor="end">{tickText(tick)}</text>}
          </g>;
        })}
        {/* Along time or a number each tick is lined in the middle of the pixel before it, at the plot's far edge too (Notion's, seen 2026-11-12: 951.5 for 952). */}
        {categoryLines && categoryAxis && categoryAxis.ticks.map((tick) => {
          const at = categoryAt(tick);
          return horizontal ? <line key={`category-grid:${tick}`} className={chartStyles.gridline} x1={left} x2={right} y1={crispY(at)} y2={crispY(at)} />
            : <line key={`category-grid:${tick}`} className={chartStyles.gridline} x1={crispX(at)} x2={crispX(at)} y1={top} y2={plotBottom} />;
        })}
        {/* Each group's room is lined at both its edges, the plot's first and last too: a bar chart's rows across
            (Notion's, seen 2026-11-06 and 2026-11-12), a column or line chart's groups down (seen 2026-11-12). */}
        {categoryLines && !categoryAxis && categories.length > 0 && Array.from({ length: slots + 1 }, (_, index) => {
          const edge = horizontal ? crispY(top + rowHeight * index) : lineX(left + slot * index);
          return horizontal
            ? <line key={`grid:${index}`} className={chartStyles.gridline} x1={left} x2={right} y1={edge} y2={edge} />
            : <line key={`grid:${index}`} className={chartStyles.gridline} x1={edge} x2={edge} y1={top} y2={plotBottom} />;
        })}
        {/* With lines down the plot, "세로" or "둘 다", its value axis is drawn as well: up a column or line chart's
            side, under a bar chart's rows (Notion's, seen 2026-11-12). */}
        {(grid === "vertical" || grid === "both") && !empty && (horizontal
          ? <line className={chartStyles.axisLine} x1={left} x2={right} y1={crispY(plotBottom)} y2={crispY(plotBottom)} />
          : <line className={chartStyles.axisLine} x1={crispX(left)} x2={crispX(left)} y1={top} y2={plotBottom} />)}
        {empty && [horizontal ? top : left, horizontal ? plotBottom : right].map((edge) => horizontal
          ? <line key={`frame:${edge}`} className={chartStyles.gridline} x1={left} x2={right} y1={edge} y2={edge} />
          : <line key={`frame:${edge}`} className={chartStyles.gridline} x1={edge} x2={edge} y1={top} y2={plotBottom} />)}
        {hovered !== null && hovered < groups.length && (data.timeAxis
          ? <line className={chartStyles.crosshair} data-chart-band {...(horizontal ? { x1: left, x2: right, y1: center(hovered), y2: center(hovered) }
            : { x1: center(hovered), x2: center(hovered), y1: top, y2: plotBottom })} />
          : <rect className={chartStyles.band} data-chart-band {...slotOf(hovered)} />)}
        <clipPath id={`${gradientId}-plot`}><rect x={left} y={top} width={right - left} height={plotBottom - top} /></clipPath>
        <g clipPath={`url(#${gradientId}-plot)`}>{marks}</g>
        {points}
        {/* A reference line inside the value axis's range, 1 wide in its color, dashed 4 and 3; its label in the axes'
            text, 10 in from the plot's start and its baseline 3 over the line's pixel, or by sideways bars 4 past the
            line, turned to read down from 10 under the top. One past either end of the axis is not drawn (Notion's, seen
            2026-11-12). */}
        {settings.referenceLines.map((line, index) => {
          if (!(line.value >= min && line.value <= max)) return null;
          const at = Math.round(along(line.value));
          const stroke = { stroke: chartReferenceLineColor(line.color) };
          const dashes = line.dashStyle === "dash" ? "4 3" : undefined;
          const labelX = horizontal ? at + 4 : left + 10, labelY = horizontal ? top + 10 : at - 3;
          return <g key={`reference:${index}`} data-chart-reference-line={line.value}>
            {horizontal
              ? <line x1={at - 0.5} x2={at - 0.5} y1={top} y2={plotBottom} style={stroke} strokeWidth={1} strokeDasharray={dashes} />
              : <line x1={left} x2={right} y1={at + 0.5} y2={at + 0.5} style={stroke} strokeWidth={1} strokeDasharray={dashes} />}
            {line.label && <text className={chartStyles.referenceLabel} x={labelX} y={labelY}
              transform={horizontal ? `rotate(90 ${labelX} ${labelY})` : undefined}>{line.label}</text>}
          </g>;
        })}
        {labels}
        {/* A line's range holding none of the values says nothing: its words are only over bars, as Notion's. */}
        {(empty || (holdsNone && kind !== "line")) && <ChartMessage text={i18next.t(empty ? "chartView:noData" : "chartView:noDataInRange")}
          horizontal={horizontal && !empty} width={width} height={total} />}
        {groups.map((category, index) => (
          <rect key={`hit:${category.key}`} className={chartStyles.hit} data-chart-hit={category.key} data-opens={onOpen ? "" : undefined} {...slotOf(index)}
            onPointerEnter={() => setHovered(index)} onClick={onOpen ? () => onOpen(category) : undefined} />
        ))}
        {tickNames ? tickNames.map(({ tick, x, text }) => (
          <text key={`category-tick:${tick}`} className={chartStyles.axisText} data-chart-category-tick={tick}
            x={x} y={horizontal ? categoryAt(tick) + 4 : plotBottom + 20} textAnchor={horizontal ? "end" : "middle"}>{text}</text>
        )) : horizontal ? groups.map((category, index) => sideNames[index] === null ? null : (
          // Beside its bar, its middle 5 over the baseline, to the pixel (Notion's, seen 2026-11-12: 38, 65, 91 for rows from 20 by 26.4).
          <text key={`name:${category.key}`} className={chartStyles.axisText} x={left - 8} y={Math.round(center(index) + 5)} textAnchor="end">
            {sideNames[index]}
          </text>
        )) : personFaces ? (
          <ChartPersonFaces faces={groups.map((category, index) => ({ key: category.key, x: center(index), empty: Boolean(category.empty) }))}
            top={plotBottom + 8} />
        ) : categoryNames?.names.map(({ index, text, x }) => categoryNames.turned ? (
          <text key={`name:${categories[index].key}`} className={chartStyles.axisText} x={x} y={plotBottom + CHART_TURNED_BASELINE}
            textAnchor="end" transform={`rotate(-45 ${x} ${plotBottom + CHART_TURNED_BASELINE})`}>{text}</text>
        ) : (
          <text key={`name:${categories[index].key}`} className={chartStyles.axisText} x={nameAt(index, text)} y={plotBottom + 20}
            textAnchor="middle">{text}</text>
        ))}
        {showX && <text className={chartStyles.axisName} x={(left + right) / 2} y={xNameY} textAnchor="middle">
          {horizontal ? axisNames.y : axisNames.x}</text>}
        {/* The Y axis's name turned up the side, its baseline 24 in (Notion's, seen 2026-11-12). */}
        {showY && <text className={chartStyles.axisName} x={24} y={(top + plotBottom) / 2} textAnchor="middle"
          transform={`rotate(-90 24 ${(top + plotBottom) / 2})`}>{horizontal ? axisNames.x : axisNames.y}</text>}
        {legendRows.flatMap((row, index) => row.map(({ entry, x }) => (
          <ChartLegendItem key={`legend:${entry.key}`} entry={entry} x={x} y={legendY + index * LEGEND_ROW}
            line={kind === "line" ? { point: isolated, area: !settings.hideLineFill } : undefined} />
        )))}
        {caption && <text className={chartStyles.caption} x={width / 2} y={captionY} textAnchor="middle"
          data-chart-caption>{settings.caption}</text>}
      </svg>
      {tip}
    </div>
  );
}
