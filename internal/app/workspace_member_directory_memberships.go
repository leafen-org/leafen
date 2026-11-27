package app

import (
	"encoding/json"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// workspaceMemberDirectoryMembership is one teamspace or group of a listed
// person, with how many that person has in all.
type workspaceMemberDirectoryMembership struct {
	UserID string `db:"user_id"`
	ID     string `db:"id"`
	Name   string `db:"name"`
	Icon   string `db:"icon"`
	Total  int    `db:"total"`
	// How many people the teamspace or group has (Notion's "N명의 멤버",
	// "멤버 N명").
	Members int `db:"members"`
}

// withWorkspaceMemberDirectoryMemberships gives each listed person their
// teamspaces and groups, as Notion's member table shows them (seen
// 2026-10-09 and 2026-10-10): how many, and the first few by name, each
// teamspace and group with how many people it has (as its list says). A
// teamspace counts when the person is in it by name, by a group or as the
// workspace's default one (not a restricted member), and only when the
// reader may see it: the workspace's owner sees every live teamspace, a
// restricted member the ones they are in, anyone else the ones that are not
// private and the private ones they are in themselves.
func withWorkspaceMemberDirectoryMemberships(
	app core.App, workspaceID, readerID string, readerOwner bool, members []workspaceMemberDirectoryItem,
) error {
	if len(members) == 0 {
		return nil
	}
	ids := make([]string, 0, len(members))
	for _, member := range members {
		ids = append(ids, member.UserID)
	}
	listed, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	params := dbx.Params{
		"workspaceId": workspaceID, "reader": readerID, "readerOwner": readerOwner,
		"listed": string(listed), "kept": maxWorkspaceMemberDirectoryMemberships,
	}
	teamspaces := []workspaceMemberDirectoryMembership{}
	if err := app.DB().NewQuery(`
		WITH [[listed]] AS (SELECT [[value]] AS [[user_id]] FROM json_each({:listed})),
		[[default_teamspace]] AS (
			SELECT [[listed_default]].[[value]] AS [[id]]
			FROM {{leafen_teamspace_settings}} [[settings]], json_each([[settings]].[[default_teamspace_ids]]) [[listed_default]]
			WHERE [[settings]].[[workspace_id]]={:workspaceId}
		),
		[[in_teamspace]] AS (
			SELECT [[member]].[[teamspace_id]], [[member]].[[principal_id]] AS [[user_id]]
			FROM {{leafen_teamspace_members}} [[member]]
			WHERE [[member]].[[workspace_id]]={:workspaceId} AND [[member]].[[deleted_at]]=0
				AND [[member]].[[principal_type]]='user'
			UNION
			SELECT [[member]].[[teamspace_id]], [[group_member]].[[user_id]]
			FROM {{leafen_teamspace_members}} [[member]]
			JOIN {{leafen_workspace_permission_groups}} [[group]]
				ON [[group]].[[id]]=[[member]].[[principal_id]] AND [[group]].[[workspace_id]]={:workspaceId}
					AND [[group]].[[deleted_at]] IS NULL
			JOIN {{leafen_workspace_permission_group_members}} [[group_member]]
				ON [[group_member]].[[group_id]]=[[group]].[[id]]
			WHERE [[member]].[[workspace_id]]={:workspaceId} AND [[member]].[[deleted_at]]=0
				AND [[member]].[[principal_type]]='group'
		),
		[[visible]] AS (
			SELECT [[teamspace]].[[id]], [[teamspace]].[[name]], [[teamspace]].[[icon]]
			FROM {{leafen_teamspaces}} [[teamspace]]
			WHERE [[teamspace]].[[workspace_id]]={:workspaceId}
				AND [[teamspace]].[[archived_at]]=0 AND [[teamspace]].[[deleted_at]]=0
				AND ({:readerOwner} OR NOT ` + workspaceRestrictedMemberSQL("{:workspaceId}", "{:reader}") + `
						AND ([[teamspace]].[[access]]<>'private'
							OR [[teamspace]].[[id]] IN (SELECT [[id]] FROM [[default_teamspace]]))
					OR EXISTS (SELECT 1 FROM [[in_teamspace]]
						WHERE [[in_teamspace]].[[teamspace_id]]=[[teamspace]].[[id]]
							AND [[in_teamspace]].[[user_id]]={:reader}))
		),
		[[effective_members]]([[teamspace_id]], [[user_id]]) AS (` + teamspaceEffectiveMembersSQL() + `
		),
		[[counted]] AS (
			SELECT [[teamspace_id]], COUNT(*) AS [[members]] FROM [[effective_members]]
			WHERE [[teamspace_id]] IN (SELECT [[id]] FROM [[visible]])
				AND NOT ` + integrationAccountSQL("[[effective_members]].[[user_id]]") + `
			GROUP BY [[teamspace_id]]
		),
		[[held]] AS (
			SELECT [[listed]].[[user_id]], [[visible]].[[id]], [[visible]].[[name]], [[visible]].[[icon]],
				COALESCE((SELECT [[members]] FROM [[counted]] WHERE [[counted]].[[teamspace_id]]=[[visible]].[[id]]), 0) AS [[members]]
			FROM [[listed]], [[visible]]
			WHERE [[visible]].[[id]] IN (SELECT [[id]] FROM [[default_teamspace]])
					AND NOT ` + workspaceRestrictedMemberSQL("{:workspaceId}", "[[listed]].[[user_id]]") + `
				OR EXISTS (SELECT 1 FROM [[in_teamspace]]
					WHERE [[in_teamspace]].[[teamspace_id]]=[[visible]].[[id]]
						AND [[in_teamspace]].[[user_id]]=[[listed]].[[user_id]])
		)
		SELECT [[user_id]], [[id]], [[name]], [[icon]], [[members]], [[total]] FROM (
			SELECT [[user_id]], [[id]], [[name]], [[icon]], [[members]],
				COUNT(*) OVER (PARTITION BY [[user_id]]) AS [[total]],
				ROW_NUMBER() OVER (PARTITION BY [[user_id]] ORDER BY lower([[name]]), [[id]]) AS [[ordinal]]
			FROM [[held]]
		) WHERE [[ordinal]] <= {:kept}
		ORDER BY [[user_id]], [[ordinal]]
	`).Bind(params).All(&teamspaces); err != nil {
		return err
	}
	groups := []workspaceMemberDirectoryMembership{}
	if err := app.DB().NewQuery(`
		SELECT [[user_id]], [[id]], [[name]], [[icon]], [[members]], [[total]] FROM (
			SELECT [[group_member]].[[user_id]], [[group]].[[id]], [[group]].[[name]], [[group]].[[icon]],
				(SELECT COUNT(*) FROM {{leafen_workspace_permission_group_members}} [[counted]]
					WHERE [[counted]].[[group_id]]=[[group]].[[id]]) AS [[members]],
				COUNT(*) OVER (PARTITION BY [[group_member]].[[user_id]]) AS [[total]],
				ROW_NUMBER() OVER (PARTITION BY [[group_member]].[[user_id]]
					ORDER BY [[group]].[[normalized_name]], [[group]].[[id]]) AS [[ordinal]]
			FROM {{leafen_workspace_permission_group_members}} [[group_member]]
			JOIN {{leafen_workspace_permission_groups}} [[group]] ON [[group]].[[id]]=[[group_member]].[[group_id]]
			WHERE [[group]].[[workspace_id]]={:workspaceId} AND [[group]].[[deleted_at]] IS NULL
				AND [[group_member]].[[user_id]] IN (SELECT [[value]] FROM json_each({:listed}))
		) WHERE [[ordinal]] <= {:kept}
		ORDER BY [[user_id]], [[ordinal]]
	`).Bind(params).All(&groups); err != nil {
		return err
	}
	index := make(map[string]*workspaceMemberDirectoryItem, len(members))
	for position := range members {
		member := &members[position]
		member.Teamspaces, member.Groups = []workspaceMemberTeamspace{}, []workspaceMemberGroup{}
		index[member.UserID] = member
	}
	for _, held := range teamspaces {
		if member := index[held.UserID]; member != nil {
			member.TeamspaceCount = held.Total
			member.Teamspaces = append(member.Teamspaces, workspaceMemberTeamspace{
				ID: held.ID, Name: held.Name, Icon: held.Icon, MemberCount: held.Members,
			})
		}
	}
	for _, held := range groups {
		if member := index[held.UserID]; member != nil {
			member.GroupCount = held.Total
			member.Groups = append(member.Groups, workspaceMemberGroup{ID: held.ID, Name: held.Name, Icon: held.Icon, MemberCount: held.Members})
		}
	}
	return nil
}
