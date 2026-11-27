package app

import (
	"slices"

	"github.com/pocketbase/pocketbase/core"
)

// An organization owner gives members admin roles, each over one area of
// the organization's settings, as Notion's admin roles (its guide
// "Enterprise workspace management", read 2026-10-04): compliance (the audit
// log, content search and legal holds), people (members, guests and groups),
// IT (identity, security and API integrations), analytics (every workspace's
// analytics) and billing (storage and file usage). A member may hold
// several; an owner may do everything and a guest holds none. They add to
// the member's workspace roles and change no page's access.
//
// The organization module keeps the roles members are given
// (OrganizationPolicy.AdminRoles); without it members hold only the roles a
// former single role became.

// effectiveOrganizationAdminRoles are a member's admin roles. A membership
// kept with one of the single roles this server had before admin roles
// holds the roles that role became (organization_admin_roles_schema.go).
func effectiveOrganizationAdminRoles(role string, assigned []string) []string {
	roles := []string{}
	switch role {
	case organizationRoleMember:
	case organizationRoleAdmin:
		roles = append(roles, organizationAdminRolePeople)
	case organizationRoleSecurityAdmin:
		roles = append(roles, organizationAdminRoleIT, organizationAdminRoleCompliance)
	case organizationRoleBillingAdmin:
		roles = append(roles, organizationAdminRoleBilling)
	default:
		return []string{}
	}
	for _, admin := range assigned {
		if slices.Contains(organizationAdminRoles, admin) {
			roles = append(roles, admin)
		}
	}
	slices.Sort(roles)
	return slices.Compact(roles)
}

func validOrganizationAdminRole(role string) bool {
	return slices.Contains(organizationAdminRoles, role)
}

// organizationSettingsCapabilitiesFor is what the person may see and do in
// the organization's settings: only the parts their admin roles cover.
func organizationSettingsCapabilitiesFor(authorization organizationAuthorization) organizationSettingsCapabilities {
	return organizationSettingsCapabilities{
		CanReadSettings:       authorization.allows(permissionOrganizationAdmin),
		CanManagePeople:       authorization.allows(permissionOrganizationPeopleAdmin),
		CanManageSecurity:     authorization.allows(permissionOrganizationSecurityAdmin),
		CanManageBilling:      authorization.allows(permissionOrganizationBillingAdmin),
		CanViewAuditLog:       authorization.allows(permissionOrganizationAuditLog),
		CanSearchContent:      authorization.allows(permissionOrganizationContentSearch),
		CanManageLegalHolds:   authorization.allows(permissionOrganizationLegalHolds),
		CanManageIdentity:     authorization.allows(permissionOrganizationIdentityAdmin),
		CanManageIntegrations: authorization.allows(permissionOrganizationIntegrationsAdmin),
		CanViewAnalytics:      authorization.allows(permissionOrganizationAnalytics),
		CanManageRoles:        authorization.allows(permissionOrganizationOwner),
		CanTransferOwnership:  authorization.allows(permissionOrganizationOwner),
	}
}

// loadOrganizationAdminRoles reads the principal's admin roles in the
// organizations, by organization.
func loadOrganizationAdminRoles(app core.App, principalID string, organizationIDs []string) (map[string][]string, error) {
	if store := organizationPolicy.AdminRoles; store != nil {
		return store.Assigned(app, principalID, organizationIDs)
	}
	return map[string][]string{}, nil
}

// loadOrganizationMembersAdminRoles reads the admin roles of an
// organization's members, by user.
func loadOrganizationMembersAdminRoles(app core.App, organizationID string) (map[string][]string, error) {
	if store := organizationPolicy.AdminRoles; store != nil {
		return store.Members(app, organizationID)
	}
	return map[string][]string{}, nil
}

// addOrganizationAdminRole gives a member an admin role; without the module
// there are none to give.
func addOrganizationAdminRole(app core.App, organizationID, userID, role, assignedBy string) error {
	if store := organizationPolicy.AdminRoles; store != nil {
		return store.Add(app, organizationID, userID, role, assignedBy)
	}
	return nil
}

func removeOrganizationAdminRole(app core.App, organizationID, userID, role string) error {
	if store := organizationPolicy.AdminRoles; store != nil {
		return store.Remove(app, organizationID, userID, role)
	}
	return nil
}
