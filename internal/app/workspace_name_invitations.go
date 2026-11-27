package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// A workspace's owner invites someone by name (the multi-server decision of
// 2026-10-20: "members join by an invite link, used once, for a short time,
// mailed or copied by the administrator"): the invitation is one link for one
// person, used once, for seven days. Opening it asks only a name, filled with
// the one the owner wrote, and the account it makes is a member of the
// workspace at once (auth_invitation_join.go). No email is asked or sent.

const (
	nameInvitationTokenPrefix = "nin_"
	nameInvitationLifetime    = 7 * 24 * time.Hour
)

var errNameInvitationInvalid = errors.New("the name invitation is not one that waits")

// workspaceNameInvitation is one waiting invitation by name, its link's key
// for the owner to copy.
type workspaceNameInvitation struct {
	ID        string `json:"id" db:"id"`
	Name      string `json:"name" db:"name"`
	Token     string `json:"token" db:"token"`
	ExpiresAt int64  `json:"expiresAt" db:"expires_at"`
	CreatedAt int64  `json:"createdAt" db:"created_at"`
}

type workspaceNameInvitationInput struct {
	Name string `json:"name"`
}

func registerWorkspaceNameInvitationRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+"/workspaces/{workspaceId}/name-invitations", listWorkspaceNameInvitations)
	event.Router.POST(apiRoot+"/workspaces/{workspaceId}/name-invitations", createWorkspaceNameInvitation)
	event.Router.DELETE(apiRoot+"/workspaces/{workspaceId}/name-invitations/{invitationId}", cancelWorkspaceNameInvitation)
}

func nameInvitationOwner(event *core.RequestEvent) (*core.Record, string, bool) {
	user, ok := authenticatedUser(event)
	if !ok {
		_ = apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
		return nil, "", false
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	if workspaceID == "" || len(workspaceID) > maxWorkspacePermissionIdentifierLength {
		_ = apiError(event, http.StatusBadRequest, errorCodeWorkspaceMemberAddInvalid, "Check the invitation.")
		return nil, "", false
	}
	if _, err := authorizeWorkspace(event.App, user.Id, workspaceID, permissionWorkspaceOwner); err != nil {
		_ = nameInvitationError(event, err)
		return nil, "", false
	}
	return user, workspaceID, true
}

func loadWorkspaceNameInvitations(app core.App, workspaceID string) ([]workspaceNameInvitation, error) {
	invitations := []workspaceNameInvitation{}
	err := app.DB().NewQuery(`SELECT [[id]], [[name]], [[token]], [[expires_at]], [[created_at]]
		FROM {{leafen_workspace_name_invitations}}
		WHERE [[workspace_id]] = {:workspace} AND [[status]] = 'pending' AND [[expires_at]] > {:now}
		ORDER BY [[created_at]], [[id]] LIMIT {:limit}`).
		Bind(dbx.Params{"workspace": workspaceID, "now": time.Now().UnixMilli(), "limit": maxWorkspaceMemberInvitations}).
		All(&invitations)
	return invitations, err
}

func listWorkspaceNameInvitations(event *core.RequestEvent) error {
	_, workspaceID, ok := nameInvitationOwner(event)
	if !ok {
		return nil
	}
	invitations, err := loadWorkspaceNameInvitations(event.App, workspaceID)
	if err != nil {
		return nameInvitationError(event, err)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, map[string]any{"invitations": invitations})
}

func createWorkspaceNameInvitation(event *core.RequestEvent) error {
	user, workspaceID, ok := nameInvitationOwner(event)
	if !ok {
		return nil
	}
	input := workspaceNameInvitationInput{}
	_ = event.BindBody(&input)
	name := strings.TrimSpace(input.Name)
	if name == "" || utf8.RuneCountInString(name) > maxAuthNameLength {
		return apiError(event, http.StatusBadRequest, errorCodeWorkspaceMemberAddInvalid, "Give the name of the person invited.")
	}
	now := time.Now().UTC()
	invitation := workspaceNameInvitation{
		ID: core.GenerateDefaultRandomId(), Name: name, Token: nameInvitationTokenPrefix + security.RandomString(32),
		ExpiresAt: now.Add(nameInvitationLifetime).UnixMilli(), CreatedAt: now.UnixMilli(),
	}
	err := event.App.RunInTransaction(func(txApp core.App) error {
		authorization, err := authorizeWorkspace(txApp, user.Id, workspaceID, permissionWorkspaceOwner)
		if err != nil {
			return err
		}
		if policy, err := loadWorkspaceMemberAddPolicy(txApp); err != nil || policy != administratorMemberAddPolicyEnabled {
			if err == nil {
				err = errWorkspaceMemberAddDisabled
			}
			return err
		}
		pending := 0
		if err := txApp.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_workspace_name_invitations}}
			WHERE [[workspace_id]] = {:workspace} AND [[status]] = 'pending' AND [[expires_at]] > {:now}`).
			Bind(dbx.Params{"workspace": workspaceID, "now": now.UnixMilli()}).Row(&pending); err != nil {
			return err
		}
		if pending >= maxWorkspaceMemberInvitations {
			return errWorkspaceMemberInvitationsFull
		}
		if _, err := txApp.DB().Insert("leafen_workspace_name_invitations", dbx.Params{
			"id": invitation.ID, "workspace_id": workspaceID, "name": name, "token": invitation.Token,
			"inviter_id": user.Id, "status": "pending", "expires_at": invitation.ExpiresAt,
			"created_at": invitation.CreatedAt, "updated_at": invitation.CreatedAt,
		}).Execute(); err != nil {
			return err
		}
		return recordOrganizationAuditEvent(txApp, organizationAuditEvent{
			OrganizationID: authorization.Organization.OrganizationID, WorkspaceID: workspaceID, ActorID: user.Id,
			Action: "workspace_member.invite", TargetType: "workspace_name_invitation", TargetID: invitation.ID,
			Metadata: map[string]any{"role": workspaceRoleMember, "name": name}, OccurredAt: invitation.CreatedAt,
		})
	})
	if err != nil {
		return nameInvitationError(event, err)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusCreated, invitation)
}

// cancelWorkspaceNameInvitation cancels a waiting invitation; one cancelled
// already stays so. An ID no invitation of the workspace has is refused as
// not found, and a used one as used, never answered as cancelled
// (2026-11-26: an unknown ID answered 204, so a mistyped one left the
// invitation meant to end still open).
func cancelWorkspaceNameInvitation(event *core.RequestEvent) error {
	_, workspaceID, ok := nameInvitationOwner(event)
	if !ok {
		return nil
	}
	id := event.Request.PathValue("invitationId")
	result, err := event.App.DB().NewQuery(`UPDATE {{leafen_workspace_name_invitations}}
		SET [[status]] = 'cancelled', [[updated_at]] = {:now}
		WHERE [[id]] = {:id} AND [[workspace_id]] = {:workspace} AND [[status]] = 'pending'`).
		Bind(dbx.Params{"id": id, "workspace": workspaceID, "now": time.Now().UnixMilli()}).Execute()
	if err != nil {
		return nameInvitationError(event, err)
	}
	if cancelled, _ := result.RowsAffected(); cancelled == 0 {
		status := ""
		err := event.App.DB().NewQuery(`SELECT [[status]] FROM {{leafen_workspace_name_invitations}}
			WHERE [[id]] = {:id} AND [[workspace_id]] = {:workspace}`).Bind(dbx.Params{"id": id, "workspace": workspaceID}).Row(&status)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return apiError(event, http.StatusNotFound, errorCodeInvitationNotFound, "Check invitationId: no invitation by name of this "+
				"workspace has that ID. GET /workspaces/{workspaceId}/name-invitations lists those waiting.")
		case err != nil:
			return nameInvitationError(event, err)
		case status == "joined":
			return apiError(event, http.StatusConflict, errorCodeWorkspaceMemberAddInvalid, "Check invitationId: that invitation "+
				"was used, and the person it named joined the workspace. DELETE /workspaces/{workspaceId}/members/{userId} removes a member.")
		}
	}
	return event.NoContent(http.StatusNoContent)
}

// waitingNameInvitation is the invitation a name invitation's key opens:
// waiting, unexpired, to a workspace still there.
type waitingNameInvitation struct {
	ID             string `db:"id"`
	Name           string `db:"name"`
	WorkspaceID    string `db:"workspace_id"`
	OrganizationID string `db:"organization"`
	InviterID      string `db:"inviter_id"`
}

func loadWaitingNameInvitation(app core.App, token string, now time.Time) (waitingNameInvitation, error) {
	invitation := waitingNameInvitation{}
	err := app.DB().NewQuery(`SELECT [[invitation]].[[id]], [[invitation]].[[name]], [[invitation]].[[workspace_id]],
			[[workspace]].[[organization]], [[invitation]].[[inviter_id]]
		FROM {{leafen_workspace_name_invitations}} [[invitation]]
		JOIN {{leafen_workspaces}} [[workspace]] ON [[workspace]].[[id]] = [[invitation]].[[workspace_id]]
		WHERE [[invitation]].[[token]] = {:token} AND [[invitation]].[[status]] = 'pending'
			AND [[invitation]].[[expires_at]] > {:now} AND [[workspace]].[[deletion_job_id]] = ''`).
		Bind(dbx.Params{"token": token, "now": now.UnixMilli()}).One(&invitation)
	if errors.Is(err, sql.ErrNoRows) {
		return invitation, errNameInvitationInvalid
	}
	return invitation, err
}

// acceptNameInvitation spends the invitation on the account just made and
// makes it a member of the workspace.
func acceptNameInvitation(txApp core.App, token string, user *core.Record, now time.Time) error {
	invitation, err := loadWaitingNameInvitation(txApp, token, now)
	if err != nil {
		return err
	}
	result, err := txApp.DB().NewQuery(`UPDATE {{leafen_workspace_name_invitations}}
		SET [[status]] = 'joined', [[joined_user_id]] = {:user}, [[updated_at]] = {:now}
		WHERE [[id]] = {:id} AND [[status]] = 'pending'`).
		Bind(dbx.Params{"id": invitation.ID, "user": user.Id, "now": now.UnixMilli()}).Execute()
	if err != nil {
		return err
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		return refusedUnlessStorage(errNameInvitationInvalid, err)
	}
	_, err = addWorkspaceMember(txApp, invitation.WorkspaceID, invitation.OrganizationID, invitation.InviterID,
		user.Id, now.UnixMilli(), map[string]any{"role": workspaceRoleMember, "invitation": invitation.ID})
	return err
}

func nameInvitationError(event *core.RequestEvent, err error) error {
	switch {
	case isPermissionDenied(err) || errors.Is(err, sql.ErrNoRows):
		return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
	case errors.Is(err, errWorkspaceMemberAddDisabled):
		return apiError(event, http.StatusForbidden, errorCodeWorkspaceMemberAddDisabled, "Server policy blocked adding workspace members.")
	case errors.Is(err, errWorkspaceMemberInvitationsFull):
		return apiError(event, http.StatusConflict, errorCodeWorkspaceMemberInvitationsFull,
			"This workspace has too many invitations waiting. Cancel some first.")
	}
	return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeWorkspaceMemberAddFailed,
		"Couldn't process the invitation.", diagnosticOperation(event), "name-invitation", errorCodeWorkspaceMemberAddFailed, err)
}
