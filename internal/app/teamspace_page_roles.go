package app

import "github.com/pocketbase/pocketbase/core"

// teamspaceKeptRefusal is why a teamspace made or changed can't be kept as
// asked, past the request's own shape (teamspaceSettingsRefusal), or nil: an
// icon showing a file that isn't one of the workspace's pictures, an open
// teamspace the rest of the workspace has no role in, or page roles that
// give its members less than the rest of the workspace. kept is the
// teamspace as it is, nil for a new one.
func teamspaceKeptRefusal(
	app core.App, authority teamspaceAuthority, kept *storedTeamspace, input teamspaceMutationInput, outsideRole string, outside bool,
) error {
	if (kept == nil || input.Icon != kept.Icon) && !checkHeldIconFile(app, authority.Workspace.WorkspaceID, input.Icon) {
		return errTeamspaceIconNotHeld
	}
	// Anyone in the workspace may join an open teamspace, and Notion offers
	// the rest of the workspace no "사용 권한 없음" there, opening one they
	// have none in with the members' role (seen 2026-11-18 at 1440, two
	// tabs); an open one kept so from before stays, so its other settings
	// still change.
	if input.Access == teamspaceAccessOpen && !outside && (kept == nil || kept.Access != teamspaceAccessOpen || kept.OutsideAccess) {
		return teamspaceRefused("openPageRole is none: the rest of the workspace, who may join an open teamspace, has a role in its pages")
	}
	if !outside || pageShareRoleRank(input.MemberPageRole) >= pageShareRoleRank(outsideRole) {
		return nil
	}
	// A pair it keeps from before stays, so its other settings still change.
	if kept != nil && kept.MemberPageRole == input.MemberPageRole && teamspaceOutsideRole(*kept) == outsideRole {
		return nil
	}
	// A default teamspace has everyone as a member, and no one else.
	if kept != nil {
		settings, err := loadTeamspaceSettings(app, authority)
		if err != nil || settings.isDefault(kept.ID) {
			return err
		}
	}
	// Notion offers the rest of the workspace no role over the members' and
	// dims the members' roles under the rest's (seen 2026-11-18 at 1440 in
	// two tabs, and once by another hand).
	return teamspaceRefused("memberPageRole is %s, under the %s the rest of the workspace has: a teamspace's members have at least what anyone else has in its pages",
		input.MemberPageRole, outsideRole)
}

// teamspaceMembersInvite is whether a teamspace's members may invite and
// remove members: as its security tab says, and always in an open one, which
// anyone may join, as Notion's fixed "모든 팀스페이스 멤버" there (seen
// 2026-11-18 at 1440: open fixed, closed and private a choice, and opening
// one sets it so).
func teamspaceMembersInvite(value storedTeamspace) bool {
	return value.MembersCanInvite || value.Access == teamspaceAccessOpen
}
