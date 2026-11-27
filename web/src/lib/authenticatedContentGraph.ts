import { api, throwInvalidContentGraphContinuation } from "@/api";
import { request } from "@/apiTransport";
import {
  compareGraphOrder,
  graphWireBounds,
  materializeContentGraph,
  type ContentGraph,
  type GraphBlock,
  type GraphReadResponse,
  type GraphRow,
} from "@/contentGraph";
import { collectContentGraphPages } from "@/lib/contentGraphCollection";
import { rememberDataSourceUntitledRowNames } from "@/lib/untitledRowNames";
import { placesById, replaceItems } from "@/lib/rowsReplaced";
import { noteWordsRead } from "@/lib/pageWordsWithBlocks";
import type { Session } from "@/types";

/** A read graph, its databases' untitled rows' names remembered (pageDisplayTitle). */
function readGraph(items: Parameters<typeof materializeContentGraph>[0]) {
  const graph = materializeContentGraph(items);
  rememberDataSourceUntitledRowNames(graph.dataSources);
  return graph;
}

/**
 * Reads a page's whole graph a window at a time. `onSnapshot` sees the graph
 * as read so far after each window; null when only the whole one is wanted,
 * which is then made once (2026-11-11: a page of 100,000 rows made its graph
 * anew after each of its thousand windows, though nothing looked at it).
 */
export function collectAuthenticatedContentGraph(
  session: Session,
  pageId: string,
  onSnapshot: ((graph: ContentGraph) => void) | null,
  shouldContinue = () => true,
) {
  return collectGraph(session, pageId, onSnapshot, shouldContinue, true);
}

/**
 * A page's whole graph as its body opens, with its words as they stood when
 * its first window was read (content_graph_words.go): drawn with its blocks,
 * the page's text and blocks are of one moment.
 */
export function collectOpenedPageGraph(session: Session, pageId: string, shouldContinue = () => true) {
  noteWordsRead(pageId);
  return collectGraph(session, pageId, null, shouldContinue, true, null, false, true);
}

/**
 * A page's graph without its databases' rows: its blocks and its databases'
 * sources, views and properties, which a few windows hold whatever the rows
 * (2026-11-11: a row's page read its database of 100,000 rows, a thousand
 * windows, for its source's name).
 */
export function collectContentGraphStructure(session: Session, pageId: string, shouldContinue = () => true) {
  return collectGraph(session, pageId, null, shouldContinue, false);
}

/**
 * A page's graph without its databases' rows, its text blocks holding their
 * words: the page drawn read only without opening its collaborative
 * document, as the layout builder's "콘텐츠" shows it.
 */
export function collectContentGraphWithText(session: Session, pageId: string, shouldContinue = () => true) {
  return collectGraph(session, pageId, null, shouldContinue, false, null, true);
}

/** A page's databases' containers, sources, views and properties. */
type ContentGraphDatabases = Pick<ContentGraph, "databaseContainers" | "dataSources" | "views" | "properties">;

/**
 * The rows a page's changes after a position changed, those it no longer
 * shows, and its databases and its blocks as they are now when the changes
 * changed them.
 */
export type ContentGraphRowChanges = { rows: GraphRow[]; gone: string[]; databases?: ContentGraphDatabases; blocks?: GraphBlock[] };

/**
 * What a page's changes after `since` changed: its rows those changes alone
 * changed, its databases' sources, views and properties whole when they
 * changed those, and its blocks whole when they changed those, when they say
 * so; or else its whole graph (2026-11-11: a page of 100,000 rows read its
 * thousand windows again, 15–46 seconds, for one value another person typed,
 * and again for a sort they set; 2026-11-25: one paragraph added there, some
 * 7 seconds on a NAS-class server for a page of 5,000 rows).
 */
export function collectContentGraphChanges(session: Session, pageId: string, since: number, shouldContinue = () => true) {
  return collectChanges(session, pageId, api.contentGraph(session, pageId, null, { since }), shouldContinue);
}

/** Another page a page shows rows of, and the position of its changes the page shows. */
export type ExternalSince = { pageId: string; since: number };

/**
 * What the changes of other pages a page shows rows of — its linked
 * databases' sources, its relations' targets — changed after the positions
 * given: the rows of theirs it shows those changes alone changed, and its
 * databases whole when they changed those, when they say so; or else its
 * whole graph (2026-11-25: one value typed in a source of 5,000 rows had the
 * page linking it read its 51 windows again, some 8 seconds on a NAS-class
 * server, before it drew the value).
 */
export function collectExternalGraphChanges(
  session: Session,
  pageId: string,
  external: readonly ExternalSince[],
  shouldContinue = () => true,
) {
  const query = new URLSearchParams({
    limit: String(graphWireBounds.readItems),
    external: external.map(({ pageId: page, since }) => `${page}.${since}`).join(","),
  });
  const first = request<GraphReadResponse>(`/pages/${encodeURIComponent(pageId)}/content-graph?${query}`, {}, session);
  return collectChanges(session, pageId, first, shouldContinue);
}

async function collectChanges(
  session: Session,
  pageId: string,
  reading: Promise<GraphReadResponse>,
  shouldContinue: () => boolean,
) {
  const first = await reading;
  if (!shouldContinue()) return null;
  if (first.changedSince === undefined) {
    const whole = await collectGraph(session, pageId, null, shouldContinue, true, first);
    return whole ? { ...whole, changes: null } : null;
  }
  const rows = first.items.flatMap((item) => item.kind === "row" ? [item.value as GraphRow] : []);
  const parts = first.changedDatabases || first.changedBlocks ? readGraph(first.items.filter(({ kind }) => kind !== "row")) : null;
  let databases: ContentGraphDatabases | undefined;
  if (parts && first.changedDatabases) {
    const { databaseContainers, dataSources, views, properties } = parts;
    databases = { databaseContainers, dataSources, views, properties };
  }
  const blocks = parts && first.changedBlocks ? parts.blocks : undefined;
  return {
    graph: null,
    changes: { rows, gone: first.goneRows ?? [], ...(databases ? { databases } : {}), ...(blocks ? { blocks } : {}) } satisfies ContentGraphRowChanges,
    snapshotRevision: first.snapshotRevision,
    canEdit: first.canEdit === true,
    canSuggest: first.canSuggest === true,
    busy: first.busy,
  };
}

/**
 * Whether changes show the page databases it doesn't hold the rows of, or
 * no longer show some it does: a whole read brings their rows (a linked
 * view moved to another database, a relation pointed elsewhere).
 */
export function changesShowOtherSources(graph: ContentGraph, { databases }: ContentGraphRowChanges) {
  if (!databases) return false;
  const shown = new Set(graph.dataSources.map(({ id }) => id));
  return databases.dataSources.length !== shown.size || databases.dataSources.some(({ id }) => !shown.has(id));
}

/**
 * `graph` with its rows changed as read, in the order a whole read gives
 * them: rows changed where they stand are replaced in place
 * (lib/rowsReplaced.ts), so only they are worked out again. Its databases
 * and its blocks read with them replace its own, each item kept that reads
 * the same.
 */
export function withChangedRows(shown: ContentGraph, { rows, gone, databases, blocks }: ContentGraphRowChanges): ContentGraph {
  const keptBlocks = blocks ? keptWhereSame(shown.blocks, blocks) : shown.blocks;
  const withBlocks = keptBlocks === shown.blocks ? shown : { ...shown, blocks: keptBlocks };
  const graph = databases ? {
    ...withBlocks,
    databaseContainers: keptWhereSame(shown.databaseContainers, databases.databaseContainers),
    dataSources: keptWhereSame(shown.dataSources, databases.dataSources),
    views: keptWhereSame(shown.views, databases.views),
    properties: keptWhereSame(shown.properties, databases.properties),
  } : withBlocks;
  if (rows.length === 0 && gone.length === 0) return graph;
  const inPlace = gone.length === 0 ? rowsChangedInPlace(graph.rows, rows) : null;
  if (inPlace) return { ...graph, rows: replaceItems(graph.rows, inPlace) };
  const fresh = new Map(rows.map((row) => [row.id, row]));
  const left = new Set(gone);
  let reorder = false;
  const next: GraphRow[] = [];
  for (const row of graph.rows) {
    if (left.has(row.id)) continue;
    const read = fresh.get(row.id);
    if (!read) {
      next.push(row);
      continue;
    }
    fresh.delete(row.id);
    if (read.position !== row.position) reorder = true;
    next.push(read);
  }
  if (fresh.size > 0) {
    reorder = true;
    next.push(...fresh.values());
  }
  if (reorder) next.sort(compareGraphOrder);
  return { ...graph, rows: next };
}

/**
 * `read`, each item the one `shown` has where it reads the same, so what is
 * worked out from it isn't again; `shown` itself when all are.
 */
function keptWhereSame<T extends { id: string }>(shown: T[], read: T[]): T[] {
  const before = new Map(shown.map((item) => [item.id, item]));
  let same = shown.length === read.length;
  const next = read.map((item, index) => {
    const kept = before.get(item.id);
    if (!kept || JSON.stringify(kept) !== JSON.stringify(item)) {
      same = false;
      return item;
    }
    same = same && shown[index] === kept;
    return kept;
  });
  return same ? shown : next;
}

/** The rows read by their places in `shown`, when each stands where it stood. */
function rowsChangedInPlace(shown: readonly GraphRow[], read: readonly GraphRow[]) {
  const places = placesById(shown, (row) => row.id);
  const replaced = new Map<number, GraphRow>();
  for (const row of read) {
    const place = places.get(row.id);
    if (place === undefined || compareGraphOrder(shown[place], row) !== 0) return null;
    replaced.set(place, row);
  }
  return replaced;
}

async function collectGraph(
  session: Session,
  pageId: string,
  onSnapshot: ((graph: ContentGraph) => void) | null,
  shouldContinue: () => boolean,
  rows: boolean,
  // A first window already read.
  first: GraphReadResponse | null = null,
  text = false,
  // The page's words with its first window.
  withWords = false,
) {
  let given = first;
  let words: string | undefined;
  const result = await collectContentGraphPages({
    readPage: async (cursor) => {
      if (cursor === null && given) {
        const window = given;
        given = null;
        return window;
      }
      const window = await api.contentGraph(session, pageId, cursor, {
        ...(rows ? {} : { rows: false as const }), ...(text ? { text: true as const } : {}),
        ...(withWords && cursor === null ? { words: true as const } : {}),
      });
      if (cursor === null) words = window.words;
      return window;
    },
    onSnapshot: onSnapshot ? (items) => onSnapshot(readGraph(items)) : undefined,
    onInvalidContinuation: throwInvalidContentGraphContinuation,
    shouldContinue,
  });
  return result ? {
    graph: readGraph(result.items),
    snapshotRevision: result.snapshotRevision,
    canEdit: result.canEdit,
    canSuggest: result.canSuggest,
    busy: result.busy,
    ...(words ? { words } : {}),
  } : null;
}
