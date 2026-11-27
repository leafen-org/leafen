import type { Organization } from "@/organizationWire.generated";

/**
 * The enterprise settings areas a person's admin roles cover, as Notion's
 * admin roles (its guide "Enterprise workspace management", read
 * 2026-10-04): IT has identity, security and API integrations; compliance
 * the audit log, content search and legal holds; the Admin API's tokens are
 * the organization owner's alone. An owner has them all.
 */
export interface EnterpriseAreas {
  identity: boolean;
  security: boolean;
  integrations: boolean;
  auditLog: boolean;
  legalHolds: boolean;
  contentSearch: boolean;
  adminTokens: boolean;
}

export function enterpriseAreas(organization: Organization | null): EnterpriseAreas {
  const capabilities = organization?.capabilities;
  return {
    identity: capabilities?.canManageIdentity === true,
    security: capabilities?.canManageSecurity === true,
    integrations: capabilities?.canManageIntegrations === true,
    auditLog: capabilities?.canViewAuditLog === true,
    legalHolds: capabilities?.canManageLegalHolds === true,
    contentSearch: capabilities?.canSearchContent === true,
    adminTokens: capabilities?.canManageRoles === true,
  };
}

export function canViewEnterpriseSettings(organization: Organization | null): boolean {
  return Object.values(enterpriseAreas(organization)).some(Boolean);
}

export function canMutateOrganizationSharingSecurity(
  organization: Organization | null,
): boolean {
  return enterpriseAreas(organization).security;
}
