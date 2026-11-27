import {
  createRecordCacheDatabaseBudgetModel,
  MAX_CACHED_DATABASES,
  MAX_CACHED_ROW_QUERIES_PER_DATABASE,
  MAX_DATABASE_CACHE_RECLAIM_SCAN_PER_TURN,
  MAX_DATABASE_CACHE_RECLAIM_VICTIMS_PER_TURN,
  type RecordCacheDatabaseBudgetOptions,
  type RecordCacheDatabaseBudgetSummary,
  type RecordCacheRowQueryRegistry,
} from "./recordCacheDatabaseBudgetModel";
import {
  databaseRowCacheKeysFromSuffix,
  recordCacheMeta,
  recordCacheTables,
} from "./recordCacheKeys";
import type { RecordCacheStorage } from "./recordCacheStorageModel";

export const LEGACY_DATABASE_LRU_KEY = "dbLru";
export const DATABASE_BUDGET_SUMMARY_KEY = "databaseBudgetV1";
const DATABASE_BUDGET_ENTRY_PREFIX = "databaseBudgetEntry:";
const DATABASE_QUERY_REGISTRY_PREFIX = "databaseQueriesV1:";
export const DATABASE_BUDGET_MIGRATION_KEY = "databaseBudgetMigrationV1";

export interface RecordCacheDatabaseBudgetEntry {
  nextId: string | null;
  prevId: string | null;
}

export interface RecordCacheDatabaseBudgetStorageOptions {
  budget?: Partial<RecordCacheDatabaseBudgetOptions>;
}

export interface RecordCacheDatabaseTouchResult {
  pending: boolean;
  summary: RecordCacheDatabaseBudgetSummary;
  victimIds: string[];
}

export interface RecordCacheDatabaseBudgetStorage {
  listRowQuerySuffixes(
    storage: RecordCacheStorage,
    databaseId: string,
  ): Promise<string[]>;
  reclaimTurn(
    storage: RecordCacheStorage,
    protectedDatabaseIds: ReadonlySet<string>,
  ): Promise<RecordCacheDatabaseTouchResult>;
  registerRowQuery(
    storage: RecordCacheStorage,
    databaseId: string,
    suffix: string,
    at: number,
  ): Promise<string[]>;
  removeDatabase(storage: RecordCacheStorage, databaseId: string): Promise<boolean>;
  touchDatabase(
    storage: RecordCacheStorage,
    databaseId: string,
    protectedDatabaseIds: ReadonlySet<string>,
  ): Promise<RecordCacheDatabaseTouchResult>;
}

export class RecordCacheDatabaseBudgetMigrationRequiredError extends Error {
  constructor() {
    super("Retained record-cache database budget requires explicit migration.");
    this.name = "RecordCacheDatabaseBudgetMigrationRequiredError";
  }
}

function assertIdentity(value: string, label: string): void {
  if (typeof value !== "string" || value.length === 0) {
    throw new RangeError(`${label} is invalid.`);
  }
}

function assertLink(value: unknown, label: string): asserts value is string | null {
  if (value !== null && (typeof value !== "string" || value.length === 0)) {
    throw new Error(`Stored database-cache ${label} link is invalid.`);
  }
}

function validateEntry(value: RecordCacheDatabaseBudgetEntry): RecordCacheDatabaseBudgetEntry {
  if (!value || typeof value !== "object") {
    throw new Error("Stored database-cache entry is invalid.");
  }
  assertLink(value.nextId, "next");
  assertLink(value.prevId, "previous");
  return value;
}

export function databaseBudgetEntryKey(databaseId: string): string {
  return `${DATABASE_BUDGET_ENTRY_PREFIX}${databaseId}`;
}

export function databaseQueryRegistryKey(databaseId: string): string {
  return `${DATABASE_QUERY_REGISTRY_PREFIX}${databaseId}`;
}

function retainedRegistryRequiresMigration(value: unknown): boolean {
  return value !== undefined && (!Array.isArray(value) || value.length > 0);
}

function retainedDatabaseLruState(value: unknown): "absent" | "empty" | "retained" {
  if (value === undefined) return "absent";
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new RecordCacheDatabaseBudgetMigrationRequiredError();
  }
  const entries = Object.entries(value);
  for (const [databaseId, timestamp] of entries) {
    if (
      databaseId.length === 0
      || typeof timestamp !== "number"
      || !Number.isSafeInteger(timestamp)
      || timestamp < 0
    ) {
      throw new RecordCacheDatabaseBudgetMigrationRequiredError();
    }
  }
  return entries.length === 0 ? "empty" : "retained";
}

export function createRecordCacheDatabaseBudgetStorage(
  options: RecordCacheDatabaseBudgetStorageOptions = {},
): RecordCacheDatabaseBudgetStorage {
  const budgetOptions: RecordCacheDatabaseBudgetOptions = {
    maxDatabases: MAX_CACHED_DATABASES,
    maxReclaimScanPerTurn: MAX_DATABASE_CACHE_RECLAIM_SCAN_PER_TURN,
    maxReclaimVictimsPerTurn: MAX_DATABASE_CACHE_RECLAIM_VICTIMS_PER_TURN,
    maxRowQueriesPerDatabase: MAX_CACHED_ROW_QUERIES_PER_DATABASE,
    ...options.budget,
  };
  const model = createRecordCacheDatabaseBudgetModel(budgetOptions);

  const assertNoMigration = async (storage: RecordCacheStorage): Promise<void> => {
    if (await storage.getMeta(DATABASE_BUDGET_MIGRATION_KEY) !== undefined) {
      throw new RecordCacheDatabaseBudgetMigrationRequiredError();
    }
  };

  const readEntry = async (
    storage: RecordCacheStorage,
    databaseId: string,
  ): Promise<RecordCacheDatabaseBudgetEntry | undefined> => {
    const entry = await storage.getMeta<RecordCacheDatabaseBudgetEntry>(
      databaseBudgetEntryKey(databaseId),
    );
    return entry === undefined ? undefined : validateEntry(entry);
  };

  const writeEntry = (
    storage: RecordCacheStorage,
    databaseId: string,
    entry: RecordCacheDatabaseBudgetEntry,
  ): Promise<void> => storage.setMeta(databaseBudgetEntryKey(databaseId), entry);

  const readSummary = async (
    storage: RecordCacheStorage,
  ): Promise<RecordCacheDatabaseBudgetSummary> => {
    await assertNoMigration(storage);
    const legacy = await storage.getMeta(LEGACY_DATABASE_LRU_KEY);
    const legacyState = retainedDatabaseLruState(legacy);
    if (legacyState === "retained") {
      throw new RecordCacheDatabaseBudgetMigrationRequiredError();
    }
    const current = await storage.getMeta<RecordCacheDatabaseBudgetSummary>(
      DATABASE_BUDGET_SUMMARY_KEY,
    );
    if (legacyState === "empty") await storage.removeMeta(LEGACY_DATABASE_LRU_KEY);
    if (current !== undefined) return model.validateDatabaseSummary(current);
    const summary = model.emptyDatabaseSummary();
    await storage.setMeta(DATABASE_BUDGET_SUMMARY_KEY, summary);
    return summary;
  };

  const attach = async (
    storage: RecordCacheStorage,
    summary: RecordCacheDatabaseBudgetSummary,
    databaseId: string,
    entry: RecordCacheDatabaseBudgetEntry,
  ): Promise<void> => {
    summary.reclaimScanned = 0;
    entry.prevId = summary.tailId;
    entry.nextId = null;
    if (summary.tailId) {
      const tail = await readEntry(storage, summary.tailId);
      if (!tail) throw new Error("Stored database-cache tail entry is missing.");
      tail.nextId = databaseId;
      await writeEntry(storage, summary.tailId, tail);
    } else {
      summary.headId = databaseId;
    }
    summary.tailId = databaseId;
    await writeEntry(storage, databaseId, entry);
  };

  const detach = async (
    storage: RecordCacheStorage,
    summary: RecordCacheDatabaseBudgetSummary,
    databaseId: string,
    entry: RecordCacheDatabaseBudgetEntry,
  ): Promise<void> => {
    if (entry.prevId) {
      const previous = await readEntry(storage, entry.prevId);
      if (!previous) throw new Error("Stored database-cache previous entry is missing.");
      previous.nextId = entry.nextId;
      await writeEntry(storage, entry.prevId, previous);
    } else {
      summary.headId = entry.nextId;
    }
    if (entry.nextId) {
      const next = await readEntry(storage, entry.nextId);
      if (!next) throw new Error("Stored database-cache next entry is missing.");
      next.prevId = entry.prevId;
      await writeEntry(storage, entry.nextId, next);
    } else {
      summary.tailId = entry.prevId;
    }
    if (summary.reclaimCursorId === databaseId) summary.reclaimCursorId = entry.nextId;
    summary.reclaimScanned = 0;
  };

  const readRegistry = async (
    storage: RecordCacheStorage,
    databaseId: string,
  ): Promise<RecordCacheRowQueryRegistry> => {
    const current = await storage.getMeta<RecordCacheRowQueryRegistry>(
      databaseQueryRegistryKey(databaseId),
    );
    return current === undefined
      ? model.emptyRowQueryRegistry()
      : model.normalizeRowQueryRegistry(current);
  };

  const assertRetainedRegistryAbsent = async (
    storage: RecordCacheStorage,
    databaseId: string,
  ): Promise<unknown> => {
    const legacyKey = recordCacheMeta.databaseRowQueryRegistry(databaseId);
    const legacy = await storage.getMeta(legacyKey);
    if (retainedRegistryRequiresMigration(legacy)) {
      throw new RecordCacheDatabaseBudgetMigrationRequiredError();
    }
    return legacy;
  };

  const clearQuery = async (
    storage: RecordCacheStorage,
    databaseId: string,
    suffix: string,
  ): Promise<void> => {
    const keys = databaseRowCacheKeysFromSuffix(databaseId, suffix);
    await storage.replaceTable(keys.dataTable, []);
    await storage.replaceTable(keys.relatedPagesTable, []);
    await storage.removeMeta(keys.meta);
  };

  const clearDatabaseSurfaces = async (
    storage: RecordCacheStorage,
    databaseId: string,
  ): Promise<void> => {
    const legacyKey = recordCacheMeta.databaseRowQueryRegistry(databaseId);
    const legacy = await storage.getMeta(legacyKey);
    if (retainedRegistryRequiresMigration(legacy)) {
      throw new RecordCacheDatabaseBudgetMigrationRequiredError();
    }
    const registry = await readRegistry(storage, databaseId);
    for (const entry of registry.entries) await clearQuery(storage, databaseId, entry.suffix);
    await storage.removeMeta(databaseQueryRegistryKey(databaseId));
    if (legacy !== undefined) await storage.removeMeta(legacyKey);
    await storage.replaceTable(recordCacheTables.databaseProperties(databaseId), []);
    await storage.replaceTable(recordCacheTables.databaseViews(databaseId), []);
    await storage.replaceTable(recordCacheTables.databaseTemplates(databaseId), []);
    await storage.removeMeta(recordCacheMeta.databaseMetadataStamp(databaseId));
  };

  const removeEntry = async (
    storage: RecordCacheStorage,
    summary: RecordCacheDatabaseBudgetSummary,
    databaseId: string,
  ): Promise<boolean> => {
    const entry = await readEntry(storage, databaseId);
    if (!entry) return false;
    await detach(storage, summary, databaseId, entry);
    summary.count = Math.max(0, summary.count - 1);
    await storage.removeMeta(databaseBudgetEntryKey(databaseId));
    if (summary.count === 0) {
      summary.headId = null;
      summary.tailId = null;
      summary.reclaimCursorId = null;
      summary.reclaimScanned = 0;
    }
    return true;
  };

  const reclaimTurn = async (
    storage: RecordCacheStorage,
    protectedDatabaseIds: ReadonlySet<string>,
  ): Promise<RecordCacheDatabaseTouchResult> => {
    const summary = await readSummary(storage);
    const candidates = [];
    const seen = new Set<string>();
    // Looked over only while over the budget, as the block cache's.
    let candidateId = model.isOverBudget(summary) ? summary.reclaimCursorId ?? summary.headId : null;
    while (
      candidateId
      && candidates.length < Math.min(summary.count, budgetOptions.maxReclaimScanPerTurn)
      && !seen.has(candidateId)
    ) {
      seen.add(candidateId);
      const candidate = await readEntry(storage, candidateId);
      if (!candidate) throw new Error("Stored database-cache reclaim entry is missing.");
      candidates.push({
        id: candidateId,
        nextId: candidate.nextId,
        protected: protectedDatabaseIds.has(candidateId),
      });
      candidateId = candidate.nextId;
      if (!candidateId && candidates.length < summary.count) candidateId = summary.headId;
    }
    const plan = model.planDatabaseReclaimTurn({ candidates, summary });
    for (const victimId of plan.victimIds) {
      await clearDatabaseSurfaces(storage, victimId);
      if (!await removeEntry(storage, summary, victimId)) {
        throw new Error("Planned database-cache reclaim victim is missing.");
      }
    }
    if (summary.count !== plan.summary.count) {
      throw new Error("Database-cache reclaim projection diverged from durable state.");
    }
    summary.reclaimCursorId = plan.summary.reclaimCursorId;
    summary.reclaimPending = plan.summary.reclaimPending;
    summary.reclaimScanned = plan.summary.reclaimScanned;
    await storage.setMeta(DATABASE_BUDGET_SUMMARY_KEY, summary);
    return {
      pending: summary.reclaimPending,
      summary: { ...summary },
      victimIds: plan.victimIds,
    };
  };

  return {
    async listRowQuerySuffixes(storage, databaseId) {
      assertIdentity(databaseId, "database identity");
      await assertNoMigration(storage);
      await assertRetainedRegistryAbsent(storage, databaseId);
      return (await readRegistry(storage, databaseId)).entries.map(
        ({ suffix }) => suffix,
      );
    },

    reclaimTurn,

    async registerRowQuery(storage, databaseId, suffix, at) {
      assertIdentity(databaseId, "database identity");
      assertIdentity(suffix, "row-query suffix");
      await assertNoMigration(storage);
      const legacyKey = recordCacheMeta.databaseRowQueryRegistry(databaseId);
      const legacy = await assertRetainedRegistryAbsent(storage, databaseId);
      await readSummary(storage);
      const result = model.touchRowQuery(await readRegistry(storage, databaseId), suffix, at);
      for (const victim of result.victimSuffixes) await clearQuery(storage, databaseId, victim);
      await storage.setMeta(databaseQueryRegistryKey(databaseId), result.registry);
      if (legacy !== undefined) await storage.removeMeta(legacyKey);
      return result.victimSuffixes;
    },

    async removeDatabase(storage, databaseId) {
      assertIdentity(databaseId, "database identity");
      const summary = await readSummary(storage);
      await clearDatabaseSurfaces(storage, databaseId);
      const removed = await removeEntry(storage, summary, databaseId);
      if (removed) await storage.setMeta(DATABASE_BUDGET_SUMMARY_KEY, summary);
      return removed;
    },

    async touchDatabase(storage, databaseId, protectedDatabaseIds) {
      assertIdentity(databaseId, "database identity");
      await assertNoMigration(storage);
      await assertRetainedRegistryAbsent(storage, databaseId);
      const summary = await readSummary(storage);
      let entry = await readEntry(storage, databaseId);
      if (!entry) {
        entry = { nextId: null, prevId: null };
        summary.count += 1;
        await attach(storage, summary, databaseId, entry);
      } else if (summary.tailId !== databaseId) {
        await detach(storage, summary, databaseId, entry);
        await attach(storage, summary, databaseId, entry);
      }
      summary.reclaimCursorId = summary.headId;
      summary.reclaimScanned = 0;
      await storage.setMeta(DATABASE_BUDGET_SUMMARY_KEY, summary);
      return reclaimTurn(storage, protectedDatabaseIds);
    },
  };
}
