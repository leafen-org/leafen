//go:build !leafen_noapps

package app

import (
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

func registerAppSettingRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+appPlatformSettingsListPath, listAppSettingsRoute)
	event.Router.PUT(apiRoot+appPlatformSettingSetPath, setAppSettingRoute).Bind(apis.BodyLimit(appPlatformInputMaxBytes))
}

func listAppSettings(app core.App, install storedAppInstall, manifest appManifest) (appSettingListResponse, error) {
	values, err := loadAppSettingValues(app, install.ID)
	response := appSettingListResponse{Settings: []appSettingWire{}}
	for _, setting := range manifest.Settings {
		response.Settings = append(response.Settings, appSettingWire{
			Key: setting.Key, Type: setting.Type, Name: setting.Name, Help: setting.Help, DefaultPort: setting.DefaultPort, Value: values[setting.Key],
		})
	}
	return response, err
}

func listAppSettingsRoute(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	install, manifest, err := adminAppInstall(event)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	response, err := listAppSettings(event.App, install, manifest)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	return event.JSON(http.StatusOK, response)
}

// setAppSettingRoute keeps an address the administrator gives (an empty
// one clears it); only a server administrator sets it, never the app.
func setAppSettingRoute(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	user, _ := authenticatedUser(event)
	install, manifest, err := adminAppInstall(event)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	key := event.Request.PathValue("key")
	var declared *appManifestSetting
	for index := range manifest.Settings {
		if manifest.Settings[index].Key == key {
			declared = &manifest.Settings[index]
		}
	}
	input := appSecretInput{}
	if declared == nil {
		return appPlatformFailure(event, refuse(errAppInputInvalid, "the app has no setting named %q", key))
	}
	if err := json.NewDecoder(event.Request.Body).Decode(&input); err != nil {
		return appPlatformFailure(event, bodyRefusal(errAppInputInvalid, "setting", err))
	}
	value := strings.TrimSpace(input.Value)
	if value != "" {
		host, port, err := parseAppSettingAddress(value, declared.DefaultPort)
		if err != nil {
			return appPlatformFailure(event, refuse(errAppInputInvalid,
				"value is a host name or IP address of at most %d characters, with ':' and a port from 1 to 65535 when it isn't %d", appSettingValueRunes, declared.DefaultPort))
		}
		value = net.JoinHostPort(host, strconv.Itoa(port))
	}
	err = event.App.RunInTransaction(func(tx core.App) error {
		if value == "" {
			if _, err := tx.DB().Delete("leafen_app_setting_values", dbx.HashExp{"install_id": install.ID, "key": key}).Execute(); err != nil {
				return err
			}
			return recordAppAudit(tx, install, "setting-clear", user.Id, map[string]any{"key": key})
		}
		if _, err := tx.DB().NewQuery(`INSERT INTO {{leafen_app_setting_values}}
			([[install_id]], [[key]], [[value]], [[updated_by]], [[updated_at]]) VALUES ({:install}, {:key}, {:value}, {:actor}, {:now})
			ON CONFLICT ([[install_id]], [[key]]) DO UPDATE SET [[value]] = excluded.[[value]], [[updated_by]] = excluded.[[updated_by]],
				[[updated_at]] = excluded.[[updated_at]]`).Bind(dbx.Params{
			"install": install.ID, "key": key, "value": value, "actor": user.Id, "now": time.Now().UnixMilli(),
		}).Execute(); err != nil {
			return err
		}
		return recordAppAudit(tx, install, "setting-set", user.Id, map[string]any{"key": key, "value": value})
	})
	if err != nil {
		return appPlatformFailure(event, err)
	}
	return listAppSettingsRoute(event)
}
