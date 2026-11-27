package app

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

var (
	errWorkspaceMemberTargetMissing = errors.New("workspace member target is missing")
	errWorkspaceMemberConflict      = errors.New(errorCodeWorkspaceMemberConflict)
	errWorkspaceMemberAddDisabled   = errors.New(errorCodeWorkspaceMemberAddDisabled)
	errOrganizationMemberInactive   = errors.New("organization membership is not active")
)

type workspaceMemberAdmissionRequest struct {
	ActorID        string
	WorkspaceID    string
	UserID         string
	Email          string
	ResolveByEmail bool
	// Message is the inviter's note an emailed invitation carries.
	Message string
	// ActorOwnAccount is an account the actor brings in for itself — an
	// app's own account, a person an app adds —, which isn't told by email.
	ActorOwnAccount bool
	// Restricted brings someone new in as a restricted member, or invites
	// them as one (workspace_invitation_restricted.go); someone already a
	// member keeps their role.
	Restricted bool
	// Request is the actor's own request, where there is one, whose language
	// an emailed invitation is written in (serverTextLanguageIn).
	Request context.Context
}

type workspaceMemberAdmissionResult struct {
	Membership workspaceMembershipResponse
	Accepted   workspaceMemberAddAcceptedResponse
}

func admitWorkspaceMember(app core.App, request workspaceMemberAdmissionRequest) (workspaceMemberAdmissionResult, error) {
	result := workspaceMemberAdmissionResult{
		Accepted: workspaceMemberAddAcceptedResponse{WorkspaceID: request.WorkspaceID, Accepted: true},
	}
	if err := workspaceRestrictedInvitationAllowed(request.Restricted); err != nil {
		return result, err
	}
	invite := func(txApp core.App, authorization workspaceAuthorization) error {
		mailed, err := inviteWorkspaceMember(request.Request, txApp, request.WorkspaceID, authorization.Organization.OrganizationID,
			request.ActorID, request.Email, time.Now().UTC())
		result.Accepted.Invited, result.Accepted.InviteEmail = err == nil, mailed
		if err == nil && request.Message != "" {
			_, err = txApp.DB().NewQuery(`UPDATE {{leafen_workspace_invitations}} SET [[invite_message]] = {:message}
				WHERE [[workspace_id]] = {:workspaceId} AND [[email]] = {:email} AND [[status]] = 'pending'`).
				Bind(dbx.Params{"message": request.Message, "workspaceId": request.WorkspaceID, "email": request.Email}).Execute()
		}
		if err == nil && request.Restricted {
			_, err = txApp.DB().NewQuery(`UPDATE {{leafen_workspace_invitations}} SET [[restricted]] = 1
				WHERE [[workspace_id]] = {:workspaceId} AND [[email]] = {:email} AND [[status]] = 'pending'`).
				Bind(dbx.Params{"workspaceId": request.WorkspaceID, "email": request.Email}).Execute()
		}
		return err
	}
	err := app.RunInTransaction(func(txApp core.App) error {
		authorization, err := authorizeWorkspace(
			txApp, request.ActorID, request.WorkspaceID, permissionWorkspaceOwner,
		)
		if err != nil {
			return err
		}
		memberID := request.UserID
		if request.ResolveByEmail {
			policy, err := loadWorkspaceMemberAddPolicy(txApp)
			if err != nil {
				return err
			}
			if policy != administratorMemberAddPolicyEnabled {
				return errWorkspaceMemberAddDisabled
			}
			member, err := findWorkspaceMemberByEmail(txApp, request.Email)
			if errors.Is(err, sql.ErrNoRows) {
				return invite(txApp, authorization)
			}
			if err != nil {
				return err
			}
			memberID = member.Id
		} else if _, err := txApp.FindRecordById("users", memberID); errors.Is(err, sql.ErrNoRows) {
			return errWorkspaceMemberTargetMissing
		} else if err != nil {
			return err
		}

		if memberID == authorization.WorkspaceOwnerID {
			if request.ResolveByEmail {
				return nil
			}
			return errWorkspaceMemberConflict
		}
		memberships := []storedWorkspaceMembership{}
		err = txApp.DB().NewQuery(`
			SELECT [[role]] FROM {{leafen_workspace_memberships}}
			WHERE [[workspace_id]] = {:workspaceId} AND [[user_id]] = {:userId}
			LIMIT 2
		`).Bind(dbx.Params{
			"workspaceId": request.WorkspaceID,
			"userId":      memberID,
		}).All(&memberships)
		if err != nil {
			return err
		}
		if len(memberships) > 1 || (len(memberships) == 1 && memberships[0].Role != workspaceRoleMember) {
			return permissionDenied()
		}
		if len(memberships) == 1 {
			cursor, err := currentWorkspacePermissionCursor(txApp, request.WorkspaceID)
			if err != nil {
				return err
			}
			result.Membership = workspaceMembershipResponse{
				WorkspaceID: request.WorkspaceID, UserID: memberID, Role: workspaceRoleMember,
				PermissionCursor: cursor, Duplicate: true,
			}
			return nil
		}
		allowed, err := administratorAccountCanAuthenticate(txApp, memberID)
		if err != nil {
			return err
		}
		if !allowed {
			if request.ResolveByEmail {
				// Told apart from an address no account has by no one.
				return invite(txApp, authorization)
			}
			return errWorkspaceMemberTargetMissing
		}
		if !request.ResolveByEmail {
			policy, err := loadWorkspaceMemberAddPolicy(txApp)
			if err != nil {
				return err
			}
			if policy != administratorMemberAddPolicyEnabled {
				return errWorkspaceMemberAddDisabled
			}
		}
		now := time.Now().UTC().UnixMilli()
		metadata := map[string]any{"role": workspaceRoleMember}
		if request.Restricted {
			metadata["restricted"] = true
		}
		cursor, err := addWorkspaceMember(txApp, request.WorkspaceID, authorization.Organization.OrganizationID,
			request.ActorID, memberID, now, metadata)
		if err != nil {
			return err
		}
		if request.Restricted {
			if cursor, err = restrictJoinedWorkspaceMember(txApp, request.WorkspaceID, authorization.Organization.OrganizationID,
				memberID, request.ActorID, now); err != nil {
				return err
			}
		}
		// An account the actor brings in for itself (an app's, a bot's) isn't told.
		if !request.ActorOwnAccount {
			if err := queueWorkspaceMemberAddedEmail(txApp, request.WorkspaceID, request.ActorID, memberID,
				request.Message, time.UnixMilli(now).UTC()); err != nil {
				return err
			}
		}
		result.Membership = workspaceMembershipResponse{
			WorkspaceID: request.WorkspaceID, UserID: memberID, Role: workspaceRoleMember,
			PermissionCursor: cursor,
		}
		return nil
	})
	return result, err
}

// addWorkspaceMember makes someone not yet in a workspace its member, as
// the actor: in its organization too, unless an organization keeps them
// out. An owner's addition tells them by email (workspace_member_added_emails.go).
func addWorkspaceMember(app core.App, workspaceID, organizationID, actorID, memberID string, now int64,
	metadata map[string]any) (int, error) {
	// An organization may keep the people it manages to its own workspaces.
	if err := requireExternalWorkspaceJoin(app, memberID, organizationID); err != nil {
		return 0, err
	}
	if err := admitOrganizationWorkspaceMember(app, organizationID, actorID, memberID); err != nil {
		return 0, err
	}
	if err := joinWorkspaceAsMember(app, workspaceID, memberID, actorID, now); err != nil {
		return 0, err
	}
	cursor, err := advanceWorkspacePermissionCursor(app, workspaceID, now)
	if err != nil {
		return 0, err
	}
	err = recordOrganizationAuditEvent(app, organizationAuditEvent{
		OrganizationID: organizationID, WorkspaceID: workspaceID, ActorID: actorID,
		Action: "workspace_member.add", TargetType: "workspace_member", TargetID: memberID,
		Metadata: metadata, OccurredAt: now,
	})
	return cursor, err
}

func admitAdministratorWorkspaceOwnershipSuccessor(app core.App, organizationID, workspaceID, userID, actorID string) error {
	if err := admitOrganizationWorkspaceMember(app, organizationID, actorID, userID); errors.Is(err, errOrganizationMemberInactive) {
		return errWorkspaceOwnerTransferTargetInvalid
	} else if err != nil {
		return err
	}
	return joinWorkspaceAsMember(app, workspaceID, userID, actorID, time.Now().UTC().UnixMilli())
}

// admitOrganizationWorkspaceMember makes a person added to one of an
// organization's workspaces a member of the organization: someone new joins
// as a member, and a guest becomes one, as a Notion guest added to the
// workspace does; a member or administrator stays as they are. A
// deactivated membership isn't added to.
func admitOrganizationWorkspaceMember(app core.App, organizationID, actorID, userID string) error {
	memberships, err := app.FindCollectionByNameOrId(organizationMembershipsCollection)
	if err != nil {
		return err
	}
	membership, err := app.FindFirstRecordByFilter(
		memberships, "organization = {:organization} && user = {:user}",
		dbx.Params{"organization": organizationID, "user": userID},
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ensureOrganizationMembership(app, organizationID, userID, organizationRoleMember, organizationMemberStatusActive)
	}
	if err != nil {
		return err
	}
	if membership.GetString("status") != organizationMemberStatusActive {
		return errOrganizationMemberInactive
	}
	if membership.GetString("role") != organizationRoleGuest {
		return nil
	}
	membership.Set("role", organizationRoleMember)
	if err := app.Save(membership); err != nil {
		return err
	}
	return recordOrganizationAuditEvent(app, organizationAuditEvent{
		ID: core.GenerateDefaultRandomId(), OrganizationID: organizationID, ActorID: actorID,
		Action: "organization_member.guest_promote", TargetType: "user", TargetID: userID,
		OccurredAt: time.Now().UTC().UnixMilli(),
	})
}

// joinWorkspaceAsMember makes someone not yet in the workspace a member, as
// the actor's change to People — their row added, or for a guest People
// already lists, their Membership Type edited (people_automation_events.go).
func joinWorkspaceAsMember(app core.App, workspaceID, userID, actorID string, now int64) error {
	if err := insertWorkspaceMembership(app, workspaceID, userID, now); err != nil {
		return err
	}
	notePeopleChange(app, workspaceID, userID, actorID)
	return nil
}

func insertWorkspaceMembership(app core.App, workspaceID, userID string, now int64) error {
	_, err := app.DB().NewQuery(`INSERT INTO {{leafen_workspace_memberships}}
		([[workspace_id]],[[user_id]],[[role]],[[created]],[[updated]])
		VALUES ({:workspaceId},{:userId},'member',{:now},{:now})
		ON CONFLICT ([[workspace_id]],[[user_id]]) DO UPDATE SET [[role]]='member',[[updated]]=excluded.[[updated]]`).Bind(dbx.Params{
		"workspaceId": workspaceID, "userId": userID, "now": now,
	}).Execute()
	if err == nil {
		noteWorkspaceSetChanges(userID)
	}
	return err
}

// findWorkspaceMemberByEmail is the account that proved the address, as its
// own or besides it: one an account only claimed (a server without mail,
// account_email_proof.go) or hasn't confirmed yet is invited instead, as
// anyone's address would be (2026-11-06: an address added without proof
// joined the workspace its invitation was for).
func findWorkspaceMemberByEmail(app core.App, email string) (*core.Record, error) {
	account, err := pageShareEmailAccount(app, email)
	if err != nil {
		return nil, err
	}
	if account == "" {
		return nil, sql.ErrNoRows
	}
	return app.FindRecordById("users", account)
}

func loadWorkspaceMemberAddPolicy(app core.App) (string, error) {
	state, err := loadWorkspaceMemberAddPolicyState(app)
	return state.Policy, err
}

type workspaceMemberAddPolicyState struct {
	Policy   string `db:"member_add_policy"`
	Revision int    `db:"revision"`
}

func loadWorkspaceMemberAddPolicyState(app core.App) (workspaceMemberAddPolicyState, error) {
	state := workspaceMemberAddPolicyState{}
	err := app.DB().NewQuery(`
		SELECT [[member_add_policy]], [[revision]] FROM {{leafen_server_settings}}
		WHERE [[id]] = 'default' AND [[format_version]] = 3
	`).One(&state)
	if err != nil {
		return workspaceMemberAddPolicyState{}, err
	}
	if (state.Policy != administratorMemberAddPolicyEnabled &&
		state.Policy != administratorMemberAddPolicyDisabled) || state.Revision < 1 {
		return workspaceMemberAddPolicyState{}, errors.New(errorCodeWorkspaceMemberListFailed)
	}
	return state, nil
}
