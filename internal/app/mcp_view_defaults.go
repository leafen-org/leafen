package app

import (
	"slices"
	"unicode/utf16"
)

// A view an agent makes starts as a person's new view does on the screen
// (the web's newViewDefaults, seen in Notion 2026-10-04 to 2026-11-04): the
// layouts of cards and lines show the title alone, a board groups by a status
// first, a list shows its first person, date, select or status beside the
// title and a gallery previews the page's content. A table beside other views
// shows what the source's first table shows. A chart says what it charts, or
// is refused as Notion's is (mcpNewChartRefusal).

var mcpTitleAloneLayouts = []string{
	graphViewTypeBoard, graphViewTypeGallery, graphViewTypeList,
	graphViewTypeCalendar, graphViewTypeTimeline, graphViewTypeFeed,
}

// mcpBoardGroupPreference is the order a board picks the property it groups
// by, as the web's boardGroupDefault: a status, a select, a person, a
// multi-select, a relation, and nothing else (Notion's, seen 2026-11-08), the
// first of the kind in the order the API lists them (seen 2026-11-12).
var mcpBoardGroupPreference = []string{
	graphValueTypeStatus, graphValueTypeSelect, graphValueTypePerson, graphValueTypeMultiSelect, graphValueTypeRelation,
}

// The keys a board's columns and sub-groups, and a table's groups, give the
// group for no value (the web's NO_BOARD_GROUP and NO_TABLE_GROUP).
const (
	mcpBoardNoValueGroup = "__none"
	mcpTableNoValueGroup = "__no_group__"
)

var mcpNewListShownTypes = []string{
	graphValueTypePerson, graphValueTypeDate, graphValueTypeMultiSelect, graphValueTypeSelect, graphValueTypeStatus,
}

// mcpNewViewConfig is a new view's first settings; properties stand in the
// order the API lists them, the first of a kind its pick
// (notionPublicOrderedProperties), and siblings are the other views of its
// source.
func mcpNewViewConfig(
	viewType string, properties []notionMCPDataSourceProperty, siblings []notionMCPDataSourceView,
) map[string]any {
	config := map[string]any{"rowOrder": []any{}}
	if slices.Contains(mcpTitleAloneLayouts, viewType) {
		config["visibleProperties"] = []any{}
	}
	first := func(types ...string) (notionMCPDataSourceProperty, bool) {
		for _, kind := range types {
			for _, property := range properties {
				if property.Type == kind {
					return property, true
				}
			}
		}
		return notionMCPDataSourceProperty{}, false
	}
	switch viewType {
	case graphViewTypeTable:
		sibling := slices.IndexFunc(siblings, func(view notionMCPDataSourceView) bool { return view.Type == graphViewTypeTable })
		if sibling >= 0 {
			for _, key := range []string{"hiddenProperties", "visibleProperties", "propertyOrder"} {
				if value, kept := siblings[sibling].Config[key]; kept {
					config[key] = value
				}
			}
		} else if len(siblings) == 0 {
			config["wrap"] = true
		}
	case graphViewTypeBoard:
		config["wrap"] = true
		if property, found := first(mcpBoardGroupPreference...); found {
			config["groupBy"] = property.ID
			if order := mcpBoardGroupOrder(property, mcpBoardNoValueGroup); order != nil {
				config["groupOrder"] = order
			}
		}
	case graphViewTypeGallery:
		config["coverProperty"] = notionViewCardPreviews["page_content"]
	case graphViewTypeList:
		for _, property := range properties {
			if slices.Contains(mcpNewListShownTypes, property.Type) {
				config["visibleProperties"] = []any{property.ID}
				break
			}
		}
	}
	return config
}

// mcpBoardGroupOrder is the order a view's groups by a property's options
// start in, new or grouped by it, as a person's do on the screen
// (boardOptionGroupOrder, Notion's seen 2026-11-07 and 2026-11-08 for a
// board, a table and a board's sub-groups): the options and the group for no
// value, keyed `none`, by name in the screen's character codes, no value
// sorted as "uncategorized".
func mcpBoardGroupOrder(property notionMCPDataSourceProperty, none string) []any {
	if property.Type != graphValueTypeSelect && property.Type != graphValueTypeMultiSelect {
		return nil
	}
	options, _ := property.Config["options"].([]any)
	named := make([][2]string, 0, len(options)+1)
	for _, raw := range options {
		option, _ := raw.(map[string]any)
		id, identified := option["id"].(string)
		name, titled := option["name"].(string)
		if identified && titled {
			named = append(named, [2]string{name, id})
		}
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

// mcpRegroupedView gives a view an agent groups, or a board it sub-groups, by
// another option property that property's first order, as a person's pick on
// the screen does (regroupedViewConfig, the sub-group's pick). before and
// beforeSub are what it grouped and sub-grouped by.
func mcpRegroupedView(viewType string, config map[string]any, before, beforeSub string, properties []notionMCPDataSourceProperty) {
	find := func(id string) (notionMCPDataSourceProperty, bool) {
		index := slices.IndexFunc(properties, func(property notionMCPDataSourceProperty) bool { return property.ID == id })
		if index < 0 {
			return notionMCPDataSourceProperty{}, false
		}
		return properties[index], true
	}
	after, _ := config["groupBy"].(string)
	if _, kept := config["groupOrder"]; !kept && after != "" && after != before {
		none := mcpTableNoValueGroup
		if viewType == graphViewTypeBoard {
			none = mcpBoardNoValueGroup
		}
		if property, found := find(after); found {
			if order := mcpBoardGroupOrder(property, none); order != nil {
				config["groupOrder"] = order
			}
		}
	}
	afterSub, _ := config["subGroupBy"].(string)
	if viewType != graphViewTypeBoard || afterSub == "" || afterSub == beforeSub {
		return
	}
	sub, _ := config["subGroup"].(map[string]any)
	if _, kept := sub["groupOrder"]; kept {
		return
	}
	if property, found := find(afterSub); found {
		if order := mcpBoardGroupOrder(property, mcpBoardNoValueGroup); order != nil {
			if sub == nil {
				sub = map[string]any{}
			}
			sub["groupOrder"] = order
			config["subGroup"] = sub
		}
	}
}
