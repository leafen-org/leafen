// The organization module's screens attach to the core before the app
// starts; the release build loads this entry ahead of the app's.
import { attachOrganizationModule } from "@/lib/organizationModule";
import { IdentityManagedUsers } from "./IdentityManagedUsers";
import { OrganizationAdminRoles } from "./OrganizationAdminRoles";
import { OrganizationGuestMFA } from "./OrganizationGuestMFA";
import { OrganizationSignInPolicy } from "./OrganizationSignInPolicy";
import { EnterpriseSettingsPanel } from "./EnterpriseSettingsPanel";
import { createEnterpriseModuleService, loadEnterpriseModuleSnapshot } from "./enterpriseSettings";
import { OrganizationIdentityDomains } from "./OrganizationIdentityDomains";
import { PropertyAccessDialog } from "./PropertyAccessDialog";
import { PropertyAccessShareRules } from "./PropertyAccessShareRules";
import { restrictWorkspaceMember } from "./restrictedMembers";
import { runOrganizationAuditExport } from "./organizationAuditTransport";

attachOrganizationModule({
  name: "organization",
  AdminRoles: OrganizationAdminRoles,
  IdentityDomains: OrganizationIdentityDomains,
  ManagedUsers: IdentityManagedUsers,
  GuestMFA: OrganizationGuestMFA,
  SignInPolicy: OrganizationSignInPolicy,
  sharingBlocks: true,
  restrictMember: restrictWorkspaceMember,
  PropertyAccessDialog,
  PropertyAccessShareRules,
  enterprise: {
    loadSnapshot: loadEnterpriseModuleSnapshot,
    createService: createEnterpriseModuleService,
    Panel: EnterpriseSettingsPanel,
  },
  exportAuditLog: runOrganizationAuditExport,
});
