import { request, requestBinaryResponse } from "@/apiTransport";
import type { Session } from "@/types";
import {
  enterpriseAuditRetentionOperations,
  type AuditRetentionPolicy,
  type AuditRetentionUpdateInput,
} from "@/enterpriseAuditRetentionWire.generated";
import type {
  DLPPolicy,
  DLPPolicyInput,
} from "@/enterpriseDLPWire.generated";
import {
  enterpriseLegalHoldOperations,
  type LegalHold,
  type LegalHoldCreateInput,
  type LegalHoldList,
  type LegalHoldReleaseInput,
} from "@/enterpriseLegalHoldWire.generated";
import {
  enterpriseMCPGovernanceOperations,
  type MCPGovernanceApprovedClientInput,
  type MCPGovernancePolicy,
  type MCPGovernancePolicyInput,
  type MCPGovernanceRenameClientInput,
} from "@/enterpriseMCPGovernanceWire.generated";
import {
  enterpriseSAMLOperations,
  type SAMLConfigurationInput,
  type SAMLProvider,
} from "@/enterpriseSAMLWire.generated";
import type {
  SCIMConfiguration,
  SCIMConfigurationInput,
  SCIMTokenCreateInput,
  SCIMTokenCreation,
} from "@/enterpriseSCIMWire.generated";
import {
  organizationAuditOperations,
  type OrganizationAuditExportContent,
  type OrganizationAuditExportCreateInput,
  type OrganizationAuditExportList,
} from "@/organizationAuditWire.generated";

type JsonRequest = <T>(
  path: string,
  init?: RequestInit,
  session?: Session | null,
) => Promise<T>;

type ResponseRequest = (
  path: string,
  session: Session,
  init?: RequestInit,
) => Promise<Response>;

const organizationPath = (template: string, organizationId: string) => template.replace(
  "{organizationId}",
  encodeURIComponent(organizationId),
);

function scopedPath(template: string, values: Record<string, string>) {
  return Object.entries(values).reduce(
    (path, [name, value]) => path.replace(`{${name}}`, encodeURIComponent(value)),
    template,
  );
}

function pagePath(path: string, cursor?: string) {
  return cursor ? `${path}?cursor=${encodeURIComponent(cursor)}` : path;
}

function createEnterpriseModuleApi(
  request: JsonRequest,
  requestBinaryResponse: ResponseRequest,
) {
  const scimPath = (organizationId: string) => `/organizations/${encodeURIComponent(organizationId)}/scim`;
  const dlpPath = (organizationId: string) => `/organizations/${encodeURIComponent(organizationId)}/dlp`;

  return {
    updateSAMLConfiguration: (
      session: Session,
      organizationId: string,
      input: SAMLConfigurationInput,
      signal?: AbortSignal,
    ) => request<SAMLProvider>(
      organizationPath(enterpriseSAMLOperations.managementSnapshot.path, organizationId),
      { method: "PUT", body: JSON.stringify(input), signal },
      session,
    ),
    // After the server's address changed: whether the identity provider has
    // the new values (enterprise_saml_address.go).
    setSAMLServiceReturn: (session: Session, organizationId: string, added: boolean, signal?: AbortSignal) => request<unknown>(
      `${organizationPath(enterpriseSAMLOperations.managementSnapshot.path, organizationId)}/return`,
      { method: "POST", body: JSON.stringify({ added }), signal },
      session,
    ),
    readSCIMConfiguration: (session: Session, organizationId: string, signal?: AbortSignal) =>
      request<SCIMConfiguration>(scimPath(organizationId), { signal }, session),
    updateSCIMConfiguration: (
      session: Session,
      organizationId: string,
      input: SCIMConfigurationInput,
      signal?: AbortSignal,
    ) => request<SCIMConfiguration>(
      scimPath(organizationId),
      { method: "PUT", body: JSON.stringify(input), signal },
      session,
    ),
    createSCIMToken: (
      session: Session,
      organizationId: string,
      input: SCIMTokenCreateInput,
      signal?: AbortSignal,
    ) => request<SCIMTokenCreation>(
      `${scimPath(organizationId)}/tokens`,
      { method: "POST", body: JSON.stringify(input), signal },
      session,
    ),
    revokeSCIMToken: (
      session: Session,
      organizationId: string,
      tokenId: string,
      signal?: AbortSignal,
    ) => request<void>(
      `${scimPath(organizationId)}/tokens/${encodeURIComponent(tokenId)}`,
      { method: "DELETE", signal },
      session,
    ),
    readMCPGovernancePolicy: (
      session: Session,
      organizationId: string,
      workspaceId: string,
      signal?: AbortSignal,
    ) => request<MCPGovernancePolicy>(scopedPath(
      enterpriseMCPGovernanceOperations.readPolicy.path,
      { organizationId, workspaceId },
    ), { signal }, session),
    updateMCPGovernancePolicy: (
      session: Session,
      organizationId: string,
      workspaceId: string,
      input: MCPGovernancePolicyInput,
      signal?: AbortSignal,
    ) => request<MCPGovernancePolicy>(scopedPath(
      enterpriseMCPGovernanceOperations.updatePolicy.path,
      { organizationId, workspaceId },
    ), {
      method: enterpriseMCPGovernanceOperations.updatePolicy.method,
      body: JSON.stringify(input),
      signal,
    }, session),
    approveMCPGovernanceClient: (
      session: Session,
      organizationId: string,
      workspaceId: string,
      input: MCPGovernanceApprovedClientInput,
      signal?: AbortSignal,
    ) => request<MCPGovernancePolicy>(scopedPath(
      enterpriseMCPGovernanceOperations.approveClient.path,
      { organizationId, workspaceId },
    ), {
      method: enterpriseMCPGovernanceOperations.approveClient.method,
      body: JSON.stringify(input),
      signal,
    }, session),
    renameMCPGovernanceClient: (
      session: Session,
      organizationId: string,
      workspaceId: string,
      clientId: string,
      input: MCPGovernanceRenameClientInput,
      signal?: AbortSignal,
    ) => request<MCPGovernancePolicy>(scopedPath(
      enterpriseMCPGovernanceOperations.renameClient.path,
      { organizationId, workspaceId, clientId },
    ), {
      method: enterpriseMCPGovernanceOperations.renameClient.method,
      body: JSON.stringify(input),
      signal,
    }, session),
    removeMCPGovernanceClient: (
      session: Session,
      organizationId: string,
      workspaceId: string,
      clientId: string,
      signal?: AbortSignal,
    ) => request<MCPGovernancePolicy>(scopedPath(
      enterpriseMCPGovernanceOperations.removeClient.path,
      { organizationId, workspaceId, clientId },
    ), { method: enterpriseMCPGovernanceOperations.removeClient.method, signal }, session),
    readDLPPolicy: (session: Session, organizationId: string, signal?: AbortSignal) =>
      request<DLPPolicy>(dlpPath(organizationId), { signal }, session),
    updateDLPPolicy: (
      session: Session,
      organizationId: string,
      input: DLPPolicyInput,
      signal?: AbortSignal,
    ) => request<DLPPolicy>(
      dlpPath(organizationId),
      { method: "PUT", body: JSON.stringify(input), signal },
      session,
    ),
    readAuditRetention: (session: Session, organizationId: string, signal?: AbortSignal) =>
      request<AuditRetentionPolicy>(organizationPath(
        enterpriseAuditRetentionOperations.get.path,
        organizationId,
      ), { signal }, session),
    updateAuditRetention: (
      session: Session,
      organizationId: string,
      input: AuditRetentionUpdateInput,
      signal?: AbortSignal,
    ) => request<AuditRetentionPolicy>(organizationPath(
      enterpriseAuditRetentionOperations.update.path,
      organizationId,
    ), {
      method: enterpriseAuditRetentionOperations.update.method,
      body: JSON.stringify(input),
      signal,
    }, session),
    listOrganizationLegalHolds: (
      session: Session,
      organizationId: string,
      cursor?: string,
      signal?: AbortSignal,
    ) => request<LegalHoldList>(pagePath(organizationPath(
      enterpriseLegalHoldOperations.list.path,
      organizationId,
    ), cursor), { signal }, session),
    createOrganizationLegalHold: (
      session: Session,
      organizationId: string,
      input: LegalHoldCreateInput,
      signal?: AbortSignal,
    ) => request<LegalHold>(organizationPath(
      enterpriseLegalHoldOperations.create.path,
      organizationId,
    ), {
      method: enterpriseLegalHoldOperations.create.method,
      body: JSON.stringify(input),
      signal,
    }, session),
    releaseOrganizationLegalHold: (
      session: Session,
      organizationId: string,
      legalHoldId: string,
      input: LegalHoldReleaseInput,
      signal?: AbortSignal,
    ) => request<LegalHold>(scopedPath(
      enterpriseLegalHoldOperations.release.path,
      { organizationId, legalHoldId },
    ), {
      method: enterpriseLegalHoldOperations.release.method,
      body: JSON.stringify(input),
      signal,
    }, session),
    listOrganizationAuditExports: (
      session: Session,
      organizationId: string,
      cursor?: string,
      signal?: AbortSignal,
    ) => request<OrganizationAuditExportList>(pagePath(organizationPath(
      organizationAuditOperations.listExports.path,
      organizationId,
    ), cursor), { signal }, session),
    startOrganizationAuditExport: (
      session: Session,
      organizationId: string,
      input: OrganizationAuditExportCreateInput,
      signal?: AbortSignal,
    ) => request<OrganizationAuditExportContent>(organizationPath(
      organizationAuditOperations.createExport.path,
      organizationId,
    ), {
      method: organizationAuditOperations.createExport.method,
      body: JSON.stringify(input),
      signal,
    }, session),
    readOrganizationAuditExport: (
      session: Session,
      organizationId: string,
      exportId: string,
      signal?: AbortSignal,
    ) => request<OrganizationAuditExportContent>(scopedPath(
      organizationAuditOperations.getExport.path,
      { organizationId, exportId },
    ), { signal }, session),
    downloadOrganizationAuditExport: (
      session: Session,
      organizationId: string,
      exportId: string,
      signal?: AbortSignal,
    ) => requestBinaryResponse(scopedPath(
      organizationAuditOperations.downloadExport.path,
      { organizationId, exportId },
    ), session, { signal }),
  };
}

/** The organization features' settings API. */
export const enterpriseApi = createEnterpriseModuleApi(request, requestBinaryResponse);
