import type { DbProperty } from "@/lib/types";
import { formatNumberForProperty, numberFormatForProperty } from "./numberFormat";
import { consumeRollupString, createRollupBudget, rollupShownFunction, rollupStatusGroupOf, type RollupBounds } from "./rollupPolicy";

const ARITHMETIC = new Set(["sum", "average", "median", "min", "max", "range"]);
const ORIGINALS = new Set(["show_original", "show_unique"]);

/**
 * A rollup's numbers show in the format of the property it rolls up, and an
 * average always with two decimal places, as Notion's (seen 2026-11-04: ₩30,
 * ₩15.00, US$3,000.50, 37.50%, and 3.00 of a plain 3). Its originals come
 * formatted already, each at its property's owner.
 */
export function rollupNumberText(
  fn: unknown, text: string, target: DbProperty | undefined, bounds: RollupBounds,
): string {
  if (!target || !["number", "formula", "rollup"].includes(target.type)
    || (numberFormatForProperty({ config: { ...target.config } }) === "number" && fn !== "average")) return text;
  const budget = createRollupBudget(bounds);
  const config = { ...target.config, ...(fn === "average" ? { decimalPlaces: 2 } : {}) };
  const format = (value: number) => formatNumberForProperty(value, { config });
  let shown = text;
  if (typeof fn === "string" && ARITHMETIC.has(fn)) {
    const value = text.trim() ? Number(text) : Number.NaN;
    if (!Number.isFinite(value)) return text;
    shown = format(value);
  }
  consumeRollupString(shown, bounds, budget);
  return shown;
}

function presentationProperty(
  property: DbProperty, properties: readonly DbProperty[],
  byDatabase: Readonly<Record<string, readonly DbProperty[]>>, visited: Set<string>, shows: (format: string) => boolean,
): DbProperty {
  if (property.type !== "rollup" || visited.has(property.id)
    || !(ARITHMETIC.has(property.config?.rollupFunction ?? "") || ORIGINALS.has(rollupShownFunction(property.config) ?? ""))) return property;
  const relation = properties.find(candidate => candidate.id === property.config?.rollupRelationPropertyId);
  const targetId = relation?.config?.relationDatabaseId;
  const targetProperties = targetId === property.databaseId ? properties : byDatabase[targetId ?? ""] ?? [];
  const target = targetProperties.find(candidate => candidate.id === property.config?.rollupTargetPropertyId);
  if (!target) return property;
  const resolved = presentationProperty(target, targetProperties, byDatabase, new Set([...visited, property.id]), shows);
  return shows(numberFormatForProperty({ config: { ...resolved.config } })) ? resolved : property;
}

/** Numeric summaries inherit a percent rollup's target without changing their raw value. */
export function percentPresentationProperty(
  property: DbProperty, properties: readonly DbProperty[],
  byDatabase: Readonly<Record<string, readonly DbProperty[]>> = {},
): DbProperty {
  return presentationProperty(property, properties, byDatabase, new Set(), (format) => format === "percent");
}

/** The property whose format a rollup's numbers show in, through rollups of rollups. */
export function rollupFormatProperty(
  property: DbProperty, properties: readonly DbProperty[],
  byDatabase: Readonly<Record<string, readonly DbProperty[]>> = {},
): DbProperty {
  return presentationProperty(property, properties, byDatabase, new Set(), (format) => format !== "number");
}

const COUNTS = new Set(["count", "count_values", "unique", "empty", "not_empty", "checked", "unchecked"]);
export const PERCENT_ROLLUPS: ReadonlySet<string> = new Set(["percent_empty", "percent_not_empty", "percent_checked", "percent_unchecked"]);
const DISPLAY_KEYS = ["numberDisplay", "numberDisplayColor", "numberDivideBy", "numberShowValue"] as const;

/**
 * Which number settings a rollup takes, as Notion's "속성 편집" (seen
 * 2026-10-10): a count or a percentage only how it shows (a number, a bar or
 * a ring); a sum, an average and the like, or a number property's originals,
 * also their format and decimal places. Other rollups show text.
 */
export function rollupNumberSettings(fn: unknown, target: Pick<DbProperty, "type"> | undefined, config?: DbProperty["config"]): "display" | "format" | null {
  if (typeof fn !== "string") return null;
  if (COUNTS.has(fn) || PERCENT_ROLLUPS.has(fn) || rollupStatusGroupOf(config)) return "display";
  if ((ARITHMETIC.has(fn) || ORIGINALS.has(fn)) && target?.type === "number") return "format";
  return null;
}

/** Whether a rollup gives one number — a count, a percentage, a sum and the like — that a table's cell sets at its end. */
export function rollupGivesNumber(fn: unknown, target: Pick<DbProperty, "type"> | undefined, config?: DbProperty["config"]): boolean {
  return rollupNumberSettings(fn, target, config) !== null && !ORIGINALS.has(String(fn));
}

/**
 * A rollup's single number and the settings it shows with, once its own
 * settings change how it shows: a bar or a ring, or for a sum and the like
 * its own format or decimal places. A percentage is its fraction, shown as
 * one; a sum keeps its target's format until it has its own.
 * Undefined while the rollup shows as it did, as text.
 */
export function rollupNumberShown(
  prop: DbProperty, text: string, target: DbProperty | undefined,
): { value: number; config: Record<string, unknown> } | undefined {
  const fn = prop.config?.rollupFunction;
  const kind = rollupNumberSettings(fn, target, prop.config);
  if (!kind) return undefined;
  const own = (prop.config ?? {}) as Record<string, unknown>;
  const statusGroup = rollupStatusGroupOf(prop.config);
  const percent = PERCENT_ROLLUPS.has(String(fn)) || statusGroup !== undefined;
  const shownAs = own.numberDisplay === "bar" || own.numberDisplay === "ring";
  const formatted = kind === "format" && (own.numberFormat !== undefined || own.decimalPlaces !== undefined);
  if (!shownAs && !formatted) return undefined;
  const trimmed = text.trim();
  const value = percent && trimmed.endsWith("%") ? Number(trimmed.slice(0, -1)) / 100
    : trimmed ? Number(trimmed) : Number.NaN;
  if (!Number.isFinite(value)) return undefined;
  const display = Object.fromEntries(DISPLAY_KEYS.filter((key) => own[key] !== undefined).map((key) => [key, own[key]]));
  if (kind === "display") {
    return { value, config: percent ? { ...display, numberFormat: "percent", ...(statusGroup ? { decimalPlaces: 2 } : {}) } : display };
  }
  const targetFormat = target ? numberFormatForProperty({ config: { ...target.config } }) : "number";
  const inherited = own.numberFormat === undefined && targetFormat !== "number" ? { numberFormat: targetFormat } : {};
  return { value, config: { ...inherited, ...own } };
}
