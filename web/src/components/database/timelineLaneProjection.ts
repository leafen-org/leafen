import type { GraphProperty, GraphRow } from "@/contentGraph";
import type { DbProperty, Page } from "@/lib/types";
import type { TimelineHierarchyRow, TimelineRenderedRow } from "./TimelineRows";
import { tableGrouper, tableGroupsOf, type TableGroupDescriptor } from "./tableGrouping";
import { groupLine } from "./viewGroupLines";
import { tableGroupPages, tableGroupValueReader } from "./tableGroupValues";
import { tableGroupsWithCounts, tableHiddenGroupKeys, tableHidesEmptyGroups, tableKeptGroupKeys } from "./tableGroupSettingsModel";

export type TimelineGroupLane = TableGroupDescriptor & { lane: number; count: number; collapsed: boolean; rowIds: readonly string[] };
/** A row's lane: under each group its value falls in (a tag's, a person's), `line` naming which (viewGroupLines.ts). */
export type TimelineTableLane = TimelineHierarchyRow & { lane: number; line?: string };
export type TimelineBarLane = TimelineRenderedRow & { line?: string };
/** A row dated wholly past the canvas's window: its lane holds an arrow at that edge (TimelineOffRangeMarkers). */
export type TimelineAwayLane = TimelineHierarchyRow & { start: Date; end: Date; side: "start" | "end"; lane: number };
/**
 * A grouped timeline's lanes, as Notion's (seen 2026-11-01 at 1280px): a
 * group's head, then — beside a table — its columns' names, its rows, its
 * "+ 새로 만들기" and a lane's space; "+ 신규 그룹" last.
 */
export type TimelineLaneKind = "head" | "columns" | "row" | "add" | "gap" | "newGroup";
export type TimelineAddLane = { key: string; lane: number };
/** Under an open row, the line that adds a sub-item to it: on a table "+ 새 하위 항목", on the canvas a bar's ghost. */
export type TimelineSubitemAddLane = { parentId: string; depth: number; lane: number };
/** A sub-item with no date under a bar: its name beside the line from its parent (Notion's, seen 2026-11-05). */
export type TimelineSubitemLink = { row: Page; lane: number; first: number };
/** The line from a bar down to its sub-item's lane. */
export type TimelineSubitemElbow = { key: string; parentLane: number; lane: number; first: number; columns: number };

/** Notion's heights for a grouped timeline without its table, a phone's rows taller; beside one the table's rows are measured. */
export function timelineLaneHeight(kind: TimelineLaneKind, phone: boolean) {
  if (phone && (kind === "row" || kind === "add")) return 44;
  return { head: 32, columns: 32, row: 36, add: 36, gap: 36, newGroup: 38 }[kind];
}

/** A table keeps every visible row's lane, even when its date is absent or outside the range; a dated row outside it keeps its lane on the canvas too. */
export function projectTimelineLanes({ dated, away: awayRows = [], undated, hierarchy, groupProperty, properties, graphRows, pagesById, propsByDatabaseId, config, currentUserId, showTable, collapsed, adds = false, newGroup = false, subitemAddsAfter }: {
  dated: readonly TimelineRenderedRow[]; hierarchy: readonly TimelineHierarchyRow[];
  /** The dated rows wholly past the canvas's window, their lanes yet to be given. */
  away?: readonly Omit<TimelineAwayLane, "lane">[];
  undated: readonly TimelineHierarchyRow[];
  groupProperty?: GraphProperty; properties: readonly GraphProperty[]; graphRows: readonly GraphRow[];
  pagesById: Readonly<Record<string, Page>>; config: Record<string, unknown>; currentUserId?: string;
  /** The properties of the databases a rollup reads, without which its related values read none. */
  propsByDatabaseId?: Readonly<Record<string, readonly DbProperty[]>>;
  showTable: boolean; collapsed: ReadonlySet<string>;
  /** Each open group ends in its "+ 새로 만들기". */
  adds?: boolean;
  /** "+ 신규 그룹" follows the last group. */
  newGroup?: boolean;
  /** Which rows' lines an open row's sub-item line follows. */
  subitemAddsAfter?: ReadonlyMap<string, ReadonlyArray<{ parentId: string; depth: number }>>;
}) {
  const rows = hierarchy;
  const datedRows = new Map(dated.map(row => [row.row.id, row]));
  const awayById = new Map(awayRows.map(row => [row.row.id, row])), away: TimelineAwayLane[] = [];
  const subitemAdds: TimelineSubitemAddLane[] = [], subitemLinks: TimelineSubitemLink[] = [];
  const parentBarOf = (row: Page) => row.subitemParentId ? datedRows.get(row.subitemParentId) : undefined;
  const addsAfter = (rowId: string, nextLane: () => number) => {
    for (const add of subitemAddsAfter?.get(rowId) ?? []) if (showTable || datedRows.has(add.parentId)) subitemAdds.push({ ...add, lane: nextLane() });
  };
  const elbowsOf = (bars: readonly TimelineRenderedRow[]) => {
    const laneOf = new Map(bars.map(bar => [bar.row.id, bar]));
    const elbow = (key: string, parentId: string | null | undefined, lane: number, to: number | null): TimelineSubitemElbow[] => {
      const parent = parentId ? laneOf.get(parentId) : undefined;
      return parent && parent.lane < lane && (to === null || to >= parent.first)
        ? [{ key, parentLane: parent.lane, lane, first: parent.first, columns: to === null ? 0 : to - parent.first }] : [];
    };
    return [
      ...bars.flatMap(bar => elbow((bar as TimelineBarLane).line ?? bar.row.id, bar.row.subitemParentId, bar.lane, bar.first)),
      ...subitemLinks.flatMap(link => elbow(link.row.id, link.row.subitemParentId, link.lane, null)),
      ...subitemAdds.flatMap(add => elbow(`add:${add.parentId}`, add.parentId, add.lane, null)),
    ];
  };
  if (!groupProperty && !showTable) {
    let at = 0;
    const bars: TimelineRenderedRow[] = [];
    for (const item of rows) {
      const bar = datedRows.get(item.row.id), parent = parentBarOf(item.row);
      const far = awayById.get(item.row.id);
      if (bar) bars.push({ ...bar, lane: at++ });
      else if (far) away.push({ ...far, lane: at++ });
      else if (parent) subitemLinks.push({ row: item.row, lane: at++, first: parent.first });
      addsAfter(item.row.id, () => at++);
    }
    return { bars, away, table: [], groups: [], kinds: [], addLanes: [], newGroupLane: undefined, laneCount: 0, undated, subitemAdds, subitemLinks, subitemElbows: elbowsOf(bars) };
  }
  const byId = new Map(graphRows.map(row => [row.id, row]));
  const readValue = groupProperty ? tableGroupValueReader(groupProperty, properties, graphRows, pagesById, propsByDatabaseId) : undefined;
  const grouper = groupProperty ? tableGrouper(groupProperty, { config, currentUserId, ...tableGroupPages(properties, graphRows, pagesById) }) : undefined;
  // A row goes under each group its value falls in, as a table's (tableGroupsOf: a tag's, a person's; Notion's, seen 2026-11-24).
  const groupsOf = grouper && groupProperty ? tableGroupsOf(groupProperty, grouper) : undefined;
  const values = rows.map(row => { const graphRow = byId.get(row.row.id); return graphRow && readValue ? readValue(graphRow) : undefined; });
  const descriptors = groupsOf && groupProperty ? tableGroupsWithCounts(groupProperty, values, config, groupsOf) : [];
  const hidden = tableHiddenGroupKeys(config), kept = tableKeptGroupKeys(config);
  const rowsByGroup = new Map<string, TimelineHierarchyRow[]>();
  rows.forEach((row, index) => { for (const { key } of groupsOf ? groupsOf(values[index]) : [{ key: "all" }]) { const current = rowsByGroup.get(key); if (current) current.push(row); else rowsByGroup.set(key, [row]); } });
  const rowGroups = grouper ? descriptors.filter(group => !hidden.has(group.key) && (group.count > 0 || !tableHidesEmptyGroups(config) || kept.has(group.key)))
    : [{ key: "all", count: rows.length }];
  const datedById = new Map(dated.map(row => [row.row.id, row]));
  const bars: TimelineBarLane[] = [], table: TimelineTableLane[] = [], groups: TimelineGroupLane[] = [];
  const kinds: TimelineLaneKind[] = [], addLanes: TimelineAddLane[] = [];
  const visibleIds = new Set<string>();
  const lane = (kind: TimelineLaneKind) => kinds.push(kind) - 1;
  for (const group of rowGroups) {
    const groupRows = rowsByGroup.get(group.key) ?? [];
    const isCollapsed = Boolean(grouper && collapsed.has(group.key));
    if (grouper) groups.push({ ...group as TableGroupDescriptor & { count: number }, lane: lane("head"), collapsed: isCollapsed, rowIds: groupRows.map(item => item.row.id) });
    if (isCollapsed) continue;
    if (grouper && showTable) lane("columns");
    groupRows.forEach(item => visibleIds.add(item.row.id));
    for (const item of groupRows) {
      const bar = datedById.get(item.row.id), far = awayById.get(item.row.id), parent = parentBarOf(item.row), line = grouper ? groupLine(group.key, item.row.id) : undefined;
      if (showTable) {
        const at = lane("row");
        table.push({ ...item, lane: at, line });
        if (bar) bars.push({ ...bar, lane: at, line });
        else if (far) away.push({ ...far, lane: at });
        else if (parent) subitemLinks.push({ row: item.row, lane: at, first: parent.first });
      } else if (bar) bars.push({ ...bar, lane: lane("row"), line });
      else if (far) away.push({ ...far, lane: lane("row") });
      else if (parent) subitemLinks.push({ row: item.row, lane: lane("row"), first: parent.first });
      addsAfter(item.row.id, () => lane("row"));
    }
    if (grouper && adds) addLanes.push({ key: group.key, lane: lane("add") });
    if (grouper && showTable) lane("gap");
  }
  const newGroupLane = grouper && newGroup ? lane("newGroup") : undefined;
  return { bars, away, table, groups, kinds, addLanes, newGroupLane, laneCount: kinds.length, subitemAdds, subitemLinks, subitemElbows: elbowsOf(bars), undated: undated.filter(item => visibleIds.has(item.row.id)) };
}

/** A timeline's lines by group, which its copies are placed among (copiesPlace): each group's rows under it, else its rows (`rowIds`). */
export function timelineGroupLines(groups: readonly TimelineGroupLane[], rowIds: readonly string[]): string[][] {
  return groups.length ? groups.map((group) => group.rowIds.map((id) => groupLine(group.key, id))) : [[...rowIds]];
}
