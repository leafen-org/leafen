package organization

import (
	"net/http"
	"strings"
	"time"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Notion's password resets for managed users (its help, read 2026-10-02; the
// Identity row that forces a reset at every managed user's next sign-in, seen
// behind its upgrade): one person's or everyone's but the admin's, each told
// by email. In Leafen the person sets a new password the
// next time they open the app, as after a server administrator's reset
// (must_change_password), their current one still signing them in until
// then.

// resetManagedUserPassword asks one person for a new password and queues the
// email telling them.
func resetManagedUserPassword(app core.App, authorization leafen.OrganizationAuthorization, userID string, now int64) error {
	if err := leafen.SetMustChangePasswordInTransaction(app, userID, authorization.PrincipalID, leafen.AccountPasswordReasonReset); err != nil {
		return err
	}
	if err := queueManagedUserNotice(app, authorization.OrganizationID, userID, managedUserNoticePasswordReset, ""); err != nil {
		return err
	}
	return recordManagedUserAudit(app, authorization, "managed_user.password_reset", userID, nil, now)
}

func resetOrganizationManagedUserPassword(event *core.RequestEvent) error {
	authorization, ok := authorizeManagedUsers(event)
	if !ok {
		return nil
	}
	userID := strings.TrimSpace(event.Request.PathValue("userId"))
	change := func(txApp core.App) error {
		if err := authorizeManagedUserTarget(txApp, authorization, userID); err != nil {
			return err
		}
		return resetManagedUserPassword(txApp, authorization, userID, time.Now().UTC().UnixMilli())
	}
	// A key's call asks its person only for a reset that would be made
	// (internal/app/user_key_acting_ask.go).
	err := leafen.RehearseBeforeAsking(event, change)
	if err == nil {
		if asked, askErr := leafen.AskBeforeActing(event); asked {
			return askErr
		}
		err = event.App.RunInTransaction(change)
	}
	if err != nil {
		return managedUsersError(event, err, "password-reset")
	}
	return respondOrganizationManagedUser(event, authorization, userID)
}

// resetOrganizationManagedUserPasswords asks everyone on the organization's
// domains for a new password but those an admin doesn't act on: the admin,
// the organization's owner and server administrators.
func resetOrganizationManagedUserPasswords(event *core.RequestEvent) error {
	authorization, ok := authorizeManagedUsers(event)
	if !ok {
		return nil
	}
	users := 0
	change := func(txApp core.App) error {
		ids := []string{}
		if err := txApp.DB().NewQuery(`SELECT [[id]] FROM (` + organizationManagedUsersSQL + `) WHERE [[id]] NOT IN ({:principalId}, {:ownerId})
			AND [[id]] NOT IN (SELECT [[user_id]] FROM {{leafen_administrators}})`).Bind(dbx.Params{
			"organizationId": authorization.OrganizationID, "query": "", "id": "",
			"principalId": authorization.PrincipalID, "ownerId": authorization.OwnerID,
		}).Column(&ids); err != nil {
			return err
		}
		now := time.Now().UTC().UnixMilli()
		for _, id := range ids {
			if err := resetManagedUserPassword(txApp, authorization, id, now); err != nil {
				return err
			}
			users++
		}
		return nil
	}
	// A key's call asks its person only for a reset that would be made
	// (internal/app/user_key_acting_ask.go).
	err := leafen.RehearseBeforeAsking(event, change)
	if err == nil {
		if asked, askErr := leafen.AskBeforeActing(event); asked {
			return askErr
		}
		users = 0
		err = event.App.RunInTransaction(change)
	}
	if err != nil {
		return managedUsersError(event, err, "password-reset-all")
	}
	return event.JSON(http.StatusOK, map[string]int{"users": users})
}
