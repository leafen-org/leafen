package app

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const accountSecondaryEmailLimit = 5

var (
	errAccountEmailLimit     = errors.New(errorCodeAccountEmailLimit)
	errAccountEmailManaged   = errors.New(errorCodeAccountEmailManaged)
	errAccountEmailPrimary   = errors.New(errorCodeAccountEmailPrimary)
	errAccountEmailChallenge = errors.New(errorCodeAccountEmailChallengeInvalid)
	// A change of the account's emails sent on settings read before they
	// changed, one naming an email the account doesn't have (taken away
	// elsewhere since), and one naming an email it hasn't verified: each is
	// still the challenge's refusal to callers that ask only that, and each
	// tells the person its own reason (accountEmailAPIError).
	errAccountEmailRevisionChanged = fmt.Errorf("%w: the email settings' revision moved on", errAccountEmailChallenge)
	errAccountEmailMissing         = fmt.Errorf("%w: not an email of the account", errAccountEmailChallenge)
	errAccountEmailNotVerified     = fmt.Errorf("%w: not a verified email of the account", errAccountEmailChallenge)
)

// findAccountByEmail includes an unverified primary for signup/verification,
// but secondary addresses are claimed only after verification.
func findAccountByEmail(app core.App, email string) (*core.Record, error) {
	normalized, valid := normalizeEmailIdentity(email)
	if !valid {
		return nil, sql.ErrNoRows
	}
	var userID string
	if err := app.DB().NewQuery(`SELECT [[user_id]] FROM {{leafen_account_emails}} WHERE [[email]] = {:email}`).
		Bind(dbx.Params{"email": normalized}).Row(&userID); err != nil {
		return nil, err
	}
	return app.FindRecordById("users", userID)
}

func verifiedAccountEmails(app core.App, userID string) ([]string, error) {
	addresses := []string{}
	err := app.DB().NewQuery(`SELECT [[email]] FROM {{leafen_account_emails}} WHERE [[user_id]] = {:userId}
		AND [[verified]] = 1 AND [[email]] NOT IN (SELECT [[email]] FROM {{leafen_unproven_account_emails}})
		ORDER BY [[email]] LIMIT 7`).Bind(dbx.Params{"userId": userID}).Column(&addresses)
	if err == nil && len(addresses) > accountSecondaryEmailLimit+1 {
		err = errAccountEmailLimit
	}
	return addresses, err
}

// provenAccountEmailSQL is the condition that the address row prefixed by
// alias ("" or "[[name]].") was verified and proven (account_email_proof.go):
// every lookup of an account by its address that decides access, or whom a
// share, invitation or search names, asks it, so an address an account only
// claimed opens nothing.
func provenAccountEmailSQL(alias string) string {
	return alias + `[[verified]] = 1 AND ` + alias + `[[email]] NOT IN (SELECT [[email]] FROM {{leafen_unproven_account_emails}})`
}

// Shared SQL consumers use this indexed set rather than another email policy.
// The addresses come without their column's text affinity (+), so a share's
// address compared by lower(trim(...)) in it is found by the email shares'
// address index (schema_baseline.json) rather than read whole.
func verifiedAccountEmailSQL(userParameter string) string {
	return `SELECT +[[email]] FROM {{leafen_account_emails}} WHERE [[user_id]] = {:` + userParameter + `} AND [[verified]] = 1
		AND [[email]] NOT IN (SELECT [[email]] FROM {{leafen_unproven_account_emails}})`
}

func managedAccountDomainSQL(userIDExpression string) string {
	return `SELECT lower(substr([[identity_email]].[[email]], instr([[identity_email]].[[email]], '@') + 1))
		FROM {{leafen_account_emails}} [[identity_email]] WHERE [[identity_email]].[[user_id]] = ` + userIDExpression
}

func addVerifiedAccountEmail(app core.App, user *core.Record, email string) error {
	if existing, err := findAccountByEmail(app, email); err == nil {
		if existing.Id == user.Id {
			if email == normalizePageShareEmail(user.Email()) && !user.Verified() {
				user.SetVerified(true)
				return app.Save(user)
			}
			return nil
		}
		return errRegistrationAccountExists
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var count int
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_account_emails}} WHERE [[user_id]] = {:userId}`).
		Bind(dbx.Params{"userId": user.Id}).Row(&count); err != nil {
		return err
	}
	if count >= accountSecondaryEmailLimit+1 {
		return errAccountEmailLimit
	}
	// An account with no address takes its first as its own (2026-10-20: it
	// stayed one more with none primary), written directly: PocketBase's save
	// of a changed email makes a new token key, signing every device out. The
	// account's addresses follow by trigger (schema_baseline.json).
	if normalizePageShareEmail(user.Email()) == "" {
		_, err := app.DB().NewQuery(`UPDATE {{users}} SET [[email]] = {:email}, [[verified]] = 1, [[updated]] = {:updated} WHERE [[id]] = {:userId}`).
			Bind(dbx.Params{"email": email, "updated": time.Now().UTC().Format("2006-01-02 15:04:05.000Z"), "userId": user.Id}).Execute()
		return err
	}
	_, err := app.DB().Insert("leafen_account_emails", dbx.Params{"email": email, "user_id": user.Id, "verified": 1}).Execute()
	return err
}

// addUnprovenAccountEmail adds an address on a server that sends no mail:
// at once, as nothing could confirm it, and kept unproven
// (account_email_proof.go), so it opens nothing an address decides — an
// invitation to it, a workspace's allowed domain — until someone proves it.
func addUnprovenAccountEmail(app core.App, userID, email, revision string) (accountEmailState, error) {
	state := accountEmailState{}
	err := app.RunInTransaction(func(txApp core.App) error {
		user, err := currentAccountEmailMutation(txApp, userID, revision)
		if err != nil {
			return err
		}
		if err := requireAccountEmailAddition(txApp, user, email); err != nil {
			return err
		}
		if state, err = accountEmailSnapshot(txApp, user); err != nil {
			return err
		}
		if !state.CanAdd {
			return errAccountEmailLimit
		}
		if _, err := findAccountByEmail(txApp, email); err == nil {
			return errRegistrationAccountExists
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err := addVerifiedAccountEmail(txApp, user, email); err != nil {
			return err
		}
		if err := markAccountEmailUnproven(txApp, user.Id, email); err != nil {
			return err
		}
		if err := auditAccountEmailChange(txApp, user, email, "add"); err != nil {
			return err
		}
		if user, err = txApp.FindRecordById("users", userID); err != nil {
			return err
		}
		if err := accountEmailsChanged(txApp, user); err != nil {
			return err
		}
		state, err = accountEmailSnapshot(txApp, user)
		return err
	})
	return state, err
}

// A matched secondary claim was verified independently; the primary's
// PocketBase flag does not certify or disqualify a different address.
func matchedAccountEmailVerified(user *core.Record, email string) bool {
	return normalizePageShareEmail(user.Email()) != email || user.Verified()
}

func accountEmailsChanged(app core.App, user *core.Record) error {
	if err := admitVerifiedEmailInvitations(app, user); err != nil {
		return err
	}
	now := time.Now().UTC().UnixMilli()
	if _, err := app.DB().NewQuery(`UPDATE {{leafen_auth_sessions}} SET [[deadline_at]] = CASE WHEN {:hours} = 0 THEN 0
		ELSE [[created_at]] + {:hours} * {:millis} END, [[updated_at]] = {:now} WHERE [[owner_id]] = {:userId} AND [[revoked_at]] = 0`).
		Bind(dbx.Params{"hours": 0, "millis": managedUserPolicyHoursMillis, "now": now, "userId": user.Id}).Execute(); err != nil {
		return err
	}
	rules, err := loadManagedUserRules(app, user)
	if err != nil {
		return err
	}
	if rules.Managed {
		if _, err := app.DB().NewQuery(`UPDATE {{leafen_auth_sessions}} SET [[deadline_at]] = [[created_at]] + {:hours} * {:millis},
			[[expires_at]] = MIN([[expires_at]], [[created_at]] + {:hours} * {:millis}) WHERE [[owner_id]] = {:userId} AND [[revoked_at]] = 0`).
			Bind(dbx.Params{"hours": rules.SessionHours, "millis": managedUserPolicyHoursMillis, "userId": user.Id}).Execute(); err != nil {
			return err
		}
	}
	recheckCollaborationAccessAfterCommit(app)
	return nil
}
