import { durableJobUnreachable } from "./durableJobPolling";
import {
  workspaceTransferJobIsActive,
  workspaceTransferJobNeedsRecovery,
  type WorkspaceTransferJob,
} from "./workspaceTransferContract";

const WORKSPACE_TRANSFER_POLL_MS = 1_000;
const WORKSPACE_TRANSFER_POLL_LIMIT = 24 * 60 * 60;

export type WorkspaceTransferPollingError = Error & {
  code:
    | "LEAFEN_WORKSPACE_TRANSFER_RESPONSE_INVALID"
    | "LEAFEN_WORKSPACE_TRANSFER_TERMINAL"
    | "LEAFEN_WORKSPACE_TRANSFER_TIMEOUT";
  stage: "start" | "status" | "terminal" | "poll";
  causeCode?: string;
};

function pollingError(
  code: WorkspaceTransferPollingError["code"],
  stage: WorkspaceTransferPollingError["stage"],
  message: string,
  causeCode?: string,
): WorkspaceTransferPollingError {
  const error = Object.assign(new Error(message), {
    code,
    stage,
    ...(causeCode ? { causeCode } : {}),
  });
  error.name = "WorkspaceTransferPollingError";
  return error;
}

function validWorkspaceTransferStatus(job: Pick<WorkspaceTransferJob, "status">) {
  return (
    job.status === "completed"
    || workspaceTransferJobIsActive(job)
    || workspaceTransferJobNeedsRecovery(job)
  );
}

export function workspaceTransferJobFromResponse(
  response: unknown,
  workspaceId: string,
  label: "start" | "status",
  expectedJobId?: string,
  operation = "Workspace transfer",
) {
  const candidate = response && typeof response === "object"
    ? (response as { job?: unknown }).job
    : undefined;
  const job = candidate as Partial<WorkspaceTransferJob> | undefined;
  if (
    !job
    || typeof job !== "object"
    || typeof job.id !== "string"
    || !job.id
    || job.workspaceId !== workspaceId
    || (expectedJobId !== undefined && job.id !== expectedJobId)
    || typeof job.status !== "string"
    || !validWorkspaceTransferStatus(job as Pick<WorkspaceTransferJob, "status">)
    || typeof job.phase !== "string"
  ) {
    throw pollingError(
      "LEAFEN_WORKSPACE_TRANSFER_RESPONSE_INVALID",
      label,
      `${operation} ${label} returned an invalid job.`,
    );
  }
  return job as WorkspaceTransferJob;
}

function workspaceTransferPollWait(signal?: AbortSignal) {
  if (signal?.aborted) return Promise.reject(new DOMException("Aborted", "AbortError"));
  return new Promise<void>((resolve, reject) => {
    const timeout = window.setTimeout(() => {
      signal?.removeEventListener("abort", onAbort);
      resolve();
    }, WORKSPACE_TRANSFER_POLL_MS);
    const onAbort = () => {
      window.clearTimeout(timeout);
      reject(new DOMException("Aborted", "AbortError"));
    };
    signal?.addEventListener("abort", onAbort, { once: true });
  });
}

export async function waitForWorkspaceTransferJob(
  initial: WorkspaceTransferJob,
  read: () => Promise<unknown>,
  workspaceId: string,
  options: {
    operation?: string;
    onProgress?: (job: WorkspaceTransferJob) => void;
    /** Told whether the server answers, as it stops answering and again. */
    onReach?: (reached: boolean) => void;
    signal?: AbortSignal;
  } = {},
) {
  const operation = options.operation ?? "Workspace transfer";
  let job = initial;
  let unreachable = false;
  options.onProgress?.(job);
  for (let attempt = 0; attempt <= WORKSPACE_TRANSFER_POLL_LIMIT; attempt += 1) {
    if (options.signal?.aborted) throw new DOMException("Aborted", "AbortError");
    if (job.status === "completed") return job;
    if (workspaceTransferJobNeedsRecovery(job)) {
      throw pollingError(
        "LEAFEN_WORKSPACE_TRANSFER_TERMINAL",
        "terminal",
        job.errorMessage
          || job.errorCode
          || `${operation} ended with status ${job.status}.`,
        job.errorCode ?? undefined,
      );
    }
    if (!workspaceTransferJobIsActive(job)) {
      throw pollingError(
        "LEAFEN_WORKSPACE_TRANSFER_RESPONSE_INVALID",
        "status",
        `${operation} status returned an invalid job.`,
      );
    }
    if (attempt >= WORKSPACE_TRANSFER_POLL_LIMIT) {
      throw pollingError(
        "LEAFEN_WORKSPACE_TRANSFER_TIMEOUT",
        "poll",
        `${operation} status timed out.`,
      );
    }
    await workspaceTransferPollWait(options.signal);
    // The job outlives its server (as waitForDurableJob): while the server
    // can't be reached or starts again, it is asked after again, and goes
    // on where it was (2026-10-16: an export left a failure on the screen
    // that the server finished once it was back).
    let response: unknown;
    try {
      response = await read();
    } catch (error) {
      if (!durableJobUnreachable(error) || options.signal?.aborted) throw error;
      if (!unreachable) options.onReach?.(false);
      unreachable = true;
      continue;
    }
    if (unreachable) options.onReach?.(true);
    unreachable = false;
    job = workspaceTransferJobFromResponse(
      response,
      workspaceId,
      "status",
      job.id,
      operation,
    );
    options.onProgress?.(job);
  }
  throw pollingError(
    "LEAFEN_WORKSPACE_TRANSFER_TIMEOUT",
    "poll",
    `${operation} status timed out.`,
  );
}
