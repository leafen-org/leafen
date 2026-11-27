package app

import (
	"bytes"
	"encoding/json"
	"math"
	"strings"
	"unicode/utf8"
)

// A property trigger may name what the edit leaves in the property, as
// Notion's do (seen 2026-10-24): a select or status set to some of its
// options (Stage set to Lead, Won or Lost), a number compared ("Amount >
// 15"), words that are, contain, start or end with some, people or a
// multi-select holding some of theirs, a checkbox checked or not, or a
// property cleared (Owner emptied). No
// operator is any edit of it; "all options" is a list left out.

const databaseAutomationTriggerMaxValues = 100

// databaseAutomationConditionValueValid tells whether a condition's value
// fits its operator: words or a number for comparing, a list of option,
// person or page IDs or of a checkbox's states (or none, for all of them),
// nothing for "cleared" or for any edit.
func databaseAutomationConditionValueValid(operator string, raw json.RawMessage) bool {
	none := len(bytes.TrimSpace(raw)) == 0 || string(bytes.TrimSpace(raw)) == "null"
	switch operator {
	case "", databaseAutomationTriggerOperatorCleared:
		return none
	case databaseAutomationTriggerOperatorGreaterThan, databaseAutomationTriggerOperatorLessThan,
		databaseAutomationTriggerOperatorGreaterOrEqual, databaseAutomationTriggerOperatorLessOrEqual:
		var number float64
		return !none && json.Unmarshal(raw, &number) == nil && !math.IsInf(number, 0) && !math.IsNaN(number)
	case databaseAutomationTriggerOperatorStartsWith, databaseAutomationTriggerOperatorEndsWith:
		var text string
		return !none && json.Unmarshal(raw, &text) == nil && boundedAutomationString(text, databaseAutomationMaxNameRunes)
	case databaseAutomationTriggerOperatorIs, databaseAutomationTriggerOperatorIsNot,
		databaseAutomationTriggerOperatorContains, databaseAutomationTriggerOperatorDoesNotContain:
		if none {
			return true
		}
		var number float64
		if json.Unmarshal(raw, &number) == nil {
			return !math.IsInf(number, 0) && !math.IsNaN(number)
		}
		var text string
		if json.Unmarshal(raw, &text) == nil {
			return utf8.RuneCountInString(text) <= databaseAutomationMaxNameRunes
		}
		var items []any
		if json.Unmarshal(raw, &items) != nil || len(items) == 0 || len(items) > databaseAutomationTriggerMaxValues {
			return false
		}
		for _, item := range items {
			switch value := item.(type) {
			case bool:
			case string:
				if !boundedAutomationString(value, databaseAutomationMaxIdentifierRunes) {
					return false
				}
			default:
				return false
			}
		}
		return true
	}
	return false
}

// databaseAutomationRowValueEmpty tells whether a property holds nothing.
func databaseAutomationRowValueEmpty(value graphRowValue, present bool) bool {
	trimmed := string(bytes.TrimSpace(value.Value))
	if !present || trimmed == "" || trimmed == "null" || trimmed == `""` || trimmed == "[]" || trimmed == "{}" {
		return true
	}
	return false
}

// databaseAutomationConditionValueMatches tells whether what an edit left in
// a property meets its condition.
func databaseAutomationConditionValueMatches(condition databaseAutomationTriggerCondition, value graphRowValue, present bool) bool {
	empty := databaseAutomationRowValueEmpty(value, present)
	switch condition.Operator {
	case "":
		return true
	case databaseAutomationTriggerOperatorCleared:
		return empty
	}
	raw := bytes.TrimSpace(condition.Value)
	all := len(raw) == 0 || string(raw) == "null"
	var wantedNumber float64
	if !all && json.Unmarshal(raw, &wantedNumber) == nil {
		var current float64
		known := !empty && json.Unmarshal(value.Value, &current) == nil
		switch condition.Operator {
		case databaseAutomationTriggerOperatorIs:
			return known && current == wantedNumber
		case databaseAutomationTriggerOperatorIsNot:
			return !known || current != wantedNumber
		case databaseAutomationTriggerOperatorGreaterThan:
			return known && current > wantedNumber
		case databaseAutomationTriggerOperatorLessThan:
			return known && current < wantedNumber
		case databaseAutomationTriggerOperatorGreaterOrEqual:
			return known && current >= wantedNumber
		case databaseAutomationTriggerOperatorLessOrEqual:
			return known && current <= wantedNumber
		}
		return false
	}
	var wantedText string
	if !all && json.Unmarshal(raw, &wantedText) == nil {
		var current string
		if !empty {
			_ = json.Unmarshal(value.Value, &current)
		}
		current, wantedText = strings.ToLower(strings.TrimSpace(current)), strings.ToLower(strings.TrimSpace(wantedText))
		switch condition.Operator {
		case databaseAutomationTriggerOperatorIs:
			return current == wantedText
		case databaseAutomationTriggerOperatorIsNot:
			return current != wantedText
		case databaseAutomationTriggerOperatorContains:
			return strings.Contains(current, wantedText)
		case databaseAutomationTriggerOperatorDoesNotContain:
			return !strings.Contains(current, wantedText)
		case databaseAutomationTriggerOperatorStartsWith:
			return strings.HasPrefix(current, wantedText)
		case databaseAutomationTriggerOperatorEndsWith:
			return strings.HasSuffix(current, wantedText)
		}
		return false
	}
	// A list: options, people or pages, or a checkbox's states; none is all of them.
	held := databaseAutomationRowValueItems(value, empty)
	hit := false
	if all {
		hit = len(held) > 0
	} else {
		var wanted []any
		_ = json.Unmarshal(raw, &wanted)
		for _, item := range wanted {
			if _, found := held[databaseAutomationItemKey(item)]; found {
				hit = true
				break
			}
		}
	}
	switch condition.Operator {
	case databaseAutomationTriggerOperatorIs, databaseAutomationTriggerOperatorContains:
		return hit
	case databaseAutomationTriggerOperatorIsNot, databaseAutomationTriggerOperatorDoesNotContain:
		return !hit
	}
	return false
}

// databaseAutomationRowValueItems is what a property holds as a list: an
// option's ID, those of people, pages or options, or a checkbox's state.
func databaseAutomationRowValueItems(value graphRowValue, empty bool) map[string]struct{} {
	items := map[string]struct{}{}
	if value.Type == "checkbox" {
		var checked bool
		_ = json.Unmarshal(value.Value, &checked)
		items[databaseAutomationItemKey(checked)] = struct{}{}
		return items
	}
	if empty {
		return items
	}
	var one string
	if json.Unmarshal(value.Value, &one) == nil {
		items[one] = struct{}{}
		return items
	}
	var many []any
	if json.Unmarshal(value.Value, &many) == nil {
		for _, item := range many {
			switch typed := item.(type) {
			case string:
				items[typed] = struct{}{}
			case map[string]any:
				// A person or a page may come as an object with its ID.
				if id, ok := typed["id"].(string); ok {
					items[id] = struct{}{}
				}
			}
		}
	}
	return items
}

func databaseAutomationItemKey(item any) string {
	switch typed := item.(type) {
	case bool:
		if typed {
			return "\x00true"
		}
		return "\x00false"
	case string:
		return typed
	}
	return ""
}
