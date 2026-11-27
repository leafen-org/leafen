import type {
  RecordCacheBlockBudgetMigration,
  RecordCacheBlockBudgetMigrationReceipt,
} from "./recordCacheBlockBudgetMigration";
import type { RecordCacheBlockStorageEstimate } from "./recordCacheBlockBudgetModel";
import type { RecordCacheBlockBudgetStorage } from "./recordCacheBlockBudgetStorage";
import type {
  RecordCacheDatabaseBudgetMigration,
  RecordCacheDatabaseBudgetMigrationReceipt,
} from "./recordCacheDatabaseBudgetMigration";
import type { RecordCacheDatabaseBudgetStorage } from "./recordCacheDatabaseBudgetStorage";
import type {
  RecordCacheDatabaseOwnedRowCleanup,
  RecordCacheDatabaseOwnedRowCleanupResult,
} from "./recordCacheDatabaseOwnedRowCleanup";
import type { RecordCacheDatabaseQueryMutation } from "./recordCacheDatabaseQueryMutation";
import type { RecordCacheStorage } from "./recordCacheStorageModel";

export type RecordCacheBudgetDomain = "block" | "database";

export type RecordCacheBudgetTurnResult =
  | { domain: "block"; kind: "complete" }
  | { domain: "block"; kind: "reclaim-pending" }
  | { domain: "database"; kind: "complete" }
  | { domain: "database"; kind: "reclaim-pending" }
  | {
      domain: "block";
      kind: "migration-pending";
      receipt: Extract<RecordCacheBlockBudgetMigrationReceipt, { kind: "pending" }>;
    }
  | {
      domain: "database";
      kind: "migration-pending";
      receipt: Extract<RecordCacheDatabaseBudgetMigrationReceipt, { kind: "pending" }>;
    };

export type RecordCacheRowQueryRegistrationResult =
  | Extract<RecordCacheBudgetTurnResult, {
      domain: "database";
      kind: "migration-pending";
    }>
  | { kind: "complete"; victimSuffixes: string[] };

export interface RecordCacheBudgetMigrationTurnResult {
  block: RecordCacheBudgetTurnResult;
  database: RecordCacheBudgetTurnResult;
  pending: boolean;
}

export type RecordCacheDatabaseOwnedRowCleanupTurnResult =
  | {
      domains: RecordCacheBudgetDomain[];
      kind: "maintenance-pending";
    }
  | ({ kind: "complete" } & RecordCacheDatabaseOwnedRowCleanupResult);

export interface RecordCacheBudgetLifecycleOptions {
  blockBudget: Pick<
    RecordCacheBlockBudgetStorage,
    "getOfflinePins" | "reclaimTurn" | "setOfflinePin" | "touch"
  >;
  blockMigration: RecordCacheBlockBudgetMigration;
  databaseBudget: Pick<
    RecordCacheDatabaseBudgetStorage,
    "reclaimTurn" | "registerRowQuery" | "removeDatabase" | "touchDatabase"
  >;
  databaseMigration: RecordCacheDatabaseBudgetMigration;
  databaseOwnedRowCleanup: Pick<RecordCacheDatabaseOwnedRowCleanup, "clear">;
  databaseQueryMutation: Pick<
    RecordCacheDatabaseQueryMutation,
    "invalidateFreshness" | "removeRow"
  >;
  now(): number;
  readStorageEstimate(): Promise<RecordCacheBlockStorageEstimate | null>;
  resolveUnsyncedPageIds(userId: string): Promise<ReadonlySet<string>>;
}

export interface RecordCacheBudgetLifecycle {
  advanceMigrations(
    storage: RecordCacheStorage,
  ): Promise<RecordCacheBudgetMigrationTurnResult>;
  clearDatabaseOwnedRows(
    storage: RecordCacheStorage,
    databaseId: string,
    knownPageIds: readonly string[],
  ): Promise<RecordCacheDatabaseOwnedRowCleanupTurnResult>;
  continueDomain(
    storage: RecordCacheStorage,
    userId: string,
    domain: RecordCacheBudgetDomain,
  ): Promise<RecordCacheBudgetTurnResult>;
  invalidateDatabaseRowsQueryFreshness(
    storage: RecordCacheStorage,
    databaseIds: readonly string[],
  ): Promise<RecordCacheBudgetTurnResult>;
  recoverQuotaTurn(
    storage: RecordCacheStorage,
    userId: string,
  ): Promise<RecordCacheBudgetTurnResult>;
  registerRowQuery(
    storage: RecordCacheStorage,
    databaseId: string,
    suffix: string,
  ): Promise<RecordCacheRowQueryRegistrationResult>;
  removeDatabase(
    storage: RecordCacheStorage,
    databaseId: string,
  ): Promise<RecordCacheBudgetTurnResult & { removed?: boolean }>;
  removeDatabaseRowFromQueries(
    storage: RecordCacheStorage,
    databaseId: string,
    rowId: string,
  ): Promise<RecordCacheBudgetTurnResult>;
  setOfflinePin(
    storage: RecordCacheStorage,
    pageId: string,
    pinned: boolean,
  ): Promise<RecordCacheBudgetTurnResult>;
  touchBlockPage(
    storage: RecordCacheStorage,
    userId: string,
    pageId: string,
  ): Promise<RecordCacheBudgetTurnResult>;
  touchDatabase(
    storage: RecordCacheStorage,
    databaseId: string,
  ): Promise<RecordCacheBudgetTurnResult>;
}

function assertIdentity(value: string, label: string): void {
  if (typeof value !== "string" || value.length === 0) {
    throw new RangeError(`${label} is invalid.`);
  }
}

function assertDomain(value: string): asserts value is RecordCacheBudgetDomain {
  if (value !== "block" && value !== "database") {
    throw new RangeError("Record-cache budget domain is invalid.");
  }
}

function assertTimestamp(value: number): void {
  if (!Number.isSafeInteger(value) || value < 0) {
    throw new RangeError("Record-cache budget clock returned an invalid timestamp.");
  }
}

export function createRecordCacheBudgetLifecycle(
  options: RecordCacheBudgetLifecycleOptions,
): RecordCacheBudgetLifecycle {
  const advanceBlock = async (
    storage: RecordCacheStorage,
  ): Promise<Extract<
    RecordCacheBudgetTurnResult,
    { domain: "block"; kind: "complete" | "migration-pending" }
  >> => {
    const receipt = await options.blockMigration.advance(storage);
    return receipt.kind === "pending"
      ? { domain: "block", kind: "migration-pending", receipt }
      : { domain: "block", kind: "complete" };
  };

  const advanceDatabase = async (
    storage: RecordCacheStorage,
  ): Promise<Extract<
    RecordCacheBudgetTurnResult,
    { domain: "database"; kind: "complete" | "migration-pending" }
  >> => {
    const receipt = await options.databaseMigration.advance(storage);
    return receipt.kind === "pending"
      ? { domain: "database", kind: "migration-pending", receipt }
      : { domain: "database", kind: "complete" };
  };

  const blockReclaimTurn = async (
    storage: RecordCacheStorage,
    userId: string,
    quotaPressure: boolean,
    estimate: RecordCacheBlockStorageEstimate | null,
  ): Promise<RecordCacheBudgetTurnResult> => {
    const unsyncedPageIds = await options.resolveUnsyncedPageIds(userId);
    const result = await options.blockBudget.reclaimTurn(storage, {
      estimate,
      quotaPressure,
      unsyncedPageIds,
    });
    return result.pending
      ? { domain: "block", kind: "reclaim-pending" }
      : { domain: "block", kind: "complete" };
  };

  const databaseReclaimTurn = async (
    storage: RecordCacheStorage,
  ): Promise<RecordCacheBudgetTurnResult> => {
    const pins = await options.blockBudget.getOfflinePins(storage);
    const result = await options.databaseBudget.reclaimTurn(
      storage,
      new Set(Object.keys(pins)),
    );
    return result.pending
      ? { domain: "database", kind: "reclaim-pending" }
      : { domain: "database", kind: "complete" };
  };

  return {
    async advanceMigrations(storage) {
      const block = await advanceBlock(storage);
      const database = await advanceDatabase(storage);
      return {
        block,
        database,
        pending: block.kind !== "complete" || database.kind !== "complete",
      };
    },

    async clearDatabaseOwnedRows(storage, databaseId, knownPageIds) {
      const block = await advanceBlock(storage);
      const database = await advanceDatabase(storage);
      const domains: RecordCacheBudgetDomain[] = [];
      if (block.kind !== "complete") domains.push("block");
      if (database.kind !== "complete") domains.push("database");
      if (domains.length > 0) return { domains, kind: "maintenance-pending" };
      return {
        kind: "complete",
        ...await options.databaseOwnedRowCleanup.clear(
          storage,
          databaseId,
          knownPageIds,
        ),
      };
    },

    async continueDomain(storage, userId, domain) {
      assertIdentity(userId, "record-cache user identity");
      assertDomain(domain);
      if (domain === "block") {
        const migration = await advanceBlock(storage);
        if (migration.kind !== "complete") return migration;
        return blockReclaimTurn(
          storage,
          userId,
          false,
          await options.readStorageEstimate(),
        );
      }
      const migration = await advanceDatabase(storage);
      if (migration.kind !== "complete") return migration;
      return databaseReclaimTurn(storage);
    },

    async recoverQuotaTurn(storage, userId) {
      assertIdentity(userId, "record-cache user identity");
      const migration = await advanceBlock(storage);
      if (migration.kind !== "complete") return migration;
      return blockReclaimTurn(storage, userId, true, null);
    },

    async invalidateDatabaseRowsQueryFreshness(storage, databaseIds) {
      const migration = await advanceDatabase(storage);
      if (migration.kind !== "complete") return migration;
      await options.databaseQueryMutation.invalidateFreshness(storage, databaseIds);
      return { domain: "database", kind: "complete" };
    },

    async registerRowQuery(storage, databaseId, suffix) {
      assertIdentity(databaseId, "database identity");
      assertIdentity(suffix, "row-query suffix");
      const migration = await advanceDatabase(storage);
      if (migration.kind !== "complete") return migration;
      await options.databaseMigration.migrateRetainedRowQueryRegistry(
        storage,
        databaseId,
      );
      const timestamp = options.now();
      assertTimestamp(timestamp);
      const victimSuffixes = await options.databaseBudget.registerRowQuery(
        storage,
        databaseId,
        suffix,
        timestamp,
      );
      return { kind: "complete", victimSuffixes };
    },

    async removeDatabase(storage, databaseId) {
      assertIdentity(databaseId, "database identity");
      const migration = await advanceDatabase(storage);
      if (migration.kind !== "complete") return migration;
      const removed = await options.databaseBudget.removeDatabase(storage, databaseId);
      return { domain: "database", kind: "complete", removed };
    },

    async removeDatabaseRowFromQueries(storage, databaseId, rowId) {
      const migration = await advanceDatabase(storage);
      if (migration.kind !== "complete") return migration;
      await options.databaseQueryMutation.removeRow(storage, databaseId, rowId);
      return { domain: "database", kind: "complete" };
    },

    async setOfflinePin(storage, pageId, pinned) {
      assertIdentity(pageId, "offline-pin page identity");
      const migration = await advanceBlock(storage);
      if (migration.kind !== "complete") return migration;
      await options.blockBudget.setOfflinePin(storage, pageId, pinned);
      return { domain: "block", kind: "complete" };
    },

    async touchBlockPage(storage, userId, pageId) {
      assertIdentity(userId, "record-cache user identity");
      assertIdentity(pageId, "block-cache page identity");
      const migration = await advanceBlock(storage);
      if (migration.kind !== "complete") return migration;
      await options.blockBudget.touch(storage, pageId);
      return blockReclaimTurn(
        storage,
        userId,
        false,
        await options.readStorageEstimate(),
      );
    },

    async touchDatabase(storage, databaseId) {
      assertIdentity(databaseId, "database identity");
      const migration = await advanceDatabase(storage);
      if (migration.kind !== "complete") return migration;
      const pins = await options.blockBudget.getOfflinePins(storage);
      const result = await options.databaseBudget.touchDatabase(
        storage,
        databaseId,
        new Set(Object.keys(pins)),
      );
      return result.pending
        ? { domain: "database", kind: "reclaim-pending" }
        : { domain: "database", kind: "complete" };
    },
  };
}
