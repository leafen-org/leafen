package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// An email invitation's link opened by an account that already holds its
// address is that account's to take, as Notion's invitation to an existing
// account lets it in (2026-11-11: an account made on a server without mail,
// its address unproven, was shared with by that address and met "사용 권한
// 없음"). The account is signed in — before opening the link, or through the
// sign-in the link sent it to — so whoever holds the account has the token
// the inviter sent or handed to that address: the token proves the address,
// as it does at sign-up (signup_invitations.go), and the account joins what
// the address was invited to. An account that doesn't hold the address hears
// the invitation isn't for it.

type invitationTakeInput struct {
	Token string `json:"token"`
}

type invitationTakeResponse struct {
	WorkspaceID string `json:"workspaceId"`
	// The page a page's invitation shares, which opens next.
	PageID string `json:"pageId,omitempty"`
}

func takeInvitationSignedIn(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	input := invitationTakeInput{}
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the invitation.")
	}
	response := invitationTakeResponse{}
	token := strings.TrimSpace(input.Token)
	// A guest invite link is anyone's to take (page_guest_link_take.go), under its pages' sharing locks.
	release := lockPageGuestLinkPages(event.App, token)
	defer release()
	err := event.App.RunInTransaction(func(txApp core.App) error {
		var err error
		if strings.HasPrefix(token, pageGuestLinkTokenPrefix) {
			response, err = takePageGuestLink(event.Request.Context(), txApp, user.Id, token, time.Now())
			return asSignupInvitationInvalid(err)
		}
		response, err = takeEmailInvitation(txApp, user, token, time.Now())
		return err
	})
	if err != nil {
		if errors.Is(err, errSignupInvitationInvalid) {
			return signupPolicyAPIError(event, err)
		}
		if held, ok := guestLinkHeldFailure(event, err); ok {
			return apiErrorWithCause(event, held.Status, held.Code, held.Message, diagnosticOperation(event), held.Stage, held.CauseCode, err)
		}
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeSignupPolicyUnavailable,
			"Couldn't take the invitation.", diagnosticOperation(event), "invitation-take", errorCodeSignupPolicyUnavailable, err)
	}
	if strings.HasPrefix(token, pageGuestLinkTokenPrefix) {
		recheckCollaborationAccessAfterCommit(event.App)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, response)
}

// takeEmailInvitation proves the invitation's address for the account that
// holds it and admits the account to what its proven addresses are invited to.
func takeEmailInvitation(app core.App, user *core.Record, token string, now time.Time) (invitationTakeResponse, error) {
	if !strings.HasPrefix(token, signupInvitationTokenPrefix) {
		return invitationTakeResponse{}, errSignupInvitationInvalid
	}
	stored := struct {
		Email    string `db:"email"`
		SourceID string `db:"source_id"`
	}{}
	err := app.DB().NewQuery(`SELECT [[email]], [[source_id]] FROM {{leafen_signup_invitations}} WHERE [[token]] = {:token}`).
		Bind(dbx.Params{"token": token}).One(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return invitationTakeResponse{}, errSignupInvitationInvalid
	}
	if err != nil {
		return invitationTakeResponse{}, err
	}
	admission, err := resolveSignupAdmission(app, token, stored.Email, now)
	if err != nil {
		return invitationTakeResponse{}, err
	}
	held := 0
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_account_emails}} WHERE [[user_id]] = {:userId} AND [[email]] = {:email}`).
		Bind(dbx.Params{"userId": user.Id, "email": stored.Email}).Row(&held); err != nil {
		return invitationTakeResponse{}, err
	}
	if held == 0 {
		return invitationTakeResponse{}, errSignupInvitationInvalid
	}
	if _, err := app.DB().NewQuery(`DELETE FROM {{leafen_unproven_account_emails}} WHERE [[email]] = {:email} AND [[user_id]] = {:userId}`).
		Bind(dbx.Params{"email": stored.Email, "userId": user.Id}).Execute(); err != nil {
		return invitationTakeResponse{}, err
	}
	if stored.Email == normalizePageShareEmail(user.Email()) && !user.Verified() {
		user.SetVerified(true)
		if err := app.Save(user); err != nil {
			return invitationTakeResponse{}, err
		}
	}
	if err := admitVerifiedEmailInvitations(app, user); err != nil {
		return invitationTakeResponse{}, err
	}
	response := invitationTakeResponse{WorkspaceID: admission.Join.WorkspaceID}
	if admission.Join.Kind == accountJoinPageEmail {
		err := app.DB().NewQuery(`SELECT [[page_id]] FROM {{leafen_page_permissions}} WHERE [[id]] = {:id}`).
			Bind(dbx.Params{"id": stored.SourceID}).Row(&response.PageID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return invitationTakeResponse{}, err
		}
	}
	return response, nil
}
