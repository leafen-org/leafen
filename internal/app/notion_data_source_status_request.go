package app

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/pocketbase/pocketbase/core"
)

// Notion exposes three fixed status groups; native statuses keep them as
// positional groups whose display names may be localized.
var notionStatusGroupNames = []string{"To-do", "In progress", "Complete"}

var notionStatusGroupDefaults = []struct{ color, option, optionColor string }{
	{color: "gray", option: "Not started", optionColor: "default"},
	{color: "blue", option: "In progress", optionColor: "blue"},
	{color: "green", option: "Done", optionColor: "green"},
}

// notionStatusNewConfig is a new status property's options and groups as
// Notion makes them: with no options named, Not started, In progress and Done
// in To-do, In progress and Complete (seen 2026-10-10 through its API), in the
// language of the person whose connection makes them (seen 2026-11-04 through
// its MCP, 시작 전·진행 중·완료); named options go in the group each names, and
// in To-do when it names none. The ids come from idOf, so a caller can make
// the same settings again.
func notionStatusNewConfig(
	named []mcpDatabaseSchemaOption, translate func(string) string, idOf func(kind string, index int) string,
) ([]map[string]any, []map[string]any) {
	if translate == nil {
		translate = func(word string) string { return word }
	}
	groups := make([]map[string]any, 0, len(notionStatusGroupNames))
	for index, name := range notionStatusGroupNames {
		groups = append(groups, map[string]any{
			"id": idOf("group", index), "name": translate(name), "color": notionStatusGroupDefaults[index].color, "optionIds": []any{},
		})
	}
	options := []map[string]any{}
	add := func(group int, name, color string) {
		id := idOf("option", len(options))
		options = append(options, map[string]any{"id": id, "name": name, "color": color, "group": translate(notionStatusGroupNames[group])})
		groups[group]["optionIds"] = append(groups[group]["optionIds"].([]any), id)
	}
	if len(named) == 0 {
		for index, defaults := range notionStatusGroupDefaults {
			add(index, translate(defaults.option), defaults.optionColor)
			if index == 0 {
				notionStatusOptionColorFromGroup(options[len(options)-1])
			}
		}
	}
	for _, option := range named {
		color := option.Color
		if color == "" {
			color = "default"
		}
		add(notionStatusSchemaGroups[option.Group], option.Name, color)
	}
	return options, groups
}

// notionStatusOptionColorFromGroup makes a new status's first option as the
// screen makes it: no color of its own, drawn in its group's gray, as
// Notion's new "Not started" is (seen 2026-11-02); the API still calls its
// color "default". Made "default" here, it drew the lighter pill a chosen
// "default" draws (2026-11-05).
func notionStatusOptionColorFromGroup(option map[string]any) {
	option["color"] = notionStatusGroupDefaults[0].color
	option["colorFromGroup"] = true
}

// notionStatusSchemaGroups are the groups a schema's STATUS names an option in.
var notionStatusSchemaGroups = map[string]int{"": 0, "to_do": 0, "in_progress": 1, "complete": 2}

var nativeStatusGroupAliases = map[string]int{
	"to-do": 0, "to do": 0, "할 일": 0,
	"in progress": 1, "진행 중": 1,
	"complete": 2, "완료": 2,
}

// notionStatusGroupIndex maps a native group to its official position when
// its name is a known default or the property keeps the three default slots.
func notionStatusGroupIndex(groups []map[string]any, index int) int {
	name, _ := groups[index]["name"].(string)
	if position, known := nativeStatusGroupAliases[strings.ToLower(strings.TrimSpace(name))]; known {
		return position
	}
	if len(groups) == len(notionStatusGroupNames) {
		return index
	}
	return -1
}

func notionStatusGroupForRequest(groups []map[string]any, requested string) int {
	position := -1
	for index, name := range notionStatusGroupNames {
		if name == requested {
			position = index
		}
	}
	if position < 0 {
		return -1
	}
	for index := range groups {
		if notionStatusGroupIndex(groups, index) == position {
			return index
		}
	}
	return -1
}

func notionStatusConfigMaps(values any) ([]map[string]any, error) {
	if values == nil {
		return []map[string]any{}, nil
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return nil, err
	}
	result := []map[string]any{}
	if json.Unmarshal(encoded, &result) != nil {
		return nil, errNotionDatabaseRowPageProjectionInvalid
	}
	return result, nil
}

// notionStatusRequestOption is one status option an API request names.
type notionStatusRequestOption struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Color       string  `json:"color"`
	Description *string `json:"description"`
	Group       string  `json:"group"`
	// The first of the options a new status gets, colored by its group.
	fromGroup bool
}

// notionDataSourceStatusRequestConfig gives a status the options, groups and
// default an API request asks for, as Notion's API gives them (seen
// 2026-11-08, 2022-06-28 and 2025-09-03 alike). Named options are all the
// status keeps, in the order named: one named by id is that one (a name
// beside it is not taken), one named by name is the one so named in any case
// (its name stays), any other is new, "default" in color and in To-do unless
// it names a group; a color, description or group named changes the one it
// is on, and one named twice is refused. The default stays while it is
// among them, else the first is; with none the status has none, and the
// rows whose option went read the default (databaseStatusValueOrDefault).
// Named no options, a status keeps what it has, and a new one gets Not
// started, In progress and Done, the first its default. Made a status from
// another type (former, its options then), it is a new one as above: the
// options it had are not kept, but one named as one of them, in any case,
// takes that one's ID, so the rows that set it keep it. Keys beside the
// options are left, and refusals say what Notion's say (seen 2026-11-12):
// names with commas, a name twice, an ID the status doesn't have.
func notionDataSourceStatusRequestConfig(
	propertyName, label string,
	object map[string]json.RawMessage,
	current map[string]any,
	former []map[string]any,
) (map[string]any, error) {
	options, groups := []map[string]any{}, []map[string]any{}
	if former == nil {
		var err error
		if options, err = notionStatusConfigMaps(current["options"]); err != nil {
			return nil, err
		}
		if groups, err = notionStatusConfigMaps(current["statusGroups"]); err != nil {
			return nil, err
		}
	}
	fresh := len(groups) == 0 && len(options) == 0
	if len(groups) == 0 {
		for index, name := range notionStatusGroupNames {
			groups = append(groups, map[string]any{
				"id": core.GenerateDefaultRandomId(), "name": name,
				"color": notionStatusGroupDefaults[index].color, "optionIds": []any{},
			})
		}
	}
	optionGroup := map[string]int{}
	for groupIndex, group := range groups {
		ids, _ := group["optionIds"].([]any)
		for _, value := range ids {
			if id, ok := value.(string); ok {
				optionGroup[id] = groupIndex
			}
		}
	}
	defaultGroup := max(notionStatusGroupForRequest(groups, "To-do"), 0)
	result := map[string]any{}
	requested := []notionStatusRequestOption{}
	encoded, named := object["options"]
	if named {
		if json.Unmarshal(encoded, &requested) != nil || len(requested) > maxNotionDatabaseRowPageOptions {
			return nil, notionDatabaseRowPageRequestInvalid("database property %q status options are invalid", propertyName)
		}
		names := make([]string, len(requested))
		for index, item := range requested {
			names[index] = item.Name
		}
		if err := notionSchemaOptionNamesRefusal("status", label, names, current); err != nil {
			return nil, err
		}
	} else if fresh {
		named = true
		for index, defaults := range notionStatusGroupDefaults {
			requested = append(requested, notionStatusRequestOption{
				Name: defaults.option, Color: defaults.optionColor, Group: notionStatusGroupNames[index], fromGroup: index == 0,
			})
		}
	} else if kept, present := current["defaultOptionId"]; present {
		result["defaultOptionId"] = kept
	}
	if named {
		before := ""
		if former == nil {
			if encoded, err := json.Marshal(current); err == nil {
				before = databaseStatusConfigFactsOf(string(encoded)).fallback
			}
		}
		var err error
		if options, err = notionStatusRequestedOptions(propertyName, requested, options, former, groups, optionGroup, defaultGroup); err != nil {
			return nil, err
		}
		delete(result, "defaultOptionId")
		for _, option := range options {
			if option["id"] == before {
				result["defaultOptionId"] = before
			}
		}
		if _, kept := result["defaultOptionId"]; !kept && len(options) > 0 {
			result["defaultOptionId"] = options[0]["id"]
		}
	}
	names := map[string]struct{}{}
	for _, option := range options {
		name, _ := option["name"].(string)
		if strings.TrimSpace(name) == "" || utf8.RuneCountInString(name) > maxGraphIdentifierLength {
			return nil, notionDatabaseRowPageRequestInvalid("database property %q status option is invalid", propertyName)
		}
		if _, duplicate := names[name]; duplicate {
			return nil, notionSchemaOptionDuplicate("status", name)
		}
		names[name] = struct{}{}
	}
	if len(options) > maxNotionDatabaseRowPageOptions {
		return nil, notionDatabaseRowPageRequestInvalid("database property %q has too many options", propertyName)
	}
	for _, group := range groups {
		group["optionIds"] = []any{}
	}
	for _, option := range options {
		id, _ := option["id"].(string)
		groupIndex, grouped := optionGroup[id]
		if !grouped {
			groupIndex = defaultGroup
		}
		groups[groupIndex]["optionIds"] = append(groups[groupIndex]["optionIds"].([]any), id)
		option["group"] = groups[groupIndex]["name"]
	}
	result["options"], result["statusGroups"] = options, groups
	return result, nil
}

// notionStatusRequestedOptions are the options a request names, in its
// order, with their groups set in optionGroup (notionDataSourceStatusRequestConfig).
func notionStatusRequestedOptions(
	propertyName string,
	requested []notionStatusRequestOption,
	options, former, groups []map[string]any,
	optionGroup map[string]int,
	defaultGroup int,
) ([]map[string]any, error) {
	pool := append([]map[string]any{}, options...)
	taken, lent := map[string]bool{}, map[string]bool{}
	next := make([]map[string]any, 0, len(requested))
	for _, item := range requested {
		item.ID, item.Name, item.Color = strings.TrimSpace(item.ID), strings.TrimSpace(item.Name), strings.TrimSpace(item.Color)
		var option map[string]any
		if item.ID != "" {
			for _, candidate := range options {
				if candidate["id"] == item.ID {
					option = candidate
				}
			}
			if option == nil {
				return nil, notionSchemaOptionMissing(item.ID)
			}
		} else if item.Name == "" {
			return nil, notionDatabaseRowPageRequestInvalid("database property %q status option is invalid", propertyName)
		} else {
			option = notionStatusOptionNamed(pool, item.Name, nil)
		}
		if option == nil {
			id := core.GenerateDefaultRandomId()
			if earlier := notionStatusOptionNamed(former, item.Name, lent); earlier != nil {
				id, _ = earlier["id"].(string)
				lent[id] = true
			}
			option = map[string]any{"id": id, "name": item.Name, "color": "default"}
			optionGroup[id] = defaultGroup
			pool = append(pool, option)
		}
		id, _ := option["id"].(string)
		if taken[id] {
			named := item.Name
			if named == "" {
				named, _ = option["name"].(string)
			}
			return nil, notionSchemaOptionDuplicate("status", named)
		}
		taken[id] = true
		if item.Color != "" {
			if _, valid := notionDatabaseRowPageOptionColors[item.Color]; !valid {
				return nil, notionDatabaseRowPageRequestInvalid("database property %q status option color is invalid", propertyName)
			}
			// "default" is what the API calls a color taken from the group:
			// sent back as read, it stays the group's.
			if fromGroup, _ := option["colorFromGroup"].(bool); !fromGroup || item.Color != "default" {
				option["color"] = item.Color
				delete(option, "colorFromGroup")
			}
		}
		if item.fromGroup {
			notionStatusOptionColorFromGroup(option)
		}
		if item.Description != nil {
			option["description"] = *item.Description
		}
		if item.Group != "" {
			groupIndex := notionStatusGroupForRequest(groups, item.Group)
			if groupIndex < 0 {
				return nil, notionDatabaseRowPageRequestInvalid("database property %q status group is invalid", propertyName)
			}
			optionGroup[id] = groupIndex
		}
		next = append(next, option)
	}
	return next, nil
}

// notionStatusOptionNamed is the option a name names, as Notion finds it: the
// one so named, else the first so named in any case; skip leaves some out.
func notionStatusOptionNamed(options []map[string]any, name string, skip map[string]bool) map[string]any {
	var folded map[string]any
	for _, option := range options {
		id, _ := option["id"].(string)
		own, _ := option["name"].(string)
		if id == "" || skip[id] {
			continue
		}
		if own == name {
			return option
		}
		if folded == nil && strings.EqualFold(own, name) {
			folded = option
		}
	}
	return folded
}

// notionDataSourceConvertedConfig carries the old configuration into a
// compatible type before the requested settings apply.
func notionDataSourceConvertedConfig(currentType, nextType string, config map[string]any) map[string]any {
	if currentType == nextType {
		return config
	}
	result := map[string]any{}
	keepDatabasePropertyOwnSettings(config, result)
	_, fromOptions := databasePropertyConversionOptionTypes[currentType]
	_, toOptions := databasePropertyConversionOptionTypes[nextType]
	if fromOptions && toOptions {
		preserved := []any{}
		for _, option := range databasePropertyConversionOptions(config) {
			if nextType != graphValueTypeStatus {
				delete(option, "group")
			}
			preserved = append(preserved, option)
		}
		result["options"] = preserved
	}
	return result
}
