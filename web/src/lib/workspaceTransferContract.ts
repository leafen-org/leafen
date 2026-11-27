import type { ImportNativeResult } from "./leafenExportContract";

export type WorkspaceTransferJobStatus =
  | "preparing"
  | "pending"
  | "running"
  | "completed"
  | "failed"
  | "cancelled"
  | "expired";

export interface WorkspaceTransferJob {
  id: string;
  workspaceId: string;
  actorId?: string;
  batchId?: string;
  filename?: string;
  archiveBytes?: number;
  parentId?: string | null;
  parentType?: "workspace" | "page";
  format?: "native_archive" | "markdown_csv" | "page_markdown" | "notion_markdown" | "database_csv" | "page_native_archive"
    | "html" | "print_html";
  options?: Record<string, boolean>;
  sourceRevision?: string;
  status: WorkspaceTransferJobStatus;
  phase: string;
  partCount?: number;
  itemCount?: number;
  artifactBytes?: number;
  progress?: Record<string, unknown>;
  sourceKind?: "archive" | "graph";
  result?: ImportNativeResult;
  errorCode?: string | null;
  errorMessage?: string | null;
  attempts?: number;
  nextAttemptAt?: string | null;
  expiresAt?: string;
  startedAt?: string | null;
  completedAt?: string | null;
  cancelledAt?: string | null;
  updatedAt?: string;
}

const activeWorkspaceTransferStatuses = new Set<WorkspaceTransferJobStatus>([
  "preparing",
  "pending",
  "running",
]);

const recoveryWorkspaceTransferStatuses = new Set<WorkspaceTransferJobStatus>([
  "failed",
  "cancelled",
  "expired",
]);

export function workspaceTransferJobIsActive(
  job: Pick<WorkspaceTransferJob, "status"> | null,
) {
  return job ? activeWorkspaceTransferStatuses.has(job.status) : false;
}

export function workspaceTransferJobNeedsRecovery(
  job: Pick<WorkspaceTransferJob, "status"> | null,
) {
  return job ? recoveryWorkspaceTransferStatuses.has(job.status) : false;
}
