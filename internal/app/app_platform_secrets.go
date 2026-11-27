package app

import (
	"database/sql"
	"errors"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// An app's secrets (docs/app-platform.md 8): an administrator puts each one
// its relay entries name, sealed with the administrator settings key — so a
// backup keeps them sealed — and shown back only by its last four
// characters. Nothing hands a secret to the app: the relay fills it into a
// request's headers where the app wrote {{secret:NAME}}.

const (
	appSecretsTable        = "leafen_app_secrets"
	appRelayLogTable       = "leafen_app_relay_log"
	appPlatformSettingsRow = "default"
)

// appDeclaredSecrets names each secret the app's relay entries use, with the
// entries that use it, and each outside sign-in's client ID and secret, for
// its token address.
func appDeclaredSecrets(manifest appManifest) map[string][]string {
	declared := map[string][]string{}
	for _, relay := range manifest.Network.Relay {
		for _, name := range relay.Secrets {
			if !slices.Contains(declared[name], relay.URL) {
				declared[name] = append(declared[name], relay.URL)
			}
		}
	}
	for _, provider := range manifest.Network.OAuth {
		id, secret := appOAuthClientSecretNames(provider.Key)
		declared[id] = []string{provider.TokenURL}
		declared[secret] = []string{provider.TokenURL}
	}
	return declared
}

// appOAuthClientSecretNames names the secrets holding a sign-in's client
// ID and secret: KEY_CLIENT_ID and KEY_CLIENT_SECRET.
func appOAuthClientSecretNames(key string) (string, string) {
	base := strings.ToUpper(strings.ReplaceAll(key, "-", "_"))
	return base + "_CLIENT_ID", base + "_CLIENT_SECRET"
}

func listAppSecrets(app core.App, install storedAppInstall, manifest appManifest) ([]appSecretWire, error) {
	stored := []struct {
		Name      string `db:"name"`
		Sealed    string `db:"sealed"`
		Last4     string `db:"last4"`
		UpdatedAt int64  `db:"updated_at"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[name]], [[sealed]], [[last4]], [[updated_at]] FROM {{leafen_app_secrets}} WHERE [[install_id]] = {:install}`).
		Bind(dbx.Params{"install": install.ID}).All(&stored); err != nil {
		return nil, err
	}
	set := map[string]appSecretWire{}
	for _, row := range stored {
		// A value sealed under another key (a restore elsewhere, a recovered
		// key) can't be opened: it needs entering again.
		_, openErr := decryptAdministratorSetting(app, row.Sealed)
		set[row.Name] = appSecretWire{Name: row.Name, Set: true, Last4: row.Last4, UpdatedAt: row.UpdatedAt, NeedsEntry: openErr != nil}
	}
	secrets := []appSecretWire{}
	for name, relays := range appDeclaredSecrets(manifest) {
		secret := set[name]
		secret.Name, secret.Relays = name, relays
		secrets = append(secrets, secret)
	}
	sort.Slice(secrets, func(left, right int) bool { return secrets[left].Name < secrets[right].Name })
	return secrets, nil
}

// setAppSecret seals a value the app declares; an empty value clears it.
func setAppSecret(app core.App, install storedAppInstall, manifest appManifest, name, value, actorID string) error {
	if _, declared := appDeclaredSecrets(manifest)[name]; !declared {
		return refuse(errAppInputInvalid, "the app declares no secret named %q", name)
	}
	if value == "" {
		return app.RunInTransaction(func(tx core.App) error {
			if _, err := tx.DB().Delete(appSecretsTable, dbx.HashExp{"install_id": install.ID, "name": name}).Execute(); err != nil {
				return err
			}
			return recordAppAudit(tx, install, "secret-clear", actorID, map[string]any{"name": name})
		})
	}
	if len(value) > appSecretMaxBytes {
		return refuse(errAppInputInvalid, "value is at most %d bytes", appSecretMaxBytes)
	}
	if !utf8.ValidString(value) || strings.ContainsAny(value, "\r\n") {
		return refuse(errAppInputInvalid, "value is one line of text")
	}
	sealed, err := encryptAdministratorSetting(app, cipherAppSecret, value)
	if err != nil {
		return err
	}
	runes := []rune(value)
	last4 := string(runes[max(0, len(runes)-4):])
	return app.RunInTransaction(func(tx core.App) error {
		if _, err := tx.DB().NewQuery(`INSERT INTO {{leafen_app_secrets}}
			([[install_id]], [[name]], [[sealed]], [[last4]], [[updated_by]], [[updated_at]])
			VALUES ({:install}, {:name}, {:sealed}, {:last4}, {:actor}, {:now})
			ON CONFLICT ([[install_id]], [[name]]) DO UPDATE SET [[sealed]] = excluded.[[sealed]], [[last4]] = excluded.[[last4]],
				[[updated_by]] = excluded.[[updated_by]], [[updated_at]] = excluded.[[updated_at]]`).Bind(dbx.Params{
			"install": install.ID, "name": name, "sealed": sealed, "last4": last4, "actor": actorID, "now": time.Now().UnixMilli(),
		}).Execute(); err != nil {
			return err
		}
		return recordAppAudit(tx, install, "secret-set", actorID, map[string]any{"name": name})
	})
}

// appSecretValues opens the secrets an install has, by name.
func appSecretValues(app core.App, installID string) (map[string]string, error) {
	rows := []struct {
		Name   string `db:"name"`
		Sealed string `db:"sealed"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[name]], [[sealed]] FROM {{leafen_app_secrets}} WHERE [[install_id]] = {:install}`).
		Bind(dbx.Params{"install": installID}).All(&rows); err != nil {
		return nil, err
	}
	values := map[string]string{}
	for _, row := range rows {
		// One that can't be opened is left out, as if unset, and the others work.
		if plain, err := decryptAdministratorSetting(app, row.Sealed); err == nil {
			values[row.Name] = plain
		}
	}
	return values, nil
}

// appInstallPolicies are how people come to apps; the first is the default.
var appInstallPolicies = []string{"admins", "request", "allowlist"}

func loadAppPlatformSettings(app core.App) (appPlatformSettingsWire, error) {
	var strict, homeScreenOff, unsignedOff int
	var policy string
	var maxPackageBytes int64
	err := app.DB().NewQuery(`SELECT [[strict_network]], [[home_screen_off]], [[install_policy]], [[unsigned_off]],
		[[max_package_bytes]] FROM {{leafen_app_platform_settings}} WHERE [[id]] = {:id}`).Bind(dbx.Params{"id": appPlatformSettingsRow}).
		Row(&strict, &homeScreenOff, &policy, &unsignedOff, &maxPackageBytes)
	if errors.Is(err, sql.ErrNoRows) {
		return appPlatformSettingsWire{InstallPolicy: appInstallPolicies[0]}, nil
	}
	return appPlatformSettingsWire{StrictNetwork: strict == 1, HomeScreenAppsOff: homeScreenOff == 1, InstallPolicy: policy,
		UnsignedAppsOff: unsignedOff == 1, MaxPackageBytes: maxPackageBytes}, err
}

func saveAppPlatformSettings(app core.App, settings appPlatformSettingsWire, actorID string) error {
	flag := func(on bool) int {
		if on {
			return 1
		}
		return 0
	}
	policy := settings.InstallPolicy
	if policy == "" {
		policy = appInstallPolicies[0]
	}
	if !slices.Contains(appInstallPolicies, policy) {
		return refuse(errAppInputInvalid, "installPolicy is %s, not %q", notionOrList(appInstallPolicies), policy)
	}
	// 0 is the most Leafen takes; an administrator may set less.
	if settings.MaxPackageBytes != 0 && (settings.MaxPackageBytes < appPackageMinLimitBytes || settings.MaxPackageBytes > appPackageMaxBytes) {
		return refuse(errAppInputInvalid, "maxPackageBytes is 0 for the most Leafen takes, or from %d to %d", int64(appPackageMinLimitBytes), int64(appPackageMaxBytes))
	}
	_, err := app.DB().NewQuery(`INSERT INTO {{leafen_app_platform_settings}} ([[id]], [[strict_network]], [[home_screen_off]], [[install_policy]],
			[[unsigned_off]], [[max_package_bytes]], [[updated_by]], [[updated_at]])
		VALUES ({:id}, {:strict}, {:homeScreenOff}, {:policy}, {:unsignedOff}, {:maxPackageBytes}, {:actor}, {:now})
		ON CONFLICT ([[id]]) DO UPDATE SET [[strict_network]] = excluded.[[strict_network]], [[home_screen_off]] = excluded.[[home_screen_off]],
			[[install_policy]] = excluded.[[install_policy]], [[unsigned_off]] = excluded.[[unsigned_off]],
			[[max_package_bytes]] = excluded.[[max_package_bytes]],
			[[updated_by]] = excluded.[[updated_by]], [[updated_at]] = excluded.[[updated_at]]`).Bind(dbx.Params{
		"id": appPlatformSettingsRow, "strict": flag(settings.StrictNetwork), "homeScreenOff": flag(settings.HomeScreenAppsOff),
		"policy": policy, "unsignedOff": flag(settings.UnsignedAppsOff),
		"maxPackageBytes": settings.MaxPackageBytes, "actor": actorID, "now": time.Now().UnixMilli(),
	}).Execute()
	return err
}
