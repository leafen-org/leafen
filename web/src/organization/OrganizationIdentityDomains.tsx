import type { OrganizationIdentityDomainsProps } from "@/lib/organizationModule";
import { IdentityWorkspaceCreation } from "./IdentityWorkspaceCreation";
import { WorkspaceOrganizationDomains } from "./WorkspaceOrganizationDomains";

/**
 * Notion's "신원" domains (seen 2026-10-02): the organization's verified
 * domains and who may create workspaces with them.
 */
export function OrganizationIdentityDomains({ organization, disabled, mutate }: OrganizationIdentityDomainsProps) {
  return (
    <>
      <WorkspaceOrganizationDomains
        domains={organization.domains}
        canManage={organization.organization.capabilities.canManageIdentity}
        disabled={disabled}
        mutate={mutate}
      />
      <IdentityWorkspaceCreation organization={organization.organization} disabled={disabled} mutate={mutate} />
    </>
  );
}
