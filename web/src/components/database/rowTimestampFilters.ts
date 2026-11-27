import type { DbProperty, FilterGroup, ViewFilter } from "@/lib/types";
import { filterGroupTerms, type FilterTerm } from "./filterGroupTerms";

/**
 * When a row's page was created or last edited, which a view's filter may
 * name without a property, as Notion's API sets one ({"timestamp":
 * "created_time", ...}) and the server keeps it under the query's own
 * timestamp properties (database_query_row_timestamps.go). Notion's view
 * keeps its rows by such a rule and counts it in its "규칙 N개", but shows no
 * row for it in its filter (seen 2026-11-11); so does this one.
 */
const ROW_TIMESTAMP_FILTER_PROPERTIES: readonly DbProperty[] = [
  { id: "leafen:row-created-time", type: "created_time" },
  { id: "leafen:row-last-edited-time", type: "last_edited_time" },
].map(({ id, type }) => ({
  id, type, name: type, databaseId: "", position: 0, createdAt: "", updatedAt: "",
} as unknown as DbProperty));

const rowTimestampIds = new Set(ROW_TIMESTAMP_FILTER_PROPERTIES.map(({ id }) => id));

/** Whether a rule names a row's page timestamp rather than a property. */
export function isRowTimestampFilter(filter: Pick<ViewFilter, "propertyId">): boolean {
  return rowTimestampIds.has(filter.propertyId);
}

/** The properties a view's filter reads its rows by: its own and the page timestamps. */
export function withRowTimestampFilterProperties<P>(byId: Map<string, P>): Map<string, P> {
  const all = new Map(byId);
  for (const property of ROW_TIMESTAMP_FILTER_PROPERTIES) {
    if (!all.has(property.id)) all.set(property.id, property as unknown as P);
  }
  return all;
}

/** A group's rules and groups as its filter shows them: those on a page timestamp left out. */
export function shownFilterGroupTerms(group: FilterGroup): FilterTerm[] {
  return filterGroupTerms(group).filter((term) => term.kind === "group" || !isRowTimestampFilter(term.filter));
}
