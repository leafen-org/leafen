package app

import (
	"encoding/json"

	"github.com/pocketbase/pocketbase/core"
)

// databaseStatusConfigMadeFrom gives a property made a status from another
// type the settings Notion's "유형 변경" gives it (seen 2026-11-08): a new
// status's three groups and options, in the words of the person who changes
// it. A select's or multi-select's options come along into "To-do" after its
// first, each in place, one named as a new option is taking that one's place
// and group with its own color, and such a status has no default
// (default_option null), so its rows that set none read empty; any other
// type's status reads its first option while a row sets none. Settings asked
// for with their groups already (the screen's statusConfigMadeFrom makes the
// same) are kept: it returns nil for them.
func databaseStatusConfigMadeFrom(fromType string, requested json.RawMessage, translate func(string) string) (json.RawMessage, error) {
	config := map[string]any{}
	if len(requested) > 0 && json.Unmarshal(requested, &config) != nil {
		return nil, nil
	}
	if groups, _ := config["statusGroups"].([]any); len(groups) > 0 {
		return nil, nil
	}
	_, carries := databaseStatusCarriedFrom[fromType]
	if carries {
		databaseStatusShaped(config, translate)
		config["defaultOptionId"] = ""
	} else {
		delete(config, "options")
		delete(config, "defaultOptionId")
		databaseStatusShaped(config, translate)
	}
	return json.Marshal(config)
}

// The types whose options a status made from them keeps.
var databaseStatusCarriedFrom = map[string]struct{}{
	graphValueTypeSelect: {}, graphValueTypeMultiSelect: {},
}

// databaseStatusShaped gives a status's settings, its options as they are,
// Notion's three groups and options with the options it has merged in: into
// "To-do" after its first, one named as a new option taking that one's place
// (one drawn in its group's color, as a status's made a select is, still is).
func databaseStatusShaped(config map[string]any, translate func(string) string) {
	made, groups := notionStatusNewConfig(nil, translate, func(string, int) string { return core.GenerateDefaultRandomId() })
	taken := map[int]map[string]any{}
	rest := []map[string]any{}
	for _, option := range databasePropertyConversionOptions(config) {
		if id, _ := option["id"].(string); id == "" {
			continue
		}
		name, _ := option["name"].(string)
		at := -1
		for index, candidate := range made {
			if candidate["name"] == name && taken[index] == nil {
				at = index
				break
			}
		}
		if at >= 0 {
			taken[at] = option
		} else {
			rest = append(rest, option)
		}
	}
	placed := make([]map[string]any, len(made))
	for index, option := range made {
		own := taken[index]
		if own == nil {
			placed[index] = option
			continue
		}
		own["group"] = option["group"]
		if color, _ := own["color"].(string); color == "" {
			own["color"] = option["color"]
		}
		placed[index] = own
	}
	options := []any{placed[0]}
	toDo := []any{placed[0]["id"]}
	for _, option := range rest {
		delete(option, "colorFromGroup")
		option["group"] = groups[0]["name"]
		if color, _ := option["color"].(string); color == "" {
			option["color"] = "default"
		}
		options = append(options, option)
		toDo = append(toDo, option["id"])
	}
	groups[0]["optionIds"] = toDo
	for index := 1; index < len(placed); index++ {
		options = append(options, placed[index])
		groups[index]["optionIds"] = []any{placed[index]["id"]}
	}
	shapedGroups := make([]any, len(groups))
	for index, group := range groups {
		shapedGroups[index] = group
	}
	config["options"] = options
	config["statusGroups"] = shapedGroups
}
