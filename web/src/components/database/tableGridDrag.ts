import type { GridEntry } from "./tableGridLines";
import { chosenRowsOf, groupLine, viewLinesDrag } from "./viewGroupLines";

type RowEntry = Extract<GridEntry, { kind: "row" }>;

/** A row line's name for its drags (viewGroupLines.ts), from its key: `row:<group>:<row>` grouped, `row:<row>` not. */
export function gridEntryLine(entry: Pick<RowEntry, "key" | "item">) {
  const id = entry.item.row.row.id;
  return entry.key === `row:${id}` ? id : groupLine(entry.key.slice("row:".length, -(id.length + 1)), id);
}

/** The lines of a group's rows, which its head's box chooses (viewGroupLines.ts). */
export function gridGroupLines(group: string, items: readonly Pick<RowEntry, "item">["item"][]) {
  return items.map((item) => groupLine(group, item.row.row.id));
}

/**
 * How a table's rows are dragged, by their lines (viewLinesDrag): a row under
 * several groups (a tag's, a person's, a related page's) has a line in each,
 * dragged and landed on by itself, as Notion's (seen 2026-11-24 in Chrome at
 * 1685, dpr 1: tags x and y dragged from y to z were x and z; moved within y
 * they stayed x and y). `chosenRows` are the rows of the chosen lines, tinted
 * on all their lines, as Notion's.
 */
export function tableGridDrag(entries: readonly GridEntry[], chosen: ReadonlySet<string>) {
  return { ...viewLinesDrag(entries, (entry) => entry.kind === "row"
    ? { line: gridEntryLine(entry), parentId: entry.item.row.parentId, depth: entry.item.depth } : undefined, chosen), chosenRows: chosenRowsOf(chosen) };
}
