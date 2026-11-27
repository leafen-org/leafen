package app

import (
	"database/sql"
	"errors"
	"net"
	"regexp"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Addresses the administrator sets (the mail decision of 2026-10-07): an
// app that must reach a server each organization has its own of — a mail
// server — declares a setting, and its socket names it as {{setting:KEY}}.
// The approval lists it as an address the administrator sets; the
// administrator gives it when installing or later, and only then does the
// socket open, to that address alone, through the same checks as any other.

const (
	appSettingTypeAddress = "address"
	appSettingValueRunes  = 255
)

var (
	appSettingPlaceholder = regexp.MustCompile(`^\{\{setting:([a-z][a-zA-Z0-9_-]{0,47})\}\}$`)
	appSettingHostPattern = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,62}[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]{0,62}[A-Za-z0-9])?)*$`)
	errAppSettingValue    = errors.New("an address is a host name or IP address with an optional port")
)

type appManifestSetting struct {
	Key  string            `json:"key"`
	Type string            `json:"type"`
	Name map[string]string `json:"name"`
	Help map[string]string `json:"help,omitempty"`
	// The port an address without one uses.
	DefaultPort int `json:"defaultPort,omitzero"`
}

// appSettingName is the setting a value names as {{setting:KEY}}.
func appSettingName(value string) (string, bool) {
	match := appSettingPlaceholder.FindStringSubmatch(strings.TrimSpace(value))
	if match == nil {
		return "", false
	}
	return match[1], true
}

func appSettingProblems(manifest appManifest, add func(string, ...any)) {
	declared := map[string]appManifestSetting{}
	for _, setting := range manifest.Settings {
		if !appKeyPattern.MatchString(setting.Key) || declared[setting.Key].Key != "" {
			add("settings key %q is invalid or repeated", setting.Key)
		}
		if setting.Type != appSettingTypeAddress {
			add("settings %q type is address", setting.Key)
		}
		if len(setting.Name) == 0 {
			add("settings %q needs a name", setting.Key)
		}
		if setting.DefaultPort < 1 || setting.DefaultPort > 65535 {
			add("settings %q needs a defaultPort 1-65535", setting.Key)
		}
		declared[setting.Key] = setting
	}
	for _, socket := range manifest.Network.Sockets {
		key, named := appSettingName(socket.Host)
		if !named {
			continue
		}
		if declared[key].Key == "" {
			add("network.sockets names setting %q, which settings doesn't declare", key)
		}
		if socket.Port != 0 {
			add("network.sockets with {{setting:%s}} takes its port from the setting; leave port out", key)
		}
	}
}

// parseAppSettingAddress reads "host" or "host:port" ("[v6]:port").
func parseAppSettingAddress(value string, defaultPort int) (string, int, error) {
	value = strings.TrimSpace(value)
	host, port := value, defaultPort
	if strings.HasPrefix(value, "[") || strings.Count(value, ":") == 1 {
		parsedHost, parsedPort, err := net.SplitHostPort(value)
		if err != nil {
			return "", 0, errAppSettingValue
		}
		number, err := strconv.Atoi(parsedPort)
		if err != nil || number < 1 || number > 65535 {
			return "", 0, errAppSettingValue
		}
		host, port = parsedHost, number
	}
	if ip := net.ParseIP(host); ip == nil && !appSettingHostPattern.MatchString(host) || len(value) > appSettingValueRunes {
		return "", 0, errAppSettingValue
	}
	return strings.ToLower(host), port, nil
}

// loadAppSettingValues reads what the administrator set for an install.
func loadAppSettingValues(app core.App, installID string) (map[string]string, error) {
	rows := []struct {
		Key   string `db:"key"`
		Value string `db:"value"`
	}{}
	err := app.DB().NewQuery(`SELECT [[key]], [[value]] FROM {{leafen_app_setting_values}} WHERE [[install_id]] = {:install}`).
		Bind(dbx.Params{"install": installID}).All(&rows)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	values := map[string]string{}
	for _, row := range rows {
		values[row.Key] = row.Value
	}
	return values, err
}

// appManifestWithSettings gives the install's manifest with each socket
// that names a setting at the address the administrator set — or without
// it, while none is set.
func appManifestWithSettings(manifest appManifest, values map[string]string) appManifest {
	if len(manifest.Settings) == 0 {
		return manifest
	}
	declared := map[string]appManifestSetting{}
	for _, setting := range manifest.Settings {
		declared[setting.Key] = setting
	}
	sockets := make([]appManifestSocket, 0, len(manifest.Network.Sockets))
	for _, socket := range manifest.Network.Sockets {
		key, named := appSettingName(socket.Host)
		if !named {
			sockets = append(sockets, socket)
			continue
		}
		setting, ok := declared[key]
		value := values[key]
		if !ok || value == "" {
			continue
		}
		host, port, err := parseAppSettingAddress(value, setting.DefaultPort)
		if err != nil {
			continue
		}
		socket.Host, socket.Port, socket.Setting = host, port, key
		sockets = append(sockets, socket)
	}
	resolved := manifest
	resolved.Network.Sockets = sockets
	return resolved
}
