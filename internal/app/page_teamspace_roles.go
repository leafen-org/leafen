package app

import (
	"database/sql"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A teamspace page's share window lists the teamspace's owners and members,
// each with their role on the page, as Notion's (seen 2026-10-12):
// "〈팀스페이스〉의 멤버 · 팀스페이스 · N명의 사용자" opens to "소유자 · 팀
// 소유자 N명" and "멤버 · 멤버 N명", and changing either ignores what the
// pages above give — below the teamspace's top, once "변경 후 연결 해제" is
// confirmed. The roles sit on the page that holds the page's sharing: the
// page itself when it is unlinked from its parent's or at the teamspace's
// top, else the nearest such page above it (page_access_ancestors.go). A
// workspace owner keeps full access, and nobody changes the group they
// themselves are in.

const (
	pageTeamspaceGroupOwner  = "owner"
	pageTeamspaceGroupMember = "member"
)

type storedPageTeamspaceRoles struct {
	PageID     string `db:"page_id"`
	OwnerRole  string `db:"owner_role"`
	MemberRole string `db:"member_role"`
}

// pageTeamspaceRoles reads the roles a teamspace's owners and members have on
// a page of it, where the page holding its sharing sets them.
func pageTeamspaceRoles(app core.App, workspaceID, teamspaceID, pageID string) (storedPageTeamspaceRoles, bool, error) {
	// Whether any page of the teamspace sets roles, asked once a read scope
	// (a search asked it for each page it found).
	any, err := remembered(app, teamspaceRolesMemoKey{workspaceID: workspaceID, teamspaceID: teamspaceID}, func() (bool, error) {
		var any bool
		err := app.DB().NewQuery(`
			SELECT EXISTS (SELECT 1 FROM {{leafen_page_teamspace_roles}}
				WHERE [[workspace_id]]={:workspaceId} AND [[teamspace_id]]={:teamspaceId})
		`).Bind(dbx.Params{"workspaceId": workspaceID, "teamspaceId": teamspaceID}).Row(&any)
		return any, err
	})
	if err != nil || !any {
		return storedPageTeamspaceRoles{}, false, err
	}
	ids, err := loadPageAccessAncestorIDs(app, workspaceID, pageID)
	if err != nil {
		return storedPageTeamspaceRoles{}, false, err
	}
	roles := storedPageTeamspaceRoles{}
	err = app.DB().NewQuery(`
		SELECT [[page_id]],[[owner_role]],[[member_role]] FROM {{leafen_page_teamspace_roles}}
		WHERE [[page_id]]={:pageId} AND [[workspace_id]]={:workspaceId} AND [[teamspace_id]]={:teamspaceId}
	`).Bind(dbx.Params{"pageId": ids[len(ids)-1], "workspaceId": workspaceID, "teamspaceId": teamspaceID}).One(&roles)
	if errors.Is(err, sql.ErrNoRows) {
		return storedPageTeamspaceRoles{}, false, nil
	}
	return roles, err == nil, err
}

// pageTeamspaceGroupRoles is what a teamspace's owners and members have on a
// page of it: the page's own roles, or the teamspace's.
func pageTeamspaceGroupRoles(app core.App, workspaceID string, teamspace storedTeamspace, pageID string) (string, string, error) {
	roles, found, err := pageTeamspaceRoles(app, workspaceID, teamspace.ID, pageID)
	if err != nil || !found {
		return teamspacePageRoleFullAccess, teamspace.MemberPageRole, err
	}
	return roles.OwnerRole, roles.MemberRole, nil
}

// pageTeamspaceChangeUnlinks says whether changing a teamspace group's role on
// a page cuts its sharing from its parent's — a page under another page that
// still follows it — or from the teamspace's default, a page at the
// teamspace's top still following it (page_teamspace_default_unlink.go);
// and whether a parent page is what it leaves.
func pageTeamspaceChangeUnlinks(app core.App, workspaceID string, page *core.Record) (unlinks bool, fromParent bool, err error) {
	ids, err := loadPageAccessAncestorIDs(app, workspaceID, page.Id)
	if err != nil || len(ids) > 1 {
		return len(ids) > 1, true, err
	}
	top, err := pageAtTeamspaceTop(app, workspaceID, page)
	if err != nil || !top {
		return false, false, err
	}
	_, own, err := pageOwnTeamspaceRoles(app, workspaceID, page.GetString("teamspace_id"), page.Id)
	return !own, false, err
}

// errPageTeamspaceRoleSame is a teamspace group given the role it has.
var errPageTeamspaceRoleSame = publicMutationRefused("role is the group's role on the page already")

func setPageTeamspaceRole(
	app core.App, authorization pageAuthorization, input publicMutationShareInheritanceInput, now time.Time,
) error {
	workspaceID, pageID := authorization.Workspace.WorkspaceID, authorization.Page.Id
	teamspaceID := authorization.Page.GetString("teamspace_id")
	if teamspaceID == "" {
		return publicMutationRefused("the page isn't in a teamspace, so it has no teamspace groups to give a role")
	}
	actorAuthority, teamspace, decision, err := authorizeTeamspace(
		app, authorization.Workspace.PrincipalID, workspaceID, teamspaceID, false,
	)
	if errors.Is(err, errTeamspaceNotFound) || err == nil && teamspace.ArchivedAt != 0 {
		return publicMutationRefused("the page's teamspace is archived or gone")
	}
	if err != nil {
		return err
	}
	// Nobody changes the role of a group they are in, past a workspace owner.
	if actorAuthority.Workspace.Role != workspaceRoleOwner && decision.Joined &&
		(decision.MembershipRole == teamspaceMemberRoleOwner) == (input.PrincipalID == pageTeamspaceGroupOwner) {
		return publicMutationRefused("principalId is your own group: only a workspace owner changes the role of a group they are in")
	}
	ownerRole, memberRole, err := pageTeamspaceGroupRoles(app, workspaceID, teamspace, pageID)
	if err != nil {
		return err
	}
	if input.PrincipalID == pageTeamspaceGroupOwner {
		if ownerRole == input.Role {
			return errPageTeamspaceRoleSame
		}
		ownerRole = input.Role
	} else {
		if memberRole == input.Role {
			return errPageTeamspaceRoleSame
		}
		memberRole = input.Role
	}
	cuts, fromParent, err := pageTeamspaceChangeUnlinks(app, workspaceID, authorization.Page)
	if err != nil {
		return err
	}
	// At the teamspace's top the roles the page now holds are what unlink it.
	if cuts && fromParent {
		if err := cutPageShareInheritance(app, authorization, now); err != nil {
			return err
		}
	}
	_, err = app.DB().NewQuery(`
		INSERT INTO {{leafen_page_teamspace_roles}}
			([[page_id]],[[workspace_id]],[[teamspace_id]],[[owner_role]],[[member_role]],[[updated_by]],[[updated_at]])
		VALUES ({:pageId},{:workspaceId},{:teamspaceId},{:ownerRole},{:memberRole},{:actorId},{:now})
		ON CONFLICT ([[page_id]]) DO UPDATE SET
			[[teamspace_id]]=excluded.[[teamspace_id]],[[owner_role]]=excluded.[[owner_role]],
			[[member_role]]=excluded.[[member_role]],[[updated_by]]=excluded.[[updated_by]],[[updated_at]]=excluded.[[updated_at]]
	`).Bind(dbx.Params{
		"pageId": pageID, "workspaceId": workspaceID, "teamspaceId": teamspaceID,
		"ownerRole": ownerRole, "memberRole": memberRole,
		"actorId": authorization.Workspace.PrincipalID, "now": now.UnixMilli(),
	}).Execute()
	return err
}

// copyPageTeamspaceRoles gives a page being unlinked the teamspace roles of
// the page that held its sharing, so nothing changes for anyone.
func copyPageTeamspaceRoles(app core.App, authorization pageAuthorization, holderID string, now time.Time) error {
	workspaceID, pageID := authorization.Workspace.WorkspaceID, authorization.Page.Id
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_page_teamspace_roles}}
			([[page_id]],[[workspace_id]],[[teamspace_id]],[[owner_role]],[[member_role]],[[updated_by]],[[updated_at]])
		SELECT {:pageId},[[workspace_id]],[[teamspace_id]],[[owner_role]],[[member_role]],{:actorId},{:now}
		FROM {{leafen_page_teamspace_roles}}
		WHERE [[page_id]]={:holderId} AND [[workspace_id]]={:workspaceId} AND [[teamspace_id]]={:teamspaceId}
		ON CONFLICT ([[page_id]]) DO NOTHING
	`).Bind(dbx.Params{
		"pageId": pageID, "holderId": holderID, "workspaceId": workspaceID,
		"teamspaceId": authorization.Page.GetString("teamspace_id"),
		"actorId":     authorization.Workspace.PrincipalID, "now": now.UnixMilli(),
	}).Execute()
	return err
}

func deletePageTeamspaceRoles(app core.App, workspaceID, pageID string) error {
	_, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_page_teamspace_roles}} WHERE [[workspace_id]]={:workspaceId} AND [[page_id]]={:pageId}
	`).Bind(dbx.Params{"workspaceId": workspaceID, "pageId": pageID}).Execute()
	return err
}

// pageTeamspaceViewerGroup is the group of the teamspace a person is in, whose
// role they leave alone; a workspace owner changes either.
func pageTeamspaceViewerGroup(app core.App, principalID, workspaceID, teamspaceID string) string {
	authority, _, decision, err := authorizeTeamspace(app, principalID, workspaceID, teamspaceID, false)
	if err != nil || authority.Workspace.Role == workspaceRoleOwner || !decision.Joined {
		return ""
	}
	if decision.MembershipRole == teamspaceMemberRoleOwner {
		return pageTeamspaceGroupOwner
	}
	return pageTeamspaceGroupMember
}

// pageTeamspaceRelinkChanges is how linking an unlinked teamspace page again
// changes its teamspace's owners' and members' roles: to those of its parent.
func pageTeamspaceRelinkChanges(
	app core.App, authorization pageAuthorization, parentID string,
) ([]publicMutationTeamspaceRelinkChange, error) {
	changes := []publicMutationTeamspaceRelinkChange{}
	workspaceID, teamspaceID := authorization.Workspace.WorkspaceID, authorization.Page.GetString("teamspace_id")
	if teamspaceID == "" {
		return changes, nil
	}
	_, teamspace, _, err := authorizeTeamspace(app, authorization.Workspace.PrincipalID, workspaceID, teamspaceID, false)
	if errors.Is(err, errTeamspaceNotFound) {
		return changes, nil
	}
	if err != nil {
		return nil, err
	}
	ownerRole, memberRole, err := pageTeamspaceGroupRoles(app, workspaceID, teamspace, authorization.Page.Id)
	if err != nil {
		return nil, err
	}
	parentOwnerRole, parentMemberRole, err := pageTeamspaceGroupRoles(app, workspaceID, teamspace, parentID)
	if err != nil {
		return nil, err
	}
	if ownerRole != parentOwnerRole {
		changes = append(changes, publicMutationTeamspaceRelinkChange{
			Group: pageTeamspaceGroupOwner, FromRole: ownerRole, ToRole: parentOwnerRole,
		})
	}
	if memberRole != parentMemberRole {
		changes = append(changes, publicMutationTeamspaceRelinkChange{
			Group: pageTeamspaceGroupMember, FromRole: memberRole, ToRole: parentMemberRole,
		})
	}
	return changes, nil
}
