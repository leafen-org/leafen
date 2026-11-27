package app

import "encoding/json"

// The settings that are a property's own whatever its type: its
// description and icon, how its pages show it and its place in its
// database's page order. Notion's keep them through a change of type (seen
// 2026-11-06: a number's description through text and back) and through any
// change its API makes, which names none of them but the description.
var databasePropertyOwnSettingKeys = [...]string{
	"description", "icon", "hideWhenEmpty", "hideInPagePanel", "pagePosition",
}

// keepDatabasePropertyOwnSettings sets on onto the own settings from has.
func keepDatabasePropertyOwnSettings(from, onto map[string]any) {
	for _, key := range databasePropertyOwnSettingKeys {
		if value, present := from[key]; present {
			onto[key] = value
		}
	}
}

// databasePropertyTakenBackWithOwnSettings is the settings a property changed
// back to a type it had takes back, with the own settings it was sent with:
// those it had then are not its own now (a line moved on its pages since, a
// description written or removed).
func databasePropertyTakenBackWithOwnSettings(taken, asked json.RawMessage) (json.RawMessage, error) {
	config, sent := map[string]any{}, map[string]any{}
	if json.Unmarshal(taken, &config) != nil || config == nil {
		return taken, nil
	}
	if len(asked) > 0 && json.Unmarshal(asked, &sent) != nil {
		return taken, nil
	}
	for _, key := range databasePropertyOwnSettingKeys {
		delete(config, key)
	}
	keepDatabasePropertyOwnSettings(sent, config)
	return json.Marshal(config)
}
