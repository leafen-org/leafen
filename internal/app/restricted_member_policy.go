package app

import (
	"github.com/pocketbase/pocketbase/core"
)

// Who may add a workspace's restricted members to teamspaces and pages, as
// Notion's Security → Members & guests rows (seen 2026-10-12). A teamspace's
// members are added by the workspace's owners; or by them and the
// teamspace's owners; or by anyone who may add members to it. A page is
// shared with a restricted member by anyone who may share it; or, under
// "관리자 승인을 받은 모든 멤버", by an owner, the others asking an owner as
// they ask to invite a guest (requestGuestInvite); or by the owners only.

const (
	restrictedAddsOwners          = "owners"
	restrictedAddsTeamspaceOwners = "teamspace_owners"
	restrictedAddsApproval        = "approval"
	restrictedAddsMembers         = "members"
)

// enforceRestrictedMemberShare refuses a share with a restricted member the
// workspace's policy keeps from the person sharing, or asks for an owner's
// approval (errGuestInviteRequiresApproval) where the policy does.
func enforceRestrictedMemberShare(app core.App, authorization pageAuthorization, input publicMutationInput) error {
	if input.Action != publicMutationActionGrantPermission {
		return nil
	}
	if authorization.Workspace.Role == workspaceRoleOwner ||
		input.PrincipalType != publicMutationPrincipalUser && input.PrincipalType != publicMutationPrincipalGroup {
		return nil
	}
	// A page reached through a share does not carry the person's standing in
	// the workspace, so it is read here.
	actorRestricted, err := workspaceMemberRestricted(app, authorization.Workspace.WorkspaceID, authorization.Workspace.PrincipalID)
	if err != nil {
		return err
	}
	// Nor does a restricted member share with a group, which they do not see.
	if input.PrincipalType == publicMutationPrincipalGroup {
		if actorRestricted {
			return errOrganizationSharingSecurityBlocked
		}
		return nil
	}
	return enforceRestrictedUserShare(app, authorization.Workspace.Organization.OrganizationID, authorization.Workspace.WorkspaceID,
		authorization.Workspace.PrincipalID, false, actorRestricted, input.PrincipalID)
}

// enforceRestrictedUserShare is the same rule for any share with a person,
// a database row's page among them.
func enforceRestrictedUserShare(app core.App, organizationID, workspaceID, actorID string, actorOwner, actorRestricted bool, granteeID string) error {
	if actorOwner {
		return nil
	}
	// A restricted member shares only with the people they share a place
	// with (restricted_member_people.go).
	if actorRestricted {
		sees, err := restrictedReaderSees(app, actorID, workspaceID, granteeID)
		if err != nil {
			return err
		}
		if !sees {
			return errOrganizationSharingSecurityBlocked
		}
	}
	restricted, err := workspaceMemberRestricted(app, workspaceID, granteeID)
	if err != nil || !restricted {
		return err
	}
	policy, err := loadEnforcedSharingSecurityPolicy(app, organizationID)
	if err != nil {
		return err
	}
	switch policy.RestrictedPageAdds {
	case restrictedAddsOwners:
		return errOrganizationSharingSecurityBlocked
	case restrictedAddsApproval:
		return errGuestInviteRequiresApproval
	}
	return nil
}

// restrictedShareNeedsApproval is true of a share with a restricted member
// that, under the approval policy, the person must ask an owner for.
func restrictedShareNeedsApproval(app core.App, authorization pageAuthorization, principalType, principalID string) (bool, error) {
	if principalType != publicMutationPrincipalUser || authorization.Workspace.Role == workspaceRoleOwner {
		return false, nil
	}
	restricted, err := workspaceMemberRestricted(app, authorization.Workspace.WorkspaceID, principalID)
	if err != nil || !restricted {
		return false, err
	}
	policy, err := loadEnforcedSharingSecurityPolicy(app, authorization.Workspace.Organization.OrganizationID)
	return err == nil && policy.RestrictedPageAdds == restrictedAddsApproval, err
}

// enforceRestrictedMemberTeamspaceAdd refuses adding a restricted member to a
// teamspace where the workspace's policy keeps it from the person adding.
func enforceRestrictedMemberTeamspaceAdd(app core.App, authority teamspaceAuthority, teamspaceOwner bool, principalType, principalID string) error {
	// The one adding is no restricted member, who adds no one
	// (decision.MembersLocked); a group is no restricted member either.
	if authority.Workspace.Role == workspaceRoleOwner || principalType != "user" {
		return nil
	}
	restricted, err := workspaceMemberRestricted(app, authority.Workspace.WorkspaceID, principalID)
	if err != nil || !restricted {
		return err
	}
	policy, err := loadEnforcedSharingSecurityPolicy(app, authority.Workspace.Organization.OrganizationID)
	if err != nil {
		return err
	}
	if policy.RestrictedTeamspaceAdds == restrictedAddsOwners ||
		policy.RestrictedTeamspaceAdds == restrictedAddsTeamspaceOwners && !teamspaceOwner {
		return errOrganizationSharingSecurityBlocked
	}
	return nil
}
