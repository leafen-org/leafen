import { createRecordCacheFacade, createRecordCacheBudgetTurnRunner } from "./recordCache";
import { createRecordCacheBlockBudgetMigration } from "./recordCacheBlockBudgetMigration";
import { createRecordCacheBlockBudgetStorage } from "./recordCacheBlockBudgetStorage";
import {
  createIndexedDbRecordCacheStorage,
  type RecordCacheIndexedDbStorage,
} from "./recordCacheIndexedDbStorage";
import {
  createRecordCacheBrowserRuntime,
  type RecordCacheWebLockManager,
} from "./recordCacheBrowserRuntime";
import { createRecordCacheBudgetContinuation } from "./recordCacheBudgetContinuation";
import { createRecordCacheBudgetLifecycle } from "./recordCacheBudgetLifecycle";
import { createRecordCacheDatabaseBudgetMigration } from "./recordCacheDatabaseBudgetMigration";
import { createRecordCacheDatabaseBudgetStorage } from "./recordCacheDatabaseBudgetStorage";
import { createRecordCacheDatabaseOwnedRowCleanup } from "./recordCacheDatabaseOwnedRowCleanup";
import { createRecordCacheDatabaseQueryMutation } from "./recordCacheDatabaseQueryMutation";
import { databaseRowCacheKeysFromSuffix, recordCacheMeta, recordCacheTables } from "./recordCacheKeys";
import { createRecordCacheMutationOrchestrator } from "./recordCacheMutationOrchestrator";
import { createRecordCacheReadFacade } from "./recordCacheReadFacade";
import { createRecordCacheTableMutationModel } from "./recordCacheTableMutationModel";
import {
  createRecordCacheTerminalAbsenceModel,
} from "./recordCacheTerminalAbsenceModel";
import { createRecordCacheWorkScheduler } from "./recordCacheWorkScheduler";
import { resolveLocalDurableStoreBox } from "./localDurableStoreBox";
import { localBoxIfSettled } from "./localLock";
import type { LocalSecretBox } from "./localSecretBox";
import { carryRecordCacheCustody, type RecordCacheCustodySide } from "./recordCacheCustodyCarry";
import { logProductError } from "./safeErrorLog";
import { serverScopedName } from "./serverOrigin";

// One cache operation takes a page's or a database's records however many
// there are, read and written 100 at a time (2026-10-13: past 1,000 records,
// 100 databases or 100 deletions an operation threw, so a large page or
// database had no offline copy and a large deletion left stale ones). What
// the cache keeps overall stays bounded by its eviction budgets.
const MAX_RECORDS_PER_OPERATION = Number.MAX_SAFE_INTEGER;
const MAX_RECORDS_PER_STEP = 100;
const MAX_USER_ID_CHARACTERS = 64;

const runtimeBounds = {
  database: { maxDatabaseNameCharacters: 256 },
  maxUserIdCharacters: MAX_USER_ID_CHARACTERS,
  storage: {
    maxMetaKeyCharacters: 512,
    maxRecordIdCharacters: 512,
    maxRecordsPerMutation: MAX_RECORDS_PER_OPERATION,
    maxRecordsPerStep: MAX_RECORDS_PER_STEP,
    maxTableCharacters: 512,
  },
} as const;

export interface BrowserRecordCacheFacadeOptions {
  resolveUnsyncedPageIds?: (userId: string) => Promise<ReadonlySet<string>>;
}

function storageEstimate() {
  const storage = globalThis.navigator?.storage;
  if (!storage?.estimate) return Promise.resolve(null);
  return storage.estimate().then(({ quota, usage }) => (
    typeof quota === "number"
    && Number.isSafeInteger(quota)
    && quota > 0
    && typeof usage === "number"
    && Number.isSafeInteger(usage)
    && usage >= 0
      ? { quota, usage }
      : null
  )).catch(() => null);
}

function observeCacheError(error: unknown): void {
  logProductError("warn", "record-cache.unavailable", error);
}

function openRecordCacheRuntime(
  userId: string,
  onCustodyInvalidated: () => void,
  locks?: RecordCacheWebLockManager | null,
) {
  return createRecordCacheBrowserRuntime({
    bounds: runtimeBounds,
    ...(locks === undefined ? {} : { locks }),
    onCustodyInvalidated,
    schemaVersion: 2,
    scope: serverScopedName(""),
    userId,
  });
}

/** The key a lock change takes the store from or to: the passphrase's, or the device's own. */
export type RecordCacheCustodyKey = LocalSecretBox | "device";

function custodyStorageName(userId: string): string {
  if (!userId || userId.length > MAX_USER_ID_CHARACTERS) {
    throw new RangeError("Record-cache userId exceeds its configured character bound.");
  }
  return `leafen-records:${userId}${serverScopedName("")}`;
}

function openCustodyStorage(storageName: string): RecordCacheIndexedDbStorage {
  const rawStorage = createIndexedDbRecordCacheStorage({
    maxDatabaseNameCharacters: runtimeBounds.database.maxDatabaseNameCharacters,
    name: storageName,
  });
  if (!rawStorage) {
    throw new Error("Browser record cache is unavailable during custody change.");
  }
  return rawStorage;
}

async function custodySide(
  key: RecordCacheCustodyKey,
  userId: string,
  storageName: string,
): Promise<RecordCacheCustodySide | null> {
  if (key !== "device") return { box: key, name: "passphrase" };
  const custody = await resolveLocalDurableStoreBox({
    gate: "device",
    maxDatabaseNameCharacters: runtimeBounds.database.maxDatabaseNameCharacters,
    storeName: storageName,
    userId,
  });
  if (custody.kind === "sealed") return { box: custody.box, name: "device" };
  if (custody.kind === "plaintext") return { box: null, name: "plaintext" };
  return null;
}

/**
 * Seals the device's kept copies again for the key a lock change goes to,
 * while the caller owns the shared outbox/cache lock. Where they can't be (a
 * key that can't be had, a store that fails mid-way) both raw stores are
 * cleared instead, as a store sealed with a key the device stops using
 * would not open; the next runtime writes its schema marker again.
 */
export async function carryBrowserRecordCacheUnderCustody(
  userId: string,
  change: { from: RecordCacheCustodyKey; to: RecordCacheCustodyKey },
): Promise<void> {
  const storageName = custodyStorageName(userId);
  const [from, to] = await Promise.all([
    custodySide(change.from, userId, storageName),
    custodySide(change.to, userId, storageName),
  ]);
  const rawStorage = openCustodyStorage(storageName);
  try {
    if (from && to) {
      try {
        await carryRecordCacheCustody(rawStorage, { from, storageName, to });
        return;
      } catch (error) {
        observeCacheError(error);
      }
    }
    await rawStorage.clear();
  } finally {
    rawStorage.close();
  }
}

function createBrowserRecordCacheFacade(
  options: BrowserRecordCacheFacadeOptions = {},
) {
  const blockBudget = createRecordCacheBlockBudgetStorage();
  const databaseBudget = createRecordCacheDatabaseBudgetStorage();
  const databaseQueries = createRecordCacheDatabaseQueryMutation({
    databaseBudget,
    keyspace: {
      rowQuery(databaseId, suffix) {
        const keys = databaseRowCacheKeysFromSuffix(databaseId, suffix);
        return { dataTable: keys.dataTable, metaKey: keys.meta };
      },
    },
    maxDatabaseIdsPerOperation: MAX_RECORDS_PER_OPERATION,
    maxRowIdsPerQuery: MAX_RECORDS_PER_OPERATION,
    maxRowQueriesPerDatabase: 3,
  });
  const databaseOwnedRowCleanup = createRecordCacheDatabaseOwnedRowCleanup({
    blockBudget,
    databaseBudget,
    databaseQueries,
    keyspace: {
      blocks: recordCacheTables.blocks,
      blocksStamp: recordCacheMeta.blocksStamp,
      comments: recordCacheTables.comments,
      commentsCachedAt: recordCacheMeta.commentsCachedAt,
      rowPage: recordCacheMeta.rowPage,
    },
    maxKnownPageIdsPerOperation: MAX_RECORDS_PER_OPERATION,
    maxOwnedPageIdsPerOperation: MAX_RECORDS_PER_OPERATION,
  });
  const blockMigration = createRecordCacheBlockBudgetMigration({
    maxRecordsPerStep: MAX_RECORDS_PER_STEP,
    maxTableStepsPerTurn: 4,
  });
  const databaseMigration = createRecordCacheDatabaseBudgetMigration({
    maxDatabasesPerTurn: 32,
    maxRetainedDatabases: 100,
  });
  const budget = createRecordCacheBudgetLifecycle({
    blockBudget,
    blockMigration,
    databaseBudget,
    databaseMigration,
    databaseOwnedRowCleanup,
    databaseQueryMutation: databaseQueries,
    now: Date.now,
    readStorageEstimate: storageEstimate,
    resolveUnsyncedPageIds: options.resolveUnsyncedPageIds
      ?? (async () => new Set<string>()),
  });

  let continuation: ReturnType<typeof createRecordCacheBudgetContinuation>;
  const scheduler = createRecordCacheWorkScheduler({
    onError: observeCacheError,
    openRuntime: (userId, onCustodyInvalidated) => openRecordCacheRuntime(
      userId,
      onCustodyInvalidated,
    ),
    async recoverQuota(runtime, userId) {
      const result = await budget.recoverQuotaTurn(runtime, userId);
      if (result.kind !== "complete") continuation.schedule(userId, result.domain);
    },
  });
  continuation = createRecordCacheBudgetContinuation({
    defer: queueMicrotask,
    maxPendingUsers: 1,
    maxUserIdCharacters: MAX_USER_ID_CHARACTERS,
    onError: observeCacheError,
    runTurn: createRecordCacheBudgetTurnRunner({ budget, scheduler }),
  });
  const reads = createRecordCacheReadFacade({
    canRead: (userId) => localBoxIfSettled(userId) !== "pending",
    maxRecordsPerPage: MAX_RECORDS_PER_STEP,
    onError: observeCacheError,
    scheduler,
  });
  const tableModel = createRecordCacheTableMutationModel({
    maxPropertyNodesPerOperation: MAX_RECORDS_PER_OPERATION,
    maxRecordIdsPerOperation: MAX_RECORDS_PER_OPERATION,
    maxRecordsPerOperation: MAX_RECORDS_PER_OPERATION,
  });
  const terminalModel = createRecordCacheTerminalAbsenceModel({
    maxBlockIdsPerOperation: MAX_RECORDS_PER_OPERATION,
    maxPageIdsPerOperation: MAX_RECORDS_PER_OPERATION,
    maxPageScopesPerOperation: MAX_RECORDS_PER_OPERATION,
  });
  const mutations = createRecordCacheMutationOrchestrator({
    blockBudget,
    bounds: {
      maxRecordsPerOperation: MAX_RECORDS_PER_OPERATION,
      maxRecordsPerStep: MAX_RECORDS_PER_STEP,
    },
    keyspace: {
      blocks: recordCacheTables.blocks,
      blocksStamp: recordCacheMeta.blocksStamp,
      comments: recordCacheTables.comments,
      commentsCachedAt: recordCacheMeta.commentsCachedAt,
      rowPage: recordCacheMeta.rowPage,
    },
    scheduler,
    tableModel,
    terminalModel,
  });
  return createRecordCacheFacade({
    blockBudget,
    budget,
    continuation,
    mutations,
    reads,
    scheduler,
  });
}

export const browserRecordCache = createBrowserRecordCacheFacade();
