import {
  admitDatabaseLookup,
  admitPageLookup,
  admitProperties,
  admitRollupValue,
  consumeRollupString,
  reserveStringCharacters,
  rollupCheckedValue,
  rollupCountsValue,
  rollupCheckboxTakes,
  rollupDateIsDay,
  rollupDateKey,
  rollupDateValuesFromAdmitted,
  rollupFunctionSupportedForPropertyType,
  rollupGroupCountOf,
  rollupGroupPercent,
  rollupPercent,
  rollupShownFunction,
  rollupStatusGroupOf,
  rollupValuePiecesFromAdmitted,
  valueAsIdsWithin,
  valueIsPresent,
  type RollupBounds,
  type RollupBudget,
  type RollupResult,
} from "./rollupPolicy";

export interface RollupProperty {
  id: string;
  databaseId: string;
  type: string;
  config?: Record<string, unknown> | null;
}

export interface RollupPage {
  id: string;
  inTrash?: boolean;
  properties?: Record<string, unknown> | null;
}

export interface RollupContext {
  pagesById: (id: string) => RollupPage | undefined;
  propsByDb: (databaseId: string) => RollupProperty[];
  rawValue: (page: RollupPage, prop: RollupProperty) => unknown;
  displayValue: (page: RollupPage, prop: RollupProperty) => string;
}

type BoundedContext = RollupContext & {
  bounds: RollupBounds;
  budget: RollupBudget;
};

const TITLE_PROP: RollupProperty = { id: "__title", databaseId: "", type: "title", config: null };
const RELATION_COUNTS = new Set(["count_values", "empty", "not_empty", "unique", "percent_empty", "percent_not_empty"]);

function boundedContext(
  context: RollupContext,
  bounds: RollupBounds,
  budget: RollupBudget,
): BoundedContext {
  const pageCache = new Map<string, RollupPage | undefined>();
  const propertyCache = new Map<string, RollupProperty[]>();
  return {
    ...context,
    bounds,
    budget,
    pagesById(id) {
      if (pageCache.has(id)) return pageCache.get(id);
      admitPageLookup(id, bounds, budget);
      const page = context.pagesById(id);
      pageCache.set(id, page);
      return page;
    },
    propsByDb(databaseId) {
      const cached = propertyCache.get(databaseId);
      if (cached) return cached;
      admitDatabaseLookup(databaseId, bounds, budget);
      const properties = context.propsByDb(databaseId);
      admitProperties(properties, bounds, budget);
      propertyCache.set(databaseId, properties);
      return properties;
    },
  };
}

function admitPages(pages: readonly RollupPage[], bounds: RollupBounds) {
  if (pages.length > bounds.maxPages) {
    throw new RangeError("Rollup pages exceed their caller-owned bound.");
  }
}

function relationTargetDatabaseId(relationProp: RollupProperty) {
  const configured = relationProp.config?.relationDatabaseId;
  return typeof configured === "string" ? configured : relationProp.databaseId;
}

function relatedPages(
  page: RollupPage,
  relationProp: RollupProperty,
  ctx: BoundedContext,
) {
  const ids = valueAsIdsWithin(page.properties?.[relationProp.id], ctx.bounds, ctx.budget);
  const pages: RollupPage[] = [];
  for (const id of ids) {
    const related = ctx.pagesById(id);
    if (!related || related.inTrash) continue;
    if (pages.length >= ctx.bounds.maxPages) {
      throw new RangeError("Rollup pages exceed their caller-owned bound.");
    }
    pages.push(related);
  }
  return pages;
}

function resolveRollupHops(
  startPages: RollupPage[],
  targetProp: RollupProperty | undefined,
  prop: RollupProperty,
  ctx: BoundedContext,
) {
  let pages = startPages;
  let current = targetProp;
  // A rollup of a rollup is followed however far, each property once (a
  // cycle ends where it comes back).
  const seenProperties = new Set<string>();
  for (let hop = 0; ; hop += 1) {
    if (!current || (current.type !== "relation" && current.type !== "rollup")) break;
    const propertyKey = `${current.databaseId}\u0000${current.id}`;
    if (seenProperties.has(propertyKey)) break;
    seenProperties.add(propertyKey);
    const ownerProps = ctx.propsByDb(current.databaseId);
    const viaId = hop === 0 && typeof prop.config?.rollupVia === "string"
      ? prop.config.rollupVia
      : undefined;
    const hopRelation = current.type === "relation"
      ? current
      : (viaId ? ownerProps.find((item) => item.id === viaId) : undefined)
        ?? ownerProps.find((item) => item.id === current?.config?.rollupRelationPropertyId);
    if (!hopRelation || hopRelation.type !== "relation") break;
    const hopDbId = relationTargetDatabaseId(hopRelation);

    const nextPages: RollupPage[] = [];
    for (const page of pages) {
      for (const related of relatedPages(page, hopRelation, ctx)) {
        if (nextPages.length >= ctx.bounds.maxPages) {
          throw new RangeError("Rollup pages exceed their caller-owned bound.");
        }
        nextPages.push(related);
      }
    }
    pages = nextPages;
    admitPages(pages, ctx.bounds);
    const hopProps = ctx.propsByDb(hopDbId);
    current = current.type === "rollup"
      ? hopProps.find((item) => item.id === current?.config?.rollupTargetPropertyId)
      : undefined;
  }
  return { pages, targetProp: current };
}

function boundedString(value: string, bounds: RollupBounds, budget: RollupBudget) {
  consumeRollupString(value, bounds, budget);
  return value;
}

function boundedJoin(
  values: readonly string[],
  separator: string,
  bounds: RollupBounds,
  budget: RollupBudget,
) {
  let length = Math.max(0, values.length - 1) * separator.length;
  for (const value of values) length += value.length;
  reserveStringCharacters(length, bounds, budget);
  return values.join(separator);
}

function extrema(values: readonly number[]) {
  let minimum = values[0];
  let maximum = values[0];
  for (let index = 1; index < values.length; index += 1) {
    minimum = Math.min(minimum, values[index]);
    maximum = Math.max(maximum, values[index]);
  }
  return { minimum, maximum };
}

export function evaluateRollupCore<T = RollupResult>(
  row: RollupPage,
  prop: RollupProperty,
  sourceProps: RollupProperty[],
  targetProps: RollupProperty[],
  context: RollupContext,
  bounds: RollupBounds,
  budget: RollupBudget,
  projectOriginal?: (values: unknown[], property: RollupProperty | undefined) => T,
): RollupResult | T {
  admitProperties(sourceProps, bounds, budget);
  admitProperties(targetProps, bounds, budget);
  const ctx = boundedContext(context, bounds, budget);
  const relationProp = sourceProps.find((item) => item.id === prop.config?.rollupRelationPropertyId);
  if (!relationProp) return "";
  const firstPages = relatedPages(row, relationProp, ctx);
  admitPages(firstPages, bounds);
  const fn = rollupShownFunction(prop.config) ?? "show_original";
  if (fn === "count") return firstPages.length;
  // No property rolled up yet: not set up, empty as the server reads it.
  if (!prop.config?.rollupTargetPropertyId) return "";

  const firstTarget = targetProps.find((item) => item.id === prop.config?.rollupTargetPropertyId);
  // A relation rolled up is counted row by row as its pages, untitled ones
  // too: a row relating to none is empty, and the count of values and the
  // different values are of the pages, as Notion's (seen 2026-11-12).
  if (firstTarget?.type === "relation" && RELATION_COUNTS.has(fn)) {
    const lists = firstPages.map((page) => relatedPages(page, firstTarget, ctx).map((related) => related.id));
    const present = lists.filter((list) => list.length > 0).length;
    if (fn === "count_values") return lists.reduce((total, list) => total + list.length, 0);
    if (fn === "empty") return lists.length - present;
    if (fn === "not_empty") return present;
    if (fn === "unique") return new Set(lists.flat()).size;
    if (!lists.length) return "";
    return boundedString(rollupPercent(fn === "percent_empty" ? lists.length - present : present, lists.length), bounds, budget);
  }
  const resolved = firstTarget && (firstTarget.type === "relation" || firstTarget.type === "rollup")
    ? resolveRollupHops(firstPages, firstTarget, prop, ctx)
    : { pages: firstPages, targetProp: firstTarget };
  const countGroup = rollupGroupCountOf(prop.config);
  const statusGroup = rollupStatusGroupOf(prop.config) ?? countGroup;
  const targetType = resolved.targetProp?.type ?? "title";
  if (!statusGroup && !rollupFunctionSupportedForPropertyType(fn, targetType) && !rollupCheckboxTakes(fn, targetType)) return "";

  const values: unknown[] = [];
  // The values the counts of values count: a checkbox, ticked or not, is
  // neither a value nor empty to them (rollupCountsValue).
  const countedValues: unknown[] = [];
  const presentValues: unknown[] = [];
  for (const page of resolved.pages) {
    const value = ctx.rawValue(page, resolved.targetProp ?? TITLE_PROP);
    admitRollupValue(value, bounds, budget);
    values.push(value);
    if (!rollupCountsValue(value, targetType)) continue;
    countedValues.push(value);
    if (valueIsPresent(value)) presentValues.push(value);
  }
  if (statusGroup) {
    const groups = (resolved.targetProp?.config as { statusGroups?: { id?: unknown; optionIds?: unknown }[] } | null | undefined)?.statusGroups;
    const options = groups?.find((group) => group.id === statusGroup)?.optionIds;
    const inGroup = Array.isArray(options) ? values.filter((value) => options.includes(value)).length : 0;
    // A group's count reads as it out of all, "2/4", "0/0" over none, as Notion's (seen 2026-11-12).
    if (countGroup) return boundedString(`${inGroup}/${values.length}`, bounds, budget);
    if (!values.length) return "";
    return boundedString(rollupGroupPercent(inGroup, values.length), bounds, budget);
  }
  // A list's items count each (a multi-select's options, a file list's files), as Notion's (seen 2026-11-12).
  if (fn === "count_values") return presentValues.reduce<number>((total, value) => total + (Array.isArray(value) ? value.filter(valueIsPresent).length : 1), 0);
  if (fn === "empty") return countedValues.length - presentValues.length;
  if (fn === "not_empty") return presentValues.length;
  // A share of nothing is empty, as Notion shows it (seen 2026-10-04); a
  // share of checkboxes alone, which it doesn't count, shows them (seen
  // 2026-11-12).
  const percent = fn === "percent_empty" || fn === "percent_not_empty";
  if (percent && !countedValues.length && values.length) return originals();
  if (fn === "percent_empty") {
    return countedValues.length ? boundedString(rollupPercent(countedValues.length - presentValues.length, countedValues.length), bounds, budget) : "";
  }
  if (fn === "percent_not_empty") {
    return countedValues.length ? boundedString(rollupPercent(presentValues.length, countedValues.length), bounds, budget) : "";
  }
  if (fn === "unique") {
    const pieces = countedValues.flatMap((value) => rollupValuePiecesFromAdmitted(value, bounds, budget));
    return new Set(pieces).size;
  }

  let checkedCount = 0;
  for (const value of values) if (rollupCheckedValue(value)) checkedCount += 1;
  if (fn === "checked") return checkedCount;
  if (fn === "unchecked") return values.length - checkedCount;
  if (fn === "percent_checked") {
    return values.length ? boundedString(rollupPercent(checkedCount, values.length), bounds, budget) : "";
  }
  if (fn === "percent_unchecked") {
    return values.length ? boundedString(rollupPercent(values.length - checkedCount, values.length), bounds, budget) : "";
  }

  const numbers: number[] = [];
  for (const value of presentValues) {
    const number = Number(value);
    if (Number.isFinite(number)) numbers.push(number);
  }
  // Nothing to add up is 0 for a sum and a median, and empty for the others,
  // as Notion shows (seen 2026-10-03 over a relation to nothing).
  if (fn === "sum") return numbers.reduce((sum, value) => sum + value, 0);
  if (fn === "average") {
    return numbers.length ? numbers.reduce((sum, value) => sum + value, 0) / numbers.length : "";
  }
  if (fn === "median") {
    if (!numbers.length) return 0;
    const sorted = numbers.slice().sort((left, right) => left - right);
    const middle = Math.floor(sorted.length / 2);
    return sorted.length % 2 ? sorted[middle] : (sorted[middle - 1] + sorted[middle]) / 2;
  }
  if (fn === "min" || fn === "max" || fn === "range") {
    if (!numbers.length) return "";
    const { minimum, maximum } = extrema(numbers);
    return fn === "min" ? minimum : fn === "max" ? maximum : maximum - minimum;
  }

  const dates = values
    .flatMap((value) => rollupDateValuesFromAdmitted(value, bounds, budget))
    .sort((left, right) => left.getTime() - right.getTime());
  if (fn === "earliest_date") {
    return dates.length ? boundedString(rollupDateKey(dates[0]), bounds, budget) : "";
  }
  if (fn === "latest_date") {
    return dates.length ? boundedString(rollupDateKey(dates[dates.length - 1]), bounds, budget) : "";
  }
  if (fn === "date_range") {
    if (!dates.length) return "";
    // The ends keep their times, which the range's length is shown from; an
    // end written as a day alone stays a day.
    const instant = (date: Date) => rollupDateIsDay(date) ? rollupDateKey(date) : date.toISOString();
    const start = instant(dates[0]);
    const end = instant(dates[dates.length - 1]);
    return boundedString(start === end ? start : `${start} → ${end}`, bounds, budget);
  }

  return originals(fn === "show_unique", fn === "show_original" || fn === "show_unique");

  function originals(unique = false, project = true) {
    if (projectOriginal && project) return projectOriginal(values, resolved.targetProp);
    const displayed: string[] = [];
    for (const page of resolved.pages) {
      const value = ctx.displayValue(page, resolved.targetProp ?? TITLE_PROP);
      if (!value) continue;
      consumeRollupString(value, bounds, budget);
      displayed.push(value);
    }
    const output = unique ? [...new Set(displayed)] : displayed;
    return boundedJoin(output, ", ", bounds, budget);
  }
}
