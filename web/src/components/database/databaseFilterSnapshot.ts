import type {
  DbProperty,
  FilterGroup,
  OrganizationProfile,
  Page,
  WorkspaceMember,
} from "@/lib/types";
import type { GraphProperty, GraphRow } from "@/contentGraph";
import { graphRowPropertySpans, graphRowTitleSpans } from "./richTextCellModel";
import { lookupById, lookupList } from "@/lib/keptLookup";
import { madeBetween, placesById, replaceItems, spliceItems, splicedBefore, workedOutBefore } from "@/lib/rowsReplaced";

export type DatabaseFilterGraph = {
  properties: readonly GraphProperty[];
  rows: readonly GraphRow[];
};

export interface DatabaseFilterSnapshot {
  snapshotIdentity: string;
  /** The workspace the database is in. */
  workspaceId?: string;
  currentUserId: string;
  organizationProfiles: readonly OrganizationProfile[];
  workspaceMembers: readonly WorkspaceMember[];
  pagesById: Readonly<Record<string, Page | undefined>>;
  rowsByDatabase: Readonly<Record<string, readonly Page[] | undefined>>;
  propertiesByDatabase: Readonly<Record<string, readonly DbProperty[] | undefined>>;
}

export interface DatabaseFilterReadRequest {
  schemaDatabaseIds: string[];
  rowDatabaseIds: string[];
}

export interface DatabaseFilterReadContext {
  contextIdentity: string;
  requestGeneration: number;
  snapshotIdentity: string;
  signal: AbortSignal;
}

export interface DatabaseFilterReadBoundary {
  contextIdentity: string;
  requestGeneration: number;
  signal: AbortSignal;
  isCurrent: (context: DatabaseFilterReadContext) => boolean;
  ensureDatabases: (
    context: DatabaseFilterReadContext,
    request: DatabaseFilterReadRequest,
  ) => Promise<void>;
  onError: (error: unknown) => void;
}

// A database's rows as read are projected once for each read of them,
// whichever parts of its view ask — the view, its toolbar, its table, their
// cells: a page of 100,000 rows wrote them out as text six times and made
// their pages five times once its graph was read, some 1.5 of the 5 seconds
// before its table drew (2026-11-11). Each list of rows, as read, keeps them.
type FilterGraphProjection = Pick<DatabaseFilterSnapshot, "pagesById" | "rowsByDatabase" | "propertiesByDatabase">;
const projections = new WeakMap<readonly GraphRow[], { properties: readonly GraphProperty[]; workspaceId: string; value: FilterGraphProjection }>();
const hydratedTargets = new WeakMap<readonly GraphRow[], { properties: readonly GraphProperty[]; value: ReadonlySet<string> }>();

// What the rows hold as two 32-bit sums of each row's text's hash mixed with
// its place, kept for each row as read and each list (lib/keptGraph.ts keeps
// the unchanged rows of a graph read again): a list with rows changed in
// place (lib/rowsReplaced.ts) is summed again only for them. Written out as
// one text, 100,000 rows made 15 MB of text each time one changed, and that
// text was compared wherever it was asked whether they did (2026-11-11).
const rowHashes = new WeakMap<GraphRow, readonly [number, number]>();
const rowsSums = new WeakMap<readonly GraphRow[], readonly [number, number]>();

function mixed(value: number) {
  value ^= value >>> 16;
  value = Math.imul(value, 0x7feb352d);
  value ^= value >>> 15;
  value = Math.imul(value, 0x846ca68b);
  return (value ^ (value >>> 16)) >>> 0;
}

function rowHash(row: GraphRow) {
  let hash = rowHashes.get(row);
  if (!hash) {
    const text = JSON.stringify(row);
    let first = 0x811c9dc5, second = 0x9e3779b9;
    for (let index = 0; index < text.length; index += 1) {
      const unit = text.charCodeAt(index);
      first = Math.imul(first ^ unit, 0x01000193);
      second = Math.imul(second + unit, 0x5bd1e995) ^ (second >>> 13);
    }
    hash = [mixed(first), mixed(second)];
    rowHashes.set(row, hash);
  }
  return hash;
}

function rowTerm(row: GraphRow, place: number, lane: 0 | 1) {
  return mixed(rowHash(row)[lane] ^ mixed(place + (lane ? 0x632be5ab : 0x9e3779b9)));
}

/** What the rows hold, as one text, which changes only when they do. */
export function graphRowsIdentity(rows: readonly GraphRow[]) {
  let sums = rowsSums.get(rows);
  if (!sums) {
    const before = workedOutBefore(rows, rowsSums);
    let first = 0, second = 0;
    if (before) {
      [first, second] = before.value;
      for (const place of before.at) {
        first = (first - rowTerm(before.from[place], place, 0) + rowTerm(rows[place], place, 0)) >>> 0;
        second = (second - rowTerm(before.from[place], place, 1) + rowTerm(rows[place], place, 1)) >>> 0;
      }
    } else {
      rows.forEach((row, place) => {
        first = (first + rowTerm(row, place, 0)) >>> 0;
        second = (second + rowTerm(row, place, 1)) >>> 0;
      });
    }
    sums = [first, second];
    rowsSums.set(rows, sums);
  }
  return `rows:${rows.length}:${sums[0].toString(36)}:${sums[1].toString(36)}`;
}

/** What a view's properties and rows hold, as one text, which changes only when they do. */
export function databaseFilterGraphIdentity(graph: DatabaseFilterGraph) {
  return `${JSON.stringify(graph.properties)}\n${graphRowsIdentity(graph.rows)}`;
}

export function nativeDatabaseFilterSnapshot({
  currentUserId,
  graph,
  snapshotIdentity,
  workspaceId,
}: {
  currentUserId: string;
  graph: DatabaseFilterGraph;
  snapshotIdentity: string;
  workspaceId: string;
}): DatabaseFilterSnapshot {
  let projection = projections.get(graph.rows);
  if (projection?.properties !== graph.properties || projection.workspaceId !== workspaceId) {
    // Rows changed in place change their pages alone (lib/rowsReplaced.ts);
    // the same rows read with properties changed but their titles' (a name,
    // an option) keep their pages, which read only the titles.
    // Rows taken out and put in (a copy, rows to the trash and back) take
    // their pages out and put theirs in, the others' kept.
    const before = workedOutBefore(graph.rows, projections);
    const spliced = before ? undefined : splicedBefore(graph.rows, projections);
    const madeAlike = (kept: { properties: readonly GraphProperty[]; workspaceId: string }) => kept.properties === graph.properties && kept.workspaceId === workspaceId;
    const patched = before && madeAlike(before.value) ? patchedFilterGraph(before.value.value, before, graph, workspaceId)
      : spliced && madeAlike(spliced.value) ? splicedFilterGraph(spliced.value.value, spliced, graph, workspaceId) : undefined;
    const kept = !patched && projection?.workspaceId === workspaceId && sameTitles(projection.properties, graph.properties)
      ? { ...projection.value, propertiesByDatabase: filterProperties(graph.properties).propertiesByDatabase } : undefined;
    projection = { properties: graph.properties, workspaceId, value: patched ?? kept ?? projectFilterGraph(graph, workspaceId) };
    projections.set(graph.rows, projection);
  }
  return {
    snapshotIdentity,
    workspaceId,
    currentUserId,
    organizationProfiles: [],
    workspaceMembers: [],
    ...projection.value,
  };
}

// The properties by database as filters read them, and each database's title property.
function filterProperties(properties: readonly GraphProperty[]) {
  const propertiesByDatabase: Record<string, DbProperty[]> = {};
  const titleProperties = new Map<string, GraphProperty>();
  properties.forEach((property, position) => {
    const projected: DbProperty = {
      id: property.id,
      databaseId: property.dataSourceId,
      name: property.name,
      position,
      type: property.type,
      config: property.config as DbProperty["config"],
    };
    (propertiesByDatabase[property.dataSourceId] ??= []).push(projected);
    if (property.type === "title") titleProperties.set(property.dataSourceId, property);
  });
  return { propertiesByDatabase, titleProperties };
}

// Whether each database's title property is the one it was (a graph read
// again keeps the properties that didn't change, lib/keptGraph.ts).
function sameTitles(before: readonly GraphProperty[], after: readonly GraphProperty[]) {
  const titles = (properties: readonly GraphProperty[]) => properties.filter((property) => property.type === "title");
  const was = titles(before), now = titles(after);
  return was.length === now.length && now.every((property, index) => property === was[index]);
}

function projectFilterGraph(graph: DatabaseFilterGraph, workspaceId: string): FilterGraphProjection {
  const { propertiesByDatabase, titleProperties } = filterProperties(graph.properties);

  const rowsByDatabase: Record<string, Page[]> = {};
  const pages = graph.rows.map((row, position) => {
    const titleProperty = titleProperties.get(row.dataSourceId);
    const page = filterRowPage(row, titleProperty, workspaceId, position);
    (rowsByDatabase[row.dataSourceId] ??= []).push(page);
    return page;
  });
  // The pages by ID follow the list of them (lib/keptLookup.ts).
  return { pagesById: lookupById(pages), rowsByDatabase, propertiesByDatabase };
}

function patchedFilterGraph(
  kept: FilterGraphProjection,
  { from, at }: { from: readonly GraphRow[]; at: readonly number[] },
  graph: DatabaseFilterGraph,
  workspaceId: string,
): FilterGraphProjection | undefined {
  const ordered = lookupList<Page>(kept.pagesById);
  if (!ordered) return undefined;
  const changed = new Map<string, Map<number, Page>>();
  const pages = new Map<number, Page>();
  for (const place of at) {
    const was = from[place], row = graph.rows[place];
    const list = kept.rowsByDatabase[row.dataSourceId];
    const index = was.id === row.id && was.dataSourceId === row.dataSourceId && list ? placesById(list, pageId).get(row.id) : undefined;
    if (index === undefined) return undefined;
    const titleProperty = graph.properties.find((property) => property.dataSourceId === row.dataSourceId && property.type === "title");
    // In the place its page held (splicedFilterGraph).
    const page = filterRowPage(row, titleProperty, workspaceId, ordered[place]?.position ?? place);
    pages.set(place, page);
    if (!changed.has(row.dataSourceId)) changed.set(row.dataSourceId, new Map());
    changed.get(row.dataSourceId)!.set(index, page);
  }
  // The pages by ID follow the list with those changed (lib/keptLookup.ts).
  const pagesById = lookupById(replaceItems(ordered, pages));
  const rowsByDatabase = { ...kept.rowsByDatabase };
  for (const [dataSourceId, replaced] of changed) rowsByDatabase[dataSourceId] = replaceItems(kept.rowsByDatabase[dataSourceId]!, replaced);
  return { pagesById, rowsByDatabase, propertiesByDatabase: kept.propertiesByDatabase };
}

const pageId = (page: Page) => page.id;

// The pages of a list of rows some were taken out of and put in: those put
// in made, each between its neighbours' places (madeBetween), the others
// kept, and each database's own taken out and put in alike. Made
// again, every page after a copy took a new place and every cell relating
// to one was drawn again (2026-11-14: 56 relation chips and 60 relation and
// rollup cells for a copy of one row, and the properties each cell reads
// were made again too).
function splicedFilterGraph(
  kept: FilterGraphProjection,
  { from, removed, added }: { from: readonly GraphRow[]; removed: readonly number[]; added: readonly number[] },
  graph: DatabaseFilterGraph,
  workspaceId: string,
): FilterGraphProjection | undefined {
  const ordered = lookupList<Page>(kept.pagesById);
  if (!ordered || ordered.length !== from.length) return undefined;
  const shown = spliceItems<Page | null>(ordered, removed, added.map((index) => [index, null] as const));
  const titles = filterProperties(graph.properties).titleProperties;
  const made = madeBetween(shown, (index, position) => filterRowPage(graph.rows[index], titles.get(graph.rows[index].dataSourceId), workspaceId, position));
  // Each database's pages: those taken out found where they stand, those put in placed among them by their places.
  const out = new Map<string, number[]>(), into = new Map<string, [number, Page][]>();
  for (const index of removed) {
    const dataSourceId = from[index].dataSourceId, list = kept.rowsByDatabase[dataSourceId];
    const at = list ? placesById(list, pageId).get(ordered[index].id) : undefined;
    if (at === undefined || list![at] !== ordered[index]) return undefined;
    (out.get(dataSourceId) ?? out.set(dataSourceId, []).get(dataSourceId)!).push(at);
  }
  for (const index of added) {
    const page = made.get(index)!, dataSourceId = graph.rows[index].dataSourceId, list = kept.rowsByDatabase[dataSourceId] ?? [];
    let low = 0, high = list.length;
    while (low < high) {
      const middle = (low + high) >>> 1;
      if (list[middle].position < page.position) low = middle + 1;
      else high = middle;
    }
    const gone = (out.get(dataSourceId) ?? []).filter((at) => at < low).length;
    const coming = into.get(dataSourceId) ?? into.set(dataSourceId, []).get(dataSourceId)!;
    coming.push([low - gone + coming.length, page]);
  }
  const rowsByDatabase = { ...kept.rowsByDatabase };
  for (const dataSourceId of new Set([...out.keys(), ...into.keys()])) {
    const list = spliceItems(kept.rowsByDatabase[dataSourceId] ?? [], (out.get(dataSourceId) ?? []).sort((left, right) => left - right),
      into.get(dataSourceId) ?? []);
    if (list.length) rowsByDatabase[dataSourceId] = list;
    else delete rowsByDatabase[dataSourceId];
  }
  // The pages by ID follow the list taken out of and put in (lib/keptLookup.ts).
  const pagesById = lookupById(spliceItems(ordered, removed, added.map((index) => [index, made.get(index)!] as const)));
  return { pagesById, rowsByDatabase, propertiesByDatabase: kept.propertiesByDatabase };
}

// Each row's page, kept while the row, its title property and its place
// stay: a graph read again keeps its unchanged rows (lib/keptGraph.ts), and
// so their pages.
const filterRowPages = new WeakMap<GraphRow, { titleProperty?: GraphProperty; workspaceId: string; position: number; page: Page }>();

function filterRowPage(row: GraphRow, titleProperty: GraphProperty | undefined, workspaceId: string, position: number): Page {
  const kept = filterRowPages.get(row);
  if (kept && kept.titleProperty === titleProperty && kept.workspaceId === workspaceId && kept.position === position) return kept.page;
  const titleValue = titleProperty ? row.values[titleProperty.id]?.value : "";
  const page: Page = {
    id: row.id,
    workspaceId,
    parentId: row.dataSourceId,
    parentType: "database",
    kind: "page",
    title: Array.isArray(titleValue) ? titleValue.join(", ") : String(titleValue ?? ""),
    icon: row.icon,
    iconType: row.iconType ?? "none",
    hasContent: row.hasContent,
    properties: Object.fromEntries(
      Object.entries(row.values).map(([propertyId, cell]) => [propertyId, cell.value]),
    ),
    propertySpans: graphRowPropertySpans(row),
    titleSpans: graphRowTitleSpans(row),
    subitemParentId: row.subitemParentId ?? undefined,
    archivedAt: row.archivedAt || undefined,
    archivedBy: row.archivedBy || undefined,
    createdAt: row.createdAt || undefined,
    createdBy: row.createdBy || undefined,
    updatedAt: row.updatedAt || undefined,
    lastEditedBy: row.lastEditedBy || undefined,
    position,
  };
  filterRowPages.set(row, { titleProperty, workspaceId, position, page });
  return page;
}

export function nativeDatabaseHydratedRelationTargetIds(graph: DatabaseFilterGraph): ReadonlySet<string> {
  const kept = hydratedTargets.get(graph.rows);
  if (kept?.properties === graph.properties) return kept.value;
  const relationPropertyIds = new Set(
    graph.properties.filter(({ type }) => type === "relation").map(({ id }) => id),
  );
  // The same rows read with the same relations, whatever else of their
  // properties changed (a name), name the same pages.
  if (kept && sameIds(relationIdsOf(kept.properties), relationPropertyIds)) {
    hydratedTargets.set(graph.rows, { properties: graph.properties, value: kept.value });
    return kept.value;
  }
  // Rows changed in place naming the same pages name what the rows did
  // (lib/rowsReplaced.ts).
  const before = workedOutBefore(graph.rows, hydratedTargets);
  const named = (row: GraphRow) => [...addRelationTargetIds(row.values, relationPropertyIds, new Set([row.id]))].sort().join("\u0000");
  if (before?.value.properties === graph.properties
    && before.at.every((place) => named(before.from[place]) === named(graph.rows[place]))) {
    hydratedTargets.set(graph.rows, before.value);
    return before.value.value;
  }
  const targetIds = new Set(graph.rows.map(({ id }) => id));
  for (const row of graph.rows) addRelationTargetIds(row.values, relationPropertyIds, targetIds);
  hydratedTargets.set(graph.rows, { properties: graph.properties, value: targetIds });
  return targetIds;
}

function relationIdsOf(properties: readonly GraphProperty[]) {
  return new Set(properties.filter(({ type }) => type === "relation").map(({ id }) => id));
}

function sameIds(left: ReadonlySet<string>, right: ReadonlySet<string>) {
  if (left.size !== right.size) return false;
  for (const id of left) if (!right.has(id)) return false;
  return true;
}

/**
 * The pages a row's relation cells name, the row's read having come with
 * them: read by its properties' types, whatever type a cell was kept with,
 * and a value kept as one id being one page, as a cell reads them
 * (rollup.ts valueAsIds) — so a page whose read has come tells a target it
 * may not read from one still on its way the same way in every view
 * (2026-11-08: a relation whose value was left a text's by a type change
 * stayed a "loading" box for good on its row's page, where the table said
 * "사용 권한 없음").
 */
export function addRelationTargetIds(
  values: Readonly<Record<string, { value?: unknown } | undefined>>,
  relationPropertyIds: ReadonlySet<string>,
  into: Set<string>,
) {
  for (const [propertyId, cell] of Object.entries(values)) {
    if (!relationPropertyIds.has(propertyId) || !cell) continue;
    const ids = Array.isArray(cell.value) ? cell.value : cell.value == null ? [] : [cell.value];
    ids.forEach((id) => into.add(String(id)));
  }
  return into;
}

function targetDatabaseId(property: DbProperty | undefined) {
  if (!property || property.type !== "relation") return undefined;
  return property.config?.relationDatabaseId ?? property.databaseId;
}

export function databaseFilterReadRequest(
  group: FilterGroup,
  properties: readonly DbProperty[],
  snapshot: Pick<DatabaseFilterSnapshot, "propertiesByDatabase">,
): DatabaseFilterReadRequest {
  const propertiesById = new Map(properties.map((property) => [property.id, property]));
  const schemaDatabaseIds = new Set<string>();
  const rowDatabaseIds = new Set<string>();

  function visit(current: FilterGroup) {
    for (const filter of current.filters) {
      const property = propertiesById.get(filter.propertyId);
      if (property?.type === "relation") {
        const databaseId = targetDatabaseId(property);
        if (databaseId && databaseId !== property.databaseId) rowDatabaseIds.add(databaseId);
        continue;
      }
      if (property?.type !== "rollup") continue;

      const relation = propertiesById.get(property.config?.rollupRelationPropertyId ?? "");
      const firstHopDatabaseId = targetDatabaseId(relation);
      if (!firstHopDatabaseId) continue;
      if (firstHopDatabaseId !== property.databaseId) schemaDatabaseIds.add(firstHopDatabaseId);

      const target = snapshot.propertiesByDatabase[firstHopDatabaseId]?.find(
        (candidate) => candidate.id === property.config?.rollupTargetPropertyId,
      );
      const relationTargetDatabaseId = target
        ? targetDatabaseId(target)
        : firstHopDatabaseId;
      if (relationTargetDatabaseId && relationTargetDatabaseId !== property.databaseId) {
        rowDatabaseIds.add(relationTargetDatabaseId);
      }
    }
    for (const child of current.groups ?? []) visit(child);
  }

  visit(group);
  return {
    schemaDatabaseIds: Array.from(schemaDatabaseIds).sort(),
    rowDatabaseIds: Array.from(rowDatabaseIds).sort(),
  };
}

export function databaseFilterReadContext(
  contextIdentity: string,
  requestGeneration: number,
  snapshot: Pick<DatabaseFilterSnapshot, "snapshotIdentity">,
  signal: AbortSignal,
): DatabaseFilterReadContext {
  return {
    contextIdentity,
    requestGeneration,
    snapshotIdentity: snapshot.snapshotIdentity,
    signal,
  };
}

export function createDatabaseFilterSingleFlight() {
  const inFlight = new Map<string, Promise<unknown>>();
  return function singleFlight<T>(key: string, run: () => Promise<T>): Promise<T> {
    const existing = inFlight.get(key);
    if (existing) return existing as Promise<T>;
    const pending = Promise.resolve().then(run).finally(() => {
      if (inFlight.get(key) === pending) inFlight.delete(key);
    });
    inFlight.set(key, pending);
    return pending;
  };
}
