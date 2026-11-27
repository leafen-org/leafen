package app

import (
	"cmp"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type pageAccessRole int

const (
	pageAccessRoleNone pageAccessRole = iota
	pageAccessRoleView
	pageAccessRoleComment
	pageAccessRoleEdit
	pageAccessRoleFullAccess
)

type storedPageAccessAncestor struct {
	ID       string `db:"id"`
	ParentID string `db:"parent_id"`
}

func pageAccessRoleForWorkspaceRequirement(requirement permissionRequirement) pageAccessRole {
	switch requirement {
	case permissionWorkspaceRead:
		return pageAccessRoleView
	case permissionWorkspaceWrite:
		return pageAccessRoleEdit
	case permissionWorkspaceOwner:
		return pageAccessRoleFullAccess
	default:
		return pageAccessRoleNone
	}
}

func workspacePageAccessRole(authorization workspaceAuthorization) pageAccessRole {
	if authorization.Role == workspaceRoleOwner {
		return pageAccessRoleFullAccess
	}
	if authorization.Role == workspaceRoleMember {
		return pageAccessRoleEdit
	}
	return pageAccessRoleNone
}

// privatePageAccessRole is what a page outside every teamspace gives by
// itself: its owner full access and nobody else anything, workspace owners
// and admins included; others need a share.
func privatePageAccessRole(page *core.Record, principalID string) pageAccessRole {
	if owner := page.GetString(privatePageOwnerField); owner != "" && owner == principalID {
		return pageAccessRoleFullAccess
	}
	return pageAccessRoleNone
}

func pageAccessRoleFromTeamspaceRole(role string) pageAccessRole {
	switch role {
	case teamspacePageRoleView:
		return pageAccessRoleView
	case teamspacePageRoleComment:
		return pageAccessRoleComment
	case teamspacePageRoleEdit:
		return pageAccessRoleEdit
	case teamspacePageRoleFullAccess:
		return pageAccessRoleFullAccess
	default:
		return pageAccessRoleNone
	}
}

func effectiveTeamspacePageAccessRole(
	app core.App,
	principalID string,
	workspaceID string,
	teamspaceID string,
) (workspaceAuthorization, pageAccessRole, error) {
	return effectiveTeamspaceRoleOnPage(app, principalID, workspaceID, teamspaceID, "")
}

// effectiveTeamspaceRoleOnPage is the role a teamspace gives on one of its
// pages: its owners' and members' roles there where the page's sharing sets
// them (page_teamspace_roles.go), else the teamspace's own. An empty page
// asks for the teamspace's own, as for putting a page at its top, which an
// archived teamspace gives no one.
//
// The workspace's owner has full access where they are in it, else what
// anyone outside has (none in a private one: Notion's "won't be visible to
// people who are not added"; its Enterprise owner joins first, Manage
// teamspaces — privilegedTeamspaceSelfMembership). An archived one's pages
// stay open to those in it by themselves or a group, as before, as Notion's
// (measured 2026-11-13: its owner and a restricted member in it opened and
// edited them; a restricted member outside it was refused).
func effectiveTeamspaceRoleOnPage(
	app core.App,
	principalID, workspaceID, teamspaceID, pageID string,
) (workspaceAuthorization, pageAccessRole, error) {
	// Read whether or not the person sees the teamspace: one they don't see
	// gives them nothing below unless it is archived and they are in it.
	authority, teamspace, decision, err := authorizeTeamspaceInvited(
		app, principalID, workspaceID, teamspaceID, false, true,
	)
	if errors.Is(err, errTeamspaceNotFound) {
		return workspaceAuthorization{}, pageAccessRoleNone, permissionDenied()
	}
	if err != nil {
		return workspaceAuthorization{}, pageAccessRoleNone, err
	}
	if teamspace.ArchivedAt == 0 && !decision.Visible {
		return workspaceAuthorization{}, pageAccessRoleNone, permissionDenied()
	}
	if teamspace.ArchivedAt != 0 {
		if pageID == "" || !archivedTeamspaceKeepsMember(decision) {
			return workspaceAuthorization{}, pageAccessRoleNone, permissionDenied()
		}
		decision.PageRole = archivedTeamspacePageRole(teamspace, decision)
	}
	if authority.Workspace.Role == workspaceRoleOwner && decision.Joined {
		return authority.Workspace, pageAccessRoleFullAccess, nil
	}
	if pageID != "" && decision.Joined {
		roles, found, err := pageTeamspaceRoles(app, workspaceID, teamspaceID, pageID)
		if err != nil {
			return workspaceAuthorization{}, pageAccessRoleNone, err
		}
		if found && decision.MembershipRole == teamspaceMemberRoleOwner {
			decision.PageRole = roles.OwnerRole
		} else if found {
			decision.PageRole = roles.MemberRole
		}
	}
	role := pageAccessRoleFromTeamspaceRole(decision.PageRole)
	if role == pageAccessRoleNone {
		return workspaceAuthorization{}, pageAccessRoleNone, permissionDenied()
	}
	return authority.Workspace, role, nil
}

func authorizePageAccessRole(
	app core.App,
	principalID string,
	pageID string,
	minimum pageAccessRole,
) (pageAuthorization, error) {
	return authorizePageAccessRoleWithOwnedCopy(app, principalID, pageID, minimum, "")
}

// An owned copy's receipt can be polled while that exact job fences its page.
// All page grants and every other fence remain authoritative.
func authorizePageAccessRoleWithOwnedCopy(app core.App, principalID, pageID string, minimum pageAccessRole, ownedCopyJobID string) (pageAuthorization, error) {
	// One decision asks its questions once even in a write: it writes nothing, and a write
	// decides afresh at its next one (2026-10-17: a block write's decision read the workspace thrice).
	app = withAccessMemo(app)
	return remembered(app, pageAccessMemoKey{principalID: principalID, pageID: pageID, ownedCopyJobID: ownedCopyJobID, minimum: minimum},
		func() (pageAuthorization, error) {
			return decidePageAccessRole(app, principalID, pageID, minimum, ownedCopyJobID)
		})
}

func decidePageAccessRole(app core.App, principalID, pageID string, minimum pageAccessRole, ownedCopyJobID string) (pageAuthorization, error) {
	if principalID == "" || pageID == "" || minimum <= pageAccessRoleNone {
		return pageAuthorization{}, permissionDenied()
	}
	page, fences, err := loadAccessPage(app, pageID, minimum)
	if err != nil {
		return pageAuthorization{}, err
	}
	// An operation holding the page refuses it to everyone, as held from
	// whoever has the role otherwise (page_access_hold.go).
	held := ""
	if fences.Moving || ownedCopyJobID != "" {
		fenced, err := blockMovePageFenceExceptOwnedCopy(app, principalID, pageID, ownedCopyJobID)
		if err != nil {
			return pageAuthorization{}, err
		}
		if fenced {
			held = pageBusyMoving
		}
	}
	// Its move to another workspace holds it until it is there, or back.
	if held == "" && fences.WorkspaceMoving {
		moving, err := pageWorkspaceMoveHeldPage(app, pageID)
		if err != nil {
			return pageAuthorization{}, err
		}
		if moving {
			held = pageBusyWorkspaceMoving
		}
	}
	// People's database and its rows open by their own rule (people_database_access.go).
	if authorization, handled, err := authorizeIfPeoplePage(app, principalID, page, minimum); handled {
		return heldDecision(held, authorization, err)
	}
	// A row's page opens through its data source's database page or its own
	// shares, for reading; row page paths write through authorizeRowPageRecord.
	if isDatabaseRowPageRecord(page) {
		if minimum > pageAccessRoleView {
			return pageAuthorization{}, permissionDenied()
		}
		authorization, err := authorizeRowPageRecord(app, principalID, page, minimum)
		return heldDecision(held, authorization, err)
	}
	workspace, stateHeld, err := pageStateAdmitsHeld(app, page, minimum, fences)
	if err != nil {
		return pageAuthorization{}, err
	}
	authorization, err := decidePersonPageRole(app, principalID, page, workspace, minimum)
	return heldDecision(cmp.Or(held, stateHeld), authorization, err)
}

// decidePersonPageRole is a person's role on a page other than a row's or
// People's, once its state lets anyone have one (pageStateAdmits): the
// widest its teamspace or its owner and its shares give, as Notion's — a
// teamspace member who views the page and is shared it with full access
// has full access (2026-11-21: they could not make it a wiki, and its share
// window offered them no change). The minimum asked only refuses: the role
// is the person's whatever was asked (pageAuthorization.AccessRole).
func decidePersonPageRole(
	app core.App, principalID string, page, workspace *core.Record, minimum pageAccessRole,
) (pageAuthorization, error) {
	pageID, workspaceID := page.Id, page.GetString("workspace")
	workspaceAuthorization, workspaceErr := authorizeWorkspace(
		app, principalID, workspaceID, permissionWorkspaceRead,
	)
	if workspaceErr == nil {
		role := privatePageAccessRole(page, principalID)
		if teamspaceID := page.GetString("teamspace_id"); teamspaceID != "" {
			workspaceAuthorization, role, workspaceErr = effectiveTeamspaceRoleOnPage(
				app, principalID, workspaceID, teamspaceID, pageID,
			)
		}
		if workspaceErr != nil && !isPermissionDenied(workspaceErr) {
			return pageAuthorization{}, workspaceErr
		}
		if role >= minimum {
			source := memberPageAccessSource(page)
			if shared, wider, err := pageSharesWiden(app, workspaceID, pageID, principalID, role); err != nil {
				return pageAuthorization{}, err
			} else if wider {
				role, source = shared, pageAccessSource{Kind: pageAccessFromShares}
			}
			return pageAuthorization{
				Workspace: workspaceAuthorization, Page: page, AccessRole: role, Source: source,
			}, nil
		}
	} else if !isPermissionDenied(workspaceErr) {
		return pageAuthorization{}, workspaceErr
	}
	pageGrantWorkspace, err := authorizePageGrantWorkspace(app, principalID, workspace)
	// A guest the organization asks for 2-step verification hears so only of
	// a page shared with them; any other page is not found.
	var guestMFAErr error
	if isGuestMFARequired(err) {
		guestMFAErr, err = err, nil
	}
	if err != nil {
		return pageAuthorization{}, err
	}
	role, err := effectiveUserPageAccessRole(app, workspaceID, pageID, principalID)
	if err != nil {
		return pageAuthorization{}, err
	}
	// A guest waiting to be a member owns their private pages meanwhile
	// (pending_guest_private_pages.go).
	if role < minimum && privatePageAccessRole(page, principalID) == pageAccessRoleFullAccess {
		keeps, err := pendingGuestPrivatePages(app, principalID, workspace)
		if err != nil {
			return pageAuthorization{}, err
		}
		if keeps {
			role = pageAccessRoleFullAccess
		}
	}
	if role < minimum {
		return pageAuthorization{}, permissionDenied()
	}
	if guestMFAErr != nil {
		return pageAuthorization{}, guestMFAErr
	}
	return pageAuthorization{
		Workspace:  pageGrantWorkspace,
		Page:       page,
		AccessRole: role, Source: pageAccessSource{Kind: pageAccessFromShares},
	}, nil
}

// pageSharesWiden is the role a page's shares, and those above it whose
// sharing reaches it, give a person where it is wider than the role they
// hold there already; where they hold full access it reads nothing.
func pageSharesWiden(app core.App, workspaceID, pageID, principalID string, held pageAccessRole) (pageAccessRole, bool, error) {
	if held >= pageAccessRoleFullAccess {
		return held, false, nil
	}
	shared, err := effectiveUserPageAccessRole(app, workspaceID, pageID, principalID)
	if err != nil || shared <= held {
		return held, false, err
	}
	return shared, true, nil
}

func authorizePageGrantWorkspace(
	app core.App,
	principalID string,
	workspace *core.Record,
) (workspaceAuthorization, error) {
	if workspace == nil {
		return workspaceAuthorization{}, permissionDenied()
	}
	// Decided once a read scope: a member's graph read decided it for
	// reading, editing and commenting alike (2026-10-17).
	return remembered(app, pageGrantWorkspaceMemoKey{principalID, workspace.Id}, func() (workspaceAuthorization, error) {
		return decidePageGrantWorkspace(app, principalID, workspace)
	})
}

type pageGrantWorkspaceMemoKey struct{ principalID, workspaceID string }

func decidePageGrantWorkspace(
	app core.App,
	principalID string,
	workspace *core.Record,
) (workspaceAuthorization, error) {
	if principalID == "" || workspace.GetString("owner") == "" ||
		workspace.GetString("organization") == "" || workspace.GetString("deletion_job_id") != "" {
		return workspaceAuthorization{}, permissionDenied()
	}
	organization, err := authorizeOrganization(
		app, principalID, workspace.GetString("organization"), permissionOrganizationActive,
	)
	if err != nil {
		if isPermissionDenied(err) || err == nil {
			return workspaceAuthorization{}, permissionDenied()
		}
		return workspaceAuthorization{}, err
	}
	if err := authorizeGuestWorkspaceMFA(app, principalID, workspace); err != nil {
		return workspaceAuthorization{}, err
	}
	return workspaceAuthorizationForPageGrant(workspace, organization), nil
}

// authorizePageSetGrantWorkspace is authorizePageGrantWorkspace for a
// workspace named by its ID.
func authorizePageSetGrantWorkspace(
	app core.App,
	principalID string,
	workspaceID string,
) (workspaceAuthorization, error) {
	workspace, err := findRecordRemembered(app, workspacesCollection, workspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return workspaceAuthorization{}, permissionDenied()
	}
	if err != nil {
		return workspaceAuthorization{}, err
	}
	return authorizePageGrantWorkspace(app, principalID, workspace)
}

// authorizeWorkspaceReader admits a workspace's members, and anyone else
// active in its organization — a guest — to the reads whose every result
// is checked page by page, as search's and My tasks' are: a guest finds
// there only what is shared with them.
func authorizeWorkspaceReader(app core.App, principalID, workspaceID string) (workspaceAuthorization, error) {
	authorization, err := authorizeWorkspace(app, principalID, workspaceID, permissionWorkspaceRead)
	if !isPermissionDenied(err) {
		return authorization, err
	}
	return authorizePageSetGrantWorkspace(app, principalID, workspaceID)
}

func workspaceAuthorizationForPageGrant(
	workspace *core.Record,
	organization organizationAuthorization,
) workspaceAuthorization {
	return workspaceAuthorization{
		Workspace: workspace, Organization: organization,
		PrincipalID: organization.PrincipalID, WorkspaceID: workspace.Id,
		OwnerID: workspace.GetString("owner"), WorkspaceOwnerID: workspacePermissionOwnerID(workspace),
	}
}

// effectiveUserPageAccessRole is the role a page's shares and those above
// it give a person. Where nothing in the workspace is shared with them, nor
// reached through a link they came in by, it is none without reading the
// path above the page (2026-11-13: a search read the path and the shares of each of 1,500 shared private pages of
// another member one at a time for a workspace's owner).
func effectiveUserPageAccessRole(
	app core.App,
	workspaceID string,
	pageID string,
	principalID string,
) (pageAccessRole, error) {
	// Its questions — who the person is to shares, whether any reach them —
	// are asked once, even outside a read scope.
	app = withAccessMemo(app)
	shares, err := pageSharesExist(app, workspaceID, principalID)
	if err != nil || !shares {
		return pageAccessRoleNone, err
	}
	pageIDs, err := loadPageAccessAncestorIDs(app, workspaceID, pageID)
	if err != nil {
		return pageAccessRoleNone, err
	}
	return pageShareRoleOn(app, workspaceID, pageID, pageIDs, principalID)
}

// pageShareRoleOn is the widest role the shares of the given pages — a
// page and those above it whose shares reach it — and a web link the
// person came in through give them on the page. Where nothing in the
// workspace is shared with them, nor reached through a link they came in
// by (pageSharesExist), it is none without reading the pages' shares.
func pageShareRoleOn(app core.App, workspaceID, pageID string, pageIDs []string, principalID string) (pageAccessRole, error) {
	app = withAccessMemo(app)
	role, primed := primedPageShareRole(app, workspaceID, principalID, pageIDs)
	if !primed {
		shares, err := pageSharesExist(app, workspaceID, principalID)
		if err != nil || !shares {
			return pageAccessRoleNone, err
		}
		// The same pages' shares asked again in a read scope (a page read
		// for reading, editing and commenting) are asked once.
		key := pageShareRoleSetMemoKey{workspaceID, principalID, strings.Join(pageIDs, "\x00")}
		if role, err = remembered(app, key, func() (int, error) {
			return readPageShareRole(app, workspaceID, pageIDs, principalID)
		}); err != nil {
			return pageAccessRoleNone, err
		}
	}
	// A web link they came in through may give more.
	if role < int(pageAccessRoleEdit) {
		linkRole, err := pageLinkAccessRole(app, workspaceID, pageID, principalID)
		if err != nil {
			return pageAccessRoleNone, err
		}
		role = max(role, int(linkRole))
	}
	return pageAccessRole(role), nil
}

type pageShareRoleSetMemoKey struct{ workspaceID, principalID, pageIDs string }

// readPageShareRole is the widest role the shares of the pages give.
func readPageShareRole(app core.App, workspaceID string, pageIDs []string, principalID string) (int, error) {
	principals, err := loadPageSharePrincipals(app, workspaceID, principalID)
	if err != nil {
		return 0, err
	}
	params := dbx.Params{"workspaceId": workspaceID}
	placeholders := make([]string, 0, len(pageIDs))
	for index, ancestorID := range pageIDs {
		key := fmt.Sprintf("accessPage%d", index)
		params[key] = ancestorID
		placeholders = append(placeholders, "{:"+key+"}")
	}
	// A share names the person, a group of theirs or their email; the
	// widest role any of them gives is theirs.
	var role int
	err = app.DB().NewQuery(`
		SELECT COALESCE(MAX(CASE [[permission]].[[role]]
			WHEN 'view' THEN 1 WHEN 'comment' THEN 2
			WHEN 'edit' THEN 3 WHEN 'full_access' THEN 4 ELSE 0 END), 0)
		FROM {{leafen_page_permissions}} [[permission]]
		INDEXED BY [[idx_leafen_page_permissions_actor_page]]
		WHERE ` + principals.condition("[[permission]]", "workspaceId", params) + `
			AND [[permission]].[[page_id]] IN (` + joinSQL(placeholders) + `)
	`).Bind(params).Row(&role)
	if err != nil {
		return 0, err
	}
	if role < int(pageAccessRoleNone) || role > int(pageAccessRoleFullAccess) {
		return 0, permissionDenied()
	}
	return role, nil
}
