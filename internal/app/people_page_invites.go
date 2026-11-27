package app

import (
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A person's page is not shared with the workspace's restricted members, as
// Notion's: its share window offers the restricted member under "추천" as
// "제한됨", yet sharing with them answers "초대를 처리하는 데 실패했습니다."
// and adds no share (seen 2026-11-16 in Chrome at 1440, the workspace's
// owner on their own person's page, in two tabs at two times). A restricted
// member reads only their own person's page (people_database_access.go), so
// such an invite would open nothing.
var errPersonPageRestrictedInvite = errors.New(errorCodePublicMutationPersonPageRestricted)

// refusePersonPageRestrictedShare refuses sharing a page of People's
// database, a person's page, with a restricted member of its workspace other
// than its own person, whoever shares it and from wherever.
func refusePersonPageRestrictedShare(app core.App, workspaceID, pageID, granteeID string) error {
	if granteeID == "" {
		return nil
	}
	persons := []string{}
	if err := app.DB().NewQuery(`SELECT [[user_id]] FROM {{leafen_workspace_people_rows}}
		WHERE [[workspace_id]] = {:workspaceId} AND [[row_id]] = {:pageId}`).
		Bind(dbx.Params{"workspaceId": workspaceID, "pageId": pageID}).Column(&persons); err != nil || len(persons) == 0 || persons[0] == granteeID {
		return err
	}
	restricted, err := workspaceMemberRestricted(app, workspaceID, granteeID)
	if err != nil || !restricted {
		return err
	}
	return errPersonPageRestrictedInvite
}

// enforcePersonPageShare is that rule for a share from the page's share
// window: a person chosen, or an address that is theirs.
func enforcePersonPageShare(app core.App, authorization pageAuthorization, input publicMutationInput) error {
	if input.Action != publicMutationActionGrantPermission {
		return nil
	}
	grantee := ""
	switch input.PrincipalType {
	case publicMutationPrincipalUser:
		grantee = input.PrincipalID
	case publicMutationPrincipalEmail:
		account, err := pageShareEmailAccount(app, input.PrincipalID)
		if err != nil {
			return err
		}
		grantee = account
	}
	return refusePersonPageRestrictedShare(app, authorization.Workspace.WorkspaceID, authorization.Page.Id, grantee)
}
