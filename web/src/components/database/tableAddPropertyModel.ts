import { distinctPropertyName } from "@/contentGraph";
import type { PropertyType } from "@/lib/types";

// Notion's "+" at the end of a table's header adds a property (seen
// 2026-09-30): a name field in a new header cell and, under it, the types in
// two columns by group — the common ones, those that work from other values,
// then the ones Notion fills in. Notion's AI and connected-app types have no
// Leafen counterpart, and Leafen's own location and last visited time, which nothing fills in, are
// not offered (decided 2026-10-16).
export const TABLE_ADD_PROPERTY_GROUPS: readonly (readonly PropertyType[])[] = [
  ["rich_text", "number", "select", "multi_select", "status", "date", "person", "files", "checkbox", "url", "phone", "email"],
  ["relation", "rollup", "formula", "button", "unique_id", "place", "verification"],
  ["created_time", "last_edited_time", "created_by", "last_edited_by"],
];

// A database page's "속성 추가" lists the same types in one column, in
// Notion's order there (seen 2026-11-06): the common ones, then formula,
// relation and rollup, the ones Notion fills in, button, place and ID.
export const ROW_PAGE_ADD_PROPERTY_TYPES: readonly PropertyType[] = [
  "rich_text", "number", "select", "multi_select", "status", "date", "person", "files", "checkbox", "url", "email", "phone",
  "formula", "relation", "rollup", "created_time", "created_by", "last_edited_time", "last_edited_by", "button", "place",
  "unique_id", "verification",
];

// newTablePropertyName is the new property's name: the one typed, else its
// type's, told apart from a name its source has already (taken) with " 1",
// " 2" …, as Notion names a second "Number" "Number 1" (seen 2026-11-14 from
// a table's "+", a page's "속성 추가" and its relation: "글A 1", "글A 2",
// "빈표 1") and as the server keeps it (distinctPropertyName), so its column
// and settings show that name before the server answers.
export function newTablePropertyName(typed: string, typeLabel: string, taken: Iterable<string> = []) {
  return distinctPropertyName(typed.trim() || typeLabel, new Set([...taken].map((name) => name.trim())));
}

// tableAddPropertyRows lays groups out as the menu shows them: two to a row,
// each group starting a row of its own.
export function tableAddPropertyRows<Item>(groups: readonly (readonly Item[])[]): Item[][][] {
  return groups.map((group) => {
    const rows: Item[][] = [];
    for (let index = 0; index < group.length; index += 2) rows.push(group.slice(index, index + 2));
    return rows;
  });
}

export type TableAddPropertySpot = { row: number; column: number };

// tableAddPropertyMove is where an arrow key takes the chosen type among the
// rows, counted across groups: up and down within its column, a row with one
// type taking the left column; left and right to the type before or after it
// as they read, from a row's left column to the right one of the row above
// and on into the next group, as Notion's (seen 2026-11-21 in Chrome at 1440
// in both test accounts, at different times: ← from 상태 marks 다중 선택). It
// stays at the list's ends, which tableAddPropertyStep goes round.
export function tableAddPropertyMove(
  rows: readonly (readonly unknown[])[],
  at: TableAddPropertySpot,
  key: string,
): TableAddPropertySpot {
  if (key === "ArrowLeft" || key === "ArrowRight") {
    const right = key === "ArrowRight";
    const column = at.column + (right ? 1 : -1);
    if (column >= 0 && column < (rows[at.row]?.length ?? 0)) return { row: at.row, column };
    const row = at.row + (right ? 1 : -1);
    if (row < 0 || row >= rows.length) return at;
    return { row, column: right ? 0 : rows[row].length - 1 };
  }
  if (key !== "ArrowDown" && key !== "ArrowUp") return at;
  const row = at.row + (key === "ArrowDown" ? 1 : -1);
  if (row < 0 || row >= rows.length) return at;
  return { row, column: Math.min(at.column, rows[row].length - 1) };
}

const FORWARD: Readonly<Record<string, boolean | undefined>> = { ArrowDown: true, ArrowRight: true, ArrowUp: false, ArrowLeft: false };

// tableAddPropertyStep is where the keys take the mark, as Notion's type menu
// (seen 2026-11-21 at 1440 in Chrome, with and without its AI rows, in
// several tabs at different times): within the rows as tableAddPropertyMove,
// and every arrow around the list's ends — ↑ and ↓ from the first or last
// row, ← and → from the first or last type — through a stop that marks no
// type, the open search's place (`searchStop`), or with the search closed
// straight from the last type to the first and back. From no mark, ↓ and →
// mark the first type, ↑ and ← the last.
export function tableAddPropertyStep(
  rows: readonly (readonly unknown[])[],
  at: TableAddPropertySpot | null,
  key: string,
  searchStop: boolean,
): TableAddPropertySpot | null {
  const forward = FORWARD[key];
  if (forward === undefined) return at;
  if (!rows.length) return null;
  const end = rows.length - 1, lastColumn = rows[end].length - 1;
  const ends = { first: { row: 0, column: 0 }, last: { row: end, column: lastColumn } };
  if (!at) return forward ? ends.first : ends.last;
  const vertical = key === "ArrowDown" || key === "ArrowUp";
  const atEnd = vertical ? at.row === (forward ? end : 0)
    : forward ? at.row === end && at.column === lastColumn : at.row === 0 && at.column === 0;
  if (atEnd) return searchStop ? null : forward ? ends.first : ends.last;
  return tableAddPropertyMove(rows, at, key);
}
