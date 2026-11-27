package app

import (
	"database/sql"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A page at its teamspace's top takes its sharing from the teamspace's
// default: its owners and members have the teamspace's roles on it. Changing
// either group's role there unlinks the page from that default, as Notion's
// does (seen 2026-11-16 at 1440 on a page at a teamspace's top: the change
// asks first whether to change the access and unlink the page's sharing, then
// the share window notes over its people that the page's sharing is no longer
// linked to the teamspace's default, and its "설정 복원" asks to give the
// groups the teamspace's roles again; Notion's code, read the same day, says
// so of a database at the top too). The page holds the roles it was given
// (page_teamspace_roles.go): while it does, it is unlinked.

// pageAtTeamspaceTop says whether a teamspace's page stands at its top, no
// page above it — not a page unlinked from the page above it, which holds
// its own sharing too.
func pageAtTeamspaceTop(app core.App, workspaceID string, page *core.Record) (bool, error) {
	if page.GetString("teamspace_id") == "" {
		return false, nil
	}
	ids, err := loadPageAccessAncestorIDs(app, workspaceID, page.Id)
	if err != nil || len(ids) > 1 {
		return false, err
	}
	unlinked, err := pageShareUnlinked(app, page.Id)
	return !unlinked, err
}

// pageOwnTeamspaceRoles reads the roles a page holds of its own, and whether
// it holds any.
func pageOwnTeamspaceRoles(app core.App, workspaceID, teamspaceID, pageID string) (storedPageTeamspaceRoles, bool, error) {
	roles := storedPageTeamspaceRoles{}
	err := app.DB().NewQuery(`
		SELECT [[page_id]],[[owner_role]],[[member_role]] FROM {{leafen_page_teamspace_roles}}
		WHERE [[page_id]]={:pageId} AND [[workspace_id]]={:workspaceId} AND [[teamspace_id]]={:teamspaceId}
	`).Bind(dbx.Params{"pageId": pageID, "workspaceId": workspaceID, "teamspaceId": teamspaceID}).One(&roles)
	if errors.Is(err, sql.ErrNoRows) {
		return roles, false, nil
	}
	return roles, err == nil, err
}

// pageTeamspaceDefaultUnlinked says whether a page at its teamspace's top no
// longer follows the teamspace's default: it holds roles of its own.
func pageTeamspaceDefaultUnlinked(app core.App, workspaceID string, page *core.Record) (bool, error) {
	top, err := pageAtTeamspaceTop(app, workspaceID, page)
	if err != nil || !top {
		return false, err
	}
	_, own, err := pageOwnTeamspaceRoles(app, workspaceID, page.GetString("teamspace_id"), page.Id)
	return own, err
}

// populatePublicMutationTeamspaceUnlink says which teamspace's default a page
// at its top left, and what linking it again changes: its owners and
// members get the teamspace's roles. It reports whether the page is one.
func populatePublicMutationTeamspaceUnlink(app core.App, authorization pageAuthorization, state *publicMutationState) (bool, error) {
	unlinked, err := pageTeamspaceDefaultUnlinked(app, state.WorkspaceID, authorization.Page)
	if err != nil || !unlinked {
		return false, err
	}
	teamspaceID := authorization.Page.GetString("teamspace_id")
	_, teamspace, _, err := authorizeTeamspace(app, authorization.Workspace.PrincipalID, state.WorkspaceID, teamspaceID, false)
	if errors.Is(err, errTeamspaceNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	roles, _, err := pageOwnTeamspaceRoles(app, state.WorkspaceID, teamspaceID, authorization.Page.Id)
	if err != nil {
		return false, err
	}
	changes := []publicMutationTeamspaceRelinkChange{}
	if roles.OwnerRole != teamspacePageRoleFullAccess {
		changes = append(changes, publicMutationTeamspaceRelinkChange{
			Group: pageTeamspaceGroupOwner, FromRole: roles.OwnerRole, ToRole: teamspacePageRoleFullAccess,
		})
	}
	if roles.MemberRole != teamspace.MemberPageRole {
		changes = append(changes, publicMutationTeamspaceRelinkChange{
			Group: pageTeamspaceGroupMember, FromRole: roles.MemberRole, ToRole: teamspace.MemberPageRole,
		})
	}
	state.ShareUnlink = &publicMutationShareUnlink{
		ParentID: teamspace.ID, ParentTitle: teamspace.Name, Teamspace: true,
		Changes: []publicMutationRelinkChange{}, TeamspaceChanges: changes,
	}
	return true, nil
}

// relinkPageTeamspaceDefault gives a page at its teamspace's top unlinked
// from the teamspace's default the teamspace's roles again, reporting
// whether it was one.
func relinkPageTeamspaceDefault(app core.App, authorization pageAuthorization) (bool, error) {
	workspaceID := authorization.Workspace.WorkspaceID
	unlinked, err := pageTeamspaceDefaultUnlinked(app, workspaceID, authorization.Page)
	if err != nil || !unlinked {
		return false, err
	}
	return true, deletePageTeamspaceRoles(app, workspaceID, authorization.Page.Id)
}
