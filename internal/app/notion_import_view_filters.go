package app

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/pocketbase/dbx"
)

// A Notion view's filter, sorts and quick filters keep the conditions on
// properties the import brought over, as the view API reads them; the rest
// are reported against the view.

func (converter *notionImportViewConverter) filter(raw json.RawMessage, reasons notionImportViewReasons) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	var node any
	if json.Unmarshal(raw, &node) != nil {
		reasons.add(notionImportIssueViewSettingOmitted)
		return nil
	}
	if kept := converter.filterNode(node, 1, reasons); kept != nil {
		return notionImportJSON(kept)
	}
	return nil
}

// filterNode keeps the conditions of a filter the import can hold, within
// the depth and size a native filter group has.
func (converter *notionImportViewConverter) filterNode(node any, depth int, reasons notionImportViewReasons) map[string]any {
	object, _ := node.(map[string]any)
	for _, conjunction := range []string{"and", "or"} {
		children, compound := object[conjunction]
		if !compound {
			continue
		}
		items, ok := children.([]any)
		if !ok || len(object) != 1 || depth > maxDatabaseQueryFilterDepth {
			reasons.add(notionImportIssueViewSettingOmitted)
			return nil
		}
		kept := []any{}
		for _, child := range items {
			if result := converter.filterNode(child, depth+1, reasons); result != nil {
				kept = append(kept, result)
			}
		}
		if len(kept) == 0 {
			return nil
		}
		return map[string]any{conjunction: kept}
	}
	if len(object) != 2 {
		reasons.add(notionImportIssueViewSettingOmitted)
		return nil
	}
	property, found := notionViewProperty{}, false
	if kind, timestamp := object["timestamp"].(string); timestamp {
		// A condition on when rows were made or edited holds on the
		// database's created or edited time property.
		property, found = converter.timestampProperty(kind)
		if !found || object[kind] == nil {
			reasons.add(notionImportIssueViewSettingOmitted)
			return nil
		}
		object = map[string]any{"property": property.ID, kind: object[kind]}
	} else if property, found = converter.property(object["property"]); !found {
		reasons.add(notionImportIssueViewPropertyUnresolved)
		return nil
	}
	for key, value := range object {
		if key == "property" {
			continue
		}
		condition, ok := converter.condition(property, key, value)
		if !ok {
			break
		}
		leaf := map[string]any{"property": property.ID, property.OfficialType: condition}
		if _, err := converter.scope.nativeViewFilterGroup(notionImportJSON(leaf)); err != nil {
			break
		}
		return leaf
	}
	reasons.add(notionImportIssueViewSettingOmitted)
	return nil
}

func (converter *notionImportViewConverter) condition(property notionViewProperty, key string, value any) (map[string]any, bool) {
	family := notionViewConditionFamily(property.OfficialType)
	condition, _ := value.(map[string]any)
	// A formula's or rollup's condition names the kind of value inside it; the
	// view's filter reads it as Notion's.
	if condition != nil && key == property.OfficialType && (key == "formula" || key == "rollup") {
		return condition, true
	}
	if family == "" || condition == nil || key != property.OfficialType && notionViewConditionFamily(key) != family {
		return nil, false
	}
	if family == "people" {
		for operator, operand := range condition {
			if operand == "me" {
				condition[operator] = map[string]any{"kind": "leafen.current_user"}
			}
		}
	}
	return condition, true
}

func (converter *notionImportViewConverter) sorts(raw json.RawMessage, reasons notionImportViewReasons) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	items := []map[string]any{}
	if json.Unmarshal(raw, &items) != nil {
		reasons.add(notionImportIssueViewSettingOmitted)
		return nil
	}
	kept := []any{}
	for _, item := range items {
		property, found := converter.property(item["property"])
		timestamp, _ := item["timestamp"].(string)
		if item["timestamp"] != nil {
			property, found = converter.timestampProperty(timestamp)
		}
		switch {
		case item["timestamp"] != nil && !found:
			reasons.add(notionImportIssueViewSettingOmitted)
		case !found:
			reasons.add(notionImportIssueViewPropertyUnresolved)
		case item["direction"] != databaseQuerySortAscending && item["direction"] != databaseQuerySortDescending:
			reasons.add(notionImportIssueViewSettingOmitted)
		default:
			kept = append(kept, map[string]any{"property": property.ID, "direction": item["direction"]})
		}
	}
	if len(kept) == 0 {
		return nil
	}
	return notionImportJSON(kept)
}

// timestampProperty is the database's property holding when each row was
// made or last edited, which a Notion view names by timestamp.
func (converter *notionImportViewConverter) timestampProperty(kind string) (notionViewProperty, bool) {
	if kind != "created_time" && kind != "last_edited_time" {
		return notionViewProperty{}, false
	}
	for _, property := range converter.scope.ordered {
		if property.OfficialType == kind {
			return property, true
		}
	}
	return notionViewProperty{}, false
}

// quickFilters are a view's filter-bar pills, one request each.
func (converter *notionImportViewConverter) quickFilters(raw json.RawMessage, reasons notionImportViewReasons) []json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	entries := map[string]any{}
	if json.Unmarshal(raw, &entries) != nil {
		reasons.add(notionImportIssueViewSettingOmitted)
		return nil
	}
	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	kept := []json.RawMessage{}
	for _, key := range keys {
		entry, _ := entries[key].(map[string]any)
		if entry == nil {
			continue
		}
		property, found := converter.property(key)
		if !found {
			reasons.add(notionImportIssueViewPropertyUnresolved)
			continue
		}
		var condition map[string]any
		for conditionKey, value := range entry {
			if len(entry) == 1 {
				condition, _ = converter.condition(property, conditionKey, value)
			}
		}
		if condition == nil {
			reasons.add(notionImportIssueViewSettingOmitted)
			continue
		}
		kept = append(kept, notionImportJSON(map[string]any{property.ID: map[string]any{property.OfficialType: condition}}))
	}
	return kept
}

// filterParts is the filter as a view's settings take it: whole, or else the
// conditions of its outer group they take on their own, joined as they were,
// the others reported. Nil when none is taken.
func (converter *notionImportViewConverter) filterParts(
	viewType, current string, filter json.RawMessage, reasons notionImportViewReasons,
) json.RawMessage {
	takes := func(candidate json.RawMessage) bool {
		_, err := converter.scope.updatedViewConfig(viewType, current, candidate, nil, nil, nil)
		return err == nil
	}
	if takes(filter) {
		return filter
	}
	group := map[string][]any{}
	if json.Unmarshal(filter, &group) != nil || len(group) != 1 {
		reasons.add(notionImportIssueViewSettingOmitted)
		return nil
	}
	for conjunction, children := range group {
		kept := []any{}
		for _, child := range children {
			if takes(notionImportJSON(map[string]any{conjunction: []any{child}})) {
				kept = append(kept, child)
			}
		}
		reasons.add(notionImportIssueViewSettingOmitted)
		if len(kept) == 0 || !takes(notionImportJSON(map[string]any{conjunction: kept})) {
			return nil
		}
		return notionImportJSON(map[string]any{conjunction: kept})
	}
	return nil
}

// gatheredViewProperties lets a view name a relation the plan gathered into
// another (notion_migration_roles.go) as that one's column: its conditions,
// and its place, width and showing in the view's list, are the column's.
func (linker *notionImportLinker) gatheredViewProperties(converter *notionImportViewConverter, dataSourceKey string) error {
	mapped := []struct {
		SourceID string `db:"source_id"`
		NativeID string `db:"native_id"`
	}{}
	prefix := dataSourceKey + ":property:"
	if err := linker.app.DB().NewQuery(`SELECT [[source_id]],[[native_id]] FROM {{leafen_notion_import_mappings}}
		WHERE [[job_id]]={:jobId} AND [[kind]]='property' AND substr([[source_id]], 1, length({:prefix}))={:prefix}`).
		Bind(dbx.Params{"jobId": linker.job.ID, "prefix": prefix}).All(&mapped); err != nil {
		return err
	}
	for _, mapping := range mapped {
		notionID := strings.TrimPrefix(mapping.SourceID, prefix)
		if strings.Contains(notionID, ":") {
			continue
		}
		key := notionImportPropertyKey(notionID)
		if _, known := converter.notionIDs[key]; known {
			continue
		}
		if _, column := converter.scope.byID[mapping.NativeID]; column {
			converter.notionIDs[key] = mapping.NativeID
		}
	}
	return nil
}
