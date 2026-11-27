package app

import (
	"database/sql"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Answering a request to join a closed teamspace (action respond). A request
// stays while its workspace lives when the person who made it is deleted,
// deactivated or suspended, as their teamspace places do (2026-11-07,
// account_deletion_workspace_inventory.go), but it is theirs, and it takes
// effect only while they may act (accountMayAct), as their keys and mail do:
// approving it then is refused, and the deciders' list leaves it out
// (readTeamspaceManagementState). Denying it stays open. A deactivated or
// suspended person's request comes back with the account.

func respondTeamspaceJoinRequest(
	app core.App, authority teamspaceAuthority, teamspace storedTeamspace, input teamspaceMutationInput, now int64,
) (teamspaceMutationResponse, error) {
	request := storedTeamspaceJoinRequest{}
	err := app.DB().NewQuery(`
		UPDATE {{leafen_teamspace_join_requests}} SET [[status]]={:status},[[revision]]=[[revision]]+1,
			[[decided_by]]={:actorId},[[decided_at]]={:now},[[updated_at]]={:now}
		WHERE [[id]]={:id} AND [[workspace_id]]={:workspaceId} AND [[teamspace_id]]={:teamspaceId} AND [[status]]='pending'
		RETURNING ` + teamspaceJoinRequestColumns + `
	`).Bind(dbx.Params{
		"status": input.JoinResponse, "actorId": authority.Workspace.PrincipalID, "now": now,
		"id": input.JoinRequestID, "workspaceId": teamspace.WorkspaceID, "teamspaceId": teamspace.ID,
	}).One(&request)
	if errors.Is(err, sql.ErrNoRows) {
		return teamspaceMutationResponse{}, teamspaceStateRefused("joinRequestId names no request to join this teamspace that is waiting for an answer")
	}
	response := baseTeamspaceMutationResponse(teamspace)
	response.JoinRequest = pointer(projectTeamspaceJoinRequest(request))
	if err != nil || input.JoinResponse != teamspaceRequestApproved {
		return response, err
	}
	// The mutation's transaction undoes the answer with the refusal, so the
	// request keeps waiting.
	if active, err := accountMayAct(app, request.UserID); err != nil {
		return response, err
	} else if !active {
		return teamspaceMutationResponse{}, teamspaceStateRefused(
			"joinRequestId names a request from an account that can't act now (deactivated, suspended or deleted): deny it instead")
	}
	response.Member, err = upsertTeamspaceMember(app, authority, teamspace.ID, "user", request.UserID, teamspaceMemberRoleMember, now)
	if err == nil {
		err = notifyTeamspaceJoinApproved(app, teamspace, request, authority.Workspace.PrincipalID)
	}
	return response, err
}
