package app

import (
	"encoding/json"
	"time"
)

// Which of a rollup's related values a filter on it asks about, as Notion's
// "최소 하나의 옵션", "값 없음" and "모든 옵션" (seen 2026-11-05). A row
// relating to nothing has no value: it matches "값 없음" and neither of the
// others, whatever the condition, "비어 있음" too.
const (
	databaseRollupMatchNone  = "none"
	databaseRollupMatchEvery = "every"
)

// databaseRollupKeepsLeaves says whether a rollup keeps its related values
// for its filters: one showing them, or counting them per group, which
// Notion filters as the values (seen 2026-11-11).
func databaseRollupKeepsLeaves(function string) bool {
	switch function {
	case "show_original", "show_unique", "count_per_group", "percent_per_group":
		return true
	}
	return false
}

// databaseRollupTargetValue is a related row's value of the property a
// rollup rolls up: a page's created or edited time, to the minute as Notion
// keeps it, or person read from the row page itself, as filters read them
// (databaseQueryRowWithTimestamps), a checkbox never set unchecked, and any
// other the value as read. Notion's rollups show, count and filter these as
// the values they are (seen 2026-11-12: the earliest of times made at
// 17:06:55 and 17:07:07 is "17:06:00.000", and they are two different
// values only when their minutes differ).
func databaseRollupTargetValue(target storedDatabaseQueryProperty, row storedDatabaseQueryRow) any {
	switch target.Type {
	case graphValueTypeCreatedTime, graphValueTypeLastEditedTime:
		at := row.Created
		if target.Type == graphValueTypeLastEditedTime {
			at = row.Updated
		}
		if at < 1 {
			return nil
		}
		return time.UnixMilli(at).UTC().Truncate(time.Minute).Format(time.RFC3339Nano)
	case graphValueTypeCreatedBy, graphValueTypeLastEditedBy:
		person := row.CreatedBy
		if target.Type == graphValueTypeLastEditedBy {
			person = row.LastEditedBy
		}
		if person == "" {
			return nil
		}
		return []any{person}
	}
	value := databaseQueryRawValue(databaseValueAsRead(target.Type, target.Config, row.Values[target.ID].Value, row.CreatedBy))
	if value == nil && target.Type == graphValueTypeCheckbox {
		// A checkbox never set is unchecked, never empty (seen 2026-11-12).
		return false
	}
	return value
}

// databaseRollupLeafKind is the kind of value Notion names a related value
// of the property rolled up by (notionViewRollupLeafKind), a formula's by
// the value itself; "" for an empty formula value.
func databaseRollupLeafKind(targetType string, leaf json.RawMessage) string {
	switch targetType {
	case graphValueTypeTitle, graphValueTypeRichText, graphValueTypeURL, graphValueTypeEmail, graphValueTypePhone:
		return "rich_text"
	case graphValueTypeNumber, graphValueTypeUniqueID:
		return "number"
	case graphValueTypeDate, graphValueTypeCreatedTime, graphValueTypeLastEditedTime:
		return "date"
	case graphValueTypePerson, graphValueTypeCreatedBy, graphValueTypeLastEditedBy:
		return "people"
	case graphValueTypeFormula:
		var value any
		_ = json.Unmarshal(leaf, &value)
		switch typed := value.(type) {
		case float64:
			return "number"
		case string:
			return "rich_text"
		case bool:
			return "checkbox"
		case map[string]any:
			if databaseQueryDateObject(typed) {
				return "date"
			}
		}
		return ""
	}
	return targetType
}

// databaseRollupEncodedLeaves is the related values a computed rollup keeps
// for its filters, none unless it is shown as them.
func databaseRollupEncodedLeaves(function string, leaves []any) json.RawMessage {
	if !databaseRollupKeepsLeaves(function) {
		return nil
	}
	if leaves == nil {
		leaves = []any{}
	}
	encoded, err := json.Marshal(leaves)
	if err != nil {
		return nil
	}
	return encoded
}

// databaseQueryRollupLeavesFilter applies a filter to each related value as a
// value of the property rolled up, and reports whether the rollup could be
// read that way; one that couldn't compares what it shows as text.
func databaseQueryRollupLeavesFilter(
	row databaseQueryRow,
	filter databaseQueryFilter,
	property storedDatabaseQueryProperty,
	context databaseQueryContext,
	plan databaseQueryPlan,
	principalID, pageID string,
) (bool, bool) {
	computed, found := row.Computed[property.ID]
	if !found || len(computed.Leaves) == 0 {
		return false, false
	}
	config, err := decodeDatabaseComputedConfig(property.Config)
	if err != nil {
		return false, false
	}
	_, targetID, _, setUp := context.rollupSetUp(config)
	target, known := context.PropertiesByID[targetID]
	if !setUp || !known || target.Type == graphValueTypeRollup {
		return false, false
	}
	leaves := []json.RawMessage{}
	if json.Unmarshal(computed.Leaves, &leaves) != nil {
		return false, false
	}
	inner := filter
	inner.RollupMatch, inner.RollupKind = "", ""
	// A filter naming another kind than a value's matches it only by being
	// empty or not, as Notion's (seen 2026-11-11).
	emptiness := filter.Operator == "is_empty" || filter.Operator == "is_not_empty"
	matched := 0
	for _, leaf := range leaves {
		kind := databaseRollupLeafKind(target.Type, leaf)
		// A checkbox is neither empty nor not to a condition of another
		// kind, as Notion's (seen 2026-11-12); its own kind has none.
		if emptiness && kind == "checkbox" || !emptiness && filter.RollupKind != "" && kind != filter.RollupKind {
			continue
		}
		leafRow := row
		leafRow.Computed = map[string]databaseComputedValue{target.ID: {Type: target.Type, Value: leaf}}
		if databaseQueryFilterMatchesProperty(leafRow, inner, target, context, plan, principalID, pageID) {
			matched++
		}
	}
	switch filter.RollupMatch {
	case databaseRollupMatchNone:
		return matched == 0, true
	case databaseRollupMatchEvery:
		return len(leaves) > 0 && matched == len(leaves), true
	}
	return matched > 0, true
}
