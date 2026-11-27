package app

import (
	"context"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// readArchivedTeamspacePlaces are the archived teamspaces whose pages the
// reader still opens (bootstrapTeamspaceAccessSQL), for the band over each
// such page and its path's teamspace piece: its name and icon, its members'
// count and whether the reader is one, and whether they may bring it back,
// which only its owners may (applyTeamspaceMutation).
func readArchivedTeamspacePlaces(
	ctx context.Context, app core.App, authority teamspaceAuthority, settings storedTeamspaceSettings,
) ([]teamspaceArchivedPlaceWire, error) {
	rows := []storedTeamspace{}
	err := app.DB().NewQuery(`
		SELECT [[id]],[[workspace_id]],[[name]],[[icon]],[[description]],[[access]],
			[[member_page_role]],[[open_page_role]],[[outside_access]],[[disable_publishing]],[[disable_guests]],[[disable_export]],[[members_can_invite]],
			[[members_can_edit_sidebar]],[[archived_at]],[[revision]],[[created_by]],
			[[updated_by]],[[created_at]],[[updated_at]]
		FROM {{leafen_teamspaces}} [[teamspace]] INDEXED BY [[idx_leafen_teamspaces_workspace_archived]]
		WHERE [[teamspace]].[[workspace_id]]={:workspaceId} AND [[teamspace]].[[deleted_at]]=0 AND [[teamspace]].[[archived_at]]>0
			AND ` + bootstrapTeamspaceAccessSQL("[[teamspace]]", "{:principalId}") + `
		ORDER BY [[teamspace]].[[id]] LIMIT {:limit}
	`).WithContext(ctx).Bind(dbx.Params{
		"workspaceId": authority.Workspace.WorkspaceID, "principalId": authority.Workspace.PrincipalID,
		"limit": teamspaceCollectionWindow,
	}).All(&rows)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	ids := make([]string, len(rows))
	for index, row := range rows {
		ids[index] = row.ID
	}
	memberships, err := loadActorTeamspaceMembers(app, authority, ids)
	if err != nil {
		return nil, err
	}
	summaries, err := readTeamspaceSummaries(ctx, app, authority.Workspace.WorkspaceID, ids)
	if err != nil {
		return nil, err
	}
	memberCounts := make(map[string]int, len(summaries))
	for _, summary := range summaries {
		memberCounts[summary.TeamspaceID] = summary.MemberCount
	}
	places := make([]teamspaceArchivedPlaceWire, 0, len(rows))
	for _, row := range rows {
		decision := decideTeamspaceAccess(row, settings, memberships, nil, authority)
		places = append(places, teamspaceArchivedPlaceWire{
			ID: row.ID, Name: row.Name, Icon: row.Icon, Access: row.Access, Revision: row.Revision,
			CanRestore:  decision.MembershipRole == teamspaceMemberRoleOwner,
			MemberCount: memberCounts[row.ID], Joined: decision.Joined,
		})
	}
	return places, nil
}

func readArchivedTeamspaceSnapshotState(
	ctx context.Context,
	app core.App,
	principalID, workspaceID, rawCursor string,
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
		if err != nil || authority.Workspace.Role != workspaceRoleOwner {
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
			FROM {{leafen_teamspaces}} INDEXED BY [[idx_leafen_teamspaces_workspace_archived]]
			WHERE [[workspace_id]]={:workspaceId} AND [[deleted_at]]=0 AND [[archived_at]]>0
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
		ids := make([]string, 0, len(rows))
		for _, row := range rows {
			response.Teamspaces = append(response.Teamspaces, projectTeamspace(row))
			ids = append(ids, row.ID)
		}
		// Each with the reader's part in it, as only its owners restore one.
		memberships, err := loadActorTeamspaceMembers(txApp, authority, ids)
		if err != nil {
			return err
		}
		for _, row := range rows {
			response.Decisions = append(response.Decisions, decideTeamspaceAccess(row, settings, memberships, nil, authority))
		}
		response.Summaries, err = readTeamspaceSummaries(ctx, txApp, workspaceID, ids)
		if err != nil {
			return err
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
