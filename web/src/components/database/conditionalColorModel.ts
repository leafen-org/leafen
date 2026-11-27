import type { DbProperty, FilterGroup, Page } from "@/lib/types";
import { withoutAwaitingFilters } from "@/shared/database/queryFilterGroups";
import { isNoValueRelativeDateFilterOperator } from "@/shared/database/queryRelativeDate.mjs";
import type { QueryFilterGroup } from "@/shared/database/queryTypes";
import { COLOR_NAMES, type ColorName } from "./colors";
import { effectiveFilterOperator, filterPropertyForComparison } from "./databaseFilterPolicy";
import { cellValue, optionIds, matchesFilterGroup, type QueryBounds } from "./query";

/**
 * A view's conditional colors, as Notion's View settings → Conditional
 * color: each setting colors the pages its conditions match, by the property
 * it colors by. "auto" takes each page's own option color from that property,
 * as Notion colors pages by a select, status or multi-select by default. In a
 * table a setting colors the whole row or only that property's cell. The
 * first setting a page matches colors it; "default" (Notion's "기본") is no
 * color, and a page it matches takes none from the settings after it (seen
 * 2026-10-24). A condition still waiting for its value colors no page
 * (matchingConditions).
 */
export type ConditionalColor = "auto" | ColorName;

export type ConditionalColorSetting = {
  id: string;
  propertyId: string;
  filterGroup: FilterGroup;
  color: ConditionalColor;
  applyTo?: "row" | "property";
};

export type ConditionalColorMatch = { color: ColorName; applyTo: "row" | "property"; propertyId: string };

/** The view types Notion offers conditional color on. */
export const CONDITIONAL_COLOR_VIEW_TYPES: ReadonlySet<string> = new Set(["table", "board", "gallery", "list", "calendar", "timeline", "feed", "map"]);

const OPTION_TYPES = new Set(["select", "status", "multi_select"]);

function isColor(value: unknown): value is ColorName {
  return typeof value === "string" && (COLOR_NAMES as readonly string[]).includes(value);
}

function filterGroup(value: unknown): FilterGroup {
  const group = value && typeof value === "object" ? value as Partial<FilterGroup> : {};
  return {
    conjunction: group.conjunction === "or" ? "or" : "and",
    filters: Array.isArray(group.filters) ? group.filters.filter((item) => item && typeof item.propertyId === "string") : [],
    ...(Array.isArray(group.groups) && group.groups.length ? { groups: group.groups.map(filterGroup) } : {}),
  };
}

/** The settings a view config holds, without unusable entries. */
export function conditionalColorSettings(config: Record<string, unknown> | undefined): ConditionalColorSetting[] {
  const raw = Array.isArray(config?.conditionalColors) ? config.conditionalColors : [];
  const seen = new Set<string>();
  const settings: ConditionalColorSetting[] = [];
  for (const item of raw) {
    if (!item || typeof item !== "object") continue;
    const entry = item as Record<string, unknown>;
    if (typeof entry.id !== "string" || !entry.id || seen.has(entry.id) || typeof entry.propertyId !== "string") continue;
    seen.add(entry.id);
    settings.push({
      id: entry.id,
      propertyId: entry.propertyId,
      filterGroup: filterGroup(entry.filterGroup),
      color: entry.color === "auto" || isColor(entry.color) ? entry.color : "gray",
      ...(entry.applyTo === "property" ? { applyTo: "property" as const } : {}),
    });
  }
  return settings;
}

/**
 * A new setting coloring by a property: by its option colors when it has them, else green, as Notion's (seen
 * 2026-10-24); its condition "비어 있지 않음", or the property's first when it has none — a checkbox's "체크 값과
 * 동일한 데이터" with no value yet, as Notion's (seen 2026-11-22 at 1440 dark, dpr 1).
 */
export function newConditionalColorSetting(property: DbProperty, id: string, properties: readonly DbProperty[] = []): ConditionalColorSetting {
  const operator = effectiveFilterOperator(filterPropertyForComparison(property, properties), "is_not_empty");
  return {
    id,
    propertyId: property.id,
    filterGroup: { conjunction: "and", filters: [{ propertyId: property.id, operator }] },
    color: OPTION_TYPES.has(property.type) ? "auto" : "green",
  };
}

/** Whether a setting has one condition, on the property it colors by: Notion's card and phone sheet show it in place. */
export function conditionalColorHasOneCondition(setting: ConditionalColorSetting) {
  const { filters, groups } = setting.filterGroup;
  return !groups?.length && filters.length === 1 && filters[0].propertyId === setting.propertyId;
}

/** Whether a property can give pages their own option colors. */
export function conditionalColorHasOptionColors(property: DbProperty | undefined) {
  return !!property && OPTION_TYPES.has(property.type);
}

/**
 * What a setting's conditions match by: each as its card shows it, the condition its property takes (a checkbox's
 * stored "비어 있지 않음" is "체크 값과 동일한 데이터"; a fixed day like "this_week", shown as 오늘 기준, reads as
 * itself), without those still waiting for a value by the filter's own rule (withoutAwaitingFilters). Unlike a
 * filter's, which then keeps every page, a setting left with none colors no page, as Notion's (seen 2026-11-22 at 1440 dark, dpr 1: a checkbox "값과 동일한"/"동일하지 않은 데이터", a text
 * "값을 포함하는 데이터", a number "≠", a select "값과 동일하지 않은 데이터" and a date "범위 내" with no value
 * colored no row, a later setting still coloring its own).
 */
function matchingConditions(group: FilterGroup, properties: readonly DbProperty[], byId: Map<string, DbProperty>): FilterGroup | null {
  const shown = (current: FilterGroup): FilterGroup => ({
    ...current,
    filters: current.filters.map((filter) => {
      const property = byId.get(filter.propertyId);
      if (!property || isNoValueRelativeDateFilterOperator(filter.operator)) return filter;
      return { ...filter, operator: effectiveFilterOperator(filterPropertyForComparison(property, properties), filter.operator) };
    }),
    ...(current.groups ? { groups: current.groups.map(shown) } : {}),
  });
  return (withoutAwaitingFilters(shown(group) as unknown as QueryFilterGroup) as unknown as FilterGroup | undefined) ?? null;
}

type MatchingSetting = ConditionalColorSetting & { conditions: FilterGroup | null };

/** A view's settings, each with the conditions it matches by, read once for all its pages. */
function matchingSettings(settings: readonly ConditionalColorSetting[], properties: DbProperty[]): MatchingSetting[] {
  const byId = new Map(properties.map((property) => [property.id, property]));
  return settings.map((setting) => ({ ...setting, conditions: matchingConditions(setting.filterGroup, properties, byId) }));
}

function conditionsOf(setting: ConditionalColorSetting | MatchingSetting, properties: readonly DbProperty[], byId: Map<string, DbProperty>) {
  return "conditions" in setting ? setting.conditions : matchingConditions(setting.filterGroup, properties, byId);
}

function optionColor(row: Page, property: DbProperty): ColorName | null {
  const first = optionIds(cellValue(row, property))[0];
  const option = first ? property.config?.options?.find((candidate) => candidate.id === first) : undefined;
  return option && isColor(option.color) && option.color !== "default" ? option.color : null;
}

/** The color the first matching setting gives a page, or none. */
export function conditionalColorFor(
  row: Page,
  settings: readonly (ConditionalColorSetting | MatchingSetting)[],
  properties: DbProperty[],
  context: { pagesById: Record<string, Page>; currentPageId?: string; currentUserId?: string },
  bounds: QueryBounds,
): ConditionalColorMatch | null {
  if (!settings.length) return null;
  const byId = new Map(properties.map((property) => [property.id, property]));
  const queryContext = { props: properties, pagesById: context.pagesById, currentPageId: context.currentPageId, currentUserId: context.currentUserId, bounds };
  for (const setting of settings) {
    const property = byId.get(setting.propertyId);
    if (!property) continue;
    const conditions = conditionsOf(setting, properties, byId);
    if (!conditions || !matchesFilterGroup(row, conditions, queryContext, byId, bounds)) continue;
    if (setting.color === "default") return null;
    const color = setting.color === "auto" ? optionColor(row, property) : setting.color;
    if (!color) continue;
    return { color, applyTo: setting.applyTo ?? "row", propertyId: property.id };
  }
  return null;
}

/**
 * A table row's colors cell by cell, as Notion's (seen 2026-10-24): a cell
 * takes the first setting the page matches that reaches it — one coloring
 * the whole row, or one coloring that property — so a property's setting
 * before a row's colors its cell over the row, and one after it doesn't;
 * "default" leaves the cells it reaches uncolored.
 */
export function conditionalCellColorsFor(
  row: Page,
  settings: readonly (ConditionalColorSetting | MatchingSetting)[],
  properties: DbProperty[],
  context: { pagesById: Record<string, Page>; currentPageId?: string; currentUserId?: string },
  bounds: QueryBounds,
): ((propertyId: string) => ColorName | null) | null {
  if (!settings.length) return null;
  const byId = new Map(properties.map((property) => [property.id, property]));
  const queryContext = { props: properties, pagesById: context.pagesById, currentPageId: context.currentPageId, currentUserId: context.currentUserId, bounds };
  const matched: Array<{ applyTo: "row" | "property"; propertyId: string; color: ColorName | null }> = [];
  for (const setting of settings) {
    const property = byId.get(setting.propertyId);
    if (!property) continue;
    const conditions = conditionsOf(setting, properties, byId);
    if (!conditions || !matchesFilterGroup(row, conditions, queryContext, byId, bounds)) continue;
    const color = setting.color === "default" ? null : setting.color === "auto" ? optionColor(row, property) : setting.color;
    if (!color && setting.color !== "default") continue;
    matched.push({ applyTo: setting.applyTo ?? "row", propertyId: property.id, color });
    if ((setting.applyTo ?? "row") === "row") break;
  }
  if (!matched.length) return null;
  return (propertyId) => matched.find((match) => match.applyTo === "row" || match.propertyId === propertyId)?.color ?? null;
}

/** Reads a table row's conditional colors, cell by cell, under a view's settings. */
export function conditionalCellColorResolver(
  config: Record<string, unknown> | undefined,
  properties: DbProperty[],
  context: { pagesById: Record<string, Page>; currentPageId?: string; currentUserId?: string },
  bounds: QueryBounds,
): (row: Page) => ((propertyId: string) => ColorName | null) | null {
  const settings = matchingSettings(conditionalColorSettings(config), properties);
  if (!settings.length) return () => null;
  return (row) => conditionalCellColorsFor(row, settings, properties, context, bounds);
}

/** Reads a page's conditional color under a view's settings. */
export function conditionalColorResolver(
  config: Record<string, unknown> | undefined,
  properties: DbProperty[],
  context: { pagesById: Record<string, Page>; currentPageId?: string; currentUserId?: string },
  bounds: QueryBounds,
): (row: Page) => ConditionalColorMatch | null {
  const settings = matchingSettings(conditionalColorSettings(config), properties);
  if (!settings.length) return () => null;
  return (row) => conditionalColorFor(row, settings, properties, context, bounds);
}

/** A page background for a conditional color. */
export function conditionalColorBackground(color: ColorName) {
  return `var(--c-bg-${color === "default" ? "gray" : color})`;
}
