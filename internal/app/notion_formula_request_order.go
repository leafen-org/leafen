package app

import (
	"bytes"
	"encoding/json"

	"github.com/pocketbase/pocketbase/core"
)

// A new source's formulas are checked in the order the request writes them,
// each against the source's other properties but its rollups and against the
// formulas written before it, as Notion's API checks them (seen 2026-11-21,
// databases and data sources alike, a synthetic page under 한지테스트페이지):
// {"B": prop("A") * 2, "A": "x"} is a type error in B naming A as not found,
// {"A": 1, "B": prop("A") > "a"} a comparison of number and text, a number
// or a relation named after the formula is there, a rollup isn't even
// before it (R not found), and of two formulas that fail the first written
// is named. So a formula naming itself or one
// written after it never comes back to itself.

// notionRequestBodyKey holds a Notion API request's body as written
// (bindNotionStrictJSON).
const notionRequestBodyKey = "leafenNotionRequestBody"

// notionRequestKeyOrder is the order a request's body gives the keys of the
// object at path in; nil where the body doesn't hold one.
func notionRequestKeyOrder(event *core.RequestEvent, path ...string) []string {
	raw, _ := event.Get(notionRequestBodyKey).([]byte)
	return notionJSONObjectKeys(json.NewDecoder(bytes.NewReader(raw)), path)
}

// notionJSONObjectKeys reads an object's keys in order, or with a path those
// of the object at it.
func notionJSONObjectKeys(decoder *json.Decoder, path []string) []string {
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return nil
	}
	keys := []string{}
	for decoder.More() {
		token, err := decoder.Token()
		key, isKey := token.(string)
		if err != nil || !isKey {
			return nil
		}
		if len(path) > 0 && key == path[0] {
			return notionJSONObjectKeys(decoder, path[1:])
		}
		keys = append(keys, key)
		var skipped json.RawMessage
		if decoder.Decode(&skipped) != nil {
			return nil
		}
	}
	if len(path) > 0 {
		return nil
	}
	return keys
}

// checkNewNotionSourceFormulaTypes keeps the formulas a new source's
// operations write, each checked against what it reads: the request's keys
// in its order (order), each key's property ID in ids.
func checkNewNotionSourceFormulaTypes(operations []graphOperation, order []string, ids map[string]string, own string, related notionFormulaRelated) error {
	scope := notionFormulaNewSourceScope(operations)
	at := map[string]int{}
	for _, key := range order {
		if id, planned := ids[key]; planned {
			if _, seen := at[id]; !seen {
				at[id] = len(at)
			}
		}
	}
	operation := map[string]int{}
	for index := range operations {
		if property := operations[index].Property; property != nil && operations[index].Kind == graphOperationKindUpsertProperty {
			operation[property.ID] = index
		}
	}
	// Without the body's order (none named), each reads every other.
	sequence := make([]string, 0, len(scope))
	for _, property := range scope {
		sequence = append(sequence, property.ID)
	}
	if len(at) == len(scope) {
		for id, position := range at {
			sequence[position] = id
		}
	}
	for _, id := range sequence {
		visible := []notionFormulaCheckProperty{}
		for _, property := range scope {
			switch normalizeNotionDatabaseRowPagePropertyType(property.Type) {
			case graphValueTypeRollup:
				continue
			case graphValueTypeFormula:
				if len(at) == len(scope) && at[property.ID] >= at[id] {
					continue
				}
			}
			visible = append(visible, property)
		}
		properties, pages := notionFormulaScopeTypes(visible, own, related)
		if err := keepNotionFormulaRequest(&operations[operation[id]], properties, pages); err != nil {
			return err
		}
	}
	return nil
}
