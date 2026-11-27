package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// Lowering or removing someone a page inherits from a page above it cuts the
// page's sharing from its parent's, as Notion's "변경 후 연결 해제" (seen
// 2026-10-11): the page keeps its own copy of every share it inherited, with
// that one change, and opens by those alone from then on
// (schema_baseline.json). Raising someone is a share of the page's own
// and needs no unlink; changing its teamspace's owners or members unlinks it
// too (page_teamspace_roles.go). "설정 복원" links it again: the copies the
// pages above cover go, its teamspace roles with them, and it opens as they
// say. Each moves the page's sharing revision on, as the share window's other
// changes do.

const (
	pageShareInheritanceUnlink    = "unlink"
	pageShareInheritanceRelink    = "relink"
	pageShareInheritanceTeamspace = "teamspace_role"
	// The web link a page has from a page above, changed on the page: its
	// sharing unlinks, keeping a copy of that link, which then takes the new
	// role, or goes with "none" (only those invited), as Notion's (seen
	// 2026-11-06: "역할을 변경하면 상위 페이지에서 설정된 권한을 무시하게
	// 됩니다").
	pageShareInheritanceWebRole = "web_role"
)

// errPageShareInheritanceCut is a page whose access is its own already, no
// longer the pages above's.
var errPageShareInheritanceCut = publicMutationRefused("the page's access is its own already, not the pages above's; relink it first")

func registerPageShareInheritanceRoute(event *core.ServeEvent) {
	event.Router.POST(apiRoot+publicMutationShareInheritancePath, mutatePageShareInheritance).
		Bind(apis.BodyLimit(publicMutationMaxBodyBytes))
	registerPageShareLeaveRoute(event)
}

func mutatePageShareInheritance(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	input := publicMutationShareInheritanceInput{}
	if err := event.BindBody(&input); err != nil {
		return publicMutationRouteError(event, bodyRefusal(errPublicMutationInvalid, "access inheritance change", err), "inheritance-input")
	}
	input.PrincipalType, input.PrincipalID = strings.TrimSpace(input.PrincipalType), strings.TrimSpace(input.PrincipalID)
	if input.PrincipalType == publicMutationPrincipalEmail {
		input.PrincipalID = normalizePageShareEmail(input.PrincipalID)
	}
	if err := pageShareInheritanceInputRefusal(input); err != nil {
		return publicMutationRouteError(event, err, "inheritance-input")
	}
	authorization, err := authorizePublicMutationManager(event.App, user.Id, pageID)
	if err == nil {
		err = refusePublicMutationInTrash(event.App, pageID)
	}
	if err != nil {
		return publicMutationRouteError(event, err, "inheritance-authorization")
	}
	release := acquireProductOperationLock(publicMutationLockNamespace, authorization.Workspace.WorkspaceID+"\x00"+pageID)
	defer release()
	ctx := event.Request.Context()
	response := publicMutationStateResponse{}
	change := func(txApp core.App) error {
		current, err := authorizePublicMutationManager(txApp, user.Id, pageID)
		if err != nil {
			return err
		}
		// A database item's page unlinks and links again as a page's does,
		// its teamspace's roles too: changing one on the item unlinks it
		// from its database's, as Notion's (seen 2026-11-11)
		// (public_mutation_row_inherited_access.go).
		row := isDatabaseRowPageRecord(current.Page)
		stored, err := loadStoredPublicMutationState(txApp, pageID)
		if errors.Is(err, sql.ErrNoRows) {
			stored = storedPublicMutationState{WorkspaceID: current.Workspace.WorkspaceID, PageID: pageID}
		} else if err != nil {
			return err
		}
		if stored.Revision != input.ExpectedRevision {
			return errPublicMutationRevisionConflict
		}
		now := time.Now().UTC()
		switch input.Action {
		case pageShareInheritanceUnlink:
			err = unlinkPageShareInheritance(txApp, current, input, now)
		case pageShareInheritanceTeamspace:
			err = setPageTeamspaceRole(txApp, current, input, now)
		case pageShareInheritanceWebRole:
			err = setInheritedWebLinkRole(txApp, current, input, now)
		default:
			err = relinkPageShareInheritance(txApp, current, now)
		}
		if err != nil {
			return err
		}
		if row {
			if err := announceRowShareChange(txApp, current.Page); err != nil {
				return err
			}
		}
		next := stored
		next.Revision, next.UpdatedBy, next.UpdatedAt = stored.Revision+1, current.Workspace.PrincipalID, now.UnixMilli()
		// Unlinking may have given the page a web link of its own.
		if next.ShareLinkID == "" {
			if err := txApp.DB().NewQuery(`
				SELECT COALESCE((SELECT [[id]] FROM {{leafen_public_share_links}}
					WHERE [[workspace_id]]={:workspaceId} AND [[root_page_id]]={:pageId} LIMIT 1), '')
			`).Bind(dbx.Params{"workspaceId": current.Workspace.WorkspaceID, "pageId": pageID}).Row(&next.ShareLinkID); err != nil {
				return err
			}
		}
		if err := storePublicMutationState(txApp, stored.Revision, next); err != nil {
			return err
		}
		response.State, err = readPublicMutationState(ctx, txApp, current, "", "", true, true)
		return err
	}
	// A key's call asks its person only for a change that would be made
	// (user_key_acting_ask.go).
	if err := rehearseBeforeAsking(event, change); err != nil {
		return publicMutationRouteError(event, err, "inheritance")
	}
	if asked, err := askBeforeActing(event); asked {
		return err
	}
	err = event.App.RunInTransaction(change)
	if err != nil {
		return publicMutationRouteError(event, err, "inheritance")
	}
	return event.JSON(http.StatusOK, response)
}

// pageShareInheritanceInputRefusal is why a change of where a page takes
// its access from can't be made as sent, or nil.
func pageShareInheritanceInputRefusal(input publicMutationShareInheritanceInput) error {
	if input.ExpectedRevision < 0 {
		return publicMutationRefused("expectedRevision is the sharing settings' revision as GET reads it, 0 or more")
	}
	switch input.Action {
	case pageShareInheritanceRelink:
		if input.PrincipalType != "" || input.PrincipalID != "" || input.Role != "" {
			return publicMutationRefused("relink takes no principalType, principalId or role")
		}
	case pageShareInheritanceUnlink:
		switch {
		case !validPublicMutationPrincipal(input.PrincipalType):
			return publicMutationRefused("principalType is user, email, group, integration or workspace, not %q", input.PrincipalType)
		case input.PrincipalID == "" || len(input.PrincipalID) > 255:
			return publicMutationRefused("principalId names whose inherited access changes, up to 255 bytes")
		case input.Role != "" && !validPublicMutationRole(input.Role):
			return publicMutationRefused("role is left out (no access) or %s", publicMutationRoles)
		case input.HideFromSearch != nil && (input.PrincipalType != publicMutationPrincipalWorkspace || input.Role == ""):
			return publicMutationRefused("hideFromSearch goes with everyone in the workspace and the role they keep")
		}
	case pageShareInheritanceWebRole:
		switch {
		case input.PrincipalType != "" || input.PrincipalID != "":
			return publicMutationRefused("web_role takes no principalType or principalId")
		case input.Role != teamspacePageRoleNone && !pageLinkRoles[input.Role]:
			return publicMutationRefused("role is none, view, comment or edit for anyone with the link")
		}
	case pageShareInheritanceTeamspace:
		// A group may also have no access to the page, as Notion's "사용 권한 없음".
		switch {
		case input.PrincipalType != "" || input.PrincipalID != pageTeamspaceGroupOwner && input.PrincipalID != pageTeamspaceGroupMember:
			return publicMutationRefused("teamspace_role names the teamspace's group in principalId, owner or member, with no principalType")
		case !validPublicMutationRole(input.Role) && input.Role != teamspacePageRoleNone:
			return publicMutationRefused("role is none or %s", publicMutationRoles)
		}
	default:
		return publicMutationRefused("action is unlink, relink, web_role or teamspace_role, not %q", input.Action)
	}
	return nil
}

type storedPageShareRole struct {
	ID            string `db:"id"`
	PrincipalType string `db:"principal_type"`
	PrincipalID   string `db:"principal_id"`
	Role          string `db:"role"`
}

// inheritedShareRoles is the widest role the pages above give each principal.
func inheritedShareRoles(app core.App, workspaceID string, ancestorIDs []string) (map[sharePrincipalKey]pageAccessRole, error) {
	roles := map[sharePrincipalKey]pageAccessRole{}
	if len(ancestorIDs) == 0 {
		return roles, nil
	}
	rows, err := loadPageShareRoles(app, workspaceID, ancestorIDs)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		key := sharePrincipalKey{row.PrincipalType, row.PrincipalID}
		if role := pageAccessRoleFromTeamspaceRole(row.Role); role > roles[key] {
			roles[key] = role
		}
	}
	return roles, nil
}

func loadPageShareRoles(app core.App, workspaceID string, pageIDs []string) ([]storedPageShareRole, error) {
	params := dbx.Params{"workspaceId": workspaceID}
	keys := make([]string, 0, len(pageIDs))
	for index, id := range pageIDs {
		key := fmt.Sprintf("sharePage%d", index)
		params[key] = id
		keys = append(keys, "{:"+key+"}")
	}
	rows := []storedPageShareRole{}
	err := app.DB().NewQuery(`
		SELECT [[id]],[[principal_type]],[[principal_id]],[[role]] FROM {{leafen_page_permissions}}
		WHERE [[workspace_id]]={:workspaceId} AND [[page_id]] IN (` + joinSQL(keys) + `)
	`).Bind(params).All(&rows)
	return rows, err
}

func unlinkPageShareInheritance(
	app core.App, authorization pageAuthorization, input publicMutationShareInheritanceInput, now time.Time,
) error {
	workspaceID, pageID := authorization.Workspace.WorkspaceID, authorization.Page.Id
	// Nobody changes their own inherited access here.
	if input.PrincipalType == publicMutationPrincipalUser && input.PrincipalID == authorization.Workspace.PrincipalID {
		return publicMutationRefused("principalId is you: nobody changes their own inherited access")
	}
	unlinked, err := pageShareUnlinked(app, pageID)
	if err != nil || unlinked {
		if err == nil {
			err = errPageShareInheritanceCut
		}
		return err
	}
	ids, err := loadPageAccessAncestorIDs(app, workspaceID, pageID)
	if err != nil {
		return err
	}
	inherited, err := pageInheritedShareRoles(app, authorization.Page, workspaceID, ids[1:])
	if err != nil {
		return err
	}
	target := sharePrincipalKey{input.PrincipalType, input.PrincipalID}
	current, found := inherited[target]
	if !found {
		return publicMutationRefused("principalId has no access from the pages above to change here")
	}
	// Everyone in the workspace keeps their role, hidden from search or not
	// (inheritedWorkspaceSearchChange).
	if input.Role != "" && pageAccessRoleFromTeamspaceRole(input.Role) >= current && !inheritedWorkspaceSearchChange(input, current) {
		return publicMutationRefused("role is less than the access they have from the pages above")
	}
	own, err := loadPageShareRoles(app, workspaceID, []string{pageID})
	if err != nil {
		return err
	}
	for _, row := range own {
		if (sharePrincipalKey{row.PrincipalType, row.PrincipalID}) == target && pageAccessRoleFromTeamspaceRole(row.Role) >= current {
			return publicMutationRefused("principalId has that access on the page itself already")
		}
	}
	if err := cutPageShareInheritance(app, authorization, now); err != nil {
		return err
	}
	change := `UPDATE {{leafen_page_permissions}} SET [[role]]={:role},[[updated_at]]={:now}`
	if input.Role == "" {
		change = `DELETE FROM {{leafen_page_permissions}}`
	} else if input.HideFromSearch != nil {
		change += `,[[hide_from_search]]={:hide}`
	}
	if _, err = app.DB().NewQuery(change + `
		WHERE [[workspace_id]]={:workspaceId} AND [[page_id]]={:pageId}
			AND [[principal_type]]={:principalType} AND [[principal_id]]={:principalId}
	`).Bind(dbx.Params{
		"role": input.Role, "now": now.UnixMilli(), "workspaceId": workspaceID, "pageId": pageID,
		"principalType": target.Type, "principalId": target.ID, "hide": input.HideFromSearch != nil && *input.HideFromSearch,
	}).Execute(); err != nil || input.Role != "" {
		return err
	}
	// Whoever removed them has the line restricting their access (page_share_unlink_notices.go).
	return tellInheritedAccessRemoved(app, authorization, target, current, now)
}

// cutPageShareInheritance unlinks a page's sharing from its parent's: the
// page keeps a copy of every share it inherited, keeping a wider share of its
// own, the teamspace roles it had and the connections that reached it
// (page_notion_connections_unlink.go); the copies tell no one, as nothing
// changes for them.
func cutPageShareInheritance(app core.App, authorization pageAuthorization, now time.Time) error {
	if err := refusePersonPageUnlink(app, authorization.Page); err != nil {
		return err
	}
	workspaceID, pageID := authorization.Workspace.WorkspaceID, authorization.Page.Id
	unlinked, err := pageShareUnlinked(app, pageID)
	if err != nil || unlinked {
		if err == nil {
			err = errPageShareInheritanceCut
		}
		return err
	}
	ids, err := loadPageAccessAncestorIDs(app, workspaceID, pageID)
	if err != nil {
		return err
	}
	if len(ids) < 2 {
		return publicMutationRefused("the page has no page above to take its access from")
	}
	inherited, err := pageInheritedShareRoles(app, authorization.Page, workspaceID, ids[1:])
	if err != nil {
		return err
	}
	own, err := loadPageShareRoles(app, workspaceID, []string{pageID})
	if err != nil {
		return err
	}
	owned := map[sharePrincipalKey]storedPageShareRole{}
	for _, row := range own {
		owned[sharePrincipalKey{row.PrincipalType, row.PrincipalID}] = row
	}
	for key, role := range inherited {
		if row, ok := owned[key]; ok && pageAccessRoleFromTeamspaceRole(row.Role) >= role {
			continue
		}
		if _, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_page_permissions}}
				([[id]],[[workspace_id]],[[page_id]],[[principal_type]],[[principal_id]],[[role]],
				 [[created_by]],[[created_at]],[[updated_at]],[[invite_message]],[[notify_on_access]])
			VALUES ({:id},{:workspaceId},{:pageId},{:principalType},{:principalId},{:role},{:actorId},{:now},{:now},'',0)
			ON CONFLICT ([[page_id]],[[principal_type]],[[principal_id]]) DO UPDATE SET
				[[role]]=excluded.[[role]],[[updated_at]]=excluded.[[updated_at]]
		`).Bind(dbx.Params{
			"id": securityRandomID(), "workspaceId": workspaceID, "pageId": pageID,
			"principalType": key.Type, "principalId": key.ID, "role": publicMutationRoleFor(role),
			"actorId": authorization.Workspace.PrincipalID, "now": now.UnixMilli(),
		}).Execute(); err != nil {
			return err
		}
	}
	if err := copyPageTeamspaceRoles(app, authorization, ids[len(ids)-1], now); err != nil {
		return err
	}
	if err := copyInheritedWebLink(app, authorization, now); err != nil {
		return err
	}
	if err := copyPageNotionConnectionsAbove(app, authorization); err != nil {
		return err
	}
	if _, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_page_share_unlinks}} ([[page_id]],[[workspace_id]],[[unlinked_at]])
		VALUES ({:pageId},{:workspaceId},{:now})
	`).Bind(dbx.Params{"pageId": pageID, "workspaceId": workspaceID, "now": now.UnixMilli()}).Execute(); err != nil {
		return err
	}
	return leavePublicShareGraphsAbove(app, pageID)
}

func relinkPageShareInheritance(app core.App, authorization pageAuthorization, now time.Time) error {
	workspaceID, pageID := authorization.Workspace.WorkspaceID, authorization.Page.Id
	result, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_page_share_unlinks}} WHERE [[page_id]]={:pageId} AND [[workspace_id]]={:workspaceId}
	`).Bind(dbx.Params{"pageId": pageID, "workspaceId": workspaceID}).Execute()
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		// A page at its teamspace's top takes the teamspace's default again.
		if relinked, err := relinkPageTeamspaceDefault(app, authorization); err != nil || relinked {
			return err
		}
		return publicMutationRefused("the page takes its access from the pages above already")
	}
	if err := deletePageTeamspaceRoles(app, workspaceID, pageID); err != nil {
		return err
	}
	if err := dropPageNotionConnectionsCoveredAbove(app, authorization); err != nil {
		return err
	}
	// The page opens by the links above again, and its own web link goes,
	// as Notion's relinked page takes its parent's general access (seen
	// 2026-11-06); a published site of its own stays up.
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_public_share_links}} SET [[web_enabled]]=0,
			[[enabled]]=EXISTS (SELECT 1 FROM {{leafen_public_sites}} [[site]]
				WHERE [[site]].[[share_link_id]]={{leafen_public_share_links}}.[[id]] AND [[site]].[[published]]=1)
		WHERE [[workspace_id]]={:workspaceId} AND [[root_page_id]]={:pageId} AND [[web_enabled]]=1
	`).Bind(dbx.Params{"workspaceId": workspaceID, "pageId": pageID}).Execute(); err != nil {
		return err
	}
	if err := joinPublicShareGraphsAbove(app, pageID); err != nil {
		return err
	}
	ids, err := loadPageAccessAncestorIDs(app, workspaceID, pageID)
	if err != nil {
		return err
	}
	inherited, err := pageInheritedShareRoles(app, authorization.Page, workspaceID, ids[1:])
	if err != nil {
		return err
	}
	own, err := loadPageShareRoles(app, workspaceID, []string{pageID})
	if err != nil {
		return err
	}
	// Whoever relinked it has the line restoring what the pages above give
	// (page_share_unlink_notices.go).
	if err := tellInheritedAccessRestored(app, authorization, inherited, own, now); err != nil {
		return err
	}
	// A share of the page's own that the pages above cover goes; a wider one
	// stays, as a share made on the page itself.
	for _, row := range own {
		if inherited[sharePrincipalKey{row.PrincipalType, row.PrincipalID}] < pageAccessRoleFromTeamspaceRole(row.Role) {
			continue
		}
		if _, err := app.DB().NewQuery(`
			DELETE FROM {{leafen_page_permissions}} WHERE [[id]]={:id} AND [[page_id]]={:pageId}
		`).Bind(dbx.Params{"id": row.ID, "pageId": pageID}).Execute(); err != nil {
			return err
		}
	}
	return nil
}

func setInheritedWebLinkRole(
	app core.App, authorization pageAuthorization, input publicMutationShareInheritanceInput, now time.Time,
) error {
	workspaceID, pageID := authorization.Workspace.WorkspaceID, authorization.Page.Id
	above, err := loadInheritedWebLink(context.Background(), app, workspaceID, pageID)
	if err != nil {
		return err
	}
	// The same role keeps the copy as it was, so its expiry can change next.
	if above == nil {
		return publicMutationRefused("no page above shares a web link for this page to take")
	}
	if err := cutPageShareInheritance(app, authorization, now); err != nil {
		return err
	}
	update := `UPDATE {{leafen_public_share_links}} SET [[role]]={:role},[[updated_at]]={:now}`
	if input.Role == teamspacePageRoleNone {
		update = `UPDATE {{leafen_public_share_links}} SET [[web_enabled]]=0,[[updated_at]]={:now},
			[[enabled]]=EXISTS (SELECT 1 FROM {{leafen_public_sites}} [[site]]
				WHERE [[site]].[[share_link_id]]={{leafen_public_share_links}}.[[id]] AND [[site]].[[published]]=1)`
	}
	_, err = app.DB().NewQuery(update + `
		WHERE [[workspace_id]]={:workspaceId} AND [[root_page_id]]={:pageId} AND [[web_enabled]]=1
	`).Bind(dbx.Params{
		"role": input.Role, "now": now.Format(time.RFC3339Nano), "workspaceId": workspaceID, "pageId": pageID,
	}).Execute()
	return err
}

// inheritedWorkspaceSearchChange is a change of whether everyone in the
// workspace, keeping the role a page above gives them, finds the page in
// search: it unlinks the page as a lowered role does, as Notion's inherited
// "<workspace>의 모든 사용자" role menu offers "검색 결과에서 숨기기" over a
// note that changing the role sets aside the access the page above gives (seen
// 2026-11-16 at 1440 on a person's page and, by w1417, on a page under one
// shared with the workspace).
func inheritedWorkspaceSearchChange(input publicMutationShareInheritanceInput, current pageAccessRole) bool {
	return input.HideFromSearch != nil && input.PrincipalType == publicMutationPrincipalWorkspace &&
		pageAccessRoleFromTeamspaceRole(input.Role) == current
}
