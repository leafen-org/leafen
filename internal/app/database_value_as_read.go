package app

import "encoding/json"

// databasePersonDefaultsToCreator reports whether a person property reads as
// its row's creator while it names no one, as Notion's "기본값: 생성자"
// (seen 2026-10-10: read, not stored; clearing it shows the creator again,
// and turning the default off empties those rows).
func databasePersonDefaultsToCreator(propertyType, config string) bool {
	if propertyType != graphValueTypePerson || config == "" {
		return false
	}
	var parsed struct {
		DefaultCreator bool `json:"defaultCreator"`
	}
	return json.Unmarshal([]byte(config), &parsed) == nil && parsed.DefaultCreator
}

// databaseValueAsRead is a row's stored value for a property as it reads,
// where a property shows a default it doesn't keep in the row: a status its
// default option while the row sets none (databaseStatusValueOrDefault), a
// person property that defaults to the creator the row's creator while it
// names no one; where a select or multi-select names an option it no longer
// has, without it (databaseOptionValueKnown); else the value as stored.
// Filters, sorts, formulas, rollups and the API read values here; the web
// reads the same (valueAsRead).
func databaseValueAsRead(propertyType, config string, value json.RawMessage, createdBy string) json.RawMessage {
	switch propertyType {
	case graphValueTypeStatus:
		return databaseStatusValueOrDefault(config, value)
	case graphValueTypeSelect, graphValueTypeMultiSelect:
		return databaseOptionValueKnown(config, value)
	}
	if createdBy == "" || !databasePersonDefaultsToCreator(propertyType, config) {
		return value
	}
	var people []json.RawMessage
	if json.Unmarshal(value, &people) == nil && len(people) > 0 {
		return value
	}
	var person string
	if json.Unmarshal(value, &person) == nil && person != "" {
		return value
	}
	encoded, err := json.Marshal([]string{createdBy})
	if err != nil {
		return value
	}
	return encoded
}

// databaseOptionValueKnown is a select's or multi-select's stored value
// without the options it no longer has, as Notion's rows read once the
// option is taken away (seen 2026-11-08: a select empty, a multi-select
// without it, and a filter for an empty select finds it).
func databaseOptionValueKnown(config string, value json.RawMessage) json.RawMessage {
	if len(value) == 0 || string(value) == "null" || string(value) == `""` || string(value) == "[]" {
		return value
	}
	facts := databaseStatusConfigFactsOf(config)
	if !facts.listed {
		return value
	}
	known := facts.known
	var option string
	if json.Unmarshal(value, &option) == nil {
		if _, kept := known[option]; kept {
			return value
		}
		return json.RawMessage("null")
	}
	var options []string
	if json.Unmarshal(value, &options) != nil {
		return value
	}
	kept := make([]string, 0, len(options))
	for _, id := range options {
		if _, ok := known[id]; ok {
			kept = append(kept, id)
		}
	}
	if len(kept) == len(options) {
		return value
	}
	encoded, err := json.Marshal(kept)
	if err != nil {
		return value
	}
	return encoded
}
