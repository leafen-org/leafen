// Package managedsettings reads what an organization sets on its computers
// through device management (the agent decision of 2026-10-19 10:11): the
// Leafen server they use. Each system keeps them where only an administrator
// writes — macOS configuration profiles, Windows policy — and a computer
// with any is a managed one. That is the computer's own word: someone with
// administrator rights on it could write the same, so it marks a computer
// managed, it doesn't prove it.
package managedsettings

import (
	"encoding/json"
	"net/url"
	"os"
	"strings"
)

// BundleID names the app's settings in a macOS configuration profile.
const BundleID = "org.leafen.app"

// Settings are the managed values; empty where the organization set none.
type Settings struct {
	// Some source of them exists: the computer is managed.
	Managed bool `json:"managed"`
	// The server the app opens, the only one offered.
	ServerURL string `json:"serverUrl,omitempty"`
}

// TestFileEnvironment names a JSON file of the same keys that a build made
// with the leafen_managed_test tag reads where the system keeps none, to try
// them without a profile; other builds never read it.
const TestFileEnvironment = "LEAFEN_MANAGED_SETTINGS_FILE"

// Read gives the computer's managed settings.
func Read() Settings {
	values, found := platformValues()
	if !found && testFileRead {
		if path := strings.TrimSpace(os.Getenv(TestFileEnvironment)); path != "" {
			values, found = jsonValues(path)
		}
	}
	if !found {
		return Settings{}
	}
	settings := Settings{Managed: true}
	if address, ok := values["ServerURL"].(string); ok {
		if parsed, err := url.Parse(strings.TrimSpace(address)); err == nil && (parsed.Scheme == "https" || parsed.Scheme == "http") && parsed.Host != "" {
			settings.ServerURL = strings.TrimRight(parsed.Scheme+"://"+parsed.Host+parsed.Path, "/")
		}
	}
	return settings
}

func jsonValues(path string) (map[string]any, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	values := map[string]any{}
	if json.Unmarshal(data, &values) != nil {
		return nil, false
	}
	return values, true
}
