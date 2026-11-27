import type { RecordCacheBlockBudgetStorage } from "./recordCacheBlockBudgetStorage";
import type { RecordCacheDatabaseBudgetStorage } from "./recordCacheDatabaseBudgetStorage";
import type { RecordCacheDatabaseQueryMutation } from "./recordCacheDatabaseQueryMutation";
import type { RecordCacheStorage } from "./recordCacheStorageModel";

export interface RecordCacheDatabaseOwnedRowKeyspace {
  blocks(pageId: string): string;
  blocksStamp(pageId: string): string;
  comments(pageId: string): string;
  commentsCachedAt(pageId: string): string;
  /** A database page's own read, which goes with its blocks (keptRowPage.ts). */
  rowPage(pageId: string): string;
}

export interface RecordCacheDatabaseOwnedRowCleanupOptions {
  blockBudget: Pick<
    RecordCacheBlockBudgetStorage,
    "removeOfflinePins" | "removePage"
  >;
  databaseBudget: Pick<RecordCacheDatabaseBudgetStorage, "removeDatabase">;
  databaseQueries: Pick<RecordCacheDatabaseQueryMutation, "collectOwnedRowIds">;
  keyspace: RecordCacheDatabaseOwnedRowKeyspace;
  maxKnownPageIdsPerOperation: number;
  maxOwnedPageIdsPerOperation: number;
}

export interface RecordCacheDatabaseOwnedRowCleanupResult {
  pageIds: string[];
  removed: boolean;
}

export interface RecordCacheDatabaseOwnedRowCleanup {
  clear(
    storage: RecordCacheStorage,
    databaseId: string,
    knownPageIds: readonly string[],
  ): Promise<RecordCacheDatabaseOwnedRowCleanupResult>;
}

function assertPositiveSafeInteger(value: number, label: string): void {
  if (!Number.isSafeInteger(value) || value <= 0) {
    throw new RangeError(`${label} must be a positive safe integer.`);
  }
}

function assertIdentity(value: string, label: string): void {
  if (typeof value !== "string" || value.length === 0) {
    throw new RangeError(`${label} is invalid.`);
  }
}

export function createRecordCacheDatabaseOwnedRowCleanup(
  options: RecordCacheDatabaseOwnedRowCleanupOptions,
): RecordCacheDatabaseOwnedRowCleanup {
  assertPositiveSafeInteger(
    options.maxKnownPageIdsPerOperation,
    "maxKnownPageIdsPerOperation",
  );
  assertPositiveSafeInteger(
    options.maxOwnedPageIdsPerOperation,
    "maxOwnedPageIdsPerOperation",
  );

  return {
    async clear(storage, databaseId, knownPageIds) {
      assertIdentity(databaseId, "database-cleanup database identity");
      if (
        !Array.isArray(knownPageIds)
        || knownPageIds.length > options.maxKnownPageIdsPerOperation
      ) {
        throw new RangeError("Known database page identities exceed their bound.");
      }
      knownPageIds.forEach((pageId) => {
        assertIdentity(pageId, "known database page identity");
      });

      const cachedRowIds = await options.databaseQueries.collectOwnedRowIds(
        storage,
        databaseId,
      );
      const pageIds = Array.from(new Set([
        databaseId,
        ...knownPageIds,
        ...cachedRowIds,
      ]));
      if (pageIds.length > options.maxOwnedPageIdsPerOperation) {
        throw new RangeError("Database-owned page identities exceed their bound.");
      }

      for (const pageId of pageIds) {
        await storage.replaceTable(options.keyspace.blocks(pageId), []);
        await storage.replaceTable(options.keyspace.comments(pageId), []);
        await storage.removeMeta(options.keyspace.blocksStamp(pageId));
        await storage.removeMeta(options.keyspace.commentsCachedAt(pageId));
        await storage.removeMeta(options.keyspace.rowPage(pageId));
        await options.blockBudget.removePage(storage, pageId);
      }
      await options.blockBudget.removeOfflinePins(storage, pageIds);
      const removed = await options.databaseBudget.removeDatabase(
        storage,
        databaseId,
      );
      return { pageIds, removed };
    },
  };
}
