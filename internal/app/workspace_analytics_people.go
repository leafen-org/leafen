package app

// workspaceAnalyticsPeopleSQL is who workspace analytics counts, one row each:
// the workspace's owner and members, guest 0, and its guests with an account,
// guest 1 — the people People lists as its guests (workspaceGuestSharesSQL):
// someone outside it with a page shared with them, active in its
// organization. Its members' card, the members active each day and the
// member list count those with guest 0, its guests' card those with guest 1.
// Versions that are Leafen's own (People's upkeep, a repeating template's or
// an automation's page, an anonymous form response) are no one's activity,
// and people who left aren't counted.
var workspaceAnalyticsPeopleSQL = workspaceGuestSharesSQL + `
	SELECT [[workspace_owner]] AS [[user_id]], '` + workspaceRoleOwner + `' AS [[role]], 0 AS [[guest]]
	FROM {{leafen_workspaces}} WHERE [[id]] = {:workspaceId}
	UNION
	SELECT [[user_id]], [[role]], 0 FROM {{leafen_workspace_memberships}}
	WHERE [[workspace_id]] = {:workspaceId}
		AND [[user_id]] NOT IN (SELECT [[workspace_owner]] FROM {{leafen_workspaces}} WHERE [[id]] = {:workspaceId})
	UNION
	SELECT [[account_id]], '` + workspaceRoleGuest + `', 1 FROM [[guest_share]] WHERE [[account_id]] IS NOT NULL`

// workspaceAnalyticsEditSQL reads the page versions of the workspace that are
// someone's edit from the moment the parameter since names: a discussion's
// mark in a page's text is a version its history lists as no edit
// (pageHistoryEditVersion), so making one alone doesn't make someone active.
// The cards, the members active each day and the member list's edits and
// last activity all read edits by it.
func workspaceAnalyticsEditSQL(since string) string {
	return `[[workspace_id]] = {:workspaceId} AND [[created_at]] >= {:` + since + `} AND [[actor_id]] != ''
		AND ` + pageHistoryEditVersion("")
}
