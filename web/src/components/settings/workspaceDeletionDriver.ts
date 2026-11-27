import type { Session } from "@/types";
import type { WorkspaceDeletionResponse } from "@/workspaceMembershipWire.generated";

export const WORKSPACE_DELETION_MAX_STALLED_RESPONSES = 3;
const WORKSPACE_DELETION_CONTINUE_DELAY_MS = 120;

type DeleteWorkspaceTransport = (
  session: Session,
  workspaceId: string,
  input: { requestId: string; confirmationName: string },
  signal?: AbortSignal,
) => Promise<WorkspaceDeletionResponse>;

export type WorkspaceDeletionRequest = {
  session: Session;
  sessionIdentity: string;
  workspaceId: string;
  confirmationName: string;
  /** The deletion's own request when carrying on with one the server named. */
  requestId?: string;
  signal?: AbortSignal;
  isCurrent: () => boolean;
  onProgress: (response: WorkspaceDeletionResponse) => void;
};

export function requestIdForWorkspaceDeletion(workspaceId: string) {
  return `workspace-delete:${workspaceId}`;
}

function deletionProgressSignature(result: WorkspaceDeletionResponse) {
  return [
    result.jobId,
    result.status,
    result.phase,
    result.progress.revision,
    result.progress.pages,
    result.progress.files,
    result.progress.residueLane,
  ].join(":");
}

function defaultWait(signal?: AbortSignal) {
  return new Promise<void>((resolve, reject) => {
    if (signal?.aborted) {
      reject(new DOMException("Workspace deletion canceled.", "AbortError"));
      return;
    }
    const onAbort = () => {
      globalThis.clearTimeout(timer);
      reject(new DOMException("Workspace deletion canceled.", "AbortError"));
    };
    const timer = globalThis.setTimeout(() => {
      signal?.removeEventListener("abort", onAbort);
      resolve();
    }, WORKSPACE_DELETION_CONTINUE_DELAY_MS);
    signal?.addEventListener("abort", onAbort, { once: true });
  });
}

function obsoleteDeletion() {
  return new DOMException("Workspace deletion context changed.", "AbortError");
}

/**
 * Drives a workspace's deletion a request at a time to its end. A large
 * workspace takes hundreds of requests (2026-11-10: a workspace of 100,000
 * database rows); the driver carries on by itself while each answer moves
 * the deletion on, so no one presses "resume" for a count or a time they
 * can't change. It stops when the deletion ends, when the tab moves on, on
 * an error, or when answers stop moving it on.
 */
export function createWorkspaceDeletionDriver({
  deleteWorkspace,
  wait = defaultWait,
}: {
  deleteWorkspace: DeleteWorkspaceTransport;
  wait?: (signal?: AbortSignal) => Promise<void>;
}) {
  const sameWorkspaceFlights = new Map<string, Promise<WorkspaceDeletionResponse>>();

  function run(input: WorkspaceDeletionRequest) {
    const key = `${input.sessionIdentity}\u0000${input.workspaceId}`;
    const existing = sameWorkspaceFlights.get(key);
    if (existing) return existing;

    const requestId = input.requestId || requestIdForWorkspaceDeletion(input.workspaceId);
    const task = (async () => {
      let previousJobId = "";
      let previousSignature = "";
      let stalledResponses = 0;
      for (;;) {
        if (!input.isCurrent() || input.signal?.aborted) throw obsoleteDeletion();
        const result = await deleteWorkspace(input.session, input.workspaceId, {
          requestId,
          confirmationName: input.confirmationName,
        }, input.signal);
        if (!input.isCurrent() || input.signal?.aborted) throw obsoleteDeletion();
        if (result.workspaceId !== input.workspaceId) {
          throw new Error("Workspace deletion returned another workspace identity.");
        }
        if (previousJobId && result.jobId !== previousJobId) {
          throw new Error("Workspace deletion changed job identity while resuming.");
        }
        if (result.status !== "pending" && result.status !== "completed") {
          throw new Error("Workspace deletion returned an unknown lifecycle status.");
        }
        previousJobId = result.jobId;
        input.onProgress(result);
        if (result.status === "completed") return result;

        const signature = deletionProgressSignature(result);
        if (signature === previousSignature) {
          stalledResponses += 1;
          if (stalledResponses >= WORKSPACE_DELETION_MAX_STALLED_RESPONSES) {
            throw new Error("Workspace deletion stopped advancing. Resume to retry.");
          }
        } else {
          previousSignature = signature;
          stalledResponses = 0;
        }
        await wait(input.signal);
      }
    })().finally(() => {
      if (sameWorkspaceFlights.get(key) === task) sameWorkspaceFlights.delete(key);
    });
    // One same-workspace flight is shared; mixed workspaces always progress independently.
    sameWorkspaceFlights.set(key, task);
    return task;
  }

  return { run };
}
