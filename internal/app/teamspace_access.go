package app

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

var (
	errTeamspaceInvalid         = errors.New("teamspace input is invalid")
	errTeamspaceNotFound        = errors.New("teamspace not found")
	errTeamspaceConflict        = errors.New("teamspace revision conflict")
	errTeamspaceRequestIDReused = errors.New("teamspace request id reused")
	// The default teamspace holds every member, so an invite-only one can't
	// be it, and no one is taken out of it.
	errTeamspacePrivateDefault = errors.New("teamspace private default")
	errTeamspaceDefaultMember  = errors.New("teamspace default member")
	errTeamspaceLastOwner      = errors.New("teamspace last owner")
	// A workspace keeps at least one default teamspace, as Notion's
	// (atLeastOneDefaultTeam, teamspace_default_mutation.go).
	errTeamspaceDefaultRequired = errors.New("teamspace default required")
)

func loadTeamspaceAuthority(app core.App, principalID, workspaceID string) (teamspaceAuthority, error) {
	workspace, err := authorizeWorkspace(app, principalID, workspaceID, permissionWorkspaceRead)
	if err != nil {
		return teamspaceAuthority{}, err
	}
	rows := []struct {
		ID   string `db:"id"`
		Name string `db:"name"`
	}{}
	err = app.DB().NewQuery(`
		SELECT [[group]].[[id]],[[group]].[[name]]
		FROM {{leafen_workspace_permission_group_members}} [[member]]
		JOIN {{leafen_workspace_permission_groups}} [[group]]
			ON [[group]].[[id]]=[[member]].[[group_id]]
		WHERE [[member]].[[user_id]]={:principalId}
			AND [[group]].[[workspace_id]]={:workspaceId}
			AND [[group]].[[deleted_at]] IS NULL
		ORDER BY [[group]].[[id]]
	`).Bind(dbx.Params{"principalId": principalID, "workspaceId": workspaceID}).All(&rows)
	if err != nil {
		return teamspaceAuthority{}, err
	}
	groups := make(map[string]struct{}, len(rows))
	names := make(map[string]string, len(rows))
	for _, row := range rows {
		groups[row.ID] = struct{}{}
		names[row.ID] = row.Name
	}
	return teamspaceAuthority{Workspace: workspace, GroupIDs: groups, GroupNames: names}, nil
}

func loadTeamspaceSettings(app core.App, authority teamspaceAuthority) (storedTeamspaceSettings, error) {
	settings := storedTeamspaceSettings{}
	err := app.DB().NewQuery(`
		SELECT [[workspace_id]],[[default_teamspace_ids]],[[owners_only_create]],
			[[revision]],[[updated_by]],[[updated_at]]
		FROM {{leafen_teamspace_settings}} WHERE [[workspace_id]]={:workspaceId}
	`).Bind(dbx.Params{"workspaceId": authority.Workspace.WorkspaceID}).One(&settings)
	if errors.Is(err, sql.ErrNoRows) {
		return storedTeamspaceSettings{
			WorkspaceID: authority.Workspace.WorkspaceID, DefaultTeamspaceIDs: "[]", Revision: 1,
			UpdatedBy: authority.Workspace.WorkspaceOwnerID,
		}, nil
	}
	return settings, err
}

func loadActorTeamspaceMembers(app core.App, authority teamspaceAuthority, teamspaceIDs []string) ([]storedTeamspaceMember, error) {
	if len(teamspaceIDs) == 0 {
		return []storedTeamspaceMember{}, nil
	}
	params := dbx.Params{
		"workspaceId": authority.Workspace.WorkspaceID,
		"principalId": authority.Workspace.PrincipalID,
	}
	placeholders := make([]string, 0, len(teamspaceIDs))
	for index, id := range teamspaceIDs {
		key := fmt.Sprintf("teamspace%d", index)
		params[key] = id
		placeholders = append(placeholders, "{:"+key+"}")
	}
	rows := []storedTeamspaceMember{}
	// The person's own rows and their groups' rows, each by its index: one
	// condition with OR read every member of the teamspace, for every page
	// a person was checked on (2026-10-15).
	columns := `[[member]].[[id]],[[member]].[[workspace_id]],[[member]].[[teamspace_id]],
			[[member]].[[principal_type]],[[member]].[[principal_id]],
			[[member]].[[workspace_member_id]],[[member]].[[role]],[[member]].[[revision]],
			[[member]].[[created_by]],[[member]].[[created_at]],[[member]].[[updated_at]]`
	err := app.DB().NewQuery(`
		SELECT ` + columns + `
		FROM {{leafen_teamspace_members}} [[member]]
		WHERE [[member]].[[workspace_id]]={:workspaceId} AND [[member]].[[principal_type]]='user'
			AND [[member]].[[principal_id]]={:principalId} AND [[member]].[[deleted_at]]=0
			AND [[member]].[[teamspace_id]] IN (` + strings.Join(placeholders, ",") + `)
		UNION ALL
		SELECT ` + columns + `
		FROM {{leafen_workspace_permission_group_members}} [[group_member]]
		JOIN {{leafen_workspace_permission_groups}} [[group]]
			ON [[group]].[[id]]=[[group_member]].[[group_id]]
				AND [[group]].[[workspace_id]]={:workspaceId} AND [[group]].[[deleted_at]] IS NULL
		JOIN {{leafen_teamspace_members}} [[member]]
			ON [[member]].[[workspace_id]]={:workspaceId} AND [[member]].[[principal_type]]='group'
				AND [[member]].[[principal_id]]=[[group]].[[id]] AND [[member]].[[deleted_at]]=0
				AND [[member]].[[teamspace_id]] IN (` + strings.Join(placeholders, ",") + `)
		WHERE [[group_member]].[[user_id]]={:principalId}
	`).Bind(params).All(&rows)
	return rows, err
}

func loadActorPendingTeamspaceRequests(
	app core.App, authority teamspaceAuthority, teamspaceIDs []string,
) (map[string]struct{}, error) {
	pending := make(map[string]struct{})
	if len(teamspaceIDs) == 0 {
		return pending, nil
	}
	params := dbx.Params{
		"workspaceId": authority.Workspace.WorkspaceID,
		"principalId": authority.Workspace.PrincipalID,
	}
	placeholders := make([]string, 0, len(teamspaceIDs))
	for index, id := range teamspaceIDs {
		key := fmt.Sprintf("pendingTeamspace%d", index)
		params[key] = id
		placeholders = append(placeholders, "{:"+key+"}")
	}
	rows := []struct {
		TeamspaceID string `db:"teamspace_id"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[teamspace_id]]
		FROM {{leafen_teamspace_join_requests}} INDEXED BY [[idx_leafen_teamspace_requests_user]]
		WHERE [[workspace_id]]={:workspaceId} AND [[user_id]]={:principalId} AND [[status]]='pending'
			AND [[teamspace_id]] IN (` + strings.Join(placeholders, ",") + `)
	`).Bind(params).All(&rows)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		pending[row.TeamspaceID] = struct{}{}
	}
	return pending, nil
}

func decideTeamspaceAccess(
	teamspace storedTeamspace,
	settings storedTeamspaceSettings,
	memberships []storedTeamspaceMember,
	pendingRequests map[string]struct{},
	authority teamspaceAuthority,
) teamspaceAccessDecisionWire {
	// A restricted member moves no one in or out (MembersLocked, authorizeTeamspaceMemberAction).
	decision, own := teamspaceAccessDecisionWire{TeamspaceID: teamspace.ID, MembersLocked: authority.Workspace.Restricted}, false
	for _, membership := range memberships {
		if membership.TeamspaceID != teamspace.ID {
			continue
		}
		if membership.Role == teamspaceMemberRoleOwner || decision.MembershipRole == "" {
			decision.MembershipRole = membership.Role
		}
		if membership.PrincipalType == "group" {
			decision.MembershipGroups = append(decision.MembershipGroups, authority.GroupNames[membership.PrincipalID])
		} else {
			own = true
		}
	}
	// Leaving takes out one's own row only: someone in it through a group
	// alone ("group") has none, and stays while the group does.
	if own {
		decision.MembershipSource = "explicit"
	} else if decision.MembershipRole != "" {
		decision.MembershipSource = "group"
	}
	slices.Sort(decision.MembershipGroups)
	// A restricted member is in only the teamspaces they were added to.
	restricted := authority.Workspace.Restricted
	if decision.MembershipRole == "" && settings.isDefault(teamspace.ID) && !restricted {
		decision.MembershipRole = teamspaceMemberRoleMember
		decision.MembershipSource = "default"
	}
	decision.Joined = decision.MembershipRole != ""
	active := teamspace.ArchivedAt == 0
	decision.Visible = active && (decision.Joined || teamspace.Access != teamspaceAccessPrivate && !restricted)
	decision.CanJoin = decision.Visible && !decision.Joined && teamspace.Access == teamspaceAccessOpen
	_, decision.RequestPending = pendingRequests[teamspace.ID]
	decision.CanRequest = decision.Visible && !decision.Joined && !decision.RequestPending && teamspace.Access == teamspaceAccessClosed
	if active && decision.MembershipRole == teamspaceMemberRoleOwner {
		decision.PageRole = teamspacePageRoleFullAccess
	} else if active && decision.MembershipRole != "" {
		decision.PageRole = teamspace.MemberPageRole
	} else if role := teamspaceOutsideRole(teamspace); active && role != teamspacePageRoleNone && !restricted {
		decision.PageRole = role
	}
	return decision
}

// archivedTeamspaceKeepsMember says someone stays in an archived teamspace
// as far as its pages go: in it by themselves or through a group, not only
// as everyone in a default one (effectiveTeamspaceRoleOnPage).
func archivedTeamspaceKeepsMember(decision teamspaceAccessDecisionWire) bool {
	return decision.MembershipSource == "explicit" || decision.MembershipSource == "group"
}

// archivedTeamspacePageRole is the role an archived teamspace's member had on
// its pages before it was archived, which they keep there.
func archivedTeamspacePageRole(teamspace storedTeamspace, decision teamspaceAccessDecisionWire) string {
	if !archivedTeamspaceKeepsMember(decision) {
		return ""
	}
	if decision.MembershipRole == teamspaceMemberRoleOwner {
		return teamspacePageRoleFullAccess
	}
	return teamspace.MemberPageRole
}

func authorizeTeamspace(
	app core.App,
	principalID, workspaceID, teamspaceID string,
	requireOwner bool,
) (teamspaceAuthority, storedTeamspace, teamspaceAccessDecisionWire, error) {
	return authorizeTeamspaceInvited(app, principalID, workspaceID, teamspaceID, requireOwner, false)
}

// authorizeTeamspaceInvited also reaches, for someone its open invite link
// let in, a teamspace they can't see (teamspace_invite_links.go).
func authorizeTeamspaceInvited(
	app core.App,
	principalID, workspaceID, teamspaceID string,
	requireOwner, invited bool,
) (teamspaceAuthority, storedTeamspace, teamspaceAccessDecisionWire, error) {
	found, err := remembered(app, teamspaceAccessMemoKey{
		principalID: principalID, workspaceID: workspaceID, teamspaceID: teamspaceID, requireOwner: requireOwner, invited: invited,
	}, func() (teamspaceAccessMemoValue, error) {
		authority, teamspace, decision, err := authorizeTeamspaceUncached(app, principalID, workspaceID, teamspaceID, requireOwner, invited)
		return teamspaceAccessMemoValue{authority: authority, teamspace: teamspace, decision: decision}, err
	})
	return found.authority, found.teamspace, found.decision, err
}

func authorizeTeamspaceUncached(
	app core.App,
	principalID, workspaceID, teamspaceID string,
	requireOwner, invited bool,
) (teamspaceAuthority, storedTeamspace, teamspaceAccessDecisionWire, error) {
	authority, err := loadTeamspaceAuthority(app, principalID, workspaceID)
	if err != nil {
		return teamspaceAuthority{}, storedTeamspace{}, teamspaceAccessDecisionWire{}, refusedUnlessStorage(errTeamspaceNotFound, err)
	}
	teamspace := storedTeamspace{}
	err = app.DB().NewQuery(`
		SELECT [[id]],[[workspace_id]],[[name]],[[icon]],[[description]],[[access]],
			[[member_page_role]],[[open_page_role]],[[outside_access]],[[disable_publishing]],[[disable_guests]],[[disable_export]],[[members_can_invite]],
			[[members_can_edit_sidebar]],[[archived_at]],[[revision]],[[created_by]],
			[[updated_by]],[[created_at]],[[updated_at]]
		FROM {{leafen_teamspaces}}
		WHERE [[id]]={:id} AND [[workspace_id]]={:workspaceId} AND [[deleted_at]]=0
	`).Bind(dbx.Params{"id": teamspaceID, "workspaceId": workspaceID}).One(&teamspace)
	if errors.Is(err, sql.ErrNoRows) {
		return teamspaceAuthority{}, storedTeamspace{}, teamspaceAccessDecisionWire{}, errTeamspaceNotFound
	}
	if err != nil {
		return teamspaceAuthority{}, storedTeamspace{}, teamspaceAccessDecisionWire{}, err
	}
	settings, err := loadTeamspaceSettings(app, authority)
	if err != nil {
		return teamspaceAuthority{}, storedTeamspace{}, teamspaceAccessDecisionWire{}, err
	}
	memberships, err := loadActorTeamspaceMembers(app, authority, []string{teamspaceID})
	if err != nil {
		return teamspaceAuthority{}, storedTeamspace{}, teamspaceAccessDecisionWire{}, err
	}
	pendingRequests, err := loadActorPendingTeamspaceRequests(app, authority, []string{teamspaceID})
	if err != nil {
		return teamspaceAuthority{}, storedTeamspace{}, teamspaceAccessDecisionWire{}, err
	}
	decision := decideTeamspaceAccess(teamspace, settings, memberships, pendingRequests, authority)
	if err := letStandInJoin(app, authority, teamspace, &decision); err != nil {
		return teamspaceAuthority{}, storedTeamspace{}, teamspaceAccessDecisionWire{}, err
	}
	workspaceOwner := authority.Workspace.Role == workspaceRoleOwner
	if requireOwner { // its owners', not a workspace owner who is only its member (managesTeamspace)
		if err := requireTeamspaceManager(app, authority, teamspace.ID, decision); err != nil {
			return teamspaceAuthority{}, storedTeamspace{}, teamspaceAccessDecisionWire{}, err
		}
	} else if !decision.Visible && !workspaceOwner && !invited {
		return teamspaceAuthority{}, storedTeamspace{}, teamspaceAccessDecisionWire{}, errTeamspaceNotFound
	}
	return authority, teamspace, decision, nil
}

func currentTeamspaceSnapshotRevision(app core.App, workspaceID string) (int, error) {
	return currentWorkspacePermissionCursor(app, workspaceID)
}

func encodeTeamspaceCursor(value teamspaceCursor) *string {
	encoded, _ := json.Marshal(value)
	cursor := base64.RawURLEncoding.EncodeToString(encoded)
	return &cursor
}

func decodeTeamspaceCursor(raw string) (teamspaceCursor, error) {
	if raw == "" {
		return teamspaceCursor{}, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	value := teamspaceCursor{}
	if len(raw) > maxTeamspaceCursorBytes || err != nil || json.Unmarshal(decoded, &value) != nil ||
		value.Version != teamspaceSchemaVersion || value.ID == "" {
		return teamspaceCursor{}, errTeamspaceCursorInvalid
	}
	return value, nil
}

func readTeamspaceSnapshotFor(
	ctx context.Context,
	app core.App,
	principalID, workspaceID, rawCursor string, everyActive bool,
) (teamspaceSnapshotResponse, error) {
	cursor, err := decodeTeamspaceCursor(rawCursor)
	if err != nil {
		return teamspaceSnapshotResponse{}, err
	}
	response := teamspaceSnapshotResponse{
		WorkspaceID: workspaceID, Teamspaces: []teamspaceWireRecord{},
		Decisions: []teamspaceAccessDecisionWire{}, Summaries: []teamspaceSummaryWire{},
	}
	err = runReadSnapshot(ctx, app, func(txApp core.App) error {
		authority, err := loadTeamspaceAuthority(txApp, principalID, workspaceID)
		if err != nil {
			return refusedUnlessStorage(errTeamspaceNotFound, err)
		}
		directoryAuthority, err := readWorkspacePrincipalDirectoryAuthority(ctx, txApp, workspaceID)
		if err != nil {
			return err
		}
		revision := directoryAuthority.PermissionRevision
		if cursor.Version != 0 && (cursor.Revision != revision ||
			cursor.AuxRevision != directoryAuthority.GroupRevision ||
			cursor.PersonRevision != directoryAuthority.PersonRevision) {
			return errTeamspaceConflict
		}
		settings, err := loadTeamspaceSettings(txApp, authority)
		if err != nil {
			return err
		}
		rows := []storedTeamspace{}
		err = txApp.DB().NewQuery(`
			SELECT [[id]],[[workspace_id]],[[name]],[[icon]],[[description]],[[access]],
				[[member_page_role]],[[open_page_role]],[[outside_access]],[[disable_publishing]],[[disable_guests]],[[disable_export]],[[members_can_invite]],
				[[members_can_edit_sidebar]],[[archived_at]],[[revision]],[[created_by]],
				[[updated_by]],[[created_at]],[[updated_at]]
			FROM {{leafen_teamspaces}}
			WHERE [[workspace_id]]={:workspaceId} AND [[deleted_at]]=0 AND [[archived_at]]=0
				AND ([[updated_at]]>{:afterUpdated} OR ([[updated_at]]={:afterUpdated} AND [[id]]>{:afterId}))
			ORDER BY [[updated_at]],[[id]] LIMIT {:limit}
		`).WithContext(ctx).Bind(dbx.Params{
			"workspaceId": workspaceID, "afterUpdated": cursor.Updated,
			"afterId": cursor.ID, "limit": teamspaceCollectionWindow + 1,
		}).All(&rows)
		if err != nil {
			return err
		}
		response.SnapshotRevision = revision
		response.Settings = projectTeamspaceSettings(settings)
		response.HasMore = len(rows) > teamspaceCollectionWindow
		if response.HasMore {
			rows = rows[:teamspaceCollectionWindow]
		}
		ids := make([]string, len(rows))
		for index, row := range rows {
			ids[index] = row.ID
		}
		memberships, err := loadActorTeamspaceMembers(txApp, authority, ids)
		if err != nil {
			return err
		}
		pendingRequests, err := loadActorPendingTeamspaceRequests(txApp, authority, ids)
		if err != nil {
			return err
		}
		visibleIDs := make([]string, 0, len(rows))
		for _, row := range rows {
			decision := decideTeamspaceAccess(row, settings, memberships, pendingRequests, authority)
			if !decision.Visible && !(everyActive && authority.Workspace.Role == workspaceRoleOwner) {
				continue
			}
			response.Teamspaces = append(response.Teamspaces, projectTeamspace(row))
			response.Decisions = append(response.Decisions, decision)
			visibleIDs = append(visibleIDs, row.ID)
		}
		response.Summaries, err = readTeamspaceSummaries(ctx, txApp, workspaceID, visibleIDs)
		if err != nil {
			return err
		}
		letStandInsJoin(authority, response.Decisions, response.Summaries)
		if cursor.Version == 0 {
			if response.ArchivedPlaces, err = readArchivedTeamspacePlaces(ctx, txApp, authority, settings); err != nil {
				return err
			}
		}
		if response.HasMore && len(rows) > 0 {
			last := rows[len(rows)-1]
			response.NextCursor = encodeTeamspaceCursor(teamspaceCursor{
				Version: teamspaceSchemaVersion, Revision: revision,
				AuxRevision: directoryAuthority.GroupRevision, PersonRevision: directoryAuthority.PersonRevision,
				Updated: last.UpdatedAt, ID: last.ID,
			})
		}
		return nil
	})
	return response, err
}
