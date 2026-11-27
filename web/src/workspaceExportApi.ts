import type { Session } from "./types";
import type {
  WorkspaceExportCurrentJobResult,
  WorkspaceExportInput,
  WorkspaceExportJobResult,
  WorkspaceExportPolicyResponse,
} from "./workspaceExportWire.generated";

type JsonRequest = <T>(
  path: string,
  init?: RequestInit,
  session?: Session | null,
) => Promise<T>;

type ResponseRequest = (path: string, session: Session, init?: RequestInit) => Promise<Response>;

const workspaceExportPath = (workspaceId: string) =>
  `/workspaces/${encodeURIComponent(workspaceId)}/exports`;

const workspaceExportJobPath = (workspaceId: string, jobId: string) =>
  `${workspaceExportPath(workspaceId)}/${encodeURIComponent(jobId)}`;

export function createWorkspaceExportApi(
  request: JsonRequest,
  requestResponse: ResponseRequest,
) {
  return {
    startWorkspaceExport: (
      session: Session,
      workspaceId: string,
      input: WorkspaceExportInput,
    ) => request<WorkspaceExportJobResult>(
      workspaceExportPath(workspaceId),
      { method: "POST", body: JSON.stringify(input) },
      session,
    ),
    /** Whether the organization lets the person export a page or the workspace. */
    workspaceExportPolicy: (session: Session, workspaceId: string, kind: "workspace" | "page", targetId = "") =>
      request<WorkspaceExportPolicyResponse>(
        `${workspaceExportPath(workspaceId)}/policy?${new URLSearchParams({ kind, targetId })}`,
        {},
        session,
      ),
    currentWorkspaceExport: (session: Session, workspaceId: string) =>
      request<WorkspaceExportCurrentJobResult>(
        `${workspaceExportPath(workspaceId)}/current`,
        {},
        session,
      ),
    workspaceExport: (session: Session, workspaceId: string, jobId: string) =>
      request<WorkspaceExportJobResult>(
        workspaceExportJobPath(workspaceId, jobId),
        {},
        session,
      ),
    retryWorkspaceExport: (session: Session, workspaceId: string, jobId: string) =>
      request<WorkspaceExportJobResult>(
        `${workspaceExportJobPath(workspaceId, jobId)}/retry`,
        { method: "POST" },
        session,
      ),
    cancelWorkspaceExport: (session: Session, workspaceId: string, jobId: string) =>
      request<WorkspaceExportJobResult>(
        `${workspaceExportJobPath(workspaceId, jobId)}/cancel`,
        { method: "POST" },
        session,
      ),
    downloadWorkspaceExport: (session: Session, workspaceId: string, jobId: string) =>
      requestResponse(`${workspaceExportJobPath(workspaceId, jobId)}/download`, session),
    downloadWorkspaceExportRange: (
      session: Session,
      workspaceId: string,
      jobId: string,
      options: { rangeStart?: number; signal?: AbortSignal } = {},
    ) => requestResponse(
      `${workspaceExportJobPath(workspaceId, jobId)}/download`,
      session,
      {
        headers: options.rangeStart === undefined
          ? undefined
          : { range: `bytes=${options.rangeStart}-` },
        signal: options.signal,
      },
    ),
    /** The workspace's members and guests as a CSV file; its owner's only. */
    downloadWorkspacePeopleCsv: (session: Session, workspaceId: string) =>
      requestResponse(`/workspaces/${encodeURIComponent(workspaceId)}/people.csv`, session),
  };
}
