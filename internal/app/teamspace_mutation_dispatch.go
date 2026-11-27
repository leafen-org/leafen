package app

import (
	"database/sql"
	"errors"

	"github.com/pocketbase/pocketbase/core"
)

type storedTeamspaceReceipt struct {
	ID           string `db:"id"`
	WorkspaceID  string `db:"workspace_id"`
	RequestHash  string `db:"request_hash"`
	ResponseJSON string `db:"response_json"`
}

type teamspaceMutationTurn struct {
	Response  teamspaceMutationResponse
	ReceiptID string
	Created   bool
}

func applyTeamspaceMutation(
	app core.App, authority teamspaceAuthority, input teamspaceMutationInput, now int64,
) (teamspaceMutationResponse, error) {
	if input.Action == teamspaceActionCreate {
		return createTeamspace(app, authority, input, now)
	}
	if input.Action == teamspaceActionUpdateSettings {
		return updateTeamspaceSettings(app, authority, input, now)
	}
	if input.Action == teamspaceActionSetDefaults {
		return setDefaultTeamspaces(app, authority, input, now)
	}
	// Joining, asking, leaving and — where the teamspace lets its members —
	// inviting or removing members are not only its owners'. The rest is
	// those who manage it (managesTeamspace), not a workspace owner who is
	// only its member.
	memberMayAct := input.Action == teamspaceActionJoin || input.Action == teamspaceActionRequest ||
		input.Action == teamspaceActionLeave || input.Action == teamspaceActionAddMember ||
		input.Action == teamspaceActionRemoveMember
	// A join through the teamspace's open invite link reaches even an
	// invite-only teamspace (teamspace_invite_links.go).
	invited := false
	if input.Action == teamspaceActionJoin && input.InviteToken != "" {
		admitted, err := teamspaceInviteAdmits(app, input.TeamspaceID, input.InviteToken)
		if err != nil {
			return teamspaceMutationResponse{}, err
		}
		if !admitted {
			return teamspaceMutationResponse{}, errTeamspaceNotFound
		}
		invited = true
	}
	authority, teamspace, decision, err := authorizeTeamspaceInvited(
		app, authority.Workspace.PrincipalID, authority.Workspace.WorkspaceID, input.TeamspaceID, !memberMayAct, invited,
	)
	if err != nil {
		return teamspaceMutationResponse{}, err
	}
	if err := authorizeTeamspaceMemberAction(app, authority, teamspace, decision, input); err != nil {
		return teamspaceMutationResponse{}, err
	}
	if teamspace.Revision != input.ExpectedRevision {
		return teamspaceMutationResponse{}, errTeamspaceConflict
	}
	// Only a teamspace's owners change its access level, "기본" too, as
	// Notion's: a workspace owner who doesn't own it sees it dim (its code
	// and seen 2026-11-11).
	if (input.Action == teamspaceActionUpdate && input.Access != teamspace.Access ||
		input.Action == teamspaceActionSetDefault) && decision.MembershipRole != teamspaceMemberRoleOwner {
		return teamspaceMutationResponse{}, errTeamspaceNotFound
	}
	// Only its owners archive a teamspace or bring it back, even the
	// workspace's owners not (Notion's "팀스페이스 보관" shows to its owners
	// alone, and its code, read 2026-11-11, lets only them restore one).
	if (input.Action == teamspaceActionArchive || input.Action == teamspaceActionRestore) &&
		decision.MembershipRole != teamspaceMemberRoleOwner {
		return teamspaceMutationResponse{}, errTeamspaceNotFound
	}
	if input.Action == teamspaceActionSetDefault && teamspace.ArchivedAt != 0 {
		return teamspaceMutationResponse{}, teamspaceStateRefused("the teamspace is archived: restore it before it is a default")
	}
	if input.Action != teamspaceActionUpdate && input.Action != teamspaceActionArchive &&
		input.Action != teamspaceActionRestore && input.Action != teamspaceActionDelete {
		if err := bumpTeamspaceRevision(app, &teamspace); err != nil {
			return teamspaceMutationResponse{}, err
		}
	}
	switch input.Action {
	case teamspaceActionUpdate:
		return updateTeamspace(app, authority, teamspace, input, now)
	case teamspaceActionJoin:
		if !decision.CanJoin && !(invited && !decision.Joined && teamspace.ArchivedAt == 0) {
			return teamspaceMutationResponse{}, errTeamspaceNotFound
		}
		member, err := upsertTeamspaceMember(
			app, authority, teamspace.ID, "user", authority.Workspace.PrincipalID, teamspaceMemberRoleMember, now,
		)
		return memberMutationResponse(teamspace, member), err
	case teamspaceActionRequest:
		if !decision.CanRequest {
			return teamspaceMutationResponse{}, errTeamspaceNotFound
		}
		message, _ := teamspaceJoinMessage(input.JoinMessage)
		request, err := createTeamspaceJoinRequest(app, authority, teamspace.ID, message, now)
		if err == nil {
			err = notifyTeamspaceJoinRequest(app, teamspace, *request)
		}
		response := baseTeamspaceMutationResponse(teamspace)
		response.JoinRequest = request
		return response, err
	case teamspaceActionRespond:
		return respondTeamspaceJoinRequest(app, authority, teamspace, input, now)
	case teamspaceActionAddMember, teamspaceActionUpdateMember, teamspaceActionRemoveMember:
		return changeTeamspaceMember(app, authority, teamspace, input, now)
	case teamspaceActionLeave:
		input.PrincipalType, input.PrincipalID = "user", authority.Workspace.PrincipalID
		input.Action = teamspaceActionRemoveMember
		return changeTeamspaceMember(app, authority, teamspace, input, now)
	case teamspaceActionSetDefault:
		if authority.Workspace.Role != workspaceRoleOwner {
			return teamspaceMutationResponse{}, errTeamspaceNotFound
		}
		if teamspace.Access == teamspaceAccessPrivate {
			return teamspaceMutationResponse{}, errTeamspacePrivateDefault
		}
		// One more default teamspace; the others stay.
		current, err := loadTeamspaceSettings(app, authority)
		if err != nil {
			return teamspaceMutationResponse{}, err
		}
		settings, err := storeTeamspaceSettings(app, authority, appendDefaultTeamspace(current.defaultIDs(), input.TeamspaceID), nil, now)
		response := baseTeamspaceMutationResponse(teamspace)
		response.Settings = settings
		return response, err
	case teamspaceActionArchive, teamspaceActionRestore, teamspaceActionDelete:
		return transitionTeamspace(app, authority, teamspace, input, now)
	default:
		return teamspaceMutationResponse{}, errTeamspaceInvalid
	}
}

// A member who doesn't manage it (managesTeamspace) — a workspace owner who
// is only its member too — may invite or remove only members, and only where
// the teamspace's security lets every member do so, as Notion's "who can
// invite or remove teamspace members" — save a workspace owner putting
// themselves in (privilegedTeamspaceSelfMembership). A restricted member
// changes no one's place in it — adding, removing, a role, a request
// answered — its owner or not, while its settings stay theirs to change as
// its owner (decision.MembersLocked, decideTeamspaceAccess; Notion's free
// workspace 2026-11-18: a restricted owner's role change, removal and add
// each refused for lacking the access it takes, their description saved). Leaving needs a
// membership of one's own: someone in it only through a group ("group") or
// as everyone in a default one has none to take out, and the screens say so
// from the same decision.
func authorizeTeamspaceMemberAction(
	app core.App, authority teamspaceAuthority, teamspace storedTeamspace,
	decision teamspaceAccessDecisionWire, input teamspaceMutationInput,
) error {
	if input.Action == teamspaceActionLeave {
		if decision.MembershipSource != "explicit" {
			return errTeamspaceNotFound
		}
		return nil
	}
	if decision.MembersLocked && (input.Action == teamspaceActionAddMember || input.Action == teamspaceActionUpdateMember ||
		input.Action == teamspaceActionRemoveMember || input.Action == teamspaceActionRespond) {
		return errTeamspaceNotFound
	}
	if input.Action != teamspaceActionAddMember && input.Action != teamspaceActionRemoveMember ||
		privilegedTeamspaceSelfMembership(authority, input) {
		return nil
	}
	if manages, err := managesTeamspace(app, authority, teamspace.ID, decision); err != nil || manages {
		return err
	}
	if decision.MembershipRole != teamspaceMemberRoleMember || !teamspaceMembersInvite(teamspace) {
		return errTeamspaceNotFound
	}
	if input.Action == teamspaceActionAddMember {
		if input.MemberRole != teamspaceMemberRoleMember {
			return errTeamspaceNotFound
		}
		// Adding someone already there would change their role.
		if _, err := loadActiveTeamspaceMember(app, teamspace.ID, input.PrincipalType, input.PrincipalID); err == nil {
			return teamspaceStateRefused("principalId is in the teamspace already")
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		return nil
	}
	current, err := loadActiveTeamspaceMember(app, teamspace.ID, input.PrincipalType, input.PrincipalID)
	if errors.Is(err, sql.ErrNoRows) {
		return errTeamspaceNotMember
	}
	if err != nil {
		return err
	}
	if current.Role == teamspaceMemberRoleOwner {
		return errTeamspaceNotFound
	}
	return nil
}

func baseTeamspaceMutationResponse(row storedTeamspace) teamspaceMutationResponse {
	return teamspaceMutationResponse{TeamspaceID: row.ID, Revision: row.Revision}
}

func memberMutationResponse(row storedTeamspace, member *teamspaceMemberWire) teamspaceMutationResponse {
	response := baseTeamspaceMutationResponse(row)
	response.Member = member
	return response
}

func pointer[T any](value T) *T { return &value }
