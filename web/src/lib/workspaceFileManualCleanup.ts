import { LeafenTransportError } from "@/apiTransport";
import {
  fileManualCleanupStatuses,
  type FileManualCleanupInput,
  type FileManualCleanupJob,
  type FileManualCleanupResponse,
} from "@/fileUsageReportWire.generated";
import { newId } from "@/lib/ids";
import type { Session } from "@/types";

type StartCleanup = (
  session: Session,
  workspaceId: string,
  input: FileManualCleanupInput,
  signal?: AbortSignal,
) => Promise<FileManualCleanupResponse>;

type ReadCleanup = (
  session: Session,
  workspaceId: string,
  jobId: string,
  signal?: AbortSignal,
) => Promise<FileManualCleanupResponse>;

type CleanupWait = (signal: AbortSignal) => Promise<void>;
type CleanupFlight = { promise: Promise<FileManualCleanupResponse>; signal: AbortSignal };

const TERMINAL_STATUSES = new Set<FileManualCleanupJob["status"]>([
  fileManualCleanupStatuses.completed,
  fileManualCleanupStatuses.partialFailure,
  fileManualCleanupStatuses.failed,
]);
const POLL_MILLISECONDS = 250;

function abortError() {
  return new DOMException("Aborted", "AbortError");
}

function invalidResponse() {
  return new Error("Invalid manual cleanup response.");
}

function nonnegativeInteger(value: number) {
  return Number.isSafeInteger(value) && value >= 0;
}

function pollWait(signal: AbortSignal) {
  if (signal.aborted) return Promise.reject(abortError());
  return new Promise<void>((resolve, reject) => {
    const timeout = window.setTimeout(() => {
      signal.removeEventListener("abort", onAbort);
      resolve();
    }, POLL_MILLISECONDS);
    const onAbort = () => {
      window.clearTimeout(timeout);
      reject(abortError());
    };
    signal.addEventListener("abort", onAbort, { once: true });
  });
}

function assertCleanupResponse(
  response: FileManualCleanupResponse,
  expected: {
    jobId?: string;
    previous?: FileManualCleanupJob;
    requestId: string;
    workspaceId: string;
  },
) {
  const job = response?.job;
  const validStatus = job?.status === fileManualCleanupStatuses.running
    || TERMINAL_STATUSES.has(job?.status);
  if (
    !job
    || typeof response.duplicate !== "boolean"
    || !job.id
    || (expected.jobId && job.id !== expected.jobId)
    || job.requestId !== expected.requestId
    || job.workspaceId !== expected.workspaceId
    || !validStatus
    || !nonnegativeInteger(job.revision)
    || !nonnegativeInteger(job.scanned)
    || !nonnegativeInteger(job.expired)
    || !nonnegativeInteger(job.deletedObjects)
    || !nonnegativeInteger(job.failedObjects)
    || !nonnegativeInteger(job.startedAt)
    || !nonnegativeInteger(job.completedAt)
    || (job.status === fileManualCleanupStatuses.running) !== (job.completedAt === 0)
  ) throw invalidResponse();
  const previous = expected.previous;
  if (
    previous
    && (
      job.revision < previous.revision
      || job.scanned < previous.scanned
      || job.expired < previous.expired
      || job.deletedObjects < previous.deletedObjects
      || job.failedObjects < previous.failedObjects
      || (job.status !== previous.status && job.revision <= previous.revision)
    )
  ) throw invalidResponse();
  return response;
}

export function createWorkspaceFileManualCleanupDriver({
  createRequestId = newId,
  read,
  start,
  wait = pollWait,
}: {
  createRequestId?: () => string;
  read: ReadCleanup;
  start: StartCleanup;
  wait?: CleanupWait;
}) {
  const inFlight = new Map<string, CleanupFlight>();
  const retryIntents = new Map<string, string>();

  function run(session: Session, workspaceId: string, signal: AbortSignal) {
    const key = `${session.user.id}\u0000${session.token}\u0000${workspaceId}`;
    const existing = inFlight.get(key);
    if (existing && !existing.signal.aborted) return existing.promise;
    if (existing) inFlight.delete(key);
    const requestId = retryIntents.get(key) ?? createRequestId();
    const task = (async () => {
      if (signal.aborted) throw abortError();
      let response = assertCleanupResponse(
        await start(session, workspaceId, { requestId }, signal),
        { requestId, workspaceId },
      );
      while (!TERMINAL_STATUSES.has(response.job.status)) {
        await wait(signal);
        if (signal.aborted) throw abortError();
        response = assertCleanupResponse(
          await read(session, workspaceId, response.job.id, signal),
          {
            jobId: response.job.id,
            previous: response.job,
            requestId,
            workspaceId,
          },
        );
      }
      if (signal.aborted) throw abortError();
      retryIntents.delete(key);
      return response;
    })().catch((error) => {
      if (error instanceof LeafenTransportError && !signal.aborted) {
        retryIntents.set(key, requestId);
      } else if (retryIntents.get(key) === requestId) {
        retryIntents.delete(key);
      }
      throw error;
    }).finally(() => {
      if (inFlight.get(key)?.promise === task) inFlight.delete(key);
    });
    inFlight.set(key, { promise: task, signal });
    return task;
  }

  return { run };
}
