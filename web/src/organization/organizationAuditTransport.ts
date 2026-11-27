import { enterpriseApi as api } from "./enterpriseSettingsApi";
import { throwLeafenApiError } from "@/apiTransport";
import {
  organizationAuditBounds,
  organizationAuditErrorCodes,
  organizationAuditExportFormats,
  organizationAuditExportStatuses,
  type OrganizationAuditExport,
  type OrganizationAuditExportCreateInput,
} from "@/organizationAuditWire.generated";
import type { Session } from "@/types";
import { sha256Hex } from "@/lib/browserCrypto";
import type { OrganizationAuditLogFile } from "@/lib/organizationModule";

const pollMilliseconds = 250;
const statuses = new Set<string>(Object.values(organizationAuditExportStatuses));
const formats = new Set<string>(Object.values(organizationAuditExportFormats));
const signalIdentities = new WeakMap<AbortSignal, number>();
const runs = new WeakMap<Session, Map<string, Promise<OrganizationAuditDownload>>>();
let nextSignalIdentity = 1;

type SessionFence = {
  token: string;
  sessionId?: string;
  userId: string;
};

export type OrganizationAuditDownload = OrganizationAuditLogFile;

function captureSession(session: Session): SessionFence {
  return { token: session.token, sessionId: session.sessionId, userId: session.user.id };
}

function assertSession(session: Session, fence: SessionFence, signal?: AbortSignal) {
  if (signal?.aborted) throw signal.reason;
  if (
    session.token !== fence.token
    || session.sessionId !== fence.sessionId
    || session.user.id !== fence.userId
  ) {
    throwLeafenApiError(
      organizationAuditErrorCodes.unavailable,
      409,
      "Audit export session changed while work was in flight.",
      { stage: "organization-audit-session" },
    );
  }
}

/**
 * A receipt the server could have written. Its limit is the count asked for,
 * and 0 asks for the whole filtered range (the server's own reading), so only
 * a positive limit caps the events processed.
 */
function assertExport(
  value: OrganizationAuditExport,
  organizationId: string,
  exportId?: string,
) {
  if (
    !value?.id
    || value.organizationId !== organizationId
    || (exportId !== undefined && value.id !== exportId)
    || !statuses.has(value.status)
    || !formats.has(value.format)
    || !Number.isSafeInteger(value.limit)
    || value.limit < 0
    || !Number.isSafeInteger(value.eventCount)
    || value.eventCount < 0
    || !Number.isSafeInteger(value.processedEvents)
    || value.processedEvents < 0
    || (value.limit > 0 && value.processedEvents > value.limit)
    || !Number.isSafeInteger(value.contentBytes)
    || value.contentBytes < 0
  ) {
    throwLeafenApiError(
      organizationAuditErrorCodes.unavailable,
      500,
      "Audit export identity is invalid.",
      { stage: "organization-audit-identity" },
    );
  }
  if (
    value.status === organizationAuditExportStatuses.completed
    && (
      value.eventCount !== value.processedEvents
      || typeof value.contentSha256 !== "string"
      || !/^[a-f0-9]{64}$/u.test(value.contentSha256)
    )
  ) {
    throwLeafenApiError(
      organizationAuditErrorCodes.unavailable,
      500,
      "Completed audit export receipt is invalid.",
      { stage: "organization-audit-identity" },
    );
  }
  return value;
}

function waitForPoll(signal?: AbortSignal): Promise<void> {
  if (signal?.aborted) return Promise.reject(signal.reason);
  return new Promise((resolve, reject) => {
    const onAbort = () => {
      globalThis.clearTimeout(timeout);
      reject(signal?.reason);
    };
    const timeout = globalThis.setTimeout(() => {
      signal?.removeEventListener("abort", onAbort);
      resolve();
    }, pollMilliseconds);
    signal?.addEventListener("abort", onAbort, { once: true });
  });
}

async function waitForCompletion(
  session: Session,
  organizationId: string,
  initial: OrganizationAuditExport,
  signal?: AbortSignal,
) {
  const fence = captureSession(session);
  let processed = initial.processedEvents;
  for (;;) {
    await waitForPoll(signal);
    assertSession(session, fence, signal);
    const response = await api.readOrganizationAuditExport(
      session,
      organizationId,
      initial.id,
      signal,
    );
    assertSession(session, fence, signal);
    const current = assertExport(response.auditExport, organizationId, initial.id);
    if (current.processedEvents < processed) {
      throwLeafenApiError(
        organizationAuditErrorCodes.unavailable,
        500,
        "Audit export progress moved backwards.",
        { stage: "organization-audit-poll" },
      );
    }
    processed = current.processedEvents;
    if (current.status === organizationAuditExportStatuses.completed) return current;
    if (current.status === organizationAuditExportStatuses.failed) {
      throwLeafenApiError(
        current.errorCode ?? organizationAuditErrorCodes.unavailable,
        409,
        "Audit export failed.",
        { reference: current.diagnosticReference, stage: "organization-audit-poll" },
      );
    }
  }
}

function auditItemCount(content: string, format: OrganizationAuditExport["format"]) {
  if (format === organizationAuditExportFormats.csv) return null;
  const values: unknown[] = format === organizationAuditExportFormats.json
    ? (() => {
      const decoded: unknown = JSON.parse(content);
      if (!Array.isArray(decoded)) throw new Error("Audit JSON artifact is not an array.");
      return decoded;
    })()
    : content === ""
      ? []
      : content.split("\n").filter(Boolean).map((line) => JSON.parse(line) as unknown);
  const identities = new Set<string>();
  for (const value of values) {
    if (typeof value !== "object" || value === null || !("id" in value) || typeof value.id !== "string") {
      throw new Error("Audit export event identity is invalid.");
    }
    if (identities.has(value.id)) throw new Error("Audit export event identity repeated.");
    identities.add(value.id);
  }
  return values.length;
}

async function download(
  session: Session,
  completed: OrganizationAuditExport,
  signal?: AbortSignal,
): Promise<OrganizationAuditDownload> {
  const fence = captureSession(session);
  if (
    completed.status !== organizationAuditExportStatuses.completed
    || organizationAuditBounds.downloadChunkWindow < 1
  ) {
    throwLeafenApiError(
      organizationAuditErrorCodes.unavailable,
      409,
      "Audit export is not ready for download.",
      { stage: "organization-audit-download" },
    );
  }
  const requestBinaryResponse = api.downloadOrganizationAuditExport;
  const response = await requestBinaryResponse(
    session,
    completed.organizationId,
    completed.id,
    signal,
  );
  assertSession(session, fence, signal);
  const bytes = new Uint8Array(await response.arrayBuffer());
  assertSession(session, fence, signal);
  const digest = await sha256Hex(bytes);
  assertSession(session, fence, signal);
  if (bytes.byteLength !== completed.contentBytes || digest !== completed.contentSha256) {
    throwLeafenApiError(
      organizationAuditErrorCodes.unavailable,
      500,
      "Audit export content does not match its receipt.",
      { stage: "organization-audit-download" },
    );
  }
  let content = "";
  try {
    content = new TextDecoder("utf-8", { fatal: true }).decode(bytes);
    const itemCount = auditItemCount(content, completed.format);
    if (itemCount !== null && itemCount !== completed.eventCount) {
      throw new Error("Audit export event count mismatch.");
    }
  } catch {
    throwLeafenApiError(
      organizationAuditErrorCodes.unavailable,
      500,
      "Audit export content is malformed.",
      { stage: "organization-audit-download" },
    );
  }
  const contentType = response.headers.get("content-type") ?? "application/octet-stream";
  const suffix = completed.format === "jsonl" ? "jsonl" : completed.format;
  return {
    record: completed,
    filename: `organization-audit-${completed.id}.${suffix}`,
    content,
    contentType,
  };
}

function runKey(
  organizationId: string,
  input: OrganizationAuditExportCreateInput,
  signal?: AbortSignal,
) {
  let signalIdentity = 0;
  if (signal) {
    signalIdentity = signalIdentities.get(signal) ?? nextSignalIdentity++;
    signalIdentities.set(signal, signalIdentity);
  }
  return JSON.stringify({ organizationId, input, signalIdentity });
}

export async function runOrganizationAuditExport(
  session: Session,
  organizationId: string,
  input: OrganizationAuditExportCreateInput,
  signal?: AbortSignal,
) {
  if (!input.mutationId) {
    throwLeafenApiError(
      organizationAuditErrorCodes.invalid,
      400,
      "Audit export mutationId is required.",
      { stage: "organization-audit-create" },
    );
  }
  const key = runKey(organizationId, input, signal);
  let sessionRuns = runs.get(session);
  if (!sessionRuns) {
    sessionRuns = new Map();
    runs.set(session, sessionRuns);
  }
  const existing = sessionRuns.get(key);
  if (existing) return existing;
  const operation = (async () => {
    const fence = captureSession(session);
    const response = await api.startOrganizationAuditExport(session, organizationId, input, signal);
    assertSession(session, fence, signal);
    const admitted = assertExport(response.auditExport, organizationId);
    const completed = admitted.status === organizationAuditExportStatuses.completed
      ? admitted
      : await waitForCompletion(session, organizationId, admitted, signal);
    assertSession(session, fence, signal);
    return download(session, completed, signal);
  })();
  sessionRuns.set(key, operation);
  try {
    return await operation;
  } finally {
    if (sessionRuns.get(key) === operation) sessionRuns.delete(key);
  }
}
