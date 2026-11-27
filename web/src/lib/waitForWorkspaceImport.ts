import { waitForDurableJob } from "./durableJobPolling";
import {
  workspaceImportStatuses,
  type WorkspaceImportJob,
} from "../workspaceImportWire.generated";

function isWorkspaceImportTerminal(status: WorkspaceImportJob["status"]) {
  return status === workspaceImportStatuses.completed
    || status === workspaceImportStatuses.cancelled
    || status === workspaceImportStatuses.failed;
}

export function waitForWorkspaceImport(options: {
  initial: WorkspaceImportJob;
  onProgress: (job: WorkspaceImportJob) => void;
  read: () => Promise<WorkspaceImportJob>;
  signal: AbortSignal;
  workspaceId: string;
}) {
  return waitForDurableJob({
    initial: options.initial,
    isSameJob: (next) => next.id === options.initial.id
      && next.workspaceId === options.workspaceId,
    isTerminal: (next) => isWorkspaceImportTerminal(next.status),
    onProgress: options.onProgress,
    read: options.read,
    signal: options.signal,
  });
}
