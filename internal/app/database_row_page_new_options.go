package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A database page adds, with a value, the select, multi-select or status
// options the value names that its property doesn't have yet, as Notion's
// property menus make an option of what was typed: the property takes them
// in the same change as the value. A status option joins its group. Only
// options the value names are added, and a name the property already has
// is refused.

const (
	// As many as one value names (Notion takes 100 new names in one write,
	// seen 2026-10-13).
	maxDatabaseRowPageNewOptions        = 100
	maxDatabaseRowPageNewOptionIDBytes  = 64
	maxDatabaseRowPageNewOptionNameRune = 100
)

// planDatabaseRowPageNewOptions is the change to the properties that the
// new options of a database page's values make.
func planDatabaseRowPageNewOptions(
	app core.App,
	authority databaseRowPageAuthority,
	options map[string][]databaseRowPageNewOption,
	values map[string]graphRowValue,
) ([]graphOperation, error) {
	propertyIDs := make([]string, 0, len(options))
	for propertyID := range options {
		propertyIDs = append(propertyIDs, propertyID)
	}
	sort.Strings(propertyIDs)
	operations := []graphOperation{}
	for _, propertyID := range propertyIDs {
		added := options[propertyID]
		value, valued := values[propertyID]
		if !valued || len(added) == 0 || len(added) > maxDatabaseRowPageNewOptions {
			return nil, refuse(errDatabaseRowPageValuesInvalid,
				"options.%s holds 1–%d new options of a value values.%s sets", propertyID, maxDatabaseRowPageNewOptions, propertyID)
		}
		property := struct {
			Type     string `db:"type"`
			Name     string `db:"name"`
			Position string `db:"position"`
			Config   string `db:"config"`
		}{}
		if err := app.DB().NewQuery(`
			SELECT [[type]], [[name]], [[position]], [[config]] FROM {{leafen_database_properties}}
			WHERE [[owner]]={:ownerId} AND [[workspace_id]]={:workspaceId} AND [[page_id]]={:pageId}
				AND [[data_source_id]]={:dataSourceId} AND [[id]]={:propertyId}
				AND ` + databasePropertyNotTrashedSQL("[[id]]") + `
		`).Bind(dbx.Params{
			"ownerId": authority.OwnerID, "workspaceId": authority.WorkspaceID,
			"pageId": authority.ContainerPageID, "dataSourceId": authority.DataSourceID, "propertyId": propertyID,
		}).One(&property); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, refuse(errDatabaseRowPageValuesInvalid, "options.%s names no property of the row's database", propertyID)
			}
			return nil, err
		}
		named, ok := databaseRowPageNamedOptions(property.Type, value)
		if !ok {
			return nil, refuse(errDatabaseRowPageValuesInvalid,
				"options.%s: column %q is a %s, which takes no options", propertyID, property.Name, property.Type)
		}
		config := map[string]any{}
		if property.Config != "" && json.Unmarshal([]byte(property.Config), &config) != nil {
			return nil, errDatabaseRowPageValuesInvalid
		}
		current, _ := config["options"].([]any)
		ids, names := map[string]bool{}, map[string]bool{}
		for _, raw := range current {
			if option, ok := raw.(map[string]any); ok {
				id, _ := option["id"].(string)
				name, _ := option["name"].(string)
				ids[id], names[strings.TrimSpace(name)] = true, true
			}
		}
		for _, option := range added {
			id, name, color := strings.TrimSpace(option.ID), strings.TrimSpace(option.Name), strings.TrimSpace(option.Color)
			if color == "" {
				color = "default"
			}
			if _, validColor := notionDatabaseRowPageOptionColors[color]; !validColor {
				return nil, refuse(errDatabaseRowPageValuesInvalid, "options.%s: column %q: option %s has color %s, which isn't one — "+
					"default, gray, brown, orange, yellow, green, blue, purple, pink or red", propertyID, property.Name, strconvQuoteJS(name), strconvQuoteJS(color))
			}
			if code, reason := databaseRowPageNewOptionReason(property.Name, id, name, named[id], ids[id], names[name]); code != "" {
				return nil, refusedFor(http.StatusBadRequest, code, "Check options."+propertyID+": "+reason+".")
			} else if reason != "" {
				return nil, refuse(errDatabaseRowPageValuesInvalid, "options.%s: %s", propertyID, reason)
			}
			entry := map[string]any{"id": id, "name": name, "color": color}
			if property.Type == graphValueTypeStatus {
				if entry["group"] = databaseRowPageJoinStatusGroup(config, id, strings.TrimSpace(option.Group)); entry["group"] == "" {
					return nil, refuse(errDatabaseRowPageValuesInvalid,
						"options.%s: column %q has no status groups to put option %s in", propertyID, property.Name, strconvQuoteJS(name))
				}
			}
			current = append(current, entry)
			ids[id], names[name] = true, true
		}
		config["options"] = current
		encoded, err := json.Marshal(config)
		if err != nil {
			return nil, err
		}
		operations = append(operations, graphOperation{Kind: graphOperationKindUpsertProperty, Property: &graphProperty{
			ID: propertyID, DataSourceID: authority.DataSourceID, Type: property.Type,
			Name: property.Name, Position: property.Position, Config: encoded,
		}})
	}
	return operations, nil
}

// databaseRowPageNewOptionReason says why a new option a value adds to a
// column can't be, with the code of a cause the screen says in its words
// (database_option_names.go): "" when it can.
func databaseRowPageNewOptionReason(column, id, name string, named, idHeld, nameHeld bool) (string, string) {
	switch {
	case id == "" || len(id) > maxDatabaseRowPageNewOptionIDBytes:
		return "", fmt.Sprintf("column %q: a new option's id is 1–%d bytes of yours", column, maxDatabaseRowPageNewOptionIDBytes)
	case name == "" || len([]rune(name)) > maxDatabaseRowPageNewOptionNameRune:
		return "", fmt.Sprintf("column %q: option %s's name is 1–%d characters", column, strconvQuoteJS(id), maxDatabaseRowPageNewOptionNameRune)
	case !named:
		return "", fmt.Sprintf("column %q: option %s isn't one the value sets", column, strconvQuoteJS(name))
	case idHeld || nameHeld:
		return errorCodeDatabaseOptionNameTaken, fmt.Sprintf("column %q has option %s already: the value names it by its id", column, strconvQuoteJS(name))
	}
	if reason := databaseOptionCommaReason(column, name); reason != "" {
		return errorCodeDatabaseOptionNameComma, reason
	}
	return "", ""
}

// databaseRowPageNamedOptions is the options a select, multi-select or
// status value names.
func databaseRowPageNamedOptions(propertyType string, value graphRowValue) (map[string]bool, bool) {
	named := map[string]bool{}
	switch propertyType {
	case graphValueTypeSelect, graphValueTypeStatus:
		id := ""
		if json.Unmarshal(value.Value, &id) != nil {
			return nil, false
		}
		named[id] = true
	case graphValueTypeMultiSelect:
		ids := []string{}
		if json.Unmarshal(value.Value, &ids) != nil {
			return nil, false
		}
		for _, id := range ids {
			named[id] = true
		}
	default:
		return nil, false
	}
	return named, true
}

// databaseRowPageJoinStatusGroup puts a new status option in the group of
// the given name, or the first group, and reports the group's name.
func databaseRowPageJoinStatusGroup(config map[string]any, optionID, groupName string) string {
	groups, _ := config["statusGroups"].([]any)
	target := -1
	for index, raw := range groups {
		group, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if target < 0 {
			target = index
		}
		if name, _ := group["name"].(string); name == groupName {
			target = index
			break
		}
	}
	if target < 0 {
		return ""
	}
	group := groups[target].(map[string]any)
	optionIDs, _ := group["optionIds"].([]any)
	group["optionIds"] = append(optionIDs, optionID)
	name, _ := group["name"].(string)
	return name
}
