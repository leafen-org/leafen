//go:build windows

package managedsettings

import "golang.org/x/sys/windows/registry"

// platformValues reads the app's policy, which group policy or device
// management writes under HKLM\SOFTWARE\Policies\Leafen.
func platformValues() (map[string]any, bool) {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Policies\Leafen`, registry.QUERY_VALUE)
	if err != nil {
		return nil, false
	}
	defer key.Close()
	values := map[string]any{}
	if text, _, err := key.GetStringValue("ServerURL"); err == nil {
		values["ServerURL"] = text
	}
	return values, true
}
