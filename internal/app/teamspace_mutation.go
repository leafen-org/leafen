package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

const teamspaceMutationLockNamespace = "teamspace-mutation"

func executeTeamspaceMutation(
	ctx context.Context,
	app core.App,
	roomInvalidator workspaceRoomInvalidator,
	actorID, workspaceID string,
	input teamspaceMutationInput,
) (teamspaceMutationResponse, bool, error) {
	normalizeTeamspaceMutation(&input)
	if workspaceID == "" || actorID == "" {
		return teamspaceMutationResponse{}, false, teamspaceRefused("the workspace ID in the path is empty")
	}
	if err := validateTeamspaceMutation(input); err != nil {
		return teamspaceMutationResponse{}, false, err
	}
	requestHash, err := teamspaceMutationRequestHash(workspaceID, input)
	if err != nil {
		return teamspaceMutationResponse{}, false, err
	}
	release := acquireProductOperationLock(teamspaceMutationLockNamespace, workspaceID)
	defer release()
	turn := teamspaceMutationTurn{}
	err = app.RunInTransaction(func(txApp core.App) error {
		existing, readErr := loadTeamspaceReceipt(txApp, actorID, input.RequestID)
		if readErr == nil {
			if existing.WorkspaceID != workspaceID || existing.RequestHash != requestHash {
				return errTeamspaceRequestIDReused
			}
			if json.Unmarshal([]byte(existing.ResponseJSON), &turn.Response) != nil {
				return errTeamspaceConflict
			}
			turn.Response.Duplicate = true
			turn.ReceiptID = existing.ID
			return nil
		}
		if !errors.Is(readErr, sql.ErrNoRows) {
			return readErr
		}
		authority, err := loadTeamspaceAuthority(txApp, actorID, workspaceID)
		if err != nil {
			return refusedUnlessStorage(errTeamspaceNotFound, err)
		}
		now := time.Now().UTC().UnixMilli()
		response, err := applyTeamspaceMutation(txApp, authority, input, now)
		if err != nil {
			return err
		}
		response.WorkspaceID, response.RequestID, response.Action = workspaceID, input.RequestID, input.Action
		response.PermissionCursor, err = advanceWorkspacePermissionCursor(txApp, workspaceID, now)
		if err != nil {
			return err
		}
		turn.ReceiptID = security.RandomString(15)
		response.PublicationPending = true
		encoded, err := json.Marshal(response)
		if err != nil {
			return err
		}
		if err = insertOrganizationAuditEvent(txApp, organizationAuditEvent{
			ID: turn.ReceiptID, OrganizationID: authority.Workspace.Organization.OrganizationID,
			WorkspaceID: workspaceID, ActorID: actorID, Action: "teamspace." + input.Action,
			TargetType: "teamspace", TargetID: response.TeamspaceID,
			Metadata: map[string]any{"requestId": input.RequestID, "revision": response.Revision}, OccurredAt: now,
		}); err != nil {
			return err
		}
		if _, err = txApp.DB().NewQuery(`
			INSERT INTO {{leafen_teamspace_mutation_receipts}}
				([[id]],[[workspace_id]],[[actor_id]],[[request_id]],[[request_hash]],[[response_json]],[[created_at]])
			VALUES ({:id},{:workspaceId},{:actorId},{:requestId},{:requestHash},{:response},{:now})
		`).Bind(dbx.Params{
			"id": turn.ReceiptID, "workspaceId": workspaceID, "actorId": actorID,
			"requestId": input.RequestID, "requestHash": requestHash, "response": string(encoded), "now": now,
		}).Execute(); err != nil {
			return err
		}
		if _, err = txApp.DB().NewQuery(`
			INSERT INTO {{leafen_teamspace_publication_jobs}}
				([[id]],[[workspace_id]],[[teamspace_id]],[[receipt_id]],[[status]],[[attempts]],[[created_at]],[[updated_at]])
			VALUES ({:id},{:workspaceId},{:teamspaceId},{:receiptId},'pending',0,{:now},{:now})
		`).Bind(dbx.Params{
			"id": security.RandomString(15), "workspaceId": workspaceID,
			"teamspaceId": response.TeamspaceID, "receiptId": turn.ReceiptID, "now": now,
		}).Execute(); err != nil {
			return err
		}
		turn.Response, turn.Created = response, true
		return nil
	})
	if err != nil {
		return teamspaceMutationResponse{}, false, err
	}
	settled, _ := settleTeamspacePublication(ctx, app, roomInvalidator, turn.ReceiptID)
	turn.Response.PublicationPending = !settled
	return turn.Response, turn.Created, nil
}

func loadTeamspaceReceipt(app core.App, actorID, requestID string) (storedTeamspaceReceipt, error) {
	row := storedTeamspaceReceipt{}
	err := app.DB().NewQuery(`
		SELECT [[id]],[[workspace_id]],[[request_hash]],[[response_json]]
		FROM {{leafen_teamspace_mutation_receipts}}
		WHERE [[actor_id]]={:actorId} AND [[request_id]]={:requestId}
	`).Bind(dbx.Params{"actorId": actorID, "requestId": requestID}).One(&row)
	return row, err
}

func createTeamspace(
	app core.App, authority teamspaceAuthority, input teamspaceMutationInput, now int64,
) (teamspaceMutationResponse, error) {
	settings, err := loadTeamspaceSettings(app, authority)
	if err != nil || settings.OwnersOnlyCreate && authority.Workspace.Role != workspaceRoleOwner || authority.Workspace.Restricted {
		return teamspaceMutationResponse{}, errors.Join(err, errTeamspaceNotFound)
	}
	openRole, outside := storedOutsideRole(input.Access, input.OpenPageRole, "")
	if err := teamspaceKeptRefusal(app, authority, nil, input, openRole, outside); err != nil {
		return teamspaceMutationResponse{}, err
	}
	row := storedTeamspace{
		ID: security.RandomString(15), WorkspaceID: authority.Workspace.WorkspaceID,
		Name: input.Name, Icon: input.Icon, Description: input.Description, Access: input.Access,
		MemberPageRole: input.MemberPageRole, OpenPageRole: openRole, OutsideAccess: outside,
		DisablePublishing: pageSecurityChoice(input.DisablePublishing, false),
		DisableGuests:     pageSecurityChoice(input.DisableGuests, false),
		DisableExport:     pageSecurityChoice(input.DisableExport, false),
		MembersCanInvite:  *input.MembersCanInvite, MembersCanEditSidebar: *input.MembersCanEditSidebar,
		Revision: 1, CreatedBy: authority.Workspace.PrincipalID, UpdatedBy: authority.Workspace.PrincipalID,
		CreatedAt: now, UpdatedAt: now,
	}
	_, err = app.DB().NewQuery(`
		INSERT INTO {{leafen_teamspaces}}
			([[id]],[[workspace_id]],[[name]],[[icon]],[[description]],[[access]],[[member_page_role]],
			 [[open_page_role]],[[outside_access]],[[members_can_invite]],[[members_can_edit_sidebar]],
			 [[disable_publishing]],[[disable_guests]],[[disable_export]],[[revision]],
			 [[created_by]],[[updated_by]],[[created_at]],[[updated_at]])
		VALUES ({:id},{:workspaceId},{:name},{:icon},{:description},{:access},{:memberPageRole},
			{:openPageRole},{:outside},{:canInvite},{:canEditSidebar},{:noPublishing},{:noGuests},{:noExport},
			1,{:actorId},{:actorId},{:now},{:now})
	`).Bind(dbx.Params{
		"id": row.ID, "workspaceId": row.WorkspaceID, "name": row.Name, "icon": row.Icon,
		"description": row.Description, "access": row.Access, "memberPageRole": row.MemberPageRole,
		"openPageRole": row.OpenPageRole, "outside": row.OutsideAccess, "canInvite": row.MembersCanInvite,
		"noPublishing": row.DisablePublishing, "noGuests": row.DisableGuests, "noExport": row.DisableExport,
		"canEditSidebar": row.MembersCanEditSidebar, "actorId": row.CreatedBy, "now": now,
	}).Execute()
	if err != nil {
		return teamspaceMutationResponse{}, err
	}
	// A new teamspace isn't the default until an owner makes it so, as in
	// Notion.
	member, err := upsertTeamspaceMember(app, authority, row.ID, "user", row.CreatedBy, teamspaceMemberRoleOwner, now)
	if err == nil {
		err = settleHeldIconFiles(app, row.WorkspaceID, row.Icon)
	}
	response := baseTeamspaceMutationResponse(row)
	response.Teamspace, response.Member = pointer(projectTeamspace(row)), member
	return response, err
}

func updateTeamspace(
	app core.App, authority teamspaceAuthority, row storedTeamspace, input teamspaceMutationInput, now int64,
) (teamspaceMutationResponse, error) {
	if input.Access == teamspaceAccessPrivate {
		if settings, err := loadTeamspaceSettings(app, authority); err != nil {
			return teamspaceMutationResponse{}, err
		} else if settings.isDefault(row.ID) {
			return teamspaceMutationResponse{}, errTeamspacePrivateDefault
		}
	}
	openRole, outside := storedOutsideRole(input.Access, input.OpenPageRole, row.OpenPageRole)
	if err := teamspaceKeptRefusal(app, authority, &row, input, openRole, outside); err != nil {
		return teamspaceMutationResponse{}, err
	}
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_teamspaces}} SET [[name]]={:name},[[icon]]={:icon},[[description]]={:description},
			[[access]]={:access},[[member_page_role]]={:memberPageRole},[[open_page_role]]={:openPageRole},
			[[outside_access]]={:outside},[[disable_publishing]]={:noPublishing},[[disable_guests]]={:noGuests},
			[[disable_export]]={:noExport},
			[[members_can_invite]]={:canInvite},[[members_can_edit_sidebar]]={:canEditSidebar},[[revision]]=[[revision]]+1
		WHERE [[id]]={:id} AND [[workspace_id]]={:workspaceId} AND [[revision]]={:revision} AND [[deleted_at]]=0
	`).Bind(dbx.Params{
		"name": input.Name, "icon": input.Icon, "description": input.Description, "access": input.Access,
		"memberPageRole": input.MemberPageRole, "openPageRole": openRole, "outside": outside,
		"noPublishing": pageSecurityChoice(input.DisablePublishing, row.DisablePublishing),
		"noGuests":     pageSecurityChoice(input.DisableGuests, row.DisableGuests),
		"noExport":     pageSecurityChoice(input.DisableExport, row.DisableExport),
		"canInvite":    *input.MembersCanInvite, "canEditSidebar": *input.MembersCanEditSidebar,
		"id": row.ID, "workspaceId": row.WorkspaceID, "revision": row.Revision,
	}).Execute()
	if err != nil {
		return teamspaceMutationResponse{}, err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return teamspaceMutationResponse{}, errTeamspaceConflict
	}
	if input.Icon != row.Icon {
		if err := settleHeldIconFiles(app, row.WorkspaceID, row.Icon, input.Icon); err != nil {
			return teamspaceMutationResponse{}, err
		}
	}
	row.Name, row.Icon, row.Description, row.Access = input.Name, input.Icon, input.Description, input.Access
	row.MemberPageRole, row.OpenPageRole, row.OutsideAccess = input.MemberPageRole, openRole, outside
	row.DisablePublishing = pageSecurityChoice(input.DisablePublishing, row.DisablePublishing)
	row.DisableGuests = pageSecurityChoice(input.DisableGuests, row.DisableGuests)
	row.DisableExport = pageSecurityChoice(input.DisableExport, row.DisableExport)
	row.MembersCanInvite, row.MembersCanEditSidebar = *input.MembersCanInvite, *input.MembersCanEditSidebar
	row.Revision++
	response := baseTeamspaceMutationResponse(row)
	response.Teamspace = pointer(projectTeamspace(row))
	return response, nil
}

// bumpTeamspaceRevision moves a teamspace's revision for a change around it
// (members, join requests, default place). No change after a teamspace is
// made moves its last update (updated_at, updated_by) — its name, icon,
// description, access, members, archiving, restoring — as Notion's settings
// table keeps its date through them all (seen 2026-11-07, 2026-11-18 and
// 2026-11-21); the revision and the socket carry the change to other tabs.
func bumpTeamspaceRevision(app core.App, row *storedTeamspace) error {
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_teamspaces}} SET [[revision]]=[[revision]]+1
		WHERE [[id]]={:id} AND [[workspace_id]]={:workspaceId} AND [[revision]]={:revision} AND [[deleted_at]]=0
	`).Bind(dbx.Params{"id": row.ID, "workspaceId": row.WorkspaceID, "revision": row.Revision}).Execute()
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return errTeamspaceConflict
	}
	row.Revision++
	return nil
}

func upsertTeamspaceMember(
	app core.App, authority teamspaceAuthority, teamspaceID, principalType, principalID, role string, now int64,
) (*teamspaceMemberWire, error) {
	if ok, err := validTeamspacePrincipal(app, authority, principalType, principalID); err != nil {
		return nil, errors.Join(err, errTeamspaceInvalid)
	} else if !ok {
		return nil, teamspaceRefused("principalId names no %s of the workspace", map[string]string{"user": "member", "group": "group"}[principalType])
	}
	row := storedTeamspaceMember{}
	err := app.DB().NewQuery(`
		INSERT INTO {{leafen_teamspace_members}}
			([[id]],[[workspace_id]],[[teamspace_id]],[[principal_type]],[[principal_id]],[[workspace_member_id]],
			 [[role]],[[revision]],[[deleted_at]],[[created_by]],[[created_at]],[[updated_at]])
		VALUES ({:id},{:workspaceId},{:teamspaceId},{:principalType},{:principalId},{:memberId},
			{:role},1,0,{:actorId},{:now},{:now})
		ON CONFLICT ([[teamspace_id]],[[principal_type]],[[principal_id]]) DO UPDATE SET
			[[role]]=excluded.[[role]],[[revision]]=[[revision]]+1,[[deleted_at]]=0,[[updated_at]]=excluded.[[updated_at]]
		RETURNING [[id]],[[workspace_id]],[[teamspace_id]],[[principal_type]],[[principal_id]],
			[[workspace_member_id]],[[role]],[[revision]],[[created_by]],[[created_at]],[[updated_at]]
	`).Bind(dbx.Params{
		"id": security.RandomString(15), "workspaceId": authority.Workspace.WorkspaceID,
		"teamspaceId": teamspaceID, "principalType": principalType, "principalId": principalID,
		"memberId": principalID, "role": role, "actorId": authority.Workspace.PrincipalID, "now": now,
	}).One(&row)
	value := projectTeamspaceMember(row)
	return &value, err
}

func changeTeamspaceMember(
	app core.App, authority teamspaceAuthority, teamspace storedTeamspace, input teamspaceMutationInput, now int64,
) (teamspaceMutationResponse, error) {
	response := baseTeamspaceMutationResponse(teamspace)
	if input.Action != teamspaceActionRemoveMember {
		current, err := loadActiveTeamspaceMember(app, teamspace.ID, input.PrincipalType, input.PrincipalID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return response, err
		}
		if input.Action == teamspaceActionUpdateMember && errors.Is(err, sql.ErrNoRows) {
			return response, errTeamspaceNotMember
		}
		// A restricted member joins only where the workspace's policy lets the person adding them.
		if input.Action == teamspaceActionAddMember && errors.Is(err, sql.ErrNoRows) {
			memberships, memberErr := loadActorTeamspaceMembers(app, authority, []string{teamspace.ID})
			if memberErr != nil {
				return response, memberErr
			}
			owner := false
			for _, membership := range memberships {
				owner = owner || membership.Role == teamspaceMemberRoleOwner
			}
			if err := enforceRestrictedMemberTeamspaceAdd(app, authority, owner, input.PrincipalType, input.PrincipalID); err != nil {
				return response, err
			}
		}
		if err == nil && current.Role == teamspaceMemberRoleOwner && input.MemberRole != teamspaceMemberRoleOwner {
			owners, countErr := countTeamspaceOwners(app, teamspace.ID)
			if countErr != nil {
				return response, countErr
			}
			if owners < 2 {
				return response, errTeamspaceLastOwner
			}
		}
		member, err := upsertTeamspaceMember(app, authority, teamspace.ID, input.PrincipalType, input.PrincipalID, input.MemberRole, now)
		response.Member = member
		return response, err
	}
	// Everyone in the workspace stays in its default teamspaces; taking a
	// row out would only look like it.
	if settings, err := loadTeamspaceSettings(app, authority); err != nil {
		return response, err
	} else if settings.isDefault(teamspace.ID) {
		return response, errTeamspaceDefaultMember
	}
	owners, err := countTeamspaceOwners(app, teamspace.ID)
	if err != nil {
		return response, err
	}
	if current, err := loadActiveTeamspaceMember(app, teamspace.ID, input.PrincipalType, input.PrincipalID); err == nil &&
		current.Role == teamspaceMemberRoleOwner && owners < 2 {
		return response, errTeamspaceLastOwner
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return response, err
	}
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_teamspace_members}} SET [[deleted_at]]={:now},[[revision]]=[[revision]]+1,[[updated_at]]={:now}
		WHERE [[teamspace_id]]={:teamspaceId} AND [[principal_type]]={:kind} AND [[principal_id]]={:id}
			AND [[deleted_at]]=0 AND ([[role]]!='owner' OR {:owners}>1)
	`).Bind(dbx.Params{
		"now": now, "teamspaceId": teamspace.ID, "kind": input.PrincipalType,
		"id": input.PrincipalID, "owners": owners,
	}).Execute()
	if err != nil {
		return response, err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return response, errTeamspaceConflict
	}
	return response, nil
}

func loadActiveTeamspaceMember(
	app core.App, teamspaceID, principalType, principalID string,
) (storedTeamspaceMember, error) {
	row := storedTeamspaceMember{}
	err := app.DB().NewQuery(`
		SELECT [[id]],[[workspace_id]],[[teamspace_id]],[[principal_type]],[[principal_id]],
			[[workspace_member_id]],[[role]],[[revision]],[[created_by]],[[created_at]],[[updated_at]]
		FROM {{leafen_teamspace_members}}
		WHERE [[teamspace_id]]={:teamspaceId} AND [[principal_type]]={:kind}
			AND [[principal_id]]={:id} AND [[deleted_at]]=0
	`).Bind(dbx.Params{"teamspaceId": teamspaceID, "kind": principalType, "id": principalID}).One(&row)
	return row, err
}

func countTeamspaceOwners(app core.App, teamspaceID string) (int, error) {
	var owners int
	err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_teamspace_members}}
		WHERE [[teamspace_id]]={:teamspaceId} AND [[role]]='owner' AND [[deleted_at]]=0
	`).Bind(dbx.Params{"teamspaceId": teamspaceID}).Row(&owners)
	return owners, err
}

func updateTeamspaceSettings(
	app core.App, authority teamspaceAuthority, input teamspaceMutationInput, now int64,
) (teamspaceMutationResponse, error) {
	if authority.Workspace.Role != workspaceRoleOwner {
		return teamspaceMutationResponse{}, errTeamspaceNotFound
	}
	settings, err := loadTeamspaceSettings(app, authority)
	if err != nil || settings.Revision != input.ExpectedRevision {
		return teamspaceMutationResponse{}, errors.Join(err, errTeamspaceConflict)
	}
	stored, err := storeTeamspaceSettings(app, authority, settings.defaultIDs(), input.OwnersOnlyCreate, now)
	return teamspaceMutationResponse{
		TeamspaceID: authority.Workspace.WorkspaceID,
		Revision:    input.ExpectedRevision + 1,
		Settings:    stored,
	}, err
}

// storeTeamspaceSettings keeps a workspace's default teamspaces, in their
// order, and whether only its owners make teamspaces (as it was, unless
// given).
func storeTeamspaceSettings(
	app core.App, authority teamspaceAuthority, defaultIDs []string, ownersOnly *bool, now int64,
) (*teamspaceSettingsWire, error) {
	current, err := loadTeamspaceSettings(app, authority)
	if err != nil {
		return nil, err
	}
	if defaultIDs == nil {
		defaultIDs = []string{}
	}
	// However the list grows — a teamspace's access set to "기본", or the
	// settings field's whole list — it follows one bound, here. Notion's has
	// no cap of its own (its transaction errors name none but "at least one",
	// read 2026-11-14), so the bound is only the request's: the whole list
	// goes back in one set_defaults body (teamspaceMutationMaxBodyBytes), and
	// maxDefaultTeamspaces ids of 15 characters fit there with room. A list
	// kept past it before is saved again as it is, or shorter.
	if len(defaultIDs) > maxDefaultTeamspaces && len(defaultIDs) > len(current.defaultIDs()) {
		return nil, teamspaceRefused("a workspace keeps at most %d default teamspaces", maxDefaultTeamspaces)
	}
	listed, err := json.Marshal(defaultIDs)
	if err != nil {
		return nil, err
	}
	restricted := current.OwnersOnlyCreate
	if ownersOnly != nil {
		restricted = *ownersOnly
	}
	// Whoever a teamspace held as a default one stays in it after
	// (teamspace_default_mutation.go).
	if err := keepDefaultTeamspaceMembers(app, authority, current.defaultIDs(), defaultIDs, now); err != nil {
		return nil, err
	}
	row := storedTeamspaceSettings{}
	err = app.DB().NewQuery(`
		INSERT INTO {{leafen_teamspace_settings}}
			([[workspace_id]],[[default_teamspace_ids]],[[owners_only_create]],[[revision]],[[updated_by]],[[updated_at]])
		VALUES ({:workspaceId},{:defaultIds},{:restricted},2,{:actorId},{:now})
		ON CONFLICT ([[workspace_id]]) DO UPDATE SET [[default_teamspace_ids]]=excluded.[[default_teamspace_ids]],
			[[owners_only_create]]=excluded.[[owners_only_create]],[[revision]]=[[revision]]+1,
			[[updated_by]]=excluded.[[updated_by]],[[updated_at]]=excluded.[[updated_at]]
		RETURNING [[workspace_id]],[[default_teamspace_ids]],[[owners_only_create]],[[revision]],[[updated_by]],[[updated_at]]
	`).Bind(dbx.Params{
		"workspaceId": authority.Workspace.WorkspaceID, "defaultIds": string(listed),
		"restricted": restricted, "actorId": authority.Workspace.PrincipalID, "now": now,
	}).One(&row)
	value := projectTeamspaceSettings(row)
	return &value, err
}

func transitionTeamspace(
	app core.App, authority teamspaceAuthority, row storedTeamspace, input teamspaceMutationInput, now int64,
) (teamspaceMutationResponse, error) {
	action := input.Action
	archivedAt, deletedAt := row.ArchivedAt, int64(0)
	archivedBy := authority.Workspace.PrincipalID
	switch action {
	case teamspaceActionArchive:
		if row.ArchivedAt != 0 {
			return teamspaceMutationResponse{}, teamspaceStateRefused("the teamspace is archived already")
		}
		archivedAt = now
	case teamspaceActionRestore:
		if row.ArchivedAt == 0 {
			return teamspaceMutationResponse{}, teamspaceStateRefused("the teamspace isn't archived")
		}
		archivedAt = 0
		archivedBy = ""
	case teamspaceActionDelete:
		deletedAt = now
	}
	settings, err := loadTeamspaceSettings(app, authority)
	if err != nil {
		return teamspaceMutationResponse{}, err
	}
	// An archived or deleted teamspace stops being a default one; the one
	// given to take its place, if any, becomes one. The only default one
	// goes only as Notion's lets it (checkLastDefaultTeamspaceGoes). The
	// list is kept once the teamspace has changed, so an archived one keeps
	// its people and a deleted one none (teamspace_default_mutation.go).
	var nextDefaults []string
	if (action == teamspaceActionArchive || action == teamspaceActionDelete) && settings.isDefault(row.ID) {
		nextDefaults = settings.withoutDefault(row.ID)
		if replacementID := input.ReplacementDefaultTeamspaceID; replacementID != "" {
			if replacementID == row.ID {
				return teamspaceMutationResponse{}, teamspaceRefused("replacementDefaultTeamspaceId names the teamspace that goes: name another to be a default in its place")
			}
			if err := checkDefaultTeamspace(app, authority, replacementID); err != nil {
				return teamspaceMutationResponse{}, err
			}
			if err := checkOwnsTeamspaces(app, authority, []string{replacementID}); err != nil {
				return teamspaceMutationResponse{}, err
			}
			nextDefaults = appendDefaultTeamspace(nextDefaults, replacementID)
		}
		if len(nextDefaults) == 0 {
			if err := checkLastDefaultTeamspaceGoes(app, authority, row.ID); err != nil {
				return teamspaceMutationResponse{}, err
			}
		}
	}
	// One comes back while a default teamspace is in use, else as the
	// default one when open (restoredDefaultTeamspaces).
	if action == teamspaceActionRestore {
		if nextDefaults, err = restoredDefaultTeamspaces(app, authority, settings, row); err != nil {
			return teamspaceMutationResponse{}, err
		}
	}
	// Its last update stays as it was (bumpTeamspaceRevision); its revision moves.
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_teamspaces}} SET [[archived_at]]={:archivedAt},[[archived_by]]={:archivedBy},
			[[deleted_at]]={:deletedAt},[[revision]]=[[revision]]+1
		WHERE [[id]]={:id} AND [[workspace_id]]={:workspaceId} AND [[revision]]={:revision} AND [[deleted_at]]=0
	`).Bind(dbx.Params{
		"archivedAt": archivedAt, "archivedBy": archivedBy, "deletedAt": deletedAt,
		"id": row.ID, "workspaceId": row.WorkspaceID, "revision": row.Revision,
	}).Execute()
	if err != nil {
		return teamspaceMutationResponse{}, err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return teamspaceMutationResponse{}, errTeamspaceConflict
	}
	row.ArchivedAt, row.Revision = archivedAt, row.Revision+1
	var nextSettings *teamspaceSettingsWire
	if nextDefaults != nil {
		if nextSettings, err = storeTeamspaceSettings(app, authority, nextDefaults, nil, now); err != nil {
			return teamspaceMutationResponse{}, err
		}
	}
	// A deleted teamspace's uploaded icon goes with it (held_icon_files.go).
	if deletedAt != 0 {
		if err := settleHeldIconFiles(app, row.WorkspaceID, row.Icon); err != nil {
			return teamspaceMutationResponse{}, err
		}
	}
	response := baseTeamspaceMutationResponse(row)
	response.Settings = nextSettings
	if deletedAt == 0 {
		response.Teamspace = pointer(projectTeamspace(row))
	}
	return response, nil
}
