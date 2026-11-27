import {
  MAX_CACHED_BLOCK_BYTES,
  MAX_CACHED_BLOCK_PAGES,
  RECORD_CACHE_BLOCK_BUDGET_STATE_VERSION,
  type RecordCacheBlockBudgetSummary,
} from "./recordCacheBlockBudgetModel";
import {
  BLOCK_CACHE_MIGRATION_KEY,
  BLOCK_CACHE_SUMMARY_KEY,
  LEGACY_BLOCKS_LRU_KEY,
  blockCacheEntryKey,
  type BlockCacheEntry,
} from "./recordCacheBlockBudgetStorage";
import { recordCacheTables } from "./recordCacheKeys";
import type {
  RecordCacheRecord,
  RecordCacheStorage,
} from "./recordCacheStorageModel";

const MIGRATION_VERSION = 1;

export interface RecordCacheBlockBudgetMigrationOptions {
  maxRecordsPerStep: number;
  maxTableStepsPerTurn: number;
}

export type RecordCacheBlockBudgetMigrationReceipt =
  | {
      kind: "pending";
      migratedPages: number;
      remainingPages: number;
    }
  | {
      kind: "complete";
      migratedPages: number;
    };

export interface RecordCacheBlockBudgetMigration {
  advance(
    storage: RecordCacheStorage,
  ): Promise<RecordCacheBlockBudgetMigrationReceipt>;
}

interface RetainedPage {
  id: string;
  timestamp: number;
}

interface MigrationState {
  afterId: string | null;
  blocksBytes: number;
  commentsBytes: number;
  pageIndex: number;
  phase: "blocks" | "comments" | "finalize";
  recordBytes: number;
  recordCount: number;
  serializationFailed: boolean;
  source: RetainedPage[];
  totalBytes: number;
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

function byteLength(value: string): number {
  return new TextEncoder().encode(value).byteLength;
}

function validateRetainedSource(value: unknown): RetainedPage[] {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new Error("Retained block-cache LRU metadata is invalid.");
  }
  const source = Object.entries(value).map(([id, timestamp]) => {
    if (
      id.length === 0
      || typeof timestamp !== "number"
      || !Number.isSafeInteger(timestamp)
      || timestamp < 0
    ) {
      throw new Error("Retained block-cache LRU entry is invalid.");
    }
    return { id, timestamp };
  });
  if (source.length > MAX_CACHED_BLOCK_PAGES) {
    throw new RangeError("Retained block-cache LRU exceeds the migration page bound.");
  }
  return source.sort((left, right) => (
    left.timestamp - right.timestamp || left.id.localeCompare(right.id)
  ));
}

function validateState(value: unknown): MigrationState {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new Error("Stored block-cache migration state is invalid.");
  }
  const state = value as MigrationState;
  if (state.version !== MIGRATION_VERSION) {
    throw new RangeError("Unsupported block-cache migration state version.");
  }
  if (!Array.isArray(state.source) || state.source.length > MAX_CACHED_BLOCK_PAGES) {
    throw new RangeError("Stored block-cache migration source exceeds its bound.");
  }
  const seen = new Set<string>();
  for (let index = 0; index < state.source.length; index += 1) {
    const page = state.source[index];
    if (
      !page
      || typeof page.id !== "string"
      || page.id.length === 0
      || seen.has(page.id)
      || typeof page.timestamp !== "number"
      || !Number.isSafeInteger(page.timestamp)
      || page.timestamp < 0
    ) {
      throw new Error("Stored block-cache migration source is invalid.");
    }
    seen.add(page.id);
    const previous = state.source[index - 1];
    if (
      previous
      && (
        previous.timestamp > page.timestamp
        || (previous.timestamp === page.timestamp && previous.id.localeCompare(page.id) >= 0)
      )
    ) {
      throw new Error("Stored block-cache migration source order is invalid.");
    }
  }
  if (!Number.isSafeInteger(state.pageIndex) || state.pageIndex < 0 || state.pageIndex > state.source.length) {
    throw new RangeError("Stored block-cache migration page cursor is invalid.");
  }
  if (!(["blocks", "comments", "finalize"] as const).includes(state.phase)) {
    throw new Error("Stored block-cache migration phase is invalid.");
  }
  if ((state.phase === "finalize") !== (state.pageIndex === state.source.length)) {
    throw new Error("Stored block-cache migration completion state is invalid.");
  }
  if (state.afterId !== null && (typeof state.afterId !== "string" || state.afterId.length === 0)) {
    throw new Error("Stored block-cache migration record cursor is invalid.");
  }
  assertNonNegativeSafeInteger(state.blocksBytes, "stored migrated block bytes");
  assertNonNegativeSafeInteger(state.commentsBytes, "stored migrated comment bytes");
  assertNonNegativeSafeInteger(state.recordBytes, "stored migration record bytes");
  assertNonNegativeSafeInteger(state.recordCount, "stored migration record count");
  assertNonNegativeSafeInteger(state.totalBytes, "stored migrated total bytes");
  if (typeof state.serializationFailed !== "boolean") {
    throw new Error("Stored block-cache migration serialization state is invalid.");
  }
  if (
    (state.phase === "blocks" && (state.blocksBytes !== 0 || state.commentsBytes !== 0))
    || (state.phase === "comments" && state.commentsBytes !== 0)
    || (state.phase === "finalize" && (
      state.afterId !== null
      || state.blocksBytes !== 0
      || state.commentsBytes !== 0
      || state.recordBytes !== 0
      || state.recordCount !== 0
      || state.serializationFailed
    ))
  ) {
    throw new Error("Stored block-cache migration phase progress is inconsistent.");
  }
  return state;
}

function emptyState(source: RetainedPage[]): MigrationState {
  return {
    afterId: null,
    blocksBytes: 0,
    commentsBytes: 0,
    pageIndex: 0,
    phase: source.length === 0 ? "finalize" : "blocks",
    recordBytes: 0,
    recordCount: 0,
    serializationFailed: false,
    source,
    totalBytes: 0,
    version: MIGRATION_VERSION,
  };
}

function consumeRecords(
  state: MigrationState,
  records: readonly RecordCacheRecord[],
): void {
  for (const record of records) {
    try {
      const serialized = JSON.stringify(record);
      if (serialized === undefined) throw new TypeError("Record cannot be serialized.");
      const separator = state.recordCount === 0 ? 0 : 1;
      const next = state.recordBytes + separator + byteLength(serialized);
      if (!Number.isSafeInteger(next)) throw new RangeError("Serialized record bytes overflowed.");
      state.recordBytes = next;
    } catch {
      state.serializationFailed = true;
    }
    const nextCount = state.recordCount + 1;
    if (!Number.isSafeInteger(nextCount)) {
      throw new RangeError("Migrated block-cache record count overflowed.");
    }
    state.recordCount = nextCount;
  }
}

function finishTable(state: MigrationState, table: string): number {
  if (state.recordCount === 0) return 0;
  if (state.serializationFailed) return MAX_CACHED_BLOCK_BYTES;
  const bytes = (
    byteLength('{"records":[')
    + state.recordBytes
    + byteLength(`],"table":${JSON.stringify(table)}}`)
  );
  if (!Number.isSafeInteger(bytes)) return MAX_CACHED_BLOCK_BYTES;
  return bytes;
}

function resetTableProgress(state: MigrationState): void {
  state.afterId = null;
  state.recordBytes = 0;
  state.recordCount = 0;
  state.serializationFailed = false;
}

function summaryFor(state: MigrationState): RecordCacheBlockBudgetSummary {
  return {
    count: state.source.length,
    headId: state.source[0]?.id ?? null,
    reclaimCursorId: state.source[0]?.id ?? null,
    reclaimPending: false,
    reclaimScanned: 0,
    tailId: state.source.at(-1)?.id ?? null,
    totalBytes: state.totalBytes,
    version: RECORD_CACHE_BLOCK_BUDGET_STATE_VERSION,
  };
}

export function createRecordCacheBlockBudgetMigration(
  options: RecordCacheBlockBudgetMigrationOptions,
): RecordCacheBlockBudgetMigration {
  assertPositiveSafeInteger(options.maxRecordsPerStep, "maxRecordsPerStep");
  assertPositiveSafeInteger(options.maxTableStepsPerTurn, "maxTableStepsPerTurn");
  const active = new WeakMap<
    RecordCacheStorage,
    Promise<RecordCacheBlockBudgetMigrationReceipt>
  >();

  const advanceOnce = async (
    storage: RecordCacheStorage,
  ): Promise<RecordCacheBlockBudgetMigrationReceipt> => {
    // The three keys read below, read together where a task's metadata is
    // read once (recordCacheMetaBatch.ts).
    await storage.prefetchMeta?.([BLOCK_CACHE_MIGRATION_KEY, LEGACY_BLOCKS_LRU_KEY, BLOCK_CACHE_SUMMARY_KEY]);
    const stored = await storage.getMeta(BLOCK_CACHE_MIGRATION_KEY);
    let state: MigrationState;
    if (stored === undefined) {
      const retained = await storage.getMeta(LEGACY_BLOCKS_LRU_KEY);
      if (retained === undefined) {
        if (await storage.getMeta(BLOCK_CACHE_SUMMARY_KEY) === undefined) {
          state = emptyState([]);
          await storage.setMeta(BLOCK_CACHE_SUMMARY_KEY, summaryFor(state));
        }
        return { kind: "complete", migratedPages: 0 };
      }
      const source = validateRetainedSource(retained);
      if (await storage.getMeta(BLOCK_CACHE_SUMMARY_KEY) !== undefined) {
        throw new Error("Current and retained block-cache budget metadata conflict.");
      }
      state = emptyState(source);
      await storage.setMeta(BLOCK_CACHE_MIGRATION_KEY, state);
    } else {
      state = validateState(stored);
    }

    let steps = 0;
    while (state.phase !== "finalize" && steps < options.maxTableStepsPerTurn) {
      const page = state.source[state.pageIndex];
      if (!page) throw new Error("Block-cache migration page cursor is missing.");
      const table = state.phase === "blocks"
        ? recordCacheTables.blocks(page.id)
        : recordCacheTables.comments(page.id);
      const result = await storage.listTableStep({
        afterId: state.afterId,
        limit: options.maxRecordsPerStep,
        table,
      });
      steps += 1;
      consumeRecords(state, result.records);
      if (result.nextAfterId !== null) {
        state.afterId = result.nextAfterId;
        continue;
      }

      const measured = finishTable(state, table);
      resetTableProgress(state);
      if (state.phase === "blocks") {
        state.blocksBytes = measured;
        state.phase = "comments";
        continue;
      }

      state.commentsBytes = measured;
      const entry: BlockCacheEntry = {
        blocksBytes: state.blocksBytes,
        commentsBytes: state.commentsBytes,
        nextId: state.source[state.pageIndex + 1]?.id ?? null,
        prevId: state.source[state.pageIndex - 1]?.id ?? null,
      };
      await storage.setMeta(blockCacheEntryKey(page.id), entry);
      const nextTotal = state.totalBytes + entry.blocksBytes + entry.commentsBytes;
      if (!Number.isSafeInteger(nextTotal)) {
        throw new RangeError("Migrated block-cache byte total overflowed.");
      }
      state.totalBytes = nextTotal;
      state.blocksBytes = 0;
      state.commentsBytes = 0;
      state.pageIndex += 1;
      state.phase = state.pageIndex === state.source.length ? "finalize" : "blocks";
    }

    if (state.phase !== "finalize") {
      await storage.setMeta(BLOCK_CACHE_MIGRATION_KEY, state);
      return {
        kind: "pending",
        migratedPages: state.pageIndex,
        remainingPages: state.source.length - state.pageIndex,
      };
    }

    await storage.setMeta(BLOCK_CACHE_SUMMARY_KEY, summaryFor(state));
    await storage.removeMeta(LEGACY_BLOCKS_LRU_KEY);
    await storage.removeMeta(BLOCK_CACHE_MIGRATION_KEY);
    return { kind: "complete", migratedPages: state.source.length };
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
  };
}
