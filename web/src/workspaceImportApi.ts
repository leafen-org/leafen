import type { Session } from "./types";
import {
  workspaceImportOperations,
  type WorkspaceArchiveImportUpload,
  type WorkspaceArchiveImportUploadCompleteInput,
  type WorkspaceArchiveImportUploadPrepareInput,
  type WorkspaceCSVMergeInput,
  type WorkspaceCSVMergeTarget,
  type WorkspaceCSVMergeTargetList,
  type WorkspaceMarkdownAppendInput,
  type WorkspaceMarkdownReplaceInput,
  type WorkspaceImportBatchDocumentsInput,
  type WorkspaceImportBatchInput,
  type WorkspaceImportBatchItemList,
  type WorkspaceImportBatchItemsInput,
  type WorkspaceImportInput,
  type WorkspaceImportJob,
  type WorkspaceImportJobList,
} from "./workspaceImportWire.generated";

type JsonRequest = <T>(
  path: string,
  init?: RequestInit,
  session?: Session | null,
) => Promise<T>;

function workspaceImportPath(workspaceId: string) {
  return workspaceImportOperations.create.path.replace(
    "{workspaceId}",
    encodeURIComponent(workspaceId),
  );
}

function workspaceArchiveImportPath(workspaceId: string) {
  return workspaceImportOperations.archiveCreate.path.replace(
    "{workspaceId}",
    encodeURIComponent(workspaceId),
  );
}

function workspaceArchiveUploadPath(
  operation: keyof Pick<typeof workspaceImportOperations,
    | "archiveUploadPrepare"
    | "archiveUploadPart"
    | "archiveUploadComplete"
    | "archiveUploadCancel"
  >,
  workspaceId: string,
  uploadId = "",
  partNumber = 0,
) {
  return workspaceImportOperations[operation].path
    .replace("{workspaceId}", encodeURIComponent(workspaceId))
    .replace("{uploadId}", encodeURIComponent(uploadId))
    .replace("{partNumber}", String(partNumber));
}

function workspaceMarkdownAppendPath(workspaceId: string, pageId: string) {
  return workspaceImportOperations.markdownAppend.path
    .replace("{workspaceId}", encodeURIComponent(workspaceId))
    .replace("{pageId}", encodeURIComponent(pageId));
}

function workspaceMarkdownReplacePath(workspaceId: string, pageId: string) {
  return workspaceImportOperations.markdownReplace.path
    .replace("{workspaceId}", encodeURIComponent(workspaceId))
    .replace("{pageId}", encodeURIComponent(pageId));
}

function workspaceCSVMergePath(workspaceId: string, dataSourceId: string) {
  return workspaceImportOperations.csvMerge.path
    .replace("{workspaceId}", encodeURIComponent(workspaceId))
    .replace("{dataSourceId}", encodeURIComponent(dataSourceId));
}

function workspaceImportJobPath(workspaceId: string, jobId: string) {
  return workspaceImportOperations.read.path
    .replace("{workspaceId}", encodeURIComponent(workspaceId))
    .replace("{jobId}", encodeURIComponent(jobId));
}

function workspaceImportBatchPath(
  operation: keyof Pick<typeof workspaceImportOperations, "batchItemsDeclare" | "batchItemsList" | "batchDocuments" | "batchStart">,
  workspaceId: string,
  jobId: string,
) {
  return workspaceImportOperations[operation].path
    .replace("{workspaceId}", encodeURIComponent(workspaceId))
    .replace("{jobId}", encodeURIComponent(jobId));
}

export function createWorkspaceImportApi(request: JsonRequest) {
  return {
    /** Opens a batch of files, or finds the one the same files opened. */
    createWorkspaceImportBatch: (
      session: Session,
      workspaceId: string,
      input: WorkspaceImportBatchInput,
      signal?: AbortSignal,
    ) => request<WorkspaceImportJob>(
      workspaceImportOperations.batchCreate.path.replace("{workspaceId}", encodeURIComponent(workspaceId)),
      { method: workspaceImportOperations.batchCreate.method, body: JSON.stringify(input), signal },
      session,
    ),
    /** Says what the batch brings; answers with each item's page ID and how far it got. */
    declareWorkspaceImportBatchItems: (
      session: Session,
      workspaceId: string,
      jobId: string,
      input: WorkspaceImportBatchItemsInput,
      signal?: AbortSignal,
    ) => request<WorkspaceImportBatchItemList>(
      workspaceImportBatchPath("batchItemsDeclare", workspaceId, jobId),
      { method: workspaceImportOperations.batchItemsDeclare.method, body: JSON.stringify(input), signal },
      session,
    ),
    workspaceImportBatchItems: (
      session: Session,
      workspaceId: string,
      jobId: string,
      options: { after?: number; signal?: AbortSignal } = {},
    ) => request<WorkspaceImportBatchItemList>(
      `${workspaceImportBatchPath("batchItemsList", workspaceId, jobId)}?after=${options.after ?? -1}`,
      { method: workspaceImportOperations.batchItemsList.method, signal: options.signal },
      session,
    ),
    sendWorkspaceImportBatchDocuments: (
      session: Session,
      workspaceId: string,
      jobId: string,
      input: WorkspaceImportBatchDocumentsInput,
      signal?: AbortSignal,
    ) => request<WorkspaceImportJob>(
      workspaceImportBatchPath("batchDocuments", workspaceId, jobId),
      { method: workspaceImportOperations.batchDocuments.method, body: JSON.stringify(input), signal },
      session,
    ),
    /** Hands the batch to the server, which goes on with no one there. */
    startWorkspaceImportBatch: (
      session: Session,
      workspaceId: string,
      jobId: string,
      signal?: AbortSignal,
    ) => request<WorkspaceImportJob>(
      workspaceImportBatchPath("batchStart", workspaceId, jobId),
      { method: workspaceImportOperations.batchStart.method, signal },
      session,
    ),
    createWorkspaceImport: (
      session: Session,
      workspaceId: string,
      input: WorkspaceImportInput,
    ) => request<WorkspaceImportJob>(
      workspaceImportPath(workspaceId),
      { method: workspaceImportOperations.create.method, body: JSON.stringify(input) },
      session,
    ),
    appendMarkdownToPage: (
      session: Session,
      workspaceId: string,
      pageId: string,
      input: WorkspaceMarkdownAppendInput,
      signal?: AbortSignal,
    ) => request<WorkspaceImportJob>(
      workspaceMarkdownAppendPath(workspaceId, pageId),
      { method: workspaceImportOperations.markdownAppend.method, body: JSON.stringify(input), signal },
      session,
    ),
    /** The databases a CSV's rows can join, by title or on one page. */
    csvMergeTargets: (
      session: Session,
      workspaceId: string,
      options: { query?: string; pageId?: string; cursor?: string; signal?: AbortSignal } = {},
    ) => {
      const query = new URLSearchParams();
      if (options.query) query.set("q", options.query);
      if (options.pageId) query.set("pageId", options.pageId);
      if (options.cursor) query.set("cursor", options.cursor);
      const path = workspaceImportOperations.csvMergeTargets.path.replace("{workspaceId}", encodeURIComponent(workspaceId));
      return request<WorkspaceCSVMergeTargetList>(
        `${path}${query.size ? `?${query}` : ""}`,
        { method: workspaceImportOperations.csvMergeTargets.method, signal: options.signal },
        session,
      );
    },
    /** A database's properties a CSV's columns can fill, in their order. */
    csvMergeTarget: (
      session: Session,
      workspaceId: string,
      dataSourceId: string,
      signal?: AbortSignal,
    ) => request<WorkspaceCSVMergeTarget>(
      workspaceCSVMergePath(workspaceId, dataSourceId),
      { method: workspaceImportOperations.csvMergeTarget.method, signal },
      session,
    ),
    /** Adds a CSV's rows to a database, each column to the property it is matched to. */
    mergeCSVIntoDatabase: (
      session: Session,
      workspaceId: string,
      dataSourceId: string,
      input: WorkspaceCSVMergeInput,
    ) => request<WorkspaceImportJob>(
      workspaceCSVMergePath(workspaceId, dataSourceId),
      { method: workspaceImportOperations.csvMerge.method, body: JSON.stringify(input) },
      session,
    ),
    replaceMarkdownPage: (
      session: Session,
      workspaceId: string,
      pageId: string,
      input: WorkspaceMarkdownReplaceInput,
    ) => request<WorkspaceImportJob>(
      workspaceMarkdownReplacePath(workspaceId, pageId),
      { method: workspaceImportOperations.markdownReplace.method, body: JSON.stringify(input) },
      session,
    ),
    createWorkspaceArchiveImport: (
      session: Session,
      workspaceId: string,
      requestId: string,
      file: File,
    ) => request<WorkspaceImportJob>(
      workspaceArchiveImportPath(workspaceId),
      {
        method: workspaceImportOperations.archiveCreate.method,
        body: file,
        headers: {
          "content-type": "application/vnd.leafen.archive+zip",
          "x-leafen-request-id": requestId,
        },
      },
      session,
    ),
    archiveUploadPrepare: (
      session: Session,
      workspaceId: string,
      input: WorkspaceArchiveImportUploadPrepareInput,
    ) => request<WorkspaceArchiveImportUpload>(
      workspaceArchiveUploadPath("archiveUploadPrepare", workspaceId),
      {
        method: workspaceImportOperations.archiveUploadPrepare.method,
        body: JSON.stringify(input),
      },
      session,
    ),
    archiveUploadPart: (
      session: Session,
      workspaceId: string,
      uploadId: string,
      partNumber: number,
      bytes: Uint8Array<ArrayBuffer>,
      sha256: string,
    ) => request<WorkspaceArchiveImportUpload>(
      workspaceArchiveUploadPath("archiveUploadPart", workspaceId, uploadId, partNumber),
      {
        method: workspaceImportOperations.archiveUploadPart.method,
        body: bytes,
        headers: {
          "content-type": "application/octet-stream",
          "x-leafen-part-sha256": sha256,
        },
      },
      session,
    ),
    archiveUploadComplete: (
      session: Session,
      workspaceId: string,
      uploadId: string,
      input: WorkspaceArchiveImportUploadCompleteInput,
    ) => request<WorkspaceImportJob>(
      workspaceArchiveUploadPath("archiveUploadComplete", workspaceId, uploadId),
      {
        method: workspaceImportOperations.archiveUploadComplete.method,
        body: JSON.stringify(input),
      },
      session,
    ),
    archiveUploadCancel: (
      session: Session,
      workspaceId: string,
      uploadId: string,
      input: WorkspaceArchiveImportUploadCompleteInput,
    ) => request<WorkspaceArchiveImportUpload>(
      workspaceArchiveUploadPath("archiveUploadCancel", workspaceId, uploadId),
      {
        method: workspaceImportOperations.archiveUploadCancel.method,
        body: JSON.stringify(input),
      },
      session,
    ),
    /** The person's imports into the workspace, newest first. */
    workspaceImports: (
      session: Session,
      workspaceId: string,
      options: { signal?: AbortSignal } = {},
    ) => request<WorkspaceImportJobList>(
      workspaceImportOperations.list.path.replace("{workspaceId}", encodeURIComponent(workspaceId)),
      { method: workspaceImportOperations.list.method, signal: options.signal },
      session,
    ),
    workspaceImportJob: (
      session: Session,
      workspaceId: string,
      jobId: string,
      options: { signal?: AbortSignal } = {},
    ) => request<WorkspaceImportJob>(
      workspaceImportJobPath(workspaceId, jobId),
      { method: workspaceImportOperations.read.method, signal: options.signal },
      session,
    ),
    cancelWorkspaceImport: (
      session: Session,
      workspaceId: string,
      jobId: string,
    ) => request<WorkspaceImportJob>(
      workspaceImportOperations.cancel.path
        .replace("{workspaceId}", encodeURIComponent(workspaceId))
        .replace("{jobId}", encodeURIComponent(jobId)),
      { method: workspaceImportOperations.cancel.method },
      session,
    ),
  };
}
