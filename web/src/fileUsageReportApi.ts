import {
  fileUsageReportOperations,
  type FileManualCleanupInput,
  type FileManualCleanupResponse,
} from "./fileUsageReportWire.generated";
import type { Session } from "./types";

type JsonRequest = <T>(
  path: string,
  init?: RequestInit,
  session?: Session | null,
) => Promise<T>;

function workspacePath(template: string, workspaceId: string) {
  return template.replace("{workspaceId}", encodeURIComponent(workspaceId));
}

export function createFileUsageReportApi(request: JsonRequest) {
  return {
    startWorkspaceFileManualCleanup(
      session: Session,
      workspaceId: string,
      input: FileManualCleanupInput,
      signal?: AbortSignal,
    ) {
      return request<FileManualCleanupResponse>(
        workspacePath(
          fileUsageReportOperations.workspaceFileManualCleanupStart.path,
          workspaceId,
        ),
        {
          method: fileUsageReportOperations.workspaceFileManualCleanupStart.method,
          body: JSON.stringify(input),
          signal,
        },
        session,
      );
    },

    readWorkspaceFileManualCleanup(
      session: Session,
      workspaceId: string,
      jobId: string,
      signal?: AbortSignal,
    ) {
      return request<FileManualCleanupResponse>(
        workspacePath(
          fileUsageReportOperations.workspaceFileManualCleanupRead.path,
          workspaceId,
        ).replace("{jobId}", encodeURIComponent(jobId)),
        {
          method: fileUsageReportOperations.workspaceFileManualCleanupRead.method,
          signal,
        },
        session,
      );
    },
  };
}
