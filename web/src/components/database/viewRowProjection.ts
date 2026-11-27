import { useDeferredValue, useMemo } from "react";
import type { GraphProperty, GraphRow, GraphView } from "@/contentGraph";
import type { DbProperty, DbView, Page, ViewConfig } from "@/lib/types";
import { nativeBoardQueryBounds } from "./boardViewModel";
import { galleryPresentationPage, galleryPresentationProperty } from "./galleryViewModel";
import { applyView, viewRowQuery } from "./query";
import { readScopedRows } from "./subitemFilterScope";
import { effectiveFilterGroup } from "@/shared/database/queryFilterGroups";
import type { QueryProperty, QueryViewConfig } from "@/shared/database/queryTypes";
import { lookupById, lookupOver } from "@/lib/keptLookup";
import { itemsReplacedFrom, moveItems, placesById, replaceItems, workedOutBefore } from "@/lib/rowsReplaced";

const NO_PAGES: Readonly<Record<string, Page | undefined>> = Object.freeze({});

/**
 * A view's rows in its order: its filters and sorts applied, as the table,
 * the list and other card views and the sidebar's database view sections
 * show them; with sub-items, as its sub-item filter option says.
 */
export function projectRows({
  currentUserId,
  pageId,
  properties,
  rows,
  view,
  workspaceId,
  subitems,
  related,
}: {
  currentUserId?: string;
  pageId: string;
  properties: readonly GraphProperty[];
  rows: readonly GraphRow[];
  view: GraphView;
  workspaceId: string;
  /** With sub-items on: where the rows shown though not kept by the filter are written. */
  subitems?: { faded: ReadonlySet<string> };
  /** The related databases, which a filter or sort on a formula or a rollup reads through. */
  related?: { pagesById: Readonly<Record<string, Page | undefined>>; propsByDb: Readonly<Record<string, readonly DbProperty[] | undefined>> };
}): GraphRow[] {
  // A view with no filter, sort, search or order of its own shows its rows
  // as given, the same list (2026-11-11: each edit that arrived on a table
  // of 100,000 rows made all their pages and their order again).
  if (!subitems && showsRowsAsGiven(view, properties)) return rows as GraphRow[];
  const titleProperty = properties.find((property) => property.type === "title");
  const presentationView: DbView = {
    id: view.id,
    databaseId: view.dataSourceId,
    name: view.name,
    type: view.type as DbView["type"],
    position: 0,
    config: view.config as ViewConfig,
  };
  const props = properties.map(galleryPresentationProperty);
  // A query that reads each row alone (placedRowsKept) places rows changed
  // in place again, not every row, and the same rows again as they were
  // while what it reads of the view and its properties stays.
  const inputs = !subitems && readsRowsAlone(view, properties)
    ? [queryKey(view), propertiesKey(properties), currentUserId, pageId, workspaceId, new Date().toDateString()] : null;
  const kept = inputs && placedRowsKept.get(rows);
  if (kept && kept.inputs.length === inputs.length && kept.inputs.every((input, index) => input === inputs[index])) return kept.placed as GraphRow[];
  const pageOf = (row: GraphRow, index: number) => presentedRow(row, titleProperty, workspaceId, index);
  const again = inputs && placedAgain(rows, inputs, () => viewRowQuery(props, presentationView, {},
    { currentPageId: pageId, currentUserId }, nativeBoardQueryBounds), pageOf);
  if (again) return again;
  const pages = rows.map(pageOf);
  // The rows' pages over the related ones, read through both rather than
  // every page copied into one record (lib/keptLookup.ts).
  const pagesById = lookupOver(related?.pagesById ?? NO_PAGES, lookupById(pages)) as Record<string, Page>;
  const idsOf = (config: ViewConfig | undefined) => applyView(pages, props, { ...presentationView, config }, pagesById,
    { currentPageId: pageId, currentUserId, propsByDb: related?.propsByDb as Record<string, DbProperty[]> | undefined }, nativeBoardQueryBounds).map((page) => page.id);
  const places = placesById(rows, (row) => row.id);
  const rowsOf = (ids: readonly string[]) => ids.flatMap((id) => { const at = places.get(id); return at === undefined ? [] : [rows[at]]; });
  if (!subitems) {
    const placed = rowsOf(idsOf(presentationView.config));
    if (inputs) placedRowsKept.set(rows, { inputs, placed });
    return placed;
  }
  // With sub-items, the view's filter keeps rows and its sub-item option
  // says which rows around them show, those not kept faded.
  const scoped = readScopedRows(rows, presentationView.config, (config) => rowsOf(idsOf(config)));
  subitems.faded = scoped.faded;
  return scoped.rows;
}

type Placing = { rows: readonly GraphRow[]; view: GraphView };

/**
 * The rows and the view a view's order is worked out from: as last placed
 * while only rows' values or the view's query changed, worked out again
 * behind the press that changed them (useDeferredValue: in pieces, dropped
 * for a newer press); as they are now once rows came or went, the view is
 * another or its own order changed, so a row made, removed or dragged shows
 * in its place at once.
 */
export function useDeferredPlacing(rows: readonly GraphRow[], view: GraphView): Placing {
  const now = useMemo<Placing>(() => ({ rows, view }), [rows, view]);
  const placed = useDeferredValue(now);
  return useMemo(() => {
    if (placed === now) return now;
    const before = placed.view, config = before.config as { rowOrder?: unknown }, order = (view.config as { rowOrder?: unknown }).rowOrder;
    if (before.id !== view.id || before.dataSourceId !== view.dataSourceId || before.type !== view.type || !sameIds(config.rowOrder, order)) return now;
    if (placed.rows === rows || sameRowIds(placed.rows, rows)) return placed;
    return before === view ? now : { rows, view: before };
  }, [now, placed, rows, view]);
}

function sameIds(left: unknown, right: unknown) {
  if (left === right) return true;
  return Array.isArray(left) && Array.isArray(right) && left.length === right.length && left.every((id, index) => id === right[index]);
}

function sameRowIds(left: readonly GraphRow[], right: readonly GraphRow[]) {
  if (left.length !== right.length) return false;
  for (let index = 0; index < left.length; index += 1) if (left[index].id !== right[index].id) return false;
  return true;
}

/**
 * The rows `placed` from `from`, each as it is in `rows` now: the same rows
 * in the same order, some changed in place since. A row the change moves
 * or lets in waits for its order to be worked out (useDeferredPlacing).
 */
export function rowsAsNow(placed: readonly GraphRow[], from: readonly GraphRow[], rows: readonly GraphRow[]): GraphRow[] {
  if (from === rows) return placed as GraphRow[];
  // Shown as given (showsRowsAsGiven): the rows themselves.
  if (placed === from) return rows as GraphRow[];
  const made = itemsReplacedFrom(rows);
  const at = made?.from === from ? made.at : rows.flatMap((row, index) => row === from[index] ? [] : [index]);
  const places = placesById(placed, rowId);
  const changed = new Map<number, GraphRow>();
  for (const index of at) {
    const place = places.get(rows[index].id);
    if (place !== undefined) changed.set(place, rows[index]);
  }
  return changed.size ? replaceItems(placed, changed) : placed as GraphRow[];
}

// What a view's query reads of it: its filters, sorts and search, not its
// name, its columns' widths or what it shows (2026-11-11: a property renamed
// on a sorted table of 100,000 rows sorted them all again).
function queryKey(view: GraphView) {
  const config = view.config as Record<string, unknown>;
  return JSON.stringify([view.id, view.dataSourceId, view.type, config.sorts, config.filterGroup, config.filters, config.filterConjunction,
    config.quickFilters, config.search]);
}

const propertiesKeys = new WeakMap<readonly GraphProperty[], string>();

// What a query reads of the properties: each one's type and settings, not
// its name, nor the names it had that a rename keeps (formerNames).
function propertiesKey(properties: readonly GraphProperty[]) {
  let key = propertiesKeys.get(properties);
  if (key === undefined) {
    key = JSON.stringify(properties.map((property) => {
      const { formerNames: _formerNames, ...config } = (property.config ?? {}) as Record<string, unknown>;
      return [property.id, property.dataSourceId, property.type, config];
    }));
    propertiesKeys.set(properties, key);
  }
  return key;
}

function showsRowsAsGiven(view: GraphView, properties: readonly GraphProperty[]) {
  const config = view.config as QueryViewConfig & { search?: unknown; sorts?: readonly unknown[]; rowOrder?: unknown };
  if (String(config.search ?? "").trim() || config.sorts?.length || Array.isArray(config.rowOrder)) return false;
  return !effectiveFilterGroup(config, new Map(properties.map((property) => [property.id, property as unknown as QueryProperty])));
}

// A view's rows as placed, kept for each list of rows with what placed
// them, when its query reads each row alone: its filters, sorts and search
// read no other row (a relation's, a rollup's, a formula's) and nothing that
// changes beside the rows (a verification's expiry; a
// date's "today" is part of what placed them). A list with rows changed in
// place has those rows placed again — where they were, or moved where its
// sorts put them, or in or out as its filters keep them (2026-11-11: each
// edit that arrived on a table of 100,000 rows filtered and sorted them all).
const placedRowsKept = new WeakMap<readonly GraphRow[], { inputs: readonly unknown[]; placed: readonly GraphRow[] }>();
const readByOthers = new Set(["relation", "rollup", "formula", "verification"]);
const rowId = (row: GraphRow) => row.id;

function readsRowsAlone(view: GraphView, properties: readonly GraphProperty[]) {
  const config = view.config as { search?: unknown; sorts?: readonly { propertyId?: unknown }[]; rowOrder?: unknown } & Record<string, unknown>;
  if (Array.isArray(config.rowOrder)) return false;
  const types = new Map(properties.map((property) => [property.id, property.type]));
  if (String(config.search ?? "").trim() && properties.some((property) => readByOthers.has(property.type))) return false;
  const read: unknown[] = [...(config.sorts ?? []).map((sort) => sort.propertyId)];
  const walk = (term: unknown) => {
    if (Array.isArray(term)) term.forEach(walk);
    else if (term && typeof term === "object") {
      const { propertyId, filters, groups } = term as { propertyId?: unknown; filters?: unknown; groups?: unknown };
      if (propertyId !== undefined) read.push(propertyId);
      walk(filters);
      walk(groups);
    }
  };
  walk([config.filterGroup, config.filters, config.quickFilters]);
  return read.every((id) => !readByOthers.has(types.get(String(id)) ?? ""));
}

function placedAgain(
  rows: readonly GraphRow[],
  inputs: readonly unknown[],
  queryOf: () => ReturnType<typeof viewRowQuery>,
  pageOf: (row: GraphRow, index: number) => Page,
): GraphRow[] | undefined {
  const before = workedOutBefore(rows, placedRowsKept);
  if (!before || before.value.inputs.length !== inputs.length || before.value.inputs.some((input, index) => input !== inputs[index])) return undefined;
  const query = queryOf();
  const places = placesById(rows, rowId);
  const entryOf = (row: GraphRow) => {
    const index = places.get(row.id) ?? -1;
    return query.entry(pageOf(row, index), index);
  };
  let placed = before.value.placed;
  const shownAt = placesById(placed, rowId);
  const changed = new Set(before.at.map((index) => rows[index].id));
  // Where it stood, between rows that didn't change and still before and after it.
  const standsAt = (at: number, entry: ReturnType<typeof entryOf>) => (at === 0 || (!changed.has(placed[at - 1].id) && query.compare(entryOf(placed[at - 1]), entry) < 0))
    && (at === placed.length - 1 || (!changed.has(placed[at + 1].id) && query.compare(entry, entryOf(placed[at + 1])) < 0));
  const inPlace = new Map<number, GraphRow>();
  let moves = false;
  for (const index of before.at) {
    const row = rows[index];
    if (before.from[index].id !== row.id) return undefined;
    const at = shownAt.get(row.id), kept = query.keeps(pageOf(row, index));
    if (at === undefined && !kept) continue;
    if (at !== undefined && kept && standsAt(at, entryOf(row))) inPlace.set(at, row);
    else moves = true;
  }
  if (!moves) placed = replaceItems(placed, inPlace);
  else {
    // The rows that didn't change stay in order; each changed one kept goes
    // where it sorts among them, the list kept as spliced (lib/rowsReplaced.ts).
    const shown = [...changed].flatMap((id) => shownAt.get(id) ?? []);
    const coming = new Map(before.at.flatMap((index) => query.keeps(pageOf(rows[index], index)) ? [[rows[index], entryOf(rows[index])] as const] : []));
    placed = moveItems(placed, shown, [...coming.keys()], (row, other) => query.compare(coming.get(row) ?? entryOf(row), coming.get(other) ?? entryOf(other)) < 0);
  }
  placedRowsKept.set(rows, { inputs, placed });
  return placed as GraphRow[];
}

// Each row's page as the view's filters and sorts read it, kept while the
// row, its title property and its place stay: a graph read again keeps its
// unchanged rows (lib/keptGraph.ts), and so their pages.
const presentedRows = new WeakMap<GraphRow, { titleProperty?: GraphProperty; workspaceId: string; position: number; page: Page }>();

function presentedRow(row: GraphRow, titleProperty: GraphProperty | undefined, workspaceId: string, position: number) {
  const kept = presentedRows.get(row);
  if (kept && kept.titleProperty === titleProperty && kept.workspaceId === workspaceId && kept.position === position) return kept.page;
  const page = galleryPresentationPage({ row, titleProperty, workspaceId, position });
  presentedRows.set(row, { titleProperty, workspaceId, position, page });
  return page;
}
