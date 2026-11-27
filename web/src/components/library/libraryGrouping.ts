import type { GraphProperty, GraphValueType } from "@/contentGraph";
import { compareTableGroups, tableGrouper, type TableGroupDescriptor } from "@/components/database/tableGrouping";
import type { Page } from "@/types";
import type { LibraryColumn } from "./libraryModel";

/**
 * The library grouped by a column, as a column menu's "그룹화" groups
 * Notion's library — a database view — (seen 2026-10-02): its groups are a
 * database table's, a name or a person by their text with the empty group
 * first, a time by how far it is from today ("오늘", "어제", "지난 날짜"...).
 * The source has no grouping, as its menu has none.
 */

export type LibraryGroupColumn = Exclude<LibraryColumn, "source">;
export type LibraryGroup = { group: TableGroupDescriptor; pages: Page[] };

const GROUP_TYPES: Record<LibraryGroupColumn, GraphValueType> = {
  name: "title",
  creator: "rich_text",
  lastEditor: "rich_text",
  edited: "last_edited_time",
  visited: "created_time",
  created: "created_time",
};

export function isLibraryGroupColumn(column: string): column is LibraryGroupColumn {
  return Object.hasOwn(GROUP_TYPES, column);
}

/** The pages in their groups, the groups in a database table's order and each group's pages in theirs. */
export function groupLibraryPages(
  pages: readonly Page[],
  column: LibraryGroupColumn,
  label: string,
  keys: { visited(page: Page): string; creator(page: Page): string; lastEditor(page: Page): string; currentUserId: string; today?: Date },
): LibraryGroup[] {
  const property: GraphProperty = {
    id: column, dataSourceId: "leafen-library", type: GROUP_TYPES[column], name: label, position: "", config: {},
  };
  const grouper = tableGrouper(property, { config: {}, currentUserId: keys.currentUserId, today: keys.today });
  const value = (page: Page) => {
    if (column === "name") return page.title.trim();
    if (column === "creator") return keys.creator(page);
    if (column === "lastEditor") return keys.lastEditor(page);
    if (column === "edited") return page.updatedAt;
    if (column === "created") return page.createdAt;
    return keys.visited(page);
  };
  const groups = new Map<string, LibraryGroup>();
  for (const page of pages) {
    const group = grouper(value(page) || null);
    const existing = groups.get(group.key);
    if (existing) existing.pages.push(page);
    else groups.set(group.key, { group, pages: [page] });
  }
  return [...groups.values()].sort((left, right) => compareTableGroups(left.group, right.group));
}
