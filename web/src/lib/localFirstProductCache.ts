import type { Bootstrap } from "@/types";
import type { ContentGraph } from "@/contentGraph";
import type { DatabaseRowPageSnapshot } from "@/databaseRowPageWire.generated";
import type { PageVerification } from "@/wikiWire.generated";
import type { DatabasePageLayoutAnswer } from "./databasePageLayout";
import type { PagePathPieceWire } from "./pagePathPieces";
import type { PageReach } from "./usePageOnlyReader";
import type {
  LocalFirstSnapshot,
  LocalFirstSnapshotIdentity,
} from "./localFirstSnapshotLifecycle";
import type { RecordCacheFacade } from "./recordCache";
import type { RecordCacheTablePage } from "./recordCacheStorageModel";
import { createBootstrapPartsKeeping, type BootstrapHead } from "./bootstrapCacheParts";
import {
  browserRecordCache,
  carryBrowserRecordCacheUnderCustody,
  type RecordCacheCustodyKey,
} from "./recordCacheBrowserFacade";
import { recordCacheMeta, recordCacheTables } from "./recordCacheKeys";
import {
  cacheWorkspaceFilesForOffline,
  clearOfflineWorkspaceFileCache,
  hasCachedWorkspaceFiles,
} from "./offlineFiles";
import { serverIdentity } from "./serverOrigin";
import { managedFileValueLocator, nativeWorkspaceFileIdFromLocator } from "./workspaceFileTransport";

// The key the workspace list's copy is kept under (useAuthenticatedBootstrap.ts).
const BOOTSTRAP_CACHE_KEY = "default";

const GRAPH_SNAPSHOT_ID = "__leafen_complete_content_graph_v1__";
// A graph's rows are kept a thousand to a record beside its blocks, each
// sealed in its own turn, and a part whose rows are all as last kept is the
// same record again, sealed once (recordCacheSealedStorage.ts) and not
// written again (recordCacheIndexedDbStorage.ts): a table of 100,000 rows in
// one record held the main thread a quarter second to seal and again to
// size, each time a value changed, and split into parts it was cut and
// written whole again, some 30 ms each time (2026-11-11). Only the parts
// whose rows changed are cut anew.
const GRAPH_ROWS_ID = "__leafen_content_graph_rows_v1__:";
const ROWS_PER_RECORD = 1_000;
const KEPT_ROW_TABLES = 8;
const READ_STEP = 100;

type GraphRest = Omit<ContentGraph, "blocks" | "rows"> & { rows?: ContentGraph["rows"] };
type StoredGraphRows = { readonly rows: readonly ContentGraph["rows"][number][] };
// The rows' records each graph table was last kept with.
const keptRowRecords = new Map<string, StoredGraphRows[]>();

function rowRecords(table: string, rows: ContentGraph["rows"]): Array<{ id: string; value: StoredGraphRows }> {
  const before = keptRowRecords.get(table) ?? [];
  const parts: StoredGraphRows[] = [];
  for (let start = 0; start < rows.length; start += ROWS_PER_RECORD) {
    const end = Math.min(rows.length, start + ROWS_PER_RECORD);
    const kept = before[parts.length];
    parts.push(kept && sameRows(kept.rows, rows, start, end)
      ? kept : Object.freeze({ rows: Object.freeze(rows.slice(start, end)) }));
  }
  // The pages kept last are the ones kept again; older ones let their rows go.
  keptRowRecords.delete(table);
  keptRowRecords.set(table, parts);
  if (keptRowRecords.size > KEPT_ROW_TABLES) keptRowRecords.delete(keptRowRecords.keys().next().value!);
  return parts.map((value, index) => ({ id: `${GRAPH_ROWS_ID}${String(index).padStart(6, "0")}`, value }));
}
/** Whether `part` holds the rows from `start` to `end` of `rows`, the very same ones. */
function sameRows(part: StoredGraphRows["rows"], rows: ContentGraph["rows"], start: number, end: number) {
  if (part.length !== end - start) return false;
  for (let at = 0; at < part.length; at += 1) {
    if (part[at] !== rows[start + at]) return false;
  }
  return true;
}
type StoredBootstrap = Omit<Bootstrap, "pageFavorites"> & {
  pageFavorites?: Bootstrap["pageFavorites"];
};
type StoredBootstrapSnapshot = Omit<LocalFirstSnapshot<Bootstrap>, "value"> & {
  value: StoredBootstrap;
};
export interface CachedContentGraph {
  bootstrapRevision: number;
  graphRevision: number;
  graph: ContentGraph;
  /** Whether the reader may change the page; a cache without it allows it. */
  canEdit?: boolean;
  /** Whether the reader may suggest edits; a cache without it does not allow it. */
  canSuggest?: boolean;
  /** The operation holding the page when it was read from the server; not kept in the cache. */
  busy?: string;
  /**
   * The changes kept for the server that the copy already shows: the ids of
   * the writes a tab had made of the page when it kept what it showed. What
   * the outbox keeps shows on top of a copy only where the copy lacks it.
   */
  shown?: string[];
}
/**
 * A page's own reads as last answered: a database page's page, its
 * database's layout and its verification, and any page's reach, which its
 * top bar shows by "공유", and the path above it the server named, which its
 * top bar draws (usePagePathsAbove.ts).
 */
export interface KeptRowPage {
  page?: DatabaseRowPageSnapshot;
  layout?: DatabasePageLayoutAnswer;
  verification?: PageVerification;
  reach?: PageReach;
  path?: PagePathPieceWire[];
}
type StoredGraphSnapshot = {
  bootstrapRevision: number;
  graphRevision: number;
  canEdit?: boolean;
  canSuggest?: boolean;
  shown?: string[];
  identity: Omit<LocalFirstSnapshot<CachedContentGraph>, "value">;
  rest: GraphRest;
};

function bootstrapMetaKey(identity: LocalFirstSnapshotIdentity): string {
  return recordCacheMeta.bootstrap(identity.key);
}

function graphTable(identity: LocalFirstSnapshotIdentity): string {
  return recordCacheTables.blocks(identity.key);
}

async function readAll<Value>(
  cache: RecordCacheFacade,
  userId: string,
  table: string,
) {
  // A page's offline copy is read to its end, however many blocks it has
  // (2026-10-13: past 1,000 it was neither kept nor read).
  const records: Array<{ id: string; value: Value }> = [];
  let afterId: string | null = null;
  for (;;) {
    const page: RecordCacheTablePage<Value> = await cache.listTablePage<Value>(userId, {
      afterId,
      limit: READ_STEP,
      table,
    });
    records.push(...page.records);
    if (page.nextAfterId === null) return records;
    if (page.nextAfterId === afterId || page.records.length === 0) return [];
    afterId = page.nextAfterId;
  }
}

// A page's copy is replaced only by one at least as new, judged by the
// revision kept beside it (the stamp that goes with the page's table) and
// checked and written under one lock across tabs: a tab behind the others
// (hidden, or one whose read answered before a change it then missed) kept
// an older copy over the newer one another tab had kept, and the page opened
// offline with the change before the last (2026-11-11). A tab that waits past
// this for another's write is refused, and keeps again later.
const GRAPH_KEEP_LOCK_WAIT_MS = 10_000;

async function underGraphKeepLock(identity: LocalFirstSnapshotIdentity, work: () => Promise<void>): Promise<void> {
  const locks = globalThis.navigator?.locks;
  if (typeof locks?.request !== "function") return work();
  await locks.request(
    `leafen-graph-keep:${identity.principalId}:${identity.key}`,
    { signal: AbortSignal.timeout(GRAPH_KEEP_LOCK_WAIT_MS) },
    work,
  );
}

type PagePictures = { icon?: string; iconType?: string; cover?: string };

// An uploaded picture's file, from the ID or the address a page keeps for it;
// none for an emoji, a built-in icon, a gallery cover (kept by the app's
// worker as it is shown, public/sw.js) or a linked picture.
function storedFileId(value: unknown) {
  return typeof value === "string" ? nativeWorkspaceFileIdFromLocator(managedFileValueLocator(value)) : "";
}

// A page kept for offline use keeps the files it shows: its own icon and
// cover, its body's files, pictures and uploaded callout icons, and its
// rows' icons, covers and files (2026-11-11: offline a pinned page showed no
// cover, as neither covers nor icons were kept).
function managedFileKeys(graph: ContentGraph, page: PagePictures | null): string[] {
  const keys = new Set<string>();
  const addPictures = (pictures: PagePictures) => {
    for (const candidate of [pictures.iconType === "image" ? storedFileId(pictures.icon) : "", storedFileId(pictures.cover)]) {
      if (candidate) keys.add(candidate);
    }
  };
  if (page) addPictures(page);
  for (const block of graph.blocks) {
    const candidate = block.content.fileId;
    if (typeof candidate === "string" && candidate) keys.add(candidate);
    if (block.content.iconType === "image") addPictures({ icon: String(block.content.icon ?? ""), iconType: "image" });
  }
  for (const row of graph.rows) {
    addPictures(row);
    for (const value of Object.values(row.values)) {
      if (value.type !== "files" || !Array.isArray(value.value)) continue;
      for (const file of value.value) {
        if (!file || typeof file !== "object") continue;
        const candidate = Reflect.get(file, "fileId") ?? Reflect.get(file, "key");
        if (typeof candidate === "string" && candidate) keys.add(candidate);
      }
    }
  }
  return Array.from(keys);
}

export function createLocalFirstProductCache(
  cache: RecordCacheFacade = browserRecordCache,
) {
  // A long list is kept in runs (bootstrapCacheParts.ts).
  const listParts = createBootstrapPartsKeeping(cache);
  const readBootstrap = async (
    identity: LocalFirstSnapshotIdentity,
  ): Promise<LocalFirstSnapshot<Bootstrap> | null> => {
    const stored = await cache.getMeta<StoredBootstrapSnapshot>(
      identity.principalId,
      bootstrapMetaKey(identity),
    );
    if (!stored || !stored.value || typeof stored.value !== "object") return null;
    const { pageParts: marks, ...head } = stored.value as BootstrapHead;
    if (Array.isArray(marks)) {
      const pages = await listParts.readPages(identity.principalId, bootstrapMetaKey(identity), marks);
      return pages ? { ...stored, complete: true, value: { ...head, pages } } as LocalFirstSnapshot<Bootstrap> : null;
    }
    if ("pageFavorites" in stored.value) {
      return Array.isArray(stored.value.pageFavorites)
        ? stored as LocalFirstSnapshot<Bootstrap>
        : null;
    }
    const upgraded = {
      ...stored,
      value: { ...stored.value, pageFavorites: [] },
    } satisfies LocalFirstSnapshot<Bootstrap>;
    await cache.mutations.setMeta(
      identity.principalId,
      bootstrapMetaKey(identity),
      upgraded,
      { propagateFailure: true },
    );
    return upgraded;
  };

  const writeBootstrap = (snapshot: LocalFirstSnapshot<Bootstrap>) => listParts.write(snapshot, bootstrapMetaKey(snapshot));

  const dropBootstrap = async (identity: LocalFirstSnapshotIdentity) => {
    await cache.clear(identity.principalId);
    await clearOfflineWorkspaceFileCache();
  };

  const readGraph = async (
    identity: LocalFirstSnapshotIdentity,
  ): Promise<LocalFirstSnapshot<CachedContentGraph> | null> => {
    const records = await readAll<ContentGraph["blocks"][number] | StoredGraphSnapshot | StoredGraphRows>(
      cache,
      identity.principalId,
      graphTable(identity),
    );
    const stored = records.find(({ id }) => id === GRAPH_SNAPSHOT_ID)?.value as StoredGraphSnapshot | undefined;
    if (!stored || typeof stored !== "object" || !("identity" in stored) || !("rest" in stored) ||
      !Number.isSafeInteger(stored.graphRevision) || stored.graphRevision < 0) {
      return null;
    }
    const blocks = records
      .filter(({ id }) => id !== GRAPH_SNAPSHOT_ID && !id.startsWith(GRAPH_ROWS_ID))
      .map(({ value }) => value as ContentGraph["blocks"][number]);
    // Rows kept in parts, in their order; a copy kept before keeps them with the rest.
    const parts = records.filter(({ id }) => id.startsWith(GRAPH_ROWS_ID)).sort((left, right) => left.id < right.id ? -1 : 1);
    const rows = stored.rest.rows ?? parts.flatMap(({ value }) => [...((value as StoredGraphRows).rows ?? [])]);
    return {
      ...stored.identity,
      value: {
        bootstrapRevision: stored.bootstrapRevision,
        graphRevision: stored.graphRevision,
        graph: { blocks, ...stored.rest, rows },
        ...(typeof stored.canEdit === "boolean" ? { canEdit: stored.canEdit } : {}),
        ...(typeof stored.canSuggest === "boolean" ? { canSuggest: stored.canSuggest } : {}),
        ...(Array.isArray(stored.shown) && stored.shown.every((id) => typeof id === "string") ? { shown: stored.shown } : {}),
      },
    };
  };

  const writeGraph = (
    snapshot: LocalFirstSnapshot<CachedContentGraph>,
  ): Promise<void> => underGraphKeepLock(snapshot, async () => {
    const stampKey = recordCacheMeta.blocksStamp(snapshot.key);
    const stamp = await cache.getMeta<{ graphRevision?: unknown }>(snapshot.principalId, stampKey);
    const keptRevision = stamp?.graphRevision;
    if (typeof keptRevision === "number" && keptRevision > snapshot.value.graphRevision) return;
    const { blocks, rows, ...rest } = snapshot.value.graph;
    const records: Array<{
      id: string;
      value: ContentGraph["blocks"][number] | StoredGraphSnapshot | StoredGraphRows;
    }> = [
      ...blocks.map((value) => ({ id: value.id, value })),
      ...rowRecords(graphTable(snapshot), rows),
      {
        id: GRAPH_SNAPSHOT_ID,
        value: {
          bootstrapRevision: snapshot.value.bootstrapRevision,
          graphRevision: snapshot.value.graphRevision,
          ...(typeof snapshot.value.canEdit === "boolean" ? { canEdit: snapshot.value.canEdit } : {}),
          ...(typeof snapshot.value.canSuggest === "boolean" ? { canSuggest: snapshot.value.canSuggest } : {}),
          ...(snapshot.value.shown?.length ? { shown: snapshot.value.shown } : {}),
          identity: {
            complete: true,
            key: snapshot.key,
            principalId: snapshot.principalId,
            serverKey: snapshot.serverKey,
            version: 1,
          },
          rest,
        },
      },
    ];
    await cache.mutations.replaceTable<
      ContentGraph["blocks"][number] | StoredGraphSnapshot | StoredGraphRows
    >(
      snapshot.principalId,
      graphTable(snapshot),
      records,
      { propagateFailure: true },
    );
    await cache.mutations.setMeta(
      snapshot.principalId,
      stampKey,
      { graphRevision: snapshot.value.graphRevision },
      { propagateFailure: true },
    );
    await cache.touchBlockPage(snapshot.principalId, snapshot.key);
  });

  const dropGraph = async (identity: LocalFirstSnapshotIdentity): Promise<void> => {
    const graph = await readGraph(identity);
    const protectedIds = [
      identity.key,
      ...(graph?.value.graph.dataSources.map(({ id }) => id) ?? []),
    ];
    await cache.mutations.replaceTable(
      identity.principalId,
      graphTable(identity),
      [],
      { propagateFailure: true },
    );
    await Promise.all(protectedIds.map((id) => cache.setOfflinePin(
      identity.principalId,
      id,
      false,
    )));
  };

  const setPin = async (userId: string, pageId: string, pinned: boolean) => {
    const identity = { key: pageId, principalId: userId, serverKey: globalThis.location?.origin ?? "local" };
    const graph = await readGraph(identity);
    const databaseIds = graph?.value.graph.dataSources.map(({ id }) => id) ?? [];
    await cache.setOfflinePin(userId, pageId, pinned);
    await Promise.all(databaseIds.map((databaseId) => cache.setOfflinePin(
      userId,
      databaseId,
      pinned,
    )));
  };

  // The page's own icon and cover as last kept here: from the workspace's
  // page list, or a database page's own read.
  const keptPagePictures = async (userId: string, pageId: string): Promise<PagePictures | null> => {
    const listKey = recordCacheMeta.bootstrap(BOOTSTRAP_CACHE_KEY);
    const list = await cache.getMeta<StoredBootstrapSnapshot>(userId, listKey);
    const listed = await listParts.keptPage(userId, listKey, list?.value as BootstrapHead | undefined, pageId);
    if (listed) return listed;
    return (await rowPage.read({ key: pageId, principalId: userId, serverKey: serverIdentity() }))?.page?.row ?? null;
  };

  const warmPin = async (userId: string, pageId: string) => {
    const identity = { key: pageId, principalId: userId, serverKey: globalThis.location?.origin ?? "local" };
    const graph = await readGraph(identity);
    if (!graph) return { complete: false };
    const databaseIds = graph.value.graph.dataSources.map(({ id }) => id);
    await Promise.all(databaseIds.map((databaseId) => cache.touchDatabase(userId, databaseId)));
    const fileKeys = managedFileKeys(graph.value.graph, await keptPagePictures(userId, pageId));
    const filesCached = await cacheWorkspaceFilesForOffline(fileKeys);
    const filesComplete = filesCached && await hasCachedWorkspaceFiles(fileKeys);
    return { complete: filesComplete };
  };

  // A page's own reads, kept beside its body's blocks; they go with them
  // (recordCacheKeys `rowPage`, keptRowPage.ts).
  const rowPage = {
    read: async (identity: LocalFirstSnapshotIdentity) => {
      const stored = await cache.getMeta<LocalFirstSnapshot<KeptRowPage> | null>(
        identity.principalId, recordCacheMeta.rowPage(identity.key));
      return stored?.complete && stored.version === 1 && stored.key === identity.key
        && stored.principalId === identity.principalId && stored.serverKey === identity.serverKey ? stored.value : null;
    },
    /** Keeps these of the page's reads, the others as they were. */
    keep: (identity: LocalFirstSnapshotIdentity, reads: KeptRowPage) => cache.mutations.updateMeta<LocalFirstSnapshot<KeptRowPage> | null>(
      identity.principalId, recordCacheMeta.rowPage(identity.key), (current) => ({
        complete: true, ...identity, version: 1,
        value: { ...(current?.serverKey === identity.serverKey ? current.value : {}), ...reads },
      }), { propagateFailure: true }),
    drop: (identity: LocalFirstSnapshotIdentity) => cache.mutations.setMeta(
      identity.principalId, recordCacheMeta.rowPage(identity.key), null, { propagateFailure: true }),
  };

  // The template changes written here the server may lack, kept until it
  // answers them (templateDraftSend.ts); they go with the person's copies.
  const templateDrafts = {
    read: (userId: string) => cache.getMeta<unknown>(userId, recordCacheMeta.templateDrafts()),
    keep: (userId: string, value: unknown) => cache.mutations.setMeta(
      userId, recordCacheMeta.templateDrafts(), value, { propagateFailure: true }),
  };

  return {
    bootstrap: { dropCache: dropBootstrap, readCache: readBootstrap, writeCache: writeBootstrap },
    rowPage,
    templateDrafts,
    clear: async (userId: string) => {
      await cache.clear(userId);
      await clearOfflineWorkspaceFileCache();
    },
    // A lock change keeps the page copies, sealed again for the next key;
    // the files kept for offline go, as they are kept in the clear and the
    // lock keeps none (offlineFiles.ts).
    carryUnderCustody: async (
      userId: string,
      change: { from: RecordCacheCustodyKey; to: RecordCacheCustodyKey },
    ) => {
      await carryBrowserRecordCacheUnderCustody(userId, change);
      await clearOfflineWorkspaceFileCache();
    },
    graph: {
      dropCache: dropGraph,
      readCache: readGraph,
      writeCache: writeGraph,
      /** The revision of the page's copy kept here, read without the copy; null when none is kept. */
      keptRevision: async (identity: LocalFirstSnapshotIdentity) => {
        const stamp = await cache.getMeta<{ graphRevision?: unknown }>(
          identity.principalId, recordCacheMeta.blocksStamp(identity.key));
        return typeof stamp?.graphRevision === "number" ? stamp.graphRevision : null;
      },
    },
    pins: {
      getPins: (userId: string) => cache.getOfflinePins(userId),
      setPin,
      warm: warmPin,
    },
    resetRuntime: () => cache.resetRuntime(),
  };
}

export const localFirstProductCache = createLocalFirstProductCache();
