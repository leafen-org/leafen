package app

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A permission group holds people of its workspace — its owner and members —
// and one asked to hold anyone else is refused naming the field and the IDs
// that aren't theirs (2026-11-26, a real agent: "Choose members of this
// workspace." for two members named by IDs this route didn't read then, and
// no word of which field or ID).

// workspacePermissionGroupOutside is a refusal of people outside the
// workspace, by the IDs given.
type workspacePermissionGroupOutside struct{ ids []string }

func (outside workspacePermissionGroupOutside) Error() string {
	return errWorkspacePermissionGroupMember.Error() + ": " + strings.Join(outside.ids, ", ")
}

func (outside workspacePermissionGroupOutside) Unwrap() error {
	return errWorkspacePermissionGroupMember
}

// workspacePermissionGroupOutsiders refuses the IDs of people who aren't the
// workspace's owner or members, naming them; none, nil.
func workspacePermissionGroupOutsiders(app core.App, workspaceID string, userIDs []string) error {
	if len(userIDs) == 0 {
		return nil
	}
	ids, _ := json.Marshal(userIDs)
	outside := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[candidate]].[[value]] FROM json_each({:ids}) [[candidate]]
		JOIN {{leafen_workspaces}} [[workspace]] ON [[workspace]].[[id]] = {:workspaceId}
		WHERE [[candidate]].[[value]] != [[workspace]].[[workspace_owner]] AND NOT EXISTS (
			SELECT 1 FROM {{leafen_workspace_memberships}} [[membership]]
			WHERE [[membership]].[[workspace_id]] = [[workspace]].[[id]]
				AND [[membership]].[[user_id]] = [[candidate]].[[value]]
		)
		ORDER BY [[candidate]].[[key]]
	`).Bind(dbx.Params{"ids": string(ids), "workspaceId": workspaceID}).Column(&outside); err != nil {
		return err
	}
	if len(outside) > 0 {
		return workspacePermissionGroupOutside{ids: outside}
	}
	return nil
}

// workspacePermissionGroupMemberRefusal is what a refusal of people outside
// the workspace says: the field that named them (memberIds, add) and their
// IDs, the first few.
func workspacePermissionGroupMemberRefusal(field string, err error) string {
	var outside workspacePermissionGroupOutside
	if !errors.As(err, &outside) {
		return "Choose members of this workspace."
	}
	shown := outside.ids
	if len(shown) > 5 {
		shown = append(shown[:5:5], "and "+strconv.Itoa(len(outside.ids)-5)+" more")
	}
	who := "who isn't a member of it"
	if len(outside.ids) > 1 {
		who = "who aren't members of it"
	}
	return "Choose members of this workspace: " + field + " names " + strings.Join(shown, ", ") + ", " + who +
		" (GET /workspaces/{workspaceId}/members lists them)."
}
