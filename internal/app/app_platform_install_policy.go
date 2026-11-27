//go:build !leafen_noapps

package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// How people come to apps (docs/app-platform.md 3). Installs are the
// server's and administrators are its administrators; the install policy
// says what everyone else may do: nothing ("admins", the default), ask for
// an app the administrators then install or decline ("request"), or add
// themselves an app the administrators allowed ("allowlist") — after their
// own consent to what it may do, asked again when that changes.

var (
	errAppPolicyRefused  = errors.New(errorCodeAppPolicyRefused)
	errAppRequestMissing = errors.New(errorCodeAppRequestNotFound)
)

const appRequestReasonRunes = 300

func registerAppInstallPolicyRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+appPersonCatalogPath, appPersonCatalogRoute)
	event.Router.POST(apiRoot+appRequestCreatePath, appRequestCreateRoute)
	event.Router.POST(apiRoot+appSelfInstallPath, appSelfInstallRoute)
	event.Router.POST(apiRoot+appConsentPath, appConsentRoute)
	event.Router.GET(apiRoot+appRequestsListPath, appRequestsListRoute)
	event.Router.POST(apiRoot+appRequestDecidePath, appRequestDecideRoute)
	event.Router.PUT(apiRoot+appAllowlistSetPath, appAllowlistSetRoute)
}

// appPerson is the person choosing their apps — signed in or through their
// key — never an app's token.
func appPerson(event *core.RequestEvent) (*core.Record, bool) {
	user, ok := authenticatedUser(event)
	if !ok {
		_ = apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
		return nil, false
	}
	if appTokenCall(event) {
		_ = apiError(event, http.StatusForbidden, errorCodeUserKeyClosed, "An app can't choose apps.")
		return nil, false
	}
	return user, true
}

func appPolicyFailure(event *core.RequestEvent, err error) error {
	switch {
	case errors.Is(err, errAppPolicyRefused):
		return apiError(event, http.StatusForbidden, errorCodeAppPolicyRefused, strings.TrimPrefix(err.Error(), errorCodeAppPolicyRefused+": "))
	case errors.Is(err, errAppRequestMissing):
		return apiError(event, http.StatusNotFound, errorCodeAppRequestNotFound, "Request not found.")
	case errors.Is(err, errAppInTrashForPerson):
		return apiError(event, http.StatusConflict, errorCodeAppInTrashAskAdministrator, "This app is in the trash. Ask an administrator to restore it.")
	case errors.Is(err, sql.ErrNoRows):
		return apiError(event, http.StatusNotFound, errorCodeAppInstallNotFound, "App not found.")
	case errors.Is(err, errAppRevoked) || errors.Is(err, errAppCatalogUnreachable) || errors.Is(err, errAppCatalogInvalid):
		// An allowed app fetched from its catalog says what the catalog answered.
		return appCatalogFailure(event, err)
	}
	return appPlatformFailure(event, err)
}

func appPolicyRefused(message string) error {
	return fmt.Errorf("%w: %s", errAppPolicyRefused, message)
}

// appAllowlisted is the allowlist by app ID, with the catalog each came from.
func appAllowlisted(app core.App) (map[string]string, error) {
	rows := []struct {
		CatalogID string `db:"catalog_id"`
		AppID     string `db:"app_id"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[catalog_id]], [[app_id]] FROM {{leafen_app_allowlist}}`).All(&rows); err != nil {
		return nil, err
	}
	allowed := map[string]string{}
	for _, row := range rows {
		allowed[row.AppID] = row.CatalogID
	}
	return allowed, nil
}

// appCatalogEntryFor finds an app a trusted catalog or Leafen offers, not revoked.
func appCatalogEntryFor(app core.App, catalogID, appID string) (appCatalogEntryWire, error) {
	browse, err := browseAppCatalogs(app)
	if err != nil {
		return appCatalogEntryWire{}, err
	}
	for _, entry := range browse.Apps {
		if entry.CatalogID == catalogID && entry.ID == appID && !entry.Revoked {
			return entry, nil
		}
	}
	return appCatalogEntryWire{}, sql.ErrNoRows
}

// appPersonTenancy checks the person works in the organization they named.
func appPersonTenancy(app core.App, personID, tenancy string) error {
	workspaces, err := appPersonWorkspaces(app, personID, tenancy)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(tenancy, "org:") {
		return refuse(errAppInputInvalid, "tenancyKey is org: and the ID of an organization you work in, not %q", tenancy)
	}
	if len(workspaces) == 0 {
		return refuse(errAppInputInvalid, "you work in none of the organization's workspaces")
	}
	return nil
}

func appPersonCatalogRoute(event *core.RequestEvent) error {
	user, ok := appPerson(event)
	if !ok {
		return nil
	}
	settings, err := loadAppPlatformSettings(event.App)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	browse, err := browseAppCatalogs(event.App)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	allowed, err := appAllowlisted(event.App)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	requests := map[string]string{}
	rows := []struct {
		AppID  string `db:"app_id"`
		Status string `db:"status"`
	}{}
	if err := event.App.DB().NewQuery(`SELECT [[app_id]], [[status]] FROM {{leafen_app_requests}} WHERE [[person_id]] = {:person}
		AND [[status]] != 'approved' ORDER BY [[created]]`).Bind(dbx.Params{"person": user.Id}).All(&rows); err != nil {
		return appPlatformFailure(event, err)
	}
	for _, row := range rows {
		requests[row.AppID] = row.Status
	}
	usable, err := usableApps(event.App, user.Id)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	slugs := map[string]string{}
	// An app that only opens files has no page of its own to open from here.
	fullPages := map[string]bool{}
	for _, entry := range usable {
		slugs[entry.Install.AppID] = entry.Install.Slug
		fullPages[entry.Install.AppID] = entry.Manifest.Surfaces.FullPage != nil
	}
	// An administrator installs from here and chooses who may use what is
	// installed (2026-10-16: the first administrator read "only
	// administrators install" with nothing to press).
	answer := appPersonCatalogWire{Policy: settings.InstallPolicy, Apps: []appPersonCatalogEntryWire{}, Categories: browse.Categories}
	if answer.CanManage, err = isAdministrator(event.App, user.Id); err != nil {
		return appPlatformFailure(event, err)
	}
	installed := map[string]bool{}
	// A stopped app is why its administrator can't use it, not its people
	// (2026-11-06: a stopped app read "사용할 사람 정하기").
	stopped := map[string]string{}
	// An app the person may use that an administrator stopped, or a catalog
	// revoked, stays theirs and says why it doesn't open, as its own page
	// does (2026-11-08: it stood among the others as "Not allowed").
	held, err := grantedApps(event.App, user.Id, "[[status]] != 'active' AND [[removed_at]] = 0")
	if err != nil {
		return appPlatformFailure(event, err)
	}
	for _, entry := range held {
		stopped[entry.Install.AppID] = entry.Install.Status
	}
	if answer.CanManage {
		installs, err := loadAppInstalls(event.App, "[[removed_at]] = 0", dbx.Params{})
		if err != nil {
			return appPlatformFailure(event, err)
		}
		for _, install := range installs {
			installed[install.AppID] = true
			if install.Status == "stopped" || install.Status == "emergency" {
				stopped[install.AppID] = install.Status
			}
		}
	}
	trashed, err := loadAppInstalls(event.App, "[[removed_at]] > 0", dbx.Params{})
	if err != nil {
		return appPlatformFailure(event, err)
	}
	inTrash := map[string]bool{}
	for _, install := range trashed {
		inTrash[install.AppID] = true
	}
	// A listed app shows its listing's icon, or, if the listing has none, the installed package's one for an app the person may use.
	usableIcons := map[string]string{}
	for _, entry := range append(usable, held...) {
		usableIcons[entry.Install.AppID] = appIconURL(event.App, entry.Install, entry.Manifest)
	}
	listed := map[string]bool{}
	for _, entry := range browse.Apps {
		if entry.Revoked {
			continue
		}
		listed[entry.ID] = true
		icon := entry.IconURL
		if icon == "" {
			icon = usableIcons[entry.ID]
		}
		answer.Apps = append(answer.Apps, appPersonCatalogEntryWire{IconURL: icon, CatalogID: entry.CatalogID, CatalogName: entry.CatalogName, ID: entry.ID,
			Name: entry.Name, Summary: entry.Summary, Developer: entry.Developer, Category: entry.Category, Reviewed: entry.Reviewed,
			Network: entry.Network, Risk: entry.Risk, Featured: entry.Featured,
			Allowed: settings.InstallPolicy == "allowlist" && allowed[entry.ID] == entry.CatalogID,
			Request: requests[entry.ID], Usable: slugs[entry.ID] != "", Slug: slugs[entry.ID], FullPage: fullPages[entry.ID], Installed: installed[entry.ID], InTrash: inTrash[entry.ID], Stopped: stopped[entry.ID]})
	}
	// An app installed from a file is on no list, nor is one its catalog
	// revoked, and whoever may use it finds it here too.
	for _, entry := range append(usable, held...) {
		if listed[entry.Install.AppID] {
			continue
		}
		listed[entry.Install.AppID] = true
		manifest := entry.Manifest
		answer.Apps = append(answer.Apps, appPersonCatalogEntryWire{IconURL: appIconURL(event.App, entry.Install, manifest), ID: entry.Install.AppID, Name: appTextOrEmpty(manifest.Name), Summary: appTextOrEmpty(manifest.Description),
			Developer: manifest.Developer.Name, Category: manifest.Category, Network: appManifestNetworkKind(manifest), Risk: appManifestRisk(manifest),
			Usable: slugs[entry.Install.AppID] != "", Slug: entry.Install.Slug, FullPage: fullPages[entry.Install.AppID], Installed: installed[entry.Install.AppID],
			Stopped: stopped[entry.Install.AppID]})
	}
	return event.JSON(http.StatusOK, answer)
}

func appAllowlistSetRoute(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	user, _ := authenticatedUser(event)
	input := appAllowlistWire{}
	if err := json.NewDecoder(event.Request.Body).Decode(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeAppPlatformInputInvalid,
			refusalMessage("List the apps people may add.", bodyRefusalReason("list of apps", err)))
	}
	for _, entry := range input.Apps {
		if _, err := appCatalogEntryFor(event.App, entry.CatalogID, entry.AppID); err != nil {
			return appPolicyFailure(event, err)
		}
	}
	// A key's call asks its person once every app named is in a catalog
	// (user_key_acting_ask.go).
	if asked, err := askBeforeActing(event); asked {
		return err
	}
	err := event.App.RunInTransaction(func(tx core.App) error {
		if _, err := tx.DB().NewQuery(`DELETE FROM {{leafen_app_allowlist}}`).Execute(); err != nil {
			return err
		}
		for _, entry := range input.Apps {
			if _, err := tx.DB().NewQuery(`INSERT INTO {{leafen_app_allowlist}} ([[app_id]], [[catalog_id]], [[added_by]], [[added_at]])
				VALUES ({:app}, {:catalog}, {:by}, {:now}) ON CONFLICT ([[app_id]]) DO UPDATE SET [[catalog_id]] = excluded.[[catalog_id]]`).
				Bind(dbx.Params{"app": entry.AppID, "catalog": entry.CatalogID, "by": user.Id, "now": time.Now().UnixMilli()}).Execute(); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return appPlatformFailure(event, err)
	}
	allowed, err := appAllowlisted(event.App)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	answer := appAllowlistWire{Apps: []appAllowlistEntry{}}
	for appID, catalogID := range allowed {
		answer.Apps = append(answer.Apps, appAllowlistEntry{CatalogID: catalogID, AppID: appID})
	}
	return event.JSON(http.StatusOK, answer)
}

func appSelfInstallRoute(event *core.RequestEvent) error {
	user, ok := appPerson(event)
	if !ok {
		return nil
	}
	input := appSelfInstallInput{}
	if err := json.NewDecoder(event.Request.Body).Decode(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeAppPlatformInputInvalid,
			refusalMessage("Name the app and your organization.", bodyRefusalReason("app to add", err)))
	}
	answer, err := selfInstallApp(event.App, user.Id, input, time.Now())
	if err != nil {
		return appPolicyFailure(event, err)
	}
	return event.JSON(http.StatusOK, answer)
}

// selfInstallApp shows a person what an allowed app may do, or — once they
// accept exactly that — installs it if it isn't yet, lets them use it in
// their organization and keeps their consent.
func selfInstallApp(app core.App, personID string, input appSelfInstallInput, now time.Time) (appConsentWire, error) {
	settings, err := loadAppPlatformSettings(app)
	if err != nil {
		return appConsentWire{}, err
	}
	allowed, err := appAllowlisted(app)
	if err != nil {
		return appConsentWire{}, err
	}
	if settings.InstallPolicy != "allowlist" || allowed[input.AppID] != input.CatalogID || input.CatalogID == "" {
		return appConsentWire{}, appPolicyRefused("Only administrators add this app here.")
	}
	if err := appPersonTenancy(app, personID, input.TenancyKey); err != nil {
		return appConsentWire{}, err
	}
	installs, err := loadAppInstalls(app, "[[app_id]] = {:app}", dbx.Params{"app": input.AppID})
	if err != nil {
		return appConsentWire{}, err
	}
	// Only an administrator can bring it back.
	if len(installs) == 1 && installs[0].RemovedAt > 0 {
		return appConsentWire{}, errAppInTrashForPerson
	}
	var packageHash string
	if len(installs) == 1 {
		packageHash = installs[0].PackageHash
	} else {
		if packageHash, err = fetchAppCatalogPackage(app, input.CatalogID, input.AppID, personID); err != nil {
			return appConsentWire{}, err
		}
	}
	_, manifest, err := loadAppPackage(app, packageHash)
	if err != nil {
		return appConsentWire{}, err
	}
	answer := appConsentWire{AppID: manifest.ID, Name: manifest.Name, Developer: manifest.Developer.Name, Permissions: appPackagePermissions(app, packageHash, manifest),
		PermissionsHash: appPermissionsHash(manifest), Builtin: appInstallBuiltin(app, storedAppInstall{PackageHash: packageHash})}
	if input.AcceptPermissionsHash == "" {
		return answer, nil
	}
	if input.AcceptPermissionsHash != answer.PermissionsHash {
		return appConsentWire{}, errAppPermissionsChange
	}
	install := storedAppInstall{}
	if len(installs) == 1 {
		install = installs[0]
	} else if install, err = installAppPackage(app, packageHash, input.AcceptPermissionsHash, personID, false); err != nil {
		return appConsentWire{}, err
	}
	if err := grantAppPerson(app, install.ID, input.TenancyKey, personID, personID); err != nil {
		return appConsentWire{}, err
	}
	if err := app.RunInTransaction(func(tx core.App) error {
		if err := saveAppConsent(tx, install.ID, personID, answer.PermissionsHash, now); err != nil {
			return err
		}
		return recordAppAudit(tx, install, "self-install", personID, map[string]any{"tenancy": input.TenancyKey, "permissionsHash": answer.PermissionsHash})
	}); err != nil {
		return appConsentWire{}, err
	}
	answer.InstallID, answer.Slug = install.ID, install.Slug
	return answer, nil
}

func saveAppConsent(app core.App, installID, personID, permissionsHash string, now time.Time) error {
	_, err := app.DB().NewQuery(`INSERT INTO {{leafen_app_consents}} ([[install_id]], [[person_id]], [[permissions_hash]], [[consented_at]])
		VALUES ({:install}, {:person}, {:hash}, {:now}) ON CONFLICT ([[install_id]], [[person_id]]) DO UPDATE
		SET [[permissions_hash]] = excluded.[[permissions_hash]], [[consented_at]] = excluded.[[consented_at]]`).Bind(dbx.Params{
		"install": installID, "person": personID, "hash": permissionsHash, "now": now.UnixMilli()}).Execute()
	return err
}

// appConsentNeeded is what a person who added an app themselves must agree
// to again because its permissions changed since they did; nil otherwise.
func appConsentNeeded(app core.App, install storedAppInstall, personID string) (*appConsentWire, error) {
	var consented string
	err := app.DB().NewQuery(`SELECT [[permissions_hash]] FROM {{leafen_app_consents}} WHERE [[install_id]] = {:install} AND [[person_id]] = {:person}`).
		Bind(dbx.Params{"install": install.ID, "person": personID}).Row(&consented)
	if errors.Is(err, sql.ErrNoRows) || err == nil && consented == install.PermissionsHash {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	_, manifest, err := loadAppPackage(app, install.PackageHash)
	if err != nil {
		return nil, err
	}
	previous, err := appConsentedPermissions(app, install.AppID, consented)
	if err != nil {
		return nil, err
	}
	return &appConsentWire{AppID: manifest.ID, Name: manifest.Name, Developer: manifest.Developer.Name, Permissions: appPackagePermissions(app, install.PackageHash, manifest),
		PermissionsHash: install.PermissionsHash, InstallID: install.ID, Slug: install.Slug, PreviousPermissions: previous,
		Builtin: appInstallBuiltin(app, install)}, nil
}

// appConsentedPermissions is the list a person agreed to, from the version of
// the app whose permissions match their consent, so agreeing again marks what
// is new (2026-11-05: it said only that something changed). Nil when that
// version is no longer kept.
func appConsentedPermissions(app core.App, appID, consented string) ([]appPermissionWire, error) {
	hashes := []string{}
	if err := app.DB().NewQuery(`SELECT [[hash]] FROM {{leafen_app_packages}} WHERE [[app_id]] = {:app} ORDER BY [[created]] DESC`).
		Bind(dbx.Params{"app": appID}).Column(&hashes); err != nil {
		return nil, err
	}
	for _, hash := range hashes {
		_, manifest, err := loadAppPackage(app, hash)
		if err != nil {
			continue
		}
		if appPermissionsHash(manifest) == consented {
			return appPackagePermissions(app, hash, manifest), nil
		}
	}
	return nil, nil
}

func appConsentRoute(event *core.RequestEvent) error {
	user, ok := appPerson(event)
	if !ok {
		return nil
	}
	input := appConsentInput{}
	if err := json.NewDecoder(event.Request.Body).Decode(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeAppPlatformInputInvalid,
			refusalMessage("Agree to what the app may do.", bodyRefusalReason("consent", err)))
	}
	install, err := loadAppInstall(event.App, event.Request.PathValue("installId"))
	if err != nil {
		return appPolicyFailure(event, err)
	}
	needed, err := appConsentNeeded(event.App, install, user.Id)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	if needed == nil {
		return event.JSON(http.StatusOK, appConsentWire{AppID: install.AppID, Name: map[string]string{}, Permissions: []appPermissionWire{},
			PermissionsHash: install.PermissionsHash, InstallID: install.ID, Slug: install.Slug})
	}
	// Without a hash: what the person is asked to agree to.
	if input.PermissionsHash == "" {
		return event.JSON(http.StatusOK, needed)
	}
	if input.PermissionsHash != install.PermissionsHash {
		return appPolicyFailure(event, errAppPermissionsChange)
	}
	if err := event.App.RunInTransaction(func(tx core.App) error {
		if err := saveAppConsent(tx, install.ID, user.Id, install.PermissionsHash, time.Now()); err != nil {
			return err
		}
		return recordAppAudit(tx, install, "consent", user.Id, map[string]any{"permissionsHash": install.PermissionsHash})
	}); err != nil {
		return appPlatformFailure(event, err)
	}
	return event.JSON(http.StatusOK, needed)
}

// appPolicyState is the allowlist and the first window of the requests
// waiting (more from GET /admin/apps/requests), for the administrators' list.
func appPolicyState(app core.App) ([]appAllowlistEntry, []appRequestWire, string, error) {
	allowed, err := appAllowlisted(app)
	if err != nil {
		return nil, nil, "", err
	}
	allowlist := []appAllowlistEntry{}
	for appID, catalogID := range allowed {
		allowlist = append(allowlist, appAllowlistEntry{CatalogID: catalogID, AppID: appID})
	}
	requests, next, err := listPendingAppRequests(app, "")
	return allowlist, requests, next, err
}
