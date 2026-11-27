package app

// People keeps someone who stops being a workspace's guest, as Notion's
// People does (seen 2026-11-15 twice, a free test workspace: a guest
// removed from the workspace stays a row, its Membership Type
// "Deactivated", left out by the views' filter and shown when it is
// lifted; shared a page again, the same row is a guest again). Their kept
// row (leafen_people_guests) turns 'deactivated' instead of going
// (readPeopleGuestsAgain), and People lists them (peopleFormerPeopleSQL).
// A member who leaves still goes unless they stay as a guest: Notion's
// help says People holds former members too, but leaving a member was not
// seen there.

// peopleMembershipKeptDeactivated is a kept row of someone no longer the
// workspace's guest.
const peopleMembershipKeptDeactivated = "deactivated"

// peopleFormerPeopleSQL selects, as People's rows, the people the workspace
// bound as workspaceId keeps as former guests: not its owner, a member nor
// a guest again — the look turns them back before People reads them, and
// a write meanwhile is read here as it stands. Its columns are those of
// readPeopleDirectoryList's rows, each deactivated.
func peopleFormerPeopleSQL(shown string) string {
	return `
	SELECT [[user]].[[id]], [[user]].[[name]], [[user]].[[profile_avatar]], 1, 0
	FROM {{users}} [[user]]
	WHERE [[user]].[[id]] IN (SELECT [[former]].[[user_id]] FROM {{leafen_people_guests}} [[former]]
			WHERE [[former]].[[workspace_id]] = {:workspaceId} AND [[former]].[[membership]] = '` + peopleMembershipKeptDeactivated + `')
		AND [[user]].[[id]] NOT IN (SELECT [[workspace_owner]] FROM {{leafen_workspaces}} WHERE [[id]] = {:workspaceId}
			UNION SELECT [[user_id]] FROM {{leafen_workspace_memberships}} WHERE [[workspace_id]] = {:workspaceId})
		AND [[user]].[[id]] NOT IN (SELECT [[account_id]] FROM [[guest_share]] WHERE [[account_id]] IS NOT NULL)` + shown
}
