package app

import (
	"encoding/json"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// What a configure string says of a view's filters (mcp_view_configure.go).

// quickFilters adds properties to the view's filter bar, each as the screen
// adds it, or with the condition given; the others stay.
func (change *mcpViewConfigure) quickFilters(words *mcpViewWords) error {
	current, _ := change.config["quickFilters"].([]any)
	for {
		name, ok := words.quoted()
		if !ok {
			return change.unreadable("QUICK FILTER", `QUICK FILTER "Property" [operator value], "Property"`)
		}
		property, err := change.property(name)
		if err != nil {
			return err
		}
		var filter databaseQueryFilter
		if words.done() || words.tokens[words.at].kind == mcpViewTokenMark && words.tokens[words.at].text == "," {
			if slices.ContainsFunc(current, func(item any) bool {
				entry, _ := item.(map[string]any)
				return entry["propertyId"] == property.ID
			}) {
				if !words.mark(",") {
					break
				}
				continue
			}
			filter = databaseQueryFilter{PropertyID: property.ID, Operator: mcpViewQuickFilterOperator(property), Value: json.RawMessage("null")}
		} else {
			words.at--
			condition, ok := mcpViewCondition(words)
			if !ok {
				return change.unreadable("QUICK FILTER", `QUICK FILTER "Property" [operator value], "Property"`)
			}
			if condition.operator == "in" {
				return mcpInvalid("configure: a quick filter holds one condition, without IN")
			}
			group := databaseQueryFilterGroup{Conjunction: databaseQueryConjunctionAnd}
			if err := change.addCondition(&group, condition); err != nil {
				return err
			}
			if len(group.Filters) != 1 {
				return mcpInvalid("configure: a quick filter holds one condition")
			}
			filter = group.Filters[0]
		}
		current = slices.DeleteFunc(current, func(item any) bool {
			entry, _ := item.(map[string]any)
			return entry["propertyId"] == property.ID
		})
		encoded, err := json.Marshal(filter)
		if err != nil {
			return err
		}
		entry := map[string]any{}
		if err := json.Unmarshal(encoded, &entry); err != nil {
			return err
		}
		current = append(current, entry)
		if !words.mark(",") {
			break
		}
	}
	change.config["quickFilters"] = current
	return nil
}

// mcpViewQuickFilterOperator is the condition a new filter starts on, as the
// screen's filter bar adds it.
func mcpViewQuickFilterOperator(property notionViewProperty) string {
	switch property.NativeType {
	case graphValueTypeDate:
		return "this_week"
	case graphValueTypeCreatedTime, graphValueTypeLastEditedTime, graphValueTypeCheckbox:
		return "equals"
	case graphValueTypeFiles, graphValueTypePlace:
		return "is_not_empty"
	case graphValueTypeNumber, graphValueTypeUniqueID:
		return "equals"
	}
	return "contains"
}

func (change *mcpViewConfigure) filterGroup(expression mcpViewExpression) (*databaseQueryFilterGroup, error) {
	group := &databaseQueryFilterGroup{Conjunction: databaseQueryConjunctionAnd}
	if expression.conjunction != "" {
		group.Conjunction = expression.conjunction
		for _, child := range expression.children {
			if child.conjunction == "" {
				if err := change.addCondition(group, child); err != nil {
					return nil, err
				}
				continue
			}
			nested, err := change.filterGroup(child)
			if err != nil {
				return nil, err
			}
			group.Groups = append(group.Groups, *nested)
			group.Order = append(group.Order, databaseQueryFilterTermGroup)
		}
		return group, nil
	}
	return group, change.addCondition(group, expression)
}

// mcpViewRelativeWindow reads this_week, past_month, past_3_days or
// next_2_weeks, which stay relative as the screen's do.
var mcpViewRelativeWindow = regexp.MustCompile(`^(past|next)_(\d+)_(day|week|month|year)s?$`)

// addCondition adds a condition to a group, as the property's kind takes it.
func (change *mcpViewConfigure) addCondition(group *databaseQueryFilterGroup, condition mcpViewExpression) error {
	property, err := change.property(condition.property)
	if err != nil {
		return err
	}
	family := notionViewConditionFamily(property.OfficialType)
	refused := func() error {
		return mcpInvalid("configure: a %s property doesn't filter by %s", property.OfficialType, condition.operator)
	}
	add := func(operator string, value any) error {
		if !databaseQueryFilterOperatorAllowed(property.NativeType, operator) {
			return refused()
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return err
		}
		group.Filters = append(group.Filters, databaseQueryFilter{PropertyID: property.ID, Operator: operator, Value: encoded})
		group.Order = append(group.Order, databaseQueryFilterTermFilter)
		return nil
	}
	if condition.operator == "is_empty" || condition.operator == "is_not_empty" {
		return add(condition.operator, nil)
	}
	if family == "files" || property.NativeType == graphValueTypePlace {
		return mcpInvalid("configure: a %s property filters only by IS EMPTY and IS NOT EMPTY", property.OfficialType)
	}
	if condition.operator == "in" && (family == "text" || family == "number" || family == "date" || family == "checkbox" || family == "") {
		any := databaseQueryFilterGroup{Conjunction: databaseQueryConjunctionOr}
		for _, value := range condition.values {
			if err := change.addCondition(&any, mcpViewExpression{property: condition.property, operator: "=", values: []string{value}}); err != nil {
				return err
			}
		}
		group.Groups = append(group.Groups, any)
		group.Order = append(group.Order, databaseQueryFilterTermGroup)
		return nil
	}
	value := ""
	if len(condition.values) > 0 {
		value = condition.values[0]
	}
	switch family {
	case "number":
		operator := map[string]string{"=": "equals", "!=": "does_not_equal", ">": "greater_than", "<": "less_than", ">=": "greater_than_or_equal_to", "<=": "less_than_or_equal_to"}[condition.operator]
		number, err := strconv.ParseFloat(value, 64)
		if operator == "" || err != nil {
			return mcpInvalid("configure: a number property compares with =, !=, >, <, >= and <= and a number; %q has %s %q", condition.property, condition.operator, value)
		}
		return add(operator, number)
	case "checkbox":
		wanted, err := strconv.ParseBool(strings.ToLower(value))
		operator := map[string]string{"=": "equals", "!=": "does_not_equal"}[condition.operator]
		if operator == "" || err != nil {
			return mcpInvalid("configure: a checkbox property compares with = or != and TRUE or FALSE")
		}
		return add(operator, wanted)
	case "select", "multi":
		values := make([]any, 0, len(condition.values))
		for _, name := range condition.values {
			values = append(values, name)
		}
		operator := map[string]string{"=": "equals", "!=": "does_not_equal", "in": "equals", "contains": "contains", "does_not_contain": "does_not_contain"}[condition.operator]
		if family == "multi" {
			operator = map[string]string{"=": "contains", "!=": "does_not_contain", "in": "contains", "contains": "contains", "does_not_contain": "does_not_contain"}[condition.operator]
		}
		if operator == "" {
			return refused()
		}
		return add(operator, values)
	case "people", "relation":
		operator := map[string]string{"=": "contains", "contains": "contains", "in": "contains", "!=": "does_not_contain", "does_not_contain": "does_not_contain"}[condition.operator]
		if operator == "" {
			return refused()
		}
		if family == "people" && len(condition.values) == 1 && strings.EqualFold(value, "me") {
			return add(operator, map[string]any{"kind": "leafen.current_user"})
		}
		ids := make([]any, 0, len(condition.values))
		for _, raw := range condition.values {
			// A template's "this page": each page made from it, its own
			// (database_template_linked_views.go), and a view in a page,
			// that page (databaseViewConfigOnPage) — as the screen's choice.
			if family == "relation" && mcpViewThisPage(raw) {
				ids = append(ids, map[string]any{"kind": "leafen.current_page"})
				continue
			}
			id, err := change.identity(family, raw, condition.property)
			if err != nil {
				return err
			}
			ids = append(ids, id)
		}
		return add(operator, ids)
	case "date":
		return change.addDateCondition(condition, add)
	}
	// Text, and what a formula or rollup gives.
	operator := map[string]string{
		"=": "equals", "!=": "does_not_equal", "contains": "contains", "does_not_contain": "does_not_contain",
		"starts_with": "starts_with", "ends_with": "ends_with", ">": "greater_than", "<": "less_than",
		">=": "greater_than_or_equal_to", "<=": "less_than_or_equal_to",
	}[condition.operator]
	if operator == "" {
		return refused()
	}
	return add(operator, value)
}

func (change *mcpViewConfigure) identity(family, raw, name string) (string, error) {
	if family == "people" {
		id := notionPersonReferenceID(raw)
		if native, ok := change.scope.userToNative(id); ok {
			return native, nil
		}
		if _, isUUID := normalizeNotionUUIDRequest(id); !isUUID && id != "" && !strings.ContainsAny(id, " /") {
			return id, nil
		}
		return "", mcpInvalid("configure: %q filters by a person's user://… address, ID or \"me\", not a name", name)
	}
	id := mcpLocalID(change.app, raw)
	if id == "" || strings.ContainsAny(id, " /") {
		return "", mcpInvalid("configure: %q filters by a page's address or ID, or \"this page\", not a name", name)
	}
	return id, nil
}

// addDateCondition adds a date condition. The days Notion's filters name by a
// word (databaseQueryRelativeFilterDays) stay that word in a comparison, as
// Notion's view keeps it relative (seen 2026-11-16: `FILTER "Since" <=
// "today"` read back as {"type":"relative","value":"today"}), and the filter
// reads it on the reader's day (databaseQueryRelativeFilterDay). A range's
// ends stand for their days when written: a word's day on the connection's
// clock, the person's, so "today" written at 03:00 in Seoul is Seoul's day,
// not UTC's yesterday (2026-11-26).
func (change *mcpViewConfigure) addDateCondition(condition mcpViewExpression, add func(string, any) error) error {
	day := func(raw string) (string, error) {
		word := strings.ToLower(strings.TrimSpace(raw))
		if named, relative := databaseQueryRelativeFilterDay(word, time.Now(), change.clock); relative {
			return named, nil
		}
		parsed, err := time.Parse("2006-01-02", strings.SplitN(strings.TrimSpace(raw), "T", 2)[0])
		if err != nil {
			return "", mcpInvalid("configure: %q compares with a date as 2026-01-31, or today, tomorrow, yesterday, one_week_ago, one_week_from_now, one_month_ago or one_month_from_now; not %q", condition.property, raw)
		}
		return parsed.Format("2006-01-02"), nil
	}
	if condition.operator == "is_between" {
		start, err := day(condition.values[0])
		if err != nil {
			return err
		}
		end, err := day(condition.values[1])
		if err != nil {
			return err
		}
		return add("is_between", map[string]any{"start": start, "end": end})
	}
	value := condition.values[0]
	word := strings.ToLower(strings.TrimSpace(value))
	if condition.operator == "=" {
		switch word {
		case "today":
			return add("relative_to_today", map[string]any{"direction": "this", "unit": "day", "count": 1})
		case "this_week", "past_week", "past_month", "past_year", "next_week", "next_month", "next_year":
			return add(word, nil)
		case "this_month", "this_year":
			return add("relative_to_today", map[string]any{"direction": "this", "unit": strings.TrimPrefix(word, "this_"), "count": 1})
		}
		if match := mcpViewRelativeWindow.FindStringSubmatch(word); match != nil {
			count, err := strconv.Atoi(match[2])
			if err != nil || count < 1 {
				return mcpInvalid("configure: %q is past_ or next_ a count of at least 1", value)
			}
			return add("relative_to_today", map[string]any{"direction": match[1], "unit": match[3], "count": count})
		}
	}
	operator := map[string]string{"=": "equals", ">": "after", "<": "before", ">=": "on_or_after", "<=": "on_or_before"}[condition.operator]
	if operator == "" {
		return mcpInvalid("configure: a date property compares with =, >, <, >=, <=, BETWEEN and IS EMPTY")
	}
	if _, relative := databaseQueryRelativeFilterDays[word]; relative {
		return add(operator, word)
	}
	parsed, err := day(value)
	if err != nil {
		return err
	}
	return add(operator, parsed)
}

// mcpViewThisPage is a relation filter's word for the page the view is on,
// or for each page a template makes: "this page" or this_page.
func mcpViewThisPage(raw string) bool {
	word := strings.ToLower(strings.TrimSpace(raw))
	return word == "this page" || word == "this_page"
}
