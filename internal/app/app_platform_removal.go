package app

import (
	"errors"
	"fmt"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Taking an app off the server (docs/app-platform.md 6): an administrator
// first sees what goes with it — each organization's app workspace with its
// pages, databases and people — then either keeps that data as ordinary
// workspaces or moves the app and its data to the trash. A trashed app is
// stopped, its people leave its workspaces, and it can be restored for
// thirty days, its people back in; then its account deletes the workspaces
// for good. A legal
// hold keeps the data: the app only stops.

const (
	appTrashKeepDays = 30
	appRemoveTrash   = "trash"
	appRemoveKeep    = "keep"

	appPurgeSweepBudget = 30 * time.Second
)

var errAppLegalHold = errors.New(errorCodeAppLegalHold)

// appRemovalPreview counts what goes with the app in each organization: its
// pages and databases but the server's own People database and its people's
// pages (workspaceArchiveTargetPageSQL), which every workspace has.
func appRemovalPreview(app core.App, installID string) (appRemovalPreviewWire, error) {
	preview := appRemovalPreviewWire{Workspaces: []appRemovalWorkspaceWire{}, KeepDays: appTrashKeepDays}
	if _, err := loadAppInstall(app, installID); err != nil {
		return preview, err
	}
	workspaces, err := loadAppWorkspaces(app, installID)
	if err != nil {
		return preview, err
	}
	tenancies, err := appTenancies(app)
	if err != nil {
		return preview, err
	}
	names := map[string]string{}
	for _, tenancy := range tenancies {
		names[tenancy.Key] = tenancy.Name
	}
	for _, workspace := range workspaces {
		row := appRemovalWorkspaceWire{WorkspaceID: workspace.WorkspaceID, TenancyName: names[workspace.TenancyKey]}
		if err := app.DB().NewQuery(`SELECT
				(SELECT COUNT(*) FROM {{leafen_pages}} AS [[page]] WHERE [[page]].[[workspace]] = {:workspace} AND [[page]].[[in_trash]] = 0
					AND `+workspaceArchiveTargetPageSQL("[[page]]")+`),
				(SELECT COUNT(*) FROM {{leafen_data_sources}} WHERE [[workspace_id]] = {:workspace}
					AND [[id]] NOT IN (SELECT [[data_source_id]] FROM {{leafen_workspace_people_databases}} WHERE [[workspace_id]] = {:workspace})),
				(SELECT COUNT(*) FROM {{leafen_workspace_memberships}} WHERE [[workspace_id]] = {:workspace} AND [[user_id]] != {:account})`).
			Bind(dbx.Params{"workspace": workspace.WorkspaceID, "account": workspace.AccountID}).
			Row(&row.Pages, &row.Databases, &row.People); err != nil {
			return preview, err
		}
		organizationID := appTenancyOrganization(workspace.TenancyKey)
		if organizationID != "" {
			hold := assertNoActiveLegalHoldForPermanentDelete(app, organizationID,
				legalHoldDeletionTarget{WorkspaceID: workspace.WorkspaceID, EntireWorkspace: true})
			row.LegalHold = errors.Is(hold, errLegalHoldPreventsPermanentDelete)
		}
		preview.LegalHold = preview.LegalHold || row.LegalHold
		preview.Workspaces = append(preview.Workspaces, row)
	}
	return preview, nil
}

// removeApp takes the app off: right away when it has no workspaces, or as
// the administrator chose — its data kept as ordinary workspaces, or the
// app and its data to the trash, unless a legal hold keeps the data.
func removeApp(app core.App, installID, mode, actorID string) error {
	install, err := loadAppInstall(app, installID)
	if err != nil {
		return err
	}
	workspaces, err := loadAppWorkspaces(app, installID)
	if err != nil {
		return err
	}
	if len(workspaces) == 0 {
		return removeAppInstall(app, installID, actorID)
	}
	switch mode {
	case appRemoveKeep:
		return app.RunInTransaction(func(tx core.App) error {
			for _, workspace := range workspaces {
				record, err := tx.FindRecordById(workspacesCollection, workspace.WorkspaceID)
				if err != nil {
					return err
				}
				// An ordinary workspace has a person for its owner: the organization's.
				record.Set("app_install_id", "")
				record.Set("workspace_owner", record.GetString("owner"))
				if err := tx.Save(record); err != nil {
					return err
				}
			}
			if err := retireAppAccounts(tx, workspaces); err != nil {
				return err
			}
			if _, err := tx.DB().Delete(appWorkspacesTable, dbx.HashExp{"install_id": installID}).Execute(); err != nil {
				return err
			}
			return removeAppInstall(tx, installID, actorID)
		})
	case appRemoveTrash:
		preview, err := appRemovalPreview(app, installID)
		if err != nil {
			return err
		}
		if preview.LegalHold {
			if _, err := setAppInstallStatus(app, installID, "stopped", actorID); err != nil {
				return err
			}
			return errAppLegalHold
		}
		if err := app.RunInTransaction(func(tx core.App) error {
			if _, err := tx.DB().Update(appInstallsTable, dbx.Params{
				"status": "stopped", "removed_at": time.Now().UnixMilli(), "removed_by": actorID,
				"updated_by": actorID, "updated_at": time.Now().UnixMilli(),
			}, dbx.HashExp{"id": installID}).Execute(); err != nil {
				return err
			}
			return recordAppAudit(tx, install, "trash", actorID, map[string]any{})
		}); err != nil {
			return err
		}
		for _, cleanup := range appStopCleanups {
			cleanup(installID)
		}
		// Its people leave its workspaces; restoring lets them back in at once.
		for _, workspace := range workspaces {
			members := []string{}
			if err := app.DB().NewQuery(`SELECT [[user_id]] FROM {{leafen_workspace_memberships}}
				WHERE [[workspace_id]] = {:workspace} AND [[user_id]] != {:account}`).
				Bind(dbx.Params{"workspace": workspace.WorkspaceID, "account": workspace.AccountID}).Column(&members); err != nil {
				return err
			}
			for _, userID := range members {
				if err := app.RunInTransaction(func(tx core.App) error {
					_, err := revokeWorkspaceMembershipInTransaction(tx, workspace.AccountID, workspace.WorkspaceID, userID)
					return err
				}); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return refuse(errAppInputInvalid, "mode is %s or %s, not %q: the app has data in %d workspaces", appRemoveKeep, appRemoveTrash, mode, len(workspaces))
}

// restoreApp brings a trashed app back, stopped, within its thirty days,
// with the people who used it and may still back in its workspaces.
func restoreApp(app core.App, installID, actorID string) (storedAppInstall, error) {
	var result storedAppInstall
	err := app.RunInTransaction(func(tx core.App) error {
		var removedAt int64
		if err := tx.DB().NewQuery(`SELECT [[removed_at]] FROM {{leafen_app_installs}} WHERE [[id]] = {:id}`).
			Bind(dbx.Params{"id": installID}).Row(&removedAt); err != nil {
			return refusedUnlessStorage(errAppInstallNotFound, err)
		}
		if removedAt == 0 {
			return refuse(errAppInputInvalid, "the app isn't in Trash")
		}
		if time.Since(time.UnixMilli(removedAt)) > appTrashKeepDays*24*time.Hour {
			return refuse(errAppInputInvalid, "the app was in Trash over %d days, so it can't be restored", appTrashKeepDays)
		}
		if _, err := tx.DB().Update(appInstallsTable, dbx.Params{
			"removed_at": 0, "removed_by": "", "updated_by": actorID, "updated_at": time.Now().UnixMilli(),
		}, dbx.HashExp{"id": installID}).Execute(); err != nil {
			return err
		}
		install, err := loadAppInstall(tx, installID)
		if err != nil {
			return err
		}
		result = install
		return recordAppAudit(tx, install, "restore", actorID, map[string]any{})
	})
	if err == nil {
		mcpAppsChanged()
		// One that fails is let back in by the hourly pass, or at their opening.
		if err := syncAppWorkspaceMembers(app, installID); err != nil {
			app.Logger().Warn("leafen app restore members failed", "install", installID, "error", err.Error())
		}
	}
	return result, err
}

// purgeTrashedApps deletes, as each app's account, the workspaces of apps
// trashed more than thirty days ago, then the apps themselves. A deletion
// that stopped partway resumes; one app failing doesn't hold back the rest.
func purgeTrashedApps(app core.App, invalidator workspaceRoomInvalidator, now time.Time) error {
	installs := []string{}
	if err := app.DB().NewQuery(`SELECT [[id]] FROM {{leafen_app_installs}} WHERE [[removed_at]] > 0 AND [[removed_at]] < {:before}`).
		Bind(dbx.Params{"before": now.Add(-appTrashKeepDays * 24 * time.Hour).UnixMilli()}).Column(&installs); err != nil {
		return err
	}
	failures := []error{}
	for _, installID := range installs {
		if err := purgeTrashedApp(app, invalidator, installID); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func purgeTrashedApp(app core.App, invalidator workspaceRoomInvalidator, installID string) error {
	workspaces, err := loadAppWorkspaces(app, installID)
	if err != nil {
		return err
	}
	unfinished := false
	for _, workspace := range workspaces {
		record, err := app.FindRecordById(workspacesCollection, workspace.WorkspaceID)
		if err != nil {
			if err := retireAppAccounts(app, []storedAppWorkspace{workspace}); err != nil {
				return err
			}
			continue
		}
		requestID := "leafen-app-trash:" + workspace.WorkspaceID
		if earlier, err := loadWorkspaceDeletionByTarget(app, workspace.AccountID, workspace.WorkspaceID); err == nil &&
			earlier.Status != workspaceDeletionStatusCompleted {
			requestID = earlier.RequestID
		}
		release := acquireProductOperationLock("permanent-workspace-delete", workspace.WorkspaceID)
		stage := "admit"
		admission, err := admitWorkspaceDeletion(app, workspace.AccountID, workspace.WorkspaceID, requestID, record.GetString("name"))
		if err == nil && admission.Job.Status != workspaceDeletionStatusCompleted && invalidator != nil {
			stage = "invalidate"
			err = invalidator.InvalidateWorkspace(admission.Job.WorkspaceID)
		}
		// Each call deletes a few pages; a sweep keeps at it for a while.
		for started := time.Now(); err == nil && admission.Job.Status != workspaceDeletionStatusCompleted &&
			time.Since(started) < appPurgeSweepBudget; {
			stage = "advance"
			admission.Job, err = advanceWorkspaceDeletion(app, admission.Job.ID)
		}
		release()
		if err == nil && admission.Job.Status == workspaceDeletionStatusCompleted {
			// The workspace's map goes with it, so its account retires now.
			stage = "retire"
			err = retireAppAccounts(app, []storedAppWorkspace{workspace})
		}
		if err != nil {
			return fmt.Errorf("app %s workspace %s %s: %w", installID, workspace.WorkspaceID, stage, err)
		}
		unfinished = unfinished || admission.Job.Status != workspaceDeletionStatusCompleted
	}
	// A large workspace takes more than one sweep; the app goes once its workspaces have.
	if unfinished {
		return nil
	}
	return removeAppInstall(app, installID, "leafen-app-trash")
}

// retireAppAccounts ends the accounts of an app's workspaces with the app:
// they leave their organizations and their connections end, so nothing
// counts or lists them as people.
func retireAppAccounts(app core.App, workspaces []storedAppWorkspace) error {
	return app.RunInTransaction(func(tx core.App) error {
		now := time.Now().UnixMilli()
		for _, workspace := range workspaces {
			memberships, err := tx.FindRecordsByFilter(organizationMembershipsCollection, "user = {:user}", "", 0, 0,
				dbx.Params{"user": workspace.AccountID})
			if err != nil {
				return err
			}
			for _, membership := range memberships {
				if err := tx.Delete(membership); err != nil {
					return err
				}
			}
			if _, err := tx.DB().NewQuery(`UPDATE {{leafen_mcp_connections}} SET [[revoked_at]] = {:now}, [[retire_after]] = {:retireAfter}
				WHERE [[owner_id]] = {:user} AND [[revoked_at]] = 0`).Bind(dbx.Params{
				"now": now, "retireAfter": now + int64(mcpCredentialRetentionMilliseconds), "user": workspace.AccountID,
			}).Execute(); err != nil {
				return err
			}
			if _, err := tx.DB().NewQuery(`UPDATE {{leafen_integration_accounts}} SET [[removed_at]] = {:now}
				WHERE [[user_id]] = {:user} AND [[removed_at]] = 0`).Bind(dbx.Params{"now": now, "user": workspace.AccountID}).Execute(); err != nil {
				return err
			}
		}
		notifyMCPCredentialRegistryAfterCommit(tx)
		return nil
	})
}
