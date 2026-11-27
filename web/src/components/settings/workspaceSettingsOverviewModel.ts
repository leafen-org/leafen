import type { WorkspaceDeletionPhase, WorkspaceDeletionResponse } from "@/workspaceMembershipWire.generated";
import { serverOrigin } from "@/lib/serverOrigin";

export function normalizeWorkspaceSlug(value: string) {
  return value
    .trim()
    .toLowerCase()
    .replace(/\s+/gu, "-")
    .replace(/[^a-z0-9-]/gu, "")
    .replace(/-+/gu, "-")
    .replace(/^-|-$/gu, "");
}

export function workspaceUrlPrefix() {
  if (typeof window === "undefined") return "/workspace/";
  return `${serverOrigin().replace(/^https?:\/\//u, "")}/workspace/`;
}

export function workspaceDeletionPhaseKey(
  phase: WorkspaceDeletionPhase | "starting" | undefined,
) {
  switch (phase) {
    case "pages":
    case "files":
      return "deletionContent";
    case "residue":
      return "deletionRecords";
    case "final":
    case "completed":
      return "deletionFinish";
    default:
      return "deletionChecking";
  }
}

export function workspaceDeletionDeletedRecords(
  result: WorkspaceDeletionResponse | undefined,
) {
  if (!result) return 0;
  return Math.max(0, result.progress.pages) + Math.max(0, result.progress.files);
}

export function workspaceSettingsRevision(revision: number | undefined) {
  return Number.isSafeInteger(revision) && (revision ?? -1) >= 0 ? revision ?? 0 : 0;
}
