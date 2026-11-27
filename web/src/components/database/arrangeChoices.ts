import { isTableGroupProperty } from "./tableGrouping";

// What a view's sort and grouping lists offer of its properties, one rule for every list:
// a column's own menu, a view's "정렬" and "그룹화 기준" and a board's "하위 그룹화".
export { sortableProperty } from "./query";

/**
 * Whether a grouping list offers the property: one a table can group by, once
 * it has something to group by — a formula once it has one to run, a rollup
 * once its relation is chosen — as Notion's (seen 2026-11-19 in Chrome 1440:
 * a new formula and a rollup with no relation missing from a table's
 * "그룹화", a board's "그룹화 기준" and "하위 그룹화" and their own column
 * menus; a formula's column menu seen so 2026-11-04). A list keeps the one
 * the view already groups by (`current`).
 */
export function offeredGroupProperty(property: { id?: string; type: string; config?: unknown } | null | undefined, current?: unknown) {
  if (!property || !isTableGroupProperty(property)) return false;
  if (current !== undefined && property.id === current) return true;
  const config = (property.config ?? {}) as { formula?: unknown; rollupRelationPropertyId?: unknown };
  if (property.type === "formula") return typeof config.formula === "string" && config.formula.trim() !== "";
  if (property.type === "rollup") return typeof config.rollupRelationPropertyId === "string" && config.rollupRelationPropertyId !== "";
  return true;
}
