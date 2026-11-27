package app

import (
	"context"
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

// An invitation lets its person in with a name (the multi-server decision of
// 2026-10-20: members are as the first administrator is): whoever opens an
// email invitation's link, or a workspace's or teamspace's invite link,
// gives the name others see and is in on that device, with no email or
// password asked. An email invitation's address, which its token proves,
// becomes the account's and joins what it was invited to; a workspace invite
// link's account has no email and joins the workspace at once. The
// device is the account's first, signed in until signed out; a password or
// another address comes later, as a backup. Joins are limited by address,
// as an invite link can be passed around.

const (
	invitationJoinsByAddress      = 10
	invitationJoinSpan            = time.Hour
	invitationJoinLimiterStoreKey = "leafen.invitationJoinLimiter"
)

var errInvitationJoinLimited = errors.New(errorCodeInvitationJoinLimited)

func registerInvitationJoinRoutes(event *core.ServeEvent) {
	event.Router.POST(apiRoot+"/auth/invitation/join", joinWithInvitation)
	event.Router.POST(apiRoot+"/auth/invitation/take", takeInvitationSignedIn)
}

func joinWithInvitation(event *core.RequestEvent) error {
	input := authInvitationJoinInput{}
	_ = event.BindBody(&input)
	name := strings.TrimSpace(input.Name)
	token := strings.TrimSpace(input.Invitation)
	if name == "" || utf8.RuneCountInString(name) > maxAuthNameLength || token == "" {
		return recordRejectedAuthAttempt(
			event, "", "", authAuditMethodInvitationJoin, authAuditPhaseVerify,
			http.StatusBadRequest, errorCodeInvalidInput, "Give the name others will see.",
		)
	}
	limiter := event.App.Store().GetOrSet(invitationJoinLimiterStoreKey, func() any {
		return newRequestWindowLimiter(invitationJoinsByAddress, invitationJoinSpan)
	}).(*requestWindowLimiter)
	if admitted, _ := limiter.admit("address:"+requestLimitAddress(event.RealIP()), time.Now()); !admitted {
		event.Response.Header().Set("Retry-After", "3600")
		return apiError(event, http.StatusTooManyRequests, errorCodeInvitationJoinLimited,
			"Too many accounts were made from here with invitations. Wait an hour and try again.")
	}

	// An email invitation names its address; an invite link names none.
	email := ""
	if strings.HasPrefix(token, signupInvitationTokenPrefix) {
		err := event.App.DB().NewQuery(`SELECT [[email]] FROM {{leafen_signup_invitations}} WHERE [[token]] = {:token}`).
			Bind(dbx.Params{"token": token}).Row(&email)
		if errors.Is(err, sql.ErrNoRows) {
			err = errSignupInvitationInvalid
		}
		if err != nil {
			return invitationJoinFailure(event, "", err)
		}
	}
	metadata := browserSessionMetadataFromEvent(event)
	// A guest invite link shares its pages as it is taken, under their sharing locks.
	release := lockPageGuestLinkPages(event.App, token)
	defer release()
	var session sessionResponse
	// No one knows its password: the person signs in by their devices until
	// they choose one. Hashed before the transaction (password_outside_transaction.go).
	unknown := preparePassword(event.App, "users", security.RandomString(48))
	err := event.App.RunInTransaction(func(txApp core.App) error {
		admission, err := resolveSignupAdmission(txApp, token, email, time.Now())
		if err != nil {
			return err
		}
		if !admission.Invited {
			return errSignupInvitationInvalid
		}
		if _, err := authorizeSelfServiceAccountCreation(txApp, admission); err != nil {
			return err
		}
		if email != "" {
			if _, err := findAccountByEmail(txApp, email); err == nil {
				return errRegistrationAccountExists
			} else if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}
		users, err := txApp.FindCollectionByNameOrId("users")
		if err != nil {
			return err
		}
		record := core.NewRecord(users)
		record.SetEmail(email)
		unknown.set(record)
		record.Set("name", name)
		// The token proves its address; an account with none has none to prove.
		record.SetVerified(true)
		if err := txApp.Save(record); err != nil {
			return err
		}
		if err := markAccountPasswordUnknown(txApp, record.Id); err != nil {
			return err
		}
		if err := recordAccountJoin(txApp, record.Id, admission.Join, time.Now()); err != nil {
			return err
		}
		if email != "" {
			if err := admitVerifiedEmailInvitations(txApp, record); err != nil {
				return err
			}
		}
		if err := takeJoinInvitation(event.Request.Context(), txApp, record, token, time.Now()); err != nil {
			return err
		}
		session, err = issueAuthenticatedBrowserSessionInTransaction(
			txApp, record, metadata, false, authSessionMethodDevice, "", "",
		)
		if err != nil {
			return err
		}
		return recordAuthAttemptInTransactionSuccessWithMetadata(
			txApp, record.Id, email, authAuditMethodInvitationJoin, authAuditPhaseVerify, metadata,
		)
	})
	if err != nil {
		return invitationJoinFailure(event, email, err)
	}
	recheckCollaborationAccessAfterCommit(event.App)
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusCreated, session)
}

// takeJoinInvitation gives a new account what the invitation that let it in
// gives, in the account's own transaction, by whichever route made it — the
// name step here or the password sign-up (auth.go; 2026-11-23: a guest
// invite link made for one person, given to the password sign-up, made one
// account after another and stayed unspent). An invitation by name is spent
// and makes the account a member; a workspace's invite link makes it a
// member at once, as it does for someone signed in (workspace_invite_links.go),
// rather than leaving them to ask from a workspace of their own; a guest
// invite link makes it a guest of the link's pages, spent if made for one
// person. An email invitation joins what its address was invited to
// (admitVerifiedEmailInvitations), and a teamspace's link is taken once
// signed in, through the teamspace's own change.
func takeJoinInvitation(ctx context.Context, txApp core.App, record *core.Record, token string, now time.Time) error {
	switch {
	case token == "" || strings.HasPrefix(token, signupInvitationTokenPrefix) || strings.HasPrefix(token, teamspaceInviteTokenPrefix):
		return nil
	case strings.HasPrefix(token, nameInvitationTokenPrefix):
		// Used or withdrawn a moment ago: said as it is now (invitation_ended.go).
		if err := acceptNameInvitation(txApp, token, record, now.UTC()); errors.Is(err, errNameInvitationInvalid) {
			return nameInvitationEndedFor(txApp, token, now)
		} else if err != nil {
			return err
		}
		return nil
	case strings.HasPrefix(token, pageGuestLinkTokenPrefix):
		_, err := takePageGuestLink(ctx, txApp, record.Id, token, now)
		return asSignupInvitationInvalid(err)
	}
	if _, err := workspaceInvite(txApp, record.Id, token, true); errors.Is(err, errWorkspaceInviteLinkInvalid) {
		return inviteLinkEndedFor(txApp, token)
	} else if err != nil {
		return err
	}
	return nil
}

func invitationJoinFailure(event *core.RequestEvent, email string, err error) error {
	if errors.Is(err, errRegistrationAccountExists) {
		return recordRejectedAuthAttempt(
			event, "", email, authAuditMethodInvitationJoin, authAuditPhaseVerify,
			http.StatusConflict, errorCodeAccountExists, "This invitation's email already has an account here: log in to accept it.",
		)
	}
	// A guest invite link whose page is held says so (page_guest_link_held.go).
	failure, refused := guestLinkHeldFailure(event, err)
	if !refused && (errors.Is(err, errSignupRestricted) || errors.Is(err, errSignupPolicyUnavailable) || errors.Is(err, errSignupInvitationInvalid)) {
		failure, refused = signupPolicyFailure(event, err), true
	}
	if refused {
		return recordRejectedAuthAttemptWithCause(
			event, "", email, authAuditMethodInvitationJoin, authAuditPhaseVerify,
			failure.Status, failure.Code, failure.Message, diagnosticOperation(event),
			failure.Stage, failure.CauseCode, err,
		)
	}
	return recordRejectedAuthAttemptWithCause(
		event, "", email, authAuditMethodInvitationJoin, authAuditPhaseVerify,
		http.StatusInternalServerError, errorCodeSignupPolicyUnavailable, "Couldn't make the account.",
		diagnosticOperation(event), "invitation_join", errorCodeSignupPolicyUnavailable, err,
	)
}
