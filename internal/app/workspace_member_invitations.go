package app

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Someone added to a workspace by an address no account here has is
// invited, as Notion invites them ("They'll receive an email with a link to
// sign in and join your workspace", Add members help): the invitation waits
// in the workspace's people, shown as pending, until an account verifies
// the address — it then joins as a member, or a restricted member where the
// invitation asked (workspace_invitation_restricted.go), under the server's
// and the organizations' policies as they are then — or until the owner
// cancels it.
// When the server sends email the address hears of it, within the limits
// page invitations keep.

var (
	errWorkspaceMemberInvitationsFull   = errors.New(errorCodeWorkspaceMemberInvitationsFull)
	errWorkspaceMemberInvitationMissing = errors.New("workspace member invitation is missing")
)

// inviteWorkspaceMember leaves an invitation for an address no account can
// join by yet, or finds the one already waiting, and says whether the
// address is mailed: the new invitation's email, or the waiting one's that
// went or goes.
func inviteWorkspaceMember(ctx context.Context, app core.App, workspaceID, organizationID, inviterID, email string, now time.Time) (bool, error) {
	var waiting, pending int
	var waitingMail string
	if err := app.DB().NewQuery(`
		SELECT
			(SELECT COUNT(*) FROM {{leafen_workspace_invitations}}
				WHERE [[workspace_id]] = {:workspace} AND [[status]] = 'pending' AND [[email]] = {:email}),
			(SELECT COUNT(*) FROM {{leafen_workspace_invitations}}
				WHERE [[workspace_id]] = {:workspace} AND [[status]] = 'pending'),
			COALESCE((SELECT [[mail_status]] FROM {{leafen_workspace_invitations}}
				WHERE [[workspace_id]] = {:workspace} AND [[status]] = 'pending' AND [[email]] = {:email}), '')
	`).Bind(dbx.Params{"workspace": workspaceID, "email": email}).Row(&waiting, &pending, &waitingMail); err != nil {
		return false, err
	}
	if waiting > 0 {
		return waitingMail == "pending" || waitingMail == "sent", nil
	}
	if pending >= maxWorkspaceMemberInvitations {
		return false, errWorkspaceMemberInvitationsFull
	}
	language, err := serverTextLanguageIn(ctx, app, inviterID)
	if err != nil {
		return false, err
	}
	mailStatus, mailReason, err := inviteEmailStatus(app, inviterID, email, now)
	if err != nil {
		return false, err
	}
	id := core.GenerateDefaultRandomId()
	if _, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_workspace_invitations}}
			([[id]], [[workspace_id]], [[email]], [[inviter_id]], [[status]], [[language]], [[mail_status]],
			 [[mail_error]], [[mail_next_at]], [[created_at]], [[updated_at]])
		VALUES ({:id}, {:workspace}, {:email}, {:inviter}, 'pending', {:language}, {:mail}, {:reason}, {:now}, {:now}, {:now})
	`).Bind(dbx.Params{
		"id": id, "workspace": workspaceID, "email": email, "inviter": inviterID, "language": language,
		"mail": mailStatus, "reason": mailReason, "now": now.UnixMilli(),
	}).Execute(); err != nil {
		return false, err
	}
	if _, err := ensureSignupInvitation(app, signupInvitationWorkspace, id, email, now); err != nil {
		return false, err
	}
	if err := recordOrganizationAuditEvent(app, organizationAuditEvent{
		OrganizationID: organizationID, WorkspaceID: workspaceID, ActorID: inviterID,
		Action: "workspace_member.invite", TargetType: "workspace_invitation", TargetID: id,
		Metadata: map[string]any{"role": workspaceRoleMember, "email": email}, OccurredAt: now.UnixMilli(),
	}); err != nil {
		return false, err
	}
	if mailStatus == "pending" {
		notifyNotificationEmailRunnerAfterCommit(app)
	}
	return mailStatus == "pending", nil
}

// loadWorkspaceMemberInvitations lists a workspace's waiting invitations,
// oldest first.
func loadWorkspaceMemberInvitations(app core.App, workspaceID string) ([]workspaceMemberInvitation, error) {
	rows := []struct {
		ID          string `db:"id"`
		Email       string `db:"email"`
		CreatedAt   int64  `db:"created_at"`
		SignupToken string `db:"signup_token"`
		MailStatus  string `db:"mail_status"`
		MailError   string `db:"mail_error"`
		Restricted  bool   `db:"restricted"`
	}{}
	// An owner sees each invitation's sign-up token to copy its link where
	// the server sends no mail (signup_invitations.go).
	if err := app.DB().NewQuery(`
		SELECT [[invitation]].[[id]], [[invitation]].[[email]], [[invitation]].[[created_at]],
			COALESCE([[signup]].[[token]], '') AS [[signup_token]],
			[[invitation]].[[mail_status]], COALESCE([[invitation]].[[mail_error]], '') AS [[mail_error]],
			[[invitation]].[[restricted]]
		FROM {{leafen_workspace_invitations}} [[invitation]]
		LEFT JOIN {{leafen_signup_invitations}} [[signup]]
			ON [[signup]].[[kind]] = 'workspace' AND [[signup]].[[source_id]] = [[invitation]].[[id]]
		WHERE [[invitation]].[[workspace_id]] = {:workspace} AND [[invitation]].[[status]] = 'pending'
		ORDER BY [[invitation]].[[created_at]], [[invitation]].[[id]] LIMIT {:limit}
	`).Bind(dbx.Params{"workspace": workspaceID, "limit": maxWorkspaceMemberInvitations}).All(&rows); err != nil {
		return nil, err
	}
	invitations := make([]workspaceMemberInvitation, 0, len(rows))
	for _, row := range rows {
		// The mail's last try failed, or every try did, or the limits held
		// it back: the owner is told, rather than left thinking it went
		// (2026-11-06: a mail server that couldn't be reached showed only
		// "초대 대기 중").
		failed := row.MailStatus == "failed" || (row.MailStatus == "pending" && row.MailError != "") ||
			row.MailError == inviteEmailRateLimited
		invitations = append(invitations, workspaceMemberInvitation{
			ID: row.ID, WorkspaceID: workspaceID, Email: row.Email, CreatedAt: row.CreatedAt, SignupToken: row.SignupToken,
			MailFailed: failed, Restricted: row.Restricted,
		})
	}
	return invitations, nil
}

func cancelWorkspaceMemberInvitation(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	invitationID := strings.TrimSpace(event.Request.PathValue("invitationId"))
	if workspaceID == "" || invitationID == "" || len(workspaceID) > maxWorkspacePermissionIdentifierLength ||
		len(invitationID) > maxWorkspacePermissionIdentifierLength {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the invitation.")
	}
	change := func(txApp core.App) error {
		authorization, err := authorizeWorkspace(txApp, user.Id, workspaceID, permissionWorkspaceOwner)
		if err != nil {
			return err
		}
		now := time.Now().UTC().UnixMilli()
		result, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_workspace_invitations}} SET [[status]] = 'cancelled', [[updated_at]] = {:now}
			WHERE [[id]] = {:id} AND [[workspace_id]] = {:workspace} AND [[status]] = 'pending'
		`).Bind(dbx.Params{"id": invitationID, "workspace": workspaceID, "now": now}).Execute()
		if err != nil {
			return err
		}
		if changed, err := result.RowsAffected(); err != nil {
			return err
		} else if changed == 0 {
			return errWorkspaceMemberInvitationMissing
		}
		return recordOrganizationAuditEvent(txApp, organizationAuditEvent{
			OrganizationID: authorization.Organization.OrganizationID, WorkspaceID: workspaceID, ActorID: user.Id,
			Action: "workspace_member.invite_cancel", TargetType: "workspace_invitation", TargetID: invitationID,
			OccurredAt: now,
		})
	}
	// A key's call asks its person only for a cancellation that would be made
	// (user_key_acting_ask.go).
	err := rehearseBeforeAsking(event, change)
	if err == nil {
		if asked, askErr := askBeforeActing(event); asked {
			return askErr
		}
		err = event.App.RunInTransaction(change)
	}
	invitationNotFound := func() error {
		return apiError(event, http.StatusNotFound, errorCodeInvitationNotFound, "Workspace invitation was not found.")
	}
	switch {
	case isPermissionDenied(err):
		return workspaceOwnersOnly(event, user.Id, workspaceID, "Only the workspace's owners cancel its invitations.", invitationNotFound)
	case errors.Is(err, errWorkspaceMemberInvitationMissing):
		return invitationNotFound()
	case err != nil:
		if storageError, ok := persistentWriteStorageAPIError(
			event, err, persistentWriteWorkspaceMemberAdd, "transaction",
		); ok {
			return storageError
		}
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeWorkspaceMemberMutationFailed, "Couldn't cancel the invitation.", err)
	}
	return event.JSON(http.StatusOK, workspaceMemberInvitationCancelResponse{
		WorkspaceID: workspaceID, InvitationID: invitationID, Cancelled: true,
	})
}

// admitVerifiedEmailInvitations brings an account that has verified an
// address into what the address was invited to: workspaces as a member,
// then organizations sharing pages with it as a guest.
func admitVerifiedEmailInvitations(app core.App, user *core.Record) error {
	if err := admitPendingWorkspaceInvitations(app, user); err != nil {
		return err
	}
	if err := admitPendingEmailShareGuests(app, user); err != nil {
		return err
	}
	return notifyEmailSharesOnAdmission(app, user)
}

type pendingWorkspaceInvitation struct {
	ID             string `db:"id"`
	WorkspaceID    string `db:"workspace_id"`
	OrganizationID string `db:"organization"`
	OwnerID        string `db:"workspace_owner"`
	InviterID      string `db:"inviter_id"`
	Restricted     bool   `db:"restricted"`
	CreatedAt      int64  `db:"created_at"`
}

// admitPendingWorkspaceInvitations joins an account to the workspaces its
// verified addresses are invited to. An invitation the server's or an
// organization's policy keeps out for now stays waiting.
func admitPendingWorkspaceInvitations(app core.App, user *core.Record) error {
	if user == nil {
		return nil
	}
	// Every pending invitation is looked at, a window at a time: one a policy
	// keeps waiting doesn't hold back those after it (2026-11-23).
	invitations, err := readPendingWorkspaceInvitations(app, user.Id, pendingWorkspaceInvitation{})
	if err != nil {
		return err
	}
	if len(invitations) == 0 {
		return nil
	}
	if policy, err := loadWorkspaceMemberAddPolicy(app); err != nil || policy != administratorMemberAddPolicyEnabled {
		return err
	}
	if allowed, err := administratorAccountCanAuthenticate(app, user.Id); err != nil || !allowed {
		return err
	}
	for {
		for _, invitation := range invitations {
			err := app.RunInTransaction(func(txApp core.App) error {
				var role string
				err := txApp.DB().NewQuery(`SELECT [[role]] FROM {{leafen_workspace_memberships}}
					WHERE [[workspace_id]] = {:workspace} AND [[user_id]] = {:user}`).Bind(dbx.Params{
					"workspace": invitation.WorkspaceID, "user": user.Id,
				}).Row(&role)
				if err != nil && !errors.Is(err, sql.ErrNoRows) {
					return err
				}
				now := time.Now().UTC().UnixMilli()
				if role == "" && invitation.OwnerID != user.Id {
					// A restricted invitation brings a restricted member where the module still gives the role.
					restricted := invitation.Restricted && organizationPolicy.RestrictedInvitations
					metadata := map[string]any{"role": workspaceRoleMember, "invitation": invitation.ID}
					if restricted {
						metadata["restricted"] = true
					}
					if _, err := addWorkspaceMember(txApp, invitation.WorkspaceID, invitation.OrganizationID,
						invitation.InviterID, user.Id, now, metadata); err != nil {
						return err
					}
					if restricted {
						if _, err := restrictJoinedWorkspaceMember(txApp, invitation.WorkspaceID, invitation.OrganizationID,
							user.Id, invitation.InviterID, now); err != nil {
							return err
						}
					}
				}
				_, err = txApp.DB().NewQuery(`UPDATE {{leafen_workspace_invitations}} SET [[status]] = 'joined', [[updated_at]] = {:now}
					WHERE [[id]] = {:id} AND [[status]] = 'pending'`).Bind(dbx.Params{"id": invitation.ID, "now": now}).Execute()
				return err
			})
			if errors.Is(err, errExternalWorkspacesDisabled) || errors.Is(err, errOrganizationMemberInactive) {
				continue
			}
			if err != nil {
				return err
			}
		}
		if len(invitations) < emailAdmissionWindow {
			return nil
		}
		if invitations, err = readPendingWorkspaceInvitations(app, user.Id, invitations[len(invitations)-1]); err != nil {
			return err
		}
	}
}

// readPendingWorkspaceInvitations is a window of the invitations an
// account's verified addresses are waiting on, oldest first, after the one
// given.
func readPendingWorkspaceInvitations(app core.App, userID string, after pendingWorkspaceInvitation) ([]pendingWorkspaceInvitation, error) {
	invitations := []pendingWorkspaceInvitation{}
	err := app.DB().NewQuery(`
		SELECT [[invitation]].[[id]], [[invitation]].[[workspace_id]], [[workspace]].[[organization]],
			[[workspace]].[[workspace_owner]], [[invitation]].[[inviter_id]],
			[[invitation]].[[restricted]], [[invitation]].[[created_at]]
		FROM {{leafen_workspace_invitations}} [[invitation]]
		JOIN {{leafen_workspaces}} [[workspace]] ON [[workspace]].[[id]] = [[invitation]].[[workspace_id]]
		WHERE [[invitation]].[[status]] = 'pending' AND [[invitation]].[[email]] IN (` + verifiedAccountEmailSQL("userId") + `)
			AND [[workspace]].[[deletion_job_id]] = ''
			AND ({:afterId} = '' OR [[invitation]].[[created_at]] > {:afterAt}
				OR ([[invitation]].[[created_at]] = {:afterAt} AND [[invitation]].[[id]] > {:afterId}))
		ORDER BY [[invitation]].[[created_at]], [[invitation]].[[id]] LIMIT {:limit}
	`).Bind(dbx.Params{"userId": userID, "afterAt": after.CreatedAt, "afterId": after.ID, "limit": emailAdmissionWindow}).All(&invitations)
	return invitations, err
}
