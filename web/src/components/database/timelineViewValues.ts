import type { GraphView } from "@/contentGraph";
import type { DbProperty, DbView, Page, ViewConfig } from "@/lib/types";
import type { TimelineGroupLane } from "./timelineLaneProjection";
import type { TimelineRowLane } from "./useTimelineRowReorder";
import { groupLine } from "./viewGroupLines";

/** The view as the timeline's presentation reads it. */
export function timelinePresentationView(view: GraphView): DbView {
  return { id: view.id, databaseId: view.dataSourceId, name: view.name, type: view.type as DbView["type"], config: view.config as ViewConfig, position: 0 };
}

function valueFor(row: Page, property: DbProperty) {
  if (property.type === "title") return row.title;
  if (property.type === "created_time") return row.createdAt;
  if (property.type === "last_edited_time") return row.updatedAt;
  return row.properties?.[property.id];
}

function relationIds(value: unknown) {
  const values = Array.isArray(value) ? value : value == null ? [] : [value];
  return [...new Set(values.map((candidate) => String(candidate ?? "").trim()).filter(Boolean))];
}

/** Its bars' links read from their rows' relation values, where no dependency graph of the server's stands for them. */
export function timelineRelationEdges(items: readonly { row: Page }[], property: DbProperty) {
  return items.flatMap((item) => relationIds(valueFor(item.row, property)).map((predecessorRowId) => ({ predecessorRowId, successorRowId: item.row.id })));
}

/**
 * The rows the timeline draws in lanes — its table's, else its bars and what
 * stands for them — with their parents and groups, in lane order; grouped,
 * each named by its line (viewGroupLines.ts), a row under several groups (a
 * tag's, a person's) having a lane under each.
 */
export function timelineRowLanes(lanes: { table: readonly { row: Page; lane: number }[]; bars: readonly { row: Page; lane: number }[];
  away: readonly { row: Page; lane: number }[]; subitemLinks: readonly { row: Page; lane: number }[]; groups: readonly TimelineGroupLane[] }, table: boolean): TimelineRowLane[] {
  const groupAt = (lane: number) => lanes.groups.reduce<string | undefined>((at, group) => group.lane < lane ? group.key : at, undefined);
  const drawn = table ? lanes.table : [...lanes.bars, ...lanes.away, ...lanes.subitemLinks];
  return drawn.map(({ row, lane }) => {
    const group = lanes.groups.length ? groupAt(lane) : undefined;
    return { id: groupLine(group, row.id), lane, parentId: row.subitemParentId ?? null, group };
  }).sort((a, b) => a.lane - b.lane);
}
