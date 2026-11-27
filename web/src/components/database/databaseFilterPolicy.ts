import type { DbProperty, FilterOperator, RelativeDateFilterValue, ViewFilter } from "@/lib/types";
import { i18next } from "@/i18n";
import { formulaPropertyValueType, formulaValueTypeOf } from "./formulaTypes";
import {
  FIXED_RELATIVE_DATE_FILTER_VALUES,
  isNoValueRelativeDateFilterOperator,
  isRelativeDateFilterValue,
} from "@/shared/database/queryRelativeDate.mjs";
import {
  DATE_ROLLUP_FUNCTIONS,
  NUMERIC_ROLLUP_FUNCTIONS,
  filterOperatorLabels,
} from "./databaseFilterLabels";

const NUMBER_SIGNS: Partial<Record<FilterOperator, string>> = {
  equals: "=",
  does_not_equal: "≠",
  greater_than: ">",
  less_than: "<",
  greater_than_or_equal_to: "≥",
  less_than_or_equal_to: "≤",
};

/** Formula comparisons and value inputs use the formula owner's scalar type. */
export function filterPropertyForComparison(prop: DbProperty, properties: readonly DbProperty[] = [], visited = new Set<string>()): DbProperty {
  // A verification filters as a list of its states, as Notion's (seen 2026-11-05).
  if (prop.type === "verification") {
    const options = [
      { id: "verified", name: i18next.t("tableView:verificationFilterVerified"), color: "default" },
      { id: "expired", name: i18next.t("tableView:verificationFilterExpired"), color: "default" },
      { id: "empty", name: i18next.t("tableView:verificationFilterEmpty"), color: "default" },
    ];
    return { ...prop, type: "select", config: { ...prop.config, options, filtersByStateOnly: true } } as DbProperty;
  }
  // A rollup shown as the related values compares as the property rolled up (withRollupFilterTargets).
  const rolled = prop.type === "rollup" ? prop.config?.rollupFilterTarget : undefined;
  if (rolled) return { ...filterPropertyForComparison(rolled), id: prop.id, name: prop.name, databaseId: prop.databaseId };
  // A rollup's date filters as a date, its range by its start or end ("시작일 ⌄"), its earliest or
  // latest day by the one day it is, as Notion's (seen 2026-11-11).
  const rollupFunction = prop.type === "rollup" ? String(prop.config?.rollupFunction ?? "") : "";
  if (DATE_ROLLUP_FUNCTIONS.has(rollupFunction)) {
    return { ...prop, type: "date", config: { ...prop.config, ...(rollupFunction === "date_range" ? {} : { dateFilterOneDay: true }) } } as DbProperty;
  }
  if (prop.type !== "formula" || visited.has(prop.id)) return prop;
  const trail = new Set([...visited, prop.id]);
  const config = (prop.config ?? {}) as Record<string, unknown>;
  const result = formulaValueTypeOf(String(config.formula ?? config.expression ?? ""), name => {
    const target = properties.find(candidate => candidate.name === name);
    return formulaPropertyValueType(target ? filterPropertyForComparison(target, properties, trail).type : undefined);
  });
  // One giving nothing but empty() filters as text, as Notion's (seen 2026-11-20).
  const type = result?.type === "boolean" ? "checkbox" : result?.type === "text" || result?.type === "undefined" ? "rich_text" : result?.type;
  return type === "number" || type === "date" || type === "checkbox" || type === "rich_text" ? { ...prop, type } : prop;
}

/**
 * The properties with each rollup shown as its related values knowing the
 * property rolled up, once that's known: its filter takes that property's
 * conditions and values, and asks which of the values, as Notion's "최소 하나의
 * 옵션 ⌄ 값과 동일한 데이터 ⌄" (seen 2026-11-05).
 */
export function withRollupFilterTargets(properties: readonly DbProperty[], propertiesByDatabase: Readonly<Record<string, readonly DbProperty[] | undefined>>): DbProperty[] {
  return properties.map((prop) => {
    const rollupFunction = prop.config?.rollupFunction ?? "show_original";
    // One counting its values per group asks which of them too, as Notion's (its API reads it as the values, seen 2026-11-11).
    if (prop.type !== "rollup" || !["show_original", "show_unique", "count_per_group", "percent_per_group"].includes(rollupFunction)) return prop;
    const relation = properties.find(({ id, type }) => type === "relation" && id === prop.config?.rollupRelationPropertyId);
    const databaseId = relation?.config?.relationDatabaseId ?? relation?.databaseId;
    const target = databaseId ? propertiesByDatabase[databaseId]?.find(({ id }) => id === prop.config?.rollupTargetPropertyId) : undefined;
    return target && target.type !== "rollup" ? { ...prop, config: { ...prop.config, rollupFilterTarget: target } } : prop;
  });
}

/** Whether a filter on this property asks which of its related values, "최소 하나의 옵션 ⌄". */
export function filterAsksRollupMatch(prop: DbProperty) {
  return prop.type === "rollup" && Boolean(prop.config?.rollupFilterTarget);
}

/** Whether a property's comparisons read as signs, "=" and "≥", as Notion's number filters do (seen 2026-10-01). */
export function filterUsesNumberSigns(prop: DbProperty) {
  prop = filterPropertyForComparison(prop);
  if (prop.type === "number" || prop.type === "unique_id") return true;
  return prop.type === "rollup" && NUMERIC_ROLLUP_FUNCTIONS.has(prop.config?.rollupFunction ?? "show_original");
}

/** A number's comparison as its sign ("≥"), for a property whose comparisons read so. */
export function filterOperatorSign(operator: FilterOperator, prop: DbProperty) {
  return filterUsesNumberSigns(prop) ? NUMBER_SIGNS[operator] : undefined;
}

/** An operator's name for a property: a number's comparison as its sign, else its words. */
export function filterOperatorLabel(operator: FilterOperator, prop: DbProperty) {
  return filterOperatorSign(operator, prop) ?? filterOperatorLabels()[operator] ?? operator;
}

export function filterOperatorsFor(prop: DbProperty): FilterOperator[] {
  prop = filterPropertyForComparison(prop);
  switch (prop.type) {
    // In Notion's order (seen 2026-10-01): =, ≠, >, <, ≥, ≤.
    case "number":
    case "unique_id":
      return [
        "equals", "does_not_equal",
        "greater_than", "less_than",
        "greater_than_or_equal_to", "less_than_or_equal_to",
        "is_empty", "is_not_empty",
      ];
    // In Notion's order (seen 2026-10-01): 값과 동일한 데이터, ~이전·~이후
    // (당일 불포함), ~이전·~이후(당일 포함), 범위 내, 오늘 기준, 비어 있음, 비어
    // 있지 않음. The fixed relative operators ("this_week") show as 오늘 기준.
    case "date":
    case "created_time":
    case "last_edited_time":
      return [
        "equals", "before", "after", "on_or_before", "on_or_after",
        "is_between", "relative_to_today",
        "is_empty", "is_not_empty",
      ];
    case "checkbox":
      return ["equals", "does_not_equal"];
    case "select":
      if ((prop.config as { filtersByStateOnly?: boolean } | undefined)?.filtersByStateOnly) return ["equals", "does_not_equal"];
      return ["equals", "does_not_equal", "is_empty", "is_not_empty"];
    case "status":
      return ["equals", "does_not_equal", "is_empty", "is_not_empty"];
    case "multi_select":
    case "person":
    case "created_by":
    case "last_edited_by":
    case "relation":
      return ["contains", "does_not_contain", "is_empty", "is_not_empty"];
    // A file or a place is there or not, as Notion's (seen 2026-11-03).
    case "files":
    case "place":
      return ["is_empty", "is_not_empty"];
    case "rollup": {
      // A count, sum or share compares as a number does, in its order (Notion's, seen 2026-11-11).
      if (NUMERIC_ROLLUP_FUNCTIONS.has(prop.config?.rollupFunction ?? "show_original")) {
        return [
          "equals", "does_not_equal",
          "greater_than", "less_than",
          "greater_than_or_equal_to", "less_than_or_equal_to",
          "is_empty", "is_not_empty",
        ];
      }
      return [
        "equals", "does_not_equal", "contains", "does_not_contain",
        "starts_with", "ends_with", "is_empty", "is_not_empty",
      ];
    }
    default:
      return [
        "equals", "does_not_equal", "contains", "does_not_contain",
        "starts_with", "ends_with", "is_empty", "is_not_empty",
      ];
  }
}

export function effectiveFilterOperator(
  prop: DbProperty,
  operator: FilterOperator,
): FilterOperator {
  const operators = filterOperatorsFor(prop);
  if (operators.includes(operator)) return operator;
  if (operators.includes("relative_to_today") && isNoValueRelativeDateFilterOperator(operator)) return "relative_to_today";
  return operators[0];
}

/**
 * A date filter's relative value as its editor shows it: its own, the one a
 * fixed operator ("this_week") names, or this week.
 */
export function dateFilterRelativeValue(filter: Pick<ViewFilter, "operator" | "value">): RelativeDateFilterValue {
  if (filter.operator === "relative_to_today" && isRelativeDateFilterValue(filter.value)) {
    return filter.value as RelativeDateFilterValue;
  }
  const fixed = FIXED_RELATIVE_DATE_FILTER_VALUES[filter.operator as keyof typeof FIXED_RELATIVE_DATE_FILTER_VALUES];
  return (fixed ?? FIXED_RELATIVE_DATE_FILTER_VALUES.this_week) as RelativeDateFilterValue;
}

export function defaultOperator(prop: DbProperty, properties: readonly DbProperty[] = []): FilterOperator {
  prop = filterPropertyForComparison(prop, properties);
  // A new date filter shows this week from today ("오늘 기준 이번 주"), and a
  // new created or edited time filter waits for its day, as Notion's do (seen
  // 2026-09-30, 2026-10-01). Notion's API reads that this week as no filter it
  // names, unlike its own "this_week" (seen 2026-11-11), so it is the screen's
  // relative date, not the fixed operator.
  if (prop.type === "date") return "relative_to_today";
  if (prop.type === "created_time" || prop.type === "last_edited_time") return "equals";
  // A select, status or multi-select waits for its options to be ticked even
  // with none to tick, as Notion's on a desktop and a phone (seen 2026-11-11:
  // "값과 동일한 데이터", "결과 없음"), keeping every row meanwhile.
  const operators = filterOperatorsFor(prop);
  // A new file or place filter keeps the rows that have one, as Notion's (seen 2026-11-03).
  if (prop.type === "files" || prop.type === "place") return "is_not_empty";
  return operators.includes("contains") ? "contains" : operators[0];
}

/**
 * A new filter's value: none, so it shows every row until one is chosen, as
 * Notion's new filter does (seen 2026-09-30: a number "=" with an empty
 * field, a select, a checkbox or a text with nothing picked); a new date
 * filter's is this week (defaultOperator).
 */
export function defaultValue(prop: DbProperty, properties: readonly DbProperty[] = []): unknown {
  return defaultOperator(prop, properties) === "relative_to_today" ? { ...FIXED_RELATIVE_DATE_FILTER_VALUES.this_week } : undefined;
}
