import type { PersonValueBounds } from "@/lib/peopleDirectory";
import type { DbProperty, Page } from "@/lib/types";
import type { ChartDateRange } from "./chartDateBucketRange";
import { chartBuckets, chartStackGrouping, type ChartBucket, type ChartValueContext } from "./chartViewBuckets";
import { chartAutomaticGroupColor, chartGroupFills } from "./chartViewPalette";
import { CHART_DATE_TYPES, type ChartSeriesSort, type ChartSettings } from "./chartViewSettings";
import { chartDateAxis, chartNumberAxis } from "./chartGroupingConfiguration";

/** A group a chart can show, as its settings list it, with whether it is hidden. */
export type ChartGroup = { key: string; label: string; color: string; empty?: boolean; option?: boolean; hidden: boolean };
/** One stacked group's part of a category. */
export type ChartSegment = { key: string; label: string; value: number; formatted: string; fill: string };
/** One of a chart's bars, points or slices. */
export type ChartCategory = {
  key: string; label: string; color: string; empty?: boolean; numericValue?: number; timeValue?: number; range?: ChartDateRange; rows: Page[];
  value: number; formatted: string; fill: string; shade?: number; segments: ChartSegment[];
};
export type ChartData = {
  categories: ChartCategory[];
  /** Every group the X axis has, in its order, for its settings. */
  groups: ChartGroup[];
  /** The groups the Y axis stacks by, in their order; none unless it does. */
  stackGroups: (ChartGroup & { fill: string })[];
  stacked: boolean;
  /** Native numbers sorted by their own value have a continuous category axis. */
  numericAxis?: { descending: boolean };
  /** Days, weeks, months or years along the X axis, which a line runs across from edge to edge. */
  dateAxis?: boolean;
  /** Numbers along the X axis, a number's or a formula's or rollup's, whose names Notion writes out as its dates'. */
  numberNames?: boolean;
  /** A date property's days, weeks, months or years along a continuous time axis. */
  timeAxis?: { unit: "day" | "week" | "month" | "year" };
  /** People along the X axis, a person's, creator's or editor's property, whose groups are drawn as their faces. */
  personAxis?: boolean;
};

/**
 * How a property's groups go: a select's or multi-select's by their names, a status's by its options, others as
 * they come; along the X axis by hand a text's, a relation's, a formula's and people's go their own ways too (ordered).
 */
type GroupKind = "option" | "status" | "text" | "relation" | "formula" | "person" | "other";

const TEXT_TYPES = new Set(["title", "rich_text", "url", "email", "phone"]);

function groupKind(prop?: DbProperty): GroupKind {
  return prop?.type === "select" || prop?.type === "multi_select" ? "option" : prop?.type === "status" ? "status" : "other";
}

const PERSON_TYPES = new Set(["person", "created_by", "last_edited_by"]);

function axisKind(prop: DbProperty): GroupKind {
  if (TEXT_TYPES.has(prop.type)) return "text";
  if (PERSON_TYPES.has(prop.type)) return "person";
  return prop.type === "relation" || prop.type === "formula" ? prop.type : groupKind(prop);
}

/** The X axes whose values a formula, a relation or a rollup gives. */
const COMPUTED_TYPES = new Set(["formula", "relation", "rollup"]);
// The key a chart's group without a value sorts by among its options' names, between "t" and "v" (Notion's, seen 2026-11-12).
const NO_VALUE_KEY = "uncategorized";
const byCodeUnits = (left: string, right: string) => left < right ? -1 : left > right ? 1 : 0;

/**
 * Orders groups by hand or by what they hold, as Notion's chart (seen
 * 2026-11-12). By hand with no order kept, a select's, multi-select's or
 * status's groups go by their names' code units, the group without a value
 * by "uncategorized" (Done · In progress · Not started; X · Y · Z · 분류 없음;
 * 태그 없음 · 가 · 나); once kept, by that order, then their own, the group
 * without a value first until it has a place. Ascending and descending put
 * a select's or multi-select's group without a value first, the others by
 * name either way round (a · B · o), a status's by its options either way
 * round; other groups the ascending way go as they come, the rows without a
 * value first, and descending turns that around — save for dates and
 * computed values and text, whose "날짜 없음", "수식 없음", "글 없음" stays
 * first (seen 2026-11-06, 2026-11-12); a formula's empty text, named so too,
 * right after it either way (seen 2026-11-12). By hand with no order kept a
 * text's go as an option's, a relation's by their pages' IDs, people by
 * their IDs ("사람 없음" after them: N · C · 오 · h · 테 · 가 · ?) and a
 * formula's by their names' code units, the group without a value last
 * (seen 2026-11-12).
 */
function ordered<T extends { key: string; label: string; empty?: boolean; blank?: boolean }>(items: readonly T[], sort: ChartSeriesSort,
  order: readonly string[], kind: GroupKind, emptyFirst = false) {
  if (sort === "manual" && !order.length && (kind === "relation" || kind === "person" || kind === "formula")) {
    const name = (item: T) => kind === "formula" ? item.label : item.key;
    const valued = items.filter((item) => !item.empty && !item.blank).sort((left, right) => byCodeUnits(name(left), name(right)));
    return [...valued, ...items.filter((item) => item.empty), ...items.filter((item) => item.blank)];
  }
  if (sort === "manual" && !order.length && kind !== "other") {
    const name = (item: T) => item.empty ? NO_VALUE_KEY : item.label;
    return [...items].sort((left, right) => byCodeUnits(name(left), name(right)));
  }
  if (sort === "manual") {
    const at = (item: T) => {
      const index = order.indexOf(item.key);
      return index >= 0 ? index : item.empty ? -1 : item.blank ? -0.5 : Number.MAX_SAFE_INTEGER;
    };
    return items.map((item, index) => ({ item, index }))
      .sort((left, right) => at(left.item) - at(right.item) || left.index - right.index).map(({ item }) => item);
  }
  const empty = [...items.filter((item) => item.empty), ...items.filter((item) => item.blank)];
  const valued = items.filter((item) => !item.empty && !item.blank);
  if (kind === "option") valued.sort((left, right) => left.label.localeCompare(right.label));
  if (kind === "option" || kind === "status") return [...empty, ...sort === "ascending" ? valued : valued.reverse()];
  return sort === "ascending" ? [...empty, ...valued] : emptyFirst ? [...empty, ...valued.reverse()] : [...empty, ...valued].reverse();
}

function group(bucket: ChartBucket, hidden: ReadonlySet<string>): ChartGroup {
  return { key: bucket.key, label: bucket.label, color: bucket.color, empty: bucket.empty, option: bucket.option, hidden: hidden.has(bucket.key) };
}

/**
 * What a chart draws from its rows: each X group's value (the sum of its
 * stacked parts when it stacks) in the chart's order, without the groups
 * hidden or, when asked, those at zero, colored by the theme and, when
 * asked, shaded by value; bars and a line can add each value to the ones
 * before it, "누적" (Notion's, seen 2026-11-06), a donut's slices never.
 */
export function chartData({ rows, groupProp, stackProp, settings, theme, colorByValue, slices = false, accumulate = false, personBounds, valueContext, aggregate, format }: {
  rows: readonly Page[];
  groupProp: DbProperty;
  stackProp?: DbProperty;
  settings: ChartSettings;
  theme: unknown;
  colorByValue: boolean;
  /** A donut's slices, which one color's shades tell apart. */
  slices?: boolean;
  /** Each value adds those before it, "누적" (chartCumulativeApplies). */
  accumulate?: boolean;
  personBounds: PersonValueBounds;
  valueContext?: ChartValueContext;
  aggregate(rows: Page[]): number | null;
  format(value: number): string;
}): ChartData {
  const numericAxis = groupProp.type === "number" && !slices && !settings.sort.startsWith("y_")
    ? { descending: settings.sort === "x_descending" } : undefined;
  // A date property's days, weeks, months or years lie along time, earliest first whichever way it sorts (Notion's, seen 2026-11-12).
  // Sorted by value they go as categories instead (Notion's, seen 2026-11-12 for a sort the views API gives).
  const byValue = settings.sort === "y_ascending" || settings.sort === "y_descending";
  const timeAxis = CHART_DATE_TYPES.has(groupProp.type) && !slices && settings.dateBy !== "relative" && !byValue ? { unit: settings.dateBy } : undefined;
  const buckets = chartBuckets(rows, groupProp, personBounds, valueContext, settings);
  const dated = chartDateAxis(groupProp, valueContext?.properties ?? []);
  // Stacked groups keep "〈속성〉 없음" first either way round where the X axis does (Notion's, seen 2026-11-12: a date
  // formula's stacked months descending, "월식 없음" first).
  const stackBuckets = stackProp ? ordered(chartBuckets(rows, stackProp, personBounds, valueContext, chartStackGrouping(settings)), settings.stackSort, settings.stackOrder,
    groupKind(stackProp), chartDateAxis(stackProp, valueContext?.properties ?? []) || COMPUTED_TYPES.has(stackProp.type) || TEXT_TYPES.has(stackProp.type))
      .filter((bucket) => !(settings.hideEmptyStackGroups && bucket.rows.length === 0)) : [];
  // A stacked group's own color goes by its place too, the group with no value taking one (Notion's, seen 2026-11-12:
  // a rollup's dates stacked "롤업날 없음" gray, then yellow, green …, as along the X axis).
  const shownStack = stackBuckets.filter((bucket) => !settings.hiddenStackGroups.has(bucket.key))
    .map((bucket, index) => bucket.automatic ? { ...bucket, color: chartAutomaticGroupColor(index) } : bucket);
  const stackFills = chartGroupFills(shownStack, theme, true);
  const stackRowIds = shownStack.map((bucket) => new Set(bucket.rows.map((row) => row.id)));

  const valued = buckets.flatMap((bucket) => {
    if (!stackProp) {
      const value = aggregate(bucket.rows);
      return value === null ? [] : [{ key: bucket.key, label: bucket.label, empty: bucket.empty, blank: bucket.blank, bucket, value, segments: [] as ChartSegment[] }];
    }
    const segments = shownStack.map((stack, index) => {
      const value = aggregate(bucket.rows.filter((row) => stackRowIds[index].has(row.id))) ?? 0;
      return { key: stack.key, label: stack.label, value, formatted: format(value), fill: stackFills[index] };
    });
    return [{ key: bucket.key, label: bucket.label, empty: bucket.empty, blank: bucket.blank, bucket, value: segments.reduce((sum, segment) => sum + segment.value, 0), segments }];
  });
  const sort = settings.sort;
  const emptyFirst = dated || COMPUTED_TYPES.has(groupProp.type) || TEXT_TYPES.has(groupProp.type);
  // Sorted by value, groups of one value keep the order their X axis goes in by its own sort: by hand, a select's
  // and a text's by their names with "〈속성〉 없음" where its name falls, either way round "〈속성〉 없음" first;
  // days by when, earlier first unless it runs the other way; numbers up from "〈속성〉 없음" whichever way it runs
  // (Notion's, seen 2026-11-12: 3 · 점수 없음 · 5 · 7, A · 종류 없음 · C · B by the axis descending).
  // The groups its settings list go that way too (Notion's, seen 2026-11-12: 종류 없음 · C · B · A).
  const axisSort = groupProp.type === "number" ? "ascending" : dated && settings.axisSort === "manual" ? "ascending" : settings.axisSort;
  const axisOrder = ordered(valued, byValue ? axisSort
    : sort === "x_ascending" || timeAxis || numericAxis && !numericAxis.descending ? "ascending" : sort === "x_descending" ? "descending" : "manual",
  settings.groupOrder, axisKind(groupProp), emptyFirst);
  const inOrder = byValue ? [...axisOrder].sort((left, right) => sort === "y_ascending" ? left.value - right.value : right.value - left.value) : axisOrder;
  const groupKeys = new Set(axisOrder.map((entry) => entry.bucket.key));
  // Groups without a value to show (an average of no numbers) still list.
  const groups = [...axisOrder.map((entry) => entry.bucket), ...buckets.filter((bucket) => !groupKeys.has(bucket.key))]
    .map((bucket) => group(bucket, settings.hiddenGroups));

  const shown = inOrder.filter((entry) => !(numericAxis && entry.bucket.empty) && !settings.hiddenGroups.has(entry.bucket.key) && !((settings.hideEmptyGroups || settings.axisHidesEmptyGroups) && entry.value === 0));
  let running = 0;
  const runningSegments = new Map<string, number>();
  const cumulative = shown.map((entry) => {
    if (!accumulate || slices) return entry;
    running += entry.value;
    const segments = entry.segments.map((segment) => {
      const value = (runningSegments.get(segment.key) ?? 0) + segment.value;
      runningSegments.set(segment.key, value);
      return { ...segment, value, formatted: format(value) };
    });
    return { ...entry, value: running, segments };
  });
  // A color of the chart's own goes by where its group is drawn, the group
  // with no value taking its place too: by relative date 날짜 없음 · 지난 30일 ·
  // 지난 7일 are gray · yellow · green either way round (Notion's, seen 2026-11-06).
  const drawn = cumulative.map(({ bucket }, index) => bucket.automatic ? { ...bucket, color: chartAutomaticGroupColor(index) } : bucket);
  const fills = chartGroupFills(drawn, theme, slices);
  const values = cumulative.map((entry) => entry.value);
  const low = Math.min(...values), high = Math.max(...values);
  const categories = cumulative.map(({ bucket, value, segments }, index) => ({
    key: bucket.key, label: bucket.label, color: drawn[index].color, empty: bucket.empty, numericValue: bucket.numericValue, timeValue: bucket.timeValue, range: bucket.range, rows: bucket.rows,
    value, formatted: format(value), fill: fills[index], segments,
    ...(colorByValue && high > low ? { shade: 0.35 + 0.65 * ((value - low) / (high - low)) } : {}),
  }));
  return {
    categories,
    groups,
    stackGroups: stackBuckets.map((bucket) => {
      const index = shownStack.findIndex((shown) => shown.key === bucket.key);
      return { ...group(bucket, settings.hiddenStackGroups), fill: index < 0 ? chartGroupFills([bucket], theme, true)[0] : stackFills[index] };
    }),
    stacked: Boolean(stackProp),
    numericAxis,
    dateAxis: dated && settings.dateBy !== "relative",
    numberNames: chartNumberAxis(groupProp, valueContext?.properties ?? []),
    timeAxis,
    personAxis: PERSON_TYPES.has(groupProp.type),
  };
}
