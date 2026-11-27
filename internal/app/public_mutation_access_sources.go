package app

import (
	"context"
	"database/sql"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	publicMutationOtherAccessTeamspace = "teamspace"
	publicMutationOtherAccessPage      = "page"
)

// publicMutationRoleFor names a page access role as a share's role.
func publicMutationRoleFor(role pageAccessRole) string {
	switch role {
	case pageAccessRoleFullAccess:
		return publicMutationRoleFullAccess
	case pageAccessRoleEdit:
		return publicMutationRoleEdit
	case pageAccessRoleComment:
		return publicMutationRoleComment
	case pageAccessRoleView:
		return publicMutationRoleView
	default:
		return ""
	}
}

// populatePublicMutationAccessSources adds what the share list shows beside
// its own shares, as Notion's does: the teamspace a page belongs to, with
// how many people it holds and the role it gives them, and for each person
// shared with whether they would still open the page without that share.
func populatePublicMutationAccessSources(
	ctx context.Context,
	app core.App,
	authorization pageAuthorization,
	state *publicMutationState,
) error {
	if authorization.Workspace.Workspace != nil {
		state.WorkspaceName = authorization.Workspace.Workspace.GetString("name")
	}
	var teamspace *storedTeamspace
	if teamspaceID := authorization.Page.GetString("teamspace_id"); teamspaceID != "" {
		stored := storedTeamspace{}
		err := app.DB().NewQuery(`
			SELECT [[id]],[[workspace_id]],[[name]],[[icon]],[[description]],[[access]],
				[[member_page_role]],[[open_page_role]],[[outside_access]],[[disable_publishing]],[[disable_guests]],[[disable_export]],[[members_can_invite]],
				[[members_can_edit_sidebar]],[[archived_at]],[[revision]],[[created_by]],
				[[updated_by]],[[created_at]],[[updated_at]]
			FROM {{leafen_teamspaces}}
			WHERE [[id]]={:id} AND [[workspace_id]]={:workspaceId} AND [[deleted_at]]=0
		`).WithContext(ctx).Bind(dbx.Params{"id": teamspaceID, "workspaceId": state.WorkspaceID}).One(&stored)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil && stored.ArchivedAt == 0 {
			teamspace = &stored
			summaries, err := readTeamspaceSummaries(ctx, app, state.WorkspaceID, []string{stored.ID})
			if err != nil {
				return err
			}
			count, owners := 0, 0
			if len(summaries) > 0 {
				count, owners = summaries[0].MemberCount, summaries[0].OwnerCount
			}
			openRole := ""
			if role := teamspaceOutsideRole(stored); role != teamspacePageRoleNone {
				openRole = role
			}
			ownerRole, memberRole, err := pageTeamspaceGroupRoles(app, state.WorkspaceID, stored, authorization.Page.Id)
			if err != nil {
				return err
			}
			unlinks, _, err := pageTeamspaceChangeUnlinks(app, state.WorkspaceID, authorization.Page)
			if err != nil {
				return err
			}
			state.Teamspace = &publicMutationTeamspace{
				ID: stored.ID, Name: stored.Name, Icon: stored.Icon, MemberCount: count, OwnerCount: owners,
				Role: memberRole, OwnerRole: ownerRole, OpenRole: openRole, ChangeUnlinks: unlinks,
				ViewerGroup: pageTeamspaceViewerGroup(app, authorization.Workspace.PrincipalID, state.WorkspaceID, stored.ID),
			}
		}
	}
	parentID, parentTitle, parentDatabase, err := publicMutationPageParent(ctx, app, state.WorkspaceID, authorization.Page.Id)
	if err != nil {
		return err
	}
	chain, err := loadPageAccessAncestorIDs(app, state.WorkspaceID, authorization.Page.Id)
	if err != nil {
		return err
	}
	for index, permission := range state.Permissions {
		if permission.PrincipalType != publicMutationPrincipalUser {
			continue
		}
		other, err := publicMutationOtherAccessFor(
			app, state.WorkspaceID, teamspace, authorization.Page.Id,
			publicMutationParent{ID: parentID, Title: parentTitle, Database: parentDatabase}, permission.PrincipalID,
		)
		if err != nil {
			return err
		}
		state.Permissions[index].OtherAccess = other
	}
	return populatePublicMutationWorkspaceAccess(ctx, app, authorization, chain, state)
}

// publicMutationParent is the page above a page, as publicMutationPageParent
// names it.
type publicMutationParent struct {
	ID, Title string
	Database  bool
}

// publicMutationOtherAccessFor is the widest access a person has to a page
// besides its own shares: their teamspace's, or a share on a page above it.
// That page says whether it is a database, which an untitled one is called
// by ("새 데이터베이스").
func publicMutationOtherAccessFor(
	app core.App,
	workspaceID string,
	teamspace *storedTeamspace,
	pageID string,
	parent publicMutationParent,
	principalID string,
) (*publicMutationOtherAccess, error) {
	var other *publicMutationOtherAccess
	best := pageAccessRoleNone
	if teamspace != nil {
		_, role, err := effectiveTeamspaceRoleOnPage(app, principalID, workspaceID, teamspace.ID, pageID)
		if err != nil && !isPermissionDenied(err) {
			return nil, err
		}
		if role > best {
			best = role
			other = &publicMutationOtherAccess{
				Source: publicMutationOtherAccessTeamspace, Label: teamspace.Name, Role: publicMutationRoleFor(role),
			}
		}
	}
	if parent.ID != "" {
		role, err := effectiveUserPageAccessRole(app, workspaceID, parent.ID, principalID)
		if err != nil && !isPermissionDenied(err) {
			return nil, err
		}
		if role > best {
			other = &publicMutationOtherAccess{
				Source: publicMutationOtherAccessPage, Label: parent.Title, Role: publicMutationRoleFor(role),
				Database: parent.Database,
			}
		}
	}
	return other, nil
}

// publicMutationPageParent names the page above a page: its ID, its title,
// and whether it is a database, which an untitled one is called by.
func publicMutationPageParent(ctx context.Context, app core.App, workspaceID, pageID string) (string, string, bool, error) {
	row := struct {
		ParentID       string `db:"parent_id"`
		ParentTitle    string `db:"parent_title"`
		ParentDatabase bool   `db:"parent_database"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[parent]].[[id]] AS [[parent_id]],[[parent]].[[title]] AS [[parent_title]],
			[[parent]].[[kind]] = '` + pageKindDatabase + `' AS [[parent_database]]
		FROM {{leafen_pages}} [[page]]
		JOIN {{leafen_pages}} [[parent]] ON [[parent]].[[id]]=` + pageAncestryParentSQL("[[page]]") + `
			AND [[parent]].[[workspace]]={:workspaceId}
		WHERE [[page]].[[id]]={:pageId} AND [[page]].[[workspace]]={:workspaceId}
	`).WithContext(ctx).Bind(dbx.Params{"pageId": pageID, "workspaceId": workspaceID}).One(&row)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", false, nil
	}
	return row.ParentID, row.ParentTitle, row.ParentDatabase, err
}
