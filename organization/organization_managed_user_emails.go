package organization

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func registerManagedUserEmailRoutes(event *core.ServeEvent) {
	path := leafen.APIRoot + organizationManagedUserPath + "/emails"
	event.Router.GET(path, readManagedUserEmails)
	event.Router.POST(path, func(event *core.RequestEvent) error { return changeManagedUserEmail(event, "add") })
	event.Router.PUT(path, func(event *core.RequestEvent) error { return changeManagedUserEmail(event, "primary") })
	event.Router.DELETE(path, func(event *core.RequestEvent) error { return changeManagedUserEmail(event, "remove") })
}

func managedUserEmailSnapshot(app core.App, user *core.Record, organizationID string) (leafen.AccountEmailState, error) {
	state, err := leafen.AccountEmailSnapshot(app, user)
	if err != nil {
		return state, err
	}
	state.CanAdd = len(state.Emails) < leafen.AccountSecondaryEmailLimit+1
	visible := []leafen.AccountEmailAddress{}
	for _, address := range state.Emails {
		if requireVerifiedOrganizationDomain(app, organizationID, address.Email) == nil {
			visible = append(visible, address)
		}
	}
	state.Emails = visible
	return state, nil
}

func readManagedUserEmails(event *core.RequestEvent) error {
	authorization, ok := authorizeManagedUsers(event)
	if !ok {
		return nil
	}
	userID := strings.TrimSpace(event.Request.PathValue("userId"))
	state := leafen.AccountEmailState{}
	err := event.App.RunInTransaction(func(txApp core.App) error {
		if err := authorizeManagedUserTarget(txApp, authorization, userID); err != nil {
			return err
		}
		user, err := txApp.FindRecordById("users", userID)
		if err != nil {
			return err
		}
		state, err = managedUserEmailSnapshot(txApp, user, authorization.OrganizationID)
		return err
	})
	if err != nil {
		return managedUsersError(event, err, "emails-read")
	}
	return event.JSON(http.StatusOK, state)
}

func changeManagedUserEmail(event *core.RequestEvent, action string) error {
	authorization, ok := authorizeManagedUsers(event)
	if !ok {
		return nil
	}
	userID := strings.TrimSpace(event.Request.PathValue("userId"))
	input := leafen.AccountEmailMutationInput{}
	if err := event.BindBody(&input); err != nil {
		return leafen.APIError(event, http.StatusBadRequest, leafen.ErrorCodeInvalidInput, "Check the email.")
	}
	email, valid := leafen.NormalizeEmailIdentity(input.Email)
	if !valid {
		return managedUsersError(event, errManagedUserEmailInvalid, "emails-validate")
	}
	state := leafen.AccountEmailState{}
	change := func(txApp core.App) error {
		if err := authorizeManagedUserTarget(txApp, authorization, userID); err != nil {
			return err
		}
		if err := requireVerifiedOrganizationDomain(txApp, authorization.OrganizationID, email); err != nil {
			return errManagedUserEmailInvalid
		}
		// An address the person no longer has says so before whether the
		// list it was sent on is the latest (account_email.go's own change).
		if action != "add" {
			if owner, err := leafen.FindAccountByEmail(txApp, email); errors.Is(err, sql.ErrNoRows) || (err == nil && owner.Id != userID) {
				return leafen.ErrAccountEmailMissing
			} else if err != nil {
				return err
			}
		}
		user, err := leafen.CurrentAccountEmailMutation(txApp, userID, input.BaseRevision)
		if err != nil {
			return err
		}
		switch action {
		case "add":
			if err := leafen.AddVerifiedAccountEmail(txApp, user, email); err != nil {
				return err
			}
		case "remove":
			if leafen.NormalizePageShareEmail(user.Email()) == email {
				return leafen.ErrAccountEmailPrimary
			}
			if _, err := txApp.DB().NewQuery(`DELETE FROM {{leafen_account_emails}} WHERE [[user_id]] = {:userId} AND [[email]] = {:email}`).
				Bind(dbx.Params{"userId": userID, "email": email}).Execute(); err != nil {
				return err
			}
		case "primary":
			owner, err := leafen.FindAccountByEmail(txApp, email)
			if err != nil || owner.Id != userID {
				return leafen.ErrAccountEmailMissing
			}
			if email != leafen.NormalizePageShareEmail(user.Email()) {
				retained, err := retainedManagedPrimary(txApp, user, authorization.OrganizationID, true)
				if err != nil {
					return err
				}
				user.SetEmail(email)
				user.SetVerified(true)
				if err := txApp.Save(user); err != nil {
					return err
				}
				if retained != "" {
					if err := leafen.AddVerifiedAccountEmail(txApp, user, retained); err != nil {
						return err
					}
				}
				if err := leafen.RevokeAllBrowserSessions(txApp, userID); err != nil {
					return err
				}
			}
		}
		if err := leafen.AccountEmailsChanged(txApp, user); err != nil {
			return err
		}
		if err := recordManagedUserAudit(txApp, authorization, "account_email."+action, userID, map[string]any{"email": email}, leafen.AuthMFANow()().UnixMilli()); err != nil {
			return err
		}
		state, err = managedUserEmailSnapshot(txApp, user, authorization.OrganizationID)
		return err
	}
	// A key's call asks its person only for a change that would be made
	// (internal/app/user_key_acting_ask.go).
	err := leafen.RehearseBeforeAsking(event, change)
	if err == nil {
		if asked, askErr := leafen.AskBeforeActing(event); asked {
			return askErr
		}
		err = event.App.RunInTransaction(change)
	}
	if err != nil {
		if errors.Is(err, leafen.ErrRegistrationAccountExists) {
			return managedUsersError(event, errManagedUserEmailTaken, "emails-change")
		}
		if errors.Is(err, leafen.ErrAccountEmailChallenge) || errors.Is(err, leafen.ErrAccountEmailLimit) || errors.Is(err, leafen.ErrAccountEmailPrimary) {
			return leafen.AccountEmailAPIError(event, err)
		}
		return managedUsersError(event, err, "emails-change")
	}
	return event.JSON(http.StatusOK, state)
}
