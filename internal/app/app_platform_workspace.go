package app

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// Each app has a workspace of its own in each organization that uses it
// (docs/app-platform.md 3, 6): its account — an integration account that
// never signs in — owns it, and the people the app is granted to are its
// members, admitted when they first open the app there, taken out when the
// grant no longer covers them and let back in when it does again. The
// workspace carries the app's mark, so the switcher lists it under the app
// rather than among the workspaces.

const appWorkspacesTable = "leafen_app_workspaces"

type storedAppWorkspace struct {
	InstallID   string `db:"install_id"`
	TenancyKey  string `db:"tenancy_key"`
	WorkspaceID string `db:"workspace_id"`
	AccountID   string `db:"account_id"`
}

func loadAppWorkspaces(app core.App, installID string) ([]storedAppWorkspace, error) {
	rows := []storedAppWorkspace{}
	err := app.DB().NewQuery(`SELECT [[install_id]], [[tenancy_key]], [[workspace_id]], [[account_id]] FROM {{leafen_app_workspaces}}
		WHERE [[install_id]] = {:installId} ORDER BY [[tenancy_key]]`).Bind(dbx.Params{"installId": installID}).All(&rows)
	return rows, err
}

// ensureAppWorkspace is the app's workspace in a tenancy, made with its
// account the first time anyone opens the app there.
func ensureAppWorkspace(app core.App, install storedAppInstall, manifest appManifest, tenancyKey, actorID string) (storedAppWorkspace, error) {
	result := storedAppWorkspace{}
	err := app.RunInTransaction(func(tx core.App) error {
		err := tx.DB().NewQuery(`SELECT [[install_id]], [[tenancy_key]], [[workspace_id]], [[account_id]] FROM {{leafen_app_workspaces}}
			WHERE [[install_id]] = {:installId} AND [[tenancy_key]] = {:tenancy}`).
			Bind(dbx.Params{"installId": install.ID, "tenancy": tenancyKey}).One(&result)
		if err == nil || !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		// A workspace deleted outright leaves a map of its data that a new one mustn't follow.
		if _, err := tx.DB().Delete(appDataTable, dbx.HashExp{"install_id": install.ID, "tenancy_key": tenancyKey}).Execute(); err != nil {
			return err
		}
		organizationID := strings.TrimPrefix(tenancyKey, "org:")
		if tenancyKey == appTenancyServer {
			organizationID = ""
		}
		name := appOrganizationName(tx, manifest, tenancyKey)
		users, err := tx.FindCollectionByNameOrId("users")
		if err != nil {
			return err
		}
		account := core.NewRecord(users)
		account.SetEmail("app-" + strings.ToLower(security.RandomStringWithAlphabet(16, "abcdefghijklmnopqrstuvwxyz0123456789")) + "@" + integrationEmailHost)
		// Nobody knows the password: the account never signs in.
		account.SetPassword(security.RandomString(48))
		account.Set("name", name)
		account.SetVerified(true)
		if err := tx.Save(account); err != nil {
			return err
		}
		if organizationID != "" {
			if err := ensureOrganizationMembership(tx, organizationID, account.Id, organizationRoleMember, organizationMemberStatusActive); err != nil {
				return err
			}
		}
		collection, err := tx.FindCollectionByNameOrId(workspacesCollection)
		if err != nil {
			return err
		}
		// As every workspace of an organization, its record names the
		// organization's owner; the app's account is the workspace's owner.
		recordOwner := account.Id
		if organizationID != "" {
			organization, err := tx.FindRecordById(organizationsCollection, organizationID)
			if err != nil {
				return err
			}
			recordOwner = organization.GetString("owner")
		}
		workspace := core.NewRecord(collection)
		workspace.Set("owner", recordOwner)
		workspace.Set("workspace_owner", account.Id)
		workspace.Set("organization", organizationID)
		workspace.Set("name", name)
		workspace.Set("app_install_id", install.ID)
		if err := tx.Save(workspace); err != nil {
			return err
		}
		now := time.Now().UTC().UnixMilli()
		if err := initializeWorkspacePermissionCursor(tx, workspace.Id, now); err != nil {
			return err
		}
		if err := initializePageInteractionRevision(tx, workspace.Id); err != nil {
			return err
		}
		if organizationID != "" {
			if err := bumpOrganizationWorkspaceSetRevision(tx, organizationID); err != nil {
				return err
			}
			if err := recordOrganizationAudit(tx, organizationID, actorID, "workspace.created", "workspace", workspace.Id); err != nil {
				return err
			}
		}
		if _, err := tx.DB().Insert("leafen_integration_accounts", dbx.Params{
			"user_id": account.Id, "app_id": appConnectionClientID + install.ID, "workspace_id": workspace.Id,
			"role": workspaceRoleMember, "installed_by": actorID, "created": now, "removed_at": 0,
		}).Execute(); err != nil {
			return err
		}
		result = storedAppWorkspace{InstallID: install.ID, TenancyKey: tenancyKey, WorkspaceID: workspace.Id, AccountID: account.Id}
		_, err = tx.DB().Insert(appWorkspacesTable, dbx.Params{
			"install_id": install.ID, "tenancy_key": tenancyKey, "workspace_id": workspace.Id, "account_id": account.Id, "created": now,
		}).Execute()
		return err
	})
	return result, err
}

// ensureAppWorkspaceMember makes a granted person a member of the app's
// workspace, as its account would add them.
func ensureAppWorkspaceMember(app core.App, workspace storedAppWorkspace, userID string) error {
	var member int
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_workspace_memberships}}
		WHERE [[workspace_id]] = {:workspace} AND [[user_id]] = {:user} AND [[role]] = 'member'`).
		Bind(dbx.Params{"workspace": workspace.WorkspaceID, "user": userID}).Row(&member); err != nil || member > 0 {
		return err
	}
	_, err := admitWorkspaceMember(app, workspaceMemberAdmissionRequest{
		ActorID: workspace.AccountID, WorkspaceID: workspace.WorkspaceID, UserID: userID, ActorOwnAccount: true,
	})
	return err
}

// revokeAppWorkspacesAfterLeaving takes a person out of a tenancy's app
// workspaces when they leave its last ordinary workspace, in the same
// change: an app's workspace follows the organization's people.
func revokeAppWorkspacesAfterLeaving(txApp core.App, workspaceID, userID string) error {
	place := struct {
		Organization string `db:"organization"`
		App          int    `db:"app"`
	}{}
	err := txApp.DB().NewQuery(`SELECT [[organization]], EXISTS(SELECT 1 FROM {{leafen_app_workspaces}} WHERE [[workspace_id]] = {:workspace}) AS [[app]]
		FROM {{leafen_workspaces}} WHERE [[id]] = {:workspace}`).Bind(dbx.Params{"workspace": workspaceID}).One(&place)
	if errors.Is(err, sql.ErrNoRows) || err == nil && place.App == 1 {
		return nil
	}
	if err != nil {
		return err
	}
	tenancy := appTenancyServer
	if place.Organization != "" {
		tenancy = "org:" + place.Organization
	}
	if ordinary, err := appPersonOrdinaryWorkspaces(txApp, userID, tenancy); err != nil || len(ordinary) > 0 {
		return err
	}
	held := []struct {
		Workspace string `db:"workspace_id"`
		Account   string `db:"account_id"`
	}{}
	if err := txApp.DB().NewQuery(`SELECT a.[[workspace_id]], a.[[account_id]] FROM {{leafen_app_workspaces}} a
		JOIN {{leafen_workspace_memberships}} m ON m.[[workspace_id]] = a.[[workspace_id]] AND m.[[user_id]] = {:user} AND m.[[role]] = 'member'
		WHERE a.[[tenancy_key]] = {:tenancy}`).Bind(dbx.Params{"user": userID, "tenancy": tenancy}).All(&held); err != nil {
		return err
	}
	for _, workspace := range held {
		if _, err := revokeWorkspaceMembershipInTransaction(txApp, workspace.Account, workspace.Workspace, userID); err != nil {
			return err
		}
	}
	return nil
}

// syncAppWorkspaceMembers keeps the app's workspaces to the people who may
// use it there: those its grants no longer cover, or no longer in the
// organization, are taken out, and those who used it there and may again —
// its restore from the trash, a grant given back — are let in at once, with
// its teamspaces, rather than at their next opening (2026-11-08: a restored
// app's pages asked them to request access). It runs after a grant changes,
// after a restore and every hour, for what changed some other way.
func syncAppWorkspaceMembers(app core.App, installID string) error {
	install, err := loadAppInstall(app, installID)
	if err != nil {
		return err
	}
	workspaces, err := loadAppWorkspaces(app, installID)
	if err != nil {
		return err
	}
	mayHold := func(tenancyKey, userID string) (bool, error) {
		granted, err := appGranted(app, installID, tenancyKey, userID)
		if err != nil || !granted {
			return false, err
		}
		ordinary, err := appPersonOrdinaryWorkspaces(app, userID, tenancyKey)
		return len(ordinary) > 0, err
	}
	for _, workspace := range workspaces {
		members := []string{}
		if err := app.DB().NewQuery(`SELECT [[user_id]] FROM {{leafen_workspace_memberships}}
			WHERE [[workspace_id]] = {:workspace} AND [[user_id]] != {:account}`).
			Bind(dbx.Params{"workspace": workspace.WorkspaceID, "account": workspace.AccountID}).Column(&members); err != nil {
			return err
		}
		member := map[string]bool{}
		for _, userID := range members {
			member[userID] = true
			keep, err := mayHold(workspace.TenancyKey, userID)
			if err != nil {
				return err
			}
			if keep {
				continue
			}
			if err := app.RunInTransaction(func(tx core.App) error {
				_, err := revokeWorkspaceMembershipInTransaction(tx, workspace.AccountID, workspace.WorkspaceID, userID)
				return err
			}); err != nil {
				return err
			}
		}
		// A trashed app's people are out until it is restored.
		if install.RemovedAt > 0 {
			continue
		}
		if err := readmitAppWorkspaceMembers(app, install, workspace, member, mayHold); err != nil {
			return err
		}
	}
	return nil
}

// readmitAppWorkspaceMembers lets back into an app's workspace those who
// used the app there (they have its connection) and may again, as their
// first opening let them in.
func readmitAppWorkspaceMembers(app core.App, install storedAppInstall, workspace storedAppWorkspace, member map[string]bool,
	mayHold func(tenancyKey, userID string) (bool, error)) error {
	used := []string{}
	if err := app.DB().NewQuery(`SELECT [[user_id]] FROM {{leafen_app_connections}} WHERE [[install_id]] = {:install}
		AND [[tenancy_key]] = {:tenancy} ORDER BY [[user_id]]`).
		Bind(dbx.Params{"install": install.ID, "tenancy": workspace.TenancyKey}).Column(&used); err != nil {
		return err
	}
	connectionID := ""
	for _, userID := range used {
		if member[userID] || userID == workspace.AccountID {
			continue
		}
		back, err := mayHold(workspace.TenancyKey, userID)
		if err != nil {
			return err
		}
		if !back {
			continue
		}
		if err := ensureAppWorkspaceMember(app, workspace, userID); err != nil {
			return err
		}
		if connectionID == "" {
			_, manifest, err := loadAppPackage(app, install.PackageHash)
			if err != nil {
				return err
			}
			if connectionID, err = appAccountConnection(app, install, manifest, workspace); err != nil {
				return err
			}
		}
		if err := joinAppTeamspaces(app, workspace, connectionID, userID); err != nil {
			app.Logger().Warn("leafen app teamspace join failed", "install", install.ID, "error", err)
		}
	}
	return nil
}

// syncAllAppWorkspaceMembers is the hourly pass over every installed app.
func syncAllAppWorkspaceMembers(app core.App) error {
	installs, err := loadAppInstalls(app, "[[removed_at]] = 0", dbx.Params{})
	if err != nil {
		return err
	}
	failures := []error{}
	for _, install := range installs {
		if err := syncAppWorkspaceMembers(app, install.ID); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
