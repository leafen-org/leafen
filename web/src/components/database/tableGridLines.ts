import type { GraphRow } from "@/contentGraph";
import { itemsReplacedFrom, itemsSplicedFrom, mapItems, replaceItems, spliceItems } from "@/lib/rowsReplaced";
import { withSubitemAdds, type TableGroup, type TableTreeRow } from "./tableViewModel";

export type TableProjectedRow = { id: string; parentId: string | null; row: GraphRow };
type Line = TableTreeRow<TableProjectedRow>;

export type GridEntry =
  | { kind: "group" | "head" | "add" | "more"; key: string; group: TableGroup<TableProjectedRow> }
  // A row's place among the rows shown; without one, its place among the
  // lines (a table whose lines are its rows, which then stay as they are
  // when rows before them move).
  | { kind: "row"; key: string; item: Line; displayIndex?: number }
  | { kind: "subitemAdd"; key: string; parentId: string; depth: number }
  | { kind: "newGroup"; key: string };

export type GridLines = {
  groups: readonly TableGroup<TableProjectedRow>[];
  shown: ReadonlyMap<string, readonly Line[]>;
  settings: readonly unknown[];
  visibleItems: readonly Line[];
  entries: readonly GridEntry[];
};

const rowEntries = new WeakMap<readonly Line[], readonly GridEntry[]>();

/**
 * A table's lines in order — each group's header, its shown rows and its
 * "더 불러오기" — and its rows as shown. One group shown whole with no group
 * lines is a line for each of its rows, kept for its list; otherwise, when
 * the groups and what they show stand as they were with rows changed in
 * place in them or taken out and put in — moved by the view's sorts,
 * filters or groups (lib/rowsReplaced.ts) — the lines drawn last (`last`)
 * change for those rows alone, those after them numbered again (2026-11-11:
 * each edit that arrived on a grouped table of 100,000 rows made all its
 * lines again).
 */
export function tableGridLines(input: {
  groups: readonly TableGroup<TableProjectedRow>[];
  shown: ReadonlyMap<string, readonly Line[]>;
  collapsedGroups: ReadonlySet<string>;
  expandedRows: ReadonlySet<string>;
  openLeaves?: ReadonlySet<string>;
  grouped: boolean;
  subitemAdds: boolean;
  readOnly: boolean;
  addGroup: boolean;
  addGroupRow: boolean;
  editEmptyGroupFilters: boolean;
}, last: { current: GridLines | null }): { visibleItems: readonly Line[]; entries: readonly GridEntry[] } {
  const { groups, shown, collapsedGroups, grouped, subitemAdds } = input;
  const only = groups.length === 1 && !collapsedGroups.has(groups[0].key) ? groups[0] : null;
  if (only && !grouped && !subitemAdds && shown.get(only.key) === only.rows) {
    return { visibleItems: only.rows, entries: mapItems(only.rows, rowEntries, (item) => ({ kind: "row", key: `row:${item.row.row.id}`, item })) };
  }
  const settings = [collapsedGroups, input.expandedRows, input.openLeaves, grouped, subitemAdds, input.readOnly, input.addGroup, input.addGroupRow, input.editEmptyGroupFilters];
  const lines = (last.current && changedLines(last.current, groups, shown, settings)) ?? allLines(input);
  last.current = { groups, shown, settings, ...lines };
  return lines;
}

/** The first line of each open group's rows among the rows shown, for a grouped table's keys (tablePart). */
export function groupFirstLines(groups: readonly TableGroup<TableProjectedRow>[], shown: ReadonlyMap<string, readonly Line[]>, collapsed: ReadonlySet<string>) {
  const starts: number[] = [];
  let line = 0;
  for (const group of groups) {
    if (collapsed.has(group.key)) continue;
    starts.push(line);
    line += shown.get(group.key)?.length ?? 0;
  }
  return starts;
}

function allLines(input: Parameters<typeof tableGridLines>[0]) {
  const { groups, shown: shownRows, collapsedGroups, expandedRows, openLeaves, grouped, subitemAdds, readOnly } = input;
  const visibleItems = groups.length === 1 && !collapsedGroups.has(groups[0].key) ? shownRows.get(groups[0].key) ?? []
    : groups.flatMap((group) => collapsedGroups.has(group.key) ? [] : shownRows.get(group.key) ?? []);
  const entries: GridEntry[] = [];
  let displayIndex = 0;
  for (const group of groups) {
    if (grouped) entries.push({ kind: "group", key: `group:${group.key}`, group });
    if (collapsedGroups.has(group.key)) continue;
    if (grouped) entries.push({ kind: "head", key: `head:${group.key}`, group });
    const shown = shownRows.get(group.key) ?? [];
    const open = (item: Line) => subitemAdds
      && (item.hasChildren ? expandedRows.has(item.row.row.id) : Boolean(openLeaves?.has(item.row.row.id)));
    // A row under several groups (a relation's pages) shows in each.
    const at = grouped ? `${group.key}:` : "";
    for (const entry of withSubitemAdds(shown, open, (row) => row.row.id)) {
      if (entry.kind === "row") entries.push({ kind: "row", key: `row:${at}${entry.item.row.row.id}`, item: entry.item, displayIndex: displayIndex++ });
      else entries.push({ kind: "subitemAdd", key: `subitem-add:${at}${entry.parentId}`, parentId: entry.parentId, depth: entry.depth });
    }
    if (shown.length < group.rows.length) entries.push({ kind: "more", key: `more:${group.key}`, group });
    if (grouped && (!readOnly ? input.addGroupRow : input.editEmptyGroupFilters && !group.rows.length)) entries.push({ kind: "add", key: `add:${group.key}`, group });
  }
  if (grouped && !readOnly && input.addGroup) entries.push({ kind: "newGroup", key: "new-group" });
  return { visibleItems, entries };
}

function changedLines(
  last: GridLines,
  groups: readonly TableGroup<TableProjectedRow>[],
  shown: ReadonlyMap<string, readonly Line[]>,
  settings: readonly unknown[],
) {
  // "+ 새 하위 항목" places follow the rows' parents, which aren't followed here.
  if (settings[4] || groups.length !== last.groups.length || settings.some((value, index) => value !== last.settings[index])) return undefined;
  const collapsed = settings[0] as ReadonlySet<string>, grouped = settings[3] === true;
  // The lines and rows shown taken out of those drawn last (their places
  // there) and put in (their places now), as the groups walk them in order.
  const linesOut: number[] = [], linesIn: [number, GridEntry][] = [], rowsOut: number[] = [], rowsIn: [number, Line][] = [];
  let spliced = false, wasAt = 0, at = 0, wasShown = 0, shownNow = 0;
  const line = (entry: GridEntry | null) => {
    if (entry) {
      linesOut.push(wasAt);
      linesIn.push([at, entry]);
    }
    wasAt += 1;
    at += 1;
  };
  for (const [index, group] of groups.entries()) {
    const before = last.groups[index];
    const list = shown.get(group.key) ?? [], was = last.shown.get(before.key) ?? [];
    if (group.key !== before.key || !group.rows.length !== !before.rows.length || (list.length < group.rows.length) !== (was.length < before.rows.length)) return undefined;
    const header = (kind: "group" | "head" | "more" | "add") => group === before ? null : { kind, key: `${kind}:${group.key}`, group };
    if (grouped) line(header("group"));
    if (collapsed.has(group.key)) continue;
    if (grouped) line(header("head"));
    const keyAt = grouped ? `${group.key}:` : "";
    if (list !== was && (list.length || was.length)) {
      const replaced = itemsReplacedFrom(list), made = replaced ? undefined : itemsSplicedFrom(list);
      if (replaced?.from === was) {
        for (const place of replaced.at) {
          const item = list[place], entry = last.entries[wasAt + place];
          if (entry?.kind !== "row" || entry.key !== `row:${keyAt}${item.row.row.id}`) return undefined;
          linesOut.push(wasAt + place);
          linesIn.push([at + place, { ...entry, item }]);
          rowsOut.push(wasShown + place);
          rowsIn.push([shownNow + place, item]);
        }
      } else if (made?.from === was) {
        spliced = true;
        for (const place of made.removed) {
          if (last.entries[wasAt + place]?.key !== `row:${keyAt}${was[place].row.row.id}`) return undefined;
          linesOut.push(wasAt + place);
          rowsOut.push(wasShown + place);
        }
        for (const place of made.added) {
          const item = list[place];
          linesIn.push([at + place, { kind: "row", key: `row:${keyAt}${item.row.row.id}`, item, displayIndex: -1 }]);
          rowsIn.push([shownNow + place, item]);
        }
      } else return undefined;
    }
    wasAt += was.length;
    at += list.length;
    wasShown += was.length;
    shownNow += list.length;
    if (list.length < group.rows.length) line(header("more"));
    if (grouped && (!settings[5] ? settings[7] : settings[8] && !group.rows.length)) line(header("add"));
  }
  const byPlace = <Item>(left: readonly [number, Item], right: readonly [number, Item]) => left[0] - right[0];
  if (!spliced) return { visibleItems: replaceItems(last.visibleItems, new Map(rowsIn)), entries: replaceItems(last.entries, new Map(linesIn)) };
  linesOut.sort((left, right) => left - right);
  rowsOut.sort((left, right) => left - right);
  const entries = spliceItems(last.entries, linesOut, linesIn.sort(byPlace));
  numberRows(entries, Math.min(linesOut[0] ?? Infinity, linesIn[0]?.[0] ?? Infinity), Math.max(linesOut.at(-1) ?? -1, linesIn.at(-1)?.[0] ?? -1));
  return { visibleItems: spliceItems(last.visibleItems, rowsOut, rowsIn.sort(byPlace)), entries };
}

// Each row line's place among the rows shown, from the first line changed:
// the lines after the last changed numbered again until one stands as it did.
function numberRows(entries: GridEntry[], first: number, last: number) {
  let shown = 0;
  for (let index = first - 1; index >= 0; index -= 1) {
    const entry = entries[index];
    if (entry.kind === "row") {
      shown = (entry.displayIndex ?? index) + 1;
      break;
    }
  }
  for (let index = first; index < entries.length; index += 1) {
    const entry = entries[index];
    if (entry.kind !== "row") continue;
    if (entry.displayIndex !== shown) entries[index] = { ...entry, displayIndex: shown };
    else if (index > last) break;
    shown += 1;
  }
}
