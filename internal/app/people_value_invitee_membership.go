package app

// An address the workspace invited to become a member, its invitation still
// waiting, is a member to be and not a guest wherever the "@" menu and the
// person pickers offer it (people_value_invitees.go): marked "제한됨" when
// invited as a restricted member and nothing otherwise, as Notion's "@" marks
// only guests and restricted members (seen 2026-11-07, 2026-11-11). An address
// only a page is shared with stays a guest, as Notion's "@" marks a guest
// invited by an address (seen 2026-11-12).

// pendingMembershipInvitationSQL is an EXISTS that holds while the workspace
// workspaceExpression has a waiting membership invitation for the address
// emailExpression (lower-cased and trimmed); restrictedOnly keeps to one that
// invites a restricted member.
func pendingMembershipInvitationSQL(workspaceExpression, emailExpression string, restrictedOnly bool) string {
	restricted := ""
	if restrictedOnly {
		restricted = " AND [[membership_invitation]].[[restricted]] = 1"
	}
	return "EXISTS (SELECT 1 FROM {{leafen_workspace_invitations}} [[membership_invitation]]" +
		" WHERE [[membership_invitation]].[[workspace_id]] = " + workspaceExpression +
		" AND [[membership_invitation]].[[email]] = " + emailExpression +
		" AND [[membership_invitation]].[[status]] = 'pending'" + restricted + ")"
}

// inviteeMembershipInvitationSQL is pendingMembershipInvitationSQL for the
// workspace's invitee whose ID idExpression names (peopleValueInviteeID).
func inviteeMembershipInvitationSQL(workspaceExpression, idExpression string, restrictedOnly bool) string {
	return "EXISTS (SELECT 1 FROM {{leafen_people_value_invitees}} [[membership_invitee]]" +
		" WHERE [[membership_invitee]].[[workspace_id]] = " + workspaceExpression +
		" AND [[membership_invitee]].[[id]] = " + idExpression +
		" AND " + pendingMembershipInvitationSQL(workspaceExpression, "[[membership_invitee]].[[email]]", restrictedOnly) + ")"
}
