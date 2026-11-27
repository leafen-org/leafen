import { request, throwLeafenApiError } from "@/apiTransport";
import {
  enterpriseSAMLErrorCodes,
  enterpriseSAMLOperations,
  samlDomainStatusValues,
  samlEnforcementValues,
  type SAMLDomain,
  type SAMLManagementSnapshot,
  type SAMLProvider,
  type SAMLWorkspace,
} from "@/enterpriseSAMLWire.generated";
import type { Session } from "@/types";

type SAMLManagementSessionFence = {
  token: string;
  sessionId?: string;
  userId: string;
};

export type SAMLManagementReadContext = {
  requestGeneration: number;
  current: () => { organizationId: string; requestGeneration: number };
  signal?: AbortSignal;
};

const samlManagementRuns = new WeakMap<Session, Map<string, Promise<SAMLManagementSnapshot>>>();
const samlManagementSignalIdentities = new WeakMap<AbortSignal, number>();
const samlEnforcements = new Set<string>(Object.values(samlEnforcementValues));
const samlDomainStatuses = new Set<string>(Object.values(samlDomainStatusValues));
let nextSAMLManagementSignalIdentity = 1;

function captureSAMLManagementSession(session: Session): SAMLManagementSessionFence {
  return { token: session.token, sessionId: session.sessionId, userId: session.user.id };
}

function assertSAMLManagementSession(
  session: Session,
  fence: SAMLManagementSessionFence,
  organizationId: string,
  context: SAMLManagementReadContext,
) {
  if (context.signal?.aborted) throw context.signal.reason;
  const current = context.current();
  if (
    session.token !== fence.token
    || session.sessionId !== fence.sessionId
    || session.user.id !== fence.userId
    || current.organizationId !== organizationId
    || current.requestGeneration !== context.requestGeneration
  ) {
    throwLeafenApiError(
      enterpriseSAMLErrorCodes.unavailable,
      409,
      "SAML management session changed while the snapshot was in flight.",
      { stage: "saml-management-session" },
    );
  }
}

function assertSAMLProvider(value: unknown, organizationId: string): SAMLProvider | null {
  if (value === null) return null;
  if (typeof value !== "object" || value === null) return invalidSAMLManagementSnapshot();
  const provider = value as SAMLProvider & Record<string, unknown>;
  if (
    !provider.id
    || provider.organizationId !== organizationId
    || typeof provider.enabled !== "boolean"
    || typeof provider.issuer !== "string"
    || typeof provider.loginUrl !== "string"
    || typeof provider.allowIdpInitiated !== "boolean"
    || !samlEnforcements.has(provider.enforcement)
    || typeof provider.emailAttribute !== "string"
    || !Number.isSafeInteger(provider.revision)
    || provider.revision < 1
    || "certificate" in provider
    || "certificateCipher" in provider
  ) return invalidSAMLManagementSnapshot();
  return provider;
}

function assertSAMLDomains(value: unknown): SAMLDomain[] {
  if (!Array.isArray(value)) return invalidSAMLManagementSnapshot();
  const identities = new Set<string>();
  let previous = "";
  for (const entry of value) {
    if (typeof entry !== "object" || entry === null) return invalidSAMLManagementSnapshot();
    const domain = entry as SAMLDomain;
    if (
      !domain.domain
      || !samlDomainStatuses.has(domain.status)
      || (domain.verificationRecord !== undefined && typeof domain.verificationRecord !== "string")
      || identities.has(domain.domain)
      || (previous !== "" && previous >= domain.domain)
    ) return invalidSAMLManagementSnapshot();
    identities.add(domain.domain);
    previous = domain.domain;
  }
  return value as SAMLDomain[];
}

// The organization's workspaces, as its SAML configuration covers them (by name);
// an app's workspace nobody has been given yet has no members.
function assertSAMLWorkspaces(value: unknown): SAMLWorkspace[] {
  if (!Array.isArray(value)) return invalidSAMLManagementSnapshot();
  const identities = new Set<string>();
  for (const entry of value) {
    if (typeof entry !== "object" || entry === null) return invalidSAMLManagementSnapshot();
    const workspace = entry as SAMLWorkspace;
    if (
      !workspace.id
      || typeof workspace.name !== "string"
      || !Number.isSafeInteger(workspace.memberCount)
      || workspace.memberCount < 0
      || typeof workspace.createdAt !== "string"
      || identities.has(workspace.id)
    ) return invalidSAMLManagementSnapshot();
    identities.add(workspace.id);
  }
  return value as SAMLWorkspace[];
}

function invalidSAMLManagementSnapshot(): never {
  return throwLeafenApiError(
    enterpriseSAMLErrorCodes.unavailable,
    500,
    "SAML management snapshot is invalid.",
    { stage: "saml-management-snapshot" },
  );
}

function signalIdentity(signal?: AbortSignal) {
  if (!signal) return 0;
  const existing = samlManagementSignalIdentities.get(signal);
  if (existing !== undefined) return existing;
  const identity = nextSAMLManagementSignalIdentity;
  nextSAMLManagementSignalIdentity += 1;
  samlManagementSignalIdentities.set(signal, identity);
  return identity;
}

export async function loadSAMLManagementSnapshot(
  session: Session,
  organizationId: string,
  context: SAMLManagementReadContext,
): Promise<SAMLManagementSnapshot> {
  if (context.signal?.aborted) throw context.signal.reason;
  if (!Number.isSafeInteger(context.requestGeneration) || context.requestGeneration < 0) {
    return invalidSAMLManagementSnapshot();
  }
  const fence = captureSAMLManagementSession(session);
  const key = `${organizationId}\u0000${context.requestGeneration}\u0000${signalIdentity(context.signal)}`;
  let runs = samlManagementRuns.get(session);
  if (!runs) {
    runs = new Map();
    samlManagementRuns.set(session, runs);
  }
  const existing = runs.get(key);
  if (existing) return existing;

  let run: Promise<SAMLManagementSnapshot>;
  run = (async () => {
    const path = enterpriseSAMLOperations.managementSnapshot.path.replace(
      "{organizationId}",
      encodeURIComponent(organizationId),
    );
    const snapshot = await request<SAMLManagementSnapshot>(path, { signal: context.signal }, session);
    assertSAMLManagementSession(session, fence, organizationId, context);
    return {
      provider: assertSAMLProvider(snapshot.provider, organizationId),
      domains: assertSAMLDomains(snapshot.domains),
      workspaces: assertSAMLWorkspaces(snapshot.workspaces),
    };
  })().finally(() => {
    if (runs?.get(key) === run) runs.delete(key);
    if (runs?.size === 0) samlManagementRuns.delete(session);
  });
  runs.set(key, run);
  return run;
}
