import type { GraphProperty, GraphRow } from "@/contentGraph";
import type { DbProperty, Page } from "@/lib/types";
import type { BoardOption } from "./boardViewModel";
import { orderTableGroups, propertyAlwaysValued, tableKeptGroupKeys } from "./tableGroupSettingsModel";
import { keptGroupSeed } from "./viewGroupAddKind";
import { isTableGroupProperty, NO_TABLE_GROUP, noValueGroupLeads, tableGroupSeedValue, tableGrouper, tableGroupsOf, type TableGroupDescriptor } from "./tableGrouping";
import { tableGroupPages, tableGroupValueReader } from "./tableGroupValues";
import { dateGroupDropValue } from "./tableDateGroups";
import type { GroupPage } from "./GroupPageGlyph";

const OPTION_TYPES = new Set(["select", "multi_select", "status", "person", "checkbox"]);
// Values worked out, not written: a card keeps them wherever it is put.
const WORKED_OUT = new Set(["formula", "created_time", "last_edited_time"]);

/**
 * Whether a board groups by what its cards hold, as a table's groups do —
 * a title, text, number, date, formula or relation, as Notion's board
 * lists them (seen 2026-10-11) — rather than by its property's options,
 * its people or a box.
 */
export function isBoardValueGroupProperty(property: { type: string } | null | undefined) {
  return !!property && isTableGroupProperty(property) && !OPTION_TYPES.has(property.type);
}

export type BoardValueGroups = {
  /** The groups its cards make, in order, without the one for no value. */
  options: Array<BoardOption & { page?: GroupPage }>;
  /** The groups a card is in, none when it has no value. */
  keysOf(row: GraphRow): string[];
  /**
   * The value a card takes in a group, made there or moved there from
   * another; undefined where the group stands for no one value.
   */
  valueFor(targetKey: string | null, row?: GraphRow, sourceKey?: string | null): unknown;
  /** Whether its group for no value comes first, as on a table (noValueGroupLeads). */
  emptyFirst: boolean;
  /**
   * Whether a group stands for cards without a value: none by when or by
   * whom a card was made or last edited, which every card has
   * (propertyAlwaysValued), unless a card comes without one.
   */
  emptyGroup: boolean;
};

function pageIdOf(key: string | null | undefined) {
  return key?.startsWith("page:") ? key.slice("page:".length) : null;
}

/**
 * A board's groups by such a property, as a table's: named and ordered as
 * the table's (text and related pages A to Z, numbers and dates in order),
 * a card under each page its relation links, as Notion's board (seen
 * 2026-10-11). A card moved from one related page's group to another's
 * swaps that one link and keeps the rest, one moved to a date's column has
 * its date brought into the column's days; one made under a title's group
 * is titled with it, as Notion's.
 */
export function boardValueGroups(property: GraphProperty, rows: readonly GraphRow[], context: {
  config: Record<string, unknown>;
  properties: readonly GraphProperty[];
  pagesById?: Readonly<Record<string, Page>>;
  /** The properties of the databases a rollup reads, without which its related values read none. */
  propsByDatabaseId?: Readonly<Record<string, readonly DbProperty[]>>;
  currentUserId?: string;
}): BoardValueGroups {
  const read = tableGroupValueReader(property, context.properties, rows, context.pagesById, context.propsByDatabaseId);
  const grouper = tableGrouper(property, {
    config: context.config, currentUserId: context.currentUserId,
    ...tableGroupPages(context.properties, rows, context.pagesById),
  });
  const groupsOf = tableGroupsOf(property, grouper);
  const described = new Map<string, TableGroupDescriptor>();
  const keysOf = (row: GraphRow) => groupsOf(read(row)).flatMap((group) => {
    if (group.key === NO_TABLE_GROUP) return [];
    described.set(group.key, group);
    return [group.key];
  });
  rows.forEach(keysOf);
  // A text's or related page's group added from "+ 신규 그룹" stands though no card holds it, as Notion's.
  for (const key of tableKeptGroupKeys(context.config)) {
    const seed = described.has(key) ? undefined : keptGroupSeed(property.type, key);
    const group = seed === undefined ? undefined : groupsOf(seed)[0];
    if (group?.key === key) described.set(key, group);
  }
  const options = orderTableGroups([...described.values()], context.config, (key) => described.get(key), property.type)
    .map((group) => ({ id: group.key, name: group.label, color: "", page: group.page }));
  function valueFor(targetKey: string | null, row?: GraphRow, sourceKey?: string | null) {
    if (WORKED_OUT.has(property.type)) return undefined;
    if (property.type === "relation") {
      const current = (row ? read(row) : []) as unknown;
      const links = (Array.isArray(current) ? current : []).map(String);
      const source = pageIdOf(sourceKey);
      const target = pageIdOf(targetKey);
      return [...new Set([...links.filter((id) => id !== source), ...(target ? [target] : [])])];
    }
    if (targetKey === null) return property.type === "title" ? "" : null;
    // A card moved to a date's column has its date brought into the column's days (dateGroupDropValue).
    if (property.type === "date" && row) return dateGroupDropValue(targetKey, read(row));
    return tableGroupSeedValue(property, targetKey, context.config);
  }
  return { options, keysOf, valueFor, emptyFirst: noValueGroupLeads(context.config, property.type), emptyGroup: !propertyAlwaysValued(property.type) };
}
