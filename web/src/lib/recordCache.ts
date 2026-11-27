import type {
  RecordCacheBlockBudgetStorage,
  RecordCacheBlockPageIdPage,
  RecordCacheBlockPageIdReadInput,
} from "./recordCacheBlockBudgetStorage";
import type {
  RecordCacheBudgetContinuation,
  RecordCacheBudgetContinuationDomain,
  RecordCacheBudgetContinuationTurn,
} from "./recordCacheBudgetContinuation";
import type {
  RecordCacheBudgetLifecycle,
  RecordCacheBudgetTurnResult,
  RecordCacheDatabaseOwnedRowCleanupTurnResult,
  RecordCacheRowQueryRegistrationResult,
} from "./recordCacheBudgetLifecycle";
import type { RecordCacheMutationOrchestrator } from "./recordCacheMutationOrchestrator";
import type { RecordCacheReadFacade } from "./recordCacheReadFacade";
import type {
  RecordCacheTablePage,
  RecordCacheTableStepInput,
} from "./recordCacheStorageModel";
import type { RecordCacheWorkScheduler } from "./recordCacheWorkScheduler";

type BudgetActionResult =
  | RecordCacheBudgetTurnResult
  | RecordCacheRowQueryRegistrationResult;

type FacadeBudgetLifecycle = Pick<
  RecordCacheBudgetLifecycle,
  | "clearDatabaseOwnedRows"
  | "continueDomain"
  | "invalidateDatabaseRowsQueryFreshness"
  | "registerRowQuery"
  | "removeDatabase"
  | "removeDatabaseRowFromQueries"
  | "setOfflinePin"
  | "touchBlockPage"
  | "touchDatabase"
>;

type FacadeBudgetContinuation = Pick<
  RecordCacheBudgetContinuation,
  "dispose" | "idle" | "schedule"
>;

type FacadeWorkScheduler = Pick<
  RecordCacheWorkScheduler,
  "clear" | "dispose" | "enqueue" | "idle" | "reset"
>;

export interface RecordCacheFacadeOptions {
  blockBudget: Pick<
    RecordCacheBlockBudgetStorage,
    "getOfflinePins" | "listPageIds"
  >;
  budget: FacadeBudgetLifecycle;
  continuation: FacadeBudgetContinuation;
  mutations: RecordCacheMutationOrchestrator;
  reads: RecordCacheReadFacade;
  scheduler: FacadeWorkScheduler;
}

export interface RecordCacheBudgetTurnRunnerOptions {
  budget: Pick<RecordCacheBudgetLifecycle, "continueDomain">;
  scheduler: Pick<RecordCacheWorkScheduler, "enqueue">;
}

export interface RecordCacheFacade {
  readonly mutations: RecordCacheMutationOrchestrator;
  clear(userId: string): Promise<void>;
  clearDatabase(
    userId: string,
    databaseId: string,
    knownPageIds: readonly string[],
  ): Promise<RecordCacheDatabaseOwnedRowCleanupTurnResult | undefined>;
  dispose(): Promise<void>;
  getMeta<V = unknown>(userId: string, key: string): Promise<V | undefined>;
  getOfflinePins(userId: string): Promise<Record<string, true>>;
  idle(): Promise<void>;
  invalidateDatabaseRowsQueryFreshness(
    userId: string,
    databaseIds: readonly string[],
  ): Promise<RecordCacheBudgetTurnResult | undefined>;
  listCachedBlockPageIds(
    userId: string,
    input: RecordCacheBlockPageIdReadInput,
  ): Promise<RecordCacheBlockPageIdPage>;
  listTablePage<V = unknown>(
    userId: string,
    input: RecordCacheTableStepInput,
  ): Promise<RecordCacheTablePage<V>>;
  registerRowQuery(
    userId: string,
    databaseId: string,
    suffix: string,
  ): Promise<RecordCacheRowQueryRegistrationResult | undefined>;
  removeDatabase(
    userId: string,
    databaseId: string,
  ): Promise<(RecordCacheBudgetTurnResult & { removed?: boolean }) | undefined>;
  removeDatabaseRowFromQueries(
    userId: string,
    databaseId: string,
    rowId: string,
  ): Promise<RecordCacheBudgetTurnResult | undefined>;
  resetRuntime(): Promise<void>;
  setOfflinePin(
    userId: string,
    pageId: string,
    pinned: boolean,
  ): Promise<RecordCacheBudgetTurnResult | undefined>;
  touchBlockPage(
    userId: string,
    pageId: string,
  ): Promise<RecordCacheBudgetTurnResult | undefined>;
  touchDatabase(
    userId: string,
    databaseId: string,
  ): Promise<RecordCacheBudgetTurnResult | undefined>;
}

interface ActionBox<Result extends BudgetActionResult> {
  value?: Result;
}

function isPending(result: BudgetActionResult): boolean {
  return result.kind !== "complete";
}

export function createRecordCacheBudgetTurnRunner(
  options: RecordCacheBudgetTurnRunnerOptions,
): (
  userId: string,
  domain: RecordCacheBudgetContinuationDomain,
) => Promise<RecordCacheBudgetContinuationTurn> {
  return async (userId, domain) => {
    const result: ActionBox<RecordCacheBudgetTurnResult> = {};
    await options.scheduler.enqueue({
      propagateFailure: true,
      task: async (storage) => {
        result.value = await options.budget.continueDomain(storage, userId, domain);
      },
      userId,
    });
    return result.value === undefined || result.value.kind === "complete"
      ? "complete"
      : "pending";
  };
}

export function createRecordCacheFacade(
  options: RecordCacheFacadeOptions,
): RecordCacheFacade {
  let generation = 0;

  const runAction = async <Result extends BudgetActionResult>(
    userId: string,
    domain: RecordCacheBudgetContinuationDomain,
    action: Parameters<FacadeWorkScheduler["enqueue"]>[0]["task"] extends (
      storage: infer Storage,
    ) => Promise<void>
      ? (storage: Storage) => Promise<Result>
      : never,
  ): Promise<Result | undefined> => {
    const token = generation;
    const first: ActionBox<Result> = {};
    await options.scheduler.enqueue({
      propagateFailure: true,
      task: async (storage) => {
        first.value = await action(storage);
      },
      userId,
    });
    if (first.value === undefined || !isPending(first.value)) return first.value;

    if (options.continuation.schedule(userId, domain) === "refused") {
      throw new Error("Record-cache maintenance continuation was refused.");
    }
    await options.continuation.idle(userId, domain);
    if (token !== generation) return undefined;

    const second: ActionBox<Result> = {};
    await options.scheduler.enqueue({
      propagateFailure: true,
      task: async (storage) => {
        second.value = await action(storage);
      },
      userId,
    });
    if (second.value !== undefined && isPending(second.value)) {
      throw new Error("Record-cache maintenance did not settle after one retry.");
    }
    return second.value;
  };

  const runDatabaseCleanup = async (
    userId: string,
    databaseId: string,
    knownPageIds: readonly string[],
  ): Promise<RecordCacheDatabaseOwnedRowCleanupTurnResult | undefined> => {
    const token = generation;
    const execute = async (
      box: { value?: RecordCacheDatabaseOwnedRowCleanupTurnResult },
    ): Promise<void> => options.scheduler.enqueue({
      propagateFailure: true,
      task: async (storage) => {
        box.value = await options.budget.clearDatabaseOwnedRows(
          storage,
          databaseId,
          knownPageIds,
        );
      },
      userId,
    });
    const first: { value?: RecordCacheDatabaseOwnedRowCleanupTurnResult } = {};
    await execute(first);
    if (first.value === undefined || first.value.kind === "complete") {
      return first.value;
    }
    if (first.value.domains.length === 0) {
      throw new Error("Record-cache cleanup returned no maintenance domain.");
    }
    const admissions = first.value.domains.map(
      (domain) => options.continuation.schedule(userId, domain),
    );
    if (admissions.includes("refused")) {
      throw new Error("Record-cache cleanup continuation was refused.");
    }
    await Promise.all(first.value.domains.map(
      (domain) => options.continuation.idle(userId, domain),
    ));
    if (token !== generation) return undefined;

    const second: { value?: RecordCacheDatabaseOwnedRowCleanupTurnResult } = {};
    await execute(second);
    if (second.value?.kind === "maintenance-pending") {
      throw new Error("Record-cache cleanup maintenance did not settle after one retry.");
    }
    return second.value;
  };

  return {
    mutations: options.mutations,

    async clear(userId) {
      generation += 1;
      await options.scheduler.clear(userId);
    },

    clearDatabase(userId, databaseId, knownPageIds) {
      return runDatabaseCleanup(userId, databaseId, knownPageIds);
    },

    async dispose() {
      generation += 1;
      options.continuation.dispose();
      await options.scheduler.dispose();
    },

    getMeta<V = unknown>(userId: string, key: string) {
      return options.reads.getMeta<V>(userId, key);
    },

    getOfflinePins(userId) {
      return options.reads.readStorage(
        userId,
        {},
        (storage) => options.blockBudget.getOfflinePins(storage),
      );
    },

    async idle() {
      await options.continuation.idle();
      await options.scheduler.idle();
    },

    invalidateDatabaseRowsQueryFreshness(userId, databaseIds) {
      return runAction(
        userId,
        "database",
        (storage) => options.budget.invalidateDatabaseRowsQueryFreshness(
          storage,
          databaseIds,
        ),
      );
    },

    listCachedBlockPageIds(userId, input) {
      return options.reads.readStorage(
        userId,
        { nextCursorId: null, pageIds: [] },
        (storage) => options.blockBudget.listPageIds(storage, input),
      );
    },

    listTablePage<V = unknown>(userId: string, input: RecordCacheTableStepInput) {
      return options.reads.listTablePage<V>(userId, input);
    },

    registerRowQuery(userId, databaseId, suffix) {
      return runAction(
        userId,
        "database",
        (storage) => options.budget.registerRowQuery(storage, databaseId, suffix),
      );
    },

    removeDatabase(userId, databaseId) {
      return runAction(
        userId,
        "database",
        (storage) => options.budget.removeDatabase(storage, databaseId),
      );
    },

    removeDatabaseRowFromQueries(userId, databaseId, rowId) {
      return runAction(
        userId,
        "database",
        (storage) => options.budget.removeDatabaseRowFromQueries(
          storage,
          databaseId,
          rowId,
        ),
      );
    },

    async resetRuntime() {
      generation += 1;
      await options.scheduler.reset();
    },

    setOfflinePin(userId, pageId, pinned) {
      return runAction(
        userId,
        "block",
        (storage) => options.budget.setOfflinePin(storage, pageId, pinned),
      );
    },

    touchBlockPage(userId, pageId) {
      return runAction(
        userId,
        "block",
        (storage) => options.budget.touchBlockPage(storage, userId, pageId),
      );
    },

    touchDatabase(userId, databaseId) {
      return runAction(
        userId,
        "database",
        (storage) => options.budget.touchDatabase(storage, databaseId),
      );
    },
  };
}
