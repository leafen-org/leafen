import {
  request,
  requestBinaryResponse,
  throwLeafenApiError,
} from "@/apiTransport";
import type { Session } from "@/types";
import {
  organizationDiscoveryBounds,
  organizationDiscoveryErrorCodes,
  organizationDiscoveryExportFormats,
  organizationDiscoveryExportPhases,
  organizationDiscoveryExportStatuses,
  organizationDiscoveryOperations,
  type OrganizationDiscoveryExport,
  type OrganizationDiscoveryExportContent,
  type OrganizationDiscoveryExportCreateInput,
  type OrganizationDiscoveryExportList,
} from "@/organizationDiscoveryWire.generated";
import { sha256Hex } from "@/lib/browserCrypto";

const organizationDiscoveryMaximumItems = organizationDiscoveryBounds.maximumItems;
const organizationDiscoveryMaximumBytes = organizationDiscoveryBounds.maximumBytes;
const organizationDiscoveryDownloadChunkWindow = organizationDiscoveryBounds.downloadChunkWindow;
const organizationDiscoveryPollMilliseconds = 250;
const organizationDiscoveryFormats = new Set<string>(Object.values(organizationDiscoveryExportFormats));
const organizationDiscoveryPhases = new Set<string>(Object.values(organizationDiscoveryExportPhases));
const organizationDiscoveryStatuses = new Set<string>(Object.values(organizationDiscoveryExportStatuses));
const organizationDiscoveryRuns = new WeakMap<
  Session,
  Map<string, Promise<Awaited<ReturnType<typeof downloadOrganizationDiscoveryExport>>>>
>();
const organizationDiscoverySignalIdentities = new WeakMap<AbortSignal, number>();
let organizationDiscoveryNextSignalIdentity = 1;

type OrganizationDiscoverySessionFence = {
  token: string;
  sessionId?: string;
  userId: string;
};

function captureOrganizationDiscoverySession(session: Session): OrganizationDiscoverySessionFence {
  return { token: session.token, sessionId: session.sessionId, userId: session.user.id };
}

function assertOrganizationDiscoverySession(
  session: Session,
  fence: OrganizationDiscoverySessionFence,
  signal?: AbortSignal,
) {
  if (signal?.aborted) throw signal.reason;
  if (
    session.token !== fence.token
    || session.sessionId !== fence.sessionId
    || session.user.id !== fence.userId
  ) {
    throwLeafenApiError(
      organizationDiscoveryErrorCodes.unavailable,
      409,
      "Discovery export session changed while work was in flight.",
      { stage: "organization-discovery-session" },
    );
  }
}

function organizationDiscoveryRunKey(
  organizationId: string,
  input: OrganizationDiscoveryExportCreateInput,
  signalIdentity: number,
) {
  const identifiers = (values: string[] | undefined) => Array.from(new Set(
    (values ?? []).map((value) => value.trim()),
  )).sort();
  return JSON.stringify({
    organizationId,
    mutationId: input.mutationId.trim(),
    query: (input.query ?? "").normalize("NFKC").trim(),
    userIds: identifiers(input.userIds),
    workspaceIds: identifiers(input.workspaceIds),
    since: input.since?.trim() ?? "",
    until: input.until?.trim() ?? "",
    includeTrashed: input.includeTrashed ?? true,
    format: input.format?.trim().toLowerCase() ?? "jsonl",
    signalIdentity,
  });
}

const discoveryCollectionPath = (organizationId: string) =>
  organizationDiscoveryOperations.listExports.path.replace(
    "{organizationId}",
    encodeURIComponent(organizationId),
  );

const discoveryExportPath = (organizationId: string, exportId: string) =>
  organizationDiscoveryOperations.getExport.path
    .replace("{organizationId}", encodeURIComponent(organizationId))
    .replace("{exportId}", encodeURIComponent(exportId));

const discoveryDownloadPath = (organizationId: string, exportId: string) =>
  organizationDiscoveryOperations.downloadExport.path
    .replace("{organizationId}", encodeURIComponent(organizationId))
    .replace("{exportId}", encodeURIComponent(exportId));

function assertOrganizationDiscoveryIdentity(
  value: OrganizationDiscoveryExport,
  organizationId: string,
  exportId?: string,
): OrganizationDiscoveryExport {
  if (
    !value.id
    || value.organizationId !== organizationId
    || (exportId !== undefined && value.id !== exportId)
    || !organizationDiscoveryFormats.has(value.format)
    || !organizationDiscoveryPhases.has(value.phase)
    || !organizationDiscoveryStatuses.has(value.status)
    || !Number.isSafeInteger(value.revision)
    || value.revision < 1
    || !Number.isSafeInteger(value.itemCount)
    || value.itemCount < 0
    || value.itemCount > organizationDiscoveryMaximumItems
    || !Number.isSafeInteger(value.byteCount)
    || value.byteCount < 0
    || value.byteCount > organizationDiscoveryMaximumBytes
    || !Number.isSafeInteger(value.chunkCount)
    || value.chunkCount < 0
    || typeof value.contentSha256 !== "string"
  ) {
    throwLeafenApiError(
      organizationDiscoveryErrorCodes.unavailable,
      500,
      "Discovery export identity is invalid.",
      { stage: "organization-discovery-identity" },
    );
  }
  return value;
}

export async function listOrganizationDiscoveryExports(
  session: Session,
  organizationId: string,
  options: { cursor?: string; signal?: AbortSignal } = {},
): Promise<OrganizationDiscoveryExportList> {
  const fence = captureOrganizationDiscoverySession(session);
  const query = new URLSearchParams();
  if (options.cursor) query.set("cursor", options.cursor);
  const suffix = query.size > 0 ? `?${query.toString()}` : "";
  const result = await request<OrganizationDiscoveryExportList>(
    `${discoveryCollectionPath(organizationId)}${suffix}`,
    { signal: options.signal },
    session,
  );
  assertOrganizationDiscoverySession(session, fence, options.signal);
  if (!Array.isArray(result.discoveryExports)) {
    throwLeafenApiError(
      organizationDiscoveryErrorCodes.unavailable,
      500,
      "Discovery export list is invalid.",
      { stage: "organization-discovery-list" },
    );
  }
  const identities = new Set<string>();
  result.discoveryExports.forEach((item) => {
    const value = assertOrganizationDiscoveryIdentity(item, organizationId);
    if (identities.has(value.id)) {
      throwLeafenApiError(
        organizationDiscoveryErrorCodes.unavailable,
        500,
        "Discovery export list repeated an identity.",
        { stage: "organization-discovery-list" },
      );
    }
    identities.add(value.id);
  });
  return result;
}

export async function createOrganizationDiscoveryExport(
  session: Session,
  organizationId: string,
  input: OrganizationDiscoveryExportCreateInput,
  signal?: AbortSignal,
): Promise<OrganizationDiscoveryExport> {
  const fence = captureOrganizationDiscoverySession(session);
  if (!input.mutationId) {
    throwLeafenApiError(
      organizationDiscoveryErrorCodes.invalid,
      400,
      "Discovery export mutationId is required.",
      { stage: "organization-discovery-create" },
    );
  }
  const result = await request<OrganizationDiscoveryExportContent>(
    discoveryCollectionPath(organizationId),
    { method: organizationDiscoveryOperations.createExport.method, body: JSON.stringify(input), signal },
    session,
  );
  assertOrganizationDiscoverySession(session, fence, signal);
  return assertOrganizationDiscoveryIdentity(result.discoveryExport, organizationId);
}

async function getOrganizationDiscoveryExport(
  session: Session,
  organizationId: string,
  exportId: string,
  signal?: AbortSignal,
): Promise<OrganizationDiscoveryExport> {
  const fence = captureOrganizationDiscoverySession(session);
  const result = await request<OrganizationDiscoveryExportContent>(
    discoveryExportPath(organizationId, exportId),
    { signal },
    session,
  );
  assertOrganizationDiscoverySession(session, fence, signal);
  return assertOrganizationDiscoveryIdentity(result.discoveryExport, organizationId, exportId);
}

function waitForDiscoveryPoll(signal?: AbortSignal): Promise<void> {
  if (signal && signal.aborted) return Promise.reject(signal.reason);
  const pollSignal = signal;
  return new Promise((resolve, reject) => {
    const onAbort = () => {
      globalThis.clearTimeout(timeout);
      reject(pollSignal?.reason);
    };
    const timeout = globalThis.setTimeout(() => {
      pollSignal?.removeEventListener("abort", onAbort);
      resolve();
    }, organizationDiscoveryPollMilliseconds);
    pollSignal?.addEventListener("abort", onAbort, { once: true });
  });
}

async function waitForOrganizationDiscoveryExport(
  session: Session,
  organizationId: string,
  exportId: string,
  initialRevision: number,
  signal?: AbortSignal,
): Promise<OrganizationDiscoveryExport> {
  const fence = captureOrganizationDiscoverySession(session);
  let revision = initialRevision;
  for (;;) {
    if (signal?.aborted) throw signal.reason;
    await waitForDiscoveryPoll(signal);
    assertOrganizationDiscoverySession(session, fence, signal);
    const current = await getOrganizationDiscoveryExport(session, organizationId, exportId, signal);
    assertOrganizationDiscoverySession(session, fence, signal);
    if (current.revision < revision) {
      throwLeafenApiError(
        organizationDiscoveryErrorCodes.unavailable,
        500,
        "Discovery export revision moved backwards.",
        { stage: "organization-discovery-poll" },
      );
    }
    revision = current.revision;
    if (current.status === organizationDiscoveryExportStatuses.completed) return current;
    if (current.status === organizationDiscoveryExportStatuses.failed) {
      throwLeafenApiError(
        current.errorCode ?? organizationDiscoveryErrorCodes.unavailable,
        409,
        "Discovery export failed.",
        { reference: current.diagnosticReference, stage: "organization-discovery-poll" },
      );
    }
  }
}

function organizationDiscoveryItemIdentity(item: unknown): string {
  if (
    typeof item !== "object"
    || item === null
    || !("type" in item)
    || !("id" in item)
    || typeof item.type !== "string"
    || typeof item.id !== "string"
    || item.type.length === 0
    || item.id.length === 0
  ) {
    throw new Error("Discovery export item identity is invalid.");
  }
  return `${item.type}:${item.id}`;
}

function organizationDiscoveryItemCount(content: string, format: "jsonl" | "json"): number {
  const items: unknown[] = format === "json"
    ? (() => {
      const decoded: unknown = JSON.parse(content);
      if (!Array.isArray(decoded)) throw new Error("Discovery JSON artifact is not an array.");
      return decoded;
    })()
    : content === ""
      ? []
      : content.split("\n").map((line) => JSON.parse(line) as unknown);
  const identities = new Set<string>();
  for (const item of items) {
    const identity = organizationDiscoveryItemIdentity(item);
    if (identities.has(identity)) throw new Error("Discovery export item identity repeated.");
    identities.add(identity);
  }
  return items.length;
}

async function downloadOrganizationDiscoveryExport(
  session: Session,
  value: OrganizationDiscoveryExport,
  signal?: AbortSignal,
): Promise<{ export: OrganizationDiscoveryExport; bytes: Uint8Array; text: string }> {
  const fence = captureOrganizationDiscoverySession(session);
  const completed = assertOrganizationDiscoveryIdentity(value, value.organizationId, value.id);
  if (
    completed.status !== organizationDiscoveryExportStatuses.completed
    || completed.contentSha256.length !== 64
    || organizationDiscoveryDownloadChunkWindow < 1
  ) {
    throwLeafenApiError(
      organizationDiscoveryErrorCodes.unavailable,
      409,
      "Discovery export is not ready for download.",
      { stage: "organization-discovery-download" },
    );
  }
  const response = await requestBinaryResponse(
    discoveryDownloadPath(completed.organizationId, completed.id),
    session,
    { signal },
  );
  assertOrganizationDiscoverySession(session, fence, signal);
  const bytes = new Uint8Array(await response.arrayBuffer());
  assertOrganizationDiscoverySession(session, fence, signal);
  const digest = await sha256Hex(bytes);
  assertOrganizationDiscoverySession(session, fence, signal);
  if (
    bytes.byteLength !== completed.byteCount
    || bytes.byteLength > organizationDiscoveryMaximumBytes
    || digest !== completed.contentSha256
  ) {
    throwLeafenApiError(
      organizationDiscoveryErrorCodes.unavailable,
      500,
      "Discovery export content does not match its receipt.",
      { stage: "organization-discovery-download" },
    );
  }
  let text = "";
  let itemCount = -1;
  try {
    text = new TextDecoder("utf-8", { fatal: true }).decode(bytes);
    itemCount = organizationDiscoveryItemCount(text, completed.format);
  } catch {
    throwLeafenApiError(
      organizationDiscoveryErrorCodes.unavailable,
      500,
      "Discovery export content is malformed.",
      { stage: "organization-discovery-download" },
    );
  }
  if (itemCount !== completed.itemCount) {
    throwLeafenApiError(
      organizationDiscoveryErrorCodes.unavailable,
      500,
      "Discovery export item count does not match its receipt.",
      { stage: "organization-discovery-download" },
    );
  }
  return { export: completed, bytes, text };
}

export async function runOrganizationDiscoveryExport(
  session: Session,
  organizationId: string,
  input: OrganizationDiscoveryExportCreateInput,
  signal?: AbortSignal,
) {
  let signalIdentity = 0;
  if (signal) {
    signalIdentity = organizationDiscoverySignalIdentities.get(signal) ?? organizationDiscoveryNextSignalIdentity++;
    organizationDiscoverySignalIdentities.set(signal, signalIdentity);
  }
  const key = organizationDiscoveryRunKey(organizationId, input, signalIdentity);
  let sessionRuns = organizationDiscoveryRuns.get(session);
  if (!sessionRuns) {
    sessionRuns = new Map();
    organizationDiscoveryRuns.set(session, sessionRuns);
  }
  const existing = sessionRuns.get(key);
  if (existing) return existing;
  const operation = (async () => {
    const fence = captureOrganizationDiscoverySession(session);
    const admitted = await createOrganizationDiscoveryExport(session, organizationId, input, signal);
    assertOrganizationDiscoverySession(session, fence, signal);
    const completed = admitted.status === organizationDiscoveryExportStatuses.completed
      ? admitted
      : await waitForOrganizationDiscoveryExport(
        session, organizationId, admitted.id, admitted.revision, signal,
      );
    assertOrganizationDiscoverySession(session, fence, signal);
    const downloaded = await downloadOrganizationDiscoveryExport(session, completed, signal);
    assertOrganizationDiscoverySession(session, fence, signal);
    return downloaded;
  })();
  sessionRuns.set(key, operation);
  try {
    return await operation;
  } finally {
    if (sessionRuns.get(key) === operation) sessionRuns.delete(key);
  }
}
