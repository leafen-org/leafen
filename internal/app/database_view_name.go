package app

import (
	"encoding/json"
	"unicode/utf8"
)

// A view's name is kept as it was given, spaces and all, and a view given
// the empty name stays apart from one never named, though both are stored
// as "": the first keeps viewNamedBlankKey in its settings. Notion's API
// reads the first as "" and the second as "Untitled", its MCP gives "" for
// both, and its app shows both by their layout's name (seen 2026-11-11: a
// name cleared in Notion's view settings or given as "" through its API
// reads "", a view added from its menus and never named reads "Untitled",
// and "   " stays "   " and shows an empty tab).
const viewNamedBlankKey = "namedBlank"

// viewNeverNamed says whether a view with this name and these settings was
// never named. It is the one rule the server and the app read a view's name
// by: a view never named is stored as "", and every other name is the
// view's own, its layout's name too, as Notion's view settings show a board
// its API named "보드" (seen 2026-11-12) and a CSV's table named "표" (seen
// 2026-11-17). Each reader sees a view never named by its layout's name in
// their own language (the app's viewDisplayName).
func viewNeverNamed(name string, settings map[string]any) bool {
	return name == "" && settings[viewNamedBlankKey] != true
}

// viewNameValid says whether a name given to a view is one to keep.
func viewNameValid(name string) bool {
	return utf8.RuneCountInString(name) <= maxGraphIdentifierLength
}

// withViewNameGiven marks a view's settings as named name: the empty name
// keeps the blank mark and any other drops it.
func withViewNameGiven(config json.RawMessage, name string) (json.RawMessage, error) {
	settings := map[string]any{}
	if err := json.Unmarshal(config, &settings); err != nil || settings == nil {
		return nil, errNotionDatabaseRowPageProjectionInvalid
	}
	_, marked := settings[viewNamedBlankKey]
	switch {
	case name == "" && !marked:
		settings[viewNamedBlankKey] = true
	case name != "" && marked:
		delete(settings, viewNamedBlankKey)
	default:
		return config, nil
	}
	return json.Marshal(settings)
}
