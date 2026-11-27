package organization

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Notion's suspension of a managed user (its help, read 2026-10-02): the
// person can't sign in or hear of anything until unsuspended, while their
// content, comments and the connections they set up stay as they are and
// others may still share pages with them and add them to workspaces. In
// Leafen their sessions end, their personal access tokens and AI connections
// stop and no notification email or digest reaches them; a connection,
// which answers as its own bot, goes on. An organization lifts only its own
// suspension, even once the person's email leaves its domains.

func suspendOrganizationManagedUser(event *core.RequestEvent) error {
	authorization, ok := authorizeManagedUsers(event)
	if !ok {
		return nil
	}
	userID := strings.TrimSpace(event.Request.PathValue("userId"))
	change := func(txApp core.App) error {
		if err := authorizeManagedUserTarget(txApp, authorization, userID); err != nil {
			return err
		}
		// Suspended already, by this organization or another, People shows
		// them deactivated as before.
		var before int
		if err := txApp.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_organization_user_suspensions}} WHERE [[user_id]] = {:userId}`).
			Bind(dbx.Params{"userId": userID}).Row(&before); err != nil {
			return err
		}
		now := time.Now().UTC().UnixMilli()
		result, err := txApp.DB().NewQuery(`
			INSERT INTO {{leafen_organization_user_suspensions}}
				([[organization_id]], [[user_id]], [[suspended_by]], [[suspended_at]])
			VALUES ({:organizationId}, {:userId}, {:actorId}, {:now})
			ON CONFLICT ([[organization_id]], [[user_id]]) DO NOTHING
		`).Bind(dbx.Params{
			"organizationId": authorization.OrganizationID, "userId": userID,
			"actorId": authorization.PrincipalID, "now": now,
		}).Execute()
		if err != nil {
			return err
		}
		if changed, err := result.RowsAffected(); err != nil || changed == 0 {
			return err
		}
		revoked, err := leafen.RevokeAllBrowserSessionsCount(txApp, userID)
		if err != nil {
			return err
		}
		if before == 0 {
			if err := leafen.PeopleMembershipChangedEverywhere(txApp, userID, authorization.PrincipalID); err != nil {
				return err
			}
		}
		return recordManagedUserAudit(txApp, authorization, "managed_user.suspend", userID,
			map[string]any{"revokedSessions": revoked}, now)
	}
	// A key's call asks its person only for a suspension that would be made
	// (internal/app/user_key_acting_ask.go).
	err := leafen.RehearseBeforeAsking(event, change)
	if err == nil {
		if asked, askErr := leafen.AskBeforeActing(event); asked {
			return askErr
		}
		err = event.App.RunInTransaction(change)
	}
	if err != nil {
		return managedUsersError(event, err, "suspend")
	}
	return respondOrganizationManagedUser(event, authorization, userID)
}

// unsuspendOrganizationManagedUser lifts the organization's own suspension.
func unsuspendOrganizationManagedUser(event *core.RequestEvent) error {
	authorization, ok := authorizeManagedUsers(event)
	if !ok {
		return nil
	}
	userID := strings.TrimSpace(event.Request.PathValue("userId"))
	change := func(txApp core.App) error {
		result, err := txApp.DB().NewQuery(`
			DELETE FROM {{leafen_organization_user_suspensions}}
			WHERE [[organization_id]] = {:organizationId} AND [[user_id]] = {:userId} AND [[deletion_job_id]] = ''
		`).Bind(dbx.Params{"organizationId": authorization.OrganizationID, "userId": userID}).Execute()
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if changed == 0 {
			// A person to be deleted is restored instead (organization_managed_user_deletion.go).
			var scheduled int
			if err := txApp.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_organization_user_suspensions}}
				WHERE [[organization_id]] = {:organizationId} AND [[user_id]] = {:userId}`).Bind(dbx.Params{
				"organizationId": authorization.OrganizationID, "userId": userID,
			}).Row(&scheduled); err != nil {
				return err
			}
			if scheduled > 0 {
				return errManagedUserDeletionScheduled
			}
			// Nothing to lift: the person must still be one to manage.
			return authorizeManagedUserTarget(txApp, authorization, userID)
		}
		// No other suspension left: People shows them by their membership again.
		var still int
		if err := txApp.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_organization_user_suspensions}} WHERE [[user_id]] = {:userId}`).
			Bind(dbx.Params{"userId": userID}).Row(&still); err != nil {
			return err
		}
		if still == 0 {
			if err := leafen.PeopleMembershipChangedEverywhere(txApp, userID, authorization.PrincipalID); err != nil {
				return err
			}
		}
		return recordManagedUserAudit(txApp, authorization, "managed_user.unsuspend", userID, nil, time.Now().UTC().UnixMilli())
	}
	// A key's call asks its person only for a lift that would be made
	// (internal/app/user_key_acting_ask.go).
	err := leafen.RehearseBeforeAsking(event, change)
	if err == nil {
		if asked, askErr := leafen.AskBeforeActing(event); asked {
			return askErr
		}
		err = event.App.RunInTransaction(change)
	}
	if err != nil {
		return managedUsersError(event, err, "unsuspend")
	}
	return respondOrganizationManagedUser(event, authorization, userID)
}

func recordManagedUserAudit(
	app core.App, authorization leafen.OrganizationAuthorization, action, userID string, metadata map[string]any, now int64,
) error {
	return leafen.RecordOrganizationAuditEvent(app, leafen.OrganizationAuditEvent{
		ID:             leafen.OrganizationAuditID(authorization.OrganizationID, action, userID+"@"+strconv.FormatInt(now, 10)),
		OrganizationID: authorization.OrganizationID, ActorID: authorization.PrincipalID,
		Action: action, TargetType: "user", TargetID: userID, Metadata: metadata, OccurredAt: now,
	})
}

func respondOrganizationManagedUser(event *core.RequestEvent, authorization leafen.OrganizationAuthorization, userID string) error {
	users, _, err := queryOrganizationManagedUsers(event.App, authorization.OrganizationID,
		organizationManagedUserFilter{UserID: userID}, 1)
	if err != nil {
		return managedUsersError(event, err, "read")
	}
	if len(users) != 1 {
		return managedUsersError(event, errManagedUserNotFound, "read")
	}
	return event.JSON(http.StatusOK, users[0])
}

// suspendedUserSQL is an SQL expression, true when an organization has
// suspended the user the given expression names.
func suspendedUserSQL(userExpression string) string {
	return `EXISTS (SELECT 1 FROM {{leafen_organization_user_suspensions}} [[suspension]]
		WHERE [[suspension]].[[user_id]] = ` + userExpression + `)`
}
