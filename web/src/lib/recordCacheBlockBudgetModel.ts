export const MAX_CACHED_BLOCK_PAGES = 5_000;
export const MAX_CACHED_BLOCK_BYTES = 256 * 1024 * 1024;
export const MAX_BLOCK_CACHE_RECLAIM_PAGES_PER_TURN = 32;
export const MAX_BLOCK_CACHE_RECLAIM_SCAN_PER_TURN = 128;
export const RECORD_CACHE_BLOCK_BUDGET_STATE_VERSION = 1;

export interface RecordCacheBlockStorageEstimate {
  quota: number;
  usage: number;
}

export interface RecordCacheBlockBudgetSummary {
  count: number;
  headId: string | null;
  reclaimCursorId: string | null;
  reclaimPending: boolean;
  reclaimScanned: number;
  tailId: string | null;
  totalBytes: number;
  version: typeof RECORD_CACHE_BLOCK_BUDGET_STATE_VERSION;
}

export interface RecordCacheBlockReclaimCandidate {
  bytes: number;
  id: string;
  nextId: string | null;
  protected: boolean;
}

export interface RecordCacheBlockReclaimInput {
  candidates: readonly RecordCacheBlockReclaimCandidate[];
  estimate: RecordCacheBlockStorageEstimate | null;
  quotaPressure: boolean;
  summary: RecordCacheBlockBudgetSummary;
}

export interface RecordCacheBlockReclaimResult {
  scanned: number;
  summary: RecordCacheBlockBudgetSummary;
  victimIds: string[];
}

export interface RecordCacheBlockBudgetOptions {
  maxBytes: number;
  maxPages: number;
  maxScanPerTurn: number;
  maxVictimsPerTurn: number;
}

export interface RecordCacheBlockBudgetModel {
  deriveByteBudget(
    estimate: RecordCacheBlockStorageEstimate | null,
    ownEstimatedBytes: number,
  ): number;
  emptySummary(): RecordCacheBlockBudgetSummary;
  isUnderPressure(
    summary: RecordCacheBlockBudgetSummary,
    estimate: RecordCacheBlockStorageEstimate | null,
  ): boolean;
  planReclaimTurn(input: RecordCacheBlockReclaimInput): RecordCacheBlockReclaimResult;
}

const DEFAULT_OPTIONS: RecordCacheBlockBudgetOptions = {
  maxBytes: MAX_CACHED_BLOCK_BYTES,
  maxPages: MAX_CACHED_BLOCK_PAGES,
  maxScanPerTurn: MAX_BLOCK_CACHE_RECLAIM_SCAN_PER_TURN,
  maxVictimsPerTurn: MAX_BLOCK_CACHE_RECLAIM_PAGES_PER_TURN,
};

function assertNonNegativeSafeInteger(value: number, label: string): void {
  if (!Number.isSafeInteger(value) || value < 0) {
    throw new RangeError(`${label} must be a non-negative safe integer.`);
  }
}

function assertPositiveSafeInteger(value: number, label: string): void {
  if (!Number.isSafeInteger(value) || value <= 0) {
    throw new RangeError(`${label} must be a positive safe integer.`);
  }
}

function validateSummary(summary: RecordCacheBlockBudgetSummary): void {
  if (summary.version !== RECORD_CACHE_BLOCK_BUDGET_STATE_VERSION) {
    throw new RangeError("Unsupported record-cache block-budget state version.");
  }
  assertNonNegativeSafeInteger(summary.count, "block-cache page count");
  assertNonNegativeSafeInteger(summary.reclaimScanned, "block-cache reclaim scan count");
  assertNonNegativeSafeInteger(summary.totalBytes, "block-cache byte count");
  if (summary.reclaimScanned > summary.count) {
    throw new RangeError("Block-cache reclaim scan count exceeds stored pages.");
  }
}

function validateEstimate(estimate: RecordCacheBlockStorageEstimate | null): void {
  if (!estimate) return;
  assertPositiveSafeInteger(estimate.quota, "storage quota");
  assertNonNegativeSafeInteger(estimate.usage, "storage usage");
}

export function createRecordCacheBlockBudgetModel(
  overrides: Partial<RecordCacheBlockBudgetOptions> = {},
): RecordCacheBlockBudgetModel {
  const options = { ...DEFAULT_OPTIONS, ...overrides };
  assertNonNegativeSafeInteger(options.maxBytes, "block-cache byte bound");
  assertNonNegativeSafeInteger(options.maxPages, "block-cache page bound");
  assertPositiveSafeInteger(options.maxScanPerTurn, "block-cache scan bound");
  assertPositiveSafeInteger(options.maxVictimsPerTurn, "block-cache victim bound");
  if (options.maxVictimsPerTurn > options.maxScanPerTurn) {
    throw new RangeError("Block-cache victim bound cannot exceed its scan bound.");
  }

  const deriveByteBudget = (
    estimate: RecordCacheBlockStorageEstimate | null,
    ownEstimatedBytes: number,
  ): number => {
    assertNonNegativeSafeInteger(ownEstimatedBytes, "owned block-cache bytes");
    validateEstimate(estimate);
    if (!estimate) return options.maxBytes;
    const otherUsage = Math.max(0, estimate.usage - ownEstimatedBytes);
    const remainingAfterOtherUsage = Math.max(0, estimate.quota - otherUsage);
    return Math.max(
      0,
      Math.floor(Math.min(
        options.maxBytes,
        estimate.quota * 0.25,
        remainingAfterOtherUsage * 0.5,
      )),
    );
  };

  const isUnderPressure = (
    summary: RecordCacheBlockBudgetSummary,
    estimate: RecordCacheBlockStorageEstimate | null,
  ): boolean => {
    validateSummary(summary);
    return (
      summary.count > options.maxPages
      || summary.totalBytes > deriveByteBudget(estimate, summary.totalBytes)
    );
  };

  return {
    deriveByteBudget,

    emptySummary() {
      return {
        count: 0,
        headId: null,
        reclaimCursorId: null,
        reclaimPending: false,
        reclaimScanned: 0,
        tailId: null,
        totalBytes: 0,
        version: RECORD_CACHE_BLOCK_BUDGET_STATE_VERSION,
      };
    },

    isUnderPressure,

    planReclaimTurn(input) {
      validateSummary(input.summary);
      validateEstimate(input.estimate);
      if (input.candidates.length > options.maxScanPerTurn) {
        throw new RangeError("Block-cache reclaim candidates exceed the scan bound.");
      }
      if (input.candidates.length > input.summary.count) {
        throw new RangeError("Block-cache reclaim candidates exceed the stored page count.");
      }

      const seen = new Set<string>();
      for (const candidate of input.candidates) {
        if (!candidate.id || seen.has(candidate.id)) {
          throw new RangeError("Block-cache reclaim candidate identity is invalid.");
        }
        seen.add(candidate.id);
        assertNonNegativeSafeInteger(candidate.bytes, "block-cache candidate bytes");
      }

      if (!input.quotaPressure && !isUnderPressure(input.summary, input.estimate)) {
        return {
          scanned: 0,
          summary: {
            ...input.summary,
            reclaimCursorId: null,
            reclaimPending: false,
            reclaimScanned: 0,
          },
          victimIds: [],
        };
      }

      let count = input.summary.count;
      let scanned = 0;
      let totalBytes = input.summary.totalBytes;
      const victimIds: string[] = [];
      for (const candidate of input.candidates) {
        scanned += 1;
        if (!candidate.protected) {
          victimIds.push(candidate.id);
          count = Math.max(0, count - 1);
          totalBytes = Math.max(0, totalBytes - candidate.bytes);
        }
        if (victimIds.length >= options.maxVictimsPerTurn) break;
        const projected = { ...input.summary, count, totalBytes };
        if (!input.quotaPressure && !isUnderPressure(projected, input.estimate)) break;
      }

      const projected = { ...input.summary, count, totalBytes };
      const remainingPressure = (
        !input.quotaPressure
        && isUnderPressure(projected, input.estimate)
      );
      const nextScanned = victimIds.length > 0
        ? 0
        : Math.min(count, input.summary.reclaimScanned + scanned);
      const exhaustedEligiblePass = victimIds.length === 0 && nextScanned >= count;
      const reclaimPending = remainingPressure && !exhaustedEligiblePass;
      const lastCandidate = scanned > 0 ? input.candidates[scanned - 1] : undefined;
      return {
        scanned,
        summary: {
          ...projected,
          reclaimCursorId: reclaimPending
            ? lastCandidate?.nextId ?? input.summary.headId
            : null,
          reclaimPending,
          reclaimScanned: reclaimPending ? nextScanned : 0,
        },
        victimIds,
      };
    },
  };
}
