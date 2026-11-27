//go:build !leafen_noapps

package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

func registerAppCredentialRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+appPlatformCredentialsListPath, listAppCredentialsRoute)
	event.Router.PUT(apiRoot+appPlatformCredentialSetPath, setAppCredentialRoute).Bind(apis.BodyLimit(appPlatformInputMaxBytes))
	event.Router.DELETE(apiRoot+appPlatformCredentialDeletePath, deleteAppCredentialRoute)
	event.Router.GET(apiRoot+appPlatformPersonCredentialsPath, listPersonAppCredentialsRoute)
	event.Router.GET(apiRoot+appPlatformCredentialsAdminPath, adminAppCredentialsRoute)
	event.Router.PUT(apiRoot+appPlatformCredentialsPolicyPath, setAppCredentialsPolicyRoute).Bind(apis.BodyLimit(appPlatformInputMaxBytes))
	event.Router.DELETE(apiRoot+appPlatformCredentialsRevokePath, revokeAppCredentialsRoute)
}

// personAppCredentialInstall is the install a person reaches — signed in or
// through their key — never through an app's token, which could
// otherwise put or read another app's credentials; and only an app they may
// use.
func personAppCredentialInstall(event *core.RequestEvent) (*core.Record, storedAppInstall, appManifest, bool, error) {
	user, ok := authenticatedUser(event)
	if !ok {
		return nil, storedAppInstall{}, appManifest{}, false, apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	if appTokenCall(event) {
		return nil, storedAppInstall{}, appManifest{}, false,
			apiError(event, http.StatusForbidden, errorCodeAppRelayDenied, "An app can't give or read an app's credentials.")
	}
	installID := event.Request.PathValue("installId")
	usable, err := usableApps(event.App, user.Id)
	if err != nil {
		return nil, storedAppInstall{}, appManifest{}, false, appPlatformFailure(event, err)
	}
	for _, app := range usable {
		if app.Install.ID == installID {
			return user, app.Install, app.Manifest, true, nil
		}
	}
	return nil, storedAppInstall{}, appManifest{}, false, apiError(event, http.StatusNotFound, errorCodeAppInstallNotFound, "No such app.")
}

func listAppCredentials(app core.App, install storedAppInstall, manifest appManifest, userID string) (appCredentialListResponse, error) {
	enabled, err := appCredentialsEnabled(app, install.ID)
	if err != nil {
		return appCredentialListResponse{}, err
	}
	rows := []struct {
		Key     string `db:"key"`
		Updated int64  `db:"updated"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[key]], [[updated]] FROM {{leafen_app_person_credentials}}
		WHERE [[install_id]] = {:install} AND [[user_id]] = {:user}`).
		Bind(dbx.Params{"install": install.ID, "user": userID}).All(&rows); err != nil {
		return appCredentialListResponse{}, err
	}
	given := map[string]int64{}
	for _, row := range rows {
		given[row.Key] = row.Updated
	}
	response := appCredentialListResponse{Enabled: enabled, Credentials: []appCredentialWire{}}
	for _, credential := range manifest.Credentials {
		fields := []appCredentialFieldWire{}
		for _, field := range credential.Fields {
			fields = append(fields, appCredentialFieldWire{Key: field.Key, Name: field.Name, Secret: field.Secret})
		}
		updated, connected := given[credential.Key]
		response.Credentials = append(response.Credentials, appCredentialWire{
			Key: credential.Key, Name: credential.Name, Help: credential.Help, Fields: fields, Connected: connected, UpdatedAt: updated,
		})
	}
	return response, nil
}

func listAppCredentialsRoute(event *core.RequestEvent) error {
	user, install, manifest, ok, failure := personAppCredentialInstall(event)
	if !ok {
		return failure
	}
	response, err := listAppCredentials(event.App, install, manifest, user.Id)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	return event.JSON(http.StatusOK, response)
}

// setAppCredentialRoute seals what the person typed, every field the app
// declares and nothing else.
func setAppCredentialRoute(event *core.RequestEvent) error {
	user, install, manifest, ok, failure := personAppCredentialInstall(event)
	if !ok {
		return failure
	}
	key := event.Request.PathValue("key")
	credential, declared := appCredentialDeclared(manifest, key)
	input := appCredentialInput{}
	if !declared {
		return appPlatformFailure(event, refuse(errAppInputInvalid, "the app asks for no account details named %q", key))
	}
	if err := json.NewDecoder(event.Request.Body).Decode(&input); err != nil {
		return appPlatformFailure(event, bodyRefusal(errAppInputInvalid, "set of account details", err))
	}
	if refusal := appCredentialValuesRefusal(credential, input.Values); refusal != nil {
		return appPlatformFailure(event, refusal)
	}
	enabled, err := appCredentialsEnabled(event.App, install.ID)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	if !enabled {
		return apiError(event, http.StatusForbidden, errorCodeAppRelayDenied, errAppCredentialsOff.Error())
	}
	encoded, _ := json.Marshal(input.Values)
	sealed, err := encryptAdministratorSetting(event.App, cipherAppCredential, string(encoded))
	if err != nil {
		return appPlatformFailure(event, err)
	}
	now := time.Now().UnixMilli()
	err = event.App.RunInTransaction(func(tx core.App) error {
		if _, err := tx.DB().NewQuery(`INSERT INTO {{leafen_app_person_credentials}}
			([[install_id]], [[user_id]], [[key]], [[sealed]], [[created]], [[updated]], [[used_at]])
			VALUES ({:install}, {:user}, {:key}, {:sealed}, {:now}, {:now}, 0)
			ON CONFLICT ([[install_id]], [[user_id]], [[key]]) DO UPDATE SET [[sealed]] = excluded.[[sealed]], [[updated]] = excluded.[[updated]]`).
			Bind(dbx.Params{"install": install.ID, "user": user.Id, "key": key, "sealed": sealed, "now": now}).Execute(); err != nil {
			return err
		}
		return recordAppAudit(tx, install, "credential-set", user.Id, map[string]any{"key": key})
	})
	if err != nil {
		return appPlatformFailure(event, err)
	}
	return listAppCredentialsRoute(event)
}

// deleteAppCredentialRoute removes what the person gave, whenever they like
// — also for an app they may no longer use.
func deleteAppCredentialRoute(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	if appTokenCall(event) {
		return apiError(event, http.StatusForbidden, errorCodeAppRelayDenied, "An app can't remove an app's credentials.")
	}
	install, err := loadAppInstall(event.App, event.Request.PathValue("installId"))
	if err != nil {
		return appPlatformFailure(event, err)
	}
	key := event.Request.PathValue("key")
	err = event.App.RunInTransaction(func(tx core.App) error {
		result, err := tx.DB().Delete("leafen_app_person_credentials", dbx.HashExp{"install_id": install.ID, "user_id": user.Id, "key": key}).Execute()
		if err != nil {
			return err
		}
		if changed, _ := result.RowsAffected(); changed == 0 {
			return nil
		}
		return recordAppAudit(tx, install, "credential-delete", user.Id, map[string]any{"key": key})
	})
	if err != nil {
		return appPlatformFailure(event, err)
	}
	return listPersonAppCredentialsRoute(event)
}

// listPersonAppCredentialsRoute lists everything the person gave apps, for
// their account settings.
func listPersonAppCredentialsRoute(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	if appTokenCall(event) {
		return apiError(event, http.StatusForbidden, errorCodeAppRelayDenied, "An app can't see what was given to apps.")
	}
	rows := []struct {
		InstallID string `db:"install_id"`
		Key       string `db:"key"`
		Updated   int64  `db:"updated"`
		UsedAt    int64  `db:"used_at"`
	}{}
	if err := event.App.DB().NewQuery(`SELECT [[install_id]], [[key]], [[updated]], [[used_at]] FROM {{leafen_app_person_credentials}}
		WHERE [[user_id]] = {:user} ORDER BY [[updated]] DESC`).Bind(dbx.Params{"user": user.Id}).All(&rows); err != nil {
		return appPlatformFailure(event, err)
	}
	response := appPersonCredentialListResponse{Credentials: []appPersonCredentialWire{}}
	for _, row := range rows {
		install, err := loadAppInstall(event.App, row.InstallID)
		if err != nil {
			continue
		}
		_, manifest, err := loadAppPackage(event.App, install.PackageHash)
		if err != nil {
			continue
		}
		name := map[string]string{"en": row.Key}
		if credential, declared := appCredentialDeclared(manifest, row.Key); declared {
			name = credential.Name
		}
		response.Credentials = append(response.Credentials, appPersonCredentialWire{
			InstallID: row.InstallID, AppName: manifest.Name, Key: row.Key, Name: name, UpdatedAt: row.Updated, UsedAt: row.UsedAt,
		})
	}
	return event.JSON(http.StatusOK, response)
}

func adminAppCredentialsRoute(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	install, manifest, err := adminAppInstall(event)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	enabled, err := appCredentialsEnabled(event.App, install.ID)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	counts := []struct {
		Key    string `db:"key"`
		People int    `db:"people"`
	}{}
	if err := event.App.DB().NewQuery(`SELECT [[key]], COUNT(*) AS [[people]] FROM {{leafen_app_person_credentials}}
		WHERE [[install_id]] = {:install} GROUP BY [[key]]`).Bind(dbx.Params{"install": install.ID}).All(&counts); err != nil {
		return appPlatformFailure(event, err)
	}
	people := map[string]int{}
	for _, count := range counts {
		people[count.Key] = count.People
	}
	response := appAdminCredentialListResponse{Enabled: enabled, Credentials: []appAdminCredentialWire{}}
	for _, credential := range manifest.Credentials {
		response.Credentials = append(response.Credentials, appAdminCredentialWire{Key: credential.Key, Name: credential.Name, People: people[credential.Key]})
	}
	return event.JSON(http.StatusOK, response)
}

// setAppCredentialsPolicyRoute lets people give the app their credentials,
// or stops it; what they gave stays until they or the administrator take it.
func setAppCredentialsPolicyRoute(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	user, _ := authenticatedUser(event)
	install, manifest, err := adminAppInstall(event)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	input := appCredentialPolicyInput{}
	if len(manifest.Credentials) == 0 {
		return appPlatformFailure(event, refuse(errAppInputInvalid, "the app asks for no one's account details"))
	}
	if err := json.NewDecoder(event.Request.Body).Decode(&input); err != nil {
		return appPlatformFailure(event, bodyRefusal(errAppInputInvalid, "account details choice", err))
	}
	// A key's call asks its person once the app takes people's credentials
	// (user_key_acting_ask.go).
	if asked, err := askBeforeActing(event); asked {
		return err
	}
	err = event.App.RunInTransaction(func(tx core.App) error { return setAppCredentialPolicy(tx, install, input.Enabled, user.Id) })
	if err != nil {
		return appPlatformFailure(event, err)
	}
	return adminAppCredentialsRoute(event)
}

// setAppCredentialPolicy lets people give the app their own account details,
// or stops them: the administrator's switch, or their install's approval,
// which turns it on unless they turned it off there (the person credentials
// decision of 2026-11-05).
func setAppCredentialPolicy(tx core.App, install storedAppInstall, enabled bool, actorID string) error {
	if _, err := tx.DB().NewQuery(`INSERT INTO {{leafen_app_credential_policies}} ([[install_id]], [[enabled]], [[updated_by]], [[updated_at]])
		VALUES ({:install}, {:enabled}, {:actor}, {:now})
		ON CONFLICT ([[install_id]]) DO UPDATE SET [[enabled]] = excluded.[[enabled]], [[updated_by]] = excluded.[[updated_by]],
			[[updated_at]] = excluded.[[updated_at]]`).
		Bind(dbx.Params{"install": install.ID, "enabled": enabled, "actor": actorID, "now": time.Now().UnixMilli()}).Execute(); err != nil {
		return err
	}
	return recordAppAudit(tx, install, "credential-policy", actorID, map[string]any{"enabled": enabled})
}

// revokeAppCredentialsRoute takes back what everyone gave the app.
func revokeAppCredentialsRoute(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	user, _ := authenticatedUser(event)
	install, _, err := adminAppInstall(event)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	// A key's call asks its person once the app is found (user_key_acting_ask.go).
	if asked, err := askBeforeActing(event); asked {
		return err
	}
	err = event.App.RunInTransaction(func(tx core.App) error {
		result, err := tx.DB().Delete("leafen_app_person_credentials", dbx.HashExp{"install_id": install.ID}).Execute()
		if err != nil {
			return err
		}
		taken, _ := result.RowsAffected()
		return recordAppAudit(tx, install, "credential-revoke-all", user.Id, map[string]any{"count": taken})
	})
	if err != nil && !errors.Is(err, errAppInputInvalid) {
		return appPlatformFailure(event, err)
	}
	return adminAppCredentialsRoute(event)
}
