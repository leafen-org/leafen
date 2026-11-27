import { createDurableJobMemory } from "./durableJobMemory";

const DIRECT_WORKSPACE_IMPORT_PREFIX = "leafen:direct-workspace-import-job";

type RememberedDirectWorkspaceImport = {
  jobId: string;
};

function validRememberedDirectWorkspaceImport(value: unknown): value is RememberedDirectWorkspaceImport {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const jobId = (value as Record<string, unknown>).jobId;
  return typeof jobId === "string" && jobId.length > 0 && jobId.length <= 96;
}

const memory = createDurableJobMemory<RememberedDirectWorkspaceImport>({
  bounds: {
    maxIdentityCharacters: 512,
    maxSerializedCharacters: 128,
    maxStorageKeyCharacters: 1_600,
  },
  decode(value) {
    const parsed: unknown = JSON.parse(value);
    return validRememberedDirectWorkspaceImport(parsed) ? parsed : null;
  },
  encode(value) {
    return validRememberedDirectWorkspaceImport(value) ? JSON.stringify(value) : null;
  },
  identityPartCount: 2,
  storagePrefix: DIRECT_WORKSPACE_IMPORT_PREFIX,
});

export function rememberDirectWorkspaceImport(actorId: string, workspaceId: string, jobId: string) {
  memory.remember([actorId, workspaceId], { jobId });
}

export function readRememberedDirectWorkspaceImport(actorId: string, workspaceId: string) {
  return memory.read([actorId, workspaceId]);
}

export function clearRememberedDirectWorkspaceImport(actorId: string, workspaceId: string) {
  memory.clear([actorId, workspaceId]);
}
