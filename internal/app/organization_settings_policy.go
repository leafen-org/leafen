package app

import "errors"

var errOrganizationSettingsInvalid = errors.New(errorCodeOrganizationSettingsInvalid)

func organizationSettingsRequirement(action string) (organizationPermissionRequirement, error) {
	switch action {
	case organizationSettingsActionGroupCreate,
		organizationSettingsActionGroupUpdate,
		organizationSettingsActionGroupDelete,
		organizationSettingsActionGroupMemberAdd,
		organizationSettingsActionGroupMemberRemove,
		organizationSettingsActionMemberDeactivate,
		organizationSettingsActionMemberReactivate,
		organizationSettingsActionMemberRemove:
		return permissionOrganizationPeopleAdmin, nil
	// Workspace creation is Identity's, beside the verified domains it
	// applies to (organization_workspace_creation.go).
	case organizationSettingsActionWorkspaceCreationPolicy,
		organizationSettingsActionDomainAdd,
		organizationSettingsActionDomainVerify,
		organizationSettingsActionDomainRemove:
		return permissionOrganizationIdentityAdmin, nil
	// How long the updates feed is kept is the audit log's people's; page
	// analytics', its analytics people's (activity_retention.go).
	case organizationSettingsActionFeedRetention:
		return permissionOrganizationAuditLog, nil
	case organizationSettingsActionAnalyticsRetention:
		return permissionOrganizationAnalytics, nil
	case organizationSettingsActionMemberRole,
		organizationSettingsActionOwnerTransfer,
		organizationSettingsActionAdminRoleAdd,
		organizationSettingsActionAdminRoleRemove:
		return permissionOrganizationOwner, nil
	default:
		return permissionOrganizationActive, errOrganizationSettingsInvalid
	}
}

// organizationSettingsCanManageTarget: the owner manages everyone else; a
// people admin manages members without admin roles, and guests.
func organizationSettingsCanManageTarget(
	authorization organizationAuthorization, targetRole string, targetAdminRoles []string,
) bool {
	if authorization.Role == organizationRoleOwner {
		return targetRole != organizationRoleOwner
	}
	return authorization.allows(permissionOrganizationPeopleAdmin) &&
		((targetRole == organizationRoleMember && len(targetAdminRoles) == 0) || targetRole == organizationRoleGuest)
}

func applyOrganizationSettingsMemberCapabilities(
	member *organizationSettingsMember, authorization organizationAuthorization,
) {
	isSelf := member.UserID == authorization.PrincipalID
	isOwner := member.UserID == authorization.OwnerID || member.Role == organizationRoleOwner
	member.CanManageLifecycle = !isSelf && !isOwner &&
		organizationSettingsCanManageTarget(authorization, member.Role, member.AdminRoles)
	member.CanManageRole = !isSelf && !isOwner && member.Status == organizationMemberStatusActive &&
		authorization.Role == organizationRoleOwner
	// Admin roles are the organization module's to give.
	member.CanManageAdminRoles = member.CanManageRole && member.Role == organizationRoleMember &&
		organizationPolicy.AdminRoles != nil
	member.CanTransferOwnership = member.CanManageRole
}
