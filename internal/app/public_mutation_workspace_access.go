package app

import (
	"context"
	"fmt"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The general access row is independent of the paginated people roster. It
// reads the same inherited page grants that native authorization evaluates:
// those of the pages of the page's chain.
func populatePublicMutationWorkspaceAccess(
	ctx context.Context, app core.App, authorization pageAuthorization, ids []string, state *publicMutationState,
) error {
	_, err := authorizeWorkspace(app, authorization.Workspace.PrincipalID, state.WorkspaceID, permissionWorkspaceRead)
	if err != nil && !isPermissionDenied(err) {
		return err
	}
	state.WorkspaceSharingAllowed = err == nil
	if err := populatePublicMutationLinkAccess(ctx, app, authorization, ids, state); err != nil {
		return err
	}
	params := dbx.Params{"workspace": state.WorkspaceID}
	keys := make([]string, 0, len(ids))
	for i, id := range ids {
		key := fmt.Sprintf("ancestor%d", i)
		params[key] = id
		keys = append(keys, "{:"+key+"}")
	}
	rows := []struct {
		ID     string `db:"id"`
		PageID string `db:"page_id"`
		Role   string `db:"role"`
		Hide   bool   `db:"hide_from_search"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[id]],[[page_id]],[[role]],[[hide_from_search]] FROM {{leafen_page_permissions}}
 WHERE [[workspace_id]]={:workspace} AND [[principal_type]]='workspace' AND [[principal_id]]={:workspace}
 AND [[page_id]] IN (` + joinSQL(keys) + `)`).WithContext(ctx).Bind(params).All(&rows); err != nil {
		return err
	}
	best := pageAccessRoleNone
	for _, row := range rows {
		if row.PageID == state.PageID {
			state.WorkspacePermission = &publicMutationPermission{ID: row.ID, PrincipalType: publicMutationPrincipalWorkspace,
				PrincipalID: state.WorkspaceID, PrincipalLabel: state.WorkspaceName, Role: row.Role, HideFromSearch: row.Hide}
		} else if role := pageAccessRoleFromTeamspaceRole(row.Role); role > best {
			best = role
		}
	}
	// People's pages, its database's and its rows', are held by the
	// workspace's owners and read by its members by People's own rule
	// (people_database_access.go), which no share holds: their general
	// access says so, as Notion's person page and People database say
	// "<workspace>의 소유자 — 전체 허용" over "<workspace>의 모든 사용자 —
	// 읽기 허용" under a building on the share button (seen 2026-11-16 in
	// Chrome at 1440, in two tabs at two times). The database's two rows are
	// fixed there; a person's page raises its members' role by a share of
	// its own and lowers it by unlinking from People's rule, after which its
	// own shares alone say who reads it (authorizePeopleDatabasePage).
	if _, _, people, err := peopleDatabasePageOf(app, authorization.Page); err != nil {
		return err
	} else if people {
		row := isDatabaseRowPageRecord(authorization.Page)
		lowered := false
		if row {
			if lowered, err = pageShareUnlinked(app, authorization.Page.Id); err != nil {
				return err
			}
		}
		if !lowered {
			best = max(best, pageAccessRoleView)
			// "고급" counts People's own page-level rule (people_database_page_rule.go).
			state.PageRuleCount++
		}
		state.WorkspaceOwnersHold = true
		state.GeneralAccessFixed = !row
	}
	state.WorkspaceInheritedRole = publicMutationRoleFor(best)
	if state.WorkspacePermission != nil {
		if role := pageAccessRoleFromTeamspaceRole(state.WorkspacePermission.Role); role > best {
			best = role
		}
	}
	if best == pageAccessRoleNone || len(state.Permissions) == 0 {
		return nil
	}
	// A single roster query identifies members who keep workspace access when
	// their individual share is removed. Guests and restricted members retain
	// no workspace grant (workspaceShareMemberSQL).
	params = dbx.Params{"workspace": state.WorkspaceID}
	keys = nil
	for i, permission := range state.Permissions {
		if permission.PrincipalType != publicMutationPrincipalUser {
			continue
		}
		key := fmt.Sprintf("person%d", i)
		params[key] = permission.PrincipalID
		keys = append(keys, "{:"+key+"}")
	}
	if len(keys) == 0 {
		return nil
	}
	members := []string{}
	if err := app.DB().NewQuery(`SELECT [[membership]].[[user]] FROM {{leafen_organization_memberships}} [[membership]]
 JOIN {{leafen_workspaces}} [[workspace]] ON [[workspace]].[[organization]]=[[membership]].[[organization]]
 WHERE [[workspace]].[[id]]={:workspace} AND [[membership]].[[status]]='active' AND [[membership]].[[user]] IN (` + joinSQL(keys) + `)
 AND ([[workspace]].[[workspace_owner]]=[[membership]].[[user]] OR ` + workspaceShareMemberSQL("{:workspace}", "[[membership]].[[user]]") + `)`).WithContext(ctx).Bind(params).Column(&members); err != nil {
		return err
	}
	known := map[string]bool{}
	for _, id := range members {
		known[id] = true
	}
	for i, permission := range state.Permissions {
		if known[permission.PrincipalID] && (permission.OtherAccess == nil || pageAccessRoleFromTeamspaceRole(permission.OtherAccess.Role) < best) {
			state.Permissions[i].OtherAccess = &publicMutationOtherAccess{Source: "workspace", Label: state.WorkspaceName, Role: publicMutationRoleFor(best)}
		}
	}
	return nil
}
