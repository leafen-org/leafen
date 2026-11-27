package app

import (
	"encoding/json"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Where each account of the server console's list is (user 2026-11-23:
// an agent read workspaceCount 0 as "in no workspace" twice, when it counted
// only the workspaces the account owns and the person was a member of one):
// the workspaces it owns or is a member of, with its role there, and those
// where pages were shared with it as a guest, with how many — each the first
// few (administratorAccountWorkspacesShown) and how many in all. The owned
// counts keep their names and say so again under ownedWorkspaceCount and
// ownedOrganizationCount; a guest is who a workspace's own People lists as
// one (guestSharesSQL).

func init() {
	nativeOperationNotes[nativeOperationShape("GET /admin/accounts")] = "Each account says where it is: joinedWorkspaces are the workspaces " +
		"it owns or is a member of, each with its role (owner, member, restricted), and joinedWorkspaceCount how many in all; " +
		"guestWorkspaces are those where pages were shared with it as a guest, each with pageCount, and guestWorkspaceCount how many in all; " +
		"each list holds the first few. workspaceCount and organizationCount count only what it owns (ownedWorkspaceCount, ownedOrganizationCount): " +
		"0 there doesn't mean it is in no workspace."
}

type storedAdministratorAccountPlace struct {
	UserID      string `db:"user_id"`
	WorkspaceID string `db:"workspace_id"`
	Name        string `db:"name"`
	Role        string `db:"role"`
	PageCount   int64  `db:"page_count"`
	Total       int64  `db:"total"`
}

// addAdministratorAccountPlaces fills in where each account is, for all of
// them in two reads.
func addAdministratorAccountPlaces(app core.App, accounts []administratorManagedAccount) error {
	ids := make([]string, 0, len(accounts))
	for _, account := range accounts {
		ids = append(ids, account.ID)
	}
	if len(ids) == 0 {
		return nil
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	params := dbx.Params{"accounts": string(encoded), "shown": administratorAccountWorkspacesShown}
	joined := []storedAdministratorAccountPlace{}
	if err := app.DB().NewQuery(`
		SELECT [[user_id]], [[workspace_id]], [[name]], [[role]], 0 AS [[page_count]], [[total]] FROM (
			SELECT [[place]].*, ROW_NUMBER() OVER (PARTITION BY [[user_id]] ORDER BY [[rank]], lower([[name]]), [[workspace_id]]) AS [[shown]],
				COUNT(*) OVER (PARTITION BY [[user_id]]) AS [[total]]
			FROM (
				SELECT [[workspace]].[[workspace_owner]] AS [[user_id]], [[workspace]].[[id]] AS [[workspace_id]], [[workspace]].[[name]],
					'owner' AS [[role]], 0 AS [[rank]]
				FROM {{leafen_workspaces}} [[workspace]]
				WHERE [[workspace]].[[workspace_owner]] IN (SELECT [[value]] FROM json_each({:accounts}))
				UNION ALL
				SELECT [[membership]].[[user_id]], [[workspace]].[[id]], [[workspace]].[[name]],
					CASE WHEN ` + workspaceRestrictedMemberSQL("[[workspace]].[[id]]", "[[membership]].[[user_id]]") + ` THEN 'restricted' ELSE 'member' END,
					1
				FROM {{leafen_workspace_memberships}} [[membership]]
				JOIN {{leafen_workspaces}} [[workspace]] ON [[workspace]].[[id]] = [[membership]].[[workspace_id]]
				WHERE [[membership]].[[user_id]] IN (SELECT [[value]] FROM json_each({:accounts}))
					AND [[membership]].[[user_id]] <> [[workspace]].[[workspace_owner]]
			) AS [[place]]
		) WHERE [[shown]] <= {:shown}
		ORDER BY [[user_id]], [[shown]]
	`).Bind(params).All(&joined); err != nil {
		return err
	}
	guests := []storedAdministratorAccountPlace{}
	if err := app.DB().NewQuery(guestSharesSQL(administratorAccountSharesSQL) + `
		SELECT [[user_id]], [[workspace_id]], [[name]], '' AS [[role]], [[page_count]], [[total]] FROM (
			SELECT [[guest]].*, ROW_NUMBER() OVER (PARTITION BY [[user_id]] ORDER BY [[page_count]] DESC, lower([[name]]), [[workspace_id]]) AS [[shown]],
				COUNT(*) OVER (PARTITION BY [[user_id]]) AS [[total]]
			FROM (
				SELECT [[guest_share]].[[account_id]] AS [[user_id]], [[guest_share]].[[share_workspace_id]] AS [[workspace_id]],
					MAX([[workspace]].[[name]]) AS [[name]], COUNT(DISTINCT [[guest_share]].[[page_id]]) AS [[page_count]]
				FROM [[guest_share]]
				JOIN {{leafen_workspaces}} [[workspace]] ON [[workspace]].[[id]] = [[guest_share]].[[share_workspace_id]]
				WHERE [[guest_share]].[[account_id]] IN (SELECT [[value]] FROM json_each({:accounts}))
				GROUP BY [[guest_share]].[[account_id]], [[guest_share]].[[share_workspace_id]]
			) AS [[guest]]
		) WHERE [[shown]] <= {:shown}
		ORDER BY [[user_id]], [[shown]]
	`).Bind(params).All(&guests); err != nil {
		return err
	}
	byID := make(map[string]*administratorManagedAccount, len(accounts))
	for index := range accounts {
		byID[accounts[index].ID] = &accounts[index]
	}
	for _, place := range joined {
		if account := byID[place.UserID]; account != nil {
			account.JoinedWorkspaces = append(account.JoinedWorkspaces, administratorAccountJoinedWorkspace{ID: place.WorkspaceID, Name: place.Name, Role: place.Role})
			account.JoinedWorkspaceCount = place.Total
		}
	}
	for _, place := range guests {
		if account := byID[place.UserID]; account != nil {
			account.GuestWorkspaces = append(account.GuestWorkspaces, administratorAccountGuestWorkspace{ID: place.WorkspaceID, Name: place.Name, PageCount: place.PageCount})
			account.GuestWorkspaceCount = place.Total
		}
	}
	return nil
}

// administratorAccountSharesSQL keeps the shares that name one of the
// accounts asked about, by its id or an address it proved.
const administratorAccountSharesSQL = `(([[permission]].[[principal_type]] = 'user'
		AND [[permission]].[[principal_id]] IN (SELECT [[value]] FROM json_each({:accounts})))
	OR ([[permission]].[[principal_type]] = 'email' AND lower(trim([[permission]].[[principal_id]])) IN (
		SELECT lower(trim([[address]].[[email]])) FROM {{leafen_account_emails}} [[address]]
		WHERE [[address]].[[user_id]] IN (SELECT [[value]] FROM json_each({:accounts})))))`
