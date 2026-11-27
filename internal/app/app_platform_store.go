package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// What the server keeps of its apps and how an administrator changes it:
// a package is stored once by its fingerprint, an install points at one,
// and a version whose permissions differ from the approved ones installs
// only when the administrator approved that exact list.

const appTenancyServer = "server"

var (
	errAppPackageNotFound   = errors.New(errorCodeAppPackageNotFound)
	errAppInstallNotFound   = errors.New(errorCodeAppInstallNotFound)
	errAppPermissionsChange = errors.New(errorCodeAppPermissionsChanged)
	errAppSlugTaken         = errors.New(errorCodeAppSlugTaken)
	errAppInTrash           = errors.New(errorCodeAppInTrash)
	errAppInTrashForPerson  = errors.New(errorCodeAppInTrashAskAdministrator)
	errAppUnsignedRefused   = errors.New(errorCodeAppUnsignedRefused)
	errAppNeedsNewerLeafen  = errors.New(errorCodeAppNeedsNewerLeafen)
	errAppInputInvalid      = errors.New(errorCodeAppPlatformInputInvalid)
)

type storedAppPackage struct {
	Hash           string `db:"hash"`
	AppID          string `db:"app_id"`
	Version        string `db:"version"`
	ManifestJSON   string `db:"manifest_json"`
	Size           int64  `db:"size"`
	FileCount      int    `db:"file_count"`
	Trust          string `db:"trust"`
	KeyFingerprint string `db:"key_fingerprint"`
}

type storedAppInstall struct {
	ID              string `db:"id"`
	AppID           string `db:"app_id"`
	Slug            string `db:"slug"`
	PackageHash     string `db:"package_hash"`
	PermissionsHash string `db:"permissions_hash"`
	Status          string `db:"status"`
	InstalledBy     string `db:"installed_by"`
	InstalledAt     int64  `db:"installed_at"`
	UpdatedAt       int64  `db:"updated_at"`
	RemovedAt       int64  `db:"removed_at"`
}

// storeAppPackage keeps a read package and its files unless the same
// fingerprint is already kept: the files in the file storage first, then
// the package's rows.
func storeAppPackage(app core.App, pkg appPackage, trust, actorID string) error {
	var kept int
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_app_packages}} WHERE [[hash]] = {:hash}`).
		Bind(dbx.Params{"hash": pkg.Hash}).Row(&kept); err != nil || kept > 0 {
		return err
	}
	if err := keepAppPackageFiles(app, pkg.Files); err != nil {
		return err
	}
	return app.RunInTransaction(func(tx core.App) error {
		var count int
		if err := tx.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_app_packages}} WHERE [[hash]] = {:hash}`).
			Bind(dbx.Params{"hash": pkg.Hash}).Row(&count); err != nil || count > 0 {
			return err
		}
		if _, err := tx.DB().Insert(appPackagesTable, dbx.Params{
			"hash": pkg.Hash, "app_id": pkg.Manifest.ID, "version": pkg.Manifest.Version, "manifest_json": string(pkg.ManifestJSON),
			"size": pkg.Size, "file_count": len(pkg.Files), "trust": trust, "key_fingerprint": pkg.KeyFingerprint,
			"uploaded_by": actorID, "created": time.Now().UnixMilli(),
		}).Execute(); err != nil {
			return err
		}
		for _, file := range pkg.Files {
			if _, err := tx.DB().Insert(appPackageFilesTable, dbx.Params{
				"package_hash": pkg.Hash, "path": file.Path, "size": file.Size, "sha256": file.SHA256,
				"content_type": file.ContentType, "data": []byte{},
			}).Execute(); err != nil {
				return err
			}
		}
		return nil
	})
}

// appInstallBuiltin says whether an install is one of the apps Leafen ships:
// screens say "리펜 제공" for it, as its card does.
func appInstallBuiltin(app core.App, install storedAppInstall) bool {
	var trust string
	_ = app.DB().NewQuery(`SELECT [[trust]] FROM {{leafen_app_packages}} WHERE [[hash]] = {:hash}`).
		Bind(dbx.Params{"hash": install.PackageHash}).Row(&trust)
	return trust == appTrustBuiltin
}

func loadAppPackage(app core.App, hash string) (storedAppPackage, appManifest, error) {
	stored := storedAppPackage{}
	err := app.DB().NewQuery(`SELECT [[hash]], [[app_id]], [[version]], [[manifest_json]], [[size]], [[file_count]], [[trust]],
		[[key_fingerprint]] FROM {{leafen_app_packages}} WHERE [[hash]] = {:hash}`).Bind(dbx.Params{"hash": hash}).One(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return stored, appManifest{}, errAppPackageNotFound
	}
	if err != nil {
		return stored, appManifest{}, err
	}
	manifest := appManifest{}
	err = json.Unmarshal([]byte(stored.ManifestJSON), &manifest)
	return stored, manifest, err
}

func loadAppInstalls(app core.App, where string, params dbx.Params) ([]storedAppInstall, error) {
	installs := []storedAppInstall{}
	query := `SELECT [[id]], [[app_id]], [[slug]], [[package_hash]], [[permissions_hash]], [[status]], [[installed_by]],
		[[installed_at]], [[updated_at]], [[removed_at]] FROM {{leafen_app_installs}}`
	if where != "" {
		query += " WHERE " + where
	}
	err := app.DB().NewQuery(query + ` ORDER BY [[installed_at]], [[id]]`).Bind(params).All(&installs)
	return installs, err
}

func loadAppInstall(app core.App, installID string) (storedAppInstall, error) {
	installs, err := loadAppInstalls(app, "[[id]] = {:id}", dbx.Params{"id": installID})
	if err != nil {
		return storedAppInstall{}, err
	}
	if len(installs) == 0 {
		return storedAppInstall{}, errAppInstallNotFound
	}
	return installs[0], nil
}

// appNeedsLeafen is the manifest's minLeafen when this server is older; a
// prerelease of Leafen counts as its release, so a development build runs
// the apps its release will.
func appNeedsLeafen(manifest appManifest) string {
	needed, neededOK := parseReleaseVersion(manifest.MinLeafen)
	running, runningOK := parseReleaseVersion(leafenReleaseVersion)
	if !neededOK || !runningOK {
		return ""
	}
	needed.prerelease, running.prerelease = nil, nil
	if compareReleaseVersions(running, needed) < 0 {
		return manifest.MinLeafen
	}
	return ""
}

// appVersionChange is what installing version next does to an installed
// app at version installed, as its review and its catalog card say it:
// "update" to a later version, "downgrade" to an earlier one, "reinstall"
// for the same version (2026-11-12: the same version was called an update).
// Versions that don't read as release versions differ as an update.
func appVersionChange(installed, next string) string {
	if installed == next {
		return "reinstall"
	}
	before, beforeOK := parseReleaseVersion(installed)
	after, afterOK := parseReleaseVersion(next)
	if !beforeOK || !afterOK {
		return "update"
	}
	switch order := compareReleaseVersions(after, before); {
	case order < 0:
		return "downgrade"
	case order == 0:
		return "reinstall"
	}
	return "update"
}

// installAppPackage installs a kept package or moves an installed app to
// it. The administrator must have approved the package's permission list
// by its hash, and the app's address name must be free.
// installAppPackage installs a package or updates the installed version to
// it; an update that removes the app's data needs acceptRemovals.
func installAppPackage(app core.App, packageHash, acceptHash, actorID string, acceptRemovals bool) (storedAppInstall, error) {
	var result storedAppInstall
	err := app.RunInTransaction(func(tx core.App) error {
		stored, manifest, err := loadAppPackage(tx, packageHash)
		if err != nil {
			return err
		}
		if err := appTrustAllowed(tx, stored.Trust); err != nil {
			return err
		}
		permissions := appPermissionsHash(manifest)
		if acceptHash != permissions {
			return errAppPermissionsChange
		}
		if needs := appNeedsLeafen(manifest); needs != "" {
			return fmt.Errorf("%w: the app needs Leafen %s or later; this server runs %s", errAppNeedsNewerLeafen, needs, leafenReleaseVersion)
		}
		now := time.Now().UnixMilli()
		existing, err := loadAppInstalls(tx, "[[app_id]] = {:appId}", dbx.Params{"appId": manifest.ID})
		if err != nil {
			return err
		}
		var taken int
		if err := tx.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_app_installs}} WHERE [[slug]] = {:slug} AND [[app_id]] != {:appId}`).
			Bind(dbx.Params{"slug": manifest.Slug, "appId": manifest.ID}).Row(&taken); err != nil {
			return err
		}
		if taken > 0 {
			return errAppSlugTaken
		}
		action := "install"
		if len(existing) == 0 {
			result = storedAppInstall{
				ID: security.RandomString(15), AppID: manifest.ID, Slug: manifest.Slug, PackageHash: packageHash,
				PermissionsHash: permissions, Status: "active", InstalledBy: actorID, InstalledAt: now, UpdatedAt: now,
			}
			_, err = tx.DB().Insert(appInstallsTable, dbx.Params{
				"id": result.ID, "app_id": result.AppID, "slug": result.Slug, "package_hash": packageHash,
				"permissions_hash": permissions, "status": "active", "installed_by": actorID, "installed_at": now,
				"updated_by": actorID, "updated_at": now,
			}).Execute()
		} else {
			result = existing[0]
			// Nothing would change for its people, and the trash would still take it.
			if result.RemovedAt > 0 {
				return errAppInTrash
			}
			action = "update"
			accepted := ""
			if result.PackageHash != packageHash {
				changes, err := appDataUpdateChanges(tx, result, packageHash, manifest, "")
				if err != nil {
					return err
				}
				if appDataChangesDestructive(changes) {
					if !acceptRemovals {
						return errAppDataRemovals
					}
					accepted = packageHash
				}
			}
			result.Slug, result.PackageHash, result.PermissionsHash, result.UpdatedAt = manifest.Slug, packageHash, permissions, now
			changes := dbx.Params{
				"slug": manifest.Slug, "package_hash": packageHash, "permissions_hash": permissions, "updated_by": actorID, "updated_at": now,
				"data_removals_accepted": accepted,
			}
			// Another version ends a revocation, and the app runs again; the
			// route has refused a version that is revoked itself.
			if result.Status == "revoked" {
				result.Status, changes["status"], changes["revoked_reason"] = "active", "active", ""
			}
			_, err = tx.DB().Update(appInstallsTable, changes, dbx.HashExp{"id": result.ID}).Execute()
		}
		if err != nil {
			return err
		}
		return recordAppAudit(tx, result, action, actorID, map[string]any{"version": manifest.Version, "packageHash": packageHash})
	})
	if err == nil {
		mcpAppsChanged()
	}
	return result, err
}

func setAppInstallStatus(app core.App, installID, status, actorID string) (storedAppInstall, error) {
	return setAppInstallStatusWith(app, installID, status, actorID, nil)
}

// appInstallStatusRefusal is what setting an app's status refuses, read
// before it is set: a status that isn't one, an app that isn't installed, a
// revoked version — it stays stopped until another version replaces it —
// and a trashed app made to run before it is restored.
func appInstallStatusRefusal(app core.App, installID, status string) (storedAppInstall, error) {
	if status != "active" && status != "stopped" && status != "emergency" {
		return storedAppInstall{}, refuse(errAppInputInvalid, "status is active, stopped or emergency, not %q", status)
	}
	install, err := loadAppInstall(app, installID)
	if err != nil {
		return install, err
	}
	if install.Status == "revoked" {
		return install, refuse(errAppInputInvalid, "this version was revoked: it stays stopped until another version replaces it")
	}
	if install.RemovedAt > 0 && status == "active" {
		return install, refuse(errAppInputInvalid, "the app is in Trash: restore it first")
	}
	return install, nil
}

// setAppInstallStatusWith sets an app's status and does `also` in the same
// transaction, before the stopped app's cleanups.
func setAppInstallStatusWith(
	app core.App, installID, status, actorID string, also func(tx core.App, install storedAppInstall) error,
) (storedAppInstall, error) {
	var result storedAppInstall
	err := app.RunInTransaction(func(tx core.App) error {
		install, err := appInstallStatusRefusal(tx, installID, status)
		if err != nil {
			return err
		}
		install.Status, install.UpdatedAt = status, time.Now().UnixMilli()
		changes := dbx.Params{"status": status, "updated_by": actorID, "updated_at": install.UpdatedAt}
		if status == "active" {
			changes["paused_reason"] = ""
		}
		if _, err := tx.DB().Update(appInstallsTable, changes, dbx.HashExp{"id": installID}).Execute(); err != nil {
			return err
		}
		result = install
		if err := recordAppAudit(tx, install, "status", actorID, map[string]any{"status": status}); err != nil {
			return err
		}
		if also != nil {
			return also(tx, install)
		}
		return nil
	})
	if err == nil && status == "emergency" {
		for _, cleanup := range appEmergencyCleanups {
			cleanup(installID)
		}
	}
	if err == nil && status != "active" {
		for _, cleanup := range appStopCleanups {
			cleanup(installID)
		}
	}
	if err == nil {
		mcpAppsChanged()
	}
	return result, err
}

// setAppGrants replaces who may use the app in the tenancies named; a
// tenancy left out keeps its grants.
func setAppGrants(app core.App, installID string, grants []appGrantWire, actorID string) error {
	if err := appGrantsRefusal(app, installID, grants); err != nil {
		return err
	}
	if err := app.RunInTransaction(func(tx core.App) error {
		install, err := loadAppInstall(tx, installID)
		if err != nil {
			return err
		}
		now := time.Now().UnixMilli()
		for _, grant := range grants {
			if _, err := tx.DB().Delete(appGrantsTable, dbx.HashExp{"install_id": installID, "tenancy_key": grant.TenancyKey}).Execute(); err != nil {
				return err
			}
			subjects := map[string]bool{}
			if grant.Everyone {
				subjects["*"] = true
			}
			for _, userID := range grant.UserIDs {
				if userID = strings.TrimSpace(userID); userID != "" && userID != "*" && !strings.HasPrefix(userID, appGrantGroupPrefix) {
					subjects[userID] = true
				}
			}
			for _, groupID := range grant.GroupIDs {
				subjects[appGrantGroupPrefix+strings.TrimSpace(groupID)] = true
			}
			for subject := range subjects {
				if _, err := tx.DB().Insert(appGrantsTable, dbx.Params{
					"install_id": installID, "tenancy_key": grant.TenancyKey, "subject": subject, "created_by": actorID, "created": now,
				}).Execute(); err != nil {
					return err
				}
			}
		}
		return recordAppAudit(tx, install, "grants", actorID, map[string]any{"grants": grants})
	}); err != nil {
		return err
	}
	defer mcpAppsChanged()
	return syncAppWorkspaceMembers(app, installID)
}

// appGrantsRefusal is what setting who may use an app refuses, read before
// it is set: an app that isn't installed, a tenancy that isn't the server's
// or one of its organizations, a group that isn't the tenancy's.
func appGrantsRefusal(app core.App, installID string, grants []appGrantWire) error {
	if _, err := loadAppInstall(app, installID); err != nil {
		return err
	}
	tenancies, err := appTenancies(app)
	if err != nil {
		return err
	}
	for position, grant := range grants {
		if refused := appTenancyRefusal(fmt.Sprintf("grants[%d].tenancyKey", position), grant.TenancyKey, tenancies); refused != nil {
			return refused
		}
		if valid, err := validAppGrantGroups(app, grant.TenancyKey, grant.GroupIDs); err != nil {
			return err
		} else if !valid && appTenancyOrganization(grant.TenancyKey) == "" {
			return refuse(errAppInputInvalid, "grants[%d].groupIds goes only with an organization: the server's own people have no groups", position)
		} else if !valid {
			return refuse(errAppInputInvalid, "grants[%d].groupIds names a group that isn't one of the organization's", position)
		}
	}
	return nil
}

func loadAppGrants(app core.App, installID string) ([]appGrantWire, error) {
	rows := []struct {
		TenancyKey string `db:"tenancy_key"`
		Subject    string `db:"subject"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[tenancy_key]], [[subject]] FROM {{leafen_app_grants}} WHERE [[install_id]] = {:id}
		ORDER BY [[tenancy_key]], [[subject]]`).Bind(dbx.Params{"id": installID}).All(&rows); err != nil {
		return nil, err
	}
	byTenancy := map[string]*appGrantWire{}
	keys := []string{}
	for _, row := range rows {
		grant := byTenancy[row.TenancyKey]
		if grant == nil {
			grant = &appGrantWire{TenancyKey: row.TenancyKey, UserIDs: []string{}}
			byTenancy[row.TenancyKey] = grant
			keys = append(keys, row.TenancyKey)
		}
		if row.Subject == "*" {
			grant.Everyone = true
		} else if group, isGroup := strings.CutPrefix(row.Subject, appGrantGroupPrefix); isGroup {
			grant.GroupIDs = append(grant.GroupIDs, group)
		} else {
			grant.UserIDs = append(grant.UserIDs, row.Subject)
		}
	}
	grants := make([]appGrantWire, 0, len(keys))
	for _, key := range keys {
		grants = append(grants, *byTenancy[key])
	}
	return grants, nil
}

// removeAppInstall takes an app off the server; its packages stay until
// nothing points at them.
// appRemovalCleanups let go of what an app keeps outside the database, and
// appStopCleanups end what it has running when it stops; an emergency stop
// first ends its running calls at once (appEmergencyCleanups).
var appRemovalCleanups, appStopCleanups, appEmergencyCleanups []func(installID string)

func removeAppInstall(app core.App, installID, actorID string) error {
	defer func() {
		for _, cleanup := range appRemovalCleanups {
			cleanup(installID)
		}
		mcpAppsChanged()
	}()
	return app.RunInTransaction(func(tx core.App) error {
		install, err := loadAppInstall(tx, installID)
		if err != nil {
			return err
		}
		// What the app knew of its workspaces goes with it; kept workspaces are ordinary ones by now.
		for _, table := range []string{appDataTable, appWorkspacesTable, appInstallsTable} {
			column := "install_id"
			if table == appInstallsTable {
				column = "id"
			}
			if _, err := tx.DB().Delete(table, dbx.HashExp{column: installID}).Execute(); err != nil {
				return err
			}
		}
		return recordAppAudit(tx, install, "remove", actorID, map[string]any{})
	})
}

// appTenancies lists where an app's data and people live apart: each
// organization, then the server's workspaces outside any.
func appTenancies(app core.App) ([]appTenancyWire, error) {
	rows := []struct {
		ID   string `db:"id"`
		Name string `db:"name"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[id]], [[name]] FROM {{leafen_organizations}} ORDER BY [[name]], [[id]]`).All(&rows); err != nil {
		return nil, err
	}
	tenancies := []appTenancyWire{}
	for _, row := range rows {
		tenancies = append(tenancies, appTenancyWire{Key: "org:" + row.ID, Name: row.Name})
	}
	sort.SliceStable(tenancies, func(left, right int) bool { return tenancies[left].Name < tenancies[right].Name })
	// Workspaces made before organizations, if any are left, share one.
	var outside int
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_workspaces}} WHERE [[organization]] = ''`).Row(&outside); err != nil {
		return nil, err
	}
	if outside > 0 {
		tenancies = append(tenancies, appTenancyWire{Key: appTenancyServer, Name: ""})
	}
	for index := range tenancies {
		tenancies[index].Overseen = appOverseen(app, tenancies[index].Key)
		tenancies[index].Strict = appTenancyStrict(app, tenancies[index].Key)
	}
	return tenancies, nil
}

// appTenancyNames names each organization by its key.
func appTenancyNames(app core.App) map[string]string {
	names := map[string]string{}
	if tenancies, err := appTenancies(app); err == nil {
		for _, tenancy := range tenancies {
			names[tenancy.Key] = tenancy.Name
		}
	}
	return names
}

func recordAppAudit(app core.App, install storedAppInstall, action, actorID string, detail map[string]any) error {
	encoded, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	_, err = app.DB().Insert(appAuditEventsTable, dbx.Params{
		"install_id": install.ID, "app_id": install.AppID, "action": action, "actor_id": actorID,
		"detail_json": string(encoded), "created": time.Now().UnixMilli(),
	}).Execute()
	return err
}

// appTrustAllowed refuses an app nobody signed while administrators allow
// only signed apps.
func appTrustAllowed(app core.App, trust string) error {
	if trust != appTrustUnsigned {
		return nil
	}
	settings, err := loadAppPlatformSettings(app)
	if err != nil {
		return err
	}
	if settings.UnsignedAppsOff {
		return errAppUnsignedRefused
	}
	return nil
}
