import type { DbProperty } from "@/lib/types";
import { FormulaDate } from "./formulaSyntax";
import { formulaValueTypeOf } from "./formulaTypes";
import { formulaPropertyTypes } from "./formulaPropertyTypes";

// A rollup's results that are dates: its earliest, its latest, or a range "start → end".
const ROLLUP_DATE_RESULTS = new Set(["earliest_date", "latest_date", "date_range"]);

/**
 * The date a computed value stands for, its "YYYY-MM-DD…" text: a formula's
 * date, a rollup's earliest or latest date, the start of its range, or one
 * of the dates it shows as they are ({ start }); "" for any other value.
 * Notion groups these as dates, a range by its start (seen 2026-11-12).
 */
export function computedDateText(value: unknown, prop: Pick<DbProperty, "type"> & { config?: unknown }): string {
  if (value instanceof FormulaDate) return value.text;
  if (prop.type !== "rollup") return "";
  const start = value && typeof value === "object" ? (value as { start?: unknown }).start : undefined;
  if (typeof start === "string") return start;
  const rollupFunction = (prop.config as { rollupFunction?: unknown } | undefined)?.rollupFunction;
  return typeof value === "string" && typeof rollupFunction === "string" && ROLLUP_DATE_RESULTS.has(rollupFunction)
    ? value.split(" → ")[0].trim() : "";
}

/** Whether a rollup's result is a date (computedDateText), which its groups and axis go by as dates. */
export function isRollupDateResult(prop: { type: string; config?: unknown }) {
  const rollupFunction = (prop.config as { rollupFunction?: unknown } | undefined)?.rollupFunction;
  return prop.type === "rollup" && typeof rollupFunction === "string" && ROLLUP_DATE_RESULTS.has(rollupFunction);
}

type ComputedProperty = { id: string; name: string; type: string; config?: unknown; dataSourceId?: string; databaseId?: string };

/**
 * Whether a view's groups by a computed property go by dates, with a date's
 * units, week start and order to pick: a formula's that gives dates, or a
 * rollup's date result or the dates it shows as they are (Notion's "날짜별",
 * seen 2026-11-12) — known from the properties of the database it rolls up
 * (propsByDb), its own when it relates to itself.
 */
export function groupsByComputedDates(prop: ComputedProperty, properties: readonly ComputedProperty[],
  propsByDb?: Readonly<Record<string, readonly ComputedProperty[]>>) {
  const config = (prop.config ?? {}) as Record<string, unknown>;
  if (prop.type === "rollup") return isRollupDateResult(prop) || rollupShowsDates(config, properties, propsByDb);
  if (prop.type !== "formula") return false;
  const result = formulaValueTypeOf(String(config.formula ?? config.expression ?? ""), formulaPropertyTypes(properties));
  return (result?.type === "array" ? result.items : result?.type) === "date";
}

// The kinds of property a rollup shows as dates when it shows them as they are.
const DATED_TYPES = new Set(["date", "created_time", "last_edited_time"]);

// Whether a rollup shows its related pages' dates as they are, each or once.
function rollupShowsDates(config: Record<string, unknown>, properties: readonly ComputedProperty[],
  propsByDb?: Readonly<Record<string, readonly ComputedProperty[]>>) {
  const shown = typeof config.rollupFunction === "string" ? config.rollupFunction : "show_original";
  if (shown !== "show_original" && shown !== "show_unique") return false;
  const relation = properties.find((property) => property.id === config.rollupRelationPropertyId);
  const target = (relation?.config as { relationDatabaseId?: unknown } | undefined)?.relationDatabaseId;
  const own = relation?.dataSourceId ?? relation?.databaseId;
  const targets = typeof target === "string" ? propsByDb?.[target] ?? (target === own ? properties : undefined) : undefined;
  return DATED_TYPES.has(targets?.find((property) => property.id === config.rollupTargetPropertyId)?.type ?? "");
}
