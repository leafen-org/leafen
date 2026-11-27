package app

// privilegedTeamspaceSelfMembership says a workspace owner puts themselves in
// a teamspace they don't manage, as its member or as its owner — any
// teamspace, a closed or private one too, one they are a member of already —
// as Notion's Enterprise workspace owner does from 설정 › 팀스페이스 and a
// teamspace's members tab ("멤버로 참여", "소유자로 참여", "소유자 되기"; its
// code, 2026-11-18, and its help: "Workspace owners on the Enterprise Plan
// can see, join and make themselves owner of any teamspace"). Leafen's
// workspace has every Enterprise power (decision 2026-10-12). Managing the
// teamspace still follows its owners (managesTeamspace): owning it is how a
// workspace owner gets there. The screens ask first, as Notion's "권한이 필요한
// 작업입니다.", and the change's audit row records it like every teamspace
// change (executeTeamspaceMutation).
func privilegedTeamspaceSelfMembership(authority teamspaceAuthority, input teamspaceMutationInput) bool {
	return authority.Workspace.Role == workspaceRoleOwner && input.Action == teamspaceActionAddMember &&
		input.PrincipalType == "user" && input.PrincipalID == authority.Workspace.PrincipalID &&
		(input.MemberRole == teamspaceMemberRoleOwner || input.MemberRole == teamspaceMemberRoleMember)
}
