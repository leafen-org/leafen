package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const teamspaceOwnerLabelLimit = 3

type teamspaceMemberCountRow struct {
	TeamspaceID string `db:"teamspace_id"`
	MemberCount int    `db:"member_count"`
}

type teamspaceOwnerSummaryRow struct {
	TeamspaceID string `db:"teamspace_id"`
	Label       string `db:"label"`
	// The owner's user id, for their face; empty for a group.
	UserID     string `db:"user_id"`
	OwnerCount int    `db:"owner_count"`
}

func readTeamspaceSummaries(
	ctx context.Context,
	app core.App,
	workspaceID string,
	teamspaceIDs []string,
) ([]teamspaceSummaryWire, error) {
	if len(teamspaceIDs) == 0 {
		return []teamspaceSummaryWire{}, nil
	}
	params := dbx.Params{"workspaceId": workspaceID}
	placeholders := make([]string, 0, len(teamspaceIDs))
	for index, id := range teamspaceIDs {
		key := fmt.Sprintf("summaryTeamspace%d", index)
		params[key] = id
		placeholders = append(placeholders, "{:"+key+"}")
	}
	targets := strings.Join(placeholders, ",")

	memberRows := []teamspaceMemberCountRow{}
	if err := app.DB().NewQuery(`
		WITH [[effective_members]]([[teamspace_id]],[[user_id]]) AS (
			` + teamspaceEffectiveMembersSQL() + `
		)
		SELECT [[teamspace_id]],COUNT(*) AS [[member_count]]
		FROM [[effective_members]]
		WHERE [[teamspace_id]] IN (` + targets + `)
			AND NOT ` + integrationAccountSQL("[[effective_members]].[[user_id]]") + `
		GROUP BY [[teamspace_id]]
	`).WithContext(ctx).Bind(params).All(&memberRows); err != nil {
		return nil, err
	}

	ownerRows := []teamspaceOwnerSummaryRow{}
	if err := app.DB().NewQuery(`
		WITH [[owner_labels]] AS (
			SELECT [[member]].[[teamspace_id]],
				CASE [[member]].[[principal_type]]
					WHEN 'user' THEN COALESCE(NULLIF(trim([[user]].[[name]]),''),
						NULLIF(trim([[user]].[[email]]),''),[[member]].[[principal_id]])
					ELSE COALESCE(NULLIF(trim([[group]].[[name]]),''),[[member]].[[principal_id]])
				END AS [[label]],
				CASE [[member]].[[principal_type]] WHEN 'user' THEN [[member]].[[principal_id]] ELSE '' END AS [[user_id]],
				COUNT(*) OVER (PARTITION BY [[member]].[[teamspace_id]]) AS [[owner_count]],
				ROW_NUMBER() OVER (PARTITION BY [[member]].[[teamspace_id]]
					ORDER BY [[member]].[[principal_type]],[[member]].[[principal_id]]) AS [[owner_rank]]
			FROM {{leafen_teamspace_members}} [[member]]
			LEFT JOIN {{users}} [[user]]
				ON [[member]].[[principal_type]]='user' AND [[user]].[[id]]=[[member]].[[principal_id]]
			LEFT JOIN {{leafen_workspace_permission_groups}} [[group]]
				ON [[member]].[[principal_type]]='group' AND [[group]].[[id]]=[[member]].[[principal_id]]
				AND [[group]].[[workspace_id]]={:workspaceId} AND [[group]].[[deleted_at]] IS NULL
			WHERE [[member]].[[workspace_id]]={:workspaceId} AND [[member]].[[deleted_at]]=0
				AND [[member]].[[role]]='owner' AND [[member]].[[teamspace_id]] IN (` + targets + `)
		)
		SELECT [[teamspace_id]],[[label]],[[user_id]],[[owner_count]] FROM [[owner_labels]]
		WHERE [[owner_rank]]<=` + fmt.Sprint(teamspaceOwnerLabelLimit) + `
		ORDER BY [[teamspace_id]],[[owner_rank]]
	`).WithContext(ctx).Bind(params).All(&ownerRows); err != nil {
		return nil, err
	}

	// Those who answer its join requests, for where the workspace's owner
	// stands in (managesTeamspace).
	answeringRows := []struct {
		TeamspaceID string `db:"teamspace_id"`
		Count       int    `db:"answering_count"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[teamspace_id]],COUNT(DISTINCT [[user_id]]) AS [[answering_count]]
		FROM (` + teamspaceAnsweringOwnersSQL(" IN ("+targets+")") + `)
		GROUP BY [[teamspace_id]]
	`).WithContext(ctx).Bind(params).All(&answeringRows); err != nil {
		return nil, err
	}

	summaries := make(map[string]teamspaceSummaryWire, len(teamspaceIDs))
	for _, id := range teamspaceIDs {
		summaries[id] = teamspaceSummaryWire{TeamspaceID: id, OwnerLabels: []string{}, OwnerUserIDs: []string{}}
	}
	for _, row := range memberRows {
		summary := summaries[row.TeamspaceID]
		summary.MemberCount = row.MemberCount
		summaries[row.TeamspaceID] = summary
	}
	for _, row := range ownerRows {
		summary := summaries[row.TeamspaceID]
		summary.OwnerCount = row.OwnerCount
		summary.OwnerLabels = append(summary.OwnerLabels, row.Label)
		summary.OwnerUserIDs = append(summary.OwnerUserIDs, row.UserID)
		summaries[row.TeamspaceID] = summary
	}
	for _, row := range answeringRows {
		summary := summaries[row.TeamspaceID]
		summary.AnsweringOwnerCount = row.Count
		summaries[row.TeamspaceID] = summary
	}
	result := make([]teamspaceSummaryWire, 0, len(teamspaceIDs))
	for _, id := range teamspaceIDs {
		result = append(result, summaries[id])
	}
	return result, nil
}

// teamspaceEffectiveMembersSQL lists, as (teamspace_id, user_id), the people
// in the workspace's ({:workspaceId}) teamspaces: those added themselves or
// through a group, and in each of its default teamspaces its owner and its
// members other than restricted ones.
func teamspaceEffectiveMembersSQL() string {
	return `
			SELECT [[member]].[[teamspace_id]],[[member]].[[principal_id]]
			FROM {{leafen_teamspace_members}} [[member]]
			WHERE [[member]].[[workspace_id]]={:workspaceId} AND [[member]].[[deleted_at]]=0
				AND [[member]].[[principal_type]]='user'
			UNION
			SELECT [[member]].[[teamspace_id]],[[group_member]].[[user_id]]
			FROM {{leafen_teamspace_members}} [[member]]
			JOIN {{leafen_workspace_permission_groups}} [[group]]
				ON [[group]].[[id]]=[[member]].[[principal_id]]
			JOIN {{leafen_workspace_permission_group_members}} [[group_member]]
				ON [[group_member]].[[group_id]]=[[group]].[[id]]
			WHERE [[member]].[[workspace_id]]={:workspaceId} AND [[member]].[[deleted_at]]=0
				AND [[member]].[[principal_type]]='group' AND [[group]].[[deleted_at]] IS NULL
			UNION
			SELECT [[default_id]].[[value]],[[workspace]].[[workspace_owner]]
			FROM {{leafen_teamspace_settings}} [[settings]], json_each([[settings]].[[default_teamspace_ids]]) [[default_id]]
			JOIN {{leafen_workspaces}} [[workspace]] ON [[workspace]].[[id]]=[[settings]].[[workspace_id]]
			WHERE [[settings]].[[workspace_id]]={:workspaceId}
			UNION
			SELECT [[default_id]].[[value]],[[membership]].[[user_id]]
			FROM {{leafen_teamspace_settings}} [[settings]], json_each([[settings]].[[default_teamspace_ids]]) [[default_id]]
			JOIN {{leafen_workspace_memberships}} [[membership]]
				ON [[membership]].[[workspace_id]]=[[settings]].[[workspace_id]]
			WHERE [[settings]].[[workspace_id]]={:workspaceId}
				AND NOT ` + workspaceRestrictedMemberSQL("[[membership]].[[workspace_id]]", "[[membership]].[[user_id]]")
}
