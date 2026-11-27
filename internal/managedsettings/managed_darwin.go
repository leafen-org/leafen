//go:build darwin && !ios

package managedsettings

import (
	"encoding/json"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
)

// platformValues reads the configuration profile's settings for the app, a
// user's own over the computer's.
func platformValues() (map[string]any, bool) {
	paths := []string{filepath.Join("/Library/Managed Preferences", BundleID+".plist")}
	if current, err := user.Current(); err == nil {
		paths = append(paths, filepath.Join("/Library/Managed Preferences", current.Username, BundleID+".plist"))
	}
	values, found := map[string]any{}, false
	for _, path := range paths {
		if _, err := os.Stat(path); err != nil {
			continue
		}
		output, err := exec.Command("/usr/bin/plutil", "-convert", "json", "-o", "-", path).Output()
		if err != nil {
			continue
		}
		read := map[string]any{}
		if json.Unmarshal(output, &read) != nil {
			continue
		}
		found = true
		for key, value := range read {
			values[key] = value
		}
	}
	return values, found
}
