import type { GraphProperty, GraphValueType, GraphView } from "@/contentGraph";
import type { PropertyMarkName } from "@/components/database/PropertyIcon";
import type { FilterGroup, ViewFilter } from "@/lib/types";
import { pageNamesNow } from "@/lib/pageNames";
import { effectiveFilterGroup, matchesFilterGroup } from "@/shared/database/queryFilterGroups";
import type { QueryAdapters, QueryProperty } from "@/shared/database/queryTypes";
import type { Page } from "@/types";
import type { LibraryColumn } from "./libraryModel";

/**
 * The library's filters, as Notion's library — a database view — has them
 * (seen 2026-10-02): its "필터 추가" offers the page name, the last visit,
 * the last edit, the last editor, the creation and the creator, a chosen
 * one is a database filter pill under the tab row, and "고급 필터 추가"
 * starts a database's advanced filter. Leafen keeps them with the view's
 * other settings and applies them with the databases' filter engine.
 */

export type LibraryQuickFilter = ViewFilter | FilterGroup;

/** The library's own data source, apart from every database's. */
const LIBRARY_DATA_SOURCE = "leafen-library";

/** The columns a filter reads, in the order Notion's "필터 추가" lists them. */
export const LIBRARY_FILTER_COLUMNS = ["name", "visited", "edited", "lastEditor", "created", "creator"] as const;
export type LibraryFilterColumn = typeof LIBRARY_FILTER_COLUMNS[number];

const FILTER_TYPES: Record<LibraryFilterColumn, GraphValueType> = {
  name: "title",
  // A visit is a moment, filtered as a creation time is.
  visited: "created_time",
  edited: "last_edited_time",
  lastEditor: "last_edited_by",
  created: "created_time",
  creator: "created_by",
};

export function isLibraryFilterColumn(column: string): column is LibraryFilterColumn {
  return (LIBRARY_FILTER_COLUMNS as readonly string[]).includes(column);
}

/** A column's filled mark (PropertyMark): a page for the name, a person for its people, a clock for its times. */
export function libraryColumnMark(column: LibraryColumn): PropertyMarkName {
  if (column === "name") return "page";
  return column === "creator" || column === "lastEditor" ? "person" : "clock";
}

/**
 * The columns as properties a database filter reads, each with its head's
 * mark, so the filter and sort lists, the pills and the phone's sheets draw
 * it as the table's head does.
 */
export function libraryFilterProperties(label: (column: LibraryColumn) => string): GraphProperty[] {
  return LIBRARY_FILTER_COLUMNS.map((column, index) => ({
    id: column,
    dataSourceId: LIBRARY_DATA_SOURCE,
    type: FILTER_TYPES[column],
    name: label(column),
    position: String(index).padStart(4, "0"),
    config: { mark: libraryColumnMark(column) },
  }));
}

/** A library view as the filter bar reads one: its quick filters and its advanced filter. */
export function libraryFilterView(
  id: string, name: string, filters: readonly LibraryQuickFilter[], filterGroup: FilterGroup | null = null,
  sorts: readonly { column: string; direction: "ascending" | "descending" }[] = [],
): GraphView {
  return {
    id: `library:${id}`,
    dataSourceId: LIBRARY_DATA_SOURCE,
    type: "table",
    name,
    position: "",
    config: { quickFilters: filters.length ? [...filters] : undefined, ...(filterGroup ? { filterGroup } : {}),
      ...(sorts.length ? { sorts: sorts.map(({ column, direction }) => ({ propertyId: column, direction: direction === "ascending" ? "asc" : "desc" })) } : {}) },
  };
}

/** The pages every filter keeps; one still waiting for a value keeps all. */
export function filterLibraryPages(
  pages: readonly Page[],
  filters: readonly LibraryQuickFilter[],
  keys: {
    visited(page: Page): string;
    /** A person's name, for a person filter's text. */
    personName(userId: string): string;
    currentUserId: string;
    now?: Date;
    timeZone?: string;
  },
  filterGroup: FilterGroup | null = null,
): Page[] {
  const properties: QueryProperty[] = LIBRARY_FILTER_COLUMNS.map((column) => ({ id: column, type: FILTER_TYPES[column] }));
  const byId = new Map(properties.map((property) => [property.id, property]));
  const group = effectiveFilterGroup({ quickFilters: [...filters], ...(filterGroup ? { filterGroup } : {}) }, byId);
  if (!group) return [...pages];
  const value = (page: Page, column: string): unknown => {
    if (column === "name") return pageNamesNow.title(page);
    if (column === "visited") return keys.visited(page) || null;
    if (column === "edited") return page.updatedAt || null;
    if (column === "created") return page.createdAt || null;
    if (column === "creator") return page.createdBy || null;
    if (column === "lastEditor") return page.lastEditedBy || null;
    return null;
  };
  const text = (input: unknown, property?: QueryProperty) => {
    if (typeof input !== "string") return "";
    return property && (property.type === "created_by" || property.type === "last_edited_by") ? keys.personName(input) : input;
  };
  const adapters: QueryAdapters = {
    cellValue: (row, property) => value(row as Page, property.id),
    displayText: (row, property) => text(value(row as Page, property.id), property),
    asText: (input) => (typeof input === "string" ? input : ""),
    personIds: (input) => (typeof input === "string" && input ? [input] : []),
    rollupTargetIds: () => [],
    currentUserId: keys.currentUserId,
    timeZone: keys.timeZone,
    referenceTime: keys.now ?? new Date(),
  };
  return pages.filter((page) => matchesFilterGroup(page, group, adapters, byId));
}
