import {
  createRecordCacheDatabaseBudgetModel,
  MAX_CACHED_DATABASES,
  MAX_CACHED_ROW_QUERIES_PER_DATABASE,
  MAX_DATABASE_CACHE_RECLAIM_SCAN_PER_TURN,
  MAX_DATABASE_CACHE_RECLAIM_VICTIMS_PER_TURN,
  RECORD_CACHE_DATABASE_BUDGET_STATE_VERSION,
  RECORD_CACHE_ROW_QUERY_REGISTRY_VERSION,
  type RecordCacheDatabaseBudgetOptions,
  type RecordCacheDatabaseBudgetSummary,
  type RecordCacheRowQueryRegistry,
} from "./recordCacheDatabaseBudgetModel";
import {
  DATABASE_BUDGET_MIGRATION_KEY,
  DATABASE_BUDGET_SUMMARY_KEY,
  LEGACY_DATABASE_LRU_KEY,
  databaseBudgetEntryKey,
  databaseQueryRegistryKey,
  type RecordCacheDatabaseBudgetEntry,
} from "./recordCacheDatabaseBudgetStorage";
import { recordCacheMeta } from "./recordCacheKeys";
import type { RecordCacheStorage } from "./recordCacheStorageModel";

const MIGRATION_VERSION = 1;

export interface RecordCacheDatabaseBudgetMigrationOptions {
  budget?: Partial<RecordCacheDatabaseBudgetOptions>;
  maxDatabasesPerTurn: number;
  maxRetainedDatabases: number;
}

export type RecordCacheDatabaseBudgetMigrationReceipt =
  | {
      kind: "pending";
      migratedDatabases: number;
      phase: MigrationPhase;
      remainingDatabases: number;
    }
  | {
      kind: "complete";
      migratedDatabases: number;
    };

export interface RecordCacheDatabaseBudgetMigration {
  advance(
    storage: RecordCacheStorage,
  ): Promise<RecordCacheDatabaseBudgetMigrationReceipt>;
  migrateRetainedRowQueryRegistry(
    storage: RecordCacheStorage,
    databaseId: string,
  ): Promise<boolean>;
}

interface RetainedQuery {
  at: number;
  h: string;
}

interface RetainedDatabase {
  id: string;
  queries: RetainedQuery[] | null;
  timestamp: number;
}

type MigrationPhase = "capture" | "install" | "publish" | "cleanup";

interface MigrationState {
  index: number;
  phase: MigrationPhase;
  source: RetainedDatabase[];
  version: typeof MIGRATION_VERSION;
}

function assertPositiveSafeInteger(value: number, label: string): void {
  if (!Number.isSafeInteger(value) || value <= 0) {
    throw new RangeError(`${label} must be a positive safe integer.`);
  }
}

function assertNonNegativeSafeInteger(value: number, label: string): void {
  if (!Number.isSafeInteger(value) || value < 0) {
    throw new RangeError(`${label} must be a non-negative safe integer.`);
  }
}

function assertIdentity(value: string, label: string): void {
  if (typeof value !== "string" || value.length === 0) {
    throw new Error(`${label} is invalid.`);
  }
}

function validateQueries(value: unknown, maximum: number): RetainedQuery[] {
  if (value === undefined) return [];
  if (!Array.isArray(value) || value.length > maximum) {
    throw new RangeError("Retained row-query registry exceeds its migration bound.");
  }
  const seen = new Set<string>();
  return value.map((candidate) => {
    if (!candidate || typeof candidate !== "object") {
      throw new Error("Retained row-query registry entry is invalid.");
    }
    const entry = candidate as RetainedQuery;
    assertIdentity(entry.h, "retained row-query suffix");
    assertNonNegativeSafeInteger(entry.at, "retained row-query recency");
    if (seen.has(entry.h)) {
      throw new Error("Retained row-query registry contains duplicate suffixes.");
    }
    seen.add(entry.h);
    return { at: entry.at, h: entry.h };
  });
}

function validateRetainedSource(value: unknown, maximum: number): RetainedDatabase[] {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new Error("Retained database-cache LRU metadata is invalid.");
  }
  const entries = Object.entries(value);
  if (entries.length > maximum) {
    throw new RangeError("Retained database-cache LRU exceeds its migration bound.");
  }
  return entries.map(([id, timestamp]) => {
    assertIdentity(id, "retained database identity");
    assertNonNegativeSafeInteger(timestamp as number, "retained database recency");
    return { id, queries: null, timestamp: timestamp as number };
  }).sort((left, right) => (
    left.timestamp - right.timestamp || left.id.localeCompare(right.id)
  ));
}

function validateState(
  value: unknown,
  maxRetainedDatabases: number,
  maxQueries: number,
): MigrationState {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new Error("Stored database-cache migration state is invalid.");
  }
  const state = value as MigrationState;
  if (state.version !== MIGRATION_VERSION) {
    throw new RangeError("Unsupported database-cache migration state version.");
  }
  if (!Array.isArray(state.source) || state.source.length > maxRetainedDatabases) {
    throw new RangeError("Stored database-cache migration source exceeds its bound.");
  }
  if (!(new Set<MigrationPhase>(["capture", "install", "publish", "cleanup"])).has(state.phase)) {
    throw new Error("Stored database-cache migration phase is invalid.");
  }
  if (!Number.isSafeInteger(state.index) || state.index < 0 || state.index > state.source.length) {
    throw new RangeError("Stored database-cache migration cursor is invalid.");
  }
  const seen = new Set<string>();
  for (let index = 0; index < state.source.length; index += 1) {
    const database = state.source[index];
    if (!database || typeof database !== "object") {
      throw new Error("Stored database-cache migration source is invalid.");
    }
    assertIdentity(database.id, "stored database identity");
    assertNonNegativeSafeInteger(database.timestamp, "stored database recency");
    if (seen.has(database.id)) {
      throw new Error("Stored database-cache migration source contains duplicate identities.");
    }
    seen.add(database.id);
    const previous = state.source[index - 1];
    if (
      previous
      && (
        previous.timestamp > database.timestamp
        || (previous.timestamp === database.timestamp && previous.id.localeCompare(database.id) >= 0)
      )
    ) {
      throw new Error("Stored database-cache migration source order is invalid.");
    }
    if (database.queries !== null) validateQueries(database.queries, maxQueries);
    const mustBeCaptured = state.phase !== "capture" || index < state.index;
    if (mustBeCaptured !== (database.queries !== null)) {
      throw new Error("Stored database-cache migration capture progress is inconsistent.");
    }
  }
  if ((state.phase === "publish") !== (state.index === state.source.length)) {
    if (state.phase === "publish") {
      throw new Error("Stored database-cache migration publish state is incomplete.");
    }
  }
  return state;
}

function currentRegistry(
  retained: readonly RetainedQuery[],
  model: ReturnType<typeof createRecordCacheDatabaseBudgetModel>,
): RecordCacheRowQueryRegistry {
  return model.normalizeRowQueryRegistry({
    entries: retained.map(({ at, h }) => ({ at, suffix: h })),
    version: RECORD_CACHE_ROW_QUERY_REGISTRY_VERSION,
  });
}

function summaryFor(
  state: MigrationState,
  maximum: number,
): RecordCacheDatabaseBudgetSummary {
  const pending = state.source.length > maximum;
  return {
    count: state.source.length,
    headId: state.source[0]?.id ?? null,
    reclaimCursorId: pending ? state.source[0]?.id ?? null : null,
    reclaimPending: pending,
    reclaimScanned: 0,
    tailId: state.source.at(-1)?.id ?? null,
    version: RECORD_CACHE_DATABASE_BUDGET_STATE_VERSION,
  };
}

function summariesAgree(
  left: RecordCacheDatabaseBudgetSummary,
  right: RecordCacheDatabaseBudgetSummary,
): boolean {
  return (
    left.count === right.count
    && left.headId === right.headId
    && left.reclaimCursorId === right.reclaimCursorId
    && left.reclaimPending === right.reclaimPending
    && left.reclaimScanned === right.reclaimScanned
    && left.tailId === right.tailId
    && left.version === right.version
  );
}

function registriesAgree(
  left: RecordCacheRowQueryRegistry,
  right: RecordCacheRowQueryRegistry,
): boolean {
  return (
    left.version === right.version
    && left.entries.length === right.entries.length
    && left.entries.every((entry, index) => (
      entry.at === right.entries[index]?.at
      && entry.suffix === right.entries[index]?.suffix
    ))
  );
}

export function createRecordCacheDatabaseBudgetMigration(
  options: RecordCacheDatabaseBudgetMigrationOptions,
): RecordCacheDatabaseBudgetMigration {
  assertPositiveSafeInteger(options.maxDatabasesPerTurn, "maxDatabasesPerTurn");
  assertPositiveSafeInteger(options.maxRetainedDatabases, "maxRetainedDatabases");
  const budget: RecordCacheDatabaseBudgetOptions = {
    maxDatabases: MAX_CACHED_DATABASES,
    maxReclaimScanPerTurn: MAX_DATABASE_CACHE_RECLAIM_SCAN_PER_TURN,
    maxReclaimVictimsPerTurn: MAX_DATABASE_CACHE_RECLAIM_VICTIMS_PER_TURN,
    maxRowQueriesPerDatabase: MAX_CACHED_ROW_QUERIES_PER_DATABASE,
    ...options.budget,
  };
  const model = createRecordCacheDatabaseBudgetModel(budget);
  const active = new WeakMap<
    RecordCacheStorage,
    Promise<RecordCacheDatabaseBudgetMigrationReceipt>
  >();

  const pending = (state: MigrationState): RecordCacheDatabaseBudgetMigrationReceipt => ({
    kind: "pending",
    migratedDatabases: state.phase === "capture" ? 0 : Math.min(state.index, state.source.length),
    phase: state.phase,
    remainingDatabases: Math.max(0, state.source.length - state.index),
  });

  const advanceOnce = async (
    storage: RecordCacheStorage,
  ): Promise<RecordCacheDatabaseBudgetMigrationReceipt> => {
    // Read together where a task's metadata is read once (recordCacheMetaBatch.ts).
    await storage.prefetchMeta?.([DATABASE_BUDGET_MIGRATION_KEY, LEGACY_DATABASE_LRU_KEY, DATABASE_BUDGET_SUMMARY_KEY]);
    const stored = await storage.getMeta(DATABASE_BUDGET_MIGRATION_KEY);
    let state: MigrationState;
    if (stored === undefined) {
      const retained = await storage.getMeta(LEGACY_DATABASE_LRU_KEY);
      if (retained === undefined) {
        const current = await storage.getMeta<RecordCacheDatabaseBudgetSummary>(
          DATABASE_BUDGET_SUMMARY_KEY,
        );
        if (current === undefined) {
          await storage.setMeta(DATABASE_BUDGET_SUMMARY_KEY, model.emptyDatabaseSummary());
        } else {
          model.validateDatabaseSummary(current);
        }
        return { kind: "complete", migratedDatabases: 0 };
      }
      const source = validateRetainedSource(retained, options.maxRetainedDatabases);
      const current = await storage.getMeta<RecordCacheDatabaseBudgetSummary>(
        DATABASE_BUDGET_SUMMARY_KEY,
      );
      if (current !== undefined && source.length === 0) {
        model.validateDatabaseSummary(current);
        await storage.removeMeta(LEGACY_DATABASE_LRU_KEY);
        return { kind: "complete", migratedDatabases: 0 };
      }
      if (current !== undefined) {
        throw new Error("Current and retained database-cache budget metadata conflict.");
      }
      state = {
        index: 0,
        phase: "capture",
        source,
        version: MIGRATION_VERSION,
      };
      await storage.setMeta(DATABASE_BUDGET_MIGRATION_KEY, state);
    } else {
      state = validateState(
        stored,
        options.maxRetainedDatabases,
        budget.maxRowQueriesPerDatabase,
      );
    }

    const expectedSummary = summaryFor(state, budget.maxDatabases);
    const currentSummary = await storage.getMeta<RecordCacheDatabaseBudgetSummary>(
      DATABASE_BUDGET_SUMMARY_KEY,
    );
    if (
      (state.phase === "capture" || state.phase === "install")
      && currentSummary !== undefined
    ) {
      throw new Error("Current database-cache budget metadata conflicts with migration progress.");
    }
    if (
      state.phase === "cleanup"
      && (
        currentSummary === undefined
        || !summariesAgree(model.validateDatabaseSummary(currentSummary), expectedSummary)
      )
    ) {
      throw new Error("Published database-cache summary conflicts with migration progress.");
    }

    if (state.phase === "publish") {
      await storage.setMeta(DATABASE_BUDGET_SUMMARY_KEY, expectedSummary);
      state.phase = "cleanup";
      state.index = 0;
      await storage.setMeta(DATABASE_BUDGET_MIGRATION_KEY, state);
      return pending(state);
    }

    const initialIndex = state.index;
    let steps = 0;
    while (steps < options.maxDatabasesPerTurn && state.index < state.source.length) {
      const database = state.source[state.index];
      if (!database) throw new Error("Database-cache migration cursor is missing.");
      if (state.phase === "capture") {
        database.queries = validateQueries(
          await storage.getMeta(recordCacheMeta.databaseRowQueryRegistry(database.id)),
          budget.maxRowQueriesPerDatabase,
        );
      } else if (state.phase === "install") {
        if (database.queries === null) {
          throw new Error("Database-cache migration registry capture is missing.");
        }
        const entry: RecordCacheDatabaseBudgetEntry = {
          nextId: state.source[state.index + 1]?.id ?? null,
          prevId: state.source[state.index - 1]?.id ?? null,
        };
        await storage.setMeta(databaseBudgetEntryKey(database.id), entry);
        await storage.setMeta(
          databaseQueryRegistryKey(database.id),
          currentRegistry(database.queries, model),
        );
      } else if (state.phase === "cleanup") {
        await storage.removeMeta(recordCacheMeta.databaseRowQueryRegistry(database.id));
      }
      state.index += 1;
      steps += 1;
    }

    if (state.index < state.source.length) {
      await storage.setMeta(DATABASE_BUDGET_MIGRATION_KEY, state);
      return pending(state);
    }
    if (state.phase === "capture") {
      state.phase = "install";
      state.index = 0;
      await storage.setMeta(DATABASE_BUDGET_MIGRATION_KEY, state);
      return pending(state);
    }
    if (state.phase === "install") {
      state.phase = "publish";
      await storage.setMeta(DATABASE_BUDGET_MIGRATION_KEY, state);
      return pending(state);
    }
    if (initialIndex < state.source.length) {
      await storage.setMeta(DATABASE_BUDGET_MIGRATION_KEY, state);
      return pending(state);
    }

    await storage.removeMeta(LEGACY_DATABASE_LRU_KEY);
    await storage.removeMeta(DATABASE_BUDGET_MIGRATION_KEY);
    return { kind: "complete", migratedDatabases: state.source.length };
  };

  return {
    advance(storage) {
      const current = active.get(storage);
      if (current) return current;
      const operation = advanceOnce(storage);
      active.set(storage, operation);
      const release = () => {
        if (active.get(storage) === operation) active.delete(storage);
      };
      void operation.then(release, release);
      return operation;
    },

    async migrateRetainedRowQueryRegistry(storage, databaseId) {
      assertIdentity(databaseId, "database identity");
      if (
        await storage.getMeta(DATABASE_BUDGET_MIGRATION_KEY) !== undefined
        || await storage.getMeta(LEGACY_DATABASE_LRU_KEY) !== undefined
      ) {
        throw new Error("Global database-cache migration must complete first.");
      }
      const summary = await storage.getMeta<RecordCacheDatabaseBudgetSummary>(
        DATABASE_BUDGET_SUMMARY_KEY,
      );
      if (summary === undefined) {
        throw new Error("Database-cache budget summary must be initialized first.");
      }
      model.validateDatabaseSummary(summary);
      const retainedKey = recordCacheMeta.databaseRowQueryRegistry(databaseId);
      const retained = await storage.getMeta(retainedKey);
      const currentKey = databaseQueryRegistryKey(databaseId);
      const current = await storage.getMeta<RecordCacheRowQueryRegistry>(currentKey);
      if (retained === undefined) {
        if (current !== undefined) model.normalizeRowQueryRegistry(current);
        return false;
      }
      const migrated = currentRegistry(
        validateQueries(retained, budget.maxRowQueriesPerDatabase),
        model,
      );
      if (current !== undefined) {
        const normalized = model.normalizeRowQueryRegistry(current);
        if (!registriesAgree(normalized, migrated)) {
          throw new Error("Current and retained row-query registries conflict.");
        }
      } else {
        await storage.setMeta(currentKey, migrated);
      }
      await storage.removeMeta(retainedKey);
      return true;
    },
  };
}
