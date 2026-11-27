import { i18next } from "@/i18n";
import {
  isDiagnosticReference,
  messageWithDiagnosticReference,
} from "./diagnosticTrace";
import { logProductError } from "./safeErrorLog";
import { isPageBusyRefusal } from "./pageBusy";
import {
  collaborationStorageStatuses,
  type CollaborationStorageStatusResponse,
} from "../collaborationStorageWire.generated";

export type CollaborationStatus =
  | "idle"
  | "loading"
  | "connecting"
  | "synced"
  | "disconnected"
  | "storage-error"
  | "storage-recovered"
  | "recovery-error";

export type CollaborationState = {
  status: CollaborationStatus;
  diagnosticReference: string | null;
};

export type CollaborationRecoveryStage =
  | "browser_recovery_append"
  | "browser_recovery_compaction"
  | "browser_recovery_replay";

const collaborationText = (key: string) => String(i18next.t(`syncStatusBadge:collaboration.${key}`));
const withReference = (message: string, reference: string) =>
  messageWithDiagnosticReference(message, reference, String(i18next.t("common:labels.reference")));

export function collaborationState(status: Exclude<CollaborationStatus, "recovery-error">): CollaborationState {
  return { status, diagnosticReference: null };
}

export function collaborationRecoveryState(diagnosticReference: unknown): CollaborationState {
  return {
    status: "recovery-error",
    diagnosticReference: isDiagnosticReference(diagnosticReference)
      ? diagnosticReference
      : null,
  };
}

export function collaborationTransportState(
  current: CollaborationState,
  status: Exclude<CollaborationStatus, "recovery-error">,
): CollaborationState {
  return current.status === "recovery-error" || current.status === "storage-error" ||
    current.status === "storage-recovered" ? current : collaborationState(status);
}

export function collaborationStorageState(
  response: CollaborationStorageStatusResponse,
  fallback: Exclude<CollaborationStatus, "recovery-error" | "storage-error" | "storage-recovered">,
): CollaborationState {
  if (response.status === collaborationStorageStatuses.healthy) {
    return collaborationState(fallback);
  }
  return {
    status: response.status === collaborationStorageStatuses.recovered
      ? "storage-recovered"
      : "storage-error",
    diagnosticReference: isDiagnosticReference(response.diagnosticReference)
      ? response.diagnosticReference
      : null,
  };
}

export function collaborationStorageTransition(
  current: CollaborationState,
  response: CollaborationStorageStatusResponse,
  fallback: Exclude<CollaborationStatus, "recovery-error" | "storage-error" | "storage-recovered">,
): CollaborationState {
  return current.status === "recovery-error"
    ? current
    : collaborationStorageState(response, fallback);
}

export function collaborationRecoveryFailure(
  error: unknown,
  causeStage: CollaborationRecoveryStage,
): CollaborationState {
  const diagnosticReference = logProductError(
    "error",
    "collaboration.recovery",
    error,
    { causeStage },
  );
  return collaborationRecoveryState(diagnosticReference);
}

export function collaborationStatusReadFailure(
  error: unknown,
  fallback: "disconnected" | "synced",
): CollaborationState {
  // A page an operation holds (its databases being copied into it) refuses
  // its room for a while: the page waits for it, nothing failed.
  if (isPageBusyRefusal(error)) return collaborationState("connecting");
  logProductError("warn", "collaboration.recovery", error, {
    causeStage: "status-read",
  });
  return collaborationState(fallback);
}

/**
 * Whether a page's live editing is lost: its connection closed and none has
 * opened again — the tries to open meanwhile are part of it (collaboration.ts
 * says "disconnected" until one opens, 2026-11-18: a database's page never
 * said it was lost, its status going between connecting and disconnected).
 * Its first opening, on its way however far the server is, is no lost
 * connection, nor one open and catching up (2026-11-18: a server 2 seconds
 * away took 14 seconds, one 5 seconds away over 40, and the top bar said
 * "오프라인" while every request was answered). A connection that stops
 * answering without closing is closed by its checks (pageRoomLiveness.ts).
 */
export function collaborationLost(state: CollaborationState): boolean {
  return state.status === "disconnected";
}

export function collaborationStatusMessage(state: CollaborationState): string | null {
  switch (state.status) {
    case "idle":
      return null;
    case "loading":
    case "connecting":
      return collaborationText("connecting");
    case "synced":
      return collaborationText("connected");
    case "disconnected":
      return collaborationText("disconnected");
    case "storage-error": {
      const message = collaborationText("storageError");
      return state.diagnosticReference ? withReference(message, state.diagnosticReference) : message;
    }
    case "storage-recovered": {
      const message = collaborationText("storageRecovered");
      return state.diagnosticReference ? withReference(message, state.diagnosticReference) : message;
    }
    case "recovery-error":
      return state.diagnosticReference
        ? withReference(collaborationText("recoveryFailed"), state.diagnosticReference)
        : collaborationText("recoveryFailed");
  }
}
