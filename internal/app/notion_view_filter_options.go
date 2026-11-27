package app

import (
	"encoding/json"
	"fmt"
	"strings"
)

// A filter on a select, multi-select or status names its options as Notion's
// API does, by name, and a status filter may name a whole group by its name
// (seen 2026-11-11: "Complete" keeps the rows in that group). A view keeps
// them by ID, as the app writes them, so it reads back by name and a filter
// the API wrote is the same option in the app.

// notionViewOption is an option or a status group: its ID and the name
// Notion's API gives it.
type notionViewOption struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// notionViewPropertyOptions are a select's, multi-select's or status's
// options, and a status's groups; nil for any other property.
func notionViewPropertyOptions(nativeType, rawConfig string) (options, groups []notionViewOption) {
	switch nativeType {
	case graphValueTypeSelect, graphValueTypeMultiSelect, graphValueTypeStatus:
	default:
		return nil, nil
	}
	config := struct {
		Options      []notionViewOption `json:"options"`
		StatusGroups []map[string]any   `json:"statusGroups"`
	}{}
	_ = json.Unmarshal([]byte(rawConfig), &config)
	options = make([]notionViewOption, 0, len(config.Options))
	for _, option := range config.Options {
		if option.ID = strings.TrimSpace(option.ID); option.ID != "" {
			options = append(options, notionViewOption{ID: option.ID, Name: strings.TrimSpace(option.Name)})
		}
	}
	if nativeType != graphValueTypeStatus {
		return options, nil
	}
	// A group by the name the data source's status gives it (To-do, In
	// progress, Complete), as notionDataSourceStatusConfig does.
	for index, group := range config.StatusGroups {
		id, _ := group["id"].(string)
		name, _ := group["name"].(string)
		if position := notionStatusGroupIndex(config.StatusGroups, index); position >= 0 {
			name = notionStatusGroupNames[position]
		}
		if id != "" {
			groups = append(groups, notionViewOption{ID: id, Name: name})
		}
	}
	return options, groups
}

// nativeOptionValues are the options a filter names, by their IDs: an
// option by its name, then a status's group by its name. A name the property
// has neither for is refused where the scope refuses them (the API's), as
// Notion's API refuses it, in Leafen's words, else kept as written (a Notion
// import's).
func (scope notionViewFilterScope) nativeOptionValues(property notionViewProperty, values []any) ([]any, error) {
	if property.Options == nil {
		// A rollup's values over a select are its options, as the app writes
		// them; a name none has is kept as written.
		ids := make([]any, len(values))
		for index, value := range values {
			name, _ := value.(string)
			ids[index] = value
			if id, found := notionViewOptionNamed(property.RolledOptions, name); found {
				ids[index] = id
			}
		}
		return ids, nil
	}
	ids := make([]any, 0, len(values))
	for _, value := range values {
		name, _ := value.(string)
		id, found := notionViewOptionNamed(property.Options, name)
		if !found {
			id, found = notionViewOptionNamed(property.Groups, name)
		}
		if !found && scope.refusesUnknownOptions {
			names := make([]string, 0, len(property.Options))
			for _, option := range property.Options {
				names = append(names, fmt.Sprintf("%q", option.Name))
			}
			// As Notion's says it (seen 2026-11-11), its groups left unlisted.
			return nil, notionViewFilterInvalid("Property %[3]q has no %[1]s option %[2]q; its options are %[4]s.",
				property.OfficialType, name, property.Name, strings.Join(names, ", "))
		}
		if !found {
			id = name
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// officialOptionValues are the options a stored filter names, by name: an
// option's or a status group's ID by its name, anything else as stored.
func (property notionViewProperty) officialOptionValues(value any) any {
	if property.Options == nil && property.RolledOptions == nil {
		return value
	}
	if property.Options == nil {
		// A rollup's values over a select read by the options' names.
		property.Options = property.RolledOptions
	}
	name := func(item any) any {
		id, _ := item.(string)
		for _, list := range [][]notionViewOption{property.Options, property.Groups} {
			for _, option := range list {
				if option.ID == id {
					return option.Name
				}
			}
		}
		return item
	}
	if values, listed := value.([]any); listed {
		named := make([]any, len(values))
		for index, item := range values {
			named[index] = name(item)
		}
		return named
	}
	return name(value)
}

func notionViewOptionNamed(options []notionViewOption, name string) (string, bool) {
	for _, option := range options {
		if option.Name == name {
			return option.ID, true
		}
	}
	return "", false
}
