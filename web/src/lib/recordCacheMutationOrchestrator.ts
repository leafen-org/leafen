import type { RecordCacheBlockBudgetStorage } from "./recordCacheBlockBudgetStorage";
import type { RecordCacheBrowserRuntime } from "./recordCacheBrowserRuntime";
import type {
  CachedRowPageAppendMeta,
  RecordCacheProjectionRecord,
  RecordCacheTableMutationModel,
} from "./recordCacheTableMutationModel";
import type {
  CachedBootstrapForTerminalAbsence,
  RecordCacheTerminalAbsenceInput,
  RecordCacheTerminalAbsenceModel,
} from "./recordCacheTerminalAbsenceModel";
import type { RecordCacheTablePage } from "./recordCacheStorageModel";
import type { RecordCacheWorkScheduler } from "./recordCacheWorkScheduler";

export interface RecordCacheMutationBounds {
  maxRecordsPerOperation: number;
  maxRecordsPerStep: number;
}

export interface RecordCacheMutationKeyspace {
  blocks(pageId: string): string;
  blocksStamp(pageId: string): string;
  comments(pageId: string): string;
  commentsCachedAt(pageId: string): string;
  /** A database page's own read, which goes with its blocks (keptRowPage.ts). */
  rowPage(pageId: string): string;
}

export interface RecordCacheMutationOptions {
  propagateFailure?: boolean;
}

export interface RecordCacheTerminalAbsenceMutationInput
  extends RecordCacheTerminalAbsenceInput {
  bootstrapMetaKey?: string;
}

export interface RecordCacheAppendRowPageInput<V> {
  dataTable: string;
  expectedOffset: number;
  meta: CachedRowPageAppendMeta;
  metaKey: string;
  records: readonly RecordCacheProjectionRecord<V>[];
  relatedRecords: readonly RecordCacheProjectionRecord<V>[];
  relatedTable: string;
}

export interface RecordCacheRelatedRecordsInput<V> {
  dataTable: string;
  records: readonly RecordCacheProjectionRecord<V>[]
    | (() => readonly RecordCacheProjectionRecord<V>[]);
  relatedTable: string;
  sourceId: string;
}

export interface RecordCacheMutationOrchestratorOptions {
  blockBudget: Pick<
    RecordCacheBlockBudgetStorage,
    "accountsTable" | "accountTable" | "removeOfflinePins" | "removePage"
  >;
  bounds: RecordCacheMutationBounds;
  keyspace: RecordCacheMutationKeyspace;
  scheduler: Pick<RecordCacheWorkScheduler, "enqueue">;
  tableModel: RecordCacheTableMutationModel;
  terminalModel: RecordCacheTerminalAbsenceModel;
}

export interface RecordCacheMutationOrchestrator {
  appendRowPage<V>(
    userId: string,
    input: RecordCacheAppendRowPageInput<V>,
    options?: RecordCacheMutationOptions,
  ): Promise<void>;
  reconcileRecords<V>(
    userId: string,
    table: string,
    input: {
      removeIds?: Iterable<string>;
      upserts?: Iterable<RecordCacheProjectionRecord<V>>;
    },
    options?: RecordCacheMutationOptions,
  ): Promise<void>;
  reconcileRelatedRecords<V>(
    userId: string,
    input: RecordCacheRelatedRecordsInput<V>,
    options?: RecordCacheMutationOptions,
  ): Promise<void>;
  removeRecords(
    userId: string,
    table: string,
    ids: Iterable<string>,
    options?: RecordCacheMutationOptions,
  ): Promise<void>;
  replaceRecordIfPresent<V>(
    userId: string,
    table: string,
    record: RecordCacheProjectionRecord<V>,
    options?: RecordCacheMutationOptions,
  ): Promise<void>;
  replaceRecordsIfPresent<V>(
    userId: string,
    table: string,
    records: readonly RecordCacheProjectionRecord<V>[]
      | (() => readonly RecordCacheProjectionRecord<V>[]),
    options?: RecordCacheMutationOptions,
  ): Promise<void>;
  replaceTable<V>(
    userId: string,
    table: string,
    records: readonly RecordCacheProjectionRecord<V>[],
    options?: RecordCacheMutationOptions,
  ): Promise<void>;
  setMeta(
    userId: string,
    key: string,
    value: unknown,
    options?: RecordCacheMutationOptions,
  ): Promise<void>;
  settleTerminalAbsence(
    userId: string,
    input: RecordCacheTerminalAbsenceMutationInput,
  ): Promise<void>;
  updateMeta<V>(
    userId: string,
    key: string,
    update: (current: V | undefined) => V | undefined,
    options?: RecordCacheMutationOptions,
  ): Promise<void>;
  updateMetaPair<V>(
    userId: string,
    firstKey: string,
    secondKey: string,
    update: (current: V | undefined) => V | undefined,
    options?: RecordCacheMutationOptions,
  ): Promise<void>;
  /** Writes the keys `write` makes of `key`'s value, together (an undefined value removes its key). */
  writeMetaFrom<V>(
    userId: string,
    key: string,
    write: (current: V | undefined) => readonly { key: string; value: unknown }[],
    options?: RecordCacheMutationOptions,
  ): Promise<void>;
  updateRecordsIfPresent<V>(
    userId: string,
    table: string,
    ids: Iterable<string>,
    update: (value: V) => V,
    options?: RecordCacheMutationOptions,
  ): Promise<void>;
  upsertRecord<V>(
    userId: string,
    table: string,
    record: RecordCacheProjectionRecord<V>,
    options?: RecordCacheMutationOptions,
  ): Promise<void>;
}

function assertPositiveSafeInteger(value: number, label: string): void {
  if (!Number.isSafeInteger(value) || value <= 0) {
    throw new RangeError(`${label} must be a positive safe integer.`);
  }
}

export function createRecordCacheMutationOrchestrator(
  options: RecordCacheMutationOrchestratorOptions,
): RecordCacheMutationOrchestrator {
  assertPositiveSafeInteger(
    options.bounds.maxRecordsPerOperation,
    "maxRecordsPerOperation",
  );
  assertPositiveSafeInteger(options.bounds.maxRecordsPerStep, "maxRecordsPerStep");

  const boundedArray = <T>(values: readonly T[], label: string): T[] => {
    if (!Array.isArray(values) || values.length > options.bounds.maxRecordsPerOperation) {
      throw new RangeError(`${label} exceeds its configured bound.`);
    }
    return Array.from(values);
  };

  const boundedIterable = <T>(values: Iterable<T>, label: string): T[] => {
    const collected: T[] = [];
    for (const value of values) {
      if (collected.length >= options.bounds.maxRecordsPerOperation) {
        throw new RangeError(`${label} exceeds its configured bound.`);
      }
      collected.push(value);
    }
    return collected;
  };

  const listTable = async <V>(
    runtime: RecordCacheBrowserRuntime,
    table: string,
  ): Promise<RecordCacheProjectionRecord<V>[]> => {
    const records: RecordCacheProjectionRecord<V>[] = [];
    let afterId: string | null = null;
    for (;;) {
      const page: RecordCacheTablePage<V> = await runtime.listTableStep<V>({
        afterId,
        limit: Math.min(
          options.bounds.maxRecordsPerStep,
          options.bounds.maxRecordsPerOperation,
        ),
        table,
      });
      if (records.length + page.records.length > options.bounds.maxRecordsPerOperation) {
        throw new RangeError("Record-cache table read exceeds its configured bound.");
      }
      records.push(...page.records);
      if (page.nextAfterId === null) return records;
      if (page.nextAfterId === afterId || page.records.length === 0) {
        throw new Error("Record-cache table continuation did not advance.");
      }
      afterId = page.nextAfterId;
    }
  };

  const replaceAccounted = async <V>(
    runtime: RecordCacheBrowserRuntime,
    table: string,
    records: readonly RecordCacheProjectionRecord<V>[],
  ): Promise<void> => {
    await runtime.replaceTable(table, records);
    await options.blockBudget.accountTable(runtime, table, records);
  };

  const enqueue = (
    userId: string,
    affectedTables: readonly string[],
    task: (runtime: RecordCacheBrowserRuntime) => Promise<void>,
    mutationOptions?: RecordCacheMutationOptions,
  ): Promise<void> => options.scheduler.enqueue({
    affectedTables,
    propagateFailure: mutationOptions?.propagateFailure,
    task,
    userId,
  });

  return {
    appendRowPage(userId, input, mutationOptions) {
      const records = boundedArray(input.records, "row-page records");
      const relatedRecords = boundedArray(
        input.relatedRecords,
        "row-page related records",
      );
      return enqueue(userId, [input.dataTable, input.relatedTable], async (runtime) => {
        const currentMeta = await runtime.getMeta<CachedRowPageAppendMeta>(input.metaKey);
        const [currentRows, currentRelated] = await Promise.all([
          listTable(runtime, input.dataTable),
          listTable(runtime, input.relatedTable),
        ]);
        const projection = options.tableModel.appendRowPage({
          currentMeta,
          currentRelated,
          currentRows,
          expectedOffset: input.expectedOffset,
          meta: input.meta,
          records,
          relatedRecords,
        });
        if (!projection.applies) return;
        await replaceAccounted(runtime, input.dataTable, projection.records);
        await replaceAccounted(runtime, input.relatedTable, projection.relatedRecords);
        await runtime.setMeta(input.metaKey, projection.meta);
      }, mutationOptions);
    },

    reconcileRecords(userId, table, input, mutationOptions) {
      const removeIds = boundedIterable(input.removeIds ?? [], "record removals");
      const upserts = boundedIterable(input.upserts ?? [], "record upserts");
      if (removeIds.length === 0 && upserts.length === 0) return Promise.resolve();
      return enqueue(userId, [table], async (runtime) => {
        const current = await listTable(runtime, table);
        await replaceAccounted(
          runtime,
          table,
          options.tableModel.reconcileRecords(current, { removeIds, upserts }),
        );
      }, mutationOptions);
    },

    reconcileRelatedRecords(userId, input, mutationOptions) {
      return enqueue(userId, [input.dataTable, input.relatedTable], async (runtime) => {
        const [sources, currentRelated] = await Promise.all([
          listTable<Record<string, unknown>>(runtime, input.dataTable),
          listTable(runtime, input.relatedTable),
        ]);
        const supplied = boundedArray(
          typeof input.records === "function" ? input.records() : input.records,
          "related-page records",
        );
        const projection = options.tableModel.reconcileRelatedRecords({
          currentRelated,
          sourceId: input.sourceId,
          sources,
          supplied,
        });
        if (projection.applies) {
          await replaceAccounted(runtime, input.relatedTable, projection.records);
        }
      }, mutationOptions);
    },

    removeRecords(userId, table, ids, mutationOptions) {
      const removals = boundedIterable(ids, "record removals");
      if (removals.length === 0) return Promise.resolve();
      return enqueue(userId, [table], async (runtime) => {
        if (!options.blockBudget.accountsTable(table)) {
          await runtime.removeRecords(table, removals);
          return;
        }
        const current = await listTable(runtime, table);
        const next = options.tableModel.reconcileRecords(current, {
          removeIds: removals,
        });
        await replaceAccounted(runtime, table, next);
      }, mutationOptions);
    },

    replaceRecordIfPresent(userId, table, record, mutationOptions) {
      return enqueue(userId, [table], async (runtime) => {
        const projection = options.tableModel.replaceRecordIfPresent(
          await listTable(runtime, table),
          record,
        );
        if (projection.changed) await replaceAccounted(runtime, table, projection.records);
      }, mutationOptions);
    },

    replaceRecordsIfPresent(userId, table, records, mutationOptions) {
      return enqueue(userId, [table], async (runtime) => {
        const supplied = boundedArray(
          typeof records === "function" ? records() : records,
          "replacement records",
        );
        const projection = options.tableModel.replaceRecordsIfPresent(
          await listTable(runtime, table),
          supplied,
        );
        if (projection.changed) await replaceAccounted(runtime, table, projection.records);
      }, mutationOptions);
    },

    replaceTable(userId, table, records, mutationOptions) {
      const bounded = boundedArray(records, "replacement table");
      return enqueue(
        userId,
        [table],
        (runtime) => replaceAccounted(runtime, table, bounded),
        mutationOptions,
      );
    },

    setMeta(userId, key, value, mutationOptions) {
      return enqueue(userId, [], (runtime) => runtime.setMeta(key, value), mutationOptions);
    },

    settleTerminalAbsence(userId, input) {
      const plan = options.terminalModel.normalize(input);
      const affectedTables = [
        ...plan.pageIds.flatMap((pageId) => [
          options.keyspace.blocks(pageId),
          options.keyspace.comments(pageId),
        ]),
        ...plan.blocksByPage.map(({ pageId }) => options.keyspace.blocks(pageId)),
      ];
      return enqueue(userId, affectedTables, async (runtime) => {
        for (const pageId of plan.pageIds) {
          await runtime.replaceTable(options.keyspace.blocks(pageId), []);
          await runtime.replaceTable(options.keyspace.comments(pageId), []);
          await runtime.removeMeta(options.keyspace.blocksStamp(pageId));
          await runtime.removeMeta(options.keyspace.commentsCachedAt(pageId));
          await runtime.removeMeta(options.keyspace.rowPage(pageId));
          await options.blockBudget.removePage(runtime, pageId);
        }
        await options.blockBudget.removeOfflinePins(runtime, plan.pageIds);
        for (const { blockIds, pageId } of plan.blocksByPage) {
          const table = options.keyspace.blocks(pageId);
          const next = options.tableModel.reconcileRecords(
            await listTable(runtime, table),
            { removeIds: blockIds },
          );
          await replaceAccounted(runtime, table, next);
        }
        if (input.bootstrapMetaKey && plan.pageIds.length > 0) {
          const current = await runtime.getMeta<CachedBootstrapForTerminalAbsence>(
            input.bootstrapMetaKey,
          );
          const projected = options.terminalModel.projectBootstrap(current, plan);
          if (projected !== undefined) {
            await runtime.setMeta(input.bootstrapMetaKey, projected);
          }
        }
      }, { propagateFailure: true });
    },

    updateMeta(userId, key, update, mutationOptions) {
      return enqueue(userId, [], async (runtime) => {
        const next = update(await runtime.getMeta(key));
        if (next !== undefined) await runtime.setMeta(key, next);
      }, mutationOptions);
    },

    writeMetaFrom(userId, key, write, mutationOptions) {
      return enqueue(userId, [], async (runtime) => {
        for (const entry of write(await runtime.getMeta(key))) {
          if (entry.value === undefined) await runtime.removeMeta(entry.key);
          else await runtime.setMeta(entry.key, entry.value);
        }
      }, mutationOptions);
    },

    updateMetaPair(userId, firstKey, secondKey, update, mutationOptions) {
      return enqueue(userId, [], async (runtime) => {
        for (const key of firstKey === secondKey ? [firstKey] : [firstKey, secondKey]) {
          const next = update(await runtime.getMeta(key));
          if (next !== undefined) await runtime.setMeta(key, next);
        }
      }, mutationOptions);
    },

    updateRecordsIfPresent(userId, table, ids, update, mutationOptions) {
      const recordIds = boundedIterable(ids, "record update identities");
      if (recordIds.length === 0) return Promise.resolve();
      return enqueue(userId, [table], async (runtime) => {
        const projection = options.tableModel.updateRecordsIfPresent(
          await listTable<Parameters<typeof update>[0]>(runtime, table),
          recordIds,
          update,
        );
        if (projection.changed) await replaceAccounted(runtime, table, projection.records);
      }, mutationOptions);
    },

    upsertRecord(userId, table, record, mutationOptions) {
      return enqueue(userId, [table], async (runtime) => {
        const next = options.tableModel.upsertRecord(
          await listTable<typeof record.value>(runtime, table),
          record,
        );
        await replaceAccounted(runtime, table, next);
      }, mutationOptions);
    },
  };
}
