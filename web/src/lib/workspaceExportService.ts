import { api } from "@/api";
import type { Session } from "@/types";
import type {
  WorkspaceExportCurrentJobResult,
  WorkspaceExportFormat,
  WorkspaceExportJob,
  WorkspaceExportJobResult,
  WorkspaceExportScope,
} from "@/workspaceExportWire.generated";
import { newId } from "@/lib/ids";
import {
  clearRememberedWorkspaceExportStartIntent,
  readRememberedWorkspaceExportStartIntent,
  rememberWorkspaceExportJob,
  rememberWorkspaceExportStartIntent,
  type RememberedWorkspaceExportStartIntent,
} from "./workspaceExportJobMemory";
import {
  waitForWorkspaceTransferJob,
  workspaceTransferJobFromResponse,
} from "./workspaceTransferPolling";

export type WorkspaceExportTransport = Pick<typeof api,
  | "startWorkspaceExport"
  | "currentWorkspaceExport"
  | "workspaceExport"
  | "retryWorkspaceExport"
  | "cancelWorkspaceExport"
  | "downloadWorkspaceExport"
  | "downloadWorkspaceExportRange"
>;

export type StartWorkspaceExportInput = {
  workspaceId: string;
  format: WorkspaceExportFormat;
  scope: WorkspaceExportScope;
  options: Record<string, boolean>;
};

function sameScope(left: WorkspaceExportScope, right: WorkspaceExportScope) {
  return left.kind === right.kind && left.targetId === right.targetId;
}

const workspaceScope = (): WorkspaceExportScope => ({ kind: "workspace", targetId: "" });

function normalizedScope(scope?: WorkspaceExportScope) {
  return scope ?? workspaceScope();
}

function sameOptions(
  left: Record<string, boolean>,
  right: Record<string, boolean>,
) {
  const leftKeys = Object.keys(left).sort();
  const rightKeys = Object.keys(right).sort();
  return (
    leftKeys.length === rightKeys.length
    && leftKeys.every((key, index) => (
      key === rightKeys[index] && left[key] === right[key]
    ))
  );
}

function sameStartIntent(
  remembered: RememberedWorkspaceExportStartIntent,
  input: StartWorkspaceExportInput,
) {
  return (
    remembered.format === input.format
    && sameScope(remembered.scope, input.scope)
    && sameOptions(remembered.options, input.options)
  );
}

function workspaceExportStartIntent(
  actorId: string,
  input: StartWorkspaceExportInput,
) {
  const scope = normalizedScope(input.scope);
  const remembered = readRememberedWorkspaceExportStartIntent(
    actorId,
    input.workspaceId,
  );
  if (remembered && sameStartIntent(remembered, { ...input, scope })) return remembered;
  const intent = {
    requestId: newId(),
    format: input.format,
    scope: { ...scope },
    options: { ...input.options },
  };
  rememberWorkspaceExportStartIntent(actorId, input.workspaceId, intent);
  return intent;
}

function exactJobResult(
  response: WorkspaceExportJobResult,
  workspaceId: string,
  label: "start" | "status",
  expectedJobId?: string,
) {
  const job = workspaceTransferJobFromResponse(
    response,
    workspaceId,
    label,
    expectedJobId,
    "Workspace export",
  ) as WorkspaceExportJob;
  return { ...response, job };
}

export function createWorkspaceExportService(transport: WorkspaceExportTransport = api) {
  const rememberStartedJob = (
    actorId: string,
    workspaceId: string,
    result: ReturnType<typeof exactJobResult>,
  ) => {
    const scope = normalizedScope(result.job.scope);
    rememberWorkspaceExportJob(actorId, workspaceId, {
      jobId: result.job.id,
      format: result.job.format,
      scope: { ...scope },
      options: result.job.options,
    });
    clearRememberedWorkspaceExportStartIntent(
      actorId,
      workspaceId,
      result.job.requestId,
    );
    return result.job.scope ? result : { ...result, job: { ...result.job, scope } };
  };

  const reconcileCurrentJob = (
    actorId: string,
    workspaceId: string,
    result: ReturnType<typeof exactJobResult>,
  ) => {
    const intent = readRememberedWorkspaceExportStartIntent(actorId, workspaceId);
    return intent?.requestId === result.job.requestId
      ? rememberStartedJob(actorId, workspaceId, result)
      : result;
  };

  const exact = async (session: Session, workspaceId: string, jobId: string) =>
    exactJobResult(
      await transport.workspaceExport(session, workspaceId, jobId),
      workspaceId,
      "status",
      jobId,
    );

  return {
    async start(session: Session, input: StartWorkspaceExportInput) {
      const intent = workspaceExportStartIntent(session.user.id, input);
      return rememberStartedJob(session.user.id, input.workspaceId, exactJobResult(
        await transport.startWorkspaceExport(session, input.workspaceId, {
          requestId: intent.requestId,
          format: input.format,
          scope: input.scope,
          options: input.options,
        }),
        input.workspaceId,
        "start",
      ));
    },

    async current(session: Session, workspaceId: string): Promise<WorkspaceExportCurrentJobResult> {
      const result = await transport.currentWorkspaceExport(session, workspaceId);
      if (!result.job) return result;
      const exactResult = {
        replayed: false,
        cancelled: false,
        job: workspaceTransferJobFromResponse(
          result,
          workspaceId,
          "status",
          undefined,
          "Workspace export",
        ) as WorkspaceExportJob,
      };
      return { job: reconcileCurrentJob(session.user.id, workspaceId, exactResult).job, finished: result.finished ?? [] };
    },

    exact,

    wait(
      session: Session,
      workspaceId: string,
      initial: WorkspaceExportJob,
      options: {
        onProgress?: (job: WorkspaceExportJob) => void;
        onReach?: (reached: boolean) => void;
        signal?: AbortSignal;
      } = {},
    ) {
      return waitForWorkspaceTransferJob(
        initial,
        () => exact(session, workspaceId, initial.id),
        workspaceId,
        {
          operation: "Workspace export",
          onProgress: options.onProgress
            ? (job) => options.onProgress?.(job as WorkspaceExportJob)
            : undefined,
          onReach: options.onReach,
          signal: options.signal,
        },
      ) as Promise<WorkspaceExportJob>;
    },

    async retry(session: Session, workspaceId: string, jobId: string) {
      return exactJobResult(
        await transport.retryWorkspaceExport(session, workspaceId, jobId),
        workspaceId,
        "start",
        jobId,
      );
    },

    async cancel(session: Session, workspaceId: string, jobId: string) {
      return exactJobResult(
        await transport.cancelWorkspaceExport(session, workspaceId, jobId),
        workspaceId,
        "status",
        jobId,
      );
    },

    download(
      session: Session,
      workspaceId: string,
      jobId: string,
      options: { rangeStart?: number; signal?: AbortSignal } = {},
    ) {
      if (options.rangeStart !== undefined || options.signal !== undefined) {
        return transport.downloadWorkspaceExportRange(session, workspaceId, jobId, options);
      }
      return transport.downloadWorkspaceExport(session, workspaceId, jobId);
    },
  };
}

export const workspaceExportService = createWorkspaceExportService();
