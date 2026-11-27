import type { DbProperty, DbView, Page } from "@/lib/types";
import type { PersonValueBounds } from "@/lib/peopleDirectory";
import type { FormulaBounds } from "./formula";

// The options a property holds, every one read: its config is at most a
// change's bytes (1 MiB) and an option at least 32 of them. Notion's grow past
// 100 (to 130 as rows named new ones, seen 2026-10-13).
export const PROPERTY_OPTION_CAPACITY = 32_768;
const PRESERVED_MAX_OPTIONS = PROPERTY_OPTION_CAPACITY;

export type QueryBounds = {
  maxRows: number;
  maxProperties: number;
  maxPages: number;
  maxSorts: number;
  maxFilters: number;
  maxFilterGroups: number;
  maxFilterDepth: number;
  maxOptionsPerProperty: number;
  maxStatusGroups: number;
  maxRelationIds: number;
  maxValueNodes: number;
  maxValueDepth: number;
  maxContainerEntries: number;
  maxScalarCharacters: number;
  maxTotalCharacters: number;
  personValue: PersonValueBounds;
  formula: FormulaBounds;
};

type QueryBudget = {
  nodes: number;
  entries: number;
  characters: number;
  seen: WeakSet<object>;
};

function assertBounds(bounds: QueryBounds) {
  const values = [
    bounds.maxRows,
    bounds.maxProperties,
    bounds.maxPages,
    bounds.maxSorts,
    bounds.maxFilters,
    bounds.maxFilterGroups,
    bounds.maxFilterDepth,
    bounds.maxOptionsPerProperty,
    bounds.maxStatusGroups,
    bounds.maxRelationIds,
    bounds.maxValueNodes,
    bounds.maxValueDepth,
    bounds.maxContainerEntries,
    bounds.maxScalarCharacters,
    bounds.maxTotalCharacters,
  ];
  if (values.some((value) => !Number.isSafeInteger(value) || value < 0)) {
    throw new RangeError("Database query bounds must be non-negative safe integers.");
  }
  if (!bounds.personValue || !bounds.formula) {
    throw new RangeError("Database query nested policy bounds are required.");
  }
  if (!bounds.formula.rollup) {
    throw new RangeError("Database query nested rollup bounds are required.");
  }
  const nestedValues = [
    bounds.personValue.maxNodes,
    bounds.personValue.maxDepth,
    bounds.personValue.maxEntries,
    bounds.formula.maxExpressionCharacters,
    bounds.formula.maxProperties,
    bounds.formula.maxOptions,
    bounds.formula.maxPages,
    bounds.formula.maxEvaluations,
    bounds.formula.maxValueNodes,
    bounds.formula.maxValueDepth,
    bounds.formula.maxStringCharacters,
    bounds.formula.rollup.maxDatabases,
    bounds.formula.rollup.maxProperties,
    bounds.formula.rollup.maxPages,
    bounds.formula.rollup.maxValueNodes,
    bounds.formula.rollup.maxValueDepth,
    bounds.formula.rollup.maxValuePieces,
    bounds.formula.rollup.maxOptions,
    bounds.formula.rollup.maxFileAttachments,
    bounds.formula.rollup.maxPersonNodes,
    bounds.formula.rollup.maxPersonDepth,
    bounds.formula.rollup.maxPeopleEntries,
    bounds.formula.rollup.maxStringCharacters,
  ];
  if (nestedValues.some((value) => !Number.isSafeInteger(value) || value < 0)) {
    throw new RangeError("Database query nested bounds must be non-negative safe integers.");
  }
  if (
    bounds.maxOptionsPerProperty < PRESERVED_MAX_OPTIONS
  ) {
    throw new RangeError("Database query bounds cannot narrow preserved property capacities.");
  }
  if (
    bounds.maxValueNodes < 1
    || bounds.maxContainerEntries < 1
    || bounds.maxScalarCharacters < 1
    || bounds.maxTotalCharacters < 1
  ) {
    throw new RangeError("Database query structural bounds must admit one valid value.");
  }
}

function createBudget(bounds: QueryBounds): QueryBudget {
  assertBounds(bounds);
  return { nodes: 0, entries: 0, characters: 0, seen: new WeakSet() };
}

function reserveString(value: unknown, bounds: QueryBounds, budget: QueryBudget) {
  if (typeof value !== "string") return;
  if (value.length > bounds.maxScalarCharacters) {
    throw new RangeError("Database query scalar exceeds its caller-owned bound.");
  }
  budget.characters += value.length;
  if (budget.characters > bounds.maxTotalCharacters) {
    throw new RangeError("Database query text exceeds its caller-owned aggregate bound.");
  }
}

function admitValue(value: unknown, bounds: QueryBounds, budget: QueryBudget) {
  const stack: Array<{ value: unknown; depth: number }> = [{ value, depth: 0 }];
  while (stack.length > 0) {
    const current = stack.pop()!;
    if (current.depth > bounds.maxValueDepth) {
      throw new RangeError("Database query value exceeds its caller-owned depth bound.");
    }
    budget.nodes += 1;
    if (budget.nodes > bounds.maxValueNodes) {
      throw new RangeError("Database query value exceeds its caller-owned node bound.");
    }
    reserveString(current.value, bounds, budget);
    if (!current.value || typeof current.value !== "object") continue;
    if (budget.seen.has(current.value)) continue;
    budget.seen.add(current.value);

    if (Array.isArray(current.value)) {
      budget.entries += current.value.length;
      if (
        current.value.length > bounds.maxContainerEntries
        || budget.entries > bounds.maxContainerEntries
      ) {
        throw new RangeError("Database query arrays exceed their caller-owned entry bound.");
      }
      for (let index = current.value.length - 1; index >= 0; index -= 1) {
        stack.push({ value: current.value[index], depth: current.depth + 1 });
      }
      continue;
    }

    const record = current.value as Record<string, unknown>;
    let ownEntries = 0;
    for (const key in record) {
      if (!Object.prototype.hasOwnProperty.call(record, key)) continue;
      ownEntries += 1;
      budget.entries += 1;
      if (
        ownEntries > bounds.maxContainerEntries
        || budget.entries > bounds.maxContainerEntries
      ) {
        throw new RangeError("Database query objects exceed their caller-owned entry bound.");
      }
      reserveString(key, bounds, budget);
      stack.push({ value: record[key], depth: current.depth + 1 });
    }
  }
}

function filterShape(value: unknown) {
  return value && typeof value === "object" && !Array.isArray(value)
    ? value as { filters?: unknown; groups?: unknown; conjunction?: unknown }
    : undefined;
}

/** How many filter rules a group holds, its nested groups' included. */
function groupFilterCount(value: unknown, depth = 0): number {
  const group = filterShape(value);
  if (!group || depth > 32) return 0;
  return (Array.isArray(group.filters) ? group.filters.length : 0)
    + (Array.isArray(group.groups) ? group.groups.reduce((sum: number, child) => sum + groupFilterCount(child, depth + 1), 0) : 0);
}

/** How many groups a group is, its nested ones included, and how deep they go. */
function groupShape(value: unknown, depth = 0): { groups: number; depth: number } {
  const group = filterShape(value);
  if (!group || depth > 32) return { groups: 0, depth: 0 };
  let groups = 1, deepest = depth;
  for (const child of Array.isArray(group.groups) ? group.groups : []) {
    const shape = groupShape(child, depth + 1);
    groups += shape.groups;
    deepest = Math.max(deepest, shape.depth);
  }
  return { groups, depth: deepest };
}

type ConfigRecord = { filterGroup?: unknown; filters?: unknown; quickFilters?: unknown; sorts?: unknown };

function configRecord(config: unknown): ConfigRecord | undefined {
  return config && typeof config === "object" && !Array.isArray(config) ? config as ConfigRecord : undefined;
}

/**
 * What a view's config asks of a query, as its admission counts it: filter
 * rules (its own, its quick filters' and its advanced group's), filter groups
 * and their depth, and sorts.
 */
export function queryConfigSize(config: unknown) {
  const record = configRecord(config);
  let filters = Array.isArray(record?.filters) ? record.filters.length : 0;
  let groups = 0, depth = 0;
  const terms = [...(record?.filterGroup ? [record.filterGroup] : []),
    ...(Array.isArray(record?.quickFilters) ? record.quickFilters : [])];
  for (const term of terms) {
    if (term !== record?.filterGroup && filterShape(term)?.conjunction === undefined) { filters += 1; continue; }
    filters += groupFilterCount(term);
    const shape = groupShape(term);
    groups += shape.groups;
    depth = Math.max(depth, shape.depth);
  }
  return { filters, groups, depth, sorts: Array.isArray(record?.sorts) ? record.sorts.length : 0 };
}

/** Whether a view's config asks more of a query than its bounds take. */
export function queryConfigOverBounds(config: unknown, bounds: Pick<QueryBounds, "maxFilters" | "maxFilterGroups" | "maxFilterDepth" | "maxSorts">) {
  const size = queryConfigSize(config);
  return {
    filters: size.filters > bounds.maxFilters,
    groups: size.groups > bounds.maxFilterGroups || size.depth > bounds.maxFilterDepth,
    sorts: size.sorts > bounds.maxSorts,
  };
}

/** A group held to a depth and a number of groups, its last ones left out first. */
function cappedGroup(value: unknown, depth: number, maxDepth: number, budget: { groups: number }): unknown {
  const group = filterShape(value);
  if (!group) return value;
  budget.groups -= 1;
  const kept: unknown[] = [];
  for (const child of Array.isArray(group.groups) ? group.groups : []) {
    if (depth + 1 > maxDepth || budget.groups <= 0) break;
    kept.push(cappedGroup(child, depth + 1, maxDepth, budget));
  }
  return Array.isArray(group.groups) ? { ...(value as object), groups: kept } : value;
}

/** A group with its last `drop` rules left out, nested groups' last first. */
function prunedGroup(value: unknown, drop: { left: number }): unknown {
  const group = filterShape(value);
  if (!group || drop.left <= 0) return value;
  const groups = Array.isArray(group.groups) ? [...group.groups] : undefined;
  for (let index = (groups?.length ?? 0) - 1; groups && index >= 0 && drop.left > 0; index -= 1) {
    groups[index] = prunedGroup(groups[index], drop);
  }
  const filters = Array.isArray(group.filters) ? [...group.filters] : undefined;
  while (filters && filters.length > 0 && drop.left > 0) {
    filters.pop();
    drop.left -= 1;
  }
  return { ...(value as object), ...(filters ? { filters } : {}), ...(groups ? { groups } : {}) };
}

/**
 * A view's config held to a query's bounds: a view saved with more than a
 * query takes shows with its first sorts, groups and rules — the quick
 * filters' last left out first, then its own rules, then its advanced
 * group's last — rather than failing the page; its filter bar says so
 * (`queryConfigOverBounds`). Changes past the bounds are refused where views
 * are saved (`refuseViewQueryGrowth`).
 */
export function boundedQueryConfig<Config>(config: Config, bounds: Pick<QueryBounds, "maxFilters" | "maxFilterGroups" | "maxFilterDepth" | "maxSorts">): Config {
  const over = queryConfigOverBounds(config, bounds);
  if ((!over.filters && !over.groups && !over.sorts) || !config || typeof config !== "object") return config;
  const record = { ...(config as Record<string, unknown>) };
  if (over.sorts && Array.isArray(record.sorts)) record.sorts = record.sorts.slice(0, bounds.maxSorts);
  const budget = { groups: bounds.maxFilterGroups };
  if (record.filterGroup) record.filterGroup = cappedGroup(record.filterGroup, 0, bounds.maxFilterDepth, budget);
  if (Array.isArray(record.quickFilters)) {
    record.quickFilters = record.quickFilters.flatMap((term) => {
      if (filterShape(term)?.conjunction === undefined) return [term];
      return budget.groups > 0 ? [cappedGroup(term, 0, bounds.maxFilterDepth, budget)] : [];
    });
  }
  const drop = { left: queryConfigSize(record).filters - bounds.maxFilters };
  if (Array.isArray(record.quickFilters) && drop.left > 0) {
    const quick = [...record.quickFilters];
    while (quick.length > 0 && drop.left > 0) {
      const last = quick.pop();
      drop.left -= filterShape(last)?.conjunction !== undefined ? groupFilterCount(last) : 1;
    }
    record.quickFilters = quick.length ? quick : undefined;
  }
  if (Array.isArray(record.filters) && drop.left > 0) {
    const own = [...record.filters];
    while (own.length > 0 && drop.left > 0) { own.pop(); drop.left -= 1; }
    record.filters = own;
  }
  if (record.filterGroup && drop.left > 0) record.filterGroup = prunedGroup(record.filterGroup, drop);
  return record as Config;
}

function admitFilterConfig(config: unknown, bounds: QueryBounds, budget: QueryBudget) {
  const record = config && typeof config === "object" && !Array.isArray(config)
    ? config as { filterGroup?: unknown; filters?: unknown; quickFilters?: unknown }
    : undefined;
  let filters = Array.isArray(record?.filters) ? record.filters.length : 0;
  let groups = 0;
  const stack: Array<{ value: unknown; depth: number }> = [];
  if (record?.filterGroup) stack.push({ value: record.filterGroup, depth: 0 });
  for (const term of Array.isArray(record?.quickFilters) ? record.quickFilters : []) {
    if (filterShape(term)?.conjunction !== undefined) stack.push({ value: term, depth: 0 });
    else filters += 1;
  }

  while (stack.length > 0) {
    const current = stack.pop()!;
    if (current.depth > bounds.maxFilterDepth) {
      throw new RangeError("Database query filter tree exceeds its caller-owned depth bound.");
    }
    const group = filterShape(current.value);
    if (!group) continue;
    groups += 1;
    filters += Array.isArray(group.filters) ? group.filters.length : 0;
    if (filters > bounds.maxFilters) {
      throw new RangeError("Database query filters exceed their caller-owned bound.");
    }
    if (groups > bounds.maxFilterGroups) {
      throw new RangeError("Database query filter groups exceed their caller-owned bound.");
    }
    for (const child of Array.isArray(group.groups) ? group.groups : []) {
      stack.push({ value: child, depth: current.depth + 1 });
    }
  }
  if (filters > bounds.maxFilters) {
    throw new RangeError("Database query filters exceed their caller-owned bound.");
  }
  admitValue(config, bounds, budget);
}

function admitProperties(
  properties: readonly DbProperty[],
  bounds: QueryBounds,
  budget: QueryBudget,
) {
  if (properties.length > bounds.maxProperties) {
    throw new RangeError("Database query properties exceed their caller-owned bound.");
  }
  for (const property of properties) {
    const options = property.config?.options ?? [];
    if (
      options.length > bounds.maxOptionsPerProperty
      || options.length > PRESERVED_MAX_OPTIONS
    ) {
      throw new RangeError("Database query options exceed the preserved or caller-owned bound.");
    }
    const config = property.config as {
      statusGroups?: unknown[];
      notion?: { status?: { groups?: unknown[] } };
    } | undefined;
    const statusGroups = config?.statusGroups ?? config?.notion?.status?.groups ?? [];
    if (statusGroups.length > bounds.maxStatusGroups) {
      throw new RangeError("Database query status groups exceed their caller-owned bound.");
    }
  }
  admitValue(properties, bounds, budget);
}

function admitRows(
  rows: readonly Page[],
  properties: readonly DbProperty[],
  bounds: QueryBounds,
  budget: QueryBudget,
) {
  if (rows.length > bounds.maxRows) {
    throw new RangeError("Database query rows exceed their caller-owned bound.");
  }
  const relationProperties = properties.filter((property) => property.type === "relation");
  for (const row of rows) {
    for (const property of relationProperties) {
      const value = row.properties?.[property.id];
      if (Array.isArray(value) && value.length > bounds.maxRelationIds) {
        throw new RangeError("Database query relation ids exceed their caller-owned bound.");
      }
    }
  }
  admitValue(rows, bounds, budget);
}

function admitPages(pagesById: Readonly<Record<string, Page>>, bounds: QueryBounds, budget: QueryBudget) {
  let pages = 0;
  for (const id in pagesById) {
    if (!Object.prototype.hasOwnProperty.call(pagesById, id)) continue;
    pages += 1;
    if (pages > bounds.maxPages) {
      throw new RangeError("Database query page context exceeds its caller-owned bound.");
    }
  }
  admitValue(pagesById, bounds, budget);
}

export function admitQueryRequest(
  rows: readonly Page[],
  properties: readonly DbProperty[],
  view: Pick<DbView, "config">,
  pagesById: Readonly<Record<string, Page>>,
  options: unknown,
  bounds: QueryBounds,
) {
  const budget = createBudget(bounds);
  const sorts = view.config?.sorts ?? [];
  if (sorts.length > bounds.maxSorts) {
    throw new RangeError("Database query sorts exceed their caller-owned bound.");
  }
  admitProperties(properties, bounds, budget);
  admitRows(rows, properties, bounds, budget);
  admitPages(pagesById, bounds, budget);
  admitFilterConfig(view.config, bounds, budget);
  admitValue(options, bounds, budget);
}

// Pages admitted under bounds that set no total: a page's values are its
// own, so one admitted stays within them and is not walked again when the
// rows around it change (2026-11-11: each edit that arrived on a card view
// of 100,000 rows walked every value of every row).
const admittedPages = new WeakMap<QueryBounds, WeakSet<Page>>();

function setsNoTotal(bounds: QueryBounds) {
  return [bounds.maxValueNodes, bounds.maxContainerEntries, bounds.maxTotalCharacters, bounds.maxRelationIds]
    .every((bound) => bound >= Number.MAX_SAFE_INTEGER);
}

export function admitQueryProjectionLifecycle(
  rows: readonly Page[],
  value: unknown,
  bounds: QueryBounds,
) {
  const budget = createBudget(bounds);
  if (!setsNoTotal(bounds)) admitRows(rows, [], bounds, budget);
  else {
    if (rows.length > bounds.maxRows) throw new RangeError("Database query rows exceed their caller-owned bound.");
    const admitted = admittedPages.get(bounds) ?? new WeakSet<Page>();
    admittedPages.set(bounds, admitted);
    const fresh = rows.filter((row) => !admitted.has(row));
    admitRows(fresh, [], bounds, budget);
    for (const row of fresh) admitted.add(row);
  }
  admitValue(value, bounds, budget);
}

export function admitQueryProperties(
  properties: readonly DbProperty[],
  view: Pick<DbView, "config">,
  bounds: QueryBounds,
) {
  const budget = createBudget(bounds);
  admitProperties(properties, bounds, budget);
  admitFilterConfig(view.config, bounds, budget);
}

export function admitQueryViewConfig(config: unknown, bounds: QueryBounds) {
  admitFilterConfig(config, bounds, createBudget(bounds));
}

export function admitQuerySeedRequest(
  properties: readonly DbProperty[],
  view: Pick<DbView, "config">,
  omittedPropertyIds: Iterable<string>,
  options: unknown,
  bounds: QueryBounds,
) {
  const budget = createBudget(bounds);
  admitProperties(properties, bounds, budget);
  admitFilterConfig(view.config, bounds, budget);
  admitValue(options, bounds, budget);
  const omitted: string[] = [];
  for (const value of omittedPropertyIds) {
    if (omitted.length >= bounds.maxProperties) {
      throw new RangeError("Database query omitted-property ids exceed their caller-owned bound.");
    }
    if (typeof value !== "string") {
      throw new RangeError("Database query omitted-property id must be a string.");
    }
    reserveString(value, bounds, budget);
    omitted.push(value);
  }
  return omitted;
}
