package app

import "encoding/json"

// databasePageLayoutViewSettingsBytes is the size of a tab's view settings,
// its own order of pages (rowOrder) apart, as a My tasks view's order is
// apart from its settings (my_task_views.go): the order holds every page a
// tab's view placed, from every page the tab shows on, and is bounded with
// the layout by the request. Counted among the settings, a tab refused any
// move once about 900 pages had a place (2026-11-24, 1,200 pages: "is over
// 16384 bytes").
func databasePageLayoutViewSettingsBytes(config json.RawMessage) int {
	settings := map[string]json.RawMessage{}
	if json.Unmarshal(config, &settings) != nil {
		return len(config)
	}
	return len(config) - len(settings["rowOrder"])
}
