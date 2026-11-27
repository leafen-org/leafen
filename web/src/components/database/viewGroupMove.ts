import type { GraphProperty, GraphRow } from "@/contentGraph";
import { dateGroupDropValue } from "./tableDateGroups";
import { isNoValueGroupKey, tableGroupSeedValue } from "./tableGrouping";

// Values a person sets, which a row dropped in its view's group for no value
// has cleared, as Notion's (seen 2026-11-24 in Chrome at 1685, dpr 1, a
// grouped table, list, gallery and timeline: a select's, tags', people's, a
// date's and a text's; a status reads its default and has no such group).
const CLEARED = new Set(["select", "multi_select", "status", "person", "relation", "number", "date", "title", "rich_text", "url", "email", "phone"]);
// Lists, whose rows stand in a group for each of their members.
const LISTS = new Set(["multi_select", "person", "relation"]);
const memberOf = (member: unknown) => typeof member === "string" ? member : (member as { id?: unknown } | null)?.id;

/**
 * A list's value (tags, people, related pages) moved from the group of one
 * member (`leaving`) to that of another (`joining`): that one swapped and the
 * rest kept, as Notion's (seen 2026-11-24: x and z dragged from x to y were
 * z and y; x and y dragged from x to y, y alone).
 */
export function swappedMember(current: unknown, leaving: unknown, joining: unknown): unknown[] {
  const kept = (Array.isArray(current) ? current : []).filter((member) => memberOf(member) !== leaving && memberOf(member) !== joining);
  return [...kept, joining];
}

/** A board card's tags or people given the column `value` names, from the column it was dragged from (`from`): that member swapped (swappedMember). */
export function boardListSwap(type: string, current: unknown, from: string | null, value: unknown) {
  return (type === "multi_select" || type === "person") && from !== null && Array.isArray(value) && value.length === 1 ? swappedMember(current, from, value[0]) : value;
}

/**
 * The value a row dropped in a view's group `to` takes, from the group it
 * stood in (`from`) and its value now (`current`): none in the group for no
 * value (CLEARED), a list's member swapped (swappedMember), a date brought
 * into the group's days (dateGroupDropValue), else the group's own value
 * (tableGroupSeedValue); undefined where it takes none — a value worked out
 * (a formula's, when a row was made or edited), a group standing for no one
 * value (a first letter) — and it stays in its group.
 */
export function groupDropValue(property: GraphProperty, to: string, from: string | undefined, current: unknown, config: Record<string, unknown>): unknown {
  if (isNoValueGroupKey(to)) return CLEARED.has(property.type) ? property.type === "title" ? "" : null : undefined;
  if (property.type === "date") return dateGroupDropValue(to, current);
  const joined = tableGroupSeedValue(property, to, config);
  if (!LISTS.has(property.type) || !Array.isArray(joined) || joined.length !== 1) return joined;
  const left = from === undefined || isNoValueGroupKey(from) ? undefined : tableGroupSeedValue(property, from, config);
  return swappedMember(current, Array.isArray(left) ? left[0] : undefined, joined[0]);
}

/**
 * The values rows moved into a view's group `to` take, by row and property:
 * what each not in it already takes there (groupDropValue), as a row dropped
 * in another group of Notion's list, gallery or timeline does, and so do
 * several chosen rows dropped there together (seen 2026-11-24). `groupOf` is
 * the group a row stands in, `rows` the rows with their values now.
 */
export function groupMoveValues(property: GraphProperty | null | undefined, rowIds: readonly string[], to: string | undefined,
  groupOf: (rowId: string) => string | undefined, config: Record<string, unknown>, rows: readonly GraphRow[]) {
  if (!property || to === undefined) return undefined;
  const values = rowIds.flatMap((id) => {
    const from = groupOf(id);
    const value = from === to ? undefined : groupDropValue(property, to, from, rows.find((row) => row.id === id)?.values[property.id]?.value, config);
    return value === undefined ? [] : [[id, { [property.id]: value }] as const];
  });
  return values.length ? Object.fromEntries(values) as Record<string, Record<string, unknown>> : undefined;
}
