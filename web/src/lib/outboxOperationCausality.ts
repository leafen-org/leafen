import {
  MAX_OUTBOX_CAUSALITY_KEY_LENGTH,
  MAX_OUTBOX_CAUSALITY_KEYS,
  OUTBOX_OPERATION_SCHEMA_VERSION,
  type MutationOutboxOp,
  type OutboxOp,
} from "./outboxOperationTypes";

export interface OutboxCausalityBounds {
  maxEntries: number;
  maxEntryKeyCharacters: number;
  maxNestingDepth: number;
  maxOperationItems: number;
}

function assertSafeBound(bound: number, label: string) {
  if (!Number.isSafeInteger(bound) || bound < 0) {
    throw new RangeError(`${label} requires a caller-owned nonnegative safe-integer bound.`);
  }
}

function assertArrayBound(
  values: readonly unknown[] | undefined,
  bounds: OutboxCausalityBounds,
  label: string,
) {
  if (values && values.length > bounds.maxOperationItems) {
    throw new RangeError(`${label} exceed their caller-owned item bound.`);
  }
}

function assertBounds(bounds: OutboxCausalityBounds) {
  assertSafeBound(bounds.maxEntries, "outbox causality entries");
  assertSafeBound(bounds.maxEntryKeyCharacters, "outbox causality entry-key characters");
  assertSafeBound(bounds.maxNestingDepth, "outbox causality nesting depth");
  assertSafeBound(bounds.maxOperationItems, "outbox causality operation items");
}

function recordValue(value: unknown) {
  return value && typeof value === "object" && !Array.isArray(value)
    ? value as Record<string, unknown>
    : undefined;
}

function boundedCausalityKeys(
  values: readonly unknown[],
  bounds: OutboxCausalityBounds,
) {
  assertArrayBound(values, bounds, "outbox causality keys");
  const result: string[] = [];
  const seen = new Set<string>();
  for (const value of values) {
    if (typeof value !== "string") continue;
    const key = value.trim();
    if (!key || key.length > MAX_OUTBOX_CAUSALITY_KEY_LENGTH || seen.has(key)) continue;
    seen.add(key);
    result.push(key);
    if (result.length >= MAX_OUTBOX_CAUSALITY_KEYS) break;
  }
  return result;
}

function addIdentityKey(
  keys: string[],
  prefix: "block" | "database" | "entity" | "page" | "row",
  value: unknown,
) {
  if (keys.length >= MAX_OUTBOX_CAUSALITY_KEYS) return;
  if (typeof value !== "string" || !value.trim()) return;
  keys.push(`${prefix}:${value.trim()}`);
}

function addRecordIdentityKeys(keys: string[], value: unknown) {
  const record = recordValue(value);
  if (!record) return;
  addIdentityKey(keys, "page", record.pageId);
  addIdentityKey(keys, "page", record.sourcePageId);
  addIdentityKey(keys, "page", record.targetPageId);
  addIdentityKey(keys, "database", record.databaseId);
  addIdentityKey(keys, "row", record.rowId);
  addIdentityKey(keys, "block", record.blockId);
  addIdentityKey(keys, "entity", record.id);
}

function addPageParentIdentityKey(keys: string[], value: unknown) {
  const record = recordValue(value);
  if (record?.parentType === "page") {
    addIdentityKey(keys, "page", record.parentId);
  } else if (record?.parentType === "database") {
    addIdentityKey(keys, "database", record.parentId);
  }
}

function assertRemoteCallArrays(
  op: Extract<MutationOutboxOp, { kind: "remote_call" }>,
  bounds: OutboxCausalityBounds,
) {
  assertArrayBound(op.args, bounds, "remote-call arguments");
  assertArrayBound(op.blockStructureInvalidation?.pageIds, bounds, "block invalidation pages");
  assertArrayBound(op.blockStructureInvalidation?.blockIds, bounds, "block invalidation blocks");
  assertArrayBound(
    op.blockStructureInvalidation?.dropReconciliation?.histories,
    bounds,
    "block invalidation recovery histories",
  );
  assertArrayBound(op.terminalReconciliation?.pageIds, bounds, "terminal page reconciliation ids");
  assertArrayBound(
    op.terminalReconciliation?.commentPageIds,
    bounds,
    "terminal comment reconciliation ids",
  );
  assertArrayBound(
    op.terminalReconciliation?.databaseMetadataIds,
    bounds,
    "terminal database-metadata reconciliation ids",
  );
  assertArrayBound(
    op.terminalReconciliation?.databaseRowIds,
    bounds,
    "terminal database-row reconciliation ids",
  );
  const first = op.args[0];
  if (Array.isArray(first)) assertArrayBound(first, bounds, "remote-call first-argument items");
}

function remoteCallCausalityKeys(
  op: Extract<MutationOutboxOp, { kind: "remote_call" }>,
  bounds: OutboxCausalityBounds,
) {
  assertRemoteCallArrays(op, bounds);
  const keys: string[] = [];
  for (const pageId of op.blockStructureInvalidation?.pageIds ?? []) {
    addIdentityKey(keys, "page", pageId);
  }
  for (const blockId of op.blockStructureInvalidation?.blockIds ?? []) {
    addIdentityKey(keys, "block", blockId);
  }
  for (const pageId of op.terminalReconciliation?.pageIds ?? []) {
    addIdentityKey(keys, "page", pageId);
  }
  for (const pageId of op.terminalReconciliation?.commentPageIds ?? []) {
    addIdentityKey(keys, "page", pageId);
  }
  for (const databaseId of op.terminalReconciliation?.databaseMetadataIds ?? []) {
    addIdentityKey(keys, "database", databaseId);
  }
  for (const databaseId of op.terminalReconciliation?.databaseRowIds ?? []) {
    addIdentityKey(keys, "database", databaseId);
  }
  addRecordIdentityKeys(keys, op.effect);

  const first = recordValue(op.args[0]);
  switch (op.fn) {
    case "createPageRemote":
      addIdentityKey(keys, "page", first?.id);
      addPageParentIdentityKey(keys, first);
      break;
    case "createDatabaseRemote":
      addIdentityKey(keys, "page", first?.id);
      addIdentityKey(keys, "database", first?.id);
      addPageParentIdentityKey(keys, first);
      break;
    case "deletePageRemote":
    case "restorePageRemote":
    case "trashPageRemote":
    case "updatePageRemote":
      addIdentityKey(keys, "page", op.args[0]);
      break;
    case "createBlockRemote":
      addIdentityKey(keys, "page", first?.pageId);
      addIdentityKey(keys, "block", first?.id);
      break;
    case "createBlocksRemote":
      for (const value of Array.isArray(op.args[0]) ? op.args[0] : []) {
        addRecordIdentityKeys(keys, value);
      }
      break;
    case "deleteBlockRemote":
    case "updateBlockRemote":
      addIdentityKey(keys, "page", op.args[1]);
      addIdentityKey(keys, "page", op.args[2]);
      addIdentityKey(keys, "block", op.args[0]);
      break;
    case "deleteBlocksRemote":
    case "updateBlocksRemote":
      addIdentityKey(keys, "page", op.args[1]);
      for (const value of Array.isArray(op.args[0]) ? op.args[0] : []) {
        if (typeof value === "string") addIdentityKey(keys, "block", value);
        else addRecordIdentityKeys(keys, value);
      }
      break;
    case "createCommentRemote":
      addIdentityKey(keys, "page", first?.pageId);
      addIdentityKey(keys, "entity", first?.id);
      break;
    case "deleteCommentsRemote":
    case "updateCommentRemote":
    case "updateCommentsRemote":
      addIdentityKey(keys, "page", op.args[1]);
      addIdentityKey(keys, "page", op.args[2]);
      break;
    case "createDatabaseRowRemote":
      addIdentityKey(keys, "database", first?.databaseId);
      addIdentityKey(keys, "row", first?.id);
      break;
    case "deleteDatabaseRowRemote":
    case "moveDatabaseRowRemote":
    case "reparentDatabaseSubitemRemote":
    case "restoreDatabaseRowRemote":
    case "trashDatabaseRowRemote":
    case "updateDatabaseRowRemote":
      addIdentityKey(keys, "row", op.args[0]);
      addIdentityKey(keys, "row", op.args[1]);
      break;
    case "configureDatabaseTaskFeatureRemote":
    case "updateDatabaseDependencyRelationRemote":
      addRecordIdentityKeys(keys, op.args[0]);
      break;
    case "createPropertyRemote":
    case "createTemplateRemote":
    case "createViewRemote":
      addIdentityKey(keys, "database", first?.databaseId);
      addIdentityKey(keys, "entity", first?.id);
      break;
    case "deletePropertyRemote":
    case "deleteTemplateRemote":
    case "deleteViewRemote":
    case "updatePropertyRemote":
    case "updateTemplateRemote":
    case "updateViewRemote":
      addIdentityKey(keys, "database", op.args[1]);
      addIdentityKey(keys, "database", op.args[2]);
      addIdentityKey(keys, "entity", op.args[0]);
      break;
    case "convertPropertyToRelationRemote":
      addIdentityKey(keys, "database", op.args[2]);
      addIdentityKey(keys, "database", op.args[3]);
      addIdentityKey(keys, "entity", op.args[0]);
      break;
    default:
      for (const value of op.args) addRecordIdentityKeys(keys, value);
  }
  return keys;
}

function derivedCausalityKeys(
  entryKey: string,
  op: OutboxOp,
  bounds: OutboxCausalityBounds,
  depth = 0,
): string[] {
  if (depth > bounds.maxNestingDepth) {
    throw new RangeError("outbox operation nesting exceeds its caller-owned depth bound.");
  }
  const keys: string[] = [];
  if (op.kind === "terminal_reconciliation") {
    keys.push(...derivedCausalityKeys(entryKey, op.operation as OutboxOp, bounds, depth + 1));
  } else if (op.kind === "native_entry") {
    keys.push(...derivedCausalityKeys(entryKey, op.entry as unknown as OutboxOp, bounds, depth + 1));
  } else if (op.kind === "rejected_mutation_recovery") {
    if ("comments" in op) {
      assertArrayBound(op.comments, bounds, "comment recovery items");
      addIdentityKey(keys, "page", op.pageId);
      for (const comment of op.comments) addIdentityKey(keys, "entity", comment.id);
    } else if ("pagePatch" in op) {
      keys.push(...derivedCausalityKeys(entryKey, op.pagePatch, bounds, depth + 1));
    } else {
      const operations = op.operations ?? (op.operation ? [op.operation] : []);
      assertArrayBound(operations, bounds, "block recovery operations");
      for (const operation of operations) {
        keys.push(...derivedCausalityKeys(entryKey, operation, bounds, depth + 1));
        if (keys.length >= MAX_OUTBOX_CAUSALITY_KEYS) break;
      }
    }
  } else if (op.kind === "page_update") {
    addIdentityKey(keys, op.target === "database_row" ? "row" : "page", op.id);
    addPageParentIdentityKey(keys, op.patch);
    addPageParentIdentityKey(keys, op.expectedFieldValues);
  } else if (op.kind === "block_update") {
    addIdentityKey(keys, "page", op.hintPageId);
    addIdentityKey(keys, "block", op.id);
  } else if (op.kind === "block_create") {
    addIdentityKey(keys, "page", op.block.pageId);
    addIdentityKey(keys, "block", op.block.id);
  } else if (op.kind === "block_delete") {
    assertArrayBound(op.ids, bounds, "block deletion ids");
    addIdentityKey(keys, "page", op.hintPageId);
    for (const id of op.ids) addIdentityKey(keys, "block", id);
  } else if (op.kind === "page_recency") {
    addIdentityKey(keys, "page", op.pageId);
    addIdentityKey(keys, "block", op.blockId);
  } else if (op.kind === "content_graph_mutation" || op.kind === "page_metadata" || op.kind === "page_create") {
    addIdentityKey(keys, "page", op.pageId);
  } else {
    keys.push(...remoteCallCausalityKeys(op, bounds));
  }
  const bounded = boundedCausalityKeys(keys, {
    ...bounds,
    maxOperationItems: Math.max(bounds.maxOperationItems, MAX_OUTBOX_CAUSALITY_KEYS),
  });
  return bounded.length ? bounded : [`entry:${entryKey.slice(0, MAX_OUTBOX_CAUSALITY_KEY_LENGTH - 6)}`];
}

export function normalizeOutboxOperation(
  entryKey: string,
  op: OutboxOp,
  bounds: OutboxCausalityBounds,
): OutboxOp {
  assertBounds(bounds);
  if (entryKey.length > bounds.maxEntryKeyCharacters) {
    throw new RangeError("outbox entry key exceeds its caller-owned character bound.");
  }
  assertArrayBound(op.causalityKeys, bounds, "declared outbox causality keys");
  const declared = boundedCausalityKeys(op.causalityKeys ?? [], bounds);
  return {
    ...op,
    causalityKeys: declared.length ? declared : derivedCausalityKeys(entryKey, op, bounds),
    schemaVersion:
      typeof op.schemaVersion === "number" && Number.isInteger(op.schemaVersion)
        ? op.schemaVersion
        : OUTBOX_OPERATION_SCHEMA_VERSION,
  };
}
