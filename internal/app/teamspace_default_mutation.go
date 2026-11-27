package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"slices"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A workspace owner chooses its default teamspaces, as Notion's "기본
// 팀스페이스" field and its "업데이트": the whole list at once, or one
// teamspace more, as a teamspace's access set to "기본" does. A workspace
// keeps at least one, as Notion's refuses an empty list
// (atLeastOneDefaultTeam, its code and seen 2026-11-11); one read with none,
// as a workspace without teamspaces has, stays so until one is chosen.

// setDefaultTeamspaces keeps the given teamspaces as the workspace's default
// ones, in that order; never none.
func setDefaultTeamspaces(
	app core.App, authority teamspaceAuthority, input teamspaceMutationInput, now int64,
) (teamspaceMutationResponse, error) {
	if authority.Workspace.Role != workspaceRoleOwner {
		return teamspaceMutationResponse{}, errTeamspaceNotFound
	}
	settings, err := loadTeamspaceSettings(app, authority)
	if err != nil || settings.Revision != input.ExpectedRevision {
		return teamspaceMutationResponse{}, errors.Join(err, errTeamspaceConflict)
	}
	if len(input.DefaultTeamspaceIDs) == 0 {
		return teamspaceMutationResponse{}, errTeamspaceDefaultRequired
	}
	defaults, added := []string{}, []string{}
	for _, id := range input.DefaultTeamspaceIDs {
		if err := checkDefaultTeamspace(app, authority, id); err != nil {
			return teamspaceMutationResponse{}, err
		}
		if !settings.isDefault(id) {
			added = append(added, id)
		}
		defaults = appendDefaultTeamspace(defaults, id)
	}
	if err := checkOwnsTeamspaces(app, authority, added); err != nil {
		return teamspaceMutationResponse{}, err
	}
	stored, err := storeTeamspaceSettings(app, authority, defaults, nil, now)
	return teamspaceMutationResponse{
		TeamspaceID: authority.Workspace.WorkspaceID,
		Revision:    input.ExpectedRevision + 1,
		Settings:    stored,
	}, err
}

// checkDefaultTeamspace says a teamspace may be one of its workspace's
// default teamspaces: it is there and in use, and not one only its members
// see, since everybody is in a default one.
func checkDefaultTeamspace(app core.App, authority teamspaceAuthority, teamspaceID string) error {
	row := struct {
		Access     string `db:"access"`
		ArchivedAt int64  `db:"archived_at"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[access]],[[archived_at]] FROM {{leafen_teamspaces}}
		WHERE [[id]]={:id} AND [[workspace_id]]={:workspaceId} AND [[deleted_at]]=0
	`).Bind(dbx.Params{"id": teamspaceID, "workspaceId": authority.Workspace.WorkspaceID}).One(&row)
	if errors.Is(err, sql.ErrNoRows) {
		return teamspaceStateRefused("%s names no teamspace of the workspace to be a default", teamspaceID)
	}
	if err == nil && row.ArchivedAt != 0 {
		return teamspaceStateRefused("%s is archived: restore it before it is a default", teamspaceID)
	}
	if err != nil {
		return err
	}
	if row.Access == teamspaceAccessPrivate {
		return errTeamspacePrivateDefault
	}
	return nil
}

// checkOwnsTeamspaces says the person owns each of these teamspaces, as
// only a teamspace's owners make it a default one in Notion
// (nonOwnersCannotSetIsDefault, its code 2026-11-11): its access level is
// theirs to change, even for the workspace's owners.
func checkOwnsTeamspaces(app core.App, authority teamspaceAuthority, teamspaceIDs []string) error {
	memberships, err := loadActorTeamspaceMembers(app, authority, teamspaceIDs)
	if err != nil {
		return err
	}
	owned := map[string]bool{}
	for _, membership := range memberships {
		owned[membership.TeamspaceID] = owned[membership.TeamspaceID] || membership.Role == teamspaceMemberRoleOwner
	}
	for _, id := range teamspaceIDs {
		if !owned[id] {
			return errTeamspaceNotFound
		}
	}
	return nil
}

// checkLastDefaultTeamspaceGoes says the workspace's only default teamspace
// may be archived or deleted, as Notion's lets it only while the workspace has
// one member and that person is in no other teamspace in use (its code,
// 2026-11-11: multipleWorkspaceMembers, multipleUnarchivedTeams): then nobody
// is left to hold a default teamspace for. Otherwise another is made default
// first.
func checkLastDefaultTeamspaceGoes(app core.App, authority teamspaceAuthority, teamspaceID string) error {
	row := struct {
		Members int `db:"members"`
		Others  int `db:"others"`
	}{}
	err := app.DB().NewQuery(`
		SELECT ` + workspaceMemberCountSQL("{:workspaceId}") + ` AS [[members]],
			(SELECT COUNT(*) FROM {{leafen_teamspaces}} [[other]]
			WHERE [[other]].[[workspace_id]]={:workspaceId} AND [[other]].[[id]]!={:teamspaceId}
				AND [[other]].[[archived_at]]=0 AND [[other]].[[deleted_at]]=0
				AND EXISTS (SELECT 1 FROM {{leafen_teamspace_members}} [[member]]
					WHERE [[member]].[[teamspace_id]]=[[other]].[[id]] AND [[member]].[[deleted_at]]=0
						AND ([[member]].[[principal_type]]='user' AND [[member]].[[principal_id]]={:actorId}
							OR [[member]].[[principal_type]]='group' AND [[member]].[[principal_id]] IN (
								SELECT [[group_id]] FROM {{leafen_workspace_permission_group_members}}
								WHERE [[user_id]]={:actorId})))) AS [[others]]
	`).Bind(dbx.Params{
		"workspaceId": authority.Workspace.WorkspaceID, "teamspaceId": teamspaceID,
		"actorId": authority.Workspace.PrincipalID,
	}).One(&row)
	if err != nil {
		return err
	}
	if row.Members > 1 || row.Others > 0 {
		return errTeamspaceDefaultRequired
	}
	return nil
}

// restoredDefaultTeamspaces is the workspace's default teamspaces once an
// archived teamspace comes back, as Notion's "팀스페이스 복원" (its code,
// 2026-11-11: canRestore, restoreArchivedTeam): it comes back while the
// workspace has a default teamspace in use, and stays out of the default
// ones; with none in use, only an open one comes back, as the default one
// (any other is refused until a default teamspace is in use). Nil when the
// list stays as it is.
func restoredDefaultTeamspaces(
	app core.App, authority teamspaceAuthority, settings storedTeamspaceSettings, row storedTeamspace,
) ([]string, error) {
	listed, err := json.Marshal(settings.defaultIDs())
	if err != nil {
		return nil, err
	}
	var active int
	err = app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_teamspaces}}
		WHERE [[workspace_id]]={:workspaceId} AND [[archived_at]]=0 AND [[deleted_at]]=0
			AND [[id]] IN (SELECT [[value]] FROM json_each({:defaultIds}))
	`).Bind(dbx.Params{
		"workspaceId": authority.Workspace.WorkspaceID, "defaultIds": string(listed),
	}).Row(&active)
	if err != nil || active > 0 {
		return nil, err
	}
	if row.Access != teamspaceAccessOpen {
		return nil, errTeamspaceDefaultRequired
	}
	return appendDefaultTeamspace(settings.defaultIDs(), row.ID), nil
}

// appendDefaultTeamspace is a list of default teamspaces with one more at
// its end, unless it is there already.
func appendDefaultTeamspace(defaults []string, teamspaceID string) []string {
	for _, id := range defaults {
		if id == teamspaceID {
			return defaults
		}
	}
	return append(defaults, teamspaceID)
}

// keepDefaultTeamspaceMembers makes everyone a teamspace held as one of its
// workspace's default teamspaces its member for real once it no longer is
// one: its owner and members but restricted ones, as a member added by
// hand, unless they are in it already. Notion's default teamspace adds them,
// current members and new ones alike, and taking it off only stops adding
// anyone more (seen 2026-11-11, its settings' notes on both). A deleted teamspace keeps no one, and an account being
// deleted is made no member (account_new_places.go).
func keepDefaultTeamspaceMembers(
	app core.App, authority teamspaceAuthority, before, after []string, now int64,
) error {
	for _, teamspaceID := range before {
		if slices.Contains(after, teamspaceID) {
			continue
		}
		_, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_teamspace_members}}
				([[id]],[[workspace_id]],[[teamspace_id]],[[principal_type]],[[principal_id]],[[workspace_member_id]],
				 [[role]],[[revision]],[[deleted_at]],[[created_by]],[[created_at]],[[updated_at]])
			SELECT substr(lower(hex(randomblob(8))),1,15),[[teamspace]].[[workspace_id]],[[teamspace]].[[id]],
				'user',[[person]].[[user_id]],[[person]].[[user_id]],'member',1,0,{:actorId},{:now},{:now}
			FROM {{leafen_teamspaces}} [[teamspace]]
			JOIN (
				SELECT [[workspace_owner]] AS [[user_id]] FROM {{leafen_workspaces}} WHERE [[id]]={:workspaceId}
				UNION
				SELECT [[membership]].[[user_id]] FROM {{leafen_workspace_memberships}} [[membership]]
				WHERE [[membership]].[[workspace_id]]={:workspaceId}
					AND NOT ` + workspaceRestrictedMemberSQL("[[membership]].[[workspace_id]]", "[[membership]].[[user_id]]") + `
			) [[person]]
			WHERE [[teamspace]].[[id]]={:teamspaceId} AND [[teamspace]].[[workspace_id]]={:workspaceId}
				AND [[teamspace]].[[deleted_at]]=0 AND ` + accountTakesNewPlacesSQL("[[person]].[[user_id]]") + `
			ON CONFLICT ([[teamspace_id]],[[principal_type]],[[principal_id]]) DO UPDATE SET
				[[role]]='member',[[revision]]=[[revision]]+1,[[deleted_at]]=0,[[updated_at]]=excluded.[[updated_at]]
			WHERE [[deleted_at]]!=0
		`).Bind(dbx.Params{
			"workspaceId": authority.Workspace.WorkspaceID, "teamspaceId": teamspaceID,
			"actorId": authority.Workspace.PrincipalID, "now": now,
		}).Execute()
		if err != nil {
			return err
		}
	}
	return nil
}
