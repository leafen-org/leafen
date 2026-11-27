import type { GraphProperty } from "@/contentGraph";
import { graphPositionBetween } from "@/lib/graphPosition";
import type { DbProperty, Page, PropertyConfig } from "@/lib/types";
import { valueAsRead } from "./valueAsRead";
import { verificationPropertyValue } from "./structuredPropertyValues";
import { comparePropertyNames } from "./viewPropertyOrder";

// A property's place in its database's own page order, once the database has one.
const PAGE_POSITION = "pagePosition";

function pagePosition(property: { config?: unknown }) {
  const position = (property.config as Record<string, unknown> | undefined)?.[PAGE_POSITION];
  return typeof position === "string" && position ? position : undefined;
}

/**
 * A page's properties in the order Notion's page lists them where its
 * layout sets none: the database's own page order where it has one, those
 * made since after it; by name where it has none — "A0" before "Due",
 * "Alpha" before "Zeta", Latin before Hangul — whatever order they were made
 * in or a table shows, its columns moved or not, in any of its views (seen
 * 2026-11-03, and 2026-11-08 in side, center and full views); a Verification
 * and its Owner first.
 */
export function rowPagePropertyOrder<T extends { property: { id: string; type?: string; name: string; config?: unknown } }>(entries: readonly T[]): T[] {
  // A Verification and the Owner it names lead the others, in that order,
  // whatever their names (Notion, seen 2026-11-11: "Verification · Owner ·
  // Zeta", the Zeta made before them).
  const owners = new Set<string>();
  for (const { property } of entries) {
    const owner = property.type === "verification" ? (property.config as { ownerPropertyId?: unknown } | undefined)?.ownerPropertyId : undefined;
    if (typeof owner === "string" && owner) owners.add(owner);
  }
  const lead = (property: T["property"]) => property.type === "verification" && owners.size ? 0 : owners.has(property.id) ? 1 : 2;
  return [...entries].sort(({ property: a }, { property: b }) => {
    const left = pagePosition(a), right = pagePosition(b);
    if (left !== right) return left === undefined ? 1 : right === undefined ? -1 : left < right ? -1 : 1;
    if (lead(a) !== lead(b)) return lead(a) - lead(b);
    return comparePropertyNames(a.name, b.name);
  });
}

/**
 * A duplicated property, and the others its database's page order sets: the
 * copy right after its source. A database without a page order takes one
 * then, from the table's columns (`columns`) when a column's menu duplicated
 * it, else from the page's lines, as Notion's does (seen 2026-11-08: a
 * column's "속성 복제" gave its pages the table's order and a page's its
 * own; moving columns afterwards left it).
 */
// A property with its place in the database's page order, or without one.
function placed(property: GraphProperty, position?: string): GraphProperty {
  const { [PAGE_POSITION]: _kept, ...config } = property.config;
  return { ...property, config: position ? { ...config, [PAGE_POSITION]: position } : config };
}

// The n-th place of a page order written whole.
const nthPosition = (index: number) => `a${String(index).padStart(8, "0")}`;

export function propertiesAfterDuplicate(
  properties: readonly GraphProperty[], sourceId: string, copy: GraphProperty, columns?: readonly string[],
): GraphProperty[] {
  const others = properties.filter((property) => property.id !== copy.id);
  const listed = rowPagePropertyOrder(others.filter((property) => pagePosition(property)).map((property) => ({ property })))
    .map(({ property }) => property);
  if (listed.length) {
    const source = listed.findIndex((property) => property.id === sourceId);
    // A source made since the order was set is after it, and so is its copy.
    return [source < 0 ? placed(copy) : placed(copy, graphPositionBetween(pagePosition(listed[source]), pagePosition(listed[source + 1])))];
  }
  // The title is no line of a page: the order is its lines', which a line
  // moved writes again from the first place (propertiesAfterPageMove), so
  // the title holding a place would share it with the first line.
  const lines = others.filter((property) => property.type !== "title");
  const ids = new Set(lines.map((property) => property.id));
  const shown = (columns ?? rowPagePropertyOrder(lines.map((property) => ({ property }))).map(({ property }) => property.id))
    .filter((id) => ids.has(id));
  const at = shown.includes(sourceId) ? shown.indexOf(sourceId) + 1 : shown.length;
  const order = [...shown.slice(0, at), copy.id, ...shown.slice(at)];
  const rest = rowPagePropertyOrder(lines.filter((property) => !order.includes(property.id)).map((property) => ({ property })));
  const byId = new Map([...lines, copy].map((property) => [property.id, property]));
  return [...order, ...rest.map(({ property }) => property.id)]
    .map((id, index) => placed(byId.get(id)!, nthPosition(index)));
}

/**
 * A property put first in its database's page order where the database has
 * none yet, the others after it by name; where it has one, as it was. A
 * status first shown as a checkbox on its pages takes it, as Notion's (seen
 * 2026-11-16 in iOS Safari and twice in Chrome: "가 · 날 · 상" became "상 · 가
 * · 날"; with a page order set by a drag, the status stayed where it was).
 */
export function propertyFirstInUnorderedPages<T extends { config: Record<string, unknown> }>(
  property: T, properties: readonly { config?: unknown }[],
): T {
  if (properties.some((candidate) => pagePosition(candidate) !== undefined)) return property;
  return { ...property, config: { ...property.config, [PAGE_POSITION]: nthPosition(0) } };
}

/**
 * The properties a page's property line dragged before or after another
 * rewrites: `listed`, the page's lines in their order, with the moved one in
 * its new place become the database's own page order, so every page of the
 * database lists them so, its tables' columns staying as they are (Notion's,
 * seen 2026-11-08). Only those whose place changes are answered.
 */
export function propertiesAfterPageMove(
  listed: readonly GraphProperty[], movedId: string, targetId: string, side: "before" | "after",
): GraphProperty[] {
  const moved = listed.find((property) => property.id === movedId);
  const rest = listed.filter((property) => property.id !== movedId);
  const at = rest.findIndex((property) => property.id === targetId);
  if (!moved || at < 0) return [];
  const order = [...rest.slice(0, side === "before" ? at : at + 1), moved, ...rest.slice(side === "before" ? at : at + 1)];
  return order.flatMap((property, index) => pagePosition(property) === nthPosition(index) ? [] : [placed(property, nthPosition(index))]);
}

const HIDE_WHEN_EMPTY_UNSUPPORTED = new Set<DbProperty["type"]>([
  "formula",
  "rollup",
  "unique_id",
  "created_time",
  "last_edited_time",
  "created_by",
  "last_edited_by",
]);

export function canHideWhenEmpty(prop: DbProperty) {
  return !HIDE_WHEN_EMPTY_UNSUPPORTED.has(prop.type);
}

// The values an editable database page or template says "비어 있음" for when
// empty, as Notion's; a checkbox and a button have no empty.
export const ROW_PROPERTY_EMPTY_HINT_TYPES: ReadonlySet<string> = new Set([
  "rich_text", "number", "select", "multi_select", "status", "date", "person", "files", "url", "email",
  "phone", "relation", "place", "verification", "location", "last_visited_time",
]);

/**
 * A formula or rollup, whose "비어 있음" follows what it shows, as Notion's
 * (seen 2026-10-30: "0" alone, "비어 있음" for a result with nothing to
 * show): the hint is drawn with it (`data-computed-hint`) and stays only
 * after a cell showing nothing (`data-computed-empty`) or where its value drew
 * nothing (DatabaseRowPage.module.css).
 */
export function isComputedProperty(prop: { type: string }) {
  return prop.type === "formula" || prop.type === "rollup";
}

export function isEmptyRowProperty(row: Page, prop: DbProperty) {
  if (prop.type === "title") return row.title.length === 0;
  if (prop.type === "created_time") return !row.createdAt;
  if (prop.type === "last_edited_time") return !row.updatedAt;
  if (prop.type === "created_by") return !row.createdBy;
  if (prop.type === "last_edited_by") return !row.lastEditedBy;
  // Its stored value is none: what it shows decides (isComputedProperty).
  if (isComputedProperty(prop)) return false;
  // An ID is never empty: its number, or "..." while it comes (uniqueIdText).
  if (prop.type === "unique_id") return false;
  const value = valueAsRead(row, prop);
  // An unverified page's verification is empty, as Notion's (seen 2026-10-15).
  if (prop.type === "verification") return (verificationPropertyValue(value)?.state ?? "unverified") === "unverified";
  if (value == null) return true;
  if (Array.isArray(value)) return value.length === 0;
  if (typeof value === "string") return value.trim().length === 0;
  return false;
}

export function isHiddenRowProperty(row: Page, prop: DbProperty) {
  return (
    !!prop.config?.hideInPagePanel ||
    (canHideWhenEmpty(prop) && !!prop.config?.hideWhenEmpty && isEmptyRowProperty(row, prop))
  );
}

export function clonePropertyConfig(config?: PropertyConfig) {
  return config ? (JSON.parse(JSON.stringify(config)) as PropertyConfig) : undefined;
}

// A property's value takes a comment, as Notion's menu offers "댓글" only
// on a value (seen 2026-10-01); computed and system values and a button
// take none. A checkbox has no empty: checked or not, its name's menu
// offers "댓글" (seen 2026-11-12 on a full page).
const COMMENTLESS_PROPERTY_TYPES = new Set<DbProperty["type"]>([
  "formula",
  "rollup",
  "unique_id",
  "button",
  "created_time",
  "last_edited_time",
  "created_by",
  "last_edited_by",
]);

export function canCommentOnProperty(row: Page, prop: DbProperty) {
  return prop.type !== "title" && !COMMENTLESS_PROPERTY_TYPES.has(prop.type)
    && (prop.type === "checkbox" || !isEmptyRowProperty(row, prop));
}

export function canDuplicateProperty(prop: DbProperty) {
  // A database verifies its pages with one Verification property; an ID is the
  // database's one numbering, and Notion's menu offers no copy (seen 2026-10-11).
  return prop.type !== "title" && prop.type !== "relation" && prop.type !== "verification" && prop.type !== "unique_id";
}
