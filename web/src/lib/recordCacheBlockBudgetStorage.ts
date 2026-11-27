import {
  createRecordCacheBlockBudgetModel,
  MAX_BLOCK_CACHE_RECLAIM_PAGES_PER_TURN,
  MAX_BLOCK_CACHE_RECLAIM_SCAN_PER_TURN,
  MAX_CACHED_BLOCK_BYTES,
  MAX_CACHED_BLOCK_PAGES,
  type RecordCacheBlockBudgetOptions,
  type RecordCacheBlockBudgetSummary,
  type RecordCacheBlockStorageEstimate,
} from "./recordCacheBlockBudgetModel";
import { recordCacheMeta, recordCacheTables } from "./recordCacheKeys";
import type {
  RecordCacheRecord,
  RecordCacheStorage,
} from "./recordCacheStorageModel";

export const LEGACY_BLOCKS_LRU_KEY = "blocksLru";
export const BLOCK_CACHE_SUMMARY_KEY = "blocksBudgetV1";
const BLOCK_CACHE_ENTRY_PREFIX = "blocksBudgetEntry:";
export const BLOCK_CACHE_MIGRATION_KEY = "blocksBudgetMigrationV1";
const PINS_KEY = "offlinePins";

export interface BlockCacheEntry {
  blocksBytes: number;
  commentsBytes: number;
  nextId: string | null;
  prevId: string | null;
}

export interface RecordCacheBlockBudgetStorageOptions {
  budget?: Partial<RecordCacheBlockBudgetOptions>;
}

export interface RecordCacheBlockReclaimStorageInput {
  estimate: RecordCacheBlockStorageEstimate | null;
  quotaPressure: boolean;
  unsyncedPageIds: ReadonlySet<string>;
}

export interface RecordCacheBlockReclaimStorageResult {
  pending: boolean;
  summary: RecordCacheBlockBudgetSummary;
  victimIds: string[];
}

export interface RecordCacheBlockPageIdReadInput {
  cursorId: string | null;
  limit: number;
}

export interface RecordCacheBlockPageIdPage {
  nextCursorId: string | null;
  pageIds: string[];
}

export interface RecordCacheBlockBudgetStorage {
  accountsTable(table: string): boolean;
  accountTable<V = unknown>(
    storage: RecordCacheStorage,
    table: string,
    records: readonly RecordCacheRecord<V>[],
  ): Promise<boolean>;
  getOfflinePins(storage: RecordCacheStorage): Promise<Record<string, true>>;
  listPageIds(
    storage: RecordCacheStorage,
    input: RecordCacheBlockPageIdReadInput,
  ): Promise<RecordCacheBlockPageIdPage>;
  reclaimTurn(
    storage: RecordCacheStorage,
    input: RecordCacheBlockReclaimStorageInput,
  ): Promise<RecordCacheBlockReclaimStorageResult>;
  removeOfflinePins(
    storage: RecordCacheStorage,
    pageIds: Iterable<string>,
  ): Promise<void>;
  removePage(storage: RecordCacheStorage, pageId: string): Promise<boolean>;
  setOfflinePin(
    storage: RecordCacheStorage,
    pageId: string,
    pinned: boolean,
  ): Promise<void>;
  touch(storage: RecordCacheStorage, pageId: string): Promise<void>;
}

export class RecordCacheBlockBudgetMigrationRequiredError extends Error {
  constructor() {
    super("Retained record-cache block budget requires explicit migration.");
    this.name = "RecordCacheBlockBudgetMigrationRequiredError";
  }
}

function assertNonNegativeSafeInteger(value: number, label: string): void {
  if (!Number.isSafeInteger(value) || value < 0) {
    throw new RangeError(`${label} must be a non-negative safe integer.`);
  }
}

function assertPageId(pageId: string): void {
  if (typeof pageId !== "string" || pageId.length === 0) {
    throw new RangeError("Block-cache page identity is invalid.");
  }
}

function assertLink(value: unknown, label: string): asserts value is string | null {
  if (value !== null && (typeof value !== "string" || value.length === 0)) {
    throw new Error(`Stored block-cache ${label} link is invalid.`);
  }
}

export function blockCacheEntryKey(pageId: string): string {
  return `${BLOCK_CACHE_ENTRY_PREFIX}${pageId}`;
}

function tableOwner(table: string): { kind: "blocks" | "comments"; pageId: string } | null {
  for (const kind of ["blocks", "comments"] as const) {
    const prefix = `${kind}:`;
    if (table.startsWith(prefix) && table.length > prefix.length) {
      return { kind, pageId: table.slice(prefix.length) };
    }
  }
  return null;
}

// A frozen value's size, which can't change (a graph's rows kept in parts,
// localFirstProductCache.ts): one part changed is the only one measured again.
const frozenValueBytes = new WeakMap<object, number>();
const sizeEncoder = new TextEncoder();

function jsonBytes(value: unknown): number {
  const frozen = typeof value === "object" && value !== null && Object.isFrozen(value) ? value : null;
  const known = frozen ? frozenValueBytes.get(frozen) : undefined;
  if (known !== undefined) return known;
  const bytes = sizeEncoder.encode(JSON.stringify(value)).byteLength;
  if (frozen) frozenValueBytes.set(frozen, bytes);
  return bytes;
}

/** The bytes of {"records":[{"id":…,"value":…},…],"table":…}, a record at a time. */
function serializedTableBytes<V>(
  table: string,
  records: readonly RecordCacheRecord<V>[],
): number {
  try {
    let bytes = 12 + 10 + jsonBytes(table) + 1 + Math.max(0, records.length - 1);
    for (const record of records) bytes += 6 + jsonBytes(record.id) + 9 + jsonBytes(record.value) + 1;
    return bytes;
  } catch {
    return MAX_CACHED_BLOCK_BYTES;
  }
}

function validateEntry(value: BlockCacheEntry): BlockCacheEntry {
  if (!value || typeof value !== "object") {
    throw new Error("Stored block-cache entry is invalid.");
  }
  assertNonNegativeSafeInteger(value.blocksBytes, "stored block bytes");
  assertNonNegativeSafeInteger(value.commentsBytes, "stored comment bytes");
  assertLink(value.nextId, "next");
  assertLink(value.prevId, "previous");
  return value;
}

function validateSummaryLinks(summary: RecordCacheBlockBudgetSummary): void {
  assertLink(summary.headId, "head");
  assertLink(summary.tailId, "tail");
  assertLink(summary.reclaimCursorId, "reclaim cursor");
  if (summary.count === 0 && (summary.headId !== null || summary.tailId !== null)) {
    throw new Error("Stored empty block-cache summary retains linked entries.");
  }
}

export function createRecordCacheBlockBudgetStorage(
  options: RecordCacheBlockBudgetStorageOptions = {},
): RecordCacheBlockBudgetStorage {
  const budgetOptions: RecordCacheBlockBudgetOptions = {
    maxBytes: MAX_CACHED_BLOCK_BYTES,
    maxPages: MAX_CACHED_BLOCK_PAGES,
    maxScanPerTurn: MAX_BLOCK_CACHE_RECLAIM_SCAN_PER_TURN,
    maxVictimsPerTurn: MAX_BLOCK_CACHE_RECLAIM_PAGES_PER_TURN,
    ...options.budget,
  };
  const model = createRecordCacheBlockBudgetModel(budgetOptions);

  const readEntry = async (
    storage: RecordCacheStorage,
    pageId: string,
  ): Promise<BlockCacheEntry | undefined> => {
    const entry = await storage.getMeta<BlockCacheEntry>(blockCacheEntryKey(pageId));
    return entry === undefined ? undefined : validateEntry(entry);
  };

  const writeEntry = (
    storage: RecordCacheStorage,
    pageId: string,
    entry: BlockCacheEntry,
  ): Promise<void> => storage.setMeta(blockCacheEntryKey(pageId), entry);

  // What a step reads next, read together where a task's metadata is read
  // once (recordCacheMetaBatch.ts).
  const readAhead = async (
    storage: RecordCacheStorage,
    keys: readonly string[],
    pageIds: ReadonlyArray<string | null>,
  ): Promise<void> => {
    await storage.prefetchMeta?.([
      ...keys,
      ...pageIds.flatMap((pageId) => (pageId ? [blockCacheEntryKey(pageId)] : [])),
    ]);
  };

  const readSummary = async (
    storage: RecordCacheStorage,
  ): Promise<RecordCacheBlockBudgetSummary> => {
    if (await storage.getMeta(BLOCK_CACHE_MIGRATION_KEY) !== undefined) {
      throw new RecordCacheBlockBudgetMigrationRequiredError();
    }
    const current = await storage.getMeta<RecordCacheBlockBudgetSummary>(
      BLOCK_CACHE_SUMMARY_KEY,
    );
    if (current !== undefined) {
      model.isUnderPressure(current, null);
      validateSummaryLinks(current);
      return current;
    }
    const legacy = await storage.getMeta<Record<string, number>>(LEGACY_BLOCKS_LRU_KEY);
    if (legacy && Object.keys(legacy).length > 0) {
      throw new RecordCacheBlockBudgetMigrationRequiredError();
    }
    if (legacy !== undefined) await storage.removeMeta(LEGACY_BLOCKS_LRU_KEY);
    const summary = model.emptySummary();
    await storage.setMeta(BLOCK_CACHE_SUMMARY_KEY, summary);
    return summary;
  };

  const attach = async (
    storage: RecordCacheStorage,
    summary: RecordCacheBlockBudgetSummary,
    pageId: string,
    entry: BlockCacheEntry,
  ): Promise<void> => {
    summary.reclaimScanned = 0;
    entry.prevId = summary.tailId;
    entry.nextId = null;
    if (summary.tailId) {
      const tail = await readEntry(storage, summary.tailId);
      if (!tail) throw new Error("Stored block-cache tail entry is missing.");
      tail.nextId = pageId;
      await writeEntry(storage, summary.tailId, tail);
    } else {
      summary.headId = pageId;
    }
    summary.tailId = pageId;
    await writeEntry(storage, pageId, entry);
  };

  const detach = async (
    storage: RecordCacheStorage,
    summary: RecordCacheBlockBudgetSummary,
    pageId: string,
    entry: BlockCacheEntry,
  ): Promise<void> => {
    if (entry.prevId) {
      const previous = await readEntry(storage, entry.prevId);
      if (!previous) throw new Error("Stored block-cache previous entry is missing.");
      previous.nextId = entry.nextId;
      await writeEntry(storage, entry.prevId, previous);
    } else {
      summary.headId = entry.nextId;
    }
    if (entry.nextId) {
      const next = await readEntry(storage, entry.nextId);
      if (!next) throw new Error("Stored block-cache next entry is missing.");
      next.prevId = entry.prevId;
      await writeEntry(storage, entry.nextId, next);
    } else {
      summary.tailId = entry.prevId;
    }
    if (summary.reclaimCursorId === pageId) summary.reclaimCursorId = entry.nextId;
    summary.reclaimScanned = 0;
  };

  const removeEntry = async (
    storage: RecordCacheStorage,
    summary: RecordCacheBlockBudgetSummary,
    pageId: string,
  ): Promise<boolean> => {
    const entry = await readEntry(storage, pageId);
    if (!entry) return false;
    await detach(storage, summary, pageId, entry);
    summary.count = Math.max(0, summary.count - 1);
    summary.totalBytes = Math.max(
      0,
      summary.totalBytes - entry.blocksBytes - entry.commentsBytes,
    );
    await storage.removeMeta(blockCacheEntryKey(pageId));
    if (summary.count === 0) {
      summary.headId = null;
      summary.tailId = null;
      summary.reclaimCursorId = null;
      summary.reclaimScanned = 0;
    }
    return true;
  };

  const getOfflinePins = async (
    storage: RecordCacheStorage,
  ): Promise<Record<string, true>> => {
    const stored = await storage.getMeta<Record<string, true>>(PINS_KEY);
    if (!stored || typeof stored !== "object" || Array.isArray(stored)) return {};
    return Object.fromEntries(
      Object.entries(stored).filter(([pageId, pinned]) => pageId.length > 0 && pinned === true),
    );
  };

  return {
    accountsTable(table) {
      return tableOwner(table) !== null;
    },

    async accountTable(storage, table, records) {
      const owner = tableOwner(table);
      if (!owner) return false;
      await readAhead(storage, [BLOCK_CACHE_MIGRATION_KEY, BLOCK_CACHE_SUMMARY_KEY], [owner.pageId]);
      const summary = await readSummary(storage);
      let entry = await readEntry(storage, owner.pageId);
      if (!entry) {
        entry = { blocksBytes: 0, commentsBytes: 0, nextId: null, prevId: null };
        summary.count += 1;
        await attach(storage, summary, owner.pageId, entry);
      }
      const before = entry.blocksBytes + entry.commentsBytes;
      const bytes = serializedTableBytes(table, records);
      if (owner.kind === "blocks") entry.blocksBytes = bytes;
      else entry.commentsBytes = bytes;
      summary.totalBytes = Math.max(
        0,
        summary.totalBytes + entry.blocksBytes + entry.commentsBytes - before,
      );
      await writeEntry(storage, owner.pageId, entry);
      await storage.setMeta(BLOCK_CACHE_SUMMARY_KEY, summary);
      return true;
    },

    getOfflinePins,

    async listPageIds(storage, input) {
      if (!Number.isSafeInteger(input.limit) || input.limit <= 0) {
        throw new RangeError("Block-cache page read limit must be a positive safe integer.");
      }
      if (input.limit > budgetOptions.maxPages) {
        throw new RangeError("Block-cache page read limit exceeds its configured budget.");
      }
      if (input.cursorId !== null) assertPageId(input.cursorId);

      const summary = await readSummary(storage);
      const pageIds: string[] = [];
      const seen = new Set<string>();
      let pageId = input.cursorId ?? summary.tailId;
      while (pageId !== null && pageIds.length < input.limit) {
        if (seen.has(pageId)) {
          throw new Error("Stored block-cache page links contain a cycle.");
        }
        seen.add(pageId);
        const entry = await readEntry(storage, pageId);
        if (!entry) throw new Error("Stored block-cache page entry is missing.");
        pageIds.push(pageId);
        pageId = entry.prevId;
      }
      if (pageId !== null && seen.has(pageId)) {
        throw new Error("Stored block-cache page links contain a cycle.");
      }
      return { nextCursorId: pageId, pageIds };
    },

    async removeOfflinePins(storage, pageIds) {
      const uniqueIds = new Set<string>();
      for (const pageId of pageIds) {
        if (uniqueIds.size >= 100 && !uniqueIds.has(pageId)) {
          throw new RangeError("Offline-pin removal exceeds its configured bound.");
        }
        assertPageId(pageId);
        uniqueIds.add(pageId);
      }
      if (uniqueIds.size === 0) return;
      if (await storage.getMeta(BLOCK_CACHE_MIGRATION_KEY) !== undefined) {
        throw new RecordCacheBlockBudgetMigrationRequiredError();
      }
      const pins = await getOfflinePins(storage);
      let changed = false;
      for (const pageId of uniqueIds) {
        if (!(pageId in pins)) continue;
        delete pins[pageId];
        changed = true;
      }
      if (changed) await storage.setMeta(PINS_KEY, pins);
    },

    async reclaimTurn(storage, input) {
      const summary = await readSummary(storage);
      // Pages are looked over for letting go only while the cache is over its
      // budget (2026-11-13: each page opened read up to 128 pages' entries
      // and let none go).
      const over = input.quotaPressure || model.isUnderPressure(summary, input.estimate);
      const pins = over ? await getOfflinePins(storage) : {};
      const candidates = [];
      const seen = new Set<string>();
      let pageId = over ? summary.reclaimCursorId ?? summary.headId : null;
      while (
        pageId
        && candidates.length < Math.min(summary.count, budgetOptions.maxScanPerTurn)
        && !seen.has(pageId)
      ) {
        seen.add(pageId);
        const entry = await readEntry(storage, pageId);
        if (!entry) throw new Error("Stored block-cache reclaim entry is missing.");
        candidates.push({
          bytes: entry.blocksBytes + entry.commentsBytes,
          id: pageId,
          nextId: entry.nextId,
          protected: pins[pageId] === true || input.unsyncedPageIds.has(pageId),
        });
        pageId = entry.nextId;
        if (!pageId && candidates.length < summary.count) pageId = summary.headId;
      }
      const plan = model.planReclaimTurn({
        candidates,
        estimate: input.estimate,
        quotaPressure: input.quotaPressure,
        summary,
      });
      for (const victimId of plan.victimIds) {
        await storage.replaceTable(recordCacheTables.blocks(victimId), []);
        await storage.replaceTable(recordCacheTables.comments(victimId), []);
        await storage.removeMeta(recordCacheMeta.blocksStamp(victimId));
        await storage.removeMeta(recordCacheMeta.commentsCachedAt(victimId));
        await storage.removeMeta(recordCacheMeta.rowPage(victimId));
        if (!await removeEntry(storage, summary, victimId)) {
          throw new Error("Planned block-cache reclaim victim is missing.");
        }
      }
      if (
        summary.count !== plan.summary.count
        || summary.totalBytes !== plan.summary.totalBytes
      ) {
        throw new Error("Block-cache reclaim projection diverged from durable state.");
      }
      summary.reclaimCursorId = plan.summary.reclaimCursorId;
      summary.reclaimPending = plan.summary.reclaimPending;
      summary.reclaimScanned = plan.summary.reclaimScanned;
      await storage.setMeta(BLOCK_CACHE_SUMMARY_KEY, summary);
      return {
        pending: summary.reclaimPending,
        summary: { ...summary },
        victimIds: plan.victimIds,
      };
    },

    async removePage(storage, pageId) {
      assertPageId(pageId);
      const summary = await readSummary(storage);
      const removed = await removeEntry(storage, summary, pageId);
      if (removed) await storage.setMeta(BLOCK_CACHE_SUMMARY_KEY, summary);
      return removed;
    },

    async setOfflinePin(storage, pageId, pinned) {
      assertPageId(pageId);
      if (await storage.getMeta(BLOCK_CACHE_MIGRATION_KEY) !== undefined) {
        throw new RecordCacheBlockBudgetMigrationRequiredError();
      }
      const pins = await getOfflinePins(storage);
      if (pinned) pins[pageId] = true;
      else delete pins[pageId];
      await storage.setMeta(PINS_KEY, pins);
    },

    async touch(storage, pageId) {
      assertPageId(pageId);
      await readAhead(storage, [BLOCK_CACHE_MIGRATION_KEY, BLOCK_CACHE_SUMMARY_KEY], [pageId]);
      const summary = await readSummary(storage);
      let entry = await readEntry(storage, pageId);
      if (summary.tailId !== pageId) {
        await readAhead(storage, [], [summary.tailId, entry?.prevId ?? null, entry?.nextId ?? null]);
      }
      if (!entry) {
        entry = { blocksBytes: 0, commentsBytes: 0, nextId: null, prevId: null };
        summary.count += 1;
        await attach(storage, summary, pageId, entry);
      } else if (summary.tailId !== pageId) {
        await detach(storage, summary, pageId, entry);
        await attach(storage, summary, pageId, entry);
      }
      summary.reclaimCursorId = summary.headId;
      summary.reclaimScanned = 0;
      await storage.setMeta(BLOCK_CACHE_SUMMARY_KEY, summary);
    },
  };
}
