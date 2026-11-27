package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// App passwords (document apps decision of 2026-10-10, 8): a program
// outside Leafen — a file manager on WebDAV, a password manager's extension
// — signs in to an app's endpoints with a password the person makes for that
// app, since it can't open Leafen's sign-in. It reaches only what the person
// may through that app, never Leafen's own sign-in, API or MCP. The person
// names each one, sees it once, and takes it back whenever they like; it
// goes with the app's install, the person's account, or their place in the
// organization the app runs for.

const (
	appPasswordsTable         = "leafen_app_passwords"
	appPasswordsPerApp        = 20
	appPasswordNameMaxRunes   = 100
	appPasswordSecretRunes    = 24
	appPasswordSecretAlphabet = "abcdefghijkmnpqrstuvwxyz23456789"
	appEndpointAddressPrefix  = "/app-endpoints/"
)

var (
	errAppPasswordInvalid = errors.New(errorCodeAppPasswordInvalid)
	errAppPasswordLimit   = errors.New(errorCodeAppPasswordLimit)
)

// appPasswordHash is how a password is kept and found: its letters without
// the dashes it is shown with, so it may be typed either way.
func appPasswordHash(secret string) string {
	digest := sha256.Sum256([]byte(strings.ToLower(strings.ReplaceAll(strings.TrimSpace(secret), "-", ""))))
	return hex.EncodeToString(digest[:])
}

// newAppPassword is six groups of four letters and digits that can't be
// mistaken for one another, about 120 bits.
func newAppPassword() string {
	raw := security.RandomStringWithAlphabet(appPasswordSecretRunes, appPasswordSecretAlphabet)
	groups := make([]string, 0, appPasswordSecretRunes/4)
	for start := 0; start < len(raw); start += 4 {
		groups = append(groups, raw[start:start+4])
	}
	return strings.Join(groups, "-")
}

// appEndpointAddress is where a program reaches an app's endpoint.
func appEndpointAddress(origin, slug, key string) string {
	return strings.TrimRight(origin, "/") + appEndpointAddressPrefix + slug + "/" + key + "/"
}

// listAppPasswords is, for each app the person may use that has endpoints,
// its endpoints' addresses and the person's passwords for it.
func listAppPasswords(app core.App, userID, origin string) (appPasswordListResponse, error) {
	response := appPasswordListResponse{Apps: []appPasswordAppWire{}, PasswordLimit: appPasswordsPerApp}
	usable, err := usableApps(app, userID)
	if err != nil {
		return response, err
	}
	for _, candidate := range usable {
		if len(candidate.Manifest.Endpoints) == 0 {
			continue
		}
		entry := appPasswordAppWire{
			InstallID: candidate.Install.ID, Tenancy: candidate.TenancyKey, TenancyName: candidate.TenancyName,
			AppName: candidate.Manifest.Name, Endpoints: []appPasswordEndpointWire{}, Passwords: []appPasswordWire{},
		}
		for _, endpoint := range candidate.Manifest.Endpoints {
			entry.Endpoints = append(entry.Endpoints, appPasswordEndpointWire{
				Key: endpoint.Key, Name: endpoint.Name, Address: appEndpointAddress(origin, candidate.Manifest.Slug, endpoint.Key),
			})
		}
		rows := []struct {
			ID         string `db:"id"`
			Name       string `db:"name"`
			Hint       string `db:"hint"`
			CreatedAt  int64  `db:"created_at"`
			LastUsedAt int64  `db:"last_used_at"`
		}{}
		if err := app.DB().NewQuery(`SELECT [[id]], [[name]], [[hint]], [[created_at]], [[last_used_at]] FROM {{leafen_app_passwords}}
			WHERE [[user_id]] = {:userId} AND [[install_id]] = {:installId} AND [[tenancy_key]] = {:tenancy}
			ORDER BY [[created_at]] DESC, [[id]]`).
			Bind(dbx.Params{"userId": userID, "installId": candidate.Install.ID, "tenancy": candidate.TenancyKey}).All(&rows); err != nil {
			return response, err
		}
		for _, row := range rows {
			entry.Passwords = append(entry.Passwords, appPasswordWire{
				ID: row.ID, Name: row.Name, Hint: row.Hint, CreatedAt: row.CreatedAt, LastUsedAt: row.LastUsedAt,
			})
		}
		if entry.StoredBytes, err = appEndpointStoredBytes(app, candidate.Install.ID, userID); err != nil {
			return response, err
		}
		entry.StorageLimitBytes = int64(loadAppEndpointLimits(app, candidate.Install.ID).PersonStorageMB) << 20
		response.Apps = append(response.Apps, entry)
	}
	return response, nil
}

// createAppPassword makes a password for an app the person may use in an
// organization, answering it this once.
func createAppPassword(app core.App, userID, installID, tenancyKey, name string) (appPasswordWire, string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > appPasswordNameMaxRunes || strings.ContainsAny(name, "\r\n") {
		return appPasswordWire{}, "", errAppPasswordInvalid
	}
	usable, err := usableApps(app, userID)
	if err != nil {
		return appPasswordWire{}, "", err
	}
	found := false
	for _, candidate := range usable {
		if candidate.Install.ID == installID && candidate.TenancyKey == tenancyKey && len(candidate.Manifest.Endpoints) > 0 {
			found = true
		}
	}
	if !found {
		return appPasswordWire{}, "", errAppNotUsable
	}
	secret := newAppPassword()
	created := appPasswordWire{
		ID: core.GenerateDefaultRandomId(), Name: name, Hint: secret[len(secret)-4:], CreatedAt: time.Now().UnixMilli(),
	}
	err = app.RunInTransaction(func(tx core.App) error {
		count := 0
		if err := tx.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_app_passwords}}
			WHERE [[user_id]] = {:userId} AND [[install_id]] = {:installId} AND [[tenancy_key]] = {:tenancy}`).
			Bind(dbx.Params{"userId": userID, "installId": installID, "tenancy": tenancyKey}).Row(&count); err != nil {
			return err
		}
		if count >= appPasswordsPerApp {
			return errAppPasswordLimit
		}
		_, err := tx.DB().Insert(appPasswordsTable, dbx.Params{
			"id": created.ID, "install_id": installID, "tenancy_key": tenancyKey, "user_id": userID, "name": name,
			"secret_hash": appPasswordHash(secret), "hint": created.Hint, "created_at": created.CreatedAt,
		}).Execute()
		return err
	})
	if err != nil {
		return appPasswordWire{}, "", err
	}
	return created, secret, nil
}

// deleteAppPassword takes one of the person's passwords back.
func deleteAppPassword(app core.App, userID, passwordID string) error {
	result, err := app.DB().Delete(appPasswordsTable, dbx.HashExp{"id": passwordID, "user_id": userID}).Execute()
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}
