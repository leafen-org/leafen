package app

import (
	"encoding/json"
	"slices"
	"unicode/utf16"
)

// The order a view's groups come in, from a group's sort as Notion's views
// API takes it (seen 2026-11-12 on boards, tables and a board's sub-groups
// made with each sort):
//   - ascending: a status's in its order, any other's by name, the group for
//     no value first;
//   - descending: the reverse, the group for no value still first;
//   - manual: with no order of the view's own yet, by name in character
//     codes — a status's options or groups too — the group for no value
//     sorted as "uncategorized" (mcpBoardGroupOrder), as a new view's
//     options start on the screen (boardOptionGroupOrder); other values'
//     by name, with no order kept, as the screen orders them then
//     (noValueGroupLeads): a text's group for no value first, a
//     relation's, a URL's, an email's, a phone's and a formula's
//     last.
//
// A person's and a box's groups go by hand alone, and a number's and a
// date's never do: Notion's API reads them back so whatever was given — a
// formula's or rollup's given as a formula's date or number family too.
// The native settings are the screen's: groupSort and groupOrder, a board's
// sub-groups' under subGroup (tableGrouping.ts, boardSubgroupModel.ts).

var notionViewGroupSorts = []string{"manual", "ascending", "descending"}

// applyOfficialViewGrouping writes a table's or board's group (key groupBy),
// a board's sub-group (subGroupBy) or a chart's axis, then, for the first
// two, whether it hides its empty groups and the order its groups come in.
func (scope notionViewFilterScope) applyOfficialViewGrouping(viewType, key string, value any, config map[string]any) error {
	before, _ := config[key].(string)
	if err := scope.applyOfficialViewGroup(key, value, config); err != nil || key != "groupBy" && key != "subGroupBy" {
		if err == nil && key == "chartGroupBy" {
			scope.applyOfficialViewChartAxisSort(value, config)
		}
		return err
	}
	settings := config
	if key == "subGroupBy" {
		sub, _ := config["subGroup"].(map[string]any)
		if sub == nil {
			sub = map[string]any{}
		}
		settings = sub
		defer func() {
			if len(sub) == 0 {
				delete(config, "subGroup")
			} else {
				config["subGroup"] = sub
			}
		}()
	}
	group, _ := value.(map[string]any)
	after, _ := config[key].(string)
	if group == nil || after != before {
		delete(settings, "groupSort")
		delete(settings, "groupOrder")
	}
	delete(settings, "officialGroupType")
	if group == nil {
		return nil
	}
	field := map[string]string{"groupBy": "group_by", "subGroupBy": "sub_group_by"}[key]
	property := scope.byID[after]
	groupType, _ := notionViewGroupType(property.OfficialType)
	if err := applyOfficialViewEmptyGroups(field, groupType, group, settings); err != nil {
		return err
	}
	// One given as a formula's is kept as given and sorts by its group_by's sort (notion_view_table_formula_group.go).
	units := group
	if typeName, _ := group["type"].(string); typeName == "formula" {
		nested, flat, err := notionViewTableFormulaGroup(field, property, group)
		if err != nil {
			return err
		}
		given := map[string]any{"property": after, "type": typeName, "group_by": nested}
		if hide, ok := group["hide_empty_groups"].(bool); ok {
			given["hide_empty_groups"] = hide
		}
		settings["officialGroupType"] = given
		// Its sort goes by its family's: a date's and a number's by value (notionViewSortingGroupType).
		units, group = flat, map[string]any{"type": nested["type"], "sort": nested["sort"]}
		if _, told := given["hide_empty_groups"]; !told && !notionViewFormulaGroupApplies(property, nested["type"]) {
			settings["hideEmptyGroups"] = true
		}
	} else if typeName != groupType {
		settings["officialGroupType"] = map[string]any{"property": after, "type": typeName}
	}
	units = notionViewRollupUnits(property, units)
	if key == "subGroupBy" {
		applyOfficialViewGroupUnit(groupType, units, settings)
		if err := applyOfficialViewSubGroupDates(units, settings); err != nil {
			return err
		}
	}
	raw, given := group["sort"]
	if !given {
		return nil
	}
	sorting, _ := raw.(map[string]any)
	sort, named := sorting["type"].(string)
	switch {
	case sorting == nil || sorting["type"] == nil:
		return notionViewFilterInvalid("%s", notionFaultSaid("body", notionFaultMissing("body.configuration."+field+".sort.type")))
	case !named || !slices.Contains(notionViewGroupSorts, sort):
		return notionViewFilterInvalid("%s", notionBodyFault("body.configuration."+field+".sort.type",
			"`\"manual\"`, `\"ascending\"`, or `\"descending\"`", notionViewQuoted(sorting["type"])))
	}
	delete(settings, "groupSort")
	delete(settings, "groupOrder")
	none := mcpBoardNoValueGroup
	if viewType == "table" {
		none = mcpTableNoValueGroup
	}
	switch notionViewSortingGroupType(groupType, group) {
	case "person", "created_by", "last_edited_by", "checkbox":
		// By hand alone.
	case "number", "date", "created_time", "last_edited_time":
		if sort == "descending" {
			settings["groupSort"] = sort
		}
	case "status", "select", "multi_select":
		if sort != "manual" {
			settings["groupSort"] = sort
			break
		}
		named := property.Options
		if groupType == "status" {
			settings["groupSort"] = sort
			if group["group_by"] != "option" {
				named = property.Groups
			}
		}
		settings["groupOrder"] = notionViewGroupsByName(named, none)
	default:
		settings["groupSort"] = sort
	}
	return nil
}

// notionViewGroupsByName are an option property's groups, and the group for
// no value keyed none, by name in character codes, no value sorted as
// "uncategorized" (mcpBoardGroupOrder).
func notionViewGroupsByName(options []notionViewOption, none string) []any {
	named := make([][2]string, 0, len(options)+1)
	for _, option := range options {
		named = append(named, [2]string{option.Name, option.ID})
	}
	named = append(named, [2]string{"uncategorized", none})
	slices.SortStableFunc(named, func(left, right [2]string) int {
		return slices.Compare(utf16.Encode([]rune(left[0])), utf16.Encode([]rune(right[0])))
	})
	order := make([]any, 0, len(named))
	for _, option := range named {
		order = append(order, option[1])
	}
	return order
}

// officialViewGroupSort is how a view's groups sort as Notion's API reads it
// back from the view's settings (settings; a board's sub-groups' own), as
// the screen sorts them (tableGroupSort, boardSubgroupSort): a number's and
// a date's by value always, a person's and a box's by hand always; any
// other's as set, else an option's by hand and the rest — a text's, a
// relation's, a status's, a formula's — ascending until moved by hand, as
// Notion's picked on the screen read back (seen 2026-11-12: a text's, a
// title's, a formula's text's, a relation's and a status's sub-groups, a
// relation's table groups).
func officialViewGroupSort(groupType string, settings map[string]any) string {
	sort, _ := settings["groupSort"].(string)
	switch groupType {
	case "person", "created_by", "last_edited_by", "checkbox":
		return "manual"
	case "number", "date", "created_time", "last_edited_time":
		if sort == "descending" {
			return sort
		}
		return "ascending"
	}
	if slices.Contains(notionViewGroupSorts, sort) {
		return sort
	}
	// A chart's axis passes its own sort, empty while the chart sorts by its
	// values, which reads by hand.
	if _, given := settings["groupSort"]; given {
		return "manual"
	}
	switch settings["groupOrder"].(type) {
	case []any, []string:
		return "manual"
	}
	if groupType == "select" || groupType == "multi_select" {
		return "manual"
	}
	return "ascending"
}

// notionViewQuoted is a value as Notion's body validation quotes it: a string
// in double quotes, anything else as JSON writes it.
func notionViewQuoted(value any) string {
	if text, ok := value.(string); ok {
		return `"` + text + `"`
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "undefined"
	}
	return string(encoded)
}
