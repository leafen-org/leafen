import { viewLinkBounds } from "@/viewLinkWire.generated";
import { api } from "@/api";
import { organizationModule } from "@/lib/organizationModule";
import type { EnterpriseAreas } from "./organizationSpecialistRolePolicy";
import { LeafenTransportError } from "@/apiTransport";
import type { EnterpriseModuleService, EnterpriseModuleSnapshot, EnterpriseSettingsSnapshot, EnterpriseSettingsService } from "@/lib/enterpriseSettingsAdapter";
import { copyText } from "@/lib/copyText";
import { newId } from "@/lib/ids";
import type { NotionAdminApiBounds, NotionAdminApiService } from "@/lib/notionAdminApiView";
import type { EnterpriseOperationContext, EnterpriseCurrentContext } from "@/lib/enterpriseSettingsAdapter";
import {
  notionAdminTokenBounds,
  type OrganizationAdminToken,
} from "@/notionAdminTokenWire.generated";
import type { Session, Workspace } from "@/types";
import { announceSharingSecurityChanged } from "./sharingSecurityEvents";

export type WorkspaceEnterpriseReadContext = {
  sessionIdentity: string;
  organizationId: string;
  workspaceId: string;
  requestGeneration: number;
  signal: AbortSignal;
  /** The areas the person's admin roles cover. */
  areas: EnterpriseAreas;
  current: () => {
    sessionIdentity: string;
    organizationId: string;
    workspaceId: string;
    requestGeneration: number;
  };
};

type CursorPage<T> = { items: T[]; nextCursor?: string | null };
const replayMutationIds = new Map<string, string>();

function abortError(message: string): never {
  throw new DOMException(message, "AbortError");
}

export function assertReadCurrent(context: WorkspaceEnterpriseReadContext) {
  if (context.signal.aborted) throw context.signal.reason ?? abortError("Enterprise read aborted.");
  const current = context.current();
  if (
    current.sessionIdentity !== context.sessionIdentity
    || current.organizationId !== context.organizationId
    || current.workspaceId !== context.workspaceId
    || current.requestGeneration !== context.requestGeneration
  ) abortError("Enterprise read context changed.");
}

export function assertOperationCurrent(
  context: EnterpriseOperationContext,
  isCurrent: EnterpriseCurrentContext,
) {
  if (context.signal.aborted || !isCurrent(context)) abortError("Enterprise operation context changed.");
}

export async function drainPages<T extends { id: string }>(
  read: (cursor?: string) => Promise<CursorPage<T>>,
  assertCurrent: () => void,
) {
  const items: T[] = [];
  const seenCursors = new Set<string>();
  const seenIds = new Set<string>();
  let cursor: string | undefined;
  for (;;) {
    const cursorIdentity = cursor ?? "";
    if (seenCursors.has(cursorIdentity)) throw new Error("Enterprise list cursor repeated.");
    seenCursors.add(cursorIdentity);
    const page = await read(cursor);
    assertCurrent();
    for (const item of page.items) {
      if (!item.id || seenIds.has(item.id)) throw new Error("Enterprise list identity repeated.");
      seenIds.add(item.id);
      items.push(item);
    }
    if (!page.nextCursor) return items;
    cursor = page.nextCursor;
  }
}




export function listAllNotionAdminTokens(
  session: Session,
  organizationId: string,
  signal: AbortSignal,
  assertCurrent: () => void,
) {
  return drainPages<OrganizationAdminToken>(async (cursor) => {
    const page = await api.listNotionAdminTokens(session, organizationId, cursor, signal);
    if (page.hasMore !== Boolean(page.nextCursor)) {
      throw new Error("Notion Admin token continuation is invalid.");
    }
    return { items: page.adminTokens, nextCursor: page.nextCursor };
  }, assertCurrent);
}

function normalize(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(normalize);
  if (value && typeof value === "object") {
    return Object.fromEntries(Object.entries(value as Record<string, unknown>)
      .sort(([left], [right]) => left.localeCompare(right))
      .map(([key, entry]) => [key, normalize(entry)]));
  }
  return typeof value === "string" ? value.normalize("NFKC").trim() : value;
}

export function replayKey(
  contextIdentity: string,
  operation: string,
  input: unknown,
) {
  return JSON.stringify([contextIdentity, operation, normalize(input)]);
}

export async function replaySafe<T>(key: string, operation: (mutationId: string) => Promise<T>) {
  const mutationId = replayMutationIds.get(key) ?? newId();
  replayMutationIds.set(key, mutationId);
  try {
    const result = await operation(mutationId);
    replayMutationIds.delete(key);
    return result;
  } catch (error) {
    if (!(error instanceof LeafenTransportError)) replayMutationIds.delete(key);
    throw error;
  }
}



// The enterprise controls' reads and changes: the organization's sharing
// security is the core's, and the organization module adds the rest
// (single sign-on, SCIM, DLP, legal holds, audit, discovery, MCP approval).
export async function loadWorkspaceEnterpriseSettingsSnapshot(
  session: Session,
  workspaces: readonly Workspace[],
  context: WorkspaceEnterpriseReadContext,
): Promise<EnterpriseSettingsSnapshot> {
  assertReadCurrent(context);
  // What a hold, a content search or an agent policy may name: every
  // workspace of the organization, the admin's own or not (2026-11-06); the
  // person's own list while that read fails.
  const ownWorkspaces = workspaces
    .filter((workspace) => workspace.organizationId === context.organizationId)
    .map(({ id, name }) => ({ id, name }));
  const { areas } = context;
  const enterprise = organizationModule()?.enterprise;
  const [moduleSnapshot, sharingSecurity, listed] = await Promise.all([
    enterprise ? enterprise.loadSnapshot(session, context) : Promise.resolve(emptyEnterpriseModuleSnapshot(context.organizationId)),
    context.areas.security
      ? api.readOrganizationSharingSecurityPolicy(session, context.organizationId, context.signal)
      // Without the security area: the server's defaults (defaultSharingSecurityPolicy).
      : Promise.resolve({
        organizationId: context.organizationId, policyVersion: 0, publicWebSharing: true,
        externalEmailSharing: true, guestAccess: true, exports: true, crossWorkspaceCopies: true,
        guestInvites: "members" as const, membershipRequests: "all" as const, memberAdds: "approval" as const, pageAccessRequests: true,
        viewLinks: true, viewLinkMaxSeconds: viewLinkBounds.maxSeconds, viewLinkSingleUseOnly: false, viewLinkWithoutSecret: true, userHtml: true,
        restrictedTeamspaceAdds: "owners" as const, restrictedPageAdds: "members" as const, pendingGuestPrivatePages: true,
        guestInviteRequests: true, restrictedPrivatePages: true,
      }),
    areas.legalHolds || areas.contentSearch || areas.integrations
      ? api.listOrganizationWorkspaces(session, context.organizationId, context.signal).then((list) => list.workspaces, () => null)
      : Promise.resolve(null),
  ]);
  assertReadCurrent(context);
  const organizationWorkspaces = [...(listed ?? ownWorkspaces)];
  organizationWorkspaces.sort((left, right) => (
    left.id === context.workspaceId ? -1 : right.id === context.workspaceId ? 1 : left.name.localeCompare(right.name)
  ));
  const { identity, ...moduleParts } = moduleSnapshot;
  const snapshotIdentity = JSON.stringify({
    sessionIdentity: context.sessionIdentity,
    organizationId: context.organizationId,
    workspaceId: context.workspaceId,
    requestGeneration: context.requestGeneration,
    sharingSecurity: sharingSecurity.policyVersion,
    organizationModule: identity,
  });
  return {
    snapshotIdentity,
    ...moduleParts,
    sharingSecurity,
    workspaces: organizationWorkspaces,
    currentWorkspaceId: context.workspaceId,
    memberUserIds: [session.user.id],
  };
}

/** The organization module's part as it reads without the module: nothing set. */
function emptyEnterpriseModuleSnapshot(organizationId: string): EnterpriseModuleSnapshot {
  return {
    identity: null,
    saml: { provider: null, domains: [], workspaces: [] },
    scim: { enabled: false, provisioningMode: "scim_v2", deprovisionAction: "deactivate", revision: 0, tokens: [] },
    dlp: {
      enabled: false, contentScanMode: "off", sensitiveTerms: [], blockPublicSharing: false,
      blockExternalSharing: false, blockFileDownloads: false, blockExports: false, revision: 0,
    },
    mcpPolicies: {},
    auditRetention: { organizationId, retentionDays: 0, revision: 0, updatedAt: 0, duplicate: false },
    legalHolds: [],
    auditExports: [],
    discoveryExports: [],
  };
}

export function createWorkspaceEnterpriseSettingsService(
  session: Session,
  isCurrent: EnterpriseCurrentContext,
): EnterpriseSettingsService {
  const run = async <T>(context: EnterpriseOperationContext, operation: () => Promise<T>) => {
    assertOperationCurrent(context, isCurrent);
    const result = await operation();
    assertOperationCurrent(context, isCurrent);
    return result;
  };
  const identity = (context: EnterpriseOperationContext) => [
    context.sessionIdentity, context.organizationId,
  ].join("\u0000");
  const enterprise = organizationModule()?.enterprise;
  return {
    ...(enterprise ? enterprise.createService(session, isCurrent) : unattachedEnterpriseModuleService),
    updateSharingSecurity: (context, input) => run(context, async () => {
      const key = replayKey(identity(context), "sharing-security", input);
      const response = await replaySafe(key, (requestId) => api.updateSharingSecurityPolicy(
        session, context.organizationId, { ...input, requestId }, context.signal,
      ));
      announceSharingSecurityChanged({ userHtml: response.policy.userHtml });
      return response.policy;
    }),
    writeClipboard: async (value) => {
      if (!(await copyText(value))) throw new Error("Clipboard write failed.");
    },
  };
}

// Without the organization module its controls aren't shown, so nothing
// asks for these.
const unattachedEnterpriseModuleService = new Proxy({}, {
  get: () => () => Promise.reject(new Error("The organization module isn't attached.")),
}) as EnterpriseModuleService;

export function createNotionAdminService(
  session: Session,
  organizationId: string,
  signal: AbortSignal,
  isCurrent: () => boolean,
): { bounds: NotionAdminApiBounds; service: NotionAdminApiService } {
  const assertCurrent = () => {
    if (signal.aborted || !isCurrent()) abortError("Notion Admin context changed.");
  };
  const contextIdentity = [session.user.id, session.token, organizationId].join("\u0000");
  const service: NotionAdminApiService = {
    listTokens: async (requestedOrganizationId) => {
      if (requestedOrganizationId !== organizationId) abortError("Notion Admin organization changed.");
      return listAllNotionAdminTokens(session, organizationId, signal, assertCurrent);
    },
    createToken: async (input) => {
      if (input.organizationId !== organizationId) abortError("Notion Admin organization changed.");
      assertCurrent();
      const { organizationId: _organizationId, ...request } = input;
      const key = replayKey(contextIdentity, "notion-admin-create", request);
      const result = await replaySafe(key, (requestId) => api.createNotionAdminToken(
        session, organizationId, { ...request, requestId }, signal,
      ));
      assertCurrent();
      return result;
    },
    revokeToken: async ({ organizationId: requestedOrganizationId, tokenId }) => {
      if (requestedOrganizationId !== organizationId) abortError("Notion Admin organization changed.");
      assertCurrent();
      const key = replayKey(contextIdentity, "notion-admin-revoke", tokenId);
      const result = await replaySafe(key, (requestId) => api.revokeNotionAdminToken(
        session, organizationId, tokenId, { requestId }, signal,
      ));
      assertCurrent();
      return result;
    },
    writeClipboard: async (value) => {
      assertCurrent();
      if (!(await copyText(value))) throw new Error("Clipboard write failed.");
      assertCurrent();
    },
  };
  return {
    bounds: {
      maxTokens: Number.MAX_SAFE_INTEGER,
      maxResourceIds: notionAdminTokenBounds.resourceIds,
      maxResourceIdCharacters: notionAdminTokenBounds.resourceIdRunes,
      maxScalarCharacters: notionAdminTokenBounds.bodyBytes,
      maxSecretCharacters: notionAdminTokenBounds.bodyBytes,
      maxTotalCharacters: notionAdminTokenBounds.bodyBytes,
    },
    service,
  };
}
