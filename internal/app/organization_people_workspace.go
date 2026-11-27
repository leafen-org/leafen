package app

import (
	"database/sql"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A person picker in a workspace offers that workspace's people, as Notion's
// person property tags "other members or guests in your workspace" (its
// database properties help, read 2026-11-12; seen 2026-11-12 under
// 한지테스트페이지: the picker lists the workspace's member and its guests,
// no one else). An organization holds its owner's workspaces, so its people
// are more than one workspace's: a search asked for one workspace
// (?workspace=) finds only the people of that workspace among them.

// workspacePickPeople lists the IDs of the people a picker in the workspace
// offers its reader: its owner and members, and its guests — the accounts its
// live pages are shared with — or, for a restricted member, those of them they share a place
// with (restricted_member_people.go). The workspace must be the
// organization's and one the reader reads, as a member or as a guest.
func workspacePickPeople(app core.App, readerID, organizationID, workspaceID string) ([]string, error) {
	var organization string
	err := app.DB().NewQuery(`SELECT [[organization]] FROM {{leafen_workspaces}} WHERE [[id]] = {:workspaceId}`).
		Bind(dbx.Params{"workspaceId": workspaceID}).Row(&organization)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && organization != organizationID) {
		return nil, permissionDenied()
	}
	if err != nil {
		return nil, err
	}
	restricted := false
	if member, err := authorizeWorkspace(app, readerID, workspaceID, permissionWorkspaceRead); err == nil {
		restricted = member.Restricted
	} else if !isPermissionDenied(err) {
		return nil, err
	} else if guest, err := hasBootstrapGuestShare(app, readerID, workspaceID); err != nil {
		return nil, err
	} else if !guest {
		return nil, permissionDenied()
	}
	narrowed := ""
	if restricted {
		narrowed = ` WHERE [[people]].[[user_id]] IN (` + restrictedVisiblePeopleSQL() + `)`
	}
	ids := []string{}
	err = app.DB().NewQuery(workspaceGuestSharesSQL + `
		SELECT DISTINCT [[people]].[[user_id]] FROM (
			SELECT [[workspace_owner]] AS [[user_id]] FROM {{leafen_workspaces}} WHERE [[id]] = {:workspaceId}
			UNION SELECT [[user_id]] FROM {{leafen_workspace_memberships}} WHERE [[workspace_id]] = {:workspaceId}
			UNION SELECT [[account_id]] FROM [[guest_share]] WHERE [[account_id]] IS NOT NULL
		) [[people]]` + narrowed).
		Bind(dbx.Params{"workspaceId": workspaceID, "readerId": readerID}).Column(&ids)
	return ids, err
}
