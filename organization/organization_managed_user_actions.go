package organization

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// One managed user's actions on Notion's dashboard (its help, read
// 2026-10-02) beside logging out and suspending: changing their name and
// email, the new email on one of the organization's verified domains and
// signing them out everywhere, and
// removing them from the workspaces of other organizations. An admin acts
// on neither themselves, the organization's owner nor a server
// administrator, whom the server's administrator screen manages.

const (
	errorCodeManagedUserProtected    = "LEAFEN_MANAGED_USER_PROTECTED"
	errorCodeManagedUserEmailInvalid = "LEAFEN_MANAGED_USER_EMAIL_INVALID"
	errorCodeManagedUserEmailTaken   = "LEAFEN_MANAGED_USER_EMAIL_TAKEN"
	errorCodeManagedUserNotFound     = "LEAFEN_MANAGED_USER_NOT_FOUND"
)

var (
	errManagedUserProtected    = errors.New(errorCodeManagedUserProtected)
	errManagedUserEmailInvalid = errors.New(errorCodeManagedUserEmailInvalid)
	errManagedUserEmailTaken   = errors.New(errorCodeManagedUserEmailTaken)
	// errManagedUserNotFound is a userId no one the organization manages
	// has, said apart from the organization not found (managedUsersError).
	errManagedUserNotFound = errors.New(errorCodeManagedUserNotFound)
)

type organizationManagedUserChange struct {
	Name  *string `json:"name"`
	Email *string `json:"email"`
}

type organizationManagedUserRemoval struct {
	User organizationManagedUser `json:"user"`
	// The workspaces the person left, and those of theirs they stay in.
	Removed int `json:"removed"`
	Owned   int `json:"owned"`
}

// authorizeManagedUserTarget holds an action to someone on the
// organization's domains other than the admin, its owner or a server
// administrator.
func authorizeManagedUserTarget(app core.App, authorization leafen.OrganizationAuthorization, userID string) error {
	var managed int
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM (` + organizationManagedUsersSQL + `) WHERE [[id]] = {:userId}`).Bind(dbx.Params{
		"organizationId": authorization.OrganizationID, "query": "", "id": "", "userId": userID,
	}).Row(&managed); err != nil {
		return err
	}
	if userID == "" || managed != 1 {
		return errManagedUserNotFound
	}
	if userID == authorization.PrincipalID || userID == authorization.OwnerID {
		return errManagedUserProtected
	}
	administrator, err := leafen.IsAdministrator(app, userID)
	if err != nil {
		return err
	}
	if administrator {
		return errManagedUserProtected
	}
	return nil
}

func changeOrganizationManagedUser(event *core.RequestEvent) error {
	authorization, ok := authorizeManagedUsers(event)
	if !ok {
		return nil
	}
	userID := strings.TrimSpace(event.Request.PathValue("userId"))
	input := organizationManagedUserChange{}
	if err := event.BindBody(&input); err != nil || (input.Name == nil && input.Email == nil) {
		return leafen.APIError(event, http.StatusBadRequest, leafen.ErrorCodeInvalidInput, "Check the name and email.")
	}
	name, email := "", ""
	if input.Name != nil {
		name = strings.TrimSpace(*input.Name)
		if name == "" || utf8.RuneCountInString(name) > leafen.MaxAuthNameLength {
			return leafen.APIError(event, http.StatusBadRequest, leafen.ErrorCodeInvalidInput, "Check the name.")
		}
	}
	if input.Email != nil {
		normalized, valid := leafen.NormalizeEmailIdentity(*input.Email)
		if !valid {
			return managedUsersError(event, errManagedUserEmailInvalid, "change-validate")
		}
		email = normalized
	}
	change := func(txApp core.App) error {
		if err := authorizeManagedUserTarget(txApp, authorization, userID); err != nil {
			return err
		}
		user, err := txApp.FindRecordById("users", userID)
		if err != nil {
			return err
		}
		changed := []string{}
		previousEmail := user.Email()
		if email != "" && !strings.EqualFold(email, previousEmail) {
			retained, err := retainedManagedPrimary(txApp, user, authorization.OrganizationID, false)
			if err != nil {
				return err
			}
			if err := requireVerifiedOrganizationDomain(txApp, authorization.OrganizationID, email); err != nil {
				return errManagedUserEmailInvalid
			}
			existing, err := leafen.FindAccountByEmail(txApp, email)
			if err == nil && existing.Id != userID {
				return errManagedUserEmailTaken
			}
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
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
			if err := leafen.AccountEmailsChanged(txApp, user); err != nil {
				return err
			}
			// A new email takes a new sign-in, as the account's token key
			// changed with it; the sessions end where everyone sees them.
			if _, err := leafen.RevokeAllBrowserSessionsCount(txApp, userID); err != nil {
				return err
			}
			changed = append(changed, "email")
		}
		if name != "" && name != user.GetString("name") {
			if _, err := leafen.MutateAccountProfileAuthority(txApp, user, name, user.GetString("profile_avatar")); err != nil {
				return err
			}
			changed = append(changed, "name")
		}
		if len(changed) == 0 {
			return nil
		}
		metadata := map[string]any{"fields": changed}
		if user.Email() != previousEmail {
			metadata["previousEmail"], metadata["email"] = previousEmail, user.Email()
		}
		return recordManagedUserAudit(txApp, authorization, "managed_user.update", userID, metadata, time.Now().UTC().UnixMilli())
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
		return managedUsersError(event, err, "change")
	}
	return respondOrganizationManagedUser(event, authorization, userID)
}

// removeOrganizationManagedUserFromExternalWorkspaces takes the person out of
// every workspace of another organization they belong to but own, one at a
// time as its owner's removal of a member does, the workspace's own
// organization hearing of it in its audit log.
func removeOrganizationManagedUserFromExternalWorkspaces(event *core.RequestEvent, rooms leafen.WorkspaceRoomInvalidator) error {
	authorization, ok := authorizeManagedUsers(event)
	if !ok {
		return nil
	}
	userID := strings.TrimSpace(event.Request.PathValue("userId"))
	type externalWorkspace struct {
		ID             string `db:"id"`
		OrganizationID string `db:"organization"`
		Owned          bool   `db:"owned"`
	}
	workspaces := []externalWorkspace{}
	err := event.App.RunInTransaction(func(txApp core.App) error {
		if err := authorizeManagedUserTarget(txApp, authorization, userID); err != nil {
			return err
		}
		return txApp.DB().NewQuery(`
			SELECT [[workspace]].[[id]], [[workspace]].[[organization]],
				[[workspace]].[[workspace_owner]] = {:userId} AS [[owned]]
			FROM {{leafen_workspaces}} [[workspace]]
			WHERE [[workspace]].[[deletion_job_id]] = '' AND [[workspace]].[[organization]] <> {:organizationId}
				AND ([[workspace]].[[workspace_owner]] = {:userId} OR EXISTS (SELECT 1 FROM {{leafen_workspace_memberships}} [[membership]]
					WHERE [[membership]].[[workspace_id]] = [[workspace]].[[id]] AND [[membership]].[[user_id]] = {:userId}
						AND [[membership]].[[role]] = {:member}))
			ORDER BY [[workspace]].[[id]]
		`).Bind(dbx.Params{
			"userId": userID, "organizationId": authorization.OrganizationID, "member": leafen.WorkspaceRoleMember,
		}).All(&workspaces)
	})
	if err != nil {
		return managedUsersError(event, err, "external-workspaces")
	}
	// A key's call asks its person once the person is found the
	// organization's to act on (internal/app/user_key_acting_ask.go).
	if asked, err := leafen.AskBeforeActing(event); asked {
		return err
	}
	result := organizationManagedUserRemoval{}
	runner := leafen.WorkspaceMembershipRevocationRunner(event.App, rooms)
	for _, workspace := range workspaces {
		if workspace.Owned {
			result.Owned++
			continue
		}
		jobID, err := removeManagedUserFromWorkspace(event.App, authorization, userID, workspace.ID, workspace.OrganizationID)
		if err != nil {
			return managedUsersError(event, err, "external-workspace-remove")
		}
		if jobID == "" {
			continue
		}
		result.Removed++
		if completed, err := runner.AdvanceWorkspaceMembershipRevocation(event.Request.Context(), jobID); err != nil || !completed {
			runner.Notify()
		}
	}
	users, _, err := queryOrganizationManagedUsers(event.App, authorization.OrganizationID,
		organizationManagedUserFilter{UserID: userID}, 1)
	if err != nil || len(users) != 1 {
		return managedUsersError(event, errors.Join(err, errManagedUserNotFound), "external-workspaces-read")
	}
	result.User = users[0]
	return event.JSON(http.StatusOK, result)
}

func removeManagedUserFromWorkspace(
	app core.App, authorization leafen.OrganizationAuthorization, userID, workspaceID, workspaceOrganizationID string,
) (string, error) {
	release := leafen.AcquireProductOperationLock(leafen.WorkspaceMembershipRevocationLockNamespace, workspaceID)
	defer release()
	jobID := ""
	err := app.RunInTransaction(func(txApp core.App) error {
		admission, err := leafen.RevokeWorkspaceMembershipInTransaction(txApp, authorization.PrincipalID, workspaceID, userID)
		if err != nil || admission.Missing {
			return err
		}
		jobID = admission.Job.ID
		var workspaceName string
		if err := txApp.DB().NewQuery(`SELECT [[name]] FROM {{leafen_workspaces}} WHERE [[id]] = {:id}`).
			Bind(dbx.Params{"id": workspaceID}).Row(&workspaceName); err != nil {
			return err
		}
		if err := queueManagedUserNotice(txApp, authorization.OrganizationID, userID, managedUserNoticeWorkspaceRemoved, workspaceName); err != nil {
			return err
		}
		now := time.Now().UTC().UnixMilli()
		subject := userID + "@" + workspaceID + "@" + strconv.FormatInt(now, 10)
		for _, event := range []leafen.OrganizationAuditEvent{{
			ID:             leafen.OrganizationAuditID(authorization.OrganizationID, "managed_user.remove_external_workspace", subject),
			OrganizationID: authorization.OrganizationID, WorkspaceID: workspaceID,
			Action: "managed_user.remove_external_workspace", Metadata: map[string]any{"workspaceOrganizationId": workspaceOrganizationID},
		}, {
			ID:             leafen.OrganizationAuditID(workspaceOrganizationID, "workspace_member.remove_by_managing_organization", subject),
			OrganizationID: workspaceOrganizationID, WorkspaceID: workspaceID,
			Action: "workspace_member.remove_by_managing_organization", Metadata: map[string]any{"managingOrganizationId": authorization.OrganizationID},
		}} {
			event.ActorID, event.TargetType, event.TargetID, event.OccurredAt = authorization.PrincipalID, "user", userID, now
			if err := leafen.RecordOrganizationAuditEvent(txApp, event); err != nil {
				return err
			}
		}
		return nil
	})
	return jobID, err
}
