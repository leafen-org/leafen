//go:build !leafen_noapps

package app

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// setAppStrictRoute puts one organization's apps under strict mode or takes
// them out (the server's own switch stays above it).
func setAppStrictRoute(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	user, _ := authenticatedUser(event)
	input := appStrictInput{}
	if err := json.NewDecoder(event.Request.Body).Decode(&input); err != nil {
		return appPlatformFailure(event, bodyRefusal(errAppInputInvalid, "strict mode choice", err))
	}
	tenancies, err := appTenancies(event.App)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	if refused := appTenancyRefusal("tenancyKey", input.TenancyKey, tenancies); refused != nil {
		// Every app on the server at once goes under it by the server's switch.
		return appPlatformFailure(event, refuse(errAppInputInvalid,
			"%s; every app on the server at once goes under strict mode with strictNetwork in PUT /admin/apps/settings", refusalReason(refused)))
	}
	// A key's call asks its person once the setting reads (user_key_acting_ask.go).
	if asked, err := askBeforeActing(event); asked {
		return err
	}
	err = event.App.RunInTransaction(func(tx core.App) error {
		action := "app_strict.disabled"
		if input.Strict {
			action = "app_strict.enabled"
			if _, err := tx.DB().NewQuery(`INSERT INTO {{leafen_app_strict_tenancies}} ([[tenancy_key]], [[updated_by]], [[updated_at]])
				VALUES ({:tenancy}, {:user}, {:now}) ON CONFLICT ([[tenancy_key]]) DO UPDATE SET [[updated_by]] = excluded.[[updated_by]], [[updated_at]] = excluded.[[updated_at]]`).
				Bind(dbx.Params{"tenancy": input.TenancyKey, "user": user.Id, "now": time.Now().UnixMilli()}).Execute(); err != nil {
				return err
			}
		} else if _, err := tx.DB().NewQuery(`DELETE FROM {{leafen_app_strict_tenancies}} WHERE [[tenancy_key]] = {:tenancy}`).
			Bind(dbx.Params{"tenancy": input.TenancyKey}).Execute(); err != nil {
			return err
		}
		if organizationID := appTenancyOrganization(input.TenancyKey); organizationID != "" {
			return recordOrganizationAudit(tx, organizationID, user.Id, action, "app_strict", organizationID)
		}
		return nil
	})
	if err != nil {
		return appPlatformFailure(event, err)
	}
	if tenancies, err = appTenancies(event.App); err != nil {
		return appPlatformFailure(event, err)
	}
	return event.JSON(http.StatusOK, tenancies)
}
