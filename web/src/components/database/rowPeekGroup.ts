import type { GraphProperty, GraphRow, GraphView } from "@/contentGraph";
import { boardSubgroupConfig } from "./boardSubgroupModel";
import { tableGrouper, tableGroupsOf } from "./tableGrouping";
import { tableGroupPages, tableGroupValueReader } from "./tableGroupValues";

/**
 * The shown rows of the open row's group — and of its sub-group, on a board
 * split again — in the order they show, which a peek's ∧ and ∨ (and ⌃⇧K,
 * ⌃⇧J) step through, as Notion's do (seen 2026-11-26 in two tabs: a board
 * by status, a card alone in its column has ∧ and ∨ both faded; "내 작업"
 * grouped by status, the keys stop at either end of a group and never go on
 * into the next). A row in several groups (tags, people) steps through the
 * first of them; a view that groups by nothing, all its rows.
 */
export function rowsOfItsGroup(shown: readonly GraphRow[], rowId: string, { properties, view, currentUserId, rows }: {
  properties: readonly GraphProperty[]; view: GraphView; currentUserId?: string; rows: readonly GraphRow[];
}): readonly GraphRow[] {
  const groupings = [{ id: view.config.groupBy, config: view.config }];
  if (view.type === "board") groupings.push({ id: view.config.subGroupBy, config: boardSubgroupConfig(view.config) });
  let kept = shown;
  for (const { id, config } of groupings) {
    const property = typeof id === "string" ? properties.find((candidate) => candidate.id === id) : undefined;
    if (property) kept = sameGroup(kept, rowId, property, { properties, config, currentUserId, rows });
  }
  return kept;
}

function sameGroup(shown: readonly GraphRow[], rowId: string, property: GraphProperty, { properties, config, currentUserId, rows }: {
  properties: readonly GraphProperty[]; config: Record<string, unknown>; currentUserId?: string; rows: readonly GraphRow[];
}): readonly GraphRow[] {
  const groupsOf = tableGroupsOf(property, tableGrouper(property, { config, currentUserId, ...tableGroupPages(properties, rows) }));
  const valueOf = tableGroupValueReader(property, properties, rows);
  const byId = new Map(shown.map((row) => [row.id, row]));
  // A sub-item goes in its top row's group, as a grouped view shows it (listGroups).
  const top = (row: GraphRow) => {
    let at = row;
    for (const seen = new Set<string>(); at.subitemParentId && byId.has(at.subitemParentId) && !seen.has(at.id);) {
      seen.add(at.id);
      at = byId.get(at.subitemParentId)!;
    }
    return at;
  };
  const keysOf = (row: GraphRow) => groupsOf(valueOf(top(row))).map((group) => group.key);
  const open = byId.get(rowId);
  if (!open) return shown;
  const key = keysOf(open)[0];
  return shown.filter((row) => keysOf(row).includes(key));
}
