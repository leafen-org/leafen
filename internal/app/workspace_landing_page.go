package app

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A workspace's landing page, as Notion's "사용자 지정 랜딩 페이지" (seen
// 2026-10-05): the page a person who joins the workspace opens first. The
// owner chooses one of the workspace's pages; a member who can't read it, or
// who has opened pages there before, starts as usual (useStartPage).

const workspaceLandingPageField = "landing_page_id"

// errWorkspaceLandingPageHidden refuses a landing page someone who joins
// the workspace couldn't open.
var errWorkspaceLandingPageHidden = errors.New(errorCodeWorkspaceLandingPageHidden)

// workspaceLandingPageUsable reports whether a page can be the workspace's
// landing page: one of its pages, out of the trash, that its owner reads.
// One that someone joining couldn't open is refused by
// errWorkspaceLandingPageHidden.
func workspaceLandingPageUsable(app core.App, ownerID, workspaceID, pageID string) (bool, error) {
	authorization, err := authorizePage(app, ownerID, pageID, permissionWorkspaceRead)
	if isPermissionDenied(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if authorization.Workspace.WorkspaceID != workspaceID || authorization.Page.GetBool("notion_import_staging") {
		return false, nil
	}
	trashed, err := pageInTrashPath(app, pageID)
	if err != nil || trashed {
		return false, err
	}
	open, err := workspaceLandingPageOpenToNewcomers(app, ownerID, workspaceID, authorization.Page)
	if err == nil && !open {
		err = errWorkspaceLandingPageHidden
	}
	return open, err
}

// workspaceLandingPageOpenToNewcomers reports whether someone who joins the
// workspace opens the page, as Notion asks of its landing page (seen
// 2026-11-14: a private page, shared with one person or with no one, and a
// page of a teamspace whose other members have no access are refused, as
// some of the workspace couldn't see them; a page of
// a default or an open teamspace is taken, whoever has joined it). Someone
// joining is a member in no group and no teamspace of their own,
// nothing shared with them by name: the page's teamspace gives them a role
// on it as it gives every member (decideTeamspaceAccess), or a share with
// the whole workspace reaches it.
func workspaceLandingPageOpenToNewcomers(app core.App, ownerID, workspaceID string, page *core.Record) (bool, error) {
	app = withAccessMemo(app)
	// A database's item opens through the page holding its database, however
	// many items deep (parent_chain.go).
	holder := page
	for chain := (parentChain[string]{}); isDatabaseRowPageRecord(holder) && chain.climb(holder.Id); {
		aboveID, err := databaseRowPageAbove(app, holder)
		if isPermissionDenied(err) || errors.Is(err, sql.ErrNoRows) {
			break
		}
		if err != nil {
			return false, err
		}
		if holder, err = findRecordRemembered(app, pagesCollection, aboveID); err != nil {
			return false, err
		}
	}
	if teamspaceID := holder.GetString("teamspace_id"); teamspaceID != "" && !isDatabaseRowPageRecord(holder) {
		role, err := newcomerTeamspaceRoleOnPage(app, ownerID, workspaceID, teamspaceID, holder.Id)
		if err != nil || role >= pageAccessRoleView {
			return err == nil, err
		}
	}
	return workspaceShareReaches(app, workspaceID, page.Id)
}

// newcomerTeamspaceRoleOnPage is the role a teamspace gives someone who
// joins the workspace on one of its pages: a default teamspace's members'
// role, where the page's sharing sets it too, else an open teamspace's role
// for the rest of the workspace.
func newcomerTeamspaceRoleOnPage(app core.App, ownerID, workspaceID, teamspaceID, pageID string) (pageAccessRole, error) {
	authority, teamspace, _, err := authorizeTeamspaceInvited(app, ownerID, workspaceID, teamspaceID, false, true)
	if errors.Is(err, errTeamspaceNotFound) {
		return pageAccessRoleNone, nil
	}
	if err != nil {
		return pageAccessRoleNone, err
	}
	settings, err := loadTeamspaceSettings(app, authority)
	if err != nil {
		return pageAccessRoleNone, err
	}
	newcomer := teamspaceAuthority{Workspace: workspaceAuthorization{
		WorkspaceID: workspaceID, OwnerID: authority.Workspace.OwnerID,
		WorkspaceOwnerID: authority.Workspace.WorkspaceOwnerID, Role: workspaceRoleMember,
	}}
	decision := decideTeamspaceAccess(teamspace, settings, nil, nil, newcomer)
	if decision.Joined {
		roles, found, err := pageTeamspaceRoles(app, workspaceID, teamspaceID, pageID)
		if err != nil {
			return pageAccessRoleNone, err
		}
		if found {
			decision.PageRole = roles.MemberRole
		}
	}
	return pageAccessRoleFromTeamspaceRole(decision.PageRole), nil
}

// workspaceShareReaches reports whether a share with the whole workspace,
// on the page or on one above it whose shares reach it, lets its members
// open it.
func workspaceShareReaches(app core.App, workspaceID, pageID string) (bool, error) {
	pageIDs, err := loadPageAccessAncestorIDs(app, workspaceID, pageID)
	if err != nil || len(pageIDs) == 0 {
		return false, err
	}
	params := dbx.Params{"workspaceId": workspaceID}
	placeholders := make([]string, 0, len(pageIDs))
	for index, id := range pageIDs {
		key := fmt.Sprintf("landingPage%d", index)
		params[key] = id
		placeholders = append(placeholders, "{:"+key+"}")
	}
	var shared bool
	err = app.DB().NewQuery(`
		SELECT EXISTS (SELECT 1 FROM {{leafen_page_permissions}}
			WHERE [[workspace_id]]={:workspaceId} AND [[principal_type]]='workspace'
				AND [[principal_id]]={:workspaceId} AND [[page_id]] IN (` + joinSQL(placeholders) + `))
	`).Bind(params).Row(&shared)
	return shared, err
}
