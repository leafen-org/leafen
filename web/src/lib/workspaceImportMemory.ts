import { createDurableJobMemory } from "./durableJobMemory";

const WORKSPACE_IMPORT_JOB_STORAGE_PREFIX = "leafen:workspace-import-job";

export interface RememberedWorkspaceImportJob {
  jobId: string;
}

function validRememberedWorkspaceImportJob(
  value: unknown,
): value is RememberedWorkspaceImportJob {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const jobId = (value as Record<string, unknown>).jobId;
  return (
    typeof jobId === "string"
    && jobId.length > 0
    && jobId.length <= 96
  );
}

const workspaceImportJobMemory = createDurableJobMemory<RememberedWorkspaceImportJob>({
  bounds: {
    maxIdentityCharacters: 512,
    maxSerializedCharacters: 128,
    maxStorageKeyCharacters: 1_600,
  },
  decode(value) {
    const parsed: unknown = JSON.parse(value);
    return validRememberedWorkspaceImportJob(parsed) ? parsed : null;
  },
  encode(value) {
    return validRememberedWorkspaceImportJob(value) ? JSON.stringify(value) : null;
  },
  identityPartCount: 2,
  storagePrefix: WORKSPACE_IMPORT_JOB_STORAGE_PREFIX,
});

/**
 * The Notion import job this browser remembered for the workspace. Nothing
 * remembers one any more — an import by hand runs as a migration, which the
 * window finds on the server (2026-10-12) — so it is one remembered before,
 * which Notion's window still shows as it goes.
 */
export function readRememberedWorkspaceImportJob(
  actorId: string,
  workspaceId: string,
): RememberedWorkspaceImportJob | null {
  return workspaceImportJobMemory.read([actorId, workspaceId]);
}

/** Forgets the job remembered for the workspace, once it has ended. */
export function forgetRememberedWorkspaceImportJob(actorId: string, workspaceId: string) {
  workspaceImportJobMemory.clear([actorId, workspaceId]);
}
