import type { GraphProperty } from "@/contentGraph";
import type { DbProperty, Page } from "@/lib/types";
import { aggregateChartValue, type ChartAggregate } from "./chartViewModel";
import { formatDateForProperty } from "./dateUtils";
import { formatNumberValue, numberFormatForProperty } from "./numberFormat";
import { percentPresentationProperty } from "./rollupNumberDisplay";
import { rollupDurationText } from "./rollupDateDisplay";
import type { SummaryValueBounds } from "./summaryValue";
import { cappedCount } from "@/lib/countCap";

// A table column's calculations, as Notion's calculate menu offers them
// (seen 2026-09-30): none, then "수 >" (counts), "비율(%) >" (percents) and,
// for numbers "추가 옵션 >", for dates "날짜 >". A checkbox counts and
// percents its checked and unchecked boxes instead of empty values. The
// values are worked out as the chart view works out its aggregates.

export type TableCalculation =
  | "count_all" | "count_values" | "unique" | "empty" | "not_empty"
  | "percent_empty" | "percent_not_empty"
  | "checked" | "unchecked" | "percent_checked" | "percent_unchecked"
  | "sum" | "average" | "median" | "min" | "max" | "range"
  | "earliest_date" | "latest_date" | "date_range";

export type TableCalculationGroup = { key: "count" | "percent" | "more" | "date"; items: readonly TableCalculation[] };

const CALCULATIONS: ReadonlySet<string> = new Set<TableCalculation>([
  "count_all", "count_values", "unique", "empty", "not_empty", "percent_empty", "percent_not_empty",
  "checked", "unchecked", "percent_checked", "percent_unchecked",
  "sum", "average", "median", "min", "max", "range", "earliest_date", "latest_date", "date_range",
]);

// What earlier tables kept: "count" counted a column's values.
const EARLIER_CALCULATIONS: Readonly<Record<string, TableCalculation>> = {
  count: "count_values",
  checked_percent: "percent_checked",
};

const NUMBER_TYPES = new Set(["number", "formula", "rollup"]);
const DATE_TYPES = new Set(["date", "created_time", "last_edited_time"]);

/** A column's calculation as the view keeps it, or none. */
export function tableCalculation(value: unknown): TableCalculation | undefined {
  if (typeof value !== "string") return undefined;
  if (CALCULATIONS.has(value)) return value as TableCalculation;
  return EARLIER_CALCULATIONS[value];
}

/** The groups of the calculate menu for a property type, in Notion's order. */
export function tableCalculationGroups(type: string): TableCalculationGroup[] {
  if (type === "checkbox") {
    return [
      { key: "count", items: ["count_all", "checked", "unchecked"] },
      { key: "percent", items: ["percent_checked", "percent_unchecked"] },
    ];
  }
  const groups: TableCalculationGroup[] = [
    { key: "count", items: ["count_all", "count_values", "unique", "empty", "not_empty"] },
    { key: "percent", items: ["percent_empty", "percent_not_empty"] },
  ];
  if (NUMBER_TYPES.has(type)) groups.push({ key: "more", items: ["sum", "average", "median", "min", "max", "range"] });
  if (DATE_TYPES.has(type)) groups.push({ key: "date", items: ["earliest_date", "latest_date", "date_range"] });
  return groups;
}

/** Whether a calculation is one the property's type offers. */
function tableCalculationFits(calculation: TableCalculation, type: string) {
  return tableCalculationGroups(type).some((group) => group.items.includes(calculation));
}

const COUNTS = new Set<TableCalculation>(["count_all", "count_values", "unique", "empty", "not_empty", "checked", "unchecked"]);

export type TableCalculationEnvironment = {
  properties: readonly DbProperty[];
  propertiesByDatabaseId?: Readonly<Record<string, readonly DbProperty[]>>;
  pagesById: Record<string, Page>;
  bounds: SummaryValueBounds;
  /** Counts over 99 show as "99+", as Notion's large database switch shows them. */
  limitLargeCounts?: boolean;
};

/**
 * A column's calculation as the table's foot shows it: its short name and
 * its value, as Notion's "합계 8", "평균 4.00", "비어 있음 33.33%",
 * "체크 표시됨 33.333%", "범위 20분"·"2일"·"1.5개월"·"1.7년" (the span from the
 * earliest date to the latest, ends included, read as a rollup's range is —
 * seen 2026-10-08); none for a calculation its type lacks.
 */
export function tableCalculationResult(
  calculation: TableCalculation,
  property: GraphProperty,
  rows: readonly Page[],
  environment: TableCalculationEnvironment,
): { label: TableCalculation; value: string } | null {
  if (!tableCalculationFits(calculation, property.type)) return null;
  const presentation = environment.properties.find((candidate) => candidate.id === property.id);
  if (!presentation) return null;
  const aggregate: ChartAggregate = calculation === "count_all" ? "count" : calculation;
  const value = aggregateChartValue([...rows], aggregate, presentation, environment.pagesById,
    [...environment.properties], environment.bounds);
  if (value == null) return { label: calculation, value: "" };
  if (COUNTS.has(calculation)) {
    return { label: calculation, value: environment.limitLargeCounts ? cappedCount(value) : String(value) };
  }
  if (calculation === "percent_empty" || calculation === "percent_not_empty") {
    return { label: calculation, value: `${trimmed(value, 2)}%` };
  }
  if (calculation === "percent_checked" || calculation === "percent_unchecked") {
    return { label: calculation, value: `${trimmed(value, 3)}%` };
  }
  if (calculation === "earliest_date" || calculation === "latest_date") {
    return { label: calculation, value: formatDateForProperty(localDateKey(new Date(value)), presentation) };
  }
  if (calculation === "date_range") {
    const span = (aggregate: ChartAggregate) => aggregateChartValue([...rows], aggregate, presentation, environment.pagesById,
      [...environment.properties], environment.bounds) ?? 0;
    return { label: calculation, value: rollupDurationText(span("latest_date") - span("earliest_date")) };
  }
  const format = numberFormatForProperty({ config: { ...percentPresentationProperty(presentation,
    environment.properties, environment.propertiesByDatabaseId).config } });
  if (calculation === "average" && format === "number") return { label: calculation, value: value.toFixed(2) };
  return { label: calculation, value: formatNumberValue(value, format) };
}

function trimmed(value: number, digits: number) {
  return String(Number(value.toFixed(digits)));
}

function localDateKey(date: Date) {
  const pad = (part: number) => String(part).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
}
