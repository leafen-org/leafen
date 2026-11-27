package app

import (
	"errors"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The rollup functions beyond counting and arithmetic, read as the app's
// rollups read them (rollupCore.ts) and valued as Notion's API values them
// (seen 2026-10-04): a percentage is a fraction (0.3333…), shown "33.33%";
// the earliest and latest dates are dates, a date range runs from the one
// to the other; "unique" counts different values; "checked" counts ticked
// boxes. A count or percentage per group shows the values themselves, as
// Notion shows them. A relation to nothing leaves a percentage, a date and a
// range empty, and the counts 0.

// databaseRollupPercentInGroup is a percent per group that names a status
// group: the share of the related rows in it (rollupCalculation);
// databaseRollupCountInGroup a count per group that names one: how many of
// them are in it.
const (
	databaseRollupPercentInGroup = "percent_in_status_group"
	databaseRollupCountInGroup   = "count_in_status_group"
)

// databaseRollupStatusGroupOptions are the options of the status group a
// rollup names, by the group's id.
func databaseRollupStatusGroupOptions(target storedDatabaseQueryProperty, groupID string) []string {
	for _, group := range databaseQueryParsedPropertyConfig(target).StatusGroups {
		if group.ID == groupID {
			return group.OptionIDs
		}
	}
	return nil
}

// databaseRollupInGroup is a status among a group's options.
func databaseRollupInGroup(value any, options []string) bool {
	for _, option := range databaseQueryValueStrings(value) {
		if slices.Contains(options, option) {
			return true
		}
	}
	return false
}

// databaseRollupShownValue is a related value as a rollup showing the
// values shows it: an option by its name and a person by theirs, as the
// app's cell shows them (rollupCore.ts originals) and Notion's API gives them
// (an option's name, a person's name); any other value as it is. Only what
// the rollup shows reads it this way: its filters, counts and groups keep
// the value itself (its leaves, the options' IDs).
func databaseRollupShownValue(target storedDatabaseQueryProperty, value any, people *databaseFormulaPeople) any {
	switch target.Type {
	case graphValueTypeSelect, graphValueTypeStatus, graphValueTypeMultiSelect:
		multi := target.Type == graphValueTypeMultiSelect
		names := []any{}
		for _, id := range databaseQueryValueStrings(value) {
			names = append(names, databaseFormulaOptionName(target, id, multi))
		}
		if len(names) == 0 {
			return nil
		}
		if !multi && len(names) == 1 {
			return names[0]
		}
		return names
	case graphValueTypePerson, graphValueTypeCreatedBy, graphValueTypeLastEditedBy:
		names := []any{}
		for _, id := range databaseQueryValueStrings(value) {
			if name := people.name(id); name != "" {
				id = name
			}
			names = append(names, id)
		}
		if len(names) == 0 {
			return nil
		}
		return names
	}
	return value
}

// databaseRollupPieces is a value's parts for counting different values: a
// list's items, or the value.
func databaseRollupPieces(value any) []string {
	if items, ok := value.([]any); ok {
		pieces := make([]string, 0, len(items))
		for _, item := range items {
			pieces = append(pieces, databaseRollupPieces(item)...)
		}
		return pieces
	}
	if value == nil || value == "" {
		return nil
	}
	return []string{databaseQueryValueText(value)}
}

// databaseRollupCountsValue says whether the counts of values (count_values,
// not_empty, unique and the shares of empty and not empty) count a related
// value: a checkbox, ticked or not, is neither a value nor empty to them,
// as Notion's (seen 2026-11-12: over a checkbox or a checkbox formula they
// are 0, the shares none).
func databaseRollupCountsValue(value any) bool {
	_, checkbox := value.(bool)
	return !checkbox
}

// databaseRollupValueCount is how many values a related value is to the
// count of values: a list's items (a relation's pages, a multi-select's
// options, a file list's files), any other value one, a checkbox none, as
// Notion counts them (seen 2026-11-12: 3 over a relation to one page and to
// two, untitled ones too, and over a multi-select of X and Y beside X).
func databaseRollupValueCount(value any) int {
	switch typed := value.(type) {
	case nil, bool:
		return 0
	case string:
		if typed == "" {
			return 0
		}
	case []any:
		count := 0
		for _, item := range typed {
			count += databaseRollupValueCount(item)
		}
		return count
	}
	return 1
}

// databaseRollupChecked is a ticked box, or text that says it is.
func databaseRollupChecked(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "true", "1", "yes", "checked":
			return true
		}
	}
	return false
}

// databaseRollupDates lists a value's dates: a date's start and end, a list's
// dates, or a date written out.
func databaseRollupDates(value any) []string {
	switch typed := value.(type) {
	case []any:
		dates := []string{}
		for _, item := range typed {
			dates = append(dates, databaseRollupDates(item)...)
		}
		return dates
	case map[string]any:
		dates := []string{}
		for _, key := range []string{"start", "end"} {
			if text, ok := typed[key].(string); ok {
				if _, valid := databaseRollupTime(text); valid {
					dates = append(dates, text)
				}
			}
		}
		return dates
	case string:
		if _, valid := databaseRollupTime(typed); valid {
			return []string{typed}
		}
	}
	return nil
}

func databaseRollupTime(text string) (time.Time, bool) {
	text = strings.TrimSpace(text)
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02"} {
		if moment, err := time.Parse(layout, text); err == nil {
			return moment, true
		}
	}
	return time.Time{}, false
}

// databaseRollupDateSpan widens an earliest and latest date by one more.
func databaseRollupDateSpan(earliest, latest, date string) (string, string) {
	moment, ok := databaseRollupTime(date)
	if !ok {
		return earliest, latest
	}
	if first, valid := databaseRollupTime(earliest); !valid || moment.Before(first) {
		earliest = date
	}
	if last, valid := databaseRollupTime(latest); !valid || moment.After(last) {
		latest = date
	}
	return earliest, latest
}

// databaseRollupDateValue is a date-valued function's value from the span.
func databaseRollupDateValue(function, earliest, latest string) any {
	if earliest == "" {
		return nil
	}
	switch function {
	case "earliest_date":
		return map[string]any{"start": earliest}
	case "latest_date":
		return map[string]any{"start": latest}
	default:
		return map[string]any{"start": earliest, "end": latest}
	}
}

// databaseRollupPercent is a count's share of a total, or nothing without one.
func databaseRollupPercent(count, total int) any {
	if total <= 0 {
		return nil
	}
	return float64(count) / float64(total)
}

func databaseRollupIsPercent(function string) bool {
	switch function {
	case "percent_empty", "percent_not_empty", "percent_checked", "percent_unchecked", databaseRollupPercentInGroup:
		return true
	}
	return false
}

// databaseRollupShowsValues is a function shown as the related values.
func databaseRollupShowsValues(function string) bool {
	switch function {
	case "show_original", "show_unique", "count_per_group", "percent_per_group":
		return true
	}
	return false
}

// databaseRollupText is how a rollup's value reads: a percentage as one, a
// date range as its two ends.
func databaseRollupText(function string, value any) string {
	if databaseRollupIsPercent(function) {
		share, ok := value.(float64)
		if !ok {
			return ""
		}
		// A group's share always shows two places, "0.00%", as Notion's (seen 2026-11-04).
		places := -1
		if function == databaseRollupPercentInGroup {
			places = 2
		}
		return strconv.FormatFloat(math.Round(share*10000)/100, 'f', places, 64) + "%"
	}
	if function == "date_range" {
		if span, ok := value.(map[string]any); ok {
			start, _ := span["start"].(string)
			end, _ := span["end"].(string)
			if end != "" && end != start {
				return start + " → " + end
			}
			return start
		}
	}
	return databaseQueryValueText(value)
}

// finalizeDatabaseQueryRollup is a rollup's value from what its accumulator
// gathered over the related rows, turn by turn.
func finalizeDatabaseQueryRollup(accumulator databaseQueryRollupAccumulator) (any, error) {
	switch accumulator.Function {
	case "count":
		return accumulator.VisibleCount, nil
	case "count_values":
		return accumulator.Values, nil
	case "not_empty":
		return accumulator.PresentCount - accumulator.Booleans, nil
	case "empty":
		return accumulator.InputCount - accumulator.PresentCount, nil
	case "unique":
		return len(accumulator.Seen), nil
	case "percent_empty":
		return databaseRollupPercent(accumulator.VisibleCount-accumulator.PresentCount, accumulator.VisibleCount-accumulator.Booleans), nil
	case "percent_not_empty":
		return databaseRollupPercent(accumulator.PresentCount-accumulator.Booleans, accumulator.VisibleCount-accumulator.Booleans), nil
	case databaseRollupPercentInGroup:
		return databaseRollupPercent(accumulator.CheckedCount, accumulator.VisibleCount), nil
	case databaseRollupCountInGroup:
		return accumulator.CheckedCount, nil
	case "checked":
		return accumulator.CheckedCount, nil
	case "unchecked":
		return accumulator.VisibleCount - accumulator.CheckedCount, nil
	case "percent_checked":
		return databaseRollupPercent(accumulator.CheckedCount, accumulator.VisibleCount), nil
	case "percent_unchecked":
		return databaseRollupPercent(accumulator.VisibleCount-accumulator.CheckedCount, accumulator.VisibleCount), nil
	case "earliest_date", "latest_date", "date_range":
		return databaseRollupDateValue(accumulator.Function, accumulator.EarliestDate, accumulator.LatestDate), nil
	case "show_original", "show_unique", "count_per_group", "percent_per_group":
		return accumulator.Text, nil
	case "sum":
		// Nothing to add up is 0, as Notion's (seen 2026-10-03).
		if !accumulator.HasNumber {
			return 0.0, nil
		}
		return accumulator.Sum, nil
	case "average":
		if accumulator.NumberCount == 0 {
			return nil, nil
		}
		return accumulator.Sum / float64(accumulator.NumberCount), nil
	case "median":
		if accumulator.MedianLower == nil || accumulator.MedianUpper == nil {
			// No numbers is 0, as Notion's (seen 2026-10-03).
			if accumulator.NumberCount == 0 {
				return 0.0, nil
			}
			return nil, errors.New(errorCodeDatabaseQueryCursorInvalid)
		}
		return (*accumulator.MedianLower + *accumulator.MedianUpper) / 2, nil
	case "min":
		if !accumulator.HasNumber {
			return nil, nil
		}
		return accumulator.Minimum, nil
	case "max":
		if !accumulator.HasNumber {
			return nil, nil
		}
		return accumulator.Maximum, nil
	case "range":
		if !accumulator.HasNumber {
			return nil, nil
		}
		return accumulator.Maximum - accumulator.Minimum, nil
	default:
		return nil, errors.New(errorCodeDatabaseQueryInvalid)
	}
}
