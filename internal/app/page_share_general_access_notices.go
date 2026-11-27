package app

import (
	"context"
	"strconv"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// pagePermissionWorkspacePerson stands for everyone in a page's workspace
// where a permission line names its share's person: Notion's line of whoever
// opened a page to "〈작업공간〉의 모든 사용자" names them so, its role under it
// as a person's is — "본인 〈page〉의 권한을 업데이트했습니다." with
// "〈작업공간〉의 모든 사용자 / 전체 허용" on the added band (seen 2026-11-16 in a
// page's updates, two tabs at two times: once joined to a person's share
// given two minutes before, once on a line of its own), taken back within the
// five minutes leaving nothing of it, and taken back later struck through
// (seen once). No user id is this word.
const pagePermissionWorkspacePerson = "workspace"

// grantPublicMutationPermissionTelling shares a page as
// grantPublicMutationPermission does and, where the share is with the whole
// workspace and gives it another role than it had, tells whoever made it.
func grantPublicMutationPermissionTelling(
	ctx context.Context, app core.App, authorization pageAuthorization, input publicMutationInput, now time.Time,
) error {
	before := ""
	if input.PrincipalType == publicMutationPrincipalWorkspace {
		roles := []string{}
		if err := app.DB().NewQuery(`
			SELECT [[role]] FROM {{leafen_page_permissions}}
			WHERE [[page_id]]={:page} AND [[principal_type]]={:type} AND [[principal_id]]={:workspace} LIMIT 1
		`).Bind(dbx.Params{
			"page": authorization.Page.Id, "type": publicMutationPrincipalWorkspace, "workspace": input.PrincipalID,
		}).Column(&roles); err != nil {
			return err
		}
		if len(roles) > 0 {
			before = roles[0]
		}
	}
	if err := grantPublicMutationPermission(ctx, app, authorization, input, now); err != nil {
		return err
	}
	if input.PrincipalType != publicMutationPrincipalWorkspace || pageShareRoleRank(before) == pageShareRoleRank(input.Role) {
		return nil
	}
	return tellGeneralAccessChange(app, authorization, before, input.Role, now)
}

// tellGeneralAccessChange writes, in the updates of whoever changed it, the
// line of a page's access for everyone in its workspace going from one role
// to another ("" none): theirs alone, as the workspace's members were not
// seen told (page_share_permission_notices.go), with no email and no push.
func tellGeneralAccessChange(app core.App, authorization pageAuthorization, from, to string, now time.Time) error {
	actorID := authorization.Workspace.PrincipalID
	if actorID == "" {
		return nil
	}
	return upsertPagePermissionLine(app, authorization.Workspace.WorkspaceID, authorization.Page, actorID, actorID,
		pagePermissionWorkspacePerson, strconv.FormatInt(now.UnixMilli(), 10), from, to, now)
}
