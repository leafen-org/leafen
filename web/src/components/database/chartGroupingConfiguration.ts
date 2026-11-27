import type { DbProperty } from "@/lib/types";
import { inPublicOrder } from "./propertyPublicOrder";
import { formulaValueTypeOf } from "./formulaTypes";
import { formulaPropertyTypes } from "./formulaPropertyTypes";
import { CHART_DATE_TYPES } from "./chartViewSettings";
import { isRollupDateResult } from "./computedDateValue";
import { NUMERIC_ROLLUP_FUNCTIONS } from "./databaseFilterLabels";

/** What a formula gives, a list's items' kind for a list: "number", "date", "text", "checkbox" and the like. */
export function chartFormulaValueType(property: DbProperty, properties: readonly DbProperty[]) {
  const config = (property.config ?? {}) as Record<string, unknown>;
  const result = formulaValueTypeOf(String(config.formula ?? config.expression ?? ""), formulaPropertyTypes(properties));
  const family = result?.type === "array" ? result.items : result?.type;
  return family === "boolean" ? "checkbox" : family;
}

/**
 * Whether a chart's axis property runs along dates, a date property's or a
 * formula's or rollup's that gives dates: one boundary for its units menu,
 * its "기존 → 신규" sort and its groups (Notion's, seen 2026-11-11).
 */
export function chartDateAxis(property: DbProperty | undefined, properties: readonly DbProperty[]) {
  if (!property) return false;
  if (CHART_DATE_TYPES.has(property.type)) return true;
  if (property.type === "formula") return chartFormulaValueType(property, properties) === "date";
  return isRollupDateResult(property);
}

/**
 * Whether a chart's axis property runs along numbers, a number property's or
 * a formula's that gives numbers or a rollup's that counts or calculates:
 * names Notion writes out as text, as its dates' (ChartCartesian).
 */
export function chartNumberAxis(property: DbProperty | undefined, properties: readonly DbProperty[]) {
  if (!property) return false;
  if (property.type === "number") return true;
  if (property.type === "formula") return chartFormulaValueType(property, properties) === "number";
  const rollupFunction = (property.config as { rollupFunction?: unknown } | undefined)?.rollupFunction;
  return property.type === "rollup" && typeof rollupFunction === "string" && NUMERIC_ROLLUP_FUNCTIONS.has(rollupFunction);
}

/**
 * What a chart's settings read its X axis as (chartSettings): "date" along
 * dates; "rollup_date" for a rollup's dates, which group by how near today
 * until a unit is picked, as Notion's chart draws them whatever unit its
 * views API was given (seen 2026-11-12: "지난 7일 · 다음 30일 · 2026년 12월"
 * by month and by week alike); else its type.
 */
export function chartAxisKind(property: DbProperty | undefined, properties: readonly DbProperty[]) {
  if (!chartDateAxis(property, properties)) return property?.type;
  return property?.type === "rollup" ? "rollup_date" : "date";
}

/**
 * The X axis a chart starts with, as Notion's new chart and a table made a
 * chart pick it (seen 2026-11-11 over ten databases; 2026-11-18 in Chrome,
 * nine databases, each start read in the views API): the first select,
 * status or multi-select, whatever comes before it; else the first date,
 * person or number, whichever comes first — a date by day, as its settings
 * name it ("Due (일별)", drawn along days with a label a week); checkboxes and
 * text never. "First" is in the order the API lists the properties, by their
 * IDs, which no screen shows (inPublicOrder, seen 2026-11-12: 선택2 of ID
 * "@_mA" before 선택1 and 선택3; 2026-11-18: 담당 "BnSw" before 날짜 "|DOt",
 * 점수 "AKbc" before 담당 and 날짜); those not told it keep the order given.
 */
export function chartFirstAxis<T extends { type: string }>(properties: readonly T[]): { property: T; dateBy?: "day" } | undefined {
  const ordered = inPublicOrder(properties);
  const option = ordered.find((property) => property.type === "select" || property.type === "status" || property.type === "multi_select");
  if (option) return { property: option };
  const counted = ordered.find((property) => property.type === "date" || property.type === "person" || property.type === "number");
  if (counted?.type === "date") return { property: counted, dateBy: "day" };
  return counted && { property: counted };
}

/** The result family recorded when a formula is picked; rendering reads current computed values. */
function formulaGroup(property: DbProperty, properties: readonly DbProperty[]) {
  const type = chartFormulaValueType(property, properties);
  if (type === "number") return { type, sort: { type: "ascending" }, range_start: -1, range_end: -1, range_size: -1 };
  if (type === "date") return { type, sort: { type: "ascending" }, group_by: "month" };
  if (type === "text") return { type, sort: { type: "ascending" }, group_by: "exact" };
  if (type === "checkbox") return { type, sort: { type: "manual" } };
  return undefined;
}

/**
 * Choosing a new X property resets its order, visibility, natural sort and a
 * status's groups, as Notion's screen does (seen 2026-11-18 in Chrome at 1440,
 * two databases, the views API read after each pick of a select, a person,
 * days by each unit and a status by its options and its groups, in column,
 * bar, line and donut charts): bars and lines sort the chart by hand along a
 * select's or a person's, "manual" even after a sort by value, and the X
 * axis's own way along dates and a status's options, x_ascending; a donut
 * keeps its own sort, its axis's ascending along dates and a status. Along a
 * select or a person the axis says whether it leaves out its empty groups —
 * as the chart's own "0 값 생략" in bars and lines, and always true in a donut
 * (chartAxisHideEmptyGroups); along dates or a status it says nothing until
 * that switch is used (notion_view_chart_screen_fields.go). `chartType` is
 * the chart's (chartViewSettings.ts normalizedChartType), none for an axis
 * started apart from a pick (chartStartedAxisPatch).
 */
export function chartGroupChangePatch(property: DbProperty, properties: readonly DbProperty[], chartType?: string) {
  const computed = property.type === "formula" ? formulaGroup(property, properties) : undefined;
  const manual = ["select", "multi_select", "checkbox", "person", "created_by", "last_edited_by"].includes(property.type)
    || property.type === "formula" && (!computed || computed.type === "checkbox");
  const ascending = CHART_DATE_TYPES.has(property.type) || property.type === "status";
  const donut = chartType === "donut";
  return {
    chartGroupBy: property.id, chartGroupOrder: undefined, chartHiddenGroups: undefined,
    chartGroupSort: donut && ascending ? "ascending" as const : undefined,
    ...(donut ? {} : { chartSort: manual ? undefined : "x_ascending" as const }),
    chartAxisHideEmptyGroups: donut && !ascending ? true : undefined,
    chartFormulaGroup: computed, chartStatusBy: undefined as "group" | undefined,
  };
}

/**
 * The X axis a number chart starts when it becomes a chart of another type,
 * as a new chart's (chartFirstAxis), whatever axis it had before (Notion's,
 * seen 2026-11-18): its property by hand, its order, hidden groups and units
 * reset as a pick of it resets them — the chart's own sort kept
 * (chartTypeChangePatch). `groupable` are the properties an axis can run along.
 */
export function chartStartedAxisPatch(groupable: readonly DbProperty[], properties: readonly DbProperty[]): Record<string, unknown> {
  const first = chartFirstAxis(groupable);
  if (!first) return {};
  const axis: Record<string, unknown> = { ...chartGroupChangePatch(first.property, properties), chartDateBy: first.dateBy };
  delete axis.chartSort;
  delete axis.chartAxisHideEmptyGroups;
  return axis;
}

/**
 * Choosing what a chart stacks by resets its stacked groups' order, visibility, sort and units, as its X axis's, and
 * says how the stacks are drawn where nothing did: group_style "normal" (Notion's, seen 2026-11-18 in two databases).
 */
export function chartStackChangePatch(property: DbProperty | undefined, properties: readonly DbProperty[], groupStyle?: unknown) {
  return {
    ...(property && typeof groupStyle !== "string" ? { chartGroupStyle: "normal" } : {}),
    chartStackBy: property?.id, chartStackOrder: undefined, chartHiddenStackGroups: undefined, chartHideEmptyStackGroups: undefined,
    chartStackSort: undefined,
    chartStackFormulaGroup: property?.type === "formula" ? formulaGroup(property, properties) : undefined,
    chartStackDateBy: undefined as string | undefined, chartStackStatusBy: undefined as "group" | undefined,
  };
}
