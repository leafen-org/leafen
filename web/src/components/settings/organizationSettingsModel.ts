import {
  organizationAdminRoles,
  organizationRoles,
  type OrganizationAdminRole,
  type OrganizationRole,
  type OrganizationSettingsMember,
} from "@/organizationWire.generated";

// A member's role is member or guest; the owner passes by transfer, and
// admin roles are given beside the role (WorkspaceOrganizationAdminRoles).
export const editableOrganizationRoles = [
  organizationRoles.member,
  organizationRoles.guest,
] as const;

// Notion's predefined admin roles, in its order (its guide "Enterprise
// workspace management", read 2026-10-04).
export const organizationAdminRoleOrder: readonly OrganizationAdminRole[] = [
  organizationAdminRoles.compliance,
  organizationAdminRoles.people,
  organizationAdminRoles.it,
  organizationAdminRoles.analytics,
  organizationAdminRoles.billing,
];

const adminRoleKeys: Record<OrganizationAdminRole, string> = {
  compliance: "adminRoleCompliance",
  people: "adminRolePeople",
  it: "adminRoleIT",
  analytics: "adminRoleAnalytics",
  billing: "adminRoleBilling",
};

export function organizationAdminRoleLabel(role: OrganizationAdminRole, t: (key: string) => string) {
  return t(adminRoleKeys[role]);
}

export function organizationAdminRoleDescription(role: OrganizationAdminRole, t: (key: string) => string) {
  return t(`${adminRoleKeys[role]}Desc`);
}

export function organizationMemberLabel(member: OrganizationSettingsMember, userId: string, t: (key: string, options?: Record<string, unknown>) => string) {
  const label = member.displayName || member.email || member.userId;
  return member.userId === userId ? t("organizationMemberYou", { name: label }) : label;
}

export function organizationRoleLabel(role: OrganizationRole, t: (key: string) => string) {
  return t({
    owner: "roleOwner",
    admin: "roleAdmin",
    security_admin: "roleSecurityAdmin",
    billing_admin: "roleBillingAdmin",
    member: "roleMember",
    guest: "roleGuest",
  }[role]);
}
