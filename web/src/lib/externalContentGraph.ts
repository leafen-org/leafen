import type { ContentGraph } from "@/contentGraph";
import { withChangedRows, type ContentGraphRowChanges } from "@/lib/authenticatedContentGraph";

/**
 * A page's graph carries rows of databases on other pages: the sources its
 * linked databases show and those its relations point to. They are the data
 * sources whose page is not this page; this page never writes them.
 */
export function externalDataSourceIds(graph: Pick<ContentGraph, "dataSources">, pageId: string): Set<string> {
  return new Set(graph.dataSources
    .filter((source) => source.pageId && source.pageId !== pageId)
    .map((source) => source.id));
}

/**
 * Takes the other-page sources, their properties and rows from a fresh read
 * and keeps everything of this page as it is, so pending local edits stay.
 * A source the fresh read no longer carries (access lost, link removed)
 * leaves with its rows.
 */
export function mergeExternalContentGraph(current: ContentGraph, fresh: ContentGraph, pageId: string): ContentGraph {
  return withExternalParts(current, fresh, pageId, true);
}

/**
 * Whether the changes read of the other pages a page shows rows of show it
 * other-page sources it doesn't hold the rows of, or no longer some it does
 * (a relation pointed elsewhere): its whole graph is read again.
 */
export function externalChangesShowOtherSources(current: ContentGraph, { databases }: ContentGraphRowChanges, pageId: string) {
  if (!databases) return false;
  const shown = externalDataSourceIds(current, pageId);
  const read = externalDataSourceIds(databases, pageId);
  return read.size !== shown.size || [...read].some((id) => !shown.has(id));
}

/**
 * `current` with the changes read of the other pages it shows rows of: their
 * rows changed where they stand (withChangedRows), so only those are worked
 * out again, and the other-page sources and properties taken from its
 * databases read when those changes changed them; this page's own are kept
 * as they are, so pending local edits stay.
 */
export function withExternalChanges(current: ContentGraph, changes: ContentGraphRowChanges, pageId: string): ContentGraph {
  const graph = withChangedRows(current, { rows: changes.rows, gone: changes.gone });
  return changes.databases ? withExternalParts(graph, changes.databases, pageId, false) : graph;
}

/** `current` with the other-page sources and properties of `fresh`, and its rows of them when `rows`. */
function withExternalParts(
  current: ContentGraph,
  fresh: Pick<ContentGraph, "dataSources" | "properties"> & Partial<Pick<ContentGraph, "rows">>,
  pageId: string,
  rows: boolean,
): ContentGraph {
  const external = externalDataSourceIds(current, pageId);
  for (const id of externalDataSourceIds(fresh, pageId)) external.add(id);
  if (external.size === 0) return current;
  const keep = <T,>(items: readonly T[], sourceOf: (item: T) => string) =>
    items.filter((item) => !external.has(sourceOf(item)));
  const take = <T,>(items: readonly T[], sourceOf: (item: T) => string) =>
    items.filter((item) => external.has(sourceOf(item)));
  return {
    ...current,
    dataSources: [...keep(current.dataSources, (source) => source.id), ...take(fresh.dataSources, (source) => source.id)],
    properties: [...keep(current.properties, (property) => property.dataSourceId), ...take(fresh.properties, (property) => property.dataSourceId)],
    ...(rows && fresh.rows ? { rows: [...keep(current.rows, (row) => row.dataSourceId), ...take(fresh.rows, (row) => row.dataSourceId)] } : {}),
  };
}
