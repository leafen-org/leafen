import type { GraphProperty } from "@/contentGraph";

/**
 * Notion's "속성 사용 권한" (its help "Database property access", read
 * 2026-10-12) as the core shows it: a restricted property's lock, a value
 * someone may not see, and the request that opens the organization module's
 * rule window from a column's menu. The server keeps the rules and leaves
 * out what a reader may not see.
 */

export type PropertyAccessLevel = "inherit" | "edit" | "edit_values" | "view" | "view_property" | "none";

export const OPEN_PROPERTY_ACCESS_EVENT = "leafen:open-property-access";

export type OpenPropertyAccessDetail = { dataSourceId: string; propertyId: string };

export function requestPropertyAccessDialog(detail: OpenPropertyAccessDetail) {
  window.dispatchEvent(new CustomEvent<OpenPropertyAccessDetail>(OPEN_PROPERTY_ACCESS_EVENT, { detail }));
}

/** Kinds no rule may keep, as Notion's: the title, IDs, who made or last edited a row and when, and sub-items. */
export function propertyAccessRestrictable(property: Pick<GraphProperty, "id" | "type">) {
  if (["title", "unique_id", "created_by", "created_time", "last_edited_by", "last_edited_time"].includes(property.type)) return false;
  return !(property.type === "relation" && property.id.startsWith("subitems-"));
}
