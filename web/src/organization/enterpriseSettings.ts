import type { EnterpriseCurrentContext, EnterpriseModuleService, EnterpriseModuleSnapshot, EnterpriseOperationContext } from "@/lib/enterpriseSettingsAdapter";
import {
  assertOperationCurrent,
  assertReadCurrent,
  drainPages,
  replayKey,
  replaySafe,
  type WorkspaceEnterpriseReadContext,
} from "@/lib/workspaceEnterpriseSettings";
import type { LegalHold } from "@/enterpriseLegalHoldWire.generated";
import type { OrganizationAuditExport } from "@/organizationAuditWire.generated";
import type { OrganizationDiscoveryExport } from "@/organizationDiscoveryWire.generated";
import type { Session } from "@/types";
import { loadSAMLManagementSnapshot } from "./enterpriseSAMLManagementTransport";
import { enterpriseApi } from "./enterpriseSettingsApi";
import { runOrganizationAuditExport } from "./organizationAuditTransport";
import { listOrganizationDiscoveryExports, runOrganizationDiscoveryExport } from "./organizationDiscoveryTransport";

// The organization features' part of the workspace settings' enterprise
// controls: what they read and the changes they make. The core's settings
// read the rest (sharing security, the Admin API's tokens) and join them.

function listAllOrganizationLegalHolds(
  session: Session,
  organizationId: string,
  signal: AbortSignal,
  assertCurrent: () => void,
) {
  return drainPages<LegalHold>(async (cursor) => {
    const page = await enterpriseApi.listOrganizationLegalHolds(session, organizationId, cursor, signal);
    return { items: page.items, nextCursor: page.nextCursor };
  }, assertCurrent);
}

function listAllOrganizationAuditExports(
  session: Session,
  organizationId: string,
  signal: AbortSignal,
  assertCurrent: () => void,
) {
  return drainPages<OrganizationAuditExport>(async (cursor) => {
    const page = await enterpriseApi.listOrganizationAuditExports(session, organizationId, cursor, signal);
    return { items: page.auditExports, nextCursor: page.nextCursor };
  }, assertCurrent);
}

function listAllOrganizationDiscoveryExports(
  session: Session,
  organizationId: string,
  signal: AbortSignal,
  assertCurrent: () => void,
) {
  return drainPages<OrganizationDiscoveryExport>(async (cursor) => {
    const page = await listOrganizationDiscoveryExports(
      session,
      organizationId,
      { cursor, signal },
    );
    return { items: page.discoveryExports, nextCursor: page.nextCursor };
  }, assertCurrent);
}

export async function loadEnterpriseModuleSnapshot(
  session: Session,
  context: WorkspaceEnterpriseReadContext,
): Promise<EnterpriseModuleSnapshot> {
  assertReadCurrent(context);
  const assertCurrent = () => assertReadCurrent(context);
  // Each area is read only when the person's admin roles cover it; the
  // others keep empty values the screens don't show.
  const { areas } = context;
  const when = <T>(allowed: boolean, read: () => Promise<T>, empty: T) => (allowed ? read() : Promise.resolve(empty));
  const samlRead = () => loadSAMLManagementSnapshot(session, context.organizationId, {
    requestGeneration: context.requestGeneration,
    current: () => {
      const current = context.current();
      return { organizationId: current.organizationId, requestGeneration: current.requestGeneration };
    },
    signal: context.signal,
  });
  const [saml, scim, dlp, auditRetention, legalHolds, auditExports, discoveryExports, mcpPolicy] = await Promise.all([
    when(areas.identity, samlRead, { provider: null, domains: [], workspaces: [] }),
    when(areas.identity, () => enterpriseApi.readSCIMConfiguration(session, context.organizationId, context.signal), {
      enabled: false, provisioningMode: "scim_v2", deprovisionAction: "deactivate", revision: 0, tokens: [],
    }),
    when(areas.security, () => enterpriseApi.readDLPPolicy(session, context.organizationId, context.signal), {
      enabled: false, contentScanMode: "off", sensitiveTerms: [], blockPublicSharing: false,
      blockExternalSharing: false, blockFileDownloads: false, blockExports: false, revision: 0,
    }),
    when(areas.auditLog, () => enterpriseApi.readAuditRetention(session, context.organizationId, context.signal), {
      organizationId: context.organizationId, retentionDays: 0, revision: 0, updatedAt: 0, duplicate: false,
    }),
    when(areas.legalHolds, () => listAllOrganizationLegalHolds(session, context.organizationId, context.signal, assertCurrent), []),
    when(areas.auditLog, () => listAllOrganizationAuditExports(session, context.organizationId, context.signal, assertCurrent), []),
    when(areas.contentSearch, () => listAllOrganizationDiscoveryExports(session, context.organizationId, context.signal, assertCurrent), []),
    when(areas.integrations, () => enterpriseApi.readMCPGovernancePolicy(
      session, context.organizationId, context.workspaceId, context.signal,
    ), null),
  ]);
  assertReadCurrent(context);
  return {
    identity: {
      revisions: [saml.provider?.revision ?? 0, scim.revision, dlp.revision, auditRetention.revision, mcpPolicy?.revision ?? 0],
      legalHolds: legalHolds.map(({ id, status }) => [id, status]),
      auditExports: auditExports.map(({ id, status }) => [id, status]),
      discoveryExports: discoveryExports.map(({ id, revision }) => [id, revision]),
    },
    saml,
    scim,
    dlp,
    mcpPolicies: mcpPolicy ? { [context.workspaceId]: mcpPolicy } : {},
    auditRetention,
    legalHolds,
    auditExports,
    discoveryExports,
  };
}

export function createEnterpriseModuleService(
  session: Session,
  isCurrent: EnterpriseCurrentContext,
): EnterpriseModuleService {
  const run = async <T>(context: EnterpriseOperationContext, operation: () => Promise<T>) => {
    assertOperationCurrent(context, isCurrent);
    const result = await operation();
    assertOperationCurrent(context, isCurrent);
    return result;
  };
  const identity = (context: EnterpriseOperationContext) => [
    context.sessionIdentity, context.organizationId,
  ].join("\u0000");
  const listHolds = (context: EnterpriseOperationContext) => listAllOrganizationLegalHolds(
    session, context.organizationId, context.signal, () => assertOperationCurrent(context, isCurrent),
  );
  return {
    readMCPPolicy: (context, workspaceId) => run(context, () => enterpriseApi.readMCPGovernancePolicy(
      session, context.organizationId, workspaceId, context.signal,
    )),
    updateSAML: async (context, input) => run(context, async () => {
      await enterpriseApi.updateSAMLConfiguration(session, context.organizationId, input, context.signal);
      assertOperationCurrent(context, isCurrent);
      return loadSAMLManagementSnapshot(session, context.organizationId, {
        requestGeneration: context.requestGeneration,
        current: () => ({ organizationId: context.organizationId, requestGeneration: context.requestGeneration }),
        signal: context.signal,
      });
    }),
    setSAMLServiceReturn: async (context, added) => run(context, async () => {
      await enterpriseApi.setSAMLServiceReturn(session, context.organizationId, added, context.signal);
      assertOperationCurrent(context, isCurrent);
      return loadSAMLManagementSnapshot(session, context.organizationId, {
        requestGeneration: context.requestGeneration,
        current: () => ({ organizationId: context.organizationId, requestGeneration: context.requestGeneration }),
        signal: context.signal,
      });
    }),
    updateSCIM: (context, input) => run(context, () => enterpriseApi.updateSCIMConfiguration(
      session, context.organizationId, input, context.signal,
    )),
    createSCIMToken: (context, input) => run(context, async () => {
      const token = await enterpriseApi.createSCIMToken(session, context.organizationId, input, context.signal);
      assertOperationCurrent(context, isCurrent);
      const configuration = await enterpriseApi.readSCIMConfiguration(session, context.organizationId, context.signal);
      return { configuration, token };
    }),
    revokeSCIMToken: (context, tokenId) => run(context, async () => {
      await enterpriseApi.revokeSCIMToken(session, context.organizationId, tokenId, context.signal);
      assertOperationCurrent(context, isCurrent);
      return enterpriseApi.readSCIMConfiguration(session, context.organizationId, context.signal);
    }),
    updateMCPPolicy: (context, workspaceId, input) => run(context, () => enterpriseApi.updateMCPGovernancePolicy(
      session, context.organizationId, workspaceId, input, context.signal,
    )),
    approveMCPClient: (context, workspaceId, input) => run(context, () => enterpriseApi.approveMCPGovernanceClient(
      session, context.organizationId, workspaceId, input, context.signal,
    )),
    renameMCPClient: (context, workspaceId, clientId, input) => run(context, () => enterpriseApi.renameMCPGovernanceClient(
      session, context.organizationId, workspaceId, clientId, input, context.signal,
    )),
    removeMCPClient: (context, workspaceId, clientId) => run(context, () => enterpriseApi.removeMCPGovernanceClient(
      session, context.organizationId, workspaceId, clientId, context.signal,
    )),
    updateDLP: (context, input) => run(context, () => enterpriseApi.updateDLPPolicy(
      session, context.organizationId, input, context.signal,
    )),
    updateAuditRetention: (context, input) => run(context, () => enterpriseApi.updateAuditRetention(
      session, context.organizationId, input, context.signal,
    )),
    exportAudit: (context, input) => run(context, () => {
      const key = replayKey(identity(context), "audit-export", input);
      return replaySafe(key, (mutationId) => runOrganizationAuditExport(
        session, context.organizationId, { ...input, mutationId }, context.signal,
      ));
    }),
    exportDiscovery: (context, input) => run(context, async () => {
      const key = replayKey(identity(context), "discovery-export", input);
      const result = await replaySafe(key, (mutationId) => runOrganizationDiscoveryExport(
        session, context.organizationId, { ...input, mutationId }, context.signal,
      ));
      return {
        record: result.export,
        filename: `organization-discovery-${result.export.id}.${result.export.format}`,
        content: result.text,
        contentType: result.export.format === "json" ? "application/json" : "application/x-ndjson",
      };
    }),
    createLegalHold: (context, input) => run(context, async () => {
      const key = replayKey(identity(context), "legal-hold-create", input);
      await replaySafe(key, (mutationId) => enterpriseApi.createOrganizationLegalHold(
        session, context.organizationId, { ...input, mutationId }, context.signal,
      ));
      assertOperationCurrent(context, isCurrent);
      return listHolds(context);
    }),
    releaseLegalHold: (context, legalHoldId) => run(context, async () => {
      const key = replayKey(identity(context), "legal-hold-release", legalHoldId);
      await replaySafe(key, (mutationId) => enterpriseApi.releaseOrganizationLegalHold(
        session, context.organizationId, legalHoldId, { mutationId }, context.signal,
      ));
      assertOperationCurrent(context, isCurrent);
      return listHolds(context);
    }),
  };
}
