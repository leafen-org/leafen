package app

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// An app registered to act as itself gets, in each workspace whose owner
// installs it, an account of its own: a workspace member, or a guest that
// sees only the pages shared with it, as a Notion integration does. The
// account isn't a person: it can't sign in, gets no notifications, emails
// or pushes, isn't found in people search or mentions, isn't counted among
// people, and reads as "〈app〉 (app)". It keeps working when the person who
// installed it leaves; an owner removes it.

const (
	appActsAsPerson      = "person"
	appActsAsIntegration = "integration"
	integrationRoleGuest = "guest"
	integrationEmailHost = "integrations.leafen.invalid"
)

type storedIntegrationAccount struct {
	UserID      string `db:"user_id"`
	AppID       string `db:"app_id"`
	WorkspaceID string `db:"workspace_id"`
	Role        string `db:"role"`
	InstalledBy string `db:"installed_by"`
	Created     int64  `db:"created"`
	RemovedAt   int64  `db:"removed_at"`
}

// integrationAccountOf reads the integration account a user is, if any.
func integrationAccountOf(app core.App, userID string) (storedIntegrationAccount, bool, error) {
	account := storedIntegrationAccount{}
	err := app.DB().NewQuery(`SELECT * FROM {{leafen_integration_accounts}} WHERE [[user_id]] = {:user}`).
		Bind(dbx.Params{"user": userID}).One(&account)
	if errors.Is(err, sql.ErrNoRows) {
		return account, false, nil
	}
	return account, err == nil, err
}

// integrationAccountUsers names which of some users are integration
// accounts, so lists of people can leave them out.
func integrationAccountUsers(app core.App, userIDs []string) (map[string]bool, error) {
	found := map[string]bool{}
	if len(userIDs) == 0 || !tableKnown(app, "leafen_integration_accounts") {
		return found, nil
	}
	params := dbx.Params{}
	placeholders := make([]string, 0, len(userIDs))
	for index, id := range userIDs {
		key := fmt.Sprintf("user%d", index)
		params[key] = id
		placeholders = append(placeholders, "{:"+key+"}")
	}
	ids := []string{}
	if err := app.DB().NewQuery(`SELECT [[user_id]] FROM {{leafen_integration_accounts}}
		WHERE [[user_id]] IN (` + strings.Join(placeholders, ",") + `)`).Bind(params).Column(&ids); err != nil {
		return nil, err
	}
	for _, id := range ids {
		found[id] = true
	}
	return found, nil
}

// integrationAccountSQL is a condition true for a user column that names
// an integration account, for queries that list people.
func integrationAccountSQL(column string) string {
	return `EXISTS (SELECT 1 FROM {{leafen_integration_accounts}} [[integration]] WHERE [[integration]].[[user_id]] = ` + column + `)`
}

// integrationGuestOf reports whether a user is a guest integration account
// installed in a workspace: it reaches the workspace's pages shared with it
// without being a member.
func integrationGuestOf(app core.App, userID, workspaceID string) bool {
	account, found, err := integrationAccountOf(app, userID)
	return err == nil && found && account.RemovedAt == 0 && account.Role == integrationRoleGuest && account.WorkspaceID == workspaceID
}

// ensureIntegrationAccount installs an app in a workspace as its own
// account, or brings back and re-roles the one it had, and returns it.
func ensureIntegrationAccount(
	txApp core.App, registration storedAppRegistration, workspaceID, role, installerID string,
) (string, error) {
	if role != workspaceRoleMember && role != integrationRoleGuest {
		return "", errors.New(errorCodeAppRegistrationInvalid)
	}
	account := storedIntegrationAccount{}
	err := txApp.DB().NewQuery(`SELECT * FROM {{leafen_integration_accounts}} WHERE [[app_id]] = {:app} AND [[workspace_id]] = {:workspace}`).
		Bind(dbx.Params{"app": registration.ID, "workspace": workspaceID}).One(&account)
	now := time.Now().UTC().UnixMilli()
	if errors.Is(err, sql.ErrNoRows) {
		users, err := txApp.FindCollectionByNameOrId("users")
		if err != nil {
			return "", err
		}
		record := core.NewRecord(users)
		suffix := strings.ToLower(security.RandomStringWithAlphabet(16, "abcdefghijklmnopqrstuvwxyz0123456789"))
		record.SetEmail("app-" + suffix + "@" + integrationEmailHost)
		// Nobody knows the password: the account never signs in.
		record.SetPassword(security.RandomString(48))
		record.Set("name", registration.ClientName)
		record.SetVerified(true)
		if err := txApp.Save(record); err != nil {
			return "", err
		}
		account = storedIntegrationAccount{
			UserID: record.Id, AppID: registration.ID, WorkspaceID: workspaceID, Role: role,
			InstalledBy: installerID, Created: now,
		}
		if _, err := txApp.DB().Insert("leafen_integration_accounts", dbx.Params{
			"user_id": account.UserID, "app_id": account.AppID, "workspace_id": workspaceID, "role": role,
			"installed_by": installerID, "created": now, "removed_at": 0,
		}).Execute(); err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	} else if _, err := txApp.DB().NewQuery(`UPDATE {{leafen_integration_accounts}} SET [[role]] = {:role},
		[[removed_at]] = 0, [[installed_by]] = {:installer} WHERE [[user_id]] = {:user}`).Bind(dbx.Params{
		"role": role, "installer": installerID, "user": account.UserID,
	}).Execute(); err != nil {
		return "", err
	}
	if role == workspaceRoleMember {
		if _, err := admitWorkspaceMember(txApp, workspaceMemberAdmissionRequest{
			ActorID: installerID, WorkspaceID: workspaceID, UserID: account.UserID, ActorOwnAccount: true,
		}); err != nil {
			return "", err
		}
	} else {
		if err := removeIntegrationMembership(txApp, installerID, account.UserID, workspaceID); err != nil {
			return "", err
		}
		// A guest is its organization's guest, so pages can be shared with it.
		workspace, err := txApp.FindRecordById(workspacesCollection, workspaceID)
		if err != nil {
			return "", err
		}
		if organizationID := workspace.GetString("organization"); organizationID != "" {
			if err := admitOrganizationGuest(txApp, organizationID, installerID, account.UserID); err != nil {
				return "", err
			}
		}
	}
	return account.UserID, nil
}

// removeIntegrationMembership takes a member account's membership away, as
// it turns guest or is removed, the way an owner removes a person.
func removeIntegrationMembership(app core.App, actorID, userID, workspaceID string) error {
	var member int
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_workspace_memberships}}
		WHERE [[workspace_id]] = {:workspace} AND [[user_id]] = {:user}`).Bind(dbx.Params{
		"workspace": workspaceID, "user": userID,
	}).Row(&member); err != nil || member == 0 {
		return err
	}
	_, err := revokeWorkspaceMembershipInTransaction(app, actorID, workspaceID, userID)
	return err
}

// findAppRegistration reads a registered app by its id, if the id is one.
func findAppRegistration(app core.App, clientID string) (storedAppRegistration, bool, error) {
	row := storedAppRegistration{}
	if !strings.HasPrefix(clientID, "lapp_") {
		return row, false, nil
	}
	err := app.DB().NewQuery(`SELECT * FROM {{leafen_app_registrations}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": clientID}).One(&row)
	if errors.Is(err, sql.ErrNoRows) {
		return row, false, nil
	}
	return row, err == nil, err
}

// authorizeConnectionWorkspaces checks that a connection's person may still
// read its workspaces. A guest integration account isn't a member: it holds
// its one workspace by being installed there, and each page it reads by
// being shared. A removed account holds nothing.
func authorizeConnectionWorkspaces(app core.App, ownerID string, workspaceIDs []string) error {
	account, found, err := integrationAccountOf(app, ownerID)
	if err != nil {
		return err
	}
	if found {
		if account.RemovedAt != 0 {
			return permissionDenied()
		}
		if account.Role == integrationRoleGuest {
			if len(workspaceIDs) == 1 && workspaceIDs[0] == account.WorkspaceID {
				return nil
			}
			return permissionDenied()
		}
	}
	return authorizeWorkspaceSet(app, ownerID, workspaceIDs, permissionWorkspaceRead)
}

func registerWorkspaceAppRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+workspaceAppListPath, listWorkspaceApps)
	event.Router.PATCH(apiRoot+workspaceAppUpdatePath, changeWorkspaceApp)
	event.Router.DELETE(apiRoot+workspaceAppRemovePath, removeWorkspaceApp)
}

// listWorkspaceApps lists the apps installed in a workspace as their own
// accounts, for its members: the page menu shares pages with the guests.
func listWorkspaceApps(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	if _, err := authorizeWorkspace(event.App, user.Id, workspaceID, permissionWorkspaceRead); err != nil {
		return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
	}
	rows := []struct {
		storedIntegrationAccount
		Name string `db:"name"`
	}{}
	if err := event.App.DB().NewQuery(`SELECT [[account]].*, [[user]].[[name]] AS [[name]]
		FROM {{leafen_integration_accounts}} [[account]] JOIN {{users}} [[user]] ON [[user]].[[id]] = [[account]].[[user_id]]
		WHERE [[account]].[[workspace_id]] = {:workspace} AND [[account]].[[removed_at]] = 0
		ORDER BY [[account]].[[created]], [[account]].[[user_id]]`).Bind(dbx.Params{"workspace": workspaceID}).All(&rows); err != nil {
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeAppRegistrationFailed,
			"Couldn't list the apps.", "workspace_apps", "list", errorCodeAppRegistrationFailed, err)
	}
	response := workspaceAppListResponse{Apps: []workspaceAppWire{}}
	for _, row := range rows {
		response.Apps = append(response.Apps, workspaceAppWire{
			AppID: row.AppID, Name: row.Name, UserID: row.UserID, Role: row.Role,
			InstalledBy: row.InstalledBy, CreatedAt: row.Created,
		})
	}
	return event.JSON(http.StatusOK, response)
}

func loadInstalledWorkspaceApp(event *core.RequestEvent) (string, storedIntegrationAccount, storedAppRegistration, bool) {
	user, ok := authenticatedUser(event)
	if !ok {
		_ = apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
		return "", storedIntegrationAccount{}, storedAppRegistration{}, false
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	appID := strings.TrimSpace(event.Request.PathValue("appId"))
	account := storedIntegrationAccount{}
	if _, err := authorizeWorkspace(event.App, user.Id, workspaceID, permissionWorkspaceOwner); err != nil ||
		event.App.DB().NewQuery(`SELECT * FROM {{leafen_integration_accounts}} WHERE [[app_id]] = {:app}
			AND [[workspace_id]] = {:workspace} AND [[removed_at]] = 0`).
			Bind(dbx.Params{"app": appID, "workspace": workspaceID}).One(&account) != nil {
		_ = apiError(event, http.StatusNotFound, errorCodeAppRegistrationNotFound, "App not found.")
		return "", storedIntegrationAccount{}, storedAppRegistration{}, false
	}
	registration, found, err := findAppRegistration(event.App, appID)
	if err != nil || !found {
		registration = storedAppRegistration{ID: appID}
	}
	return user.Id, account, registration, true
}

// changeWorkspaceApp makes an installed app a member or a guest.
func changeWorkspaceApp(event *core.RequestEvent) error {
	actorID, account, registration, ok := loadInstalledWorkspaceApp(event)
	if !ok {
		return nil
	}
	input := workspaceAppRoleInput{}
	if err := event.BindBody(&input); err != nil || (input.Role != workspaceRoleMember && input.Role != integrationRoleGuest) {
		return apiError(event, http.StatusBadRequest, errorCodeAppRegistrationInvalid, "Choose member or guest.")
	}
	if registration.ClientName == "" {
		registration.ClientName = account.AppID
	}
	err := event.App.RunInTransaction(func(txApp core.App) error {
		_, err := ensureIntegrationAccount(txApp, registration, account.WorkspaceID, input.Role, actorID)
		return err
	})
	if err != nil {
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeAppRegistrationFailed,
			"Couldn't change the app.", "workspace_apps", "change", errorCodeAppRegistrationFailed, err)
	}
	return event.NoContent(http.StatusNoContent)
}

// removeWorkspaceApp takes an app out of a workspace: its account loses its
// membership and its connections end.
func removeWorkspaceApp(event *core.RequestEvent) error {
	actorID, account, _, ok := loadInstalledWorkspaceApp(event)
	if !ok {
		return nil
	}
	err := event.App.RunInTransaction(func(txApp core.App) error {
		now := time.Now().UTC().UnixMilli()
		if err := removeIntegrationMembership(txApp, actorID, account.UserID, account.WorkspaceID); err != nil {
			return err
		}
		if _, err := txApp.DB().NewQuery(`UPDATE {{leafen_integration_accounts}} SET [[removed_at]] = {:now}
			WHERE [[user_id]] = {:user}`).Bind(dbx.Params{"now": now, "user": account.UserID}).Execute(); err != nil {
			return err
		}
		if _, err := txApp.DB().NewQuery(`UPDATE {{leafen_mcp_connections}}
			SET [[revoked_at]] = {:now}, [[retire_after]] = {:retireAfter}
			WHERE [[owner_id]] = {:user} AND [[revoked_at]] = 0`).Bind(dbx.Params{
			"now": now, "retireAfter": now + int64(mcpCredentialRetentionMilliseconds), "user": account.UserID,
		}).Execute(); err != nil {
			return err
		}
		notifyMCPCredentialRegistryAfterCommit(txApp)
		return nil
	})
	if err != nil {
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeAppRegistrationFailed,
			"Couldn't remove the app.", "workspace_apps", "remove", errorCodeAppRegistrationFailed, err)
	}
	return event.NoContent(http.StatusNoContent)
}
