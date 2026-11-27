import { differenceInCalendarDays } from "@/lib/calendarDay";
import type { DbProperty, DbView, NotionChartAggregate, Page } from "@/lib/types";
import { isChartGroupableProperty } from "./chartViewBuckets";
import { shownDate, shownEndDate } from "./dateUtils";
import {
  effectiveSummaryValue,
  summaryValuePieces,
  summaryValuePresent,
  type SummaryValueBounds,
} from "./summaryValue";

export type ChartType = "bar" | "horizontal_bar" | "line" | "donut" | "number";
export type ChartHeight = "small" | "medium" | "large" | "extra_large";
export type ChartAggregate = NotionChartAggregate;

type ChartAggregateFamily =
  | "count"
  | "presence"
  | "number"
  | "percent"
  | "checkbox"
  | "percent_checkbox"
  | "date"
  | "date_range";

interface ChartAggregateDefinition {
  readonly id: ChartAggregate;
  readonly family: ChartAggregateFamily;
  readonly propertyTypes: null | "any" | readonly string[];
}

const NUMBER_PROPERTY_TYPES = Object.freeze(["number", "formula", "rollup"]);
const CHECKBOX_PROPERTY_TYPES = Object.freeze(["checkbox"]);
const DATE_PROPERTY_TYPES = Object.freeze(["date", "created_time", "last_edited_time"]);

const NOTION_CHART_AGGREGATE_DEFINITIONS: readonly ChartAggregateDefinition[] = Object.freeze([
  Object.freeze({ id: "count", family: "count", propertyTypes: null }),
  Object.freeze({ id: "count_values", family: "presence", propertyTypes: "any" }),
  Object.freeze({ id: "sum", family: "number", propertyTypes: NUMBER_PROPERTY_TYPES }),
  Object.freeze({ id: "average", family: "number", propertyTypes: NUMBER_PROPERTY_TYPES }),
  Object.freeze({ id: "median", family: "number", propertyTypes: NUMBER_PROPERTY_TYPES }),
  Object.freeze({ id: "min", family: "number", propertyTypes: NUMBER_PROPERTY_TYPES }),
  Object.freeze({ id: "max", family: "number", propertyTypes: NUMBER_PROPERTY_TYPES }),
  Object.freeze({ id: "range", family: "number", propertyTypes: NUMBER_PROPERTY_TYPES }),
  Object.freeze({ id: "unique", family: "presence", propertyTypes: "any" }),
  Object.freeze({ id: "empty", family: "presence", propertyTypes: "any" }),
  Object.freeze({ id: "not_empty", family: "presence", propertyTypes: "any" }),
  Object.freeze({ id: "percent_empty", family: "percent", propertyTypes: "any" }),
  Object.freeze({ id: "percent_not_empty", family: "percent", propertyTypes: "any" }),
  Object.freeze({ id: "checked", family: "checkbox", propertyTypes: CHECKBOX_PROPERTY_TYPES }),
  Object.freeze({ id: "unchecked", family: "checkbox", propertyTypes: CHECKBOX_PROPERTY_TYPES }),
  Object.freeze({ id: "percent_checked", family: "percent_checkbox", propertyTypes: CHECKBOX_PROPERTY_TYPES }),
  Object.freeze({ id: "percent_unchecked", family: "percent_checkbox", propertyTypes: CHECKBOX_PROPERTY_TYPES }),
  Object.freeze({ id: "earliest_date", family: "date", propertyTypes: DATE_PROPERTY_TYPES }),
  Object.freeze({ id: "latest_date", family: "date", propertyTypes: DATE_PROPERTY_TYPES }),
  Object.freeze({ id: "date_range", family: "date_range", propertyTypes: DATE_PROPERTY_TYPES }),
]);

export const CHART_TYPES: readonly ChartType[] = ["bar", "horizontal_bar", "line", "donut", "number"];
export const CHART_HEIGHTS: readonly ChartHeight[] = ["small", "medium", "large", "extra_large"];

/**
 * The pixel height a chart draws at, as Notion's for every chart type (seen
 * 2026-11-08: 작게 300, 중간 400, 크게 600, 아주 크게 800).
 */
export function chartHeightPixels(value: unknown) {
  return value === "small" ? 300 : value === "large" ? 600 : value === "extra_large" ? 800 : 400;
}

/** A view's chart height, one it doesn't name read as "medium", as the chart draws it. */
export function chartHeightOf(value: unknown): ChartHeight {
  return CHART_HEIGHTS.find((height) => height === value) ?? "medium";
}
export const CHART_AGGREGATES: readonly ChartAggregate[] = NOTION_CHART_AGGREGATE_DEFINITIONS.map(
  ({ id }) => id
);
const DEFINITIONS_BY_ID = new Map(
  NOTION_CHART_AGGREGATE_DEFINITIONS.map((definition) => [definition.id, definition])
);

const AGGREGATE_ALIASES: Readonly<Record<string, ChartAggregate>> = Object.freeze({
  count_all: "count",
  show_count: "count",
  count_unique: "unique",
  count_unique_values: "unique",
  count_empty: "empty",
  count_not_empty: "not_empty",
  total: "sum",
  avg: "average",
  mean: "average",
  minimum: "min",
  maximum: "max",
});

function normalizeNotionChartAggregate(value: unknown): ChartAggregate | undefined {
  if (typeof value !== "string") return undefined;
  const token = value.trim().toLowerCase().replace(/[\s-]+/g, "_");
  const canonical = AGGREGATE_ALIASES[token] ?? token;
  return DEFINITIONS_BY_ID.has(canonical as ChartAggregate)
    ? (canonical as ChartAggregate)
    : undefined;
}

function notionChartAggregateDefinition(value: unknown) {
  const canonical = normalizeNotionChartAggregate(value);
  return canonical ? DEFINITIONS_BY_ID.get(canonical) : undefined;
}

export function notionChartAggregateRequiresProperty(value: unknown) {
  const definition = notionChartAggregateDefinition(value);
  return !!definition && definition.propertyTypes !== null;
}

export function notionChartAggregateSupportsProperty(value: unknown, propertyType?: string | null) {
  const definition = notionChartAggregateDefinition(value);
  if (!definition) return false;
  if (definition.propertyTypes === null) return propertyType == null;
  if (typeof propertyType !== "string" || !propertyType) return false;
  return definition.propertyTypes === "any" || definition.propertyTypes.includes(propertyType);
}

export function notionChartAggregateFamily(value: unknown) {
  return notionChartAggregateDefinition(value)?.family;
}

export function normalizedChartType(value: unknown): ChartType | undefined {
  return CHART_TYPES.includes(value as ChartType) ? (value as ChartType) : undefined;
}

export function normalizedChartAggregate(value: unknown): ChartAggregate | undefined {
  return normalizeNotionChartAggregate(value);
}

function aggregateRawValue(
  row: Page,
  prop: DbProperty,
  props: DbProperty[],
  pagesById: Record<string, Page>,
  bounds: SummaryValueBounds
) {
  return effectiveSummaryValue(row, prop, props, pagesById, bounds);
}

function numericAggregateValue(value: unknown): number | null {
  if (typeof value === "number") return Number.isFinite(value) ? value : null;
  if (typeof value !== "string" || !value.trim()) return null;
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : null;
}

// A date counts on the day and at the time it shows on for the viewer, as its
// cell and Notion's calculations place a time on a clock of its own; a range
// footer counts its minutes and hours (seen 2026-10-08).
function aggregateDateValues(value: unknown): Date[] {
  return [shownMoment(shownDate(value)), shownMoment(shownEndDate(value))]
    .filter((date): date is Date => !!date)
    .sort((left, right) => left.getTime() - right.getTime());
}

function shownMoment(shown: { date: Date; time: string } | null): Date | null {
  if (!shown) return null;
  const [hour, minute] = shown.time.split(":").map(Number);
  if (!shown.time || Number.isNaN(hour) || Number.isNaN(minute)) return shown.date;
  const moment = new Date(shown.date);
  moment.setHours(hour, minute, 0, 0);
  return moment;
}

export function aggregateChartValue(
  rows: Page[],
  aggregate: ChartAggregate,
  prop: DbProperty | undefined,
  pagesById: Record<string, Page>,
  props: DbProperty[],
  bounds: SummaryValueBounds
): number | null {
  if (aggregate === "count") return rows.length;
  if (!prop || !notionChartAggregateSupportsProperty(aggregate, prop.type)) return null;

  const values = rows.map((row) => aggregateRawValue(row, prop, props, pagesById, bounds));
  const present = values.filter((value) => summaryValuePresent(value, prop, pagesById, bounds));
  if (aggregate === "count_values") return present.length;
  if (aggregate === "unique") {
    return new Set(values.flatMap((value) => summaryValuePieces(value, prop, pagesById, bounds))).size;
  }
  if (aggregate === "empty") return rows.length - present.length;
  if (aggregate === "not_empty") return present.length;
  if (aggregate === "percent_empty") {
    return rows.length ? ((rows.length - present.length) / rows.length) * 100 : 0;
  }
  if (aggregate === "percent_not_empty") {
    return rows.length ? (present.length / rows.length) * 100 : 0;
  }

  const checked = values.filter((value) => value === true).length;
  if (aggregate === "checked") return checked;
  if (aggregate === "unchecked") return rows.length - checked;
  if (aggregate === "percent_checked") return rows.length ? (checked / rows.length) * 100 : 0;
  if (aggregate === "percent_unchecked") {
    return rows.length ? ((rows.length - checked) / rows.length) * 100 : 0;
  }

  const numbers = values
    .map(numericAggregateValue)
    .filter((value): value is number => value !== null)
    .sort((left, right) => left - right);
  if (aggregate === "sum") return numbers.reduce((sum, value) => sum + value, 0);
  if (aggregate === "average") {
    return numbers.length ? numbers.reduce((sum, value) => sum + value, 0) / numbers.length : null;
  }
  if (aggregate === "median") {
    if (!numbers.length) return null;
    const middle = Math.floor(numbers.length / 2);
    return numbers.length % 2 ? numbers[middle] : (numbers[middle - 1] + numbers[middle]) / 2;
  }
  if (aggregate === "min") return numbers.length ? numbers[0] : null;
  if (aggregate === "max") return numbers.length ? numbers[numbers.length - 1] : null;
  if (aggregate === "range") {
    return numbers.length ? numbers[numbers.length - 1] - numbers[0] : null;
  }

  const dates = values.flatMap(aggregateDateValues).sort((left, right) => left.getTime() - right.getTime());
  if (!dates.length) return null;
  if (aggregate === "earliest_date") return dates[0].getTime();
  if (aggregate === "latest_date") return dates[dates.length - 1].getTime();
  if (aggregate === "date_range") {
    return differenceInCalendarDays(dates[dates.length - 1], dates[0]);
  }
  return null;
}

export interface RecoveredNotionChartConfig {
  chartType?: ChartType;
  groupById?: string;
  aggregate?: ChartAggregate;
  aggregateById?: string;
}

const NOTION_CHART_TYPE_TOKENS: Record<string, ChartType> = {
  bar: "bar", bar_chart: "bar", column: "bar", column_chart: "bar", vertical_bar: "bar",
  horizontal: "horizontal_bar", horizontal_bar: "horizontal_bar",
  horizontal_bar_chart: "horizontal_bar", row_chart: "horizontal_bar",
  line: "line", line_chart: "line", area: "line", area_chart: "line",
  donut: "donut", donut_chart: "donut", doughnut: "donut", pie: "donut",
  pie_chart: "donut", ring: "donut",
};

function asRecord(value: unknown): Record<string, unknown> | undefined {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : undefined;
}

function normalizedToken(value: unknown): string {
  return typeof value === "string" ? value.trim().toLowerCase().replace(/[\s-]+/g, "_") : "";
}

function normalizedScanKey(key: string): string {
  return key.toLowerCase().replace(/[^a-z]/g, "");
}

function notionPropertyRef(value: unknown, depth = 0): string | undefined {
  if (typeof value === "string" && value.trim()) return value.trim();
  const record = asRecord(value);
  if (!record || depth > 2) return undefined;
  for (const key of ["property", "property_id", "propertyId", "id", "name", "property_name", "propertyName"]) {
    const ref = notionPropertyRef(record[key], depth + 1);
    if (ref) return ref;
  }
  return undefined;
}

interface NotionChartScan {
  chartType?: ChartType;
  aggregate?: ChartAggregate;
  groupByRef?: string;
  aggregateByRef?: string;
}

function scanNotionChartRecord(value: unknown, inChart: boolean, out: NotionChartScan, depth = 0) {
  if (depth > 5) return;
  if (Array.isArray(value)) {
    for (const item of value) scanNotionChartRecord(item, inChart, out, depth + 1);
    return;
  }
  const record = asRecord(value);
  if (!record) return;
  for (const [rawKey, entry] of Object.entries(record)) {
    const key = normalizedScanKey(rawKey);
    const chartish = inChart || key.includes("chart");
    if (!out.chartType && (key === "charttype" || (chartish && ["type", "kind", "variant", "layout"].includes(key)))) {
      out.chartType = NOTION_CHART_TYPE_TOKENS[normalizedToken(entry)];
    }
    if (!out.aggregate && key.startsWith("aggregat")) {
      out.aggregate = normalizeNotionChartAggregate(normalizedToken(entry));
    }
    if (!out.groupByRef && ["xaxis", "xaxisproperty", "xproperty", "groupby", "groupbyproperty", "groupproperty"].includes(key)) {
      out.groupByRef = notionPropertyRef(entry);
    }
    if (!out.aggregateByRef && ["yaxis", "yaxisproperty", "yproperty", "valueproperty", "aggregateby"].includes(key)) {
      out.aggregateByRef = notionPropertyRef(entry);
    }
    scanNotionChartRecord(entry, chartish, out, depth + 1);
  }
}

function localPropertyForNotionRef(props: readonly DbProperty[], ref?: string) {
  if (!ref) return undefined;
  let decoded: string | undefined;
  try {
    const value = decodeURIComponent(ref);
    decoded = value !== ref ? value : undefined;
  } catch {
    decoded = undefined;
  }
  return props.find((prop) =>
    prop.id === ref || prop.config?.notionPropertyId === ref || prop.name === ref ||
    (decoded !== undefined && (prop.config?.notionPropertyId === decoded || prop.name === decoded))
  );
}

export function recoveredNotionChartConfig(
  view: DbView,
  props: readonly DbProperty[]
): RecoveredNotionChartConfig {
  const raw = asRecord(view.config?.notion);
  const notionType = view.config?.notionType ?? (typeof raw?.type === "string" ? raw.type : undefined);
  if (view.type !== "chart" && notionType !== "chart") return {};
  const scan: NotionChartScan = {};
  scanNotionChartRecord(view.config, true, scan);
  const groupProp = localPropertyForNotionRef(props, scan.groupByRef);
  const aggregateProp = localPropertyForNotionRef(props, scan.aggregateByRef);
  return {
    chartType: scan.chartType,
    aggregate: scan.aggregate,
    groupById: groupProp && isChartGroupableProperty(groupProp) ? groupProp.id : undefined,
    aggregateById: aggregateProp && notionChartAggregateSupportsProperty(scan.aggregate, aggregateProp.type)
      ? aggregateProp.id
      : undefined,
  };
}
