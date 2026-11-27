//go:build !(darwin && !ios) && !windows

package managedsettings

func platformValues() (map[string]any, bool) { return nil, false }
