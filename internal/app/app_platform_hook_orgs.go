package app

import (
	"database/sql"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A hook's address for one organization (docs/app-platform.md 11): besides
// the hook's one address, whose calls the worker places itself, the
// administrator may make an address — with its own secret — for each
// organization; what arrives there runs for that organization, with
// `tenancy` in the worker's input.

func appHookOrgAddress(origin string, install storedAppInstall, key, tenancy string) string {
	return appHookAddress(origin, install, key) + "?org=" + url.QueryEscape(strings.TrimPrefix(tenancy, "org:"))
}

// appHookOrganizations are the addresses made for organizations.
func appHookOrganizations(app core.App, install storedAppInstall, key, origin string) ([]appHookOrgWire, error) {
	rows := []struct {
		Tenancy string `db:"tenancy_key"`
		Sealed  string `db:"sealed"`
		Last4   string `db:"last4"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[tenancy_key]], [[sealed]], [[last4]] FROM {{leafen_app_hook_org_secrets}}
		WHERE [[install_id]] = {:install} AND [[hook]] = {:hook} ORDER BY [[created_at]]`).
		Bind(dbx.Params{"install": install.ID, "hook": key}).All(&rows); err != nil {
		return nil, err
	}
	names := appTenancyNames(app)
	organizations := []appHookOrgWire{}
	for _, row := range rows {
		organization := appHookOrgWire{TenancyKey: row.Tenancy, Name: names[row.Tenancy], URL: appHookOrgAddress(origin, install, key, row.Tenancy), Last4: row.Last4}
		_, openErr := decryptAdministratorSetting(app, row.Sealed)
		organization.NeedsNewSecret = openErr != nil
		if err := app.DB().NewQuery(`SELECT COUNT(*) FILTER (WHERE [[status]] = 'waiting'), COALESCE(MAX([[received_at]]), 0)
			FROM {{leafen_app_hook_events}} WHERE [[install_id]] = {:install} AND [[hook]] = {:hook} AND [[kind]] = 'hook' AND [[topic]] = {:tenancy}`).
			Bind(dbx.Params{"install": install.ID, "hook": key, "tenancy": row.Tenancy}).Row(&organization.Waiting, &organization.LastReceivedAt); err != nil {
			return nil, err
		}
		earlier, err := appHookEarlierAddress(app, install.ID, key, row.Tenancy)
		if err != nil {
			return nil, err
		}
		organization.EarlierAddress = earlier
		organizations = append(organizations, organization)
	}
	return organizations, nil
}

// storeAppHookOrgSecret keeps an organization's secret for the hook; the
// organization must be one of the server's.
func storeAppHookOrgSecret(tx core.App, install storedAppInstall, key, tenancy, sealed, last4, actorID string) error {
	tenancies, err := appTenancies(tx)
	if err != nil {
		return err
	}
	// Only an organization's: the server's own address is the hook's.
	organizations := []appTenancyWire{}
	for _, item := range tenancies {
		if strings.HasPrefix(item.Key, "org:") {
			organizations = append(organizations, item)
		}
	}
	if refused := appTenancyRefusal("tenancyKey", tenancy, organizations); refused != nil {
		return refused
	}
	_, err = tx.DB().NewQuery(`INSERT INTO {{leafen_app_hook_org_secrets}}
		([[install_id]], [[hook]], [[tenancy_key]], [[sealed]], [[last4]], [[created_by]], [[created_at]])
		VALUES ({:install}, {:hook}, {:tenancy}, {:sealed}, {:last4}, {:actor}, {:now})
		ON CONFLICT ([[install_id]], [[hook]], [[tenancy_key]]) DO UPDATE SET [[sealed]] = excluded.[[sealed]], [[last4]] = excluded.[[last4]],
			[[created_by]] = excluded.[[created_by]], [[created_at]] = excluded.[[created_at]]`).Bind(dbx.Params{
		"install": install.ID, "hook": key, "tenancy": tenancy, "sealed": sealed, "last4": last4, "actor": actorID, "now": time.Now().UnixMilli(),
	}).Execute()
	return err
}

// removeAppHookOrg takes an organization's address away; calls to it are
// refused from then on, and what waits from it still runs.
func removeAppHookOrg(app core.App, install storedAppInstall, manifest appManifest, key, organizationID, actorID string) error {
	if _, ok := appManifestHook(manifest, key); !ok {
		return refuse(errAppInputInvalid, "the app declares no hook named %q", key)
	}
	return app.RunInTransaction(func(tx core.App) error {
		result, err := tx.DB().NewQuery(`DELETE FROM {{leafen_app_hook_org_secrets}} WHERE [[install_id]] = {:install} AND [[hook]] = {:hook} AND [[tenancy_key]] = {:tenancy}`).
			Bind(dbx.Params{"install": install.ID, "hook": key, "tenancy": "org:" + organizationID}).Execute()
		if err != nil {
			return err
		}
		if count, _ := result.RowsAffected(); count != 1 {
			return refuse(errAppInputInvalid, "the hook has no address of its own for that organization")
		}
		return recordAppAudit(tx, install, "hook-org-removed", actorID, map[string]any{"hook": key, "tenancy": "org:" + organizationID})
	})
}

// appHookSecretFor is the sealed secret a call to the hook proves itself
// with: the organization's when the address names one, else the hook's.
func appHookSecretFor(app core.App, installID, key, tenancy string) (string, error) {
	var sealed string
	var err error
	if tenancy != "" {
		err = app.DB().NewQuery(`SELECT [[sealed]] FROM {{leafen_app_hook_org_secrets}} WHERE [[install_id]] = {:install} AND [[hook]] = {:hook} AND [[tenancy_key]] = {:tenancy}`).
			Bind(dbx.Params{"install": installID, "hook": key, "tenancy": tenancy}).Row(&sealed)
	} else {
		err = app.DB().NewQuery(`SELECT [[sealed]] FROM {{leafen_app_hook_secrets}} WHERE [[install_id]] = {:install} AND [[hook]] = {:hook}`).
			Bind(dbx.Params{"install": installID, "hook": key}).Row(&sealed)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return "", errAppHookRefused
	}
	return sealed, err
}
