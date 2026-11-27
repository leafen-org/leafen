package app

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Sharing follows the pages above a page, as Notion's (seen 2026-10-11): its
// share window lists, after the page's own shares, the people invited to
// each page above it — "〈page〉에 초대된 사용자", how many and the widest role
// among them — and marks guests. A page whose sharing is unlinked from its
// parent's (page_share_inheritance.go) says so instead, with what linking it
// again would change.

const (
	// Each page above shows this many of its people as it opens; the rest
	// come a window of publicMutationInheritedPeoplePage at a time.
	publicMutationInheritedPeopleWindow = 50
	publicMutationInheritedPeoplePage   = 100
)

// publicMutationPrincipalColumnsSQL names the principal of the share bound as
// [[permission]] as the share window shows it: its label, its email, the
// account it is (a person's, or the one an address names once they are in,
// whose face the window shows as Notion's does), whether
// it is a guest of the workspace (an account or address that is neither its
// owner nor a member) and whether it is one of its restricted members, whom
// the window marks "제한됨" as Notion's does (seen 2026-11-07).
// publicMutationPrincipalJoinsSQL joins what it reads.
var publicMutationPrincipalColumnsSQL = `
	COALESCE(
		NULLIF(trim([[user]].[[name]]),''),NULLIF(trim([[user]].[[email]]),''),
		NULLIF(trim([[invited]].[[name]]),''),
		NULLIF(trim([[permission_group]].[[name]]),''),[[permission]].[[principal_id]]
	) AS [[principal_label]],
	COALESCE(NULLIF(trim([[user]].[[email]]),''),
		CASE WHEN [[invited]].[[id]] IS NOT NULL THEN [[permission]].[[principal_id]] END,'') AS [[principal_email]],
	COALESCE([[user]].[[id]],[[invited]].[[id]],'') AS [[principal_user_id]],
	CASE WHEN [[permission]].[[principal_type]] IN ('user','email') THEN NOT EXISTS (
		SELECT 1 FROM {{leafen_workspaces}} [[guest_workspace]]
		WHERE [[guest_workspace]].[[id]]=[[permission]].[[workspace_id]]
			AND [[guest_workspace]].[[workspace_owner]]=COALESCE([[user]].[[id]],[[invited]].[[id]],'')
		UNION ALL
		SELECT 1 FROM {{leafen_workspace_memberships}} [[guest_member]]
		WHERE [[guest_member]].[[workspace_id]]=[[permission]].[[workspace_id]]
			AND [[guest_member]].[[user_id]]=COALESCE([[user]].[[id]],[[invited]].[[id]],'')
	) ELSE 0 END AS [[principal_guest]],
	CASE WHEN [[permission]].[[principal_type]] IN ('user','email') THEN ` +
	workspaceRestrictedMemberSQL("[[permission]].[[workspace_id]]", "COALESCE([[user]].[[id]],[[invited]].[[id]],'')") +
	` ELSE 0 END AS [[principal_restricted]]`

// A share made to an address names the person once they are in the page's
// organization (a guest admitted by it, or a member) under that verified
// address, as Notion's share list does; an address no one there has yet
// stays the address.
var publicMutationPrincipalJoinsSQL = `
	LEFT JOIN {{users}} [[user]] ON [[permission]].[[principal_type]]='user'
		AND [[user]].[[id]]=[[permission]].[[principal_id]]
	LEFT JOIN {{users}} [[invited]] ON [[permission]].[[principal_type]]='email' AND [[invited]].[[id]]=(
		SELECT [[identity]].[[user_id]] FROM {{leafen_account_emails}} [[identity]]
		JOIN {{leafen_organization_memberships}} [[membership]] ON [[membership]].[[user]]=[[identity]].[[user_id]]
			AND [[membership]].[[status]]='active'
		JOIN {{leafen_workspaces}} [[invited_workspace]] ON [[invited_workspace]].[[id]]=[[permission]].[[workspace_id]]
			AND [[invited_workspace]].[[organization]]!='' AND [[membership]].[[organization]]=[[invited_workspace]].[[organization]]
		WHERE [[identity]].[[email]]=lower(trim([[permission]].[[principal_id]])) AND ` + provenAccountEmailSQL("[[identity]].") + `
		LIMIT 1)
	LEFT JOIN {{leafen_workspace_permission_groups}} [[permission_group]]
		ON [[permission]].[[principal_type]]='group'
		AND [[permission_group]].[[workspace_id]]=[[permission]].[[workspace_id]]
		AND [[permission_group]].[[id]]=[[permission]].[[principal_id]]`

// inheritedShareFilterSQL keeps, of the shares bound as [[permission]] on a
// page above, those the page's share window lists under that page: not the
// workspace's, not the owner's, and not one the page has of its own.
const inheritedShareFilterSQL = `[[permission]].[[principal_type]]!='workspace'
	AND NOT ([[permission]].[[principal_type]]='user' AND [[permission]].[[principal_id]]={:ownerId})
	AND NOT EXISTS (
		SELECT 1 FROM {{leafen_page_permissions}} [[own]]
		WHERE [[own]].[[page_id]]={:pageId} AND [[own]].[[principal_type]]=[[permission]].[[principal_type]]
			AND [[own]].[[principal_id]]=[[permission]].[[principal_id]])`

type storedInheritedShare struct {
	PageID         string `db:"page_id"`
	PrincipalType  string `db:"principal_type"`
	PrincipalID    string `db:"principal_id"`
	PrincipalLabel string `db:"principal_label"`
	PrincipalEmail string `db:"principal_email"`
	PrincipalUser  string `db:"principal_user_id"`
	Guest          bool   `db:"principal_guest"`
	Restricted     bool   `db:"principal_restricted"`
	Role           string `db:"role"`
	Total          int    `db:"total"`
	TopRole        int    `db:"top_role"`
}

type sharePrincipalKey struct{ Type, ID string }

// loadInheritedShares reads the shares made on pages above a page, at most a
// window of people per page with each page's count and widest role. Shares
// the page has of its own, and the workspace's owner, are left to its own
// rows.
func loadInheritedShares(
	ctx context.Context, app core.App, workspaceID, pageID, ownerID string, ancestorIDs []string, window int,
) ([]storedInheritedShare, error) {
	rows := []storedInheritedShare{}
	if len(ancestorIDs) == 0 {
		return rows, nil
	}
	encoded, err := json.Marshal(ancestorIDs)
	if err != nil {
		return nil, err
	}
	params := dbx.Params{"workspaceId": workspaceID, "pageId": pageID, "ownerId": ownerID, "window": window, "pages": string(encoded)}
	err = app.DB().NewQuery(`
		SELECT [[page_id]],[[principal_type]],[[principal_id]],[[principal_label]],[[principal_email]],[[principal_user_id]],
			[[principal_guest]],[[principal_restricted]],[[role]],[[total]],[[top_role]]
		FROM (
			SELECT [[permission]].[[page_id]],[[permission]].[[principal_type]],[[permission]].[[principal_id]],
				` + publicMutationPrincipalColumnsSQL + `,
				[[permission]].[[role]],
				ROW_NUMBER() OVER (PARTITION BY [[permission]].[[page_id]]
					ORDER BY [[permission]].[[created_at]],[[permission]].[[id]]) AS [[position]],
				COUNT(*) OVER (PARTITION BY [[permission]].[[page_id]]) AS [[total]],
				MAX(CASE [[permission]].[[role]] WHEN 'view' THEN 1 WHEN 'comment' THEN 2
					WHEN 'edit' THEN 3 WHEN 'full_access' THEN 4 ELSE 0 END)
					OVER (PARTITION BY [[permission]].[[page_id]]) AS [[top_role]]
			FROM {{leafen_page_permissions}} [[permission]]
			` + publicMutationPrincipalJoinsSQL + `
			WHERE [[permission]].[[workspace_id]]={:workspaceId}
				AND [[permission]].[[page_id]] IN (SELECT [[value]] FROM json_each({:pages}))
				AND ` + inheritedShareFilterSQL + `
		)
		WHERE [[position]] <= {:window}
		ORDER BY [[page_id]],[[position]]
	`).WithContext(ctx).Bind(params).All(&rows)
	return rows, err
}

func populatePublicMutationInheritedAccess(
	ctx context.Context, app core.App, authorization pageAuthorization, state *publicMutationState,
) error {
	if isDatabaseRowPageRecord(authorization.Page) {
		return populatePublicMutationRowInheritedAccess(ctx, app, authorization, state)
	}
	unlinked, err := pageShareUnlinked(app, state.PageID)
	if err != nil {
		return err
	}
	if unlinked {
		return populatePublicMutationShareUnlink(ctx, app, authorization, state)
	}
	if atTop, err := populatePublicMutationTeamspaceUnlink(app, authorization, state); err != nil || atTop {
		return err
	}
	ids, err := loadPageAccessAncestorIDs(app, authorization.Page.GetString("workspace"), authorization.Page.Id)
	if err != nil || len(ids) < 2 {
		return err
	}
	ancestors := ids[1:]
	if err := populatePublicMutationShareDiffers(ctx, app, state, ancestors); err != nil {
		return err
	}
	rows, err := loadInheritedShares(
		ctx, app, state.WorkspaceID, state.PageID, state.AccessOwner.PrincipalID, ancestors, publicMutationInheritedPeopleWindow,
	)
	if err != nil || len(rows) == 0 {
		return err
	}
	titles, err := loadPageTitles(ctx, app, state.WorkspaceID, ancestors)
	if err != nil {
		return err
	}
	byPage := map[string]*publicMutationInheritedGroup{}
	for _, row := range rows {
		group := byPage[row.PageID]
		if group == nil {
			group = &publicMutationInheritedGroup{
				PageID: row.PageID, PageTitle: titles[row.PageID], Count: row.Total,
				Role: publicMutationRoleFor(pageAccessRole(row.TopRole)), People: []publicMutationInheritedPerson{},
			}
			byPage[row.PageID] = group
		}
		group.People = append(group.People, publicMutationInheritedPerson{
			PrincipalType: row.PrincipalType, PrincipalID: row.PrincipalID, Label: row.PrincipalLabel,
			Email: row.PrincipalEmail, UserID: row.PrincipalUser, Role: row.Role, Guest: row.Guest, Restricted: row.Restricted,
		})
	}
	// The nearest page above comes first.
	for _, id := range ancestors {
		if group := byPage[id]; group != nil {
			state.InheritedGroups = append(state.InheritedGroups, *group)
		}
	}
	return nil
}

// populatePublicMutationShareDiffers says a linked page's sharing differs
// from its parent's when the page gives someone a role of its own over one a
// page above gives them, as Notion's share window says (seen 2026-11-06:
// raising an inherited person on a child page).
func populatePublicMutationShareDiffers(
	ctx context.Context, app core.App, state *publicMutationState, ancestors []string,
) error {
	encoded, err := json.Marshal(ancestors)
	if err != nil {
		return err
	}
	var differs bool
	// The roles above are found by page and principal; their workspace is
	// read after (+), or SQLite, which keeps no statistics, reads the
	// workspace's every role.
	if err := app.DB().NewQuery(`
		SELECT EXISTS (
			SELECT 1 FROM {{leafen_page_permissions}} [[own]]
			JOIN {{leafen_page_permissions}} [[above]]
				ON +[[above]].[[workspace_id]]=[[own]].[[workspace_id]]
				AND [[above]].[[principal_type]]=[[own]].[[principal_type]]
				AND [[above]].[[principal_id]]=[[own]].[[principal_id]]
				AND [[above]].[[page_id]] IN (SELECT [[value]] FROM json_each({:pages}))
			WHERE +[[own]].[[workspace_id]]={:workspaceId} AND [[own]].[[page_id]]={:pageId}
				AND [[own]].[[principal_type]]!='workspace'
				AND NOT ([[own]].[[principal_type]]='user' AND [[own]].[[principal_id]]={:ownerId})
		)
	`).WithContext(ctx).Bind(dbx.Params{
		"workspaceId": state.WorkspaceID, "pageId": state.PageID, "ownerId": state.AccessOwner.PrincipalID, "pages": string(encoded),
	}).Row(&differs); err != nil || !differs {
		return err
	}
	parentID, parentTitle, parentDatabase, err := publicMutationPageParent(ctx, app, state.WorkspaceID, state.PageID)
	if err != nil || parentID == "" {
		return err
	}
	state.ShareDiffers = &publicMutationShareDiffers{ParentID: parentID, ParentTitle: parentTitle, ParentDatabase: parentDatabase}
	return nil
}

// populatePublicMutationShareUnlink says which page an unlinked page's
// sharing left, and what linking it again changes: everyone the pages above
// give more than the page's own share does gets that again, and its
// teamspace's owners and members the roles the parent gives them.
func populatePublicMutationShareUnlink(
	ctx context.Context, app core.App, authorization pageAuthorization, state *publicMutationState,
) error {
	parentID, parentTitle, parentDatabase, err := publicMutationPageParent(ctx, app, state.WorkspaceID, state.PageID)
	if err != nil || parentID == "" {
		return err
	}
	changes, err := pageShareRelinkChanges(ctx, app, state.WorkspaceID, state.PageID, parentID, state.AccessOwner.PrincipalID)
	if err != nil {
		return err
	}
	teamspaceChanges, err := pageTeamspaceRelinkChanges(app, authorization, parentID)
	if err != nil {
		return err
	}
	state.ShareUnlink = &publicMutationShareUnlink{
		ParentID: parentID, ParentTitle: parentTitle, ParentDatabase: parentDatabase, Changes: changes, TeamspaceChanges: teamspaceChanges,
	}
	return nil
}

func pageShareRelinkChanges(
	ctx context.Context, app core.App, workspaceID, pageID, parentID, ownerID string,
) ([]publicMutationRelinkChange, error) {
	chain, err := loadPageAccessAncestorIDs(app, workspaceID, parentID)
	if err != nil {
		return nil, err
	}
	// Every share above, own ones included, with the page's own role beside it.
	rows := []struct {
		storedInheritedShare
		OwnRole string `db:"own_role"`
	}{}
	encoded, err := json.Marshal(chain)
	if err != nil {
		return nil, err
	}
	params := dbx.Params{"workspaceId": workspaceID, "pageId": pageID, "ownerId": ownerID, "chain": string(encoded)}
	if err := app.DB().NewQuery(`
		SELECT [[permission]].[[principal_type]],[[permission]].[[principal_id]],
			` + publicMutationPrincipalColumnsSQL + `,
			MAX(CASE [[permission]].[[role]] WHEN 'view' THEN 1 WHEN 'comment' THEN 2
				WHEN 'edit' THEN 3 WHEN 'full_access' THEN 4 ELSE 0 END) AS [[top_role]],
			COALESCE((SELECT [[own]].[[role]] FROM {{leafen_page_permissions}} [[own]]
				WHERE [[own]].[[page_id]]={:pageId} AND [[own]].[[principal_type]]=[[permission]].[[principal_type]]
					AND [[own]].[[principal_id]]=[[permission]].[[principal_id]]), '') AS [[own_role]]
		FROM {{leafen_page_permissions}} [[permission]]
		` + publicMutationPrincipalJoinsSQL + `
		WHERE [[permission]].[[workspace_id]]={:workspaceId} AND [[permission]].[[page_id]] IN (SELECT [[value]] FROM json_each({:chain}))
			AND NOT ([[permission]].[[principal_type]]='user' AND [[permission]].[[principal_id]]={:ownerId})
		GROUP BY [[permission]].[[principal_type]],[[permission]].[[principal_id]]
		ORDER BY MIN([[permission]].[[created_at]])
	`).WithContext(ctx).Bind(params).All(&rows); err != nil {
		return nil, err
	}
	changes := []publicMutationRelinkChange{}
	for _, row := range rows {
		if pageAccessRole(row.TopRole) <= pageAccessRoleFromTeamspaceRole(row.OwnRole) {
			continue
		}
		label := row.PrincipalLabel
		if row.PrincipalType == publicMutationPrincipalWorkspace {
			label = ""
		}
		changes = append(changes, publicMutationRelinkChange{
			PrincipalType: row.PrincipalType, PrincipalID: row.PrincipalID, Label: label,
			FromRole: row.OwnRole, ToRole: publicMutationRoleFor(pageAccessRole(row.TopRole)),
		})
	}
	return changes, nil
}

// pageShareUnlinked is whether a page's shares no longer follow the page
// above it, once a read scope (access_memo.go; primeRelationRowsRemembered).
func pageShareUnlinked(app core.App, pageID string) (bool, error) {
	return remembered(app, pageShareUnlinkedMemoKey{pageID}, func() (bool, error) {
		var unlinked bool
		err := app.DB().NewQuery(`
			SELECT EXISTS (SELECT 1 FROM {{leafen_page_share_unlinks}} WHERE [[page_id]]={:pageId})
		`).Bind(dbx.Params{"pageId": pageID}).Row(&unlinked)
		return unlinked, err
	})
}

type pageShareUnlinkedMemoKey struct{ pageID string }

func loadPageTitles(ctx context.Context, app core.App, workspaceID string, pageIDs []string) (map[string]string, error) {
	encoded, err := json.Marshal(pageIDs)
	if err != nil {
		return nil, err
	}
	rows := []struct {
		ID    string `db:"id"`
		Title string `db:"title"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[id]],[[title]] FROM {{leafen_pages}}
		WHERE [[workspace]]={:workspaceId} AND [[id]] IN (SELECT [[value]] FROM json_each({:pages}))
	`).WithContext(ctx).Bind(dbx.Params{"workspaceId": workspaceID, "pages": string(encoded)}).All(&rows); err != nil {
		return nil, err
	}
	titles := make(map[string]string, len(rows))
	for _, row := range rows {
		titles[row.ID] = row.Title
	}
	return titles, nil
}

// readInheritedSharePeople lists the people one page above gives the page,
// a window at a time from the cursor (the last one's share time and id),
// for its row opened in the share window
// (GET /pages/{pageId}/inherited-shares?from=<page above>&cursor=).
func readInheritedSharePeople(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	authorization, err := authorizePublicMutationReader(event.App, user.Id, pageID)
	if err != nil {
		return publicMutationRouteError(event, err, "inherited-authorization")
	}
	if authorization.AccessRole < pageAccessRoleFullAccess {
		seesPeople, _, err := publicMutationReaderSeesPeople(event.App, authorization)
		if err == nil && !seesPeople {
			err = errPublicMutationNotFound
		}
		if err != nil {
			return publicMutationRouteError(event, err, "inherited-authorization")
		}
	}
	query := event.Request.URL.Query()
	from := strings.TrimSpace(query.Get("from"))
	afterAt, afterID := int64(0), ""
	if raw := strings.TrimSpace(query.Get("cursor")); raw != "" {
		at, id, found := strings.Cut(raw, ":")
		parsed, parseErr := strconv.ParseInt(at, 10, 64)
		if !found || parseErr != nil || id == "" {
			return publicMutationRouteError(event, publicMutationRefused("cursor is the nextCursor the last list gave, or none to start"), "inherited-cursor")
		}
		afterAt, afterID = parsed, id
	}
	workspaceID := authorization.Workspace.WorkspaceID
	ancestors, err := loadPageAccessAncestorIDs(event.App, workspaceID, pageID)
	if err != nil {
		return publicMutationRouteError(event, err, "inherited-ancestors")
	}
	// Only a page above gives the page anyone.
	if from == "" || from == pageID || !slices.Contains(ancestors, from) {
		return publicMutationRouteError(event, errPublicMutationInheritedFrom, "inherited-from")
	}
	// A database item's page has one group, from its database's page.
	if isDatabaseRowPageRecord(authorization.Page) {
		if from != ancestors[1] {
			return publicMutationRouteError(event, errPublicMutationInheritedFrom, "inherited-from")
		}
		response, err := readRowInheritedSharePeople(event.Request.Context(), event.App, authorization, ancestors, afterAt, afterID)
		if err != nil {
			return publicMutationRouteError(event, err, "inherited-read")
		}
		return event.JSON(http.StatusOK, response)
	}
	owner, _, err := publicMutationFullAccessHolder(event.Request.Context(), event.App, authorization, ancestors)
	if err != nil {
		return publicMutationRouteError(event, err, "inherited-holder")
	}
	rows := []struct {
		storedInheritedShare
		CreatedAt int64  `db:"created_at"`
		ShareID   string `db:"share_id"`
	}{}
	if err := event.App.DB().NewQuery(`
		SELECT [[permission]].[[page_id]],[[permission]].[[principal_type]],[[permission]].[[principal_id]],
			` + publicMutationPrincipalColumnsSQL + `,
			[[permission]].[[role]], [[permission]].[[created_at]], [[permission]].[[id]] AS [[share_id]]
		FROM {{leafen_page_permissions}} [[permission]]
		` + publicMutationPrincipalJoinsSQL + `
		WHERE [[permission]].[[workspace_id]]={:workspaceId} AND [[permission]].[[page_id]]={:from}
			AND ` + inheritedShareFilterSQL + `
			AND ([[permission]].[[created_at]], [[permission]].[[id]]) > ({:afterAt}, {:afterId})
		ORDER BY [[permission]].[[created_at]], [[permission]].[[id]]
		LIMIT {:limit}
	`).WithContext(event.Request.Context()).Bind(dbx.Params{
		"workspaceId": workspaceID, "pageId": pageID, "from": from, "ownerId": owner,
		"afterAt": afterAt, "afterId": afterID, "limit": publicMutationInheritedPeoplePage + 1,
	}).All(&rows); err != nil {
		return publicMutationRouteError(event, err, "inherited-read")
	}
	response := publicMutationInheritedPeopleResponse{People: []publicMutationInheritedPerson{}}
	if len(rows) > publicMutationInheritedPeoplePage {
		rows = rows[:publicMutationInheritedPeoplePage]
		last := rows[len(rows)-1]
		response.NextCursor = strconv.FormatInt(last.CreatedAt, 10) + ":" + last.ShareID
	}
	for _, row := range rows {
		response.People = append(response.People, publicMutationInheritedPerson{
			PrincipalType: row.PrincipalType, PrincipalID: row.PrincipalID, Label: row.PrincipalLabel,
			Email: row.PrincipalEmail, UserID: row.PrincipalUser, Role: row.Role, Guest: row.Guest, Restricted: row.Restricted,
		})
	}
	return event.JSON(http.StatusOK, response)
}

// errPublicMutationInheritedFrom is a page named as where access comes
// from that gives this page none.
var errPublicMutationInheritedFrom = publicMutationRefused("from names a page above whose shares open this page; the sharing settings list them")
