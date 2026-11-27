package app

import (
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A restricted member, as Notion's "제한된 멤버" (its help "Who's who in a
// workspace", read 2026-10-12), is a member in everything but where the rest
// of the workspace would let them in: they make private pages and belong to
// groups, but the default teamspace doesn't hold them, an open teamspace
// neither shows nor lets them in and gives them no role in its pages, and
// they make no teamspace. A teamspace they were added to treats them as any
// member. The organization module adds and takes people off; the core reads
// who they are wherever it decides.

var errWorkspaceRestrictedMemberTarget = errors.New("restricted member target is not a workspace member")

// workspaceRestrictedMemberSQL is an SQL expression, true when the user the
// second expression names is a restricted member of the workspace the first
// names.
func workspaceRestrictedMemberSQL(workspaceExpression, userExpression string) string {
	return "EXISTS (SELECT 1 FROM {{leafen_workspace_restricted_members}} [[restricted_member]]" +
		" WHERE [[restricted_member]].[[workspace_id]] = " + workspaceExpression +
		" AND [[restricted_member]].[[user_id]] = " + userExpression + ")"
}

func workspaceMemberRestricted(app core.App, workspaceID, userID string) (bool, error) {
	var restricted bool
	err := app.DB().NewQuery(`SELECT ` + workspaceRestrictedMemberSQL("{:workspaceId}", "{:userId}")).
		Bind(dbx.Params{"workspaceId": workspaceID, "userId": userID}).Row(&restricted)
	return restricted, err
}

// workspaceRestrictedMembers are the workspace's restricted members.
func workspaceRestrictedMembers(app core.App, workspaceID string) (map[string]bool, error) {
	rows := []struct {
		UserID string `db:"user_id"`
	}{}
	err := app.DB().NewQuery(`SELECT [[user_id]] FROM {{leafen_workspace_restricted_members}}
		WHERE [[workspace_id]] = {:workspaceId}`).Bind(dbx.Params{"workspaceId": workspaceID}).All(&rows)
	restricted := make(map[string]bool, len(rows))
	for _, row := range rows {
		restricted[row.UserID] = true
	}
	return restricted, err
}

// setWorkspaceMemberRestricted makes a member of the workspace restricted, or
// a member again, and moves the workspace's permission cursor when that
// changes anything; the caller runs it in its transaction and closes the
// workspace's rooms after. The workspace owner is no member and can't be
// restricted.
func setWorkspaceMemberRestricted(app core.App, workspaceID, userID, actorID string, restricted bool, now int64) (bool, error) {
	var member int
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_workspace_memberships}}
		WHERE [[workspace_id]] = {:workspaceId} AND [[user_id]] = {:userId} AND [[role]] = 'member'`).
		Bind(dbx.Params{"workspaceId": workspaceID, "userId": userID}).Row(&member); err != nil {
		return false, err
	}
	if member != 1 {
		return false, errWorkspaceRestrictedMemberTarget
	}
	params := dbx.Params{"workspaceId": workspaceID, "userId": userID, "actorId": actorID, "now": now}
	query := `INSERT INTO {{leafen_workspace_restricted_members}} ([[workspace_id]],[[user_id]],[[assigned_by]],[[created_at]])
		VALUES ({:workspaceId},{:userId},{:actorId},{:now}) ON CONFLICT DO NOTHING`
	if !restricted {
		query = `DELETE FROM {{leafen_workspace_restricted_members}}
			WHERE [[workspace_id]] = {:workspaceId} AND [[user_id]] = {:userId}`
	}
	result, err := app.DB().NewQuery(query).Bind(params).Execute()
	if err != nil {
		return false, err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed == 0 {
		return false, err
	}
	if _, err = advanceWorkspacePermissionCursor(app, workspaceID, now); err != nil {
		return false, err
	}
	// People's Membership Type reads restricted, or member again.
	notePeopleMembershipChanged(app, workspaceID, actorID, userID)
	return true, nil
}
