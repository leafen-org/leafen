package app

import (
	"errors"

	"github.com/pocketbase/pocketbase/core"
)

// An owner's "멤버 추가하기" brings either role the organization module
// gives, as Notion's role card under "역할 선택" does (user decision
// 2026-10-30: its menu holds 멤버 and 제한된 멤버, no owner): someone with an
// account here joins restricted at once, and an invitation keeps the role
// until its address joins (workspace_member_invitations.go). Without the
// module nobody is restricted, so an invitation asks for the role only where
// the module gives it (OrganizationPolicy.RestrictedInvitations), and one
// left from a server that had it brings a member.

var errWorkspaceRestrictedInvitationUnavailable = errors.New("restricted invitations need the organization module")

// workspaceRestrictedInvitationAllowed refuses a restricted role where no
// module gives it.
func workspaceRestrictedInvitationAllowed(restricted bool) error {
	if restricted && !organizationPolicy.RestrictedInvitations {
		return errWorkspaceRestrictedInvitationUnavailable
	}
	return nil
}

// restrictJoinedWorkspaceMember makes someone who has just joined as a
// member restricted, as the module's role change does, its audit event
// included, and gives the workspace's permission cursor after it.
func restrictJoinedWorkspaceMember(txApp core.App, workspaceID, organizationID, userID, actorID string, now int64) (int, error) {
	changed, err := setWorkspaceMemberRestricted(txApp, workspaceID, userID, actorID, true, now)
	if err != nil {
		return 0, err
	}
	if changed {
		if err := recordOrganizationAuditEvent(txApp, organizationAuditEvent{
			ID: core.GenerateDefaultRandomId(), OrganizationID: organizationID, WorkspaceID: workspaceID, ActorID: actorID,
			Action: "workspace.member.restricted", TargetType: "user", TargetID: userID, OccurredAt: now,
		}); err != nil {
			return 0, err
		}
	}
	return currentWorkspacePermissionCursor(txApp, workspaceID)
}
