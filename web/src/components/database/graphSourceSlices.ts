import type { GraphProperty, GraphRow } from "@/contentGraph";
import { keptItems } from "@/lib/rowsReplaced";

/**
 * One data source's part of a graph's rows or properties, kept for each list
 * as read. A page draws its database blocks again whenever anything on it
 * changes — someone else's edit announced, a band's check, a keystroke
 * elsewhere — and a new list of the same 100,000 rows made each time had all
 * that the table works out from its rows (their pages, their order, their
 * identities) worked out again, some 1.3 seconds of the main thread each
 * time (2026-11-11). The same list read gives the same part, so those stay;
 * a list with rows changed in place gives its part changed alike, or the
 * list itself when it is all the source's (lib/rowsReplaced.ts).
 */
const slices = new Map<string, WeakMap<readonly object[], readonly object[]>>();

export function ofDataSource<T extends { dataSourceId: string; id: string }>(items: readonly T[], dataSourceId: string | undefined): T[] {
  const key = dataSourceId ?? "";
  let kept = slices.get(key) as WeakMap<readonly T[], readonly T[]> | undefined;
  if (!kept) {
    kept = new WeakMap();
    slices.set(key, kept as WeakMap<readonly object[], readonly object[]>);
  }
  return steady(`source:${key}`, items, kept, keptItems(items, kept, (item) => item.dataSourceId === dataSourceId, (item) => item.id)) as T[];
}

// The last parts given of each source, which a part read from a list made
// some other way (a row made, its list built anew) is when it holds the
// same items: the views of another database on the page then draw nothing
// again for it (2026-11-14: the other database's rows were a new list).
const recentParts = new Map<string, (readonly object[])[]>();

function steady<T extends object>(key: string, list: readonly T[], kept: WeakMap<readonly T[], readonly T[]>, part: readonly T[]): readonly T[] {
  const recent = (recentParts.get(key) ?? []) as (readonly T[])[];
  if (recent[0] === part) return part;
  const same = recent.find((shown) => shown.length === part.length && shown.every((item, index) => item === part[index]));
  if (same) {
    kept.set(list, same);
    return same;
  }
  recentParts.set(key, [part, ...recent].slice(0, RECENT_PARTS));
  return part;
}
const RECENT_PARTS = 3;

type SourceGraph = { readonly properties: readonly GraphProperty[]; readonly rows: readonly GraphRow[] };

// The sources read from each list of properties, by the sources read from.
const reachedBy = new WeakMap<readonly GraphProperty[], Map<string, string>>();

/**
 * The data sources whose rows a database's views read: its own and those its
 * relations relate to, however far (a rollup of a rollup, a formula naming
 * one), as a key of their ids.
 */
function sourcesRead(properties: readonly GraphProperty[], from: readonly string[]): string {
  const start = [...new Set(from.filter(Boolean))].sort().join("\u0000");
  let kept = reachedBy.get(properties);
  if (!kept) reachedBy.set(properties, kept = new Map());
  let key = kept.get(start);
  if (key !== undefined) return key;
  const reached = new Set(start ? start.split("\u0000") : []), waiting = [...reached];
  for (let id = waiting.pop(); id !== undefined; id = waiting.pop()) {
    for (const property of properties) {
      if (property.dataSourceId !== id || property.type !== "relation") continue;
      const target = typeof property.config?.relationDatabaseId === "string" ? property.config.relationDatabaseId : id;
      if (!reached.has(target)) { reached.add(target); waiting.push(target); }
    }
  }
  kept.set(start, key = [...reached].sort().join("\u0000"));
  return key;
}

const readSlices = new Map<string, WeakMap<readonly GraphRow[], readonly GraphRow[]>>();
const readGraphs = new WeakMap<readonly GraphProperty[], WeakMap<readonly GraphRow[], SourceGraph>>();

/**
 * The part of a page's graph a database's views read (sourcesRead), the same
 * while those rows are: a row made in another database on the page drew
 * this one's views again, every row of them (2026-11-14), as what they
 * filter, sort and show by was the page's whole graph.
 */
export function graphReadBy<G extends SourceGraph>(graph: G, sourceIds: readonly string[]): SourceGraph {
  const key = sourcesRead(graph.properties, sourceIds);
  let kept = readSlices.get(key);
  if (!kept) readSlices.set(key, kept = new WeakMap());
  const ids = new Set(key.split("\u0000"));
  const rows = steady(`read:${key}`, graph.rows, kept, keptItems(graph.rows, kept, (row) => ids.has(row.dataSourceId), (row) => row.id));
  let byRows = readGraphs.get(graph.properties);
  if (!byRows) readGraphs.set(graph.properties, byRows = new WeakMap());
  let read = byRows.get(rows);
  if (!read) byRows.set(rows, read = { properties: graph.properties, rows });
  return read;
}
