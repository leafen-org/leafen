import { createDurableJobMemory } from "./durableJobMemory";
import type {
  WorkspaceExportFormat,
  WorkspaceExportScope,
} from "@/workspaceExportWire.generated";

const WORKSPACE_EXPORT_JOB_STORAGE_PREFIX = "leafen:workspace-export-job";
const WORKSPACE_EXPORT_START_STORAGE_PREFIX = "leafen:workspace-export-start";
const WORKSPACE_EXPORT_JOB_MEMORY_BOUNDS = Object.freeze({
  maxOptionKeyCharacters: 64,
  maxOptions: 32,
  maxRequestIdRunes: 255,
});

export interface RememberedWorkspaceExportJob {
  jobId: string;
  format: WorkspaceExportFormat;
  scope: WorkspaceExportScope;
  options: Record<string, boolean>;
}

export interface RememberedWorkspaceExportStartIntent {
  requestId: string;
  format: WorkspaceExportFormat;
  scope: WorkspaceExportScope;
  options: Record<string, boolean>;
}

function validWorkspaceExportFormat(value: unknown): value is WorkspaceExportFormat {
  return [
    "native_archive", "markdown_csv", "page_markdown", "notion_markdown",
    "database_csv", "page_native_archive", "html", "print_html",
  ].includes(String(value));
}

function validWorkspaceExportScope(value: unknown): value is WorkspaceExportScope {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const record = value as Record<string, unknown>;
  if (![
    "workspace", "page", "database", "block_subtree",
  ].includes(String(record.kind)) || typeof record.targetId !== "string") return false;
  return record.kind === "workspace"
    ? record.targetId === ""
    : record.targetId.length > 0 && record.targetId.length <= 255;
}

function withLegacyWorkspaceScope(value: unknown): unknown {
  if (!value || typeof value !== "object" || Array.isArray(value)) return value;
  const record = value as Record<string, unknown>;
  if (record.scope !== undefined
    || (record.format !== "native_archive" && record.format !== "markdown_csv")) return value;
  return { ...record, scope: { kind: "workspace", targetId: "" } };
}

function validWorkspaceExportOptions(value: unknown): value is Record<string, boolean> {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const prototype = Object.getPrototypeOf(value);
  if (prototype !== Object.prototype && prototype !== null) return false;
  let count = 0;
  for (const key in value) {
    if (!Object.hasOwn(value, key)) continue;
    count += 1;
    if (
      count > WORKSPACE_EXPORT_JOB_MEMORY_BOUNDS.maxOptions
      || key.length === 0
      || key.length > WORKSPACE_EXPORT_JOB_MEMORY_BOUNDS.maxOptionKeyCharacters
      || (value as Record<string, unknown>)[key] !== true
      && (value as Record<string, unknown>)[key] !== false
    ) {
      return false;
    }
  }
  return true;
}

function validRememberedWorkspaceExportJob(
  value: unknown,
): value is RememberedWorkspaceExportJob {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const record = value as Record<string, unknown>;
  return (
    typeof record.jobId === "string"
    && record.jobId.startsWith("workspace-export-")
    && record.jobId.length <= 96
    && validWorkspaceExportFormat(record.format)
    && validWorkspaceExportScope(record.scope)
    && validWorkspaceExportOptions(record.options)
  );
}

function validRememberedWorkspaceExportStartIntent(
  value: unknown,
): value is RememberedWorkspaceExportStartIntent {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const record = value as Record<string, unknown>;
  return (
    typeof record.requestId === "string"
    && record.requestId.length > 0
    && Array.from(record.requestId).length
      <= WORKSPACE_EXPORT_JOB_MEMORY_BOUNDS.maxRequestIdRunes
    && validWorkspaceExportFormat(record.format)
    && validWorkspaceExportScope(record.scope)
    && validWorkspaceExportOptions(record.options)
  );
}

const workspaceExportJobMemory = createDurableJobMemory<RememberedWorkspaceExportJob>({
  bounds: {
    maxIdentityCharacters: 512,
    maxSerializedCharacters: 4_096,
    maxStorageKeyCharacters: 1_600,
  },
  decode(value) {
    const parsed: unknown = withLegacyWorkspaceScope(JSON.parse(value));
    return validRememberedWorkspaceExportJob(parsed) ? parsed : null;
  },
  encode(value) {
    const normalized = withLegacyWorkspaceScope(value);
    return validRememberedWorkspaceExportJob(normalized) ? JSON.stringify(normalized) : null;
  },
  identityPartCount: 2,
  storagePrefix: WORKSPACE_EXPORT_JOB_STORAGE_PREFIX,
});

const workspaceExportStartIntentMemory =
  createDurableJobMemory<RememberedWorkspaceExportStartIntent>({
    bounds: {
      maxIdentityCharacters: 512,
      maxSerializedCharacters: 4_096,
      maxStorageKeyCharacters: 1_600,
    },
    decode(value) {
      const parsed: unknown = withLegacyWorkspaceScope(JSON.parse(value));
      return validRememberedWorkspaceExportStartIntent(parsed) ? parsed : null;
    },
    encode(value) {
      const normalized = withLegacyWorkspaceScope(value);
      return validRememberedWorkspaceExportStartIntent(normalized)
        ? JSON.stringify(normalized)
        : null;
    },
    identityPartCount: 2,
    storagePrefix: WORKSPACE_EXPORT_START_STORAGE_PREFIX,
  });

const volatileWorkspaceExportStartIntents =
  new Map<string, RememberedWorkspaceExportStartIntent>();

function workspaceExportStartIntentStorageKey(actorId: string, workspaceId: string) {
  return workspaceExportStartIntentMemory.storageKey([actorId, workspaceId]);
}

export function rememberWorkspaceExportJob(
  actorId: string,
  workspaceId: string,
  job: RememberedWorkspaceExportJob,
) {
  workspaceExportJobMemory.remember([actorId, workspaceId], job);
}

export function readRememberedWorkspaceExportJob(
  actorId: string,
  workspaceId: string,
): RememberedWorkspaceExportJob | null {
  return workspaceExportJobMemory.read([actorId, workspaceId]);
}

export function rememberWorkspaceExportStartIntent(
  actorId: string,
  workspaceId: string,
  intent: RememberedWorkspaceExportStartIntent,
) {
  const key = workspaceExportStartIntentStorageKey(actorId, workspaceId);
  if (!key || !validRememberedWorkspaceExportStartIntent(intent)) return false;
  const remembered = {
    requestId: intent.requestId,
    format: intent.format,
    scope: { ...intent.scope },
    options: { ...intent.options },
  };
  if (workspaceExportStartIntentMemory.remember([actorId, workspaceId], remembered)) {
    volatileWorkspaceExportStartIntents.delete(key);
  } else {
    volatileWorkspaceExportStartIntents.set(key, remembered);
  }
  return true;
}

export function readRememberedWorkspaceExportStartIntent(
  actorId: string,
  workspaceId: string,
): RememberedWorkspaceExportStartIntent | null {
  const key = workspaceExportStartIntentStorageKey(actorId, workspaceId);
  if (!key) return null;
  const durable = workspaceExportStartIntentMemory.read([actorId, workspaceId]);
  if (durable) {
    volatileWorkspaceExportStartIntents.delete(key);
    return durable;
  }
  return volatileWorkspaceExportStartIntents.get(key) ?? null;
}

export function clearRememberedWorkspaceExportStartIntent(
  actorId: string,
  workspaceId: string,
  requestId: string,
) {
  const key = workspaceExportStartIntentStorageKey(actorId, workspaceId);
  if (!key) return false;
  const remembered = readRememberedWorkspaceExportStartIntent(actorId, workspaceId);
  if (!remembered || remembered.requestId !== requestId) return false;
  volatileWorkspaceExportStartIntents.delete(key);
  workspaceExportStartIntentMemory.clear([actorId, workspaceId]);
  return true;
}
