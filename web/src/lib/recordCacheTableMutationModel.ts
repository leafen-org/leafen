export interface RecordCacheProjectionRecord<V = unknown> {
  id: string;
  value: V;
}

export interface CachedRowPageAppendMeta {
  feedStamp?: string;
  hasMore: boolean;
  nextCursor?: string;
  nextOffset?: number;
  pagePredecessors?: Record<string, unknown>;
  queryKey: string;
  rowIds: string[];
  totalCount?: number;
}

export interface RecordCacheTableMutationBounds {
  maxPropertyNodesPerOperation: number;
  maxRecordIdsPerOperation: number;
  maxRecordsPerOperation: number;
}

export interface RecordCacheConditionalRecords<V> {
  changed: boolean;
  records: RecordCacheProjectionRecord<V>[];
}

export interface RecordCacheRelatedReconcileInput<V> {
  currentRelated: readonly RecordCacheProjectionRecord<V>[];
  sources: readonly RecordCacheProjectionRecord<Record<string, unknown>>[];
  sourceId: string;
  supplied: readonly RecordCacheProjectionRecord<V>[];
}

export interface RecordCacheRelatedReconcileResult<V> {
  applies: boolean;
  records: RecordCacheProjectionRecord<V>[];
}

export interface RecordCacheRowPageAppendInput<V> {
  currentMeta: CachedRowPageAppendMeta | undefined;
  currentRelated: readonly RecordCacheProjectionRecord<V>[];
  currentRows: readonly RecordCacheProjectionRecord<V>[];
  expectedOffset: number;
  meta: CachedRowPageAppendMeta;
  records: readonly RecordCacheProjectionRecord<V>[];
  relatedRecords: readonly RecordCacheProjectionRecord<V>[];
}

export type RecordCacheRowPageAppendResult<V> =
  | {
      applies: false;
      meta: CachedRowPageAppendMeta | undefined;
      records: RecordCacheProjectionRecord<V>[];
      relatedRecords: RecordCacheProjectionRecord<V>[];
    }
  | {
      applies: true;
      meta: CachedRowPageAppendMeta;
      records: RecordCacheProjectionRecord<V>[];
      relatedRecords: RecordCacheProjectionRecord<V>[];
    };

export interface RecordCacheTableMutationModel {
  appendRowPage<V>(
    input: RecordCacheRowPageAppendInput<V>,
  ): RecordCacheRowPageAppendResult<V>;
  reconcileRecords<V>(
    current: readonly RecordCacheProjectionRecord<V>[],
    input: {
      removeIds?: Iterable<string>;
      upserts?: Iterable<RecordCacheProjectionRecord<V>>;
    },
  ): RecordCacheProjectionRecord<V>[];
  reconcileRelatedRecords<V>(
    input: RecordCacheRelatedReconcileInput<V>,
  ): RecordCacheRelatedReconcileResult<V>;
  replaceRecordIfPresent<V>(
    current: readonly RecordCacheProjectionRecord<V>[],
    record: RecordCacheProjectionRecord<V>,
  ): RecordCacheConditionalRecords<V>;
  replaceRecordsIfPresent<V>(
    current: readonly RecordCacheProjectionRecord<V>[],
    records: Iterable<RecordCacheProjectionRecord<V>>,
  ): RecordCacheConditionalRecords<V>;
  updateRecordsIfPresent<V>(
    current: readonly RecordCacheProjectionRecord<V>[],
    recordIds: Iterable<string>,
    update: (value: V) => V,
  ): RecordCacheConditionalRecords<V>;
  upsertRecord<V>(
    current: readonly RecordCacheProjectionRecord<V>[],
    record: RecordCacheProjectionRecord<V>,
  ): RecordCacheProjectionRecord<V>[];
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
    throw new RangeError(`${label} is invalid.`);
  }
}

function collectBounded<T>(
  values: Iterable<T>,
  maximum: number,
  label: string,
): T[] {
  const collected: T[] = [];
  for (const value of values) {
    if (collected.length >= maximum) {
      throw new RangeError(`${label} exceeds its configured bound.`);
    }
    collected.push(value);
  }
  return collected;
}

export function createRecordCacheTableMutationModel(
  bounds: RecordCacheTableMutationBounds,
): RecordCacheTableMutationModel {
  assertPositiveSafeInteger(
    bounds.maxPropertyNodesPerOperation,
    "maxPropertyNodesPerOperation",
  );
  assertPositiveSafeInteger(bounds.maxRecordIdsPerOperation, "maxRecordIdsPerOperation");
  assertPositiveSafeInteger(bounds.maxRecordsPerOperation, "maxRecordsPerOperation");

  const validateRecord = <V>(
    record: RecordCacheProjectionRecord<V>,
  ): RecordCacheProjectionRecord<V> => {
    if (!record || typeof record !== "object") {
      throw new TypeError("Record-cache projection record is invalid.");
    }
    assertIdentity(record.id, "record-cache projection identity");
    return record;
  };

  const validateRecords = <V>(
    records: readonly RecordCacheProjectionRecord<V>[],
    label: string,
  ): RecordCacheProjectionRecord<V>[] => {
    if (!Array.isArray(records) || records.length > bounds.maxRecordsPerOperation) {
      throw new RangeError(`${label} exceeds its configured bound.`);
    }
    const seen = new Set<string>();
    return records.map((record) => {
      validateRecord(record);
      if (seen.has(record.id)) {
        throw new Error(`${label} contains duplicate record identities.`);
      }
      seen.add(record.id);
      return record;
    });
  };

  const collectRecords = <V>(
    records: Iterable<RecordCacheProjectionRecord<V>>,
    label: string,
  ): RecordCacheProjectionRecord<V>[] => collectBounded(
    records,
    bounds.maxRecordsPerOperation,
    label,
  ).map(validateRecord);

  const collectIds = (ids: Iterable<string>, label: string): string[] => collectBounded(
    ids,
    bounds.maxRecordIdsPerOperation,
    label,
  ).map((id) => {
    assertIdentity(id, "record-cache projection identity");
    return id;
  });

  const validateMeta = (meta: CachedRowPageAppendMeta, label: string): void => {
    if (!meta || typeof meta !== "object" || typeof meta.hasMore !== "boolean") {
      throw new TypeError(`${label} is invalid.`);
    }
    assertIdentity(meta.queryKey, `${label} query key`);
    collectIds(meta.rowIds, `${label} row identities`);
    if (meta.nextOffset !== undefined) {
      assertNonNegativeSafeInteger(meta.nextOffset, `${label} next offset`);
    }
  };

  const mergeRecords = <V>(
    current: readonly RecordCacheProjectionRecord<V>[],
    incoming: readonly RecordCacheProjectionRecord<V>[],
  ): RecordCacheProjectionRecord<V>[] => {
    const byId = new Map(current.map((record) => [record.id, record]));
    for (const record of incoming) byId.set(record.id, record);
    const merged = Array.from(byId.values());
    if (merged.length > bounds.maxRecordsPerOperation) {
      throw new RangeError("Merged record-cache projection exceeds its configured bound.");
    }
    return merged;
  };

  return {
    appendRowPage(input) {
      assertNonNegativeSafeInteger(input.expectedOffset, "expected row-page offset");
      const currentRows = validateRecords(input.currentRows, "current row projection");
      const currentRelated = validateRecords(
        input.currentRelated,
        "current related-page projection",
      );
      const records = collectRecords(input.records, "incoming row projection");
      const relatedRecords = collectRecords(
        input.relatedRecords,
        "incoming related-page projection",
      );
      validateMeta(input.meta, "incoming row-page metadata");
      if (input.currentMeta) validateMeta(input.currentMeta, "current row-page metadata");
      if (
        !input.currentMeta
        || input.currentMeta.queryKey !== input.meta.queryKey
        || input.currentMeta.rowIds.length !== input.expectedOffset
        || input.meta.rowIds.length < input.expectedOffset
        || input.currentMeta.rowIds.some((id, index) => input.meta.rowIds[index] !== id)
      ) {
        return {
          applies: false,
          meta: input.currentMeta,
          records: currentRows,
          relatedRecords: currentRelated,
        };
      }
      const pagePredecessors = {
        ...(input.currentMeta.pagePredecessors ?? {}),
        ...(input.meta.pagePredecessors ?? {}),
      };
      return {
        applies: true,
        meta: {
          ...input.meta,
          ...(Object.keys(pagePredecessors).length > 0 ? { pagePredecessors } : {}),
        },
        records: mergeRecords(currentRows, records),
        relatedRecords: mergeRecords(currentRelated, relatedRecords),
      };
    },

    reconcileRecords(current, input) {
      const retained = validateRecords(current, "current record projection");
      const removeIds = new Set(collectIds(input.removeIds ?? [], "reconcile removals"));
      const upserts = new Map(
        collectRecords(input.upserts ?? [], "reconcile upserts")
          .map((record) => [record.id, record]),
      );
      const next = retained.filter((record) => !removeIds.has(record.id));
      const indexById = new Map(next.map((record, index) => [record.id, index]));
      for (const record of upserts.values()) {
        const index = indexById.get(record.id);
        if (index === undefined) {
          if (next.length >= bounds.maxRecordsPerOperation) {
            throw new RangeError("Reconciled record projection exceeds its configured bound.");
          }
          indexById.set(record.id, next.length);
          next.push(record);
        } else {
          next[index] = record;
        }
      }
      return next;
    },

    reconcileRelatedRecords(input) {
      assertIdentity(input.sourceId, "related source identity");
      const sources = validateRecords(input.sources, "related source projection");
      const currentRelated = validateRecords(
        input.currentRelated,
        "current related-page projection",
      );
      const supplied = collectRecords(input.supplied, "supplied related-page projection");
      if (!sources.some((record) => record.id === input.sourceId)) {
        return { applies: false, records: currentRelated };
      }
      const referencedIds = new Set<string>();
      const seen = new Set<object>();
      let visited = 0;
      const collectStrings = (value: unknown): void => {
        visited += 1;
        if (visited > bounds.maxPropertyNodesPerOperation) {
          throw new RangeError("Related property traversal exceeds its configured bound.");
        }
        if (typeof value === "string") {
          referencedIds.add(value);
          return;
        }
        if (!value || typeof value !== "object" || seen.has(value)) return;
        seen.add(value);
        if (Array.isArray(value)) {
          for (const item of value) collectStrings(item);
        } else {
          for (const nested of Object.values(value as Record<string, unknown>)) {
            collectStrings(nested);
          }
        }
      };
      for (const source of sources) {
        const properties = source.value?.properties;
        if (properties !== undefined) collectStrings(properties);
      }
      const relatedById = new Map(
        currentRelated
          .filter((record) => referencedIds.has(record.id))
          .map((record) => [record.id, record]),
      );
      for (const record of supplied) {
        if (referencedIds.has(record.id)) relatedById.set(record.id, record);
      }
      return { applies: true, records: Array.from(relatedById.values()) };
    },

    replaceRecordIfPresent(current, record) {
      validateRecord(record);
      const records = validateRecords(current, "current record projection");
      const index = records.findIndex((candidate) => candidate.id === record.id);
      if (index < 0) return { changed: false, records };
      const next = records.slice();
      next[index] = record;
      return { changed: true, records: next };
    },

    replaceRecordsIfPresent(current, records) {
      const retained = validateRecords(current, "current record projection");
      const replacements = new Map(
        collectRecords(records, "replacement record projection")
          .map((record) => [record.id, record]),
      );
      let changed = false;
      const next = retained.map((record) => {
        const replacement = replacements.get(record.id);
        if (!replacement) return record;
        changed = true;
        return replacement;
      });
      return { changed, records: changed ? next : retained };
    },

    updateRecordsIfPresent(current, recordIds, update) {
      const retained = validateRecords(current, "current record projection");
      const ids = new Set(collectIds(recordIds, "record update identities"));
      let changed = false;
      const next = retained.map((record) => {
        if (!ids.has(record.id)) return record;
        const value = update(record.value);
        if (value === record.value) return record;
        changed = true;
        return { ...record, value };
      });
      return { changed, records: changed ? next : retained };
    },

    upsertRecord(current, record) {
      validateRecord(record);
      const records = validateRecords(current, "current record projection");
      const index = records.findIndex((candidate) => candidate.id === record.id);
      if (index < 0) {
        if (records.length >= bounds.maxRecordsPerOperation) {
          throw new RangeError("Upserted record projection exceeds its configured bound.");
        }
        return [...records, record];
      }
      const next = records.slice();
      next[index] = record;
      return next;
    },
  };
}
