import type { Dispatch, SetStateAction } from "react";
import { compareGraphOrder, type ContentGraph, type GraphOperation, type GraphProperty, type GraphRow } from "@/contentGraph";
import { afterCopiesMade } from "@/lib/copiesOnTheirWay";

function operationDataSourceId(graph: ContentGraph, operation: GraphOperation) {
  if (operation.kind === "upsertDataSource") return operation.dataSource.id;
  if (operation.kind === "deleteDataSource") return operation.dataSourceDeletion.id;
  if (operation.kind === "upsertView") return operation.view.dataSourceId;
  if (operation.kind === "updateView") return operation.viewUpdate.view.dataSourceId;
  if (operation.kind === "deleteView") {
    return graph.views.find(({ id }) => id === operation.viewDeletion.id)?.dataSourceId;
  }
  if (operation.kind === "upsertProperty") return operation.property.dataSourceId;
  if (operation.kind === "deleteProperty") {
    return graph.properties.find(({ id }) => id === operation.propertyDeletion.id)?.dataSourceId;
  }
  if (operation.kind === "upsertRow") return operation.row.dataSourceId;
  if (operation.kind === "deleteRow") {
    return graph.rows.find(({ id }) => id === operation.rowDeletion.id)?.dataSourceId;
  }
  if (operation.kind === "configureDependency") return operation.dependencyBinding.dataSourceId;
  if (operation.kind === "upsertDependencyEdge") return operation.dependencyEdge.dataSourceId;
  if (operation.kind === "deleteDependencyEdge") return operation.dependencyEdgeDeletion.dataSourceId;
  return undefined;
}

export function graphDataSourceAuthorityPageId(
  graph: ContentGraph,
  fallbackPageId: string | null,
  dataSourceId: string,
) {
  return graph.dataSources.find(({ id }) => id === dataSourceId)?.pageId ?? fallbackPageId;
}

// A linked database's views live on the page that shows it, not with the source.
function operationOwnedViewContainerId(graph: ContentGraph, operation: GraphOperation) {
  if (operation.kind === "upsertView") return operation.view.containerId;
  if (operation.kind === "updateView") return operation.viewUpdate.view.containerId;
  if (operation.kind === "deleteView") {
    return graph.views.find(({ id }) => id === operation.viewDeletion.id)?.containerId;
  }
  return undefined;
}

// The page a write belongs to; none for one that names no source or view.
function operationAuthorityPageId(graph: ContentGraph, fallbackPageId: string | null, operation: GraphOperation) {
  if (operationOwnedViewContainerId(graph, operation)) return fallbackPageId;
  const dataSourceId = operationDataSourceId(graph, operation);
  return dataSourceId ? graphDataSourceAuthorityPageId(graph, fallbackPageId, dataSourceId) : null;
}

export function graphOperationsAuthorityPageId(
  graph: ContentGraph,
  fallbackPageId: string | null,
  operations: readonly GraphOperation[],
) {
  const pages = new Set(operations.flatMap((operation) => operationAuthorityPageId(graph, fallbackPageId, operation) ?? []));
  if (pages.size > 1) return null;
  return pages.values().next().value ?? fallbackPageId;
}

/**
 * The writes in the order made, in runs by the page each belongs to; one
 * naming no page goes with the run before it, or the first. A change that
 * spans a source's page and a page linking it is written page by page: a
 * linked database's new board writes its status on the source's page, then
 * the view on the page showing it, as Notion makes both (seen 2026-11-11).
 * Null where no write has a page.
 */
export function graphOperationRuns(
  graph: ContentGraph,
  fallbackPageId: string | null,
  operations: readonly GraphOperation[],
) {
  const runs: { pageId: string; operations: GraphOperation[] }[] = [];
  let leading: GraphOperation[] = [];
  for (const operation of operations) {
    const pageId = operationAuthorityPageId(graph, fallbackPageId, operation), last = runs.at(-1);
    if (!pageId && !last) leading.push(operation);
    else if (!pageId || last?.pageId === pageId) last!.operations.push(operation);
    else { runs.push({ pageId, operations: [...leading, operation] }); leading = []; }
  }
  if (!runs.length) return fallbackPageId ? [{ pageId: fallbackPageId, operations: leading }] : null;
  return runs;
}

/**
 * Sends each page its run of the writes in turn, stopping at the first
 * refused (graphOperationRuns); writes to copies drawn here go once the
 * server has made them (copiesOnTheirWay.ts).
 *
 * A run sent only once another was answered, or once copies were made, is
 * written as made on `graph`, the graph the write was made against
 * (`madeOn`): the screen shows the write by then (saveShownAtOnce), so read
 * from it, a view the write makes would go as a change from itself and one
 * it changes from its changed self (2026-11-18: a linked database's new
 * board, its "상태" written on its source's page first, went as a change of
 * the board from the board and was refused, every time).
 *
 * Each run of a write sent over more than one page is told the whole
 * change it is part of (`change`), whose step of ⌘Z is one, as Notion's:
 * a linked database's new board went at one ⌘Z with the "상태" made on its
 * source, and came back with it at ⌘⇧Z (seen 2026-11-18).
 */
export async function persistGraphOperationRuns(
  graph: ContentGraph,
  fallbackPageId: string | null,
  operations: readonly GraphOperation[],
  persistPage: (pageId: string, operations: GraphOperation[], madeOn?: ContentGraph, change?: GraphOperationChange) => Promise<boolean>,
) {
  const runs = graphOperationRuns(graph, fallbackPageId, operations);
  if (!runs) return false;
  const copiesMade = afterCopiesMade(operations);
  if (copiesMade) await copiesMade;
  let waited = Boolean(copiesMade);
  for (const [index, run] of runs.entries()) {
    const change = runs.length > 1 ? { graph, pageId: fallbackPageId, operations, last: index === runs.length - 1 } : undefined;
    if (!await persistPage(run.pageId, run.operations, waited ? graph : undefined, change)) return false;
    waited = true;
  }
  return true;
}

/**
 * A write sent page by page as one change (persistGraphOperationRuns): the
 * graph and page it was made on, all its writes, and whether this run is its
 * last, sent once every run before it was taken.
 */
export type GraphOperationChange = {
  graph: ContentGraph;
  pageId: string | null;
  operations: readonly GraphOperation[];
  last: boolean;
};

/**
 * Saves a setting's change and shows it at once, as Notion's settings do,
 * so one made right after builds on it: the writes still go out in the
 * order made, and one the server refuses reads the graph again. What is
 * sent is always shown, by its ids, even when the screen that made it has
 * drawn again since: a board's card moved in a view keeping its own order
 * wrote the view after its row, from the screen before the row moved, and
 * the view sent but never shown made the next card's view write start from
 * the old order and be refused (2026-11-07, 409 on two quick moves). It
 * answers whether it was saved while the place it was made in is current.
 */
export async function saveShownAtOnce(
  graph: ContentGraph,
  fallbackPageId: string | null,
  operations: GraphOperation[],
  persist: (operations: GraphOperation[]) => Promise<boolean>,
  isCurrent: () => boolean,
  show: () => void,
) {
  if (!graphOperationRuns(graph, fallbackPageId, operations)) return false;
  const saved = persist(operations);
  show();
  return await saved && isCurrent();
}

/**
 * Saves rows' changed values (and new places) and shows them at once, as
 * Notion shows an edited cell: an edit made before the server answers the
 * last one builds on it, not on the row as it was, so two options picked in
 * a row both stay. As with a setting, the writes go out in the order made
 * and one the server refuses reads the graph again. A row whose values
 * change shows itself last edited now, by the person editing, as Notion's
 * "최종 편집 일시" and "최종 편집자" turn within two seconds (seen
 * 2026-11-09); the server keeps its own time.
 */
export function saveRowsShownAtOnce(
  graph: ContentGraph,
  fallbackPageId: string | null,
  rows: readonly GraphRow[],
  persist: (operations: GraphOperation[]) => Promise<boolean>,
  isCurrent: () => boolean,
  setGraph: Dispatch<SetStateAction<ContentGraph>>,
  editedBy?: string,
) {
  const byId = new Map(rows.map((row) => [row.id, row]));
  const moved = rows.some((row) => graph.rows.find(({ id }) => id === row.id)?.position !== row.position);
  return saveShownAtOnce(graph, fallbackPageId, rows.map((row) => ({ kind: "upsertRow", row })), persist, isCurrent, () => {
    const now = new Date().toISOString();
    const edited = (row: GraphRow) => ({ ...row, updatedAt: now, ...(editedBy ? { lastEditedBy: editedBy } : {}) });
    setGraph((current) => {
      const mirrored = mirroredRelationValues(current, byId);
      const next = current.rows.map((item) => {
        const change = byId.get(item.id);
        const mirror = mirrored.get(item.id);
        const merged = change ? { ...item, ...change, values: { ...item.values, ...change.values } } : item;
        // A card moved within its group keeps its values, and its last edit.
        const changed = change && Object.entries(change.values).some(([id, cell]) => JSON.stringify(cell) !== JSON.stringify(item.values[id]));
        const shown = changed ? edited(merged) : merged;
        return mirror ? { ...edited(shown), values: { ...shown.values, ...mirror } } : shown;
      });
      return { ...current, rows: moved ? next.sort(compareGraphOrder) : next };
    });
  });
}

/**
 * The property a relation's other side is written in: its pair while that
 * names it back (a side another relation pairs with is that one's), or itself
 * when symmetric.
 */
function relationOtherSide(property: GraphProperty, propertiesById: ReadonlyMap<string, GraphProperty>) {
  const pairId = property.config.relatedPropertyId;
  if (typeof pairId === "string" && pairId !== "" && pairId !== property.id) {
    const pair = propertiesById.get(pairId);
    return pair?.config.relatedPropertyId === property.id ? pair : undefined;
  }
  const target = property.config.relationDatabaseId;
  return property.config.relationSymmetric === true && target === property.dataSourceId ? property : undefined;
}

function relationIds(value: unknown): string[] {
  return Array.isArray(value) ? value.filter((id): id is string => typeof id === "string") : [];
}

/**
 * A two-way relation's other side as the server will write it (its pair
 * runner): a page linked here gains this row in the paired property, one
 * unlinked loses it, shown at once on the pages this graph holds so every
 * view of them agrees, as Notion's do (seen 2026-11-02 within two seconds).
 * A symmetric relation to its own database is its own other side (seen
 * 2026-11-12); a row related to itself is its own. A side limited to one page
 * takes this row in place of the page it held, which leaves it on its own
 * side, as the server keeps it (database_relation_limit.go).
 */
function mirroredRelationValues(graph: ContentGraph, changes: ReadonlyMap<string, GraphRow>) {
  const mirrored = new Map<string, GraphRow["values"]>();
  const rowsById = new Map(graph.rows.map((row) => [row.id, row]));
  const propertiesById = new Map(graph.properties.map((property) => [property.id, property]));
  const idsOf = (rowId: string, propertyId: string) => relationIds((mirrored.get(rowId)?.[propertyId]
    ?? changes.get(rowId)?.values[propertyId] ?? rowsById.get(rowId)?.values[propertyId])?.value);
  const write = (rowId: string, propertyId: string, ids: string[]) => {
    const values = mirrored.get(rowId) ?? {};
    values[propertyId] = { type: "relation", value: ids };
    mirrored.set(rowId, values);
  };
  for (const change of changes.values()) {
    const before = rowsById.get(change.id);
    for (const [propertyId, cell] of Object.entries(change.values)) {
      const property = propertiesById.get(propertyId);
      const pair = property?.type === "relation" ? relationOtherSide(property, propertiesById) : undefined;
      if (!property || !pair || pair.type !== "relation") continue;
      const was = new Set(relationIds(before?.values[propertyId]?.value));
      const now = new Set(relationIds(cell?.value));
      for (const targetId of new Set([...was, ...now])) {
        if (was.has(targetId) === now.has(targetId)) continue;
        const target = rowsById.get(targetId);
        if (!target || target.id === change.id || target.dataSourceId !== pair.dataSourceId) continue;
        const held = idsOf(targetId, pair.id);
        const ids = held.filter((id) => id !== change.id);
        if (!now.has(targetId)) {
          write(targetId, pair.id, ids);
          continue;
        }
        const letGo = pair.config.limit === 1 && !held.includes(change.id) ? ids : [];
        write(targetId, pair.id, [...ids.filter((id) => !letGo.includes(id)), change.id]);
        for (const goneId of letGo) {
          if (goneId === change.id || !rowsById.has(goneId)) continue;
          write(goneId, property.id, idsOf(goneId, property.id).filter((id) => id !== targetId));
        }
      }
    }
  }
  return mirrored;
}
