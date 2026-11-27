import { pageNamesNow } from "@/lib/pageNames";
import type { DbProperty, Page } from "@/lib/types";
import { formatDateForProperty, formatNotionTimestamp } from "./dateUtils";
import { normalizeFileAttachments } from "./files";
import { formatNumberForProperty } from "./numberFormat";
import { normalizePersonIds, personLabel } from "@/lib/peopleDirectory";
import {
  evaluateRollupCore,
  type RollupContext,
  type RollupPage,
  type RollupProperty,
} from "./rollupCore";
import {
  admitPageLookup,
  admitProperties,
  admitRollupValue,
  consumeRollupString,
  createRollupBudget,
  reserveStringCharacters,
  reserveValuePieces,
  rollupShownFunction,
  valueAsIdsWithin,
  type RollupBounds,
  type RollupBudget,
  type RollupResult,
} from "./rollupPolicy";
import { valueAsRead } from "./valueAsRead";

export type { RollupBounds, RollupResult } from "./rollupPolicy";

function joined(
  values: readonly string[],
  bounds: RollupBounds,
  budget: RollupBudget,
) {
  let length = Math.max(0, values.length - 1) * 2;
  for (const value of values) length += value.length;
  reserveStringCharacters(length, bounds, budget);
  return values.join(", ");
}

function output(value: string, bounds: RollupBounds, budget: RollupBudget) {
  consumeRollupString(value, bounds, budget);
  return value;
}

function admitOptions(prop: DbProperty, bounds: RollupBounds, budget: RollupBudget) {
  const options = prop.config?.options ?? [];
  if (options.length > bounds.maxOptions) {
    throw new RangeError("Rollup options exceed their caller-owned bound.");
  }
  for (const option of options) {
    consumeRollupString(option.id, bounds, budget);
    consumeRollupString(option.name, bounds, budget);
    consumeRollupString(option.color, bounds, budget);
    if (option.group) consumeRollupString(option.group, bounds, budget);
  }
  return options;
}

/**
 * The related rows a reader may not see: looked up, yet not among the pages
 * they can read. Notion shows such a relation, and a rollup or formula that
 * reads it, as "사용 권한 없음" (seen 2026-10-06) rather than reckoning without
 * them.
 */
export function unreadableRelationIds(
  ids: readonly string[],
  pagesById: Readonly<Record<string, unknown>>,
  hydrated: ReadonlySet<string>,
) {
  return ids.filter((id) => !pagesById[id] && hydrated.has(id));
}

export function valueAsIds(value: unknown, bounds: RollupBounds): string[] {
  return valueAsIdsWithin(value, bounds, createRollupBudget(bounds));
}

/**
 * The pages a rollup shows as they are, each a page chip as Notion's: the
 * related pages when it rolls up their names (seen 2026-11-02), the pages
 * their relation names when it rolls up a relation (seen 2026-11-04). None
 * for anything else, or a related page not loaded.
 */
export function rolledUpPageIds(row: Page, relation: DbProperty, target: DbProperty | undefined, pagesById: Record<string, Page>,
  bounds: RollupBounds): string[] | null {
  const related = valueAsIds(row.properties?.[relation.id], bounds);
  if (target?.type === "title") return related;
  if (target?.type !== "relation") return null;
  const ids: string[] = [];
  for (const id of related) {
    const page = pagesById[id];
    if (!page) return null;
    ids.push(...valueAsIds(rawValue(page, target), bounds));
  }
  return ids;
}

// A page's created and edited times as a rollup reads them: to the minute,
// as Notion keeps them (seen 2026-11-12: two times in one minute are one
// value, and the earliest is that minute).
function minuteOf(value: string | null | undefined) {
  const at = value ? Date.parse(value) : Number.NaN;
  return Number.isFinite(at) ? new Date(at - (((at % 60_000) + 60_000) % 60_000)).toISOString() : value;
}

function rawValue(row: Page, prop: DbProperty): unknown {
  if (prop.type === "title") return row.title;
  if (prop.type === "created_time") return minuteOf(row.createdAt);
  if (prop.type === "last_edited_time") return minuteOf(row.updatedAt);
  if (prop.type === "created_by") return row.createdBy;
  if (prop.type === "last_edited_by") return row.lastEditedBy;
  return valueAsRead(row, prop);
}

export function displayPropertyValue(
  row: Page,
  prop: DbProperty,
  pagesById: Record<string, Page>,
  bounds: RollupBounds,
  sharedBudget?: RollupBudget,
): string {
  const budget = sharedBudget ?? createRollupBudget(bounds);
  const value = rawValue(row, prop);
  if (prop.type === "title") return output(pageNamesNow.title(row), bounds, budget);
  if (prop.type === "select" || prop.type === "status") {
    const id = value ? String(value) : "";
    consumeRollupString(id, bounds, budget);
    const name = admitOptions(prop, bounds, budget).find((option) => option.id === id)?.name ?? id;
    return output(name, bounds, budget);
  }
  if (prop.type === "multi_select") {
    const ids = valueAsIdsWithin(value, bounds, budget);
    const options = admitOptions(prop, bounds, budget);
    const names: string[] = [];
    const seen = new Set<string>();
    for (const id of ids) {
      if (seen.has(id)) continue;
      seen.add(id);
      const name = options.find((option) => option.id === id)?.name ?? id;
      if (name) names.push(name);
    }
    return joined(names, bounds, budget);
  }
  if (prop.type === "checkbox") return value ? "Checked" : "Unchecked";
  if (prop.type === "number") {
    admitRollupValue(value, bounds, budget);
    const numberProperty = prop as unknown as Parameters<typeof formatNumberForProperty>[1];
    return output(formatNumberForProperty(value, numberProperty), bounds, budget);
  }
  if (prop.type === "date") {
    admitRollupValue(value, bounds, budget);
    return output(formatDateForProperty(value, prop), bounds, budget);
  }
  if (prop.type === "created_time" || prop.type === "last_edited_time") {
    admitRollupValue(value, bounds, budget);
    return output(formatNotionTimestamp(value), bounds, budget);
  }
  if (prop.type === "relation") {
    const titles: string[] = [];
    for (const id of valueAsIdsWithin(value, bounds, budget)) {
      admitPageLookup(id, bounds, budget);
      const title = pageNamesNow.title(pagesById[id]);
      consumeRollupString(title, bounds, budget);
      titles.push(title);
    }
    return joined(titles, bounds, budget);
  }
  if (prop.type === "files") {
    admitRollupValue(value, bounds, budget);
    const inputCount = Array.isArray(value) ? value.length : value ? 1 : 0;
    if (inputCount > bounds.maxFileAttachments) {
      throw new RangeError("Rollup file attachments exceed their caller-owned bound.");
    }
    const files = normalizeFileAttachments(value);
    if (files.length > bounds.maxFileAttachments) {
      throw new RangeError("Rollup file attachments exceed their caller-owned bound.");
    }
    const names = files.map((file) => {
      consumeRollupString(file.name, bounds, budget);
      return file.name;
    });
    return joined(names, bounds, budget);
  }
  if (prop.type === "person" || prop.type === "created_by" || prop.type === "last_edited_by") {
    admitRollupValue(value, bounds, budget);
    const remainingPieces = bounds.maxValuePieces - budget.valuePieces;
    const ids = normalizePersonIds(value, {
      maxNodes: Math.min(bounds.maxPersonNodes, remainingPieces),
      maxDepth: bounds.maxPersonDepth,
      maxEntries: bounds.maxPeopleEntries,
    });
    if (ids.length > bounds.maxValuePieces) {
      throw new RangeError("Rollup person values exceed their caller-owned bound.");
    }
    reserveValuePieces(ids.length, bounds, budget);
    const labels = Array.from(new Set(ids)).map((id) => {
      consumeRollupString(id, bounds, budget);
      return personLabel(id);
    });
    for (const label of labels) consumeRollupString(label, bounds, budget);
    return joined(labels, bounds, budget);
  }
  if (value == null) return "";
  admitRollupValue(value, bounds, budget);
  if (Array.isArray(value)) {
    if (value.length > bounds.maxValuePieces) {
      throw new RangeError("Rollup value pieces exceed their caller-owned bound.");
    }
    const values = value.map(String);
    for (const item of values) consumeRollupString(item, bounds, budget);
    reserveValuePieces(values.length, bounds, budget);
    return joined(values, bounds, budget);
  }
  return output(String(value), bounds, budget);
}

/**
 * The databases a rollup of rollups reaches past its relation's, however
 * far, as far as their properties are loaded (each once): loading each
 * makes the next known.
 */
export function rollupHopDatabaseIds(
  prop: DbProperty,
  targetProps: DbProperty[],
  propsByDb: Record<string, DbProperty[]> | undefined,
  bounds: RollupBounds,
) {
  const budget = createRollupBudget(bounds);
  admitProperties(targetProps, bounds, budget);
  let targetProp = targetProps.find((item) => item.id === prop.config?.rollupTargetPropertyId);
  const hops: string[] = [];
  const seen = new Set<string>();
  for (let hop = 0; targetProp; hop += 1) {
    if (targetProp.type === "relation") {
      hops.push(targetProp.config?.relationDatabaseId ?? targetProp.databaseId);
      break;
    }
    const key = `${targetProp.databaseId}\u0000${targetProp.id}`;
    if (targetProp.type !== "rollup" || seen.has(key)) break;
    seen.add(key);
    const rollupProp: DbProperty = targetProp;
    const ownerProps = propsByDb?.[rollupProp.databaseId] ?? [];
    admitProperties(ownerProps, bounds, budget);
    const viaId = hop === 0 ? prop.config?.rollupVia : undefined;
    const hopRelation = (viaId ? ownerProps.find((item) => item.id === viaId) : undefined)
      ?? ownerProps.find((item) => item.id === rollupProp.config?.rollupRelationPropertyId);
    if (hopRelation?.type !== "relation") break;
    const hopDbId = hopRelation.config?.relationDatabaseId ?? hopRelation.databaseId;
    hops.push(hopDbId);
    const hopProps = propsByDb?.[hopDbId];
    if (!hopProps) break;
    admitProperties(hopProps, bounds, budget);
    targetProp = hopProps.find((item) => item.id === rollupProp.config?.rollupTargetPropertyId);
  }
  return hops;
}

export type EvaluateRollupOptions = {
  row: Page;
  prop: DbProperty;
  sourceProps: DbProperty[];
  targetProps: DbProperty[];
  pagesById: Record<string, Page>;
  propsByDb?: Record<string, DbProperty[]>;
  resolveComputedDisplay?: (row: Page, prop: DbProperty) => string | undefined;
  /** What a related row's formula gives, which it doesn't store. */
  resolveComputedValue?: (row: Page, prop: DbProperty) => unknown;
  bounds: RollupBounds;
};

function evaluateRollupValueWithin<T = RollupResult>(
  options: EvaluateRollupOptions,
  budget: RollupBudget,
  projectOriginal?: (values: unknown[], property: RollupProperty | undefined) => T,
): RollupResult | T {
  const { row, prop, sourceProps, targetProps, pagesById, propsByDb, resolveComputedDisplay, resolveComputedValue, bounds } = options;
  const ctx: RollupContext = {
    pagesById: (id) => pagesById[id],
    propsByDb: (databaseId) => (propsByDb?.[databaseId] ?? []) as unknown as RollupProperty[],
    rawValue: (page, coreProp) => {
      const webProp = coreProp as unknown as DbProperty;
      return webProp.type === "formula" && resolveComputedValue ? resolveComputedValue(page as Page, webProp) : rawValue(page as Page, webProp);
    },
    displayValue: (page, coreProp) => {
      const webPage = page as Page;
      const webProp = coreProp as unknown as DbProperty;
      return resolveComputedDisplay?.(webPage, webProp)
        ?? displayPropertyValue(webPage, webProp, pagesById, bounds, budget);
    },
  };
  return evaluateRollupCore(
    row as RollupPage,
    prop as unknown as RollupProperty,
    sourceProps as unknown as RollupProperty[],
    targetProps as unknown as RollupProperty[],
    ctx,
    bounds,
    budget,
    projectOriginal,
  );
}

export function evaluateRollupValue(options: EvaluateRollupOptions): RollupResult {
  return evaluateRollupValueWithin(options, createRollupBudget(options.bounds));
}

/** Group consumers retain original value members rather than splitting display text. */
export function evaluateRollupGroupingValue(options: EvaluateRollupOptions): unknown {
  const budget = createRollupBudget(options.bounds);
  return evaluateRollupValueWithin(options, budget, (values, target) => {
    const property = target as DbProperty | undefined;
    if (!property) return values;
    if (["select", "multi_select", "status"].includes(property.type)) {
      const names = new Map(admitOptions(property, options.bounds, budget).map((option) => [option.id, option.name]));
      return values.map((value) => (Array.isArray(value) ? value : [value]).map((id) => names.get(String(id)) ?? String(id ?? "")));
    }
    if (property.type === "person" || property.type === "created_by" || property.type === "last_edited_by") {
      return values.map((value) => normalizePersonIds(value, {
        maxNodes: options.bounds.maxPersonNodes, maxDepth: options.bounds.maxPersonDepth, maxEntries: options.bounds.maxPeopleEntries,
      }).map((id) => ({ kind: "person", id, name: personLabel(id) })));
    }
    if (property.type === "date" || property.type === "created_time" || property.type === "last_edited_time") {
      return values.map((value) => typeof value === "string" && value ? { start: value } : value);
    }
    return values;
  });
}

/** Whether a rollup may show checkboxes: one rolling up a checkbox, a formula or a rollup. */
export function rollupMayShowCheckboxes(targetProps: readonly DbProperty[], prop: DbProperty) {
  const target = targetProps.find((property) => property.id === prop.config?.rollupTargetPropertyId);
  return target?.type === "checkbox" || target?.type === "formula" || target?.type === "rollup";
}

/** The related values a rollup shows as they are, and the property they are of; null when it shows something else. */
export function evaluateRollupOriginals(options: EvaluateRollupOptions) {
  const shown = evaluateRollupValueWithin(options, createRollupBudget(options.bounds), (values, target) => ({ values, target }));
  return typeof shown === "object" && shown !== null ? shown : null;
}

/**
 * A rollup of a status's or select's values shown as they are, each value its
 * option in the related pages' order (once each for "고유한 값 표시"), drawn as
 * chips, as Notion's (seen 2026-11-05). Null when it shows something else, or
 * a related page isn't read here, or none has a value.
 */
export function rollupOptionValues(row: Page, prop: DbProperty, relationProp: DbProperty | undefined, target: DbProperty | undefined,
  pagesById: Readonly<Record<string, Page | undefined>>, bounds: RollupBounds) {
  const rollupFunction = rollupShownFunction(prop.config) ?? "show_original";
  if (!relationProp || !target || (rollupFunction !== "show_original" && rollupFunction !== "show_unique")) return null;
  if (target.type !== "status" && target.type !== "select" && target.type !== "multi_select") return null;
  const options = target.config?.options ?? [];
  const values: { key: string; option: (typeof options)[number]; status: boolean }[] = [];
  const seen = new Set<string>();
  for (const id of valueAsIds(row.properties?.[relationProp.id], bounds)) {
    const page = pagesById[id];
    if (!page) return null;
    // A status no one chose reads as its default, a grey chip (seen 2026-11-12).
    const raw = valueAsRead(page, target);
    for (const optionId of (Array.isArray(raw) ? raw : raw ? [raw] : []).map(String)) {
      const option = options.find((candidate) => candidate.id === optionId);
      if (!option || (rollupFunction === "show_unique" && seen.has(option.id))) continue;
      seen.add(option.id);
      values.push({ key: `${id}:${option.id}`, option, status: target.type === "status" });
    }
  }
  return values.length ? values : null;
}
