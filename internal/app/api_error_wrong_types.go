package app

import (
	"math"
	"strings"
)

// A body field sent as a JSON value of another type than the one its
// operation reads — a list sent as text, a number as text, an object as a
// list — is named among a refusal's fields with the type it takes, and an
// agent's request is told "Check <field>: …" as for a value its operation
// doesn't take (api_error_wrong_values.go), on every route alike: such a body
// doesn't decode, and its route answered its own plain words only, naming no
// field (2026-11-26, the wider agent scorecard: "Check the legal hold
// details." for a list of people sent as text; the DLP policy, audit
// retention, MCP governance, search export, guest 2-step verification and
// SAML routes alike).

// apiErrorFieldWrongType is the problem of such a field.
const apiErrorFieldWrongType = "not the type it takes"

// apiErrorValueFits is whether a JSON value reads as a field of the type its
// operation describes (scripts/native-operation-inputs): text, true or
// false, a number (a whole one for an integer), a list of such, an object
// for a record or a map; null reads as any. A type it doesn't know — any, or
// a type of the product's own that reads JSON its own way — fits.
func apiErrorValueFits(input apiOperationInput, fieldType string, value any) bool {
	if value == nil {
		return true
	}
	fieldType = strings.TrimPrefix(fieldType, "*")
	if strings.HasPrefix(fieldType, "map of ") {
		// A map is an object whatever its entries take — lists too ("map of
		// DatabaseRowPageNewOption[]"), whose entries are checked by their keys
		// (api_error_map_entries.go): read as a list, a row's options were named
		// whole, never options.Category (2026-11-27).
		_, ok := value.(map[string]any)
		return ok
	}
	if entry, isList := strings.CutSuffix(fieldType, "[]"); isList {
		if entry == "uint8" || entry == "byte" {
			// Bytes read as base64 text.
			_, ok := value.(string)
			return ok
		}
		entries, ok := value.([]any)
		if !ok {
			return false
		}
		for _, item := range entries {
			if !apiErrorValueFits(input, entry, item) {
				return false
			}
		}
		return true
	}
	if _, isRecord := input.Records[fieldType]; isRecord || fieldType == "object" {
		_, ok := value.(map[string]any)
		return ok
	}
	switch fieldType {
	case "string":
		_, ok := value.(string)
		return ok
	case "bool":
		_, ok := value.(bool)
		return ok
	case "float32", "float64":
		_, ok := value.(float64)
		return ok
	case "int", "int8", "int16", "int32", "int64":
		number, ok := value.(float64)
		return ok && number == math.Trunc(number)
	case "uint", "uint8", "uint16", "uint32", "uint64":
		number, ok := value.(float64)
		return ok && number == math.Trunc(number) && number >= 0
	}
	return true
}
