import { triggerBrowserFileDownload } from "./browserFileDownload";
import type {
  AuditRetentionPolicy,
  AuditRetentionUpdateInput,
} from "@/enterpriseAuditRetentionWire.generated";
import type {
  DLPPolicy,
  DLPPolicyInput,
} from "@/enterpriseDLPWire.generated";
import type {
  LegalHold,
  LegalHoldCreateInput,
} from "@/enterpriseLegalHoldWire.generated";
import type {
  MCPGovernanceApprovedClientInput,
  MCPGovernancePolicy,
  MCPGovernancePolicyInput,
  MCPGovernanceRenameClientInput,
} from "@/enterpriseMCPGovernanceWire.generated";
import type {
  SAMLConfigurationInput,
  SAMLManagementSnapshot,
} from "@/enterpriseSAMLWire.generated";
import type {
  SCIMConfiguration,
  SCIMConfigurationInput,
  SCIMTokenCreateInput,
  SCIMTokenCreation,
} from "@/enterpriseSCIMWire.generated";
import type {
  OrganizationAuditExport,
  OrganizationAuditExportCreateInput,
} from "@/organizationAuditWire.generated";
import type {
  OrganizationDiscoveryExport,
  OrganizationDiscoveryExportCreateInput,
} from "@/organizationDiscoveryWire.generated";
import type {
  OrganizationSharingSecurityPolicy,
  OrganizationSharingSecurityPolicyUpdateInput,
} from "@/organizationWire.generated";

export interface EnterpriseWorkspaceOption {
  id: string;
  name: string;
}

export interface EnterpriseSettingsSnapshot {
  snapshotIdentity: string;
  saml: SAMLManagementSnapshot;
  scim: SCIMConfiguration;
  dlp: DLPPolicy;
  mcpPolicies: Readonly<Record<string, MCPGovernancePolicy | undefined>>;
  auditRetention: AuditRetentionPolicy;
  legalHolds: readonly LegalHold[];
  sharingSecurity: OrganizationSharingSecurityPolicy;
  auditExports: readonly OrganizationAuditExport[];
  discoveryExports: readonly OrganizationDiscoveryExport[];
  workspaces: readonly EnterpriseWorkspaceOption[];
  /** The workspace whose settings are open, as Notion's 현재 marks it. */
  currentWorkspaceId: string;
  memberUserIds: readonly string[];
}

/**
 * The organization module's part of the snapshot, with what tells one read
 * of it from another.
 */
export type EnterpriseModuleSnapshot = Omit<EnterpriseSettingsSnapshot, "snapshotIdentity" | "sharingSecurity" | "workspaces" | "currentWorkspaceId" | "memberUserIds"> & {
  identity: unknown;
};

export interface EnterpriseOperationContext {
  sessionIdentity: string;
  organizationId: string;
  requestGeneration: number;
  snapshotIdentity: string;
  signal: AbortSignal;
}

export type EnterpriseCurrentContext = (
  context: EnterpriseOperationContext,
) => boolean;

export interface EnterpriseDownload<T> {
  record: T;
  filename: string;
  content: string;
  contentType: string;
}

export interface EnterpriseSettingsService {
  readMCPPolicy(
    context: EnterpriseOperationContext,
    workspaceId: string,
  ): Promise<MCPGovernancePolicy>;
  updateSAML(
    context: EnterpriseOperationContext,
    input: SAMLConfigurationInput,
  ): Promise<SAMLManagementSnapshot>;
  /** After the server's address changed, says the identity provider has the new values, or takes it back. */
  setSAMLServiceReturn(
    context: EnterpriseOperationContext,
    added: boolean,
  ): Promise<SAMLManagementSnapshot>;
  updateSCIM(
    context: EnterpriseOperationContext,
    input: SCIMConfigurationInput,
  ): Promise<SCIMConfiguration>;
  createSCIMToken(
    context: EnterpriseOperationContext,
    input: SCIMTokenCreateInput,
  ): Promise<{ configuration: SCIMConfiguration; token: SCIMTokenCreation }>;
  revokeSCIMToken(
    context: EnterpriseOperationContext,
    tokenId: string,
  ): Promise<SCIMConfiguration>;
  updateMCPPolicy(
    context: EnterpriseOperationContext,
    workspaceId: string,
    input: MCPGovernancePolicyInput,
  ): Promise<MCPGovernancePolicy>;
  approveMCPClient(
    context: EnterpriseOperationContext,
    workspaceId: string,
    input: MCPGovernanceApprovedClientInput,
  ): Promise<MCPGovernancePolicy>;
  renameMCPClient(
    context: EnterpriseOperationContext,
    workspaceId: string,
    clientId: string,
    input: MCPGovernanceRenameClientInput,
  ): Promise<MCPGovernancePolicy>;
  removeMCPClient(
    context: EnterpriseOperationContext,
    workspaceId: string,
    clientId: string,
  ): Promise<MCPGovernancePolicy>;
  updateDLP(
    context: EnterpriseOperationContext,
    input: DLPPolicyInput,
  ): Promise<DLPPolicy>;
  updateAuditRetention(
    context: EnterpriseOperationContext,
    input: AuditRetentionUpdateInput,
  ): Promise<AuditRetentionPolicy>;
  exportAudit(
    context: EnterpriseOperationContext,
    input: Omit<OrganizationAuditExportCreateInput, "mutationId">,
  ): Promise<EnterpriseDownload<OrganizationAuditExport>>;
  exportDiscovery(
    context: EnterpriseOperationContext,
    input: Omit<OrganizationDiscoveryExportCreateInput, "mutationId">,
  ): Promise<EnterpriseDownload<OrganizationDiscoveryExport>>;
  createLegalHold(
    context: EnterpriseOperationContext,
    input: Omit<LegalHoldCreateInput, "mutationId">,
  ): Promise<readonly LegalHold[]>;
  releaseLegalHold(
    context: EnterpriseOperationContext,
    legalHoldId: string,
  ): Promise<readonly LegalHold[]>;
  updateSharingSecurity(
    context: EnterpriseOperationContext,
    input: Omit<OrganizationSharingSecurityPolicyUpdateInput, "requestId">,
  ): Promise<OrganizationSharingSecurityPolicy>;
  writeClipboard(value: string): Promise<void>;
}

export type EnterpriseActionRunner = <T>(
  key: string,
  operation: (context: EnterpriseOperationContext) => Promise<T>,
) => Promise<T | undefined>;

export type EnterpriseSingleFlight = <T>(
  key: string,
  operation: () => Promise<T>,
) => Promise<T>;

export function createEnterpriseSingleFlight(): EnterpriseSingleFlight {
  const inFlight = new Map<string, Promise<unknown>>();
  return <T>(key: string, operation: () => Promise<T>) => {
    const existing = inFlight.get(key);
    if (existing) return existing as Promise<T>;
    const task = Promise.resolve().then(operation).finally(() => {
      if (inFlight.get(key) === task) inFlight.delete(key);
    });
    inFlight.set(key, task);
    return task;
  };
}

export function enterpriseOperationContext(
  context: EnterpriseOperationContext,
): EnterpriseOperationContext {
  return { ...context };
}

function enterpriseOperationError(message: string): Error {
  const error = new Error(message);
  error.name = "AbortError";
  return error;
}

function assertEnterpriseOperationCurrent(
  context: EnterpriseOperationContext,
  isCurrent: EnterpriseCurrentContext,
) {
  if (context.signal.aborted) {
    throw enterpriseOperationError("aborted enterprise settings operation");
  }
  if (!isCurrent(context)) {
    throw enterpriseOperationError("obsolete enterprise settings operation");
  }
}

export async function runEnterpriseOperation<T>(
  context: EnterpriseOperationContext,
  isCurrent: EnterpriseCurrentContext,
  operation: (context: EnterpriseOperationContext) => Promise<T>,
): Promise<T> {
  assertEnterpriseOperationCurrent(context, isCurrent);
  const result = await operation(context);
  assertEnterpriseOperationCurrent(context, isCurrent);
  return result;
}

export function enterpriseSplitLines(value: string): string[] {
  return Array.from(new Set(
    value.split(/[\n,]/u).map((entry) => entry.trim()).filter(Boolean),
  ));
}

export function enterpriseDownloadText(download: EnterpriseDownload<unknown>) {
  return triggerBrowserFileDownload(new Blob([download.content], { type: download.contentType }), download.filename, { revokeAfterMs: 60_000 });
}

/** The organization module's part of the enterprise controls' changes. */
export type EnterpriseModuleService = Omit<EnterpriseSettingsService, "updateSharingSecurity" | "writeClipboard">;
