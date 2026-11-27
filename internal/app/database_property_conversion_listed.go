package app

import (
	"encoding/json"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A property the Notion-compatible API makes a select or multi-select from
// another type has the options its request lists and no others, as Notion's
// API makes one (seen 2026-11-09 from a status, multi-select, select, text,
// number, checkbox, URL and date): none carried over from the old type or
// made from its values, so {"select":{}} has none. Each cell keeps the listed
// options its value names, in any case: a text by its comma-separated parts
// ("Z, b, A" takes B with A and B listed), a multi-select by its options'
// names, a number, checkbox or URL by what it shows; a select takes the first
// so named. A date names none. A change back to a type the property had
// reads the values it had then by the same rule (seen 2026-11-09: a
// multi-select made a select and a multi-select again with A and c listed
// had A and c on the rows that had A and C).

// databasePropertyConversionListsOptions tells the types the API makes with
// just the options listed.
func databasePropertyConversionListsOptions(valueType string) bool {
	return valueType == graphValueTypeSelect || valueType == graphValueTypeMultiSelect
}

// newDatabasePropertyListedConversion prepares a conversion to a select or
// multi-select whose options are just those targetConfig lists.
func newDatabasePropertyListedConversion(
	from, to string, sourceConfig, targetConfig map[string]any, names databasePropertyConversionNames,
) (*databasePropertyConversion, error) {
	conversion, err := newDatabasePropertyConversion(from, to, sourceConfig, nil, names)
	if err != nil {
		return nil, err
	}
	conversion.listed, conversion.options, conversion.optionByName = true, nil, map[string]string{}
	for _, option := range databasePropertyConversionOptions(targetConfig) {
		id, _ := option["id"].(string)
		name, _ := option["name"].(string)
		if id == "" || name == "" {
			continue
		}
		conversion.options = append(conversion.options, option)
		if folded := strings.ToLower(name); conversion.optionByName[folded] == "" {
			conversion.optionByName[folded] = id
		}
	}
	return conversion, nil
}

// listedValue is a cell's value as the listed options read it, nil when it
// names none of them.
func (conversion *databasePropertyConversion) listedValue(raw json.RawMessage) (json.RawMessage, error) {
	if conversion.from == graphValueTypeDate {
		return nil, nil
	}
	// An option's name is read whole, a comma of an older one too
	// (database_option_names.go); a text by its comma pieces.
	parts := []string(nil)
	if _, options := databasePropertyConversionOptionTypes[conversion.from]; options {
		ids := []string{}
		if conversion.from != graphValueTypeMultiSelect {
			ids = append(ids, "")
			if json.Unmarshal(raw, &ids[0]) != nil {
				return nil, errNotionDatabaseRowPageProjectionInvalid
			}
		} else if json.Unmarshal(raw, &ids) != nil {
			return nil, errNotionDatabaseRowPageProjectionInvalid
		}
		for _, id := range ids {
			parts = append(parts, conversion.sourceOptions[id])
		}
	} else {
		text, err := conversion.sourceText(raw)
		if err != nil {
			return nil, err
		}
		parts = strings.Split(text, ",")
	}
	ids, seen := []string{}, map[string]bool{}
	for _, part := range parts {
		id := conversion.optionByName[strings.ToLower(strings.TrimSpace(part))]
		if id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	switch {
	case len(ids) == 0:
		return nil, nil
	case conversion.to == graphValueTypeMultiSelect:
		return json.Marshal(ids)
	default:
		return json.Marshal(ids[0])
	}
}

// databasePropertyConversionRulesFor are a recorded conversion's rules.
func databasePropertyConversionRulesFor(
	listed bool, from, to string, sourceConfig, targetConfig map[string]any, names databasePropertyConversionNames,
) (*databasePropertyConversion, error) {
	if listed {
		return newDatabasePropertyListedConversion(from, to, sourceConfig, targetConfig, names)
	}
	return newDatabasePropertyConversion(from, to, sourceConfig, targetConfig, names)
}

// notionDataSourceLendOptionIDs gives each listed option of a select made from
// a status the ID of the status option named alike, in any case: the cells,
// which a select reads as a status's, keep naming it.
func notionDataSourceLendOptionIDs(config map[string]any, former []map[string]any) {
	options, _ := config["options"].([]map[string]any)
	lent := map[string]bool{}
	for _, option := range options {
		name, _ := option["name"].(string)
		if earlier := notionStatusOptionNamed(former, name, lent); earlier != nil {
			id, _ := earlier["id"].(string)
			option["id"], lent[id] = id, true
		}
	}
}

// databasePropertyFormerStatusOptions are the options a property had when it
// was last a status, which a status the API makes of it again lends its IDs
// to by name, as Notion's gives the rows their status back (seen 2026-11-09:
// a status made a select with no options and a status again had Done and In
// progress on the rows that had them).
func databasePropertyFormerStatusOptions(app core.App, propertyID string) []map[string]any {
	encoded := ""
	if app.DB().NewQuery(`SELECT [[from_config]] FROM {{leafen_database_property_conversions}}
		WHERE [[property_id]]={:propertyId} AND [[from_type]]={:status}
		ORDER BY [[created_at]] DESC,[[id]] DESC LIMIT 1`,
	).Bind(dbx.Params{"propertyId": propertyID, "status": graphValueTypeStatus}).Row(&encoded) != nil {
		return nil
	}
	config := map[string]any{}
	if json.Unmarshal([]byte(encoded), &config) != nil {
		return nil
	}
	return databasePropertyConversionOptions(config)
}

// markDatabasePropertyConversionListed records that a conversion's options are
// those listed.
func markDatabasePropertyConversionListed(app core.App, id string) error {
	result, err := app.DB().NewQuery(`UPDATE {{leafen_database_property_conversions}} SET [[listed_options]]=1
		WHERE [[id]]={:id}`).Bind(dbx.Params{"id": id}).Execute()
	return requireDatabaseMutationJobUpdate(result, err)
}
