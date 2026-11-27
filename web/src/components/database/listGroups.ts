import type { GraphProperty, GraphRow } from "@/contentGraph";
import type { DbProperty, Page } from "@/lib/types";
import { arrangeViewGroups } from "./tableGroupSettingsModel";
import { tableGrouper, tableGroupsOf, type TableGroupDescriptor } from "./tableGrouping";
import { tableGroupPages, tableGroupValueReader } from "./tableGroupValues";

/**
 * A grouped list's rows in their groups, named, shown and ordered as a
 * table's (Notion's list, seen 2026-10-25): each page in the group its
 * value makes — in each, for a value of several — and its sub-items with it.
 */
export function listGroups<Row extends { id: string; subitemParentId?: string | null }>({ property, properties, config, currentUserId, rows,
  graphRows, pagesById, propsByDatabaseId }: {
  property: GraphProperty; properties: readonly GraphProperty[]; config: Record<string, unknown>; currentUserId?: string;
  rows: readonly Row[]; graphRows: Readonly<Record<string, GraphRow>>;
  pagesById?: Readonly<Record<string, Page>>; propsByDatabaseId?: Readonly<Record<string, readonly DbProperty[]>>;
}) {
  const all = Object.values(graphRows);
  const grouper = tableGrouper(property, { config, currentUserId, ...tableGroupPages(properties, all, pagesById) });
  const groupsOf = tableGroupsOf(property, grouper);
  const valueOf = tableGroupValueReader(property, properties, all, pagesById, propsByDatabaseId);
  const byId = new Map(rows.map((row) => [row.id, row]));
  const top = (row: Row) => {
    let at = row;
    for (const seen = new Set<string>(); at.subitemParentId && byId.has(at.subitemParentId) && !seen.has(at.id);) {
      seen.add(at.id);
      at = byId.get(at.subitemParentId)!;
    }
    return at;
  };
  const described = new Map<string, TableGroupDescriptor>();
  const buckets = new Map<string, Row[]>();
  for (const row of rows) {
    const graphRow = graphRows[top(row).id];
    const keys = graphRow ? groupsOf(valueOf(graphRow)).map((group) => { described.set(group.key, group); return group.key; }) : [];
    for (const key of keys.length ? keys : ["__no_group__"]) {
      const bucket = buckets.get(key);
      if (bucket) bucket.push(row);
      else buckets.set(key, [row]);
    }
  }
  return arrangeViewGroups([...buckets].map(([key, grouped]) => ({ key, rows: grouped })), {
    propertyType: property.type, propertyConfig: property.config, grouper, config, described, empty: (key) => ({ key, rows: [] as Row[] }),
  });
}
