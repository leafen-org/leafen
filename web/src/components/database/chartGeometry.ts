import type { ChartCategory } from "./chartViewSeries";
import type { ChartGroupStyle, ChartSettings } from "./chartViewSettings";
import { chartTickStep } from "./chartCategoryAxis";

export function truncatedLabel(label: string, length = 14) {
  return label.length > length ? `${label.slice(0, length - 1)}…` : label;
}

/**
 * A size Notion gives a chart part at each of its heights (300, 400, 600,
 * 800 for 작게 to 아주 크게), read at any other height along the lines
 * between them: in proportion under the first, on the last line's slope
 * past the last.
 */
export function sizeAtChartHeight(height: number, sizes: readonly (readonly [number, number])[]) {
  const [firstHeight, firstSize] = sizes[0];
  if (height <= firstHeight) return (firstSize * height) / firstHeight;
  const after = sizes.findIndex(([at]) => at >= height);
  const [fromHeight, fromSize] = sizes[after === -1 ? sizes.length - 2 : after - 1];
  const [toHeight, toSize] = sizes[after === -1 ? sizes.length - 1 : after];
  return fromSize + ((toSize - fromSize) * (height - fromHeight)) / (toHeight - fromHeight);
}

function niceStep(rough: number) {
  if (!Number.isFinite(rough) || rough <= 0) return 1;
  const magnitude = 10 ** Math.floor(Math.log10(rough));
  const normalized = rough / magnitude;
  const step = normalized <= 1 ? 1 : normalized <= 2 ? 2 : normalized <= 5 ? 5 : 10;
  return step * magnitude;
}

const clean = (tick: number, step: number) => Math.abs(tick) < step / 1e6 ? 0 : Number(tick.toFixed(6));
const exact = (value: number) => Number(value.toPrecision(12));

/**
 * Notion's own axis when nothing sets its range (seen 2026-11-04 on bars,
 * horizontal bars and lines; each the same): five ticks from zero, the step
 * a quarter of the span with its leading digits rounded up to the next 0.05
 * of their decade (0.1 between 1 and 10) — 5 → 2, 10 → 3, 23 → 6, 48 → 15 —
 * whole unless a quarter of the span is under 0.3 (0.5 → 0.15, 1.1 → 0.3)
 * or the values are counts; an end landing on a value other than zero goes
 * a step past it (4 → 0–8, 1 → 0–1.2, −3 and 5 → −6–6); five ticks are kept,
 * the step growing by one rounding while they are too many, those missing
 * added past the highest value (past the lowest when none is above zero).
 */
function notionTicks(min: number, max: number, counts: boolean): number[] {
  const span = max - min;
  const decimals = !counts && span / 4 < 0.3;
  const digits = Math.floor(Math.log10(span / 4)) + 1;
  const decade = 10 ** digits, scale = digits !== 1 ? 0.05 : 0.1;
  for (let correction = 0; correction < 100; correction += 1) {
    const rounded = exact((Math.ceil(exact(span / 4 / decade / scale)) + correction) * scale * decade);
    const step = decimals ? rounded : Math.ceil(rounded);
    let below = Math.ceil(exact(-min / step)), above = Math.ceil(exact(max / step));
    if (min < 0 && exact(below * step) === exact(-min)) below += 1;
    if (max > 0 && exact(above * step) === exact(max)) above += 1;
    const count = below + above + 1;
    if (count > 5) continue;
    if (max > 0) above += 5 - count; else below += 5 - count;
    return Array.from({ length: below + above + 1 }, (_, index) => clean(exact((index - below) * step), step));
  }
  return [min, max];
}

/**
 * A value axis's ticks with no range set: Notion's own around the values and
 * zero (above), or round steps around the values alone on a chart of dates.
 * `integers`: the values are counts.
 */
export function valueTicks(values: readonly number[], includeZero = true, integers = false): number[] {
  const finite = values.filter(Number.isFinite);
  if (includeZero) {
    const low = Math.min(0, ...finite), high = Math.max(0, ...finite);
    return notionTicks(low, high > low ? high : low + 1, integers);
  }
  let min = finite.length ? Math.min(...finite) : 0;
  let max = finite.length ? Math.max(...finite) : 1;
  if (max <= min) max = min + 1;
  // Counts step by whole numbers, small ones over four steps (Notion's 0–4 for a highest count of 2).
  const step = integers ? Math.max(1, niceStep((max - min) / 4)) : niceStep((max - min) / 4);
  if (integers && step === 1 && max - min < 4) max = min + 4;
  min = Math.floor(min / step) * step;
  max = Math.ceil(max / step) * step;
  if (finite.length && min >= Math.min(...finite)) min -= step;
  const ticks: number[] = [];
  for (let tick = min; tick <= max + step / 2; tick += step) ticks.push(clean(Math.min(tick, max), step));
  if (ticks[ticks.length - 1] !== max) ticks.push(clean(max, step));
  return [...new Set(ticks)];
}

/** The scale a chart with no group to draw keeps, as Notion's (seen 2026-10-11). */
const EMPTY_TICKS = [0, 15, 30, 45, 60];
/** Shares of a whole with no range set, as Notion's (seen 2026-11-12). */
const SHARE_TICKS = [0, 25, 50, 75, 100];
/** The room a tick asks for up or along a value axis (Highcharts' own for it, which Notion's chart keeps). */
const VALUE_TICK_ROOM = 72;

/** How a column, bar or line chart stacks its groups: lines "누적됨" each run at their own values, as "누적되지 않음" (Notion's, seen 2026-11-12). */
export function chartDrawnGroupStyle(kind: "bar" | "horizontal_bar" | "line", style: ChartGroupStyle): ChartGroupStyle {
  return kind === "line" && style === "normal" ? "side_by_side" : style;
}

/**
 * What a chart's values are, for its value axis: `counts` the values count rows (stepping by whole
 * numbers); `wholeSteps` they are a count of rows or of unique values, whose set range never steps by a
 * fraction; `percentFormat` they show as percents, whose set range may; `length` the axis's pixels,
 * which step a set range's ticks (its ends never depend on it).
 */
export type ChartValueAxisBasis = {
  includeZero: boolean;
  counts: boolean;
  wholeSteps?: boolean;
  percentFormat?: boolean;
  length?: number;
};

/**
 * A column, bar or line chart's value axis as it draws it: its ends and its ticks — around its values and
 * zero, never its reference lines (Notion's, seen 2026-11-12: a line at 50 by counts up to 3 keeps 0–4),
 * or its shares' 0–100% by 25, or the empty chart's scale; or across a range it sets, Notion's Highcharts
 * axis without a count of ticks (seen 2026-11-12, and in its chart code): the range's ends where they are set
 * — where one is not, the values' lowest (5% past it below zero) or their highest and 1% — and a tick at each
 * multiple of a step near 72 pixels of the axis inside them, neither end its own tick unless it falls on
 * one: 4–10 on 342 → 4, 5, … 10; 0–15 → 0, 2, … 14; 0–7 by a line's 300 → 0, 2, 4, 6. The step is
 * never a fraction for counts of rows or of unique values; for others it may be when they show as
 * percents, the range is under 2 wide, an end it sets has a fraction or the values all lie within ±1.2
 * (0–1.5 summed → 0, 0.25, … 1.5). `holdsNone`: the range set leaves out every value — the values' highest
 * (and zero) under its least or their lowest (and zero) over its greatest, Notion's "bad Y axis range" (seen
 * 2026-11-12, and in its chart code); an axis whose set end leaves the other none of its own keeps that end
 * alone, in its middle (a least of 5 over counts up to 3 → 5).
 */
export function chartValueAxis(categories: readonly ChartCategory[], stacked: boolean, kind: "bar" | "horizontal_bar" | "line",
  settings: Pick<ChartSettings, "groupStyle" | "yMin" | "yMax">, basis: ChartValueAxisBasis): { min: number; max: number; ticks: number[]; holdsNone: boolean } {
  const ends = (ticks: number[]) => ({ min: ticks[0], max: ticks[ticks.length - 1], ticks, holdsNone: false });
  if (categories.length === 0) return ends(EMPTY_TICKS);
  const style = chartDrawnGroupStyle(kind, settings.groupStyle);
  const percent = stacked && style === "percent";
  const values = chartExtent(categories, stacked, style).filter(Number.isFinite);
  const { yMin, yMax } = settings;
  if (yMin === undefined && yMax === undefined) {
    return ends(percent ? SHARE_TICKS : valueTicks(values, basis.includeZero, basis.counts || (!basis.includeZero && values.every(Number.isInteger))));
  }
  const low = values.length ? (basis.includeZero ? Math.min(0, ...values) : Math.min(...values)) : 0;
  const high = values.length ? (basis.includeZero ? Math.max(0, ...values) : Math.max(...values)) : 1;
  const holdsNone = yMin === undefined ? low > yMax! : yMax === undefined ? high < yMin : high < yMin || low > yMax;
  const min = yMin ?? (low < 0 ? exact(low * 1.05) : low);
  const max = yMax ?? (high > 0 ? exact(high * 1.01) : high);
  if (!(max > min)) {
    const kept = yMin ?? max;
    return { min: kept - 1, max: kept + 1, ticks: [kept], holdsNone };
  }
  const fraction = (end: number | undefined) => end !== undefined && !Number.isInteger(end);
  const decimals = !basis.wholeSteps && Boolean(basis.percentFormat
    || (yMin !== undefined && yMax !== undefined && Math.abs(yMax - yMin) < 2) || fraction(yMin) || fraction(yMax)
    || (Math.abs(high) < 1.2 && Math.abs(low) < 1.2));
  const length = basis.length ?? VALUE_TICK_ROOM;
  const step = chartTickStep(((max - min) * VALUE_TICK_ROOM) / Math.max(length, VALUE_TICK_ROOM), decimals);
  const ticks: number[] = [];
  for (let index = Math.floor(exact(min / step)); index <= Math.ceil(exact(max / step)) && ticks.length < 1000; index += 1) {
    const tick = clean(exact(index * step), step);
    if (tick >= min && tick <= max) ticks.push(tick);
  }
  return { min, max, ticks: ticks.length ? ticks : [exact((min + max) / 2)], holdsNone };
}

/**
 * The values a category draws at for a group style: each stacked part on
 * the one after it, the last group at the bottom and the first on top (as a
 * share of the whole for "percent"; Notion's, seen 2026-11-12), or side by
 * side from zero. The parts keep the groups' order.
 */
export function stackedParts(category: ChartCategory, style: ChartGroupStyle) {
  const total = category.segments.reduce((sum, segment) => sum + Math.abs(segment.value), 0);
  let below = 0;
  return category.segments.map((segment) => {
    const value = style === "percent" ? (total ? (segment.value / total) * 100 : 0) : segment.value;
    return { segment, value };
  }).reverse().map(({ segment, value }) => {
    if (style === "side_by_side") return { segment, from: 0, to: value };
    const part = { segment, from: below, to: below + value };
    below += value;
    return part;
  }).reverse();
}

/**
 * How a group's room holds its bars, as Notion's chart lays them out (seen
 * 2026-11-12): 22% of the room at each side, the rest shared equally by the
 * bars side by side, and each bar 56% of its share, 16 at the most and 2 at
 * the least, rounded up to the pixel — 48.86 a row gives 16, 42.86 gives 14,
 * 34.3 gives 11, 26.4 gives 9; three side by side in 132.29 are 14 each, 24.69
 * apart. `offset` is how far a bar's middle sits from its group's, `exact` the
 * width before the rounding up, which places it.
 */
export function chartBarShares(slot: number, count: number) {
  const group = slot * 0.56;
  const pitch = group / Math.max(1, count);
  const exact = Math.min(16, pitch * 0.56);
  return { exact, size: Math.ceil(Math.max(2, exact)), offset: (index: number) => -group / 2 + pitch * (index + 0.5) };
}

/** The values a value axis spans for a chart's categories and their parts. */
export function chartExtent(categories: readonly ChartCategory[], stacked: boolean, style: ChartGroupStyle) {
  if (!stacked) return categories.map((category) => category.value);
  if (style === "percent") return [0, 100];
  return categories.flatMap((category) => stackedParts(category, style).map((part) => part.to));
}

/** A line through points, straight or as a smooth curve through each. */
export function linePath(points: readonly { x: number; y: number }[], smooth: boolean) {
  if (!points.length) return "";
  if (!smooth || points.length < 3) return points.map((point, index) => `${index ? "L" : "M"} ${point.x} ${point.y}`).join(" ");
  let path = `M ${points[0].x} ${points[0].y}`;
  for (let index = 0; index < points.length - 1; index += 1) {
    const before = points[index - 1] ?? points[index], from = points[index], to = points[index + 1], after = points[index + 2] ?? to;
    const c1 = { x: from.x + (to.x - before.x) / 6, y: from.y + (to.y - before.y) / 6 };
    const c2 = { x: to.x - (after.x - from.x) / 6, y: to.y - (after.y - from.y) / 6 };
    path += ` C ${c1.x} ${c1.y}, ${c2.x} ${c2.y}, ${to.x} ${to.y}`;
  }
  return path;
}
