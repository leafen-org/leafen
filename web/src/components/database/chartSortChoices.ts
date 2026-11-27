import type { DbProperty } from "@/lib/types";
import { chartDateAxis, chartFormulaValueType } from "./chartGroupingConfiguration";

/**
 * How a chart's sort menus offer and name an axis property's own order, by
 * its kind, as Notion's do on the X axis, a donut's data and the stacked
 * groups alike (seen 2026-11-12 over every kind a chart groups by):
 * - an option's (select, multi-select, status): "수동 · 〈속성〉 오름차순 · 내림차순";
 * - a text's (title, text, URL, email, phone, relation, created or edited by,
 *   a formula's text): "수동 · 〈속성〉 A → Z · Z → A";
 * - a number's, a formula's too: "〈속성〉 낮음 → 높음 · 높음 → 낮음", never by hand;
 * - a date's (created or edited time, a formula's date): "기존 → 신규 · 신규 → 기존" alone;
 * - a rollup's: "〈속성〉 오름차순 · 내림차순", never by hand;
 * - a person's or checkbox's (a formula's true or false): by hand alone.
 * The X axis and a donut add the values' "〈값〉 낮음 → 높음 · 높음 → 낮음",
 * save on dates and a rollup of dates.
 */
export type ChartSortFamily = "option" | "text" | "number" | "date" | "rollup" | "none";

export function chartSortFamily(property: DbProperty | undefined, properties: readonly DbProperty[]): ChartSortFamily {
  if (!property) return "option";
  if (property.type === "rollup") return "rollup";
  if (chartDateAxis(property, properties)) return "date";
  const type = property.type === "formula" ? chartFormulaValueType(property, properties) : property.type;
  if (type === "number") return "number";
  if (type === "select" || type === "multi_select" || type === "status") return "option";
  if (type === "checkbox" || type === "person") return "none";
  return "text";
}

/** The X axis's (or a donut's) sorts, in the menu's order. */
export function chartAxisSorts(family: ChartSortFamily, dated: boolean) {
  return [
    ...family === "option" || family === "text" || family === "none" ? ["manual"] as const : [],
    ...family === "none" ? [] : ["x_ascending", "x_descending"] as const,
    ...dated ? [] : ["y_ascending", "y_descending"] as const,
  ];
}

/** The stacked groups' sorts, in the menu's order. */
export function chartStackSorts(family: ChartSortFamily) {
  return [
    ...family === "option" || family === "text" || family === "none" ? ["manual"] as const : [],
    ...family === "none" ? [] : ["ascending", "descending"] as const,
  ];
}

/**
 * The sort a menu marks and its row names: by hand where the kind has no
 * order of its own, its ascending way where it is never by hand, else the
 * one kept (a date's by value, which only the API sets, too).
 */
export function chartOfferedSort<T extends string>(sort: T, offered: readonly T[], own: { manual: T; ascending: T; descending: T }) {
  if (sort === own.manual && !offered.includes(own.manual) && offered.includes(own.ascending)) return own.ascending;
  if ((sort === own.ascending || sort === own.descending) && !offered.includes(sort) && offered.includes(own.manual)) return own.manual;
  return sort;
}
