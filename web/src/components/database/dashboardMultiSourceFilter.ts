import type { DbProperty, ViewFilter } from "@/lib/types";
import { statusGroupsFromConfig, statusOptionsFromConfig } from "@/shared/database/queryStatusPolicy.mjs";
import type { DashboardFilterSource } from "./dashboardFilterSources";

/**
 * A dashboard's filter over one property of each of several widget sources,
 * Notion's "다중 출처 필터링" (seen twice on 2026-11-17 at 1440 in the
 * Business trial's synthetic dashboard): one pill and one condition that each
 * source reads through its own property — a text and a name property are one
 * kind ("텍스트"), a source gives at most one, and the filter is named after
 * its first property until it names two or more, when it takes a name of
 * its own. Kept on the filter itself (ViewFilter.multiSource), `propertyId`
 * its first property, so its pill and condition are any filter's.
 */
export type MultiSourceFilter = ViewFilter & { multiSource: NonNullable<ViewFilter["multiSource"]> };

export function isMultiSourceFilter(filter: unknown): filter is MultiSourceFilter {
  const multi = (filter as ViewFilter | null)?.multiSource;
  return Boolean(multi && Array.isArray(multi.propertyIds));
}

/**
 * The ids of the properties it names, its first property first; none once
 * its last was taken away, when it filters nothing until one is picked again
 * or it goes as its window closes (Notion's: "필터 ⌄" over the card again).
 */
export function multiSourcePropertyIds(filter: ViewFilter): string[] {
  const ids = filter.multiSource?.propertyIds;
  if (ids && !ids.length) return [];
  return [filter.propertyId, ...(ids ?? []).filter((id) => id !== filter.propertyId)];
}

/** The properties it names that are still there, in its order. */
export function multiSourceProperties<P extends { id: string }>(filter: ViewFilter, properties: readonly P[]): P[] {
  return multiSourcePropertyIds(filter).flatMap((id) => properties.filter((property) => property.id === id));
}

/** The kind of property one filter names across sources: a name property is a text one ("텍스트"), as Notion's lists them together. */
export function multiSourceKind(type: string): string {
  return type === "title" ? "rich_text" : type;
}

/** What its pill and condition call it: the name it was given once it names two or more properties, else its first property's (Notion's). */
export function multiSourceName(filter: ViewFilter, properties: readonly { id: string; name: string }[]): string {
  const named = multiSourceProperties(filter, properties);
  const given = filter.multiSource?.name?.trim();
  return named.length > 1 && given ? given : named[0]?.name ?? "";
}

/**
 * Its first property as its pill, condition and a phone's filter row show it:
 * named as the filter is ("필터" with no property left) and drawn with the
 * icon it was given.
 */
export function multiSourceShownProperty<P extends { id: string; name: string; config?: unknown }>(filter: ViewFilter, property: P,
  properties: readonly { id: string; name: string }[], emptyName: string): P {
  const icon = filter.multiSource?.icon;
  const name = multiSourceProperties(filter, properties).length ? multiSourceName(filter, properties) : emptyName;
  return { ...property, name, config: icon ? { ...(property.config as object | undefined), icon } : property.config };
}

/**
 * The widget sources it may name a property of next, each with its
 * properties of the filter's kind: those it names none of yet (Notion's "더
 * 추가하기": a text filter on one source's "Q3" offered another source's "설명"
 * and "작업 이름", not the first source's "Q4"; a status filter, with no other
 * source holding a status, had no "더 추가하기"). With no property left, any.
 */
export function multiSourceMoreSources(filter: ViewFilter, sources: readonly DashboardFilterSource[]): DashboardFilterSource[] {
  const ids = new Set(multiSourcePropertyIds(filter));
  const first = sources.flatMap(({ properties }) => properties).find(({ id }) => id === filter.propertyId);
  const kind = ids.size && first ? multiSourceKind(first.type) : null;
  return sources.flatMap((entry) => {
    if (entry.properties.some(({ id }) => ids.has(id))) return [];
    const properties = kind ? entry.properties.filter(({ type }) => multiSourceKind(type) === kind) : entry.properties;
    return properties.length ? [{ ...entry, properties }] : [];
  });
}

/** The filter with `propertyIds` as its properties, the first standing as its own (kept as it was while there is none). */
export function withMultiSourceProperties(filter: ViewFilter, propertyIds: readonly string[]): ViewFilter {
  const [first = filter.propertyId] = propertyIds;
  return { ...filter, propertyId: first, multiSource: { ...filter.multiSource, propertyIds: [...propertyIds] } };
}

/** A choice's options, and a status's groups, by name, so another source's options of the same names match (optionTargets). */
function valueByName(value: unknown, property: DbProperty): unknown {
  if (property.type !== "select" && property.type !== "multi_select" && property.type !== "status") return value;
  const options = property.type === "status" ? statusOptionsFromConfig(property.config)
    : ((property.config as { options?: { id: string; name: string }[] } | undefined)?.options ?? []);
  const groups = property.type === "status" ? statusGroupsFromConfig(property.config) : [];
  const named = (item: unknown) => [...options, ...groups].find(({ id }) => id === item)?.name ?? item;
  return Array.isArray(value) ? value.map(named) : named(value);
}

/**
 * The filter as one source reads it: that source's property in its place and
 * a choice named as its first property's options are; null where it names no
 * property of the source, which it then leaves alone. Any other filter as it is.
 */
export function filterForSource<F>(filter: F, sourceProperties: readonly DbProperty[], properties: readonly DbProperty[]): F | null {
  if (!isMultiSourceFilter(filter)) return filter;
  const own = multiSourceProperties(filter, sourceProperties)[0];
  if (!own) return null;
  if (own.id === filter.propertyId) return filter;
  const first = properties.find(({ id }) => id === filter.propertyId);
  return { ...filter, propertyId: own.id, value: first ? valueByName(filter.value, first) : filter.value };
}

// A property just picked from "다중 출처 필터링"'s list: its pill, added as any
// filter is, opens on the filter's own window rather than its condition.
const picked = new Set<string>();

/** Marks a property picked from "다중 출처 필터링" before its filter is added. */
export function pickForMultiSource(propertyId: string) {
  picked.add(propertyId);
}

/** Whether the filter on `propertyId` was just added from "다중 출처 필터링"; answers once. */
export function takeMultiSourcePick(propertyId: string): boolean {
  return picked.delete(propertyId);
}
