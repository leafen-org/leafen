import type { DbProperty } from "@/lib/types";

// The property kinds an automation edits, as Notion's lists (seen 2026-11-03).
const EDITABLE_TYPES = new Set([
  "title",
  "rich_text",
  "number",
  "select",
  "multi_select",
  "status",
  "date",
  "person",
  "checkbox",
  "url",
  "email",
  "phone",
  "place",
  "verification",
  // A relation is set to rows of its database, as Notion's "속성 편집" lists it
  // (seen 2026-10-06); rollups and formulas are computed and are not.
  "relation",
]);

export function isDatabaseAutomationEditableProperty(property: DbProperty) {
  return EDITABLE_TYPES.has(property.type);
}

/**
 * What a condition or "속성 편집" offers, as Notion's lists (seen 2026-11-03,
 * a database of every kind): the editable kinds but a place. A condition or
 * edit already made on another kind still shows and runs.
 */
export function isDatabaseAutomationListedProperty(property: DbProperty) {
  return EDITABLE_TYPES.has(property.type) && property.type !== "place";
}
