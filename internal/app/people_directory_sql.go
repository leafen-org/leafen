package app

// peopleDirectorySQL reads everyone People lists in a workspace
// ({:workspaceId}, its owner {:ownerId}) — its owner, members, guests with an
// account and the deactivated, suspended or former guests — with their name,
// picture, email, About and membership, narrowed by shown; its caller
// orders and windows it. People's list reads it (readPeopleDirectoryList),
// and so does its database's rows (syncPeopleDatabase).
func peopleDirectorySQL(shown string) string {
	return workspaceGuestSharesSQL + `
		SELECT [[person]].[[user_id]], [[person]].[[display_name]], [[person]].[[avatar]], [[user]].[[email]],
			COALESCE([[profile]].[[about]], '') AS [[about]],
			CASE WHEN [[person]].[[suspended]] != 0 THEN 'deactivated'
				WHEN [[person]].[[guest]] = 1 THEN 'guest'
				WHEN [[person]].[[user_id]] = {:ownerId} THEN 'owner'
				WHEN ` + workspaceRestrictedMemberSQL("{:workspaceId}", "[[person]].[[user_id]]") + ` THEN 'restricted'
				ELSE 'member' END AS [[membership]]
		FROM (
			-- Each side names its columns, so a column the members' list gains
			-- (2026-11-05: when each came) can't leave the two sides apart.
			SELECT [[member]].[[user_id]], [[member]].[[display_name]], [[member]].[[avatar]], [[member]].[[suspended]], 0 AS [[guest]]
			FROM (` + workspacePeopleSQL() + shown + `) [[member]]
			UNION ALL
			SELECT [[user]].[[id]], [[user]].[[name]], [[user]].[[profile_avatar]], ` + organizationSuspendedSQL("[[user]].[[id]]") + `, 1
			FROM {{users}} [[user]]
			WHERE [[user]].[[id]] IN (SELECT [[account_id]] FROM [[guest_share]] WHERE [[account_id]] IS NOT NULL)` + shown + `
			UNION ALL` + peopleFormerPeopleSQL(shown) + `
		) [[person]]
		JOIN {{users}} [[user]] ON [[user]].[[id]] = [[person]].[[user_id]]
		LEFT JOIN {{leafen_workspace_person_profiles}} [[profile]]
			ON [[profile]].[[workspace_id]] = {:workspaceId} AND [[profile]].[[user_id]] = [[person]].[[user_id]]
`
}
