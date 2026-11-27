import type { DbProperty, DbView, FilterGroup, ViewFilter } from "@/lib/types";
import { dateRangeFilterValue, resolveFilterValue as coreResolveFilterValue } from "@/shared/database/queryFilterPolicy";
import { relativeSprintIds } from "./databaseSprintContext";
import {
  admitQuerySeedRequest,
  type QueryBounds,
} from "./queryBudget";
import {
  dateKey,
  effectiveFilterGroup,
  isQueryableProperty,
  optionIds,
  type ViewFilterSeedValues,
} from "./queryProjection";

const NO_FILTER_SEED = Symbol("no-filter-seed");
function seedDateKey(value: unknown): string {
  return dateKey(value);
}

function optionSeedValue(prop: DbProperty, value: unknown): string {
  const raw = String(value ?? "").trim();
  if (!raw) return "";
  const lower = raw.toLowerCase();
  return (
    prop.config?.options?.find(
      (option) => option.id.toLowerCase() === lower || option.name.toLowerCase() === lower
    )?.id ?? raw
  );
}

/** A day's key, days later (earlier when negative). */
function shiftedDateKey(key: string, days: number): string {
  const [year, month, day] = key.split("-").map(Number);
  return new Date(Date.UTC(year, month - 1, day + days)).toISOString().slice(0, 10);
}

function filterSeedValue(prop: DbProperty, filter: ViewFilter): unknown | typeof NO_FILTER_SEED {
  // A number not left empty starts at 0, as Notion's (seen 2026-10-30).
  if (filter.operator === "is_not_empty" && prop.type === "number") return 0;
  if (filter.operator === "is_empty" || filter.operator === "is_not_empty") return NO_FILTER_SEED;

  if (prop.type === "select" || prop.type === "status") {
    if (filter.operator !== "equals") return NO_FILTER_SEED;
    const value = optionSeedValue(prop, filter.value);
    return value ? value : NO_FILTER_SEED;
  }

  if (prop.type === "multi_select") {
    if (filter.operator !== "equals" && filter.operator !== "contains") return NO_FILTER_SEED;
    const value = optionSeedValue(prop, filter.value);
    return value ? [value] : NO_FILTER_SEED;
  }

  if (prop.type === "checkbox") {
    if (filter.operator !== "equals") return NO_FILTER_SEED;
    return filter.value === true || filter.value === "true";
  }

  if (prop.type === "number" || prop.type === "unique_id") {
    const value = filter.value === "" || filter.value == null ? NaN : Number(filter.value);
    if (!Number.isFinite(value)) return NO_FILTER_SEED;
    if (filter.operator === "equals") return value;
    if (prop.type !== "number") return NO_FILTER_SEED;
    // A comparison gives the nearest whole number it keeps, as Notion's
    // (seen 2026-10-30: "> 4" gives 5, "< 4" gives 3).
    switch (filter.operator) {
      case "greater_than": return Math.floor(value) + 1;
      case "less_than": return Math.ceil(value) - 1;
      case "greater_than_or_equal_to":
      case "less_than_or_equal_to": return value;
      default: return NO_FILTER_SEED;
    }
  }

  if (prop.type === "date") {
    if (filter.operator === "is_between") {
      // A range's first day, or its last when it has only that.
      const range = dateRangeFilterValue(filter.value);
      const value = seedDateKey(range.start ?? range.end);
      return value ? value : NO_FILTER_SEED;
    }
    if (filter.operator === "after" || filter.operator === "before") {
      // The day after or before, as Notion's (seen 2026-10-30: after
      // 10월 15일 gives 10월 16일).
      const value = seedDateKey(filter.value);
      return /^\d{4}-\d{2}-\d{2}$/u.test(value) ? shiftedDateKey(value, filter.operator === "after" ? 1 : -1) : NO_FILTER_SEED;
    }
    if (
      filter.operator !== "equals" &&
      filter.operator !== "on_or_after" &&
      filter.operator !== "on_or_before"
    ) {
      return NO_FILTER_SEED;
    }
    const value = seedDateKey(filter.value);
    return value ? value : NO_FILTER_SEED;
  }

  if (
    prop.type === "title" ||
    prop.type === "rich_text" ||
    prop.type === "url" ||
    prop.type === "email" ||
    prop.type === "phone"
  ) {
    if (filter.operator !== "equals" && filter.operator !== "contains") return NO_FILTER_SEED;
    const value = String(filter.value ?? "").trim();
    return value ? value : NO_FILTER_SEED;
  }

  if (prop.type === "relation") {
    if (filter.operator !== "contains" && filter.operator !== "equals") return NO_FILTER_SEED;
    const ids = optionIds(filter.value);
    return ids.length ? ids : NO_FILTER_SEED;
  }

  // A view kept to someone, "me" among them, makes its pages theirs, as
  // Notion's does (seen 2026-10-15).
  if (prop.type === "person") {
    if (filter.operator !== "contains" && filter.operator !== "equals") return NO_FILTER_SEED;
    const ids = optionIds(filter.value);
    return ids.length ? ids : NO_FILTER_SEED;
  }

  return NO_FILTER_SEED;
}

function sameSeedValue(a: unknown, b: unknown) {
  return JSON.stringify(a) === JSON.stringify(b);
}

export function viewFilterSeedValues(
  props: DbProperty[],
  view: DbView,
  omitPropertyIds: Iterable<string> = [],
  options: { currentPageId?: string; currentUserId?: string } = {},
  bounds: QueryBounds,
): ViewFilterSeedValues {
  const omitted = new Set(admitQuerySeedRequest(
    props,
    view,
    omitPropertyIds,
    options,
    bounds,
  ));
  const byId = new Map(props.map((prop) => [prop.id, prop]));
  const blocked = new Set<string>();
  const seeds: ViewFilterSeedValues = { properties: {} };

  function put(prop: DbProperty, value: unknown) {
    if (omitted.has(prop.id) || blocked.has(prop.id)) return;
    if (prop.type === "title") {
      if (seeds.title == null) seeds.title = String(value);
      else if (seeds.title !== String(value)) seeds.title = undefined;
      return;
    }
    const current = seeds.properties[prop.id];
    if (current === undefined) {
      seeds.properties[prop.id] = value;
      return;
    }
    if (prop.type === "multi_select" || prop.type === "person") {
      const merged = Array.from(
        new Set([...(Array.isArray(current) ? current : [current]), ...(Array.isArray(value) ? value : [value])])
      ).filter(Boolean);
      seeds.properties[prop.id] = merged;
      return;
    }
    if (sameSeedValue(current, value)) return;
    delete seeds.properties[prop.id];
    blocked.add(prop.id);
  }

  function collectFilter(filter: ViewFilter) {
    const prop = byId.get(filter.propertyId);
    if (!prop || !isQueryableProperty(prop)) return;
    const value = filterSeedValue(prop, {
      ...filter,
      value: coreResolveFilterValue(filter.value, options.currentPageId, options.currentUserId,
        (which) => relativeSprintIds(prop.config?.relationDatabaseId, which)),
    });
    if (value === NO_FILTER_SEED) return;
    put(prop, value);
  }

  function collectGroup(group: FilterGroup) {
    const stack: Array<FilterGroup> = [group];
    while (stack.length > 0) {
      const current = stack.pop()!;
      if (current.conjunction !== "and") continue;
      for (const filter of current.filters) collectFilter(filter);
      const groups = current.groups ?? [];
      for (let index = groups.length - 1; index >= 0; index -= 1) {
        stack.push(groups[index]);
      }
    }
  }

  // The filters the view shows its rows by, the filter bar's among them, as
  // its query reads them (2026-10-30: the bar's were left out, so a row made
  // under them took no value and left the view while its title was typed).
  const group = effectiveFilterGroup(view.config, byId, bounds);
  if (group) collectGroup(group);

  return seeds;
}
