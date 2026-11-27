import type { GraphProperty, GraphRow, GraphView } from "@/contentGraph";
import { groupDropValue } from "./viewGroupMove";
import { shownRowsPlaced } from "./viewRowMove";

type Groups = readonly { key: string; rows: readonly { row: { id: string } }[] }[];
/** The group each dragged row left and the one it lands in, by the lines dragged (tableGridDrag): a row under several groups has one in each. */
type Lines = { to?: string; groupOf(rowId: string): string | undefined };

/**
 * Where rows dropped beside `target` in a table go (shownRowsPlaced): a view keeping its own
 * order moves them in it alone, as Notion's views do (`view`, its order with
 * the rows placed there); else they take new places between their
 * neighbours in the database's one order. `shown` is the table's rows as it
 * shows them. Dropped in another group of a grouped table (`grouped`), the
 * rows not in it take its value (groupDropValue: in the group for no value
 * none) in the move's one step of ⌘Z, as Notion's
 * table (seen 2026-11-24 in two tabs, also two chosen rows dragged together:
 * they stood in the group as dropped, its value theirs, chosen; one ⌘Z put
 * back their group and place) and as a list's, a board's and a timeline's
 * (moveShownRows), each from the group of the line it was dragged by
 * (`lines`: tags x and y dragged from y to z were x and z, seen 2026-11-24).
 * `rows` are the rows the move writes — their new places,
 * or, in a view keeping its own order, the rows taking a value — with
 * `values` by row; `written`, those rows with their values, for a writer
 * that saves rows alone.
 */
export function tableRowsDropped(view: GraphView, shown: readonly GraphRow[], sourceIds: readonly string[],
  target: { id: string; side: "before" | "after" }, grouped?: { property: GraphProperty; groups: Groups; lines?: Lines }) {
  const values = groupValues(view, shown, sourceIds, target.id, grouped);
  return { ...shownRowsPlaced(view, shown, sourceIds, target, values), values };
}

/** What each dropped row not in the group `targetId` stands in takes there (groupDropValue): a list's member swapped, none in the group for no value. */
function groupValues(view: GraphView, shown: readonly GraphRow[], sourceIds: readonly string[], targetId: string, grouped?: { property: GraphProperty; groups: Groups; lines?: Lines }) {
  const firstGroup = (id: string) => grouped?.groups.find(({ rows }) => rows.some(({ row }) => row.id === id))?.key;
  const groupOf = (id: string) => grouped?.lines?.groupOf(id) ?? firstGroup(id);
  const to = grouped?.lines?.to ?? firstGroup(targetId);
  if (!grouped || to === undefined) return undefined;
  const values = sourceIds.flatMap((id) => {
    const from = groupOf(id);
    const value = from === to ? undefined : groupDropValue(grouped.property, to, from, shown.find((row) => row.id === id)?.values[grouped.property.id]?.value, view.config);
    return value === undefined ? [] : [[id, { [grouped.property.id]: { type: grouped.property.type, value } }] as const];
  });
  return values.length ? Object.fromEntries(values) as Record<string, GraphRow["values"]> : undefined;
}
