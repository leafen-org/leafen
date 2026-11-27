export interface RecordCacheTerminalAbsenceBounds {
  maxBlockIdsPerOperation: number;
  maxPageIdsPerOperation: number;
  maxPageScopesPerOperation: number;
}

export interface RecordCacheTerminalAbsenceInput {
  blocksByPage?: readonly RecordCacheTerminalAbsenceScope[];
  pageIds?: readonly string[];
}

export interface RecordCacheTerminalAbsenceScope {
  blockIds: readonly string[];
  pageId: string;
}

export interface NormalizedRecordCacheTerminalAbsence {
  blocksByPage: Array<{ blockIds: string[]; pageId: string }>;
  pageIds: string[];
}

export type CachedBootstrapForTerminalAbsence = {
  pageInventories?: Record<
    string,
    ({ itemIds: string[] } & Record<string, unknown>) | undefined
  >;
  pageRoles?: Record<string, unknown>;
  pages?: Array<{ id: string } & Record<string, unknown>>;
  sharedPageIds?: string[];
} & Record<string, unknown>;

export interface RecordCacheTerminalAbsenceModel {
  normalize(
    input: RecordCacheTerminalAbsenceInput,
  ): NormalizedRecordCacheTerminalAbsence;
  projectBootstrap<T extends CachedBootstrapForTerminalAbsence>(
    current: T | undefined,
    plan: NormalizedRecordCacheTerminalAbsence,
  ): T | undefined;
}

function assertPositiveSafeInteger(value: number, label: string): void {
  if (!Number.isSafeInteger(value) || value <= 0) {
    throw new RangeError(`${label} must be a positive safe integer.`);
  }
}

function assertIdentity(value: unknown, label: string): value is string {
  if (typeof value !== "string") {
    throw new TypeError(`${label} must be a string.`);
  }
  return value.length > 0;
}

function assertArrayWithinBound(
  value: unknown,
  maximum: number,
  label: string,
): asserts value is readonly unknown[] {
  if (!Array.isArray(value)) {
    throw new TypeError(`${label} must be an array.`);
  }
  if (value.length > maximum) {
    throw new RangeError(`${label} exceeds its configured bound.`);
  }
}

export function createRecordCacheTerminalAbsenceModel(
  bounds: RecordCacheTerminalAbsenceBounds,
): RecordCacheTerminalAbsenceModel {
  assertPositiveSafeInteger(
    bounds.maxBlockIdsPerOperation,
    "maxBlockIdsPerOperation",
  );
  assertPositiveSafeInteger(
    bounds.maxPageIdsPerOperation,
    "maxPageIdsPerOperation",
  );
  assertPositiveSafeInteger(
    bounds.maxPageScopesPerOperation,
    "maxPageScopesPerOperation",
  );

  const normalize = (
    input: RecordCacheTerminalAbsenceInput,
  ): NormalizedRecordCacheTerminalAbsence => {
    if (!input || typeof input !== "object") {
      throw new TypeError("Terminal local-absence input is invalid.");
    }
    const rawPageIds = input.pageIds ?? [];
    const rawScopes = input.blocksByPage ?? [];
    assertArrayWithinBound(
      rawPageIds,
      bounds.maxPageIdsPerOperation,
      "Terminal local-absence page identities",
    );
    assertArrayWithinBound(
      rawScopes,
      bounds.maxPageScopesPerOperation,
      "Terminal local-absence page scopes",
    );

    const pageIds: string[] = [];
    const pageIdSet = new Set<string>();
    for (const pageId of rawPageIds) {
      if (!assertIdentity(pageId, "Terminal local-absence page identity")) continue;
      if (pageIdSet.has(pageId)) continue;
      pageIdSet.add(pageId);
      pageIds.push(pageId);
    }

    let rawBlockIdCount = 0;
    const blockIdsByPage = new Map<string, string[]>();
    const blockIdSetsByPage = new Map<string, Set<string>>();
    for (const scope of rawScopes) {
      if (!scope || typeof scope !== "object") {
        throw new TypeError("Terminal local-absence page scope is invalid.");
      }
      assertArrayWithinBound(
        scope.blockIds,
        bounds.maxBlockIdsPerOperation,
        "Terminal local-absence block identities",
      );
      rawBlockIdCount += scope.blockIds.length;
      if (rawBlockIdCount > bounds.maxBlockIdsPerOperation) {
        throw new RangeError(
          "Terminal local-absence block identities exceed their configured bound.",
        );
      }
      if (!assertIdentity(scope.pageId, "Terminal local-absence scope page identity")) {
        for (const blockId of scope.blockIds) {
          assertIdentity(blockId, "Terminal local-absence block identity");
        }
        continue;
      }
      const blockIds = blockIdsByPage.get(scope.pageId) ?? [];
      const blockIdSet = blockIdSetsByPage.get(scope.pageId) ?? new Set<string>();
      for (const blockId of scope.blockIds) {
        if (!assertIdentity(blockId, "Terminal local-absence block identity")) continue;
        if (blockIdSet.has(blockId)) continue;
        blockIdSet.add(blockId);
        blockIds.push(blockId);
      }
      blockIdsByPage.set(scope.pageId, blockIds);
      blockIdSetsByPage.set(scope.pageId, blockIdSet);
    }

    return {
      blocksByPage: Array.from(blockIdsByPage, ([pageId, blockIds]) => ({
        blockIds,
        pageId,
      })).filter(({ blockIds, pageId }) => (
        blockIds.length > 0 && !pageIdSet.has(pageId)
      )),
      pageIds,
    };
  };

  const projectBootstrap = <T extends CachedBootstrapForTerminalAbsence>(
    current: T | undefined,
    plan: NormalizedRecordCacheTerminalAbsence,
  ): T | undefined => {
    if (!current) return undefined;
    const normalized = normalize(plan);
    if (normalized.pageIds.length === 0) return current;
    const absentPages = new Set(normalized.pageIds);
    const pageRoles = { ...(current.pageRoles ?? {}) };
    for (const pageId of normalized.pageIds) delete pageRoles[pageId];

    const pageInventories = current.pageInventories
      ? Object.fromEntries(
          Object.entries(current.pageInventories).map(([inventory, snapshot]) => {
            if (!snapshot) return [inventory, snapshot];
            if (!Array.isArray(snapshot.itemIds)) {
              throw new TypeError(
                `Terminal local-absence ${inventory} inventory is invalid.`,
              );
            }
            return [inventory, {
              ...snapshot,
              itemIds: snapshot.itemIds.filter((pageId) => !absentPages.has(pageId)),
            }];
          }),
        )
      : undefined;

    return {
      ...current,
      pages: current.pages?.filter((page) => !absentPages.has(page.id)),
      pageRoles,
      sharedPageIds: current.sharedPageIds?.filter(
        (pageId) => !absentPages.has(pageId),
      ),
      ...(pageInventories ? { pageInventories } : {}),
    } as T;
  };

  return { normalize, projectBootstrap };
}
