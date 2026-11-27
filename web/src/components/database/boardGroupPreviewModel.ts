import type { GraphProperty, GraphView } from "@/contentGraph";
import type { FilterGroup, Page, ViewFilter } from "@/lib/types";
import type { BoardGroupPreviewFilters } from "./BoardGroupPreview";
import type { DatabaseFilterSnapshot } from "./databaseFilterSnapshot";
import type { QueryBounds } from "./query";
import { cardSubitemFilter } from "./subitemFilterScope";

/**
 * The rule that keeps a board group's rows, as Notion's group preview shows
 * it in "규칙 1개" (seen 2026-10-11: "Team 값과 동일한 데이터 Alpha"): the
 * group's option, person, page or check, or no value for the group of none.
 * A group of dates, numbers or text ranges has none.
 */
function boardGroupRule(property: { id: string; type: string }, key: string, ranged: boolean): ViewFilter | null {
  if (ranged) return null;
  if (key === "__none") return { propertyId: property.id, operator: "is_empty" };
  switch (property.type) {
    case "select":
    case "status":
      return { propertyId: property.id, operator: "equals", value: [key] };
    case "multi_select":
    case "person":
    case "created_by":
    case "last_edited_by":
    case "relation":
      return { propertyId: property.id, operator: "contains", value: [key] };
    case "checkbox":
      return { propertyId: property.id, operator: "equals", value: key === "true" };
    default:
      return null;
  }
}

function hasTerms(group: unknown): group is FilterGroup {
  const value = group as FilterGroup | undefined;
  return Array.isArray(value?.filters) && (value.filters.length > 0 || (value.groups?.length ?? 0) > 0);
}

/**
 * The table a group's preview starts as, as Notion's: the board's own
 * filters and sorts with the group's rule first among its rules, and the
 * board's sub-item option. Only what decides the rows comes along. A
 * chart's group has its rule as a filter of its own instead, "상태: 완료"
 * first in the bar (Notion's, seen 2026-11-11).
 */
export function boardGroupPreviewView(board: GraphView, rule: ViewFilter | null, ruleAsPill = false): GraphView {
  const config = board.config;
  const legacy = Array.isArray(config.filters) && config.filters.length > 0
    ? { conjunction: config.filterConjunction === "or" ? "or" as const : "and" as const, filters: config.filters as ViewFilter[] }
    : undefined;
  const own = hasTerms(config.filterGroup) ? config.filterGroup : legacy;
  const filterGroup: FilterGroup | undefined = rule && !ruleAsPill
    ? { conjunction: "and", filters: [rule], ...(own ? { groups: [own] } : {}) }
    : own;
  const quickFilters = [...(rule && ruleAsPill ? [rule] : []), ...(Array.isArray(config.quickFilters) ? config.quickFilters : [])];
  return {
    ...board, type: "table",
    config: {
      ...(filterGroup ? { filterGroup } : {}),
      ...(quickFilters.length ? { quickFilters } : {}),
      ...(Array.isArray(config.sorts) && config.sorts.length ? { sorts: config.sorts } : {}),
      ...(config.subtasks ? { subtasks: config.subtasks } : {}),
    },
  };
}

/**
 * What a board's pinned group preview filters from: its view, filters, rows
 * and bounds, and the database's own container a saved view links. None
 * without the view's filter snapshot.
 */
export function boardGroupPreviewFilters(snapshot: {
  view: GraphView; properties: readonly GraphProperty[]; dataSource: { id: string }; queryBounds: QueryBounds;
  currentUserId?: string; filterSnapshot?: DatabaseFilterSnapshot; sourceContainerId?: string;
  databaseFeatures?: { subitems?: { enabled?: unknown; parentPropertyId?: unknown } | null };
}, groupProperty: { id: string; type: string }, ranged: boolean, rows: readonly Page[], pagesById: Readonly<Record<string, Page>>,
): BoardGroupPreviewFilters | undefined {
  if (!snapshot.filterSnapshot) return undefined;
  return {
    view: snapshot.view, graphProperties: snapshot.properties, snapshot: snapshot.filterSnapshot,
    lockedFilter: cardSubitemFilter(snapshot.view, snapshot.databaseFeatures?.subitems),
    rule: (key) => boardGroupRule(groupProperty, key, ranged),
    rows, pagesById, bounds: snapshot.queryBounds, currentUserId: snapshot.currentUserId,
    save: snapshot.sourceContainerId ? { containerId: snapshot.sourceContainerId, dataSourceId: snapshot.dataSource.id } : undefined,
  };
}
