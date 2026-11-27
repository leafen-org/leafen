import { newId } from "@/lib/ids";
import { request } from "@/apiTransport";
import {
  fileUsageReportOperations,
  type FileUsageReportType,
  type FileUsageReportResponse,
} from "@/fileUsageReportWire.generated";
import type { Session } from "@/types";

const REPORT_MAINTENANCE_HISTORY = 5;
type ReportFlight = {
  promise: Promise<FileUsageReportResponse>;
  signal: AbortSignal;
};

const inFlight = new Map<string, ReportFlight>();

type ReportTarget = {
  id: string;
  method: "POST";
  path: string;
  reportType: FileUsageReportType;
};

function workspaceTarget(workspaceId: string): ReportTarget {
  return {
    id: workspaceId,
    method: fileUsageReportOperations.workspaceFileUsageReport.method,
    path: fileUsageReportOperations.workspaceFileUsageReport.path.replace(
      "{workspaceId}",
      encodeURIComponent(workspaceId),
    ),
    reportType: "workspace",
  };
}

function organizationTarget(organizationId: string): ReportTarget {
  return {
    id: organizationId,
    method: fileUsageReportOperations.organizationFileUsageReport.method,
    path: fileUsageReportOperations.organizationFileUsageReport.path.replace(
      "{organizationId}",
      encodeURIComponent(organizationId),
    ),
    reportType: "organization",
  };
}

function invalidReport(message: string): never {
  throw new Error(`Invalid file usage report: ${message}`);
}

function assertReport(
  value: FileUsageReportResponse,
  target: ReportTarget,
  requestId: string,
  jobId: string,
  priorRevision: number,
) {
  if (!value || typeof value !== "object") invalidReport("response");
  const job = value.fileUsageReportJob;
  if (
    !job
    || job.reportType !== target.reportType
    || (target.reportType === "workspace" && job.workspaceId !== target.id)
    || (target.reportType === "organization" && job.organizationId !== target.id)
    || job.requestId !== requestId
    || !job.id
    || (jobId && job.id !== jobId)
    || !Number.isInteger(job.revision)
    || job.revision <= priorRevision
  ) invalidReport("identity or revision");
  if (
    !value.totals
    || !value.pending
    || !Array.isArray(value.byScope)
    || !Array.isArray(value.maintenanceRuns)
    || !Array.isArray(value.byWorkspace)
    || typeof value.byWorkspaceHasMore !== "boolean"
  ) invalidReport("projection");
  if (value.partial) {
    if (job.status !== "running") invalidReport("partial status");
  } else if (job.status !== "completed" || job.phase !== "completed") {
    invalidReport("terminal status");
  }
  if (target.reportType === "organization") {
    if (value.byWorkspaceHasMore && !value.byWorkspaceNextCursor) {
      invalidReport("workspace result cursor");
    }
    if (!value.byWorkspaceHasMore && value.byWorkspaceNextCursor) {
      invalidReport("terminal workspace result cursor");
    }
  }
  return value;
}

async function drainReport(
  session: Session,
  target: ReportTarget,
  signal: AbortSignal,
) {
  const requestId = newId();
  let fileUsageReportJobId = "";
  let revision = -1;
  for (;;) {
    if (signal.aborted) throw signal.reason ?? new DOMException("Aborted", "AbortError");
    const value = await request<FileUsageReportResponse>(
      target.path,
      {
        method: target.method,
        body: JSON.stringify({
          requestId,
          fileUsageReportJobId,
          byWorkspaceAfter: "",
          maintenanceLimit: REPORT_MAINTENANCE_HISTORY,
        }),
        signal,
      },
      session,
    );
    if (signal.aborted) throw signal.reason ?? new DOMException("Aborted", "AbortError");
    const current = assertReport(
      value,
      target,
      requestId,
      fileUsageReportJobId,
      revision,
    );
    fileUsageReportJobId = current.fileUsageReportJob.id;
    revision = current.fileUsageReportJob.revision;
    if (!current.partial) return current;
  }
}

function loadFileUsageReport(
  session: Session,
  target: ReportTarget,
  signal: AbortSignal,
): Promise<FileUsageReportResponse> {
  const key = `${session.user.id}\u0000${session.token}\u0000${target.reportType}\u0000${target.id}`;
  const existing = inFlight.get(key);
  if (existing && !existing.signal.aborted) return existing.promise;
  if (existing) inFlight.delete(key);
  const task = drainReport(session, target, signal).finally(() => {
    if (inFlight.get(key)?.promise === task) inFlight.delete(key);
  });
  inFlight.set(key, { promise: task, signal });
  return task;
}

export function loadWorkspaceFileUsageReport(
  session: Session,
  workspaceId: string,
  signal: AbortSignal,
): Promise<FileUsageReportResponse> {
  return loadFileUsageReport(session, workspaceTarget(workspaceId), signal);
}

export function loadOrganizationFileUsageReport(
  session: Session,
  organizationId: string,
  signal: AbortSignal,
): Promise<FileUsageReportResponse> {
  return loadFileUsageReport(session, organizationTarget(organizationId), signal);
}
