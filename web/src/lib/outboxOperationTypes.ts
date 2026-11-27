import type {
  Block,
  Comment,
  DbProperty,
  DbTemplate,
  DbView,
  Page,
} from "./types";

export interface DatabaseCreateEffect {
  databaseId: string;
  kind: "database_create";
  originHref: string;
  page: Page;
  properties: DbProperty[];
  rows: Page[];
  templates: DbTemplate[];
  views: DbView[];
}

/**
 * Marks a durable composite block create whose later block-update entries must
 * adopt the storage-committed create authority before replay can advance.
 */
export interface BlockCreateBatchCommitEffect {
  kind: "block_create_batch_commit";
}

export interface DatabaseMetadataReconciliationEffect {
  databaseId: string;
  kind: "database_metadata_reconcile";
}

export interface DatabaseRelationConversionCommitEffect {
  kind: "database_relation_conversion_commit";
  sourceDatabaseId: string;
  targetDatabaseId: string;
}

export interface DatabaseRowsReconciliationEffect {
  databaseId: string;
  kind: "database_rows_reconcile";
  mutationId: string;
  rowId: string;
  targetParentId: string;
}

export interface DatabaseDependencyCommitEffect {
  addRelatedRowIds: string[];
  databaseId: string;
  direction: "predecessors" | "successors";
  kind: "database_dependency_commit";
  mutationId: string;
  propertyId: string;
  removeRelatedRowIds: string[];
  rowId: string;
}

export interface DatabaseTaskFeatureCommitEffect {
  avoidWeekends?: boolean;
  databaseId: string;
  dateMode?: "range" | "separate";
  datePropertyId?: string;
  endDatePropertyId?: string;
  feature: "dependencies" | "subitems";
  kind: "database_task_feature_commit";
  nestedPropertyId?: string;
  primaryPropertyId: string;
  secondaryPropertyId: string;
  showToggleOnTitle?: boolean;
  shiftMode?: "overlap" | "maintain_spacing" | "none";
  startDatePropertyId?: string;
}

export interface DatabaseTaskFeatureTurnOffEffect {
  databaseId: string;
  expectedBindingRevision: number;
  expectedDatabaseFeaturesRevision: number;
  feature: "dependencies" | "subitems";
  kind: "database_task_feature_turn_off";
  operationId: string;
  primaryPropertyId: string;
  propertyDisposition: "keep" | "remove";
  secondaryPropertyId: string;
}

export interface RowFileRemovalEffect {
  cacheKey?: string;
  databaseId: string;
  expectedMutationId?: string;
  expectedUpdatedAt?: string;
  kind: "row_file_remove";
  mutationId?: string;
  nextValue: unknown;
  previousValue: unknown;
  propertyId: string;
  removedIndex: number;
  removedItem: unknown;
  rowId: string;
}

export interface BlockStructureInvalidation {
  blockIds?: string[];
  dropReconciliation?: {
    direction: "undo" | "redo";
    histories: Array<{
      at: number;
      linkId?: string;
      operationOccurredAt: string;
      pageId: string;
    }>;
  };
  pageIds: string[];
}

export interface RemoteCallTerminalReconciliation {
  commentPageIds?: string[];
  databaseMetadataIds?: string[];
  databaseRowIds?: string[];
  pageIds?: string[];
}

export type RemoteCallEffect =
  | BlockCreateBatchCommitEffect
  | DatabaseCreateEffect
  | DatabaseDependencyCommitEffect
  | DatabaseMetadataReconciliationEffect
  | DatabaseRelationConversionCommitEffect
  | DatabaseRowsReconciliationEffect
  | DatabaseTaskFeatureCommitEffect
  | DatabaseTaskFeatureTurnOffEffect
  | RowFileRemovalEffect;

export interface PageRecencyOutboxOp {
  blockId: string;
  blockUpdatedAt: string;
  dueAt: number;
  kind: "page_recency";
  mutationId: string;
  pageId: string;
}

export type MutationOutboxOp =
  | {
      expectedUpdatedAt?: string;
      expectedMutationId?: string;
      expectedFieldValues?: Record<string, unknown>;
      hintPageId?: string;
      id: string;
      kind: "block_update";
      mutationId?: string;
      patch: Partial<Block>;
    }
  | { block: Block; kind: "block_create"; touchPage?: boolean }
  | { hintPageId?: string; ids: string[]; kind: "block_delete" }
  | {
      expectedUpdatedAt?: string;
      expectedMutationId?: string;
      expectedFieldValues?: Record<string, unknown>;
      expectedPropertyValues?: Record<string, unknown>;
      id: string;
      kind: "page_update";
      mutationId?: string;
      patch: Partial<Page> & { content?: string; cursor?: number };
      sameUserMoveRebaseCount?: number;
      target: "database_row" | "page";
    }
  | PageRecencyOutboxOp
  | {
      // A page's structure changes (blocks, databases) made here and kept
      // until the server takes them, sent again with the same mutation ID.
      kind: "content_graph_mutation";
      mutationId: string;
      operations: unknown[];
      pageId: string;
    }
  | {
      // A page's settings change (title, icon, cover, trash, …) made here and
      // kept until the server takes it, with the page as it was when made
      // (nativePageMetadata.ts).
      baseline: { cursor: number; inTrash: boolean; parentId: string | null; title: string };
      change: Record<string, unknown>;
      kind: "page_metadata";
      mutationId: string;
      pageId: string;
    }
  | {
      // A page made here, shown and opened at once, and kept until the
      // server makes it under the same ID (nativePageLaneEntries.ts).
      input: Record<string, unknown>;
      kind: "page_create";
      mutationId: string;
      page: Record<string, unknown>;
      pageId: string;
    }
  | {
      // A page's change as the page outbox keeps it: one of the kinds above,
      // in the format its schemaVersion names (nativeOutboxEntryFormat.ts).
      entry: Record<string, unknown>;
      kind: "native_entry";
    }
  | {
      args: unknown[];
      blockStructureInvalidation?: BlockStructureInvalidation;
      effect?: RemoteCallEffect;
      fn: string;
      kind: "remote_call";
      terminalReconciliation?: RemoteCallTerminalReconciliation;
    };

export interface TerminalReconciliationOutboxOp {
  disposition: "drop" | "silent_drop";
  kind: "terminal_reconciliation";
  operation: MutationOutboxOp;
  recovery?:
    | { kind: "copy_block_patch" }
    | { kind: "copy_block_patches"; nextIndex?: number }
    | { kind: "copy_page_patch" }
    | { comments: Comment[]; kind: "copy_comment_thread"; pageId: string };
  status?: number;
  successor?: Extract<MutationOutboxOp, { kind: "block_update" | "page_update" }>;
}

type RejectedRecoveryEnvelope = {
  kind: "rejected_mutation_recovery";
  status: 403;
};

export type RejectedBlockMutationRecoveryOutboxOp = RejectedRecoveryEnvelope & (
  | {
      operation: Extract<MutationOutboxOp, { kind: "block_update" }>;
      operations?: never;
    }
  | {
      operation?: never;
      operations: Array<Extract<MutationOutboxOp, { kind: "block_update" }>>;
    }
);

export type RejectedCommentMutationRecoveryOutboxOp = RejectedRecoveryEnvelope & {
  comments: Comment[];
  pageId: string;
};

export type RejectedPageMutationRecoveryOutboxOp = RejectedRecoveryEnvelope & {
  pagePatch: Extract<MutationOutboxOp, { kind: "page_update" }>;
};

export type RejectedMutationRecoveryOutboxOp =
  | RejectedBlockMutationRecoveryOutboxOp
  | RejectedCommentMutationRecoveryOutboxOp
  | RejectedPageMutationRecoveryOutboxOp;

/**
 * The format of what this app keeps in the outbox, which each entry names
 * (its schemaVersion): one higher every time an entry comes to keep what an
 * app before would read wrong — a field it would leave out when sending it.
 * An app leaves an entry of a format above its own for one that knows it
 * (nativeOutboxEntryFormat.ts); from 5 a page's change is kept in a
 * `native_entry`, which apps from before 5 can't read and leave; from 6 a
 * page's structure change may keep the page's becoming a full-page database
 * (`pageKind`), which an app before 6 would send without it; from 7 it may
 * keep a database the page starts as that the server makes (`start`), which
 * an app before 7 would send as its empty lines' removal alone; from 8 a
 * database start may keep that it takes the place of a page of the person's
 * (`replacesPage`), which an app before 8 would send without the page going
 * to the trash as it was; from 9 a structure change may keep the templates
 * the rows it makes start from (`rowTemplates`), which an app before 9 would
 * send without them, its rows made unfilled.
 */
export const OUTBOX_OPERATION_SCHEMA_VERSION = 9;
export const MAX_OUTBOX_CAUSALITY_KEYS = 8;
export const MAX_OUTBOX_CAUSALITY_KEY_LENGTH = 160;

export interface OutboxReplayState {
  attempt: number;
  nextAttemptAt?: number;
  quarantine?: {
    notifiedAt?: number;
    reason: "future_schema" | "unknown_remote_call";
  };
}

export interface OutboxOperationMetadata {
  causalityKeys?: string[];
  replay?: OutboxReplayState;
  schemaVersion?: number;
}

export type OutboxOp = (
  | MutationOutboxOp
  | RejectedMutationRecoveryOutboxOp
  | TerminalReconciliationOutboxOp
) & OutboxOperationMetadata;

/**
 * A durable outbox entry's exact shape. Persistence adapters implement this
 * contract.
 */
export interface OutboxEntry {
  entryKey: string;
  seq: number;
  tabId: string;
  updatedAt: number;
  value: OutboxOp;
}
