import type { GraphProperty, GraphRow, GraphView } from "@/contentGraph";
import { tableGrouper, tableGroupsOf } from "@/components/database/tableGrouping";
import { boardGroupPropertyId } from "@/components/database/viewLayoutDefaults";
import { projectRows } from "@/components/database/viewRowProjection";
import type { MyTasksDropGroup } from "./myTasksViewOrder";

/** The tasks a "내 작업" view moved or made (`ids`) and the values the move gives them (`values`, by task: another group's). */
export type MyTasksPlacing = { ids: readonly string[]; values?: Readonly<Record<string, GraphRow["values"]>> };

/**
 * The tasks a drop in a "내 작업" view writes down in its order, as Notion's
 * "내 작업" does (seen 2026-11-24 in Chrome, a list in one tab and a board in
 * another, its kept order read after each drop): every task the view shows
 * in the group the moved or made tasks stand in once moved — the ones never
 * moved there too, none of another group's — and every task it shows when it
 * has no groups. A board's groups are its columns, another layout's its
 * grouping's (groupBy), each as the screen draws them (tableGrouper).
 */
export function myTasksDropGroup({ view, properties, rows, placing, currentUserId, pageId, workspaceId }: {
  view: GraphView;
  properties: readonly GraphProperty[];
  rows: readonly GraphRow[];
  placing: MyTasksPlacing;
  currentUserId: string;
  pageId: string;
  workspaceId: string;
}): ReadonlySet<string> {
  const moved = new Set(placing.ids), values = placing.values ?? {};
  const asMoved = rows.map((row) => values[row.id] ? { ...row, values: { ...row.values, ...values[row.id] } } : row);
  const shown = projectRows({ currentUserId, pageId, properties, rows: asMoved, view, workspaceId });
  const groupBy = view.type === "board" ? boardGroupPropertyId(view.config, properties) : view.config.groupBy;
  const property = typeof groupBy === "string" ? properties.find(({ id }) => id === groupBy) : undefined;
  if (!property) return new Set([...shown.map(({ id }) => id), ...moved]);
  const groupsOf = tableGroupsOf(property, tableGrouper(property, { config: view.config as Record<string, unknown>, currentUserId }));
  const keysOf = (row: GraphRow) => groupsOf(row.values[property.id]?.value ?? null).map(({ key }) => key);
  const dropped = new Set(shown.filter(({ id }) => moved.has(id)).flatMap(keysOf));
  return new Set([...shown.filter((row) => keysOf(row).some((key) => dropped.has(key))).map(({ id }) => id), ...moved]);
}

/**
 * How a save reads the group a drop wrote down (MyTasksDropGroup): the tasks
 * as they show (`rows`), those made here and not yet read (`made`), with what
 * the move sets (`values`).
 */
export function myTasksDropGroupOf(context: { properties: readonly GraphProperty[]; currentUserId: string; pageId: string; workspaceId: string },
  rows: readonly GraphRow[], made: Iterable<GraphRow>, values?: MyTasksPlacing["values"]): MyTasksDropGroup {
  const madeRows = [...made];
  return (view, ids) => myTasksDropGroup({ ...context, view, placing: { ids, values },
    rows: [...new Map([...madeRows, ...rows].map((row) => [row.id, row])).values()] });
}
