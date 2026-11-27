import { formatFileSize } from "./database/files";
import {
  type WorkspaceExportFormat,
  type WorkspaceExportJob,
} from "@/workspaceExportWire.generated";
import {
  productErrorMessage,
  productErrorReceiptPresentation,
  type ProductErrorPresentation,
} from "@/lib/productErrorPresentation";

export type WorkspaceExportFailureStage =
  | "cancel"
  | "download"
  | "poll"
  | "retry"
  | "start"
  | "status";

export function workspaceExportJobIsActive(job: WorkspaceExportJob | null) {
  return (
    job?.status === "pending"
    || job?.status === "running"
    || job?.status === "preparing"
  );
}

export function workspaceExportJobCanRetry(job: WorkspaceExportJob | null) {
  return job?.status === "failed" && job.canRetry;
}

export function workspaceExportJobFailure(
  job: WorkspaceExportJob | null,
): ProductErrorPresentation | null {
  if (job?.status !== "failed") return null;
  return productErrorReceiptPresentation({
    code: job.errorCode,
    reference: job.diagnosticReference,
  }, "workspaceExport.job", { sameJobRetry: workspaceExportJobCanRetry(job) });
}

export function workspaceExportOptions(
  format: WorkspaceExportFormat,
  choice: { includeFiles: boolean; createFolders: boolean; defaultDatabaseViews: boolean; includeComments: boolean; includePrivatePages: boolean },
): Record<string, boolean> {
  if (format === "native_archive") return { includeFiles: true };
  const { includeFiles, createFolders, defaultDatabaseViews } = choice;
  // Private pages stay out unless asked for, as Notion's; the archive, which
  // moves and restores a workspace, takes them always.
  const leavePrivatePagesOut = !choice.includePrivatePages;
  return format === "html"
    ? { includeFiles, createFolders, defaultDatabaseViews, leavePrivatePagesOut, includeComments: choice.includeComments }
    : { includeFiles, createFolders, defaultDatabaseViews, leavePrivatePagesOut };
}

/** The workspace export formats that write readable pages. */
export function workspaceExportIsReadable(format: WorkspaceExportFormat) {
  return format === "markdown_csv" || format === "html";
}

export function formatWorkspaceExportBytes(bytes: number | undefined) {
  if (!Number.isFinite(bytes) || !bytes) return "";
  // One way of writing a size everywhere, Notion's (files.ts).
  return formatFileSize(bytes);
}

export function workspaceExportFailureMessage(
  error: unknown,
  stage: WorkspaceExportFailureStage,
) {
  return productErrorMessage(error, `workspaceExport.${stage}`);
}
