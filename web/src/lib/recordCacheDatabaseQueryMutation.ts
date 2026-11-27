import type { RecordCacheDatabaseBudgetStorage } from "./recordCacheDatabaseBudgetStorage";
import type { RecordCacheStorage } from "./recordCacheStorageModel";

export interface RecordCacheDatabaseQueryKeyspace {
  rowQuery(
    databaseId: string,
    suffix: string,
  ): { dataTable: string; metaKey: string };
}

export interface RecordCacheDatabaseQueryMutationOptions {
  databaseBudget: Pick<
    RecordCacheDatabaseBudgetStorage,
    "listRowQuerySuffixes"
  >;
  keyspace: RecordCacheDatabaseQueryKeyspace;
  maxDatabaseIdsPerOperation: number;
  maxRowIdsPerQuery: number;
  maxRowQueriesPerDatabase: number;
}

export interface RecordCacheDatabaseQueryMutation {
  collectOwnedRowIds(
    storage: RecordCacheStorage,
    databaseId: string,
  ): Promise<string[]>;
  invalidateFreshness(
    storage: RecordCacheStorage,
    databaseIds: readonly string[],
  ): Promise<void>;
  removeRow(
    storage: RecordCacheStorage,
    databaseId: string,
    rowId: string,
  ): Promise<void>;
}

type CachedDatabaseRowMembership = Record<string, unknown> & {
  feedStamp?: string;
  pagePredecessors?: Record<string, unknown>;
  rowIds: string[];
  totalCount?: number;
};

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

function asMembership(
  value: unknown,
  maximumRows: number,
): CachedDatabaseRowMembership {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new Error("Stored database-query membership is invalid.");
  }
  const membership = value as CachedDatabaseRowMembership;
  if (!Array.isArray(membership.rowIds)) {
    throw new Error("Stored database-query rowIds are invalid.");
  }
  if (membership.rowIds.length > maximumRows) {
    throw new RangeError("Stored database-query rowIds exceed their configured bound.");
  }
  const unique = new Set<string>();
  membership.rowIds.forEach((rowId) => {
    assertIdentity(rowId, "stored database-query row identity");
    if (unique.has(rowId)) {
      throw new Error("Stored database-query rowIds contain a duplicate.");
    }
    unique.add(rowId);
  });
  if (
    membership.totalCount !== undefined
    && (!Number.isSafeInteger(membership.totalCount) || membership.totalCount < 0)
  ) {
    throw new Error("Stored database-query total count is invalid.");
  }
  if (
    membership.pagePredecessors !== undefined
    && (
      !membership.pagePredecessors
      || typeof membership.pagePredecessors !== "object"
      || Array.isArray(membership.pagePredecessors)
    )
  ) {
    throw new Error("Stored database-query page predecessors are invalid.");
  }
  return membership;
}

export function createRecordCacheDatabaseQueryMutation(
  options: RecordCacheDatabaseQueryMutationOptions,
): RecordCacheDatabaseQueryMutation {
  assertPositiveSafeInteger(
    options.maxDatabaseIdsPerOperation,
    "maxDatabaseIdsPerOperation",
  );
  assertPositiveSafeInteger(options.maxRowIdsPerQuery, "maxRowIdsPerQuery");
  assertPositiveSafeInteger(
    options.maxRowQueriesPerDatabase,
    "maxRowQueriesPerDatabase",
  );

  const databaseIds = (values: readonly string[]): string[] => {
    if (!Array.isArray(values) || values.length > options.maxDatabaseIdsPerOperation) {
      throw new RangeError("Database-query invalidation exceeds its configured bound.");
    }
    const unique = new Set<string>();
    values.forEach((databaseId) => {
      assertIdentity(databaseId, "database-query database identity");
      unique.add(databaseId);
    });
    return Array.from(unique);
  };

  const suffixes = async (
    storage: RecordCacheStorage,
    databaseId: string,
  ): Promise<string[]> => {
    assertIdentity(databaseId, "database-query database identity");
    const values = await options.databaseBudget.listRowQuerySuffixes(
      storage,
      databaseId,
    );
    if (
      !Array.isArray(values)
      || values.length > options.maxRowQueriesPerDatabase
    ) {
      throw new RangeError("Database-query registry exceeds its configured bound.");
    }
    const unique = new Set<string>();
    values.forEach((suffix) => {
      assertIdentity(suffix, "database-query suffix");
      if (unique.has(suffix)) {
        throw new Error("Database-query registry contains a duplicate suffix.");
      }
      unique.add(suffix);
    });
    return values;
  };

  return {
    async collectOwnedRowIds(storage, databaseId) {
      const rowIds: string[] = [];
      const unique = new Set<string>();
      for (const suffix of await suffixes(storage, databaseId)) {
        const { metaKey } = options.keyspace.rowQuery(databaseId, suffix);
        const raw = await storage.getMeta(metaKey);
        if (raw === undefined) continue;
        const membership = asMembership(raw, options.maxRowIdsPerQuery);
        membership.rowIds.forEach((rowId) => {
          if (unique.has(rowId)) return;
          unique.add(rowId);
          rowIds.push(rowId);
        });
      }
      const maximum = Math.min(options.maxRowIdsPerQuery * options.maxRowQueriesPerDatabase, Number.MAX_SAFE_INTEGER);
      if (!Number.isSafeInteger(maximum) || rowIds.length > maximum) {
        throw new RangeError("Database-owned row identities exceed their bound.");
      }
      return rowIds;
    },

    async invalidateFreshness(storage, rawDatabaseIds) {
      for (const databaseId of databaseIds(rawDatabaseIds)) {
        for (const suffix of await suffixes(storage, databaseId)) {
          const { metaKey } = options.keyspace.rowQuery(databaseId, suffix);
          const current = await storage.getMeta<Record<string, unknown>>(metaKey);
          if (
            !current
            || typeof current !== "object"
            || Array.isArray(current)
            || !Object.prototype.hasOwnProperty.call(current, "feedStamp")
          ) continue;
          const { feedStamp: _feedStamp, ...withoutFeedStamp } = current;
          await storage.setMeta(metaKey, withoutFeedStamp);
        }
      }
    },

    async removeRow(storage, databaseId, rowId) {
      assertIdentity(rowId, "database-query row identity");
      for (const suffix of await suffixes(storage, databaseId)) {
        const { dataTable, metaKey } = options.keyspace.rowQuery(databaseId, suffix);
        const raw = await storage.getMeta(metaKey);
        if (raw === undefined) continue;
        const current = asMembership(raw, options.maxRowIdsPerQuery);
        if (!current.rowIds.includes(rowId)) continue;
        await storage.removeRecords(dataTable, [rowId]);
        const pagePredecessors = { ...(current.pagePredecessors ?? {}) };
        delete pagePredecessors[rowId];
        const next: CachedDatabaseRowMembership = {
          ...current,
          rowIds: current.rowIds.filter((id) => id !== rowId),
          ...(typeof current.totalCount === "number"
            ? { totalCount: Math.max(0, current.totalCount - 1) }
            : {}),
        };
        if (Object.keys(pagePredecessors).length > 0) {
          next.pagePredecessors = pagePredecessors;
        } else {
          delete next.pagePredecessors;
        }
        await storage.setMeta(metaKey, next);
      }
    },
  };
}
