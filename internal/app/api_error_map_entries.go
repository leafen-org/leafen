package app

import (
	"maps"
	"slices"
)

// A map field's entries are checked as its type names them — "map of
// GraphRowValue" holds an object under each key, "map of
// DatabaseRowPageNewOption[]" a list of them — so an entry of another
// type is named by its key, values.Category, with the form it takes, and an
// object under a key is checked as its record, among a refusal's fields on
// every route alike (api_error_details.go): such a body doesn't decode, and
// its route may have answered words about something else (2026-11-26: a
// row's values sent as {"values":{"Category":"Category 47"}} were refused for
// their mutationId, and the agent gave up).

// apiErrorMapEntries checks each entry of the map field name against the
// type its entries take.
func apiErrorMapEntries(input apiOperationInput, name, entryType string, held map[string]any, add func(apiErrorField) bool) {
	record := apiInputRecordOf(entryType)
	_, isRecord := input.Records[record]
	for _, key := range slices.Sorted(maps.Keys(held)) {
		entry := held[key]
		if !apiErrorValueFits(input, entryType, entry) {
			// A record's form names its fields, which its name alone doesn't.
			form := entryType
			if isRecord {
				form = apiInputFieldForm(input, nativeOperationInputField{Type: entryType})
			}
			if !add(apiErrorField{Name: name + "." + key, Problem: apiErrorFieldWrongType, Allowed: []string{form}}) {
				return
			}
			continue
		}
		if !isRecord {
			continue
		}
		switch value := entry.(type) {
		case map[string]any:
			apiErrorRecordFields(input, record, name+"."+key+".", value, add)
		case []any:
			for _, item := range value {
				if object, isObject := item.(map[string]any); isObject {
					apiErrorRecordFields(input, record, name+"."+key+"[].", object, add)
					break
				}
			}
		}
	}
}
