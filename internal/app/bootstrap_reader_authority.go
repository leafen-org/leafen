package app

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// bootstrapReaderAuthority names, as one digest, what decides which
// workspaces and parts of them a person's page list holds: the workspaces
// they own or belong to and their role and restriction in each, their
// organizations' memberships, policies and suspensions, their teamspaces and
// permission groups, and the server's settings. Each part of the list says it.
//
// The list is read a part at a time (bootstrapCollection.ts). A change to its
// pages meanwhile doesn't start the read over: the parts after it are read as
// the list is then, and the list's refresh tokens stay those of its first
// part, so its change check reads it again (2026-11-14: in a workspace of
// 29,000 pages a page copy moved the snapshot revision between every two
// parts, every read began again and gave up after the second time, and the
// sidebar showed no new page until the copy ended). A change to this does
// start it over, so a list never mixes workspaces read before the person
// lost or gained them with ones read after.
func bootstrapReaderAuthority(app core.App, principalID string) (string, error) {
	rows := []struct {
		Kind  string `db:"kind"`
		ID    string `db:"id"`
		State string `db:"state"`
	}{}
	err := app.DB().NewQuery(`
		SELECT 'owner' AS [[kind]], [[id]], '' AS [[state]] FROM {{leafen_workspaces}}
			WHERE [[workspace_owner]] = {:principalId} AND COALESCE([[deletion_job_id]], '') = ''
		UNION ALL
		SELECT 'member', [[workspace_id]], [[role]] FROM {{leafen_workspace_memberships}}
			WHERE [[user_id]] = {:principalId}
		UNION ALL
		SELECT 'restricted', [[workspace_id]], '' FROM {{leafen_workspace_restricted_members}}
			WHERE [[user_id]] = {:principalId}
		UNION ALL
		SELECT 'organization', [[organization]], [[role]] || ':' || [[status]] FROM {{leafen_organization_memberships}}
			WHERE [[user]] = {:principalId}
		UNION ALL
		SELECT 'policy', [[policy]].[[organization_id]], CAST([[policy]].[[version]] AS TEXT)
			FROM {{leafen_organization_policy_state}} [[policy]]
			JOIN {{leafen_organization_memberships}} [[membership]]
				ON [[membership]].[[organization]] = [[policy]].[[organization_id]]
			WHERE [[membership]].[[user]] = {:principalId}
		UNION ALL
		SELECT 'suspended', [[organization_id]], '' FROM {{leafen_organization_user_suspensions}}
			WHERE [[user_id]] = {:principalId}
		UNION ALL
		SELECT 'teamspace', [[teamspace_id]], [[role]] FROM {{leafen_teamspace_members}}
			WHERE [[workspace_id]] IN (
					SELECT [[workspace_id]] FROM {{leafen_workspace_memberships}} WHERE [[user_id]] = {:principalId}
					UNION SELECT [[id]] FROM {{leafen_workspaces}} WHERE [[workspace_owner]] = {:principalId})
				AND [[principal_type]] = 'user' AND [[principal_id]] = {:principalId} AND [[deleted_at]] = 0
		UNION ALL
		SELECT 'group', [[group_id]], [[role]] FROM {{leafen_workspace_permission_group_members}}
			WHERE [[user_id]] = {:principalId}
		UNION ALL
		SELECT 'server', [[id]], CAST([[revision]] AS TEXT) FROM {{leafen_server_settings}}
		ORDER BY 1, 2, 3
	`).Bind(dbx.Params{"principalId": principalID}).All(&rows)
	if err != nil {
		return "", err
	}
	parts := make([]string, 0, len(rows))
	for _, row := range rows {
		parts = append(parts, row.Kind+"\x1f"+row.ID+"\x1f"+row.State)
	}
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x1e")))
	return hex.EncodeToString(digest[:]), nil
}
