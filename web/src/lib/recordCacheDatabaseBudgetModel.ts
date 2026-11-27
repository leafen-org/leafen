export const MAX_CACHED_DATABASES = 100;
export const MAX_CACHED_ROW_QUERIES_PER_DATABASE = 3;
export const MAX_DATABASE_CACHE_RECLAIM_SCAN_PER_TURN = 128;
export const MAX_DATABASE_CACHE_RECLAIM_VICTIMS_PER_TURN = 32;
export const RECORD_CACHE_DATABASE_BUDGET_STATE_VERSION = 1;
export const RECORD_CACHE_ROW_QUERY_REGISTRY_VERSION = 1;

export interface RecordCacheDatabaseBudgetSummary {
  count: number;
  headId: string | null;
  reclaimCursorId: string | null;
  reclaimPending: boolean;
  reclaimScanned: number;
  tailId: string | null;
  version: typeof RECORD_CACHE_DATABASE_BUDGET_STATE_VERSION;
}

export interface RecordCacheDatabaseReclaimCandidate {
  id: string;
  nextId: string | null;
  protected: boolean;
}

export interface RecordCacheDatabaseReclaimInput {
  candidates: readonly RecordCacheDatabaseReclaimCandidate[];
  summary: RecordCacheDatabaseBudgetSummary;
}

export interface RecordCacheDatabaseReclaimResult {
  scanned: number;
  summary: RecordCacheDatabaseBudgetSummary;
  victimIds: string[];
}

export interface RecordCacheRowQueryRegistryEntry {
  at: number;
  suffix: string;
}

export interface RecordCacheRowQueryRegistry {
  entries: RecordCacheRowQueryRegistryEntry[];
  version: typeof RECORD_CACHE_ROW_QUERY_REGISTRY_VERSION;
}

export interface RecordCacheRowQueryTouchResult {
  registry: RecordCacheRowQueryRegistry;
  victimSuffixes: string[];
}

export interface RecordCacheDatabaseBudgetOptions {
  maxDatabases: number;
  maxReclaimScanPerTurn: number;
  maxReclaimVictimsPerTurn: number;
  maxRowQueriesPerDatabase: number;
}

export interface RecordCacheDatabaseBudgetModel {
  emptyDatabaseSummary(): RecordCacheDatabaseBudgetSummary;
  emptyRowQueryRegistry(): RecordCacheRowQueryRegistry;
  /** Whether more databases are kept than the budget's, which a reclaim turn lets go. */
  isOverBudget(summary: RecordCacheDatabaseBudgetSummary): boolean;
  planDatabaseReclaimTurn(
    input: RecordCacheDatabaseReclaimInput,
  ): RecordCacheDatabaseReclaimResult;
  normalizeRowQueryRegistry(
    registry: RecordCacheRowQueryRegistry,
  ): RecordCacheRowQueryRegistry;
  touchRowQuery(
    registry: RecordCacheRowQueryRegistry,
    suffix: string,
    at: number,
  ): RecordCacheRowQueryTouchResult;
  validateDatabaseSummary(
    summary: RecordCacheDatabaseBudgetSummary,
  ): RecordCacheDatabaseBudgetSummary;
}

const DEFAULT_OPTIONS: RecordCacheDatabaseBudgetOptions = {
  maxDatabases: MAX_CACHED_DATABASES,
  maxReclaimScanPerTurn: MAX_DATABASE_CACHE_RECLAIM_SCAN_PER_TURN,
  maxReclaimVictimsPerTurn: MAX_DATABASE_CACHE_RECLAIM_VICTIMS_PER_TURN,
  maxRowQueriesPerDatabase: MAX_CACHED_ROW_QUERIES_PER_DATABASE,
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

function assertIdentity(value: string, label: string): void {
  if (typeof value !== "string" || value.length === 0) {
    throw new RangeError(`${label} is invalid.`);
  }
}

function assertLink(value: unknown, label: string): asserts value is string | null {
  if (value !== null && (typeof value !== "string" || value.length === 0)) {
    throw new RangeError(`${label} is invalid.`);
  }
}

function validateSummary(summary: RecordCacheDatabaseBudgetSummary): void {
  if (summary.version !== RECORD_CACHE_DATABASE_BUDGET_STATE_VERSION) {
    throw new RangeError("Unsupported database-cache budget state version.");
  }
  assertNonNegativeSafeInteger(summary.count, "database-cache count");
  assertLink(summary.headId, "database-cache head");
  assertLink(summary.tailId, "database-cache tail");
  assertLink(summary.reclaimCursorId, "database-cache reclaim cursor");
  if (typeof summary.reclaimPending !== "boolean") {
    throw new TypeError("Database-cache reclaim state is invalid.");
  }
  assertNonNegativeSafeInteger(summary.reclaimScanned, "database-cache reclaim scan count");
  if (summary.reclaimScanned > summary.count) {
    throw new RangeError("Database-cache reclaim scan count exceeds stored databases.");
  }
  if (
    (summary.count === 0 && (summary.headId !== null || summary.tailId !== null))
    || (summary.count > 0 && (summary.headId === null || summary.tailId === null))
  ) {
    throw new Error("Database-cache summary links disagree with its count.");
  }
}

function validateRegistry(
  registry: RecordCacheRowQueryRegistry,
  maximum: number,
): RecordCacheRowQueryRegistryEntry[] {
  if (
    !registry
    || registry.version !== RECORD_CACHE_ROW_QUERY_REGISTRY_VERSION
    || !Array.isArray(registry.entries)
  ) {
    throw new RangeError("Unsupported row-query registry version.");
  }
  if (registry.entries.length > maximum) {
    throw new RangeError("Stored row-query registry exceeds its bound.");
  }
  const seen = new Set<string>();
  return registry.entries.map((entry) => {
    if (!entry || typeof entry !== "object") {
      throw new TypeError("Stored row-query registry entry is invalid.");
    }
    assertIdentity(entry.suffix, "row-query suffix");
    assertNonNegativeSafeInteger(entry.at, "row-query recency");
    if (seen.has(entry.suffix)) {
      throw new Error("Stored row-query registry contains duplicate suffixes.");
    }
    seen.add(entry.suffix);
    return { at: entry.at, suffix: entry.suffix };
  });
}

export function createRecordCacheDatabaseBudgetModel(
  overrides: Partial<RecordCacheDatabaseBudgetOptions> = {},
): RecordCacheDatabaseBudgetModel {
  const options = { ...DEFAULT_OPTIONS, ...overrides };
  assertNonNegativeSafeInteger(options.maxDatabases, "database-cache bound");
  assertPositiveSafeInteger(options.maxReclaimScanPerTurn, "database-cache scan bound");
  assertPositiveSafeInteger(options.maxReclaimVictimsPerTurn, "database-cache victim bound");
  assertPositiveSafeInteger(options.maxRowQueriesPerDatabase, "row-query registry bound");
  if (options.maxReclaimVictimsPerTurn > options.maxReclaimScanPerTurn) {
    throw new RangeError("Database-cache victim bound cannot exceed its scan bound.");
  }
  const overBudget = (summary: RecordCacheDatabaseBudgetSummary) => summary.count > options.maxDatabases;

  return {
    emptyDatabaseSummary() {
      return {
        count: 0,
        headId: null,
        reclaimCursorId: null,
        reclaimPending: false,
        reclaimScanned: 0,
        tailId: null,
        version: RECORD_CACHE_DATABASE_BUDGET_STATE_VERSION,
      };
    },

    emptyRowQueryRegistry() {
      return {
        entries: [],
        version: RECORD_CACHE_ROW_QUERY_REGISTRY_VERSION,
      };
    },

    normalizeRowQueryRegistry(registry) {
      const entries = validateRegistry(registry, options.maxRowQueriesPerDatabase);
      entries.sort((left, right) => left.at - right.at || left.suffix.localeCompare(right.suffix));
      return {
        entries,
        version: RECORD_CACHE_ROW_QUERY_REGISTRY_VERSION,
      };
    },

    isOverBudget(summary) {
      validateSummary(summary);
      return overBudget(summary);
    },

    planDatabaseReclaimTurn(input) {
      validateSummary(input.summary);
      if (input.candidates.length > options.maxReclaimScanPerTurn) {
        throw new RangeError("Database-cache reclaim candidates exceed the scan bound.");
      }
      if (input.candidates.length > input.summary.count) {
        throw new RangeError("Database-cache reclaim candidates exceed the stored database count.");
      }
      const seen = new Set<string>();
      for (const candidate of input.candidates) {
        if (!candidate || typeof candidate !== "object") {
          throw new TypeError("Database-cache reclaim candidate is invalid.");
        }
        assertIdentity(candidate.id, "database-cache candidate identity");
        assertLink(candidate.nextId, "database-cache candidate next link");
        if (seen.has(candidate.id) || typeof candidate.protected !== "boolean") {
          throw new Error("Database-cache reclaim candidate is invalid.");
        }
        seen.add(candidate.id);
      }

      if (!overBudget(input.summary)) {
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
      const victimIds: string[] = [];
      for (const candidate of input.candidates) {
        scanned += 1;
        if (!candidate.protected) {
          victimIds.push(candidate.id);
          count -= 1;
        }
        if (
          count <= options.maxDatabases
          || victimIds.length >= options.maxReclaimVictimsPerTurn
        ) {
          break;
        }
      }
      const remainingPressure = count > options.maxDatabases;
      const nextScanned = victimIds.length > 0
        ? 0
        : Math.min(count, input.summary.reclaimScanned + scanned);
      const exhaustedProtectedPass = victimIds.length === 0 && nextScanned >= count;
      const reclaimPending = remainingPressure && !exhaustedProtectedPass;
      const lastCandidate = scanned > 0 ? input.candidates[scanned - 1] : undefined;
      return {
        scanned,
        summary: {
          ...input.summary,
          count,
          reclaimCursorId: reclaimPending
            ? lastCandidate?.nextId ?? input.summary.headId
            : null,
          reclaimPending,
          reclaimScanned: reclaimPending ? nextScanned : 0,
        },
        victimIds,
      };
    },

    touchRowQuery(registry, suffix, at) {
      assertIdentity(suffix, "row-query suffix");
      assertNonNegativeSafeInteger(at, "row-query recency");
      const entries = validateRegistry(registry, options.maxRowQueriesPerDatabase)
        .filter((entry) => entry.suffix !== suffix);
      entries.push({ at, suffix });
      entries.sort((left, right) => left.at - right.at || left.suffix.localeCompare(right.suffix));
      const victimSuffixes = [];
      while (entries.length > options.maxRowQueriesPerDatabase) {
        const victim = entries.shift();
        if (victim) victimSuffixes.push(victim.suffix);
      }
      return {
        registry: {
          entries,
          version: RECORD_CACHE_ROW_QUERY_REGISTRY_VERSION,
        },
        victimSuffixes,
      };
    },

    validateDatabaseSummary(summary) {
      validateSummary(summary);
      return { ...summary };
    },
  };
}
