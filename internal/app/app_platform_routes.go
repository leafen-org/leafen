//go:build !leafen_noapps

package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// The server administrator's app routes: upload a package to review what
// it asks for, install or update it with that exact list approved, decide
// who may use it, stop it at once, or remove it. A certified build leaves
// the whole platform out (app_platform_routes_noapps.go).

const appPlatformAvailable = true

func registerAppPlatformRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+appPlatformListPath, listAppPlatformRoute)
	event.Router.POST(apiRoot+appPlatformPackageUploadPath, uploadAppPackageRoute).Bind(apis.BodyLimit(appPackageMaxBytes + 1))
	event.Router.GET(apiRoot+appPlatformPackageReadPath, readAppPackageRoute)
	event.Router.POST(apiRoot+appPlatformInstallPath, installAppRoute).Bind(apis.BodyLimit(appPlatformInputMaxBytes))
	event.Router.PATCH(apiRoot+appPlatformInstallStatusPath, setAppStatusRoute).Bind(apis.BodyLimit(appPlatformInputMaxBytes))
	// A grant names any number of people and groups; the body is bounded
	// only as a safety, at sixty-four times the other app requests.
	event.Router.PUT(apiRoot+appPlatformInstallGrantsPath, setAppGrantsRoute).Bind(apis.BodyLimit(64 * appPlatformInputMaxBytes))
	event.Router.GET(apiRoot+appPlatformTenancyPeoplePath, readAppTenancyPeopleRoute)
	event.Router.GET(apiRoot+appPlatformTenancyGroupsPath, readAppTenancyGroupsRoute)
	registerAppRunHistoryRoutes(event)
	event.Router.PUT(apiRoot+appPlatformOversightSetPath, setAppOversightRoute).Bind(apis.BodyLimit(appPlatformInputMaxBytes))
	event.Router.PUT(apiRoot+appPlatformStrictSetPath, setAppStrictRoute).Bind(apis.BodyLimit(appPlatformInputMaxBytes))
	event.Router.DELETE(apiRoot+appPlatformInstallRemovePath, removeAppRoute).Bind(apis.BodyLimit(appPlatformInputMaxBytes))
	event.Router.GET(apiRoot+appPlatformInstallRemovalPath, readAppRemovalRoute)
	event.Router.POST(apiRoot+appPlatformInstallRestorePath, restoreAppRoute)
	registerAppFrameRoutes(event)
	registerAppRelayRoutes(event)
	registerAppPersonScheduleRoutes(event)
	registerAppActionRoutes(event)
	registerAppRoomRoutes(event)
	registerAppGuestRoutes(event)
	registerConnectionAppRoutes(event)
	registerHTMLFrameRoutes(event)
	registerAppCatalogRoutes(event)
	registerAppUsageRoutes(event)
	registerAppDataReadsRoutes(event)
	registerAppHomeScreenRoutes(event)
	registerAppIconRoutes(event)
	registerAppStorageRoutes(event)
	registerAppInstallPolicyRoutes(event)
	registerAppAvailabilityRoutes(event)
	registerAppDataMapRoutes(event)
}

func listAppPlatformRoute(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	installs, err := loadAppInstalls(event.App, "", dbx.Params{})
	if err != nil {
		return appPlatformFailure(event, err)
	}
	response := appPlatformListResponse{Installs: []appInstallWire{}}
	for _, install := range installs {
		projected, err := projectAppInstall(event.App, install)
		if err != nil {
			return appPlatformFailure(event, err)
		}
		response.Installs = append(response.Installs, projected)
	}
	if response.Settings, err = loadAppPlatformSettings(event.App); err != nil {
		return appPlatformFailure(event, err)
	}
	if response.Allowlist, response.Requests, response.RequestsNextCursor, err = appPolicyState(event.App); err != nil {
		return appPlatformFailure(event, err)
	}
	if response.Tenancies, err = appTenancies(event.App); err != nil {
		return appPlatformFailure(event, err)
	}
	return event.JSON(http.StatusOK, response)
}

// readAppTenancyGroupsRoute lists the groups one organization may grant apps
// to, a window at a time by name from the cursor (GET /admin/apps/groups?tenancy=&cursor=).
func readAppTenancyGroupsRoute(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	query := event.Request.URL.Query()
	groups, next, err := appGrantGroupWindow(event.App, strings.TrimSpace(query.Get("tenancy")), query.Get("cursor"))
	if errors.Is(err, errAppGroupCursorInvalid) {
		return apiError(event, http.StatusBadRequest, errorCodeAppPlatformInputInvalid, "Start the list again.")
	}
	if err != nil {
		return appPlatformFailure(event, err)
	}
	return event.JSON(http.StatusOK, appGroupListWire{Groups: groups, NextCursor: next})
}

func uploadAppPackageRoute(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	user, _ := authenticatedUser(event)
	// The package goes to disk as it arrives, up to the server's limit, and
	// is read from there, its files spooled beside it.
	limit := appPackageLimit(event.App)
	spool, err := os.MkdirTemp("", "leafen-app-package-")
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAppPackageInvalid, "The package couldn't be kept.", err)
	}
	defer os.RemoveAll(spool)
	packagePath, files := filepath.Join(spool, "package.leafenapp"), filepath.Join(spool, "files")
	out, err := os.OpenFile(packagePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAppPackageInvalid, "The package couldn't be kept.", err)
	}
	written, copyErr := io.Copy(out, io.LimitReader(event.Request.Body, limit+1))
	if closeErr := out.Close(); copyErr == nil {
		copyErr = closeErr
	}
	if written > limit {
		return appPlatformFailure(event, appPackageFailure(errAppPackageTooLarge, fmt.Sprintf("a package is at most %d bytes on this server", limit)))
	}
	if copyErr != nil || os.Mkdir(files, 0o700) != nil {
		return apiError(event, http.StatusBadRequest, errorCodeAppPackageInvalid, "The package couldn't be read.")
	}
	pkg, err := readAppPackageFile(packagePath, files, limit)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	trust := appTrustUnsigned
	if pkg.Signed {
		trust = appTrustSigned
	}
	if err := appTrustAllowed(event.App, trust); err != nil {
		return appPlatformFailure(event, err)
	}
	if err := storeAppPackage(event.App, pkg, trust, user.Id); err != nil {
		return appPlatformFailure(event, err)
	}
	return respondAppPackageReview(event, pkg.Hash)
}

func readAppPackageRoute(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	return respondAppPackageReview(event, strings.TrimSpace(event.Request.PathValue("packageHash")))
}

func respondAppPackageReview(event *core.RequestEvent, hash string) error {
	stored, manifest, err := loadAppPackage(event.App, hash)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	review := appPackageReviewWire{
		PackageHash: stored.Hash, AppID: manifest.ID, Slug: manifest.Slug, Version: manifest.Version, IconURL: appPackageIcon(event.App, stored.Hash, manifest),
		Name: manifest.Name, Description: appTextOrEmpty(manifest.Description), Developer: appDeveloper(manifest),
		Trust: stored.Trust, Size: stored.Size, FileCount: stored.FileCount, Permissions: appPackagePermissions(event.App, stored.Hash, manifest),
		PermissionsHash: appPermissionsHash(manifest), NeedsLeafen: appNeedsLeafen(manifest), NameLooksLike: appNameLooksLike(manifest, stored.Trust),
	}
	// A version a trusted catalog revoked says so as soon as it is uploaded.
	revocations, err := appCatalogRevocations(event.App)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	if revoked, found := revocations[stored.Hash]; found {
		review.Revoked, review.RevokedReason = true, revoked.Reason
	}
	installed, err := loadAppInstalls(event.App, "[[app_id]] = {:appId}", dbx.Params{"appId": manifest.ID})
	if err != nil {
		return appPlatformFailure(event, err)
	}
	if len(installed) > 0 {
		_, current, err := loadAppPackage(event.App, installed[0].PackageHash)
		if err != nil {
			return appPlatformFailure(event, err)
		}
		// The installed version's lines, so the review marks what an update adds
		// and drops, and its fingerprint as this Leafen reckons it: one stored by
		// an older Leafen differed for the very same manifest (2026-10-16).
		// What installing it does says so: the very package installed changes
		// nothing, and the same version or an earlier one is no update.
		change := appVersionChange(current.Version, manifest.Version)
		if installed[0].PackageHash == stored.Hash {
			change = "unchanged"
		}
		review.Installed = &appInstalledRefWire{
			InstallID: installed[0].ID, Version: current.Version, PermissionsHash: appPermissionsHash(current),
			Permissions: appPackagePermissions(event.App, installed[0].PackageHash, current), Change: change,
		}
		if installed[0].PackageHash != stored.Hash {
			if review.DataChanges, err = appDataUpdateChanges(event.App, installed[0], stored.Hash, manifest, appReviewerLanguage(event)); err != nil {
				return appPlatformFailure(event, err)
			}
		}
	}
	return event.JSON(http.StatusOK, review)
}

// appInstallInputRefusal is why an install's body doesn't name a package
// and its approved permissions, or "".
func appInstallInputRefusal(decodeErr error, input appInstallInput) string {
	switch {
	case decodeErr != nil:
		return bodyRefusalReason("app install", decodeErr)
	case input.PackageHash == "":
		return "packageHash is empty: give the packageHash the package's upload answered"
	case input.AcceptPermissionsHash == "":
		return "acceptPermissionsHash is empty: give the permissionsHash of the package's review"
	}
	return ""
}

func installAppRoute(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	user, _ := authenticatedUser(event)
	input := appInstallInput{}
	if reason := appInstallInputRefusal(json.NewDecoder(event.Request.Body).Decode(&input), input); reason != "" {
		return apiError(event, http.StatusBadRequest, errorCodeAppPlatformInputInvalid,
			refusalMessage("Choose a package and approve its permissions.", reason))
	}
	// A version a trusted catalog revoked isn't installed, however it came.
	if revoked, err := appPackageRevoked(event.App, input.PackageHash); err != nil {
		return appPlatformFailure(event, err)
	} else if revoked {
		return apiError(event, http.StatusConflict, errorCodeAppRevoked, "That version was revoked by its catalog.")
	}
	// Whether the app is already here, before this install: an update.
	previous := false
	if _, manifest, err := loadAppPackage(event.App, input.PackageHash); err == nil {
		existing, err := loadAppInstalls(event.App, "[[app_id]] = {:appId}", dbx.Params{"appId": manifest.ID})
		previous = err == nil && len(existing) > 0
	}
	install, err := installAppPackage(event.App, input.PackageHash, input.AcceptPermissionsHash, user.Id, input.AcceptDataRemovals)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	// A new install's approval lets people give their account details, as
	// its review asked, unless the administrator turned that off there; an
	// update keeps what was chosen.
	if !previous {
		if _, manifest, err := loadAppPackage(event.App, input.PackageHash); err == nil && len(manifest.Credentials) > 0 {
			if err := setAppCredentialPolicy(event.App, install, input.PersonCredentials == nil || *input.PersonCredentials, user.Id); err != nil {
				return appPlatformFailure(event, err)
			}
		}
	}
	catchUpAppDataEverywhere(event.App, install)
	return respondAppInstall(event, install)
}

func setAppStatusRoute(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	user, _ := authenticatedUser(event)
	input := appStatusInput{}
	decoded := json.NewDecoder(event.Request.Body).Decode(&input)
	if reason := appStatusInputRefusal(decoded, input); reason != "" {
		return apiError(event, http.StatusBadRequest, errorCodeAppPlatformInputInvalid, refusalMessage("Choose whether the app runs.", reason))
	}
	// A key's call asks its person once the app's status could be set: it
	// isn't rehearsed, as a stopped app's cleanups run once it is
	// (user_key_acting_ask.go).
	if userKeyAsksWhereItActs(event) {
		if _, err := appInstallStatusRefusal(event.App, event.Request.PathValue("installId"), input.Status); err != nil {
			return appPlatformFailure(event, err)
		}
		if asked, err := askBeforeActing(event); asked {
			return err
		}
	}
	install, err := setAppInstallStatus(event.App, event.Request.PathValue("installId"), input.Status, user.Id)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	return respondAppInstall(event, install)
}

func setAppGrantsRoute(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	user, _ := authenticatedUser(event)
	input := appGrantsInput{}
	if err := json.NewDecoder(event.Request.Body).Decode(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeAppPlatformInputInvalid,
			refusalMessage("Choose who may use the app.", bodyRefusalReason("list of who may use the app", err)))
	}
	installID := event.Request.PathValue("installId")
	// A key's call asks its person once whom it names reads
	// (user_key_acting_ask.go).
	if userKeyAsksWhereItActs(event) {
		if err := appGrantsRefusal(event.App, installID, input.Grants); err != nil {
			return appPlatformFailure(event, err)
		}
		if asked, err := askBeforeActing(event); asked {
			return err
		}
	}
	if err := setAppGrants(event.App, installID, input.Grants, user.Id); err != nil {
		return appPlatformFailure(event, err)
	}
	install, err := loadAppInstall(event.App, installID)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	return respondAppInstall(event, install)
}

// readAppTenancyPeopleRoute lists, for the administrator choosing who may
// use an app, the people of an organization (?tenancy=org:…, or the
// server's own workspaces), found by name or email (?q=), 100 at a time.
func readAppTenancyPeopleRoute(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	tenancy := event.Request.URL.Query().Get("tenancy")
	organization, isOrganization := strings.CutPrefix(tenancy, "org:")
	if !isOrganization && tenancy != appTenancyServer {
		return apiError(event, http.StatusBadRequest, errorCodeAppPlatformInputInvalid,
			refusalMessage("Choose an organization.", fmt.Sprintf("tenancy is %q or org: and an organization's ID, not %q", appTenancyServer, tenancy)))
	}
	search := "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(strings.ToLower(strings.TrimSpace(event.Request.URL.Query().Get("q")))) + "%"
	rows := []appTenancyPersonWire{}
	// Members and owners of the organization's ordinary workspaces.
	query := strings.ReplaceAll(`SELECT u.[[id]] AS [[user_id]], u.[[name]] AS [[name]], u.[[email]] AS [[email]] FROM {{users}} u
		WHERE EXISTS (SELECT 1 FROM {{leafen_workspaces}} w
			LEFT JOIN {{leafen_workspace_memberships}} m ON m.[[workspace_id]] = w.[[id]] AND m.[[user_id]] = u.[[id]] AND m.[[role]] = 'member'
			WHERE w.[[organization]] = {:organization} AND w.[[deletion_job_id]] = ''
				AND w.[[id]] NOT IN (SELECT [[workspace_id]] FROM {{leafen_app_workspaces}})
				AND (__OWNER__ = u.[[id]] OR m.[[user_id]] IS NOT NULL))
			AND (LOWER(u.[[name]]) LIKE {:search} ESCAPE '\' OR LOWER(u.[[email]]) LIKE {:search} ESCAPE '\')
		ORDER BY u.[[name]], u.[[email]] LIMIT 101`, "__OWNER__", workspacePermissionOwnerSQL(event.App, "w"))
	if err := event.App.DB().NewQuery(query).Bind(dbx.Params{"organization": organization, "search": search}).All(&rows); err != nil {
		return appPlatformFailure(event, err)
	}
	answer := appTenancyPeopleWire{People: rows, More: len(rows) > 100}
	if answer.More {
		answer.People = rows[:100]
	}
	return event.JSON(http.StatusOK, answer)
}

func removeAppRoute(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	user, _ := authenticatedUser(event)
	// An app with data asks how to remove it; one without goes at once.
	input := appRemoveInput{}
	_ = json.NewDecoder(event.Request.Body).Decode(&input)
	if err := removeApp(event.App, event.Request.PathValue("installId"), input.Mode, user.Id); err != nil {
		return appPlatformFailure(event, err)
	}
	return event.NoContent(http.StatusNoContent)
}

func readAppRemovalRoute(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	preview, err := appRemovalPreview(event.App, event.Request.PathValue("installId"))
	if err != nil {
		return appPlatformFailure(event, err)
	}
	return event.JSON(http.StatusOK, preview)
}

func restoreAppRoute(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	user, _ := authenticatedUser(event)
	install, err := restoreApp(event.App, event.Request.PathValue("installId"), user.Id)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	return respondAppInstall(event, install)
}

// startAppPlatformUpkeep starts the app platform's work and deletes, every
// hour and once at start, what apps trashed more than thirty days ago left
// behind. The returned shutdown stops all of it.
func startAppPlatformUpkeep(app core.App) func(context.Context) error {
	background := newAppPlatformBackground(app)
	startAppWorkerExecutor(app, background)
	startAppUsageSaving(app, background)
	startAppLongRunner(app, background)
	background.run(func(ctx context.Context) {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			invalidator, _ := app.Store().Get(ygoRoomAuthorizationStoreKey).(*ygoRoomAuthorization)
			var rooms workspaceRoomInvalidator
			if invalidator != nil {
				rooms = invalidator
			}
			if err := purgeTrashedApps(app, rooms, time.Now()); err != nil {
				app.Logger().Warn("leafen app trash upkeep failed", "error", err)
			}
			if err := pruneAppRelayLog(app, time.Now()); err != nil {
				app.Logger().Warn("leafen app relay log upkeep failed", "error", err)
			}
			if err := pruneAppHookEvents(app, time.Now()); err != nil {
				app.Logger().Warn("leafen app hook upkeep failed", "error", err)
			}
			if err := expireAgentApprovals(app, time.Now()); err != nil {
				app.Logger().Warn("leafen agent approval upkeep failed", "error", err)
			}
			if err := refreshDueAppCatalogs(app, time.Now()); err != nil {
				app.Logger().Warn("leafen app catalog upkeep failed", "error", err)
			}
			if err := pruneAppDataReads(app, time.Now()); err != nil {
				app.Logger().Warn("leafen app data reads upkeep failed", "error", err)
			}
			if err := pruneAppApprovalAllowances(app, time.Now()); err != nil {
				app.Logger().Warn("leafen app allowance upkeep failed", "error", err)
			}
			if err := pruneAppGuestReports(app, time.Now()); err != nil {
				app.Logger().Warn("leafen app guest report upkeep failed", "error", err)
			}
			if err := syncAllAppWorkspaceMembers(app); err != nil {
				app.Logger().Warn("leafen app members upkeep failed", "error", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	})
	return background.Shutdown
}

func respondAppInstall(event *core.RequestEvent, install storedAppInstall) error {
	projected, err := projectAppInstall(event.App, install)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	return event.JSON(http.StatusOK, projected)
}

func projectAppInstall(app core.App, install storedAppInstall) (appInstallWire, error) {
	stored, manifest, err := loadAppPackage(app, install.PackageHash)
	if err != nil {
		return appInstallWire{}, err
	}
	grants, err := loadAppGrants(app, install.ID)
	if err != nil {
		return appInstallWire{}, err
	}
	wire := appInstallWire{
		InstallID: install.ID, AppID: install.AppID, Slug: install.Slug, Version: manifest.Version, Name: manifest.Name,
		Description: appTextOrEmpty(manifest.Description), Developer: appDeveloper(manifest), Trust: stored.Trust,
		Status: install.Status, Permissions: appPackagePermissions(app, install.PackageHash, manifest), PermissionsHash: install.PermissionsHash,
		PackageHash: install.PackageHash, Grants: grants, InstalledAt: install.InstalledAt, InstalledBy: install.InstalledBy,
		UpdatedAt: install.UpdatedAt, RemovedAt: install.RemovedAt, PurgeAt: appPurgeAt(install.RemovedAt),
		IconURL: appIconURL(app, install, manifest),
	}
	if install.Status == "revoked" {
		_ = app.DB().NewQuery(`SELECT [[revoked_reason]] FROM {{leafen_app_installs}} WHERE [[id]] = {:id}`).
			Bind(dbx.Params{"id": install.ID}).Row(&wire.RevokedReason)
	}
	if install.Status == "stopped" {
		_ = app.DB().NewQuery(`SELECT [[paused_reason]] FROM {{leafen_app_installs}} WHERE [[id]] = {:id}`).
			Bind(dbx.Params{"id": install.ID}).Row(&wire.PausedReason)
	}
	showAppDataFailures(app, install, &wire)
	return wire, nil
}

func appPurgeAt(removedAt int64) int64 {
	if removedAt == 0 {
		return 0
	}
	return time.UnixMilli(removedAt).Add(appTrashKeepDays * 24 * time.Hour).UnixMilli()
}

func appDeveloper(manifest appManifest) appDeveloperWire {
	return appDeveloperWire{Name: manifest.Developer.Name, URL: manifest.Developer.URL, KeyFingerprint: manifest.Developer.KeyFingerprint}
}

func appTextOrEmpty(text map[string]string) map[string]string {
	if text == nil {
		return map[string]string{}
	}
	return text
}

// appPlatformFailure answers with the code the failure carries; a package
// that can't be installed says why, a few reasons at a time.
func appPlatformFailure(event *core.RequestEvent, err error) error {
	packageFailure := &appPackageError{}
	if errors.As(err, &packageFailure) {
		problems := packageFailure.problems
		if len(problems) > 8 {
			problems = append(problems[:8:8], "…")
		}
		return apiError(event, http.StatusBadRequest, packageFailure.cause.Error(), strings.Join(problems, "\n"))
	}
	switch {
	case errors.Is(err, errAppPackageNotFound):
		return apiError(event, http.StatusNotFound, errorCodeAppPackageNotFound, "Package not found.")
	case errors.Is(err, errAppInstallNotFound):
		return apiError(event, http.StatusNotFound, errorCodeAppInstallNotFound, "App not found.")
	case errors.Is(err, errAppPermissionsChange):
		return apiError(event, http.StatusConflict, errorCodeAppPermissionsChanged, "The app's permissions changed; review them again.")
	case errors.Is(err, errAppDataRemovals):
		return apiError(event, http.StatusConflict, errorCodeAppDataRemovals, "The update removes the app's data; confirm that first.")
	case errors.Is(err, errAppUnsignedRefused):
		return apiError(event, http.StatusForbidden, errorCodeAppUnsignedRefused, "This server takes only signed apps.")
	case errors.Is(err, errAppInTrash):
		return apiError(event, http.StatusConflict, errorCodeAppInTrash, "This app is in the trash. Restore it, then update it.")
	case errors.Is(err, errAppSlugTaken):
		return apiError(event, http.StatusConflict, errorCodeAppSlugTaken, "Another app already uses this address.")
	case errors.Is(err, errAppNeedsNewerLeafen):
		return apiError(event, http.StatusConflict, errorCodeAppNeedsNewerLeafen, strings.TrimPrefix(err.Error(), errorCodeAppNeedsNewerLeafen+": "))
	case errors.Is(err, errAppLegalHold):
		return apiError(event, http.StatusPreconditionFailed, errorCodeAppLegalHold, "A legal hold keeps the app's data; the app is stopped.")
	case errors.Is(err, errAppInputInvalid):
		return apiError(event, http.StatusBadRequest, errorCodeAppPlatformInputInvalid, refusalMessage("Check the app settings.", refusalReason(err)))
	}
	return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeAppPlatformFailed, "Couldn't change the app.",
		"app.platform", "response", errorCodeAppPlatformFailed, err)
}
