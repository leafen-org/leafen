package app

import (
	"bytes"
	"encoding/json"
	"strings"
)

// A rollup's and a formula's settings have one set of keys, the ones the
// native API documents (native_operation_field_shapes.go) and the web, the
// server's values and the export read: a rollup's
// rollupRelationPropertyId, rollupTargetPropertyId and rollupFunction, a
// formula's formula. A change may still name them by the older keys —
// relationPropertyId, targetPropertyId and function, expression — or a
// calculation by an older name (count_all); it is stored in the documented
// ones, and a property stored before in the older ones reads in them, so
// the table's cell, the server's value and the export never part.

// databaseRollupFunctionAliases are the older names of a rollup's
// calculations, as Notion's API brought them.
var databaseRollupFunctionAliases = map[string]string{
	"count_all": "count", "count_not_empty": "count_values", "count_empty": "empty", "count_unique": "unique",
}

// databaseRollupFormerKeys are a rollup's older keys and the ones they stand for.
var databaseRollupFormerKeys = [][2]string{
	{"relationPropertyId", "rollupRelationPropertyId"},
	{"targetPropertyId", "rollupTargetPropertyId"},
	{"function", "rollupFunction"},
}

// canonicalDatabasePropertyConfig puts a rollup's or a formula's settings in
// their documented keys, a rollup's calculation by its name; whether it
// changed them.
func canonicalDatabasePropertyConfig(propertyType string, config map[string]any) bool {
	changed := false
	settle := func(former, documented string) {
		value, held := config[former]
		if !held {
			return
		}
		delete(config, former)
		changed = true
		if text, _ := config[documented].(string); strings.TrimSpace(text) == "" {
			if text, _ := value.(string); strings.TrimSpace(text) != "" {
				config[documented] = value
			}
		}
	}
	switch propertyType {
	case graphValueTypeRollup:
		for _, keys := range databaseRollupFormerKeys {
			settle(keys[0], keys[1])
		}
		if function, _ := config["rollupFunction"].(string); databaseRollupFunctionAliases[function] != "" {
			config["rollupFunction"] = databaseRollupFunctionAliases[function]
			changed = true
		}
	case graphValueTypeFormula:
		settle("expression", "formula")
	}
	return changed
}

// normalizeGraphPropertyConfig keeps a property's settings a change writes
// in their documented keys, and checks the app editor it names.
func normalizeGraphPropertyConfig(property *graphProperty) error {
	config := map[string]any{}
	decoder := json.NewDecoder(bytes.NewReader(property.Config))
	decoder.UseNumber()
	if decoder.Decode(&config) == nil && canonicalDatabasePropertyConfig(property.Type, config) {
		encoded, err := json.Marshal(config)
		if err != nil {
			return graphInputError(errorCodeGraphInvalid, "Check the content structure.")
		}
		property.Config = encoded
	}
	return normalizeGraphPropertyAppEditor(property.Config)
}

// canonicalGraphReadPropertyConfig reads a property stored before in the
// older keys in the documented ones.
func canonicalGraphReadPropertyConfig(value map[string]any) {
	propertyType, _ := value["type"].(string)
	if config, ok := value["config"].(map[string]any); ok {
		canonicalDatabasePropertyConfig(propertyType, config)
	}
}
