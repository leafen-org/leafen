package app

import (
	"database/sql"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// withPeopleRuleRelinkChange adds to a lowered person's page's relink what
// linking it again gives back by People's rule — everyone in the workspace
// reading it, as a page above gives (pageInheritedShareRoles) — when the
// page's own share gives them less: the relink window lists each one a page
// above gives back, its role before and after (populatePublicMutationShareUnlink,
// TopBarShareInheritanceActions), and People's rule is one of them. A
// person's page no longer unlinks (people_page_linked.go), as Notion's, which
// never reaches its "설정 복원"; this is for one lowered before, its list
// Notion's for a page under a page shared with the workspace.
func withPeopleRuleRelinkChange(app core.App, page *core.Record, state *publicMutationState) error {
	if state.ShareUnlink == nil {
		return nil
	}
	if _, _, people, err := peopleDatabasePageOf(app, page); err != nil || !people {
		return err
	}
	var own string
	err := app.DB().NewQuery(`SELECT [[role]] FROM {{leafen_page_permissions}}
		WHERE [[page_id]] = {:pageId} AND [[principal_type]] = {:type} AND [[principal_id]] = {:workspaceId}`).
		Bind(dbx.Params{"pageId": page.Id, "type": publicMutationPrincipalWorkspace, "workspaceId": state.WorkspaceID}).Row(&own)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if pageAccessRoleFromTeamspaceRole(own) >= pageAccessRoleView {
		return nil
	}
	for _, change := range state.ShareUnlink.Changes {
		if change.PrincipalType == publicMutationPrincipalWorkspace && change.PrincipalID == state.WorkspaceID {
			return nil
		}
	}
	state.ShareUnlink.Changes = append([]publicMutationRelinkChange{{
		PrincipalType: publicMutationPrincipalWorkspace, PrincipalID: state.WorkspaceID,
		FromRole: own, ToRole: publicMutationRoleView,
	}}, state.ShareUnlink.Changes...)
	return nil
}
