import { compareGraphOrder, type GraphRow } from "@/contentGraph";
import { keptItems, mapItems, moveItems, placesById, replaceItems, spliceItems, splicedBefore, workedOutBefore, type Places } from "@/lib/rowsReplaced";
import type { StatusStage } from "./propertyStatusPolicy";
import { resizeTableColumn } from "./tableInteractionModel";
import type { GroupPage } from "./GroupPageGlyph";
import { orderViewProperties } from "./viewPropertyOrder";

export type TablePropertyLike = { id: string; name: string; position: string; type: string };
export type TableRowLike = { id: string; parentId?: string | null };
export type TableTreeRow<Row> = { row: Row; depth: number; hasChildren: boolean };

/**
 * The rows in order with, after each open row's sub-items, the place of its
 * "+ 새 하위 항목", which makes one, as Notion's table shows it (seen
 * 2026-10-10): a row with sub-items open, or one without opened to add one.
 */
export function withSubitemAdds<Row>(
  items: readonly TableTreeRow<Row>[],
  open: (item: TableTreeRow<Row>) => boolean,
  idOf: (row: Row) => string,
) {
  const result: ({ kind: "row"; item: TableTreeRow<Row> } | { kind: "subitemAdd"; parentId: string; depth: number })[] = [];
  const opened: { id: string; depth: number }[] = [];
  const closeTo = (depth: number) => {
    for (let last = opened.at(-1); last && last.depth >= depth; last = opened.at(-1)) {
      opened.pop();
      result.push({ kind: "subitemAdd", parentId: last.id, depth: last.depth + 1 });
    }
  };
  for (const item of items) {
    closeTo(item.depth);
    result.push({ kind: "row", item });
    if (open(item)) opened.push({ id: idOf(item.row), depth: item.depth });
  }
  closeTo(0);
  return result;
}
export type TableGroup<Row> = {
  key: string; rows: TableTreeRow<Row>[]; label?: string; color?: string; checkbox?: boolean; page?: GroupPage; statusStage?: StatusStage; statusOption?: boolean;
};

const NO_GROUP = "__no_group__";

/**
 * The table's columns in view order: the view's property order, then the
 * rest by name, less the properties the view hides. An
 * older table view that listed its columns in visiblePropertyIds keeps
 * showing exactly those, in that order.
 */
export function projectVisibleProperties<Property extends TablePropertyLike>({
  properties,
  visiblePropertyIds,
  propertyOrder,
  visibleProperties,
  hiddenProperties,
}: {
  properties: readonly Property[];
  visiblePropertyIds?: readonly string[];
  propertyOrder?: readonly string[];
  visibleProperties?: readonly string[];
  hiddenProperties?: readonly string[];
}) {
  const ordered = orderTableProperties(properties, propertyOrder ?? visiblePropertyIds);
  const allowed = visibleProperties ?? (visiblePropertyIds?.length ? visiblePropertyIds : undefined);
  const hidden = new Set(hiddenProperties ?? []);
  const shown = ordered.filter((property) => property.type === "title"
    || (allowed ? allowed.includes(property.id) : !hidden.has(property.id)));
  const title = shown.find((property) => property.type === "title");
  // The title sits where the view's order puts it, as Notion's (a private task
  // database's status comes before it, seen 2026-11-05); first where the order doesn't name it.
  if (!title || propertyOrder?.includes(title.id)) return shown;
  return [title, ...shown.filter((property) => property.id !== title.id)];
}

/** All properties in the view's order: listed ones first, then the title and the rest by name (viewPropertyOrder.ts). */
export function orderTableProperties<Property extends TablePropertyLike>(
  properties: readonly Property[],
  order?: readonly string[],
) {
  return orderViewProperties([...properties].sort(compareGraphOrder), order);
}

// As Notion's, a row's sub-items show only once it is opened, each time the
// view is shown (seen 2026-10-10: not kept across reloads, visits or views).
function projectHierarchy<Row extends TableRowLike>(
  rows: readonly Row[],
  expandedRowIds: ReadonlySet<string>,
) {
  const byParent = new Map<string | null, Row[]>();
  const ids = new Set(rows.map((row) => row.id));
  for (const row of rows) {
    const parentId = row.parentId && ids.has(row.parentId) ? row.parentId : null;
    const bucket = byParent.get(parentId) ?? [];
    bucket.push(row);
    byParent.set(parentId, bucket);
  }
  const result: TableTreeRow<Row>[] = [];
  const visited = new Set<string>();
  // A closed row's sub-items, and theirs, stay out of the table rather than
  // falling back to its top level.
  const cover = (row: Row) => {
    if (visited.has(row.id)) return;
    visited.add(row.id);
    (byParent.get(row.id) ?? []).forEach(cover);
  };
  const visit = (row: Row, depth: number) => {
    if (visited.has(row.id)) return;
    visited.add(row.id);
    const children = byParent.get(row.id) ?? [];
    result.push({ row, depth, hasChildren: children.length > 0 });
    children.forEach((child) => expandedRowIds.has(row.id) ? visit(child, depth + 1) : cover(child));
  };
  (byParent.get(null) ?? []).forEach((row) => visit(row, 0));
  // Rows whose parents form a loop have no top; they start at the top level.
  rows.forEach((row) => visit(row, 0));
  return result;
}

// Each row as the table's projection takes it, nested under its parent or
// not, kept for each list of rows: one with rows changed in place has those
// alone made again (lib/rowsReplaced.ts).
const projectedNested = new WeakMap<readonly GraphRow[], readonly TableInputRow[]>();
const projectedFlat = new WeakMap<readonly GraphRow[], readonly TableInputRow[]>();
export type TableInputRow = { id: string; parentId: string | null; row: GraphRow };

const underParents = new WeakMap<readonly GraphRow[], readonly GraphRow[]>();

/** The rows that are another row's sub-items, kept for each list of rows (lib/rowsReplaced.ts). */
export function rowsUnderParents(rows: readonly GraphRow[]) {
  return keptItems(rows, underParents, (row) => Boolean(row.subitemParentId), (row) => row.id);
}

export function tableProjectedRows(rows: readonly GraphRow[], nested: boolean): TableInputRow[] {
  return mapItems(rows, nested ? projectedNested : projectedFlat, (row) => ({ id: row.id, parentId: nested ? row.subitemParentId : null, row })) as TableInputRow[];
}

export function projectTableRows<Row extends TableRowLike>({
  rows,
  expandedRowIds,
  groupKey,
}: {
  rows: readonly Row[];
  expandedRowIds: ReadonlySet<string>;
  /** A row's group, or its groups when it falls in several (a relation's pages). */
  groupKey(row: Row): string | readonly string[] | null | undefined;
}) {
  const hierarchy = projectHierarchy(rows, expandedRowIds);
  const groups: TableGroup<Row>[] = [];
  const byKey = new Map<string, TableGroup<Row>>();
  for (const item of hierarchy) {
    const keys = [groupKey(item.row) ?? ""].flat().map((key) => String(key).trim() || NO_GROUP);
    for (const normalized of new Set(keys.length ? keys : [NO_GROUP])) {
      let group = byKey.get(normalized);
      if (!group) {
        group = { key: normalized, rows: [] };
        byKey.set(normalized, group);
        groups.push(group);
      }
      group.rows.push(item);
    }
  }
  return { groups, rows: hierarchy };
}

type Ungrouped = { groups: TableGroup<TableRowLike>[]; rows: TableTreeRow<TableRowLike>[] };
type Line = TableTreeRow<TableRowLike>;
// What a projection was worked out from: whether no row was under another,
// each line then a row of its own at the top in the rows' order.
type UngroupedKept = { expandedRowIds: ReadonlySet<string>; value: Ungrouped; parentless: boolean };
const ungroupedKept = new WeakMap<readonly TableRowLike[], UngroupedKept>();
const parentless = (rows: readonly TableRowLike[]) => !rows.some((row) => row.parentId);
const topLine = (row: TableRowLike): Line => ({ row, depth: 0, hasChildren: false });

/**
 * An ungrouped table's rows, as projectTableRows gives them, kept for each
 * list of rows: a list with rows changed in place under the same parents has
 * those alone changed in it, and one spliced (a sort or filter moved rows)
 * with no row under another has their lines alone taken out and put in
 * (lib/rowsReplaced.ts).
 */
export function projectUngroupedTableRows<Row extends TableRowLike>(rows: readonly Row[], expandedRowIds: ReadonlySet<string>) {
  let kept = ungroupedKept.get(rows);
  if (kept?.expandedRowIds !== expandedRowIds) {
    const before = workedOutBefore(rows, ungroupedKept);
    const spliced = before ? undefined : splicedBefore(rows, ungroupedKept);
    const value = (before?.value.expandedRowIds === expandedRowIds ? patchedUngrouped(before.value.value, before.from, rows, before.at) : undefined)
      ?? (spliced?.value.expandedRowIds === expandedRowIds && spliced.value.parentless && !spliced.added.some((index) => rows[index].parentId)
        ? ungrouped(spliceItems(spliced.value.value.rows, spliced.removed, spliced.added.map((index) => [index, topLine(rows[index])] as const))) : undefined);
    kept = value ? { expandedRowIds, value, parentless: (before ?? spliced)!.value.parentless }
      : { expandedRowIds, value: ungrouped(projectHierarchy(rows, expandedRowIds)), parentless: parentless(rows) };
    ungroupedKept.set(rows, kept);
  }
  return kept.value as unknown as { groups: TableGroup<Row>[]; rows: TableTreeRow<Row>[] };
}

function ungrouped(rows: Line[]): Ungrouped {
  return { rows, groups: rows.length ? [{ key: NO_GROUP, rows }] : [] };
}

function patchedUngrouped(kept: Ungrouped, from: readonly TableRowLike[], rows: readonly TableRowLike[], at: readonly number[]) {
  const places = placesById(kept.rows, (entry) => entry.row.id);
  const replaced = new Map<number, Line>();
  for (const index of at) {
    const was = from[index], row = rows[index];
    if (was.id !== row.id || (was.parentId ?? null) !== (row.parentId ?? null)) return undefined;
    // A row under a closed one isn't in the table.
    const place = places.get(row.id);
    if (place !== undefined) replaced.set(place, { ...kept.rows[place], row });
  }
  return ungrouped(replaceItems(kept.rows, replaced));
}

type Grouped<Group> = Ungrouped & { described: ReadonlyMap<string, Group> };
type GroupedKept = { inputs: readonly unknown[]; expandedRowIds: ReadonlySet<string>; value: Grouped<{ key: string }>; parentless: boolean };
const groupedKept = new WeakMap<readonly TableRowLike[], GroupedKept>();
// Each line's groups, as its row's descriptions' keys.
const lineGroups = new WeakMap<object, readonly string[]>();
const groupKeys = (described: readonly { key: string }[]) => [...new Set(described.length ? described.map(({ key }) => key.trim() || NO_GROUP) : [NO_GROUP])];
// A row's line changed: the groups it was in and is in, and whether it moved among the lines.
type LineChange = { id: string; line: Line | null; was: readonly string[]; now: readonly string[]; moved: boolean };

/**
 * A grouped table's lines worked out from its rows as last placed, each
 * showing its row as it is now: the same rows in the same order, some
 * changed in place since (useDeferredPlacing). A value picked shows at once,
 * the row moving to its new group as soon as the groups are worked out.
 */
export function groupedLinesAsNow<Projected extends { groups: TableGroup<TableInputRow>[]; rows: TableTreeRow<TableInputRow>[] }>(
  projected: Projected, placed: readonly GraphRow[], rows: readonly GraphRow[],
): Projected {
  if (placed === rows) return projected;
  const now = new Map<string, GraphRow>();
  for (let index = 0; index < rows.length; index += 1) if (rows[index] !== placed[index]) now.set(rows[index].id, rows[index]);
  if (!now.size) return projected;
  const asNow = (line: TableTreeRow<TableInputRow>) => {
    const row = now.get(line.row.id);
    return row ? { ...line, row: { ...line.row, row } } : line;
  };
  const changes = (lines: readonly TableTreeRow<TableInputRow>[]) => lines.some((line) => now.has(line.row.id));
  return {
    ...projected,
    groups: projected.groups.map((group) => changes(group.rows) ? { ...group, rows: group.rows.map(asNow) } : group),
    rows: changes(projected.rows) ? projected.rows.map(asNow) : projected.rows,
  };
}

/**
 * A grouped table's rows, as projectTableRows gives them with `groupsOf`
 * describing each row's groups, and what describes each group: kept for each
 * list of rows with what groups them (`inputs`). A list with rows changed in
 * place under the same parents has those alone changed in it, moved between
 * the groups that hold them when theirs changed, and one spliced (a sort or
 * filter moved rows) with no row under another has them taken out of their
 * groups and put in where they now stand; a group made or left empty, or
 * `patchable` false (groups read from other rows), groups them all again.
 */
export function projectGroupedTableRows<Row extends TableRowLike, Group extends { key: string }>(
  rows: readonly Row[],
  expandedRowIds: ReadonlySet<string>,
  inputs: readonly unknown[],
  groupsOf: (row: Row) => readonly Group[],
  patchable: boolean,
) {
  const same = (kept: GroupedKept | undefined) => kept?.expandedRowIds === expandedRowIds
    && kept.inputs.length === inputs.length && kept.inputs.every((input, index) => input === inputs[index]);
  type Projected = { groups: TableGroup<Row>[]; rows: TableTreeRow<Row>[]; described: ReadonlyMap<string, Group> };
  const describe = groupsOf as unknown as (row: TableRowLike) => readonly { key: string }[];
  if (!patchable) return groupedWhole(rows, expandedRowIds, describe) as unknown as Projected;
  let kept = groupedKept.get(rows);
  if (!same(kept)) {
    const before = workedOutBefore(rows, groupedKept);
    const spliced = before ? undefined : splicedBefore(rows, groupedKept);
    const changed = before && same(before.value) ? linesReplaced(before.value.value, before.from, rows, before.at, describe)
      : spliced && same(spliced.value) && spliced.value.parentless ? linesSpliced(spliced.value.value, spliced, rows, describe) : undefined;
    const value = changed ? regrouped((before ?? spliced)!.value.value, changed, describe) : undefined;
    kept = value ? { inputs, expandedRowIds, value, parentless: (before ?? spliced)!.value.parentless }
      : { inputs, expandedRowIds, value: groupedWhole(rows, expandedRowIds, describe), parentless: parentless(rows) };
    groupedKept.set(rows, kept);
  }
  return kept!.value as unknown as Projected;
}

function groupedWhole(rows: readonly TableRowLike[], expandedRowIds: ReadonlySet<string>, groupsOf: (row: TableRowLike) => readonly { key: string }[]) {
  const described = new Map<string, { key: string }>();
  const keysOf = new Map<TableRowLike, readonly string[]>();
  const projected = projectTableRows({ rows, expandedRowIds, groupKey: (row) => {
    const groups = groupsOf(row);
    for (const group of groups) described.set(group.key, group);
    keysOf.set(row, groupKeys(groups));
    return groups.map(({ key }) => key);
  } });
  for (const line of projected.rows) lineGroups.set(line, keysOf.get(line.row) ?? [NO_GROUP]);
  return { ...projected, described };
}

// The lines of rows changed in place, each where its row's line stood.
function linesReplaced(kept: Grouped<{ key: string }>, from: readonly TableRowLike[], rows: readonly TableRowLike[], at: readonly number[],
  groupsOf: (row: TableRowLike) => readonly { key: string }[]) {
  const lineAt = placesById(kept.rows, (line) => line.row.id);
  const lines = new Map<number, Line>();
  const changes: LineChange[] = [];
  for (const index of at) {
    const was = from[index], row = rows[index];
    if (was.id !== row.id || (was.parentId ?? null) !== (row.parentId ?? null)) return undefined;
    // A row under a closed one isn't in the table.
    const place = lineAt.get(row.id);
    if (place === undefined) continue;
    const wasIn = lineGroups.get(kept.rows[place]);
    if (!wasIn) return undefined;
    const line = { ...kept.rows[place], row };
    lines.set(place, line);
    changes.push({ id: row.id, line, was: wasIn, now: lineKeys(line, groupsOf), moved: false });
  }
  return { shown: replaceItems(kept.rows, lines), changes };
}

// The lines of a list of rows spliced with no row under another: each a row's own at the top.
function linesSpliced(kept: Grouped<{ key: string }>, spliced: { removed: readonly number[]; added: readonly number[] }, rows: readonly TableRowLike[],
  groupsOf: (row: TableRowLike) => readonly { key: string }[]) {
  if (spliced.added.some((index) => rows[index].parentId)) return undefined;
  const changes = new Map<string, LineChange>();
  for (const index of spliced.removed) {
    const line = kept.rows[index], was = lineGroups.get(line);
    if (!was) return undefined;
    changes.set(line.row.id, { id: line.row.id, line: null, was, now: [], moved: true });
  }
  const added = spliced.added.map((index) => {
    const line = topLine(rows[index]), change = changes.get(line.row.id);
    changes.set(line.row.id, { id: line.row.id, line, was: change?.was ?? [], now: lineKeys(line, groupsOf), moved: true });
    return [index, line] as const;
  });
  return { shown: spliceItems(kept.rows, spliced.removed, added), changes: [...changes.values()] };
}

function lineKeys(line: Line, groupsOf: (row: TableRowLike) => readonly { key: string }[]) {
  const keys = groupKeys(groupsOf(line.row));
  lineGroups.set(line, keys);
  return keys;
}

// The groups with the lines changed taken out of those they were in and put
// in those they are in where they stand among the lines (`shown`); in place
// when they stayed in a group and in their place.
function regrouped(kept: Grouped<{ key: string }>, { shown, changes }: { shown: Line[]; changes: readonly LineChange[] },
  groupsOf: (row: TableRowLike) => readonly { key: string }[]) {
  const described = new Map(kept.described);
  for (const { line } of changes) if (line) for (const group of groupsOf(line.row)) described.set(group.key, group);
  const shownAt = placesById(shown, (line) => line.row.id);
  const groups = kept.groups.map((group) => {
    const replaced = new Map<number, Line>(), leaving: number[] = [], coming: Line[] = [];
    let places: Places | undefined;
    const placeOf = (id: string) => (places ??= placesById(group.rows, (item) => item.row.id)).get(id) ?? -1;
    for (const { id, line, was, now, moved } of changes) {
      const before = was.includes(group.key), after = now.includes(group.key);
      if (before && after && !moved) replaced.set(placeOf(id), line!);
      else {
        if (before) leaving.push(placeOf(id));
        if (after) coming.push(line!);
      }
    }
    if (replaced.has(-1) || leaving.includes(-1)) return null;
    if (!leaving.length && !coming.length) return replaced.size ? { ...group, rows: replaceItems(group.rows, replaced) } : group;
    for (const [place, line] of replaced) {
      leaving.push(place);
      coming.push(line);
    }
    const rows = moveItems(group.rows, leaving, coming, (line, other) => (shownAt.get(line.row.id) ?? 0) < (shownAt.get(other.row.id) ?? 0));
    return rows.length ? { ...group, rows } : null;
  });
  // A row gone to a group not shown, or a group left empty, groups them all again.
  if (groups.some((group) => group === null) || changes.some(({ now }) => now.some((key) => !kept.groups.some((group) => group.key === key)))) return undefined;
  return { rows: shown, groups: groups as TableGroup<TableRowLike>[], described };
}

/**
 * A column's width: the one it was given, else Notion's for a column nobody
 * resized (seen 2026-10-24 in tables made from Notion's menu, inline and
 * full page) — the title 280, any other 200.
 */
export function tableColumnWidth(config: Record<string, unknown>, propertyId: string, type?: string) {
  const fallback = type === "title" ? 280 : 200;
  const widths = config.propertyWidths;
  if (!widths || typeof widths !== "object" || Array.isArray(widths)) return fallback;
  const value = Number((widths as Record<string, unknown>)[propertyId]);
  if (!Number.isFinite(value) || value <= 0) return fallback;
  // A status shown as a checkbox may be as narrow as the box, as a private task database's (Notion's 39, seen 2026-11-05).
  const checkbox = Array.isArray(config.statusCheckboxColumns) && config.statusCheckboxColumns.includes(propertyId);
  return checkbox ? Math.max(32, Math.round(value)) : resizeTableColumn(value, type);
}

/** Whether the view's filters may be what keeps a group empty: quick filters, or an advanced filter with conditions. */
export function tableViewFiltered(config: { quickFilters?: unknown; filterGroup?: unknown }) {
  const group = config.filterGroup as { filters?: unknown[]; groups?: unknown[] } | undefined;
  return (Array.isArray(config.quickFilters) && config.quickFilters.length > 0) || Boolean(group?.filters?.length || group?.groups?.length);
}

