package app

// What People kept of each person's row before it was a database (migration
// 796): its number, when they joined and who last changed one of its values.
// Migration 797 moves them to the rows of the workspace's People database
// (people_database_moves.go). When a person joined — the order its rows are
// made in, as Notion's ID numbers People in the order they joined (seen
// 2026-11-15) — is still read from the workspace (peopleJoinedSQL).

// peopleJoinedSQL is everyone People lists in a workspace, whoever reads it,
// with when they joined it: its owner when it was made, a member when they
// joined, a guest when first shared with; someone kept as deactivated after
// leaving joined before anyone numbered, unknown (0).
func peopleJoinedSQL() string {
	return workspaceGuestSharesSQL + `
	SELECT [[person]].[[user_id]], MIN([[person]].[[joined_at]]) AS [[joined_at]] FROM (
		SELECT [[workspace_owner]] AS [[user_id]], MAX(0, CAST((julianday([[created]]) - 2440587.5) * 86400000 AS INTEGER)) AS [[joined_at]]
		FROM {{leafen_workspaces}} WHERE [[id]] = {:workspaceId}
		UNION ALL SELECT [[user_id]], [[created]] FROM {{leafen_workspace_memberships}} WHERE [[workspace_id]] = {:workspaceId}
		UNION ALL SELECT [[account_id]], [[created_at]] FROM [[guest_share]] WHERE [[account_id]] IS NOT NULL
		UNION ALL SELECT [[user_id]], 0 FROM {{leafen_people_guests}}
			WHERE [[workspace_id]] = {:workspaceId} AND [[membership]] = '` + peopleMembershipKeptDeactivated + `'
	) [[person]]
	WHERE [[person]].[[user_id]] != '' AND NOT ` + integrationAccountSQL("[[person]].[[user_id]]") + `
	GROUP BY [[person]].[[user_id]]`
}
