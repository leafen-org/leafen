package app

import (
	"slices"
	"strings"
)

// What a configure string says of a view's groups by an option or a status:
// their order, which hide, whether a board colors its columns and whether
// the groups show their counts — as a person sets them on the screen
// (boardGroupSettings.tsx, DatabaseGroupMenu.tsx), and as an agent matches
// them to a Notion view whose API doesn't give them.

// groupKeys are the keys of the groups a view's group by options or status
// has, by their names, and the group for no value's.
func (change *mcpViewConfigure) groupKeys(directive string) (map[string]string, string, error) {
	property, grouped := change.scope.byID[mcpString(change.config["groupBy"])]
	if !grouped {
		return nil, "", mcpInvalid("configure: %s needs the view grouped by an option or status property: GROUP BY it first", directive)
	}
	options := property.Options
	switch property.NativeType {
	case graphValueTypeStatus:
		if change.config["groupByMode"] != "option" {
			options = property.Groups
		}
	case graphValueTypeSelect, graphValueTypeMultiSelect:
	default:
		return nil, "", mcpInvalid("configure: %s reads the groups of an option or status property; %q isn't one", directive, property.Name)
	}
	keys := map[string]string{}
	for _, option := range options {
		keys[option.Name] = option.ID
	}
	none := mcpBoardNoValueGroup
	if change.viewType != graphViewTypeBoard {
		none = mcpTableNoValueGroup
	}
	return keys, none, nil
}

// groupList reads quoted group names and NO VALUE, separated by commas.
func (change *mcpViewConfigure) groupList(words *mcpViewWords, directive string) ([]string, error) {
	keys, none, err := change.groupKeys(directive)
	if err != nil {
		return nil, err
	}
	listed := []string{}
	for {
		key := ""
		if words.keyword("NO", "VALUE") {
			key = none
		} else if name, ok := words.quoted(); !ok {
			return nil, change.unreadable(directive, directive+` "Group", "Group", NO VALUE`)
		} else if key, ok = keys[name]; !ok {
			names := make([]string, 0, len(keys))
			for name := range keys {
				names = append(names, name)
			}
			slices.Sort(names)
			return nil, mcpInvalid("configure: the view has no group %q; it has %s and NO VALUE", name, strings.Join(names, ", "))
		}
		if !slices.Contains(listed, key) {
			listed = append(listed, key)
		}
		if !words.mark(",") {
			return listed, nil
		}
	}
}

// groupSetting reads GROUPS (their order), HIDE GROUPS, SHOW GROUPS, COLOR
// COLUMNS and GROUP COUNTS; it says whether the directive was one of them.
func (change *mcpViewConfigure) groupSetting(words *mcpViewWords) (bool, error) {
	config := change.config
	switch {
	case words.keyword("GROUPS"):
		order, err := change.groupList(words, "GROUPS")
		if err != nil {
			return true, err
		}
		// The groups named come first, the others after in the order they had.
		before, _ := config["groupOrder"].([]any)
		for _, raw := range before {
			if key, ok := raw.(string); ok && !slices.Contains(order, key) {
				order = append(order, key)
			}
		}
		config["groupOrder"], config["groupSort"] = mcpAnyStrings(order), "manual"
	case words.keyword("HIDE", "GROUPS"), words.keyword("SHOW", "GROUPS"):
		hide := strings.EqualFold(words.tokens[words.at-2].text, "HIDE")
		named, err := change.groupList(words, map[bool]string{true: "HIDE GROUPS", false: "SHOW GROUPS"}[hide])
		if err != nil {
			return true, err
		}
		hidden := []string{}
		for _, raw := range mcpAnySlice(config["hiddenGroups"]) {
			if key, ok := raw.(string); ok && !slices.Contains(named, key) {
				hidden = append(hidden, key)
			}
		}
		if hide {
			hidden = append(hidden, named...)
		}
		config["hiddenGroups"] = mcpAnyStrings(hidden)
	case words.keyword("COLOR", "COLUMNS"):
		if err := change.only("COLOR COLUMNS", graphViewTypeBoard); err != nil {
			return true, err
		}
		value, ok := words.oneOf("true", "false")
		if !ok {
			return true, change.unreadable("COLOR COLUMNS", "COLOR COLUMNS true or false")
		}
		delete(config, "boardColumnColors")
		if value == "false" {
			config["boardColumnColors"] = false
		}
	case words.keyword("GROUP", "COUNTS"):
		value, ok := words.oneOf("true", "false")
		if !ok {
			return true, change.unreadable("GROUP COUNTS", "GROUP COUNTS true or false")
		}
		// A board shows its columns' counts until hidden; other views' groups
		// show theirs once asked.
		if change.viewType == graphViewTypeBoard {
			delete(config, "groupCountsHidden")
			delete(config, "hiddenGroupCounts")
			if value == "false" {
				config["groupCountsHidden"] = true
			}
		} else {
			delete(config, "showGroupCounts")
			if value == "true" {
				config["showGroupCounts"] = true
			}
		}
	default:
		return false, nil
	}
	return true, nil
}

func mcpString(value any) string {
	text, _ := value.(string)
	return text
}

func mcpAnySlice(value any) []any {
	items, _ := value.([]any)
	return items
}

func mcpAnyStrings(values []string) []any {
	items := make([]any, 0, len(values))
	for _, value := range values {
		items = append(items, value)
	}
	return items
}
