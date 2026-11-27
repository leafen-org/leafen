package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// notion-query-data-sources's rows mode takes Notion's structured filter and
// sorts, by property names. They read into the conditions a view's configure
// writes, so one list of what each kind of property filters by holds.

type notionMCPRowsFilter struct {
	Type         string                `json:"type"`
	Operator     string                `json:"operator"`
	Filters      []notionMCPRowsFilter `json:"filters"`
	Property     string                `json:"property"`
	Value        json.RawMessage       `json:"value"`
	ResultFilter *notionMCPRowsFilter  `json:"resultFilter"`
}

// notionMCPRowsSort is one of rows mode's sorts.
type notionMCPRowsSort struct {
	Property  string `json:"property" jsonschema:"Exact data source property name to sort, or createdTime or lastEditedTime for when a row was created or last edited"`
	Direction string `json:"direction" jsonschema:"ascending or descending"`
}

var notionMCPRowsOperators = map[string]string{
	"string_is": "=", "string_is_not": "!=", "string_contains": "contains", "string_does_not_contain": "does_not_contain",
	"string_starts_with": "starts_with", "string_ends_with": "ends_with",
	"number_equals": "=", "number_does_not_equal": "!=", "number_greater_than": ">", "number_less_than": "<",
	"number_greater_than_or_equal_to": ">=", "number_less_than_or_equal_to": "<=",
	"date_is": "=", "date_is_before": "<", "date_is_after": ">", "date_is_on_or_before": "<=", "date_is_on_or_after": ">=",
	"date_is_within": "=", "date_is_relative_to": "=",
	"enum_is": "=", "enum_is_not": "!=", "enum_contains": "contains", "enum_does_not_contain": "does_not_contain",
	"status_is": "=", "status_is_not": "!=",
	"person_contains": "contains", "person_does_not_contain": "does_not_contain",
	"relation_contains": "contains", "relation_does_not_contain": "does_not_contain",
	"checkbox_is": "=", "checkbox_is_not": "!=",
	"is_empty": "is_empty", "is_not_empty": "is_not_empty",
}

// expression reads a filter into a view filter's expression.
func (filter notionMCPRowsFilter) expression() (mcpViewExpression, error) {
	if filter.Type == "group" {
		conjunction := databaseQueryConjunctionAnd
		if filter.Operator == "or" {
			conjunction = databaseQueryConjunctionOr
		} else if filter.Operator != "and" {
			return mcpViewExpression{}, mcpInvalid("a filter group's operator is and or or")
		}
		group := mcpViewExpression{conjunction: conjunction}
		for _, child := range filter.Filters {
			expression, err := child.expression()
			if err != nil {
				return mcpViewExpression{}, err
			}
			group.children = append(group.children, expression)
		}
		return group, nil
	}
	if filter.Type != "property" || strings.TrimSpace(filter.Property) == "" {
		return mcpViewExpression{}, mcpInvalid("a filter's type is group or property, and a property filter names its property")
	}
	// A formula's or rollup's filter asks of what it gives.
	if filter.ResultFilter != nil {
		result := *filter.ResultFilter
		result.Property = filter.Property
		if filter.Operator == "none" {
			return mcpViewExpression{}, mcpInvalid("a formula's or rollup's filter takes any or every")
		}
		return result.expression()
	}
	operator, known := notionMCPRowsOperators[filter.Operator]
	if !known {
		return mcpViewExpression{}, mcpInvalid("%q filters by no operator named %q", filter.Property, filter.Operator)
	}
	values, relative, err := notionMCPRowsFilterValues(filter.Value)
	if err != nil {
		return mcpViewExpression{}, mcpInvalid("%q: %s", filter.Property, err.Error())
	}
	if operator == "=" && len(values) == 2 && relative {
		operator = "is_between"
	}
	if operator == "=" && len(values) > 1 {
		operator = "in"
	}
	if operator == "contains" && len(values) > 1 {
		operator = "in"
	}
	return mcpViewExpression{property: filter.Property, operator: operator, values: values}, nil
}

// notionMCPRowsFilterValues reads a filter's value or values; relative is a
// date range's two ends.
func notionMCPRowsFilterValues(raw json.RawMessage) ([]string, bool, error) {
	if len(raw) == 0 {
		return nil, false, nil
	}
	items := []json.RawMessage{raw}
	if strings.HasPrefix(strings.TrimSpace(string(raw)), "[") {
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, false, fmt.Errorf("its value doesn't read")
		}
	}
	values := []string{}
	for _, item := range items {
		value := struct {
			Type      string `json:"type"`
			Value     any    `json:"value"`
			StartDate string `json:"start_date"`
			EndDate   string `json:"end_date"`
			Direction string `json:"direction"`
			Unit      string `json:"unit"`
			Count     int    `json:"count"`
		}{}
		if err := json.Unmarshal(item, &value); err != nil {
			return nil, false, fmt.Errorf("its value doesn't read")
		}
		switch inner := value.Value.(type) {
		case string:
			switch {
			case inner == "custom":
				past := map[string]string{"past": "past", "future": "next"}[value.Direction]
				if past == "" || value.Unit == "" || value.Count < 1 {
					return nil, false, fmt.Errorf("a custom range has a direction, a unit and a count")
				}
				values = append(values, past+"_"+strconv.Itoa(value.Count)+"_"+value.Unit+"s")
			case strings.HasPrefix(inner, "the_"):
				values = append(values, strings.TrimPrefix(inner, "the_"))
			default:
				values = append(values, inner)
			}
		case float64:
			values = append(values, strconv.FormatFloat(inner, 'f', -1, 64))
		case bool:
			values = append(values, strconv.FormatBool(inner))
		case map[string]any:
			start, _ := inner["start_date"].(string)
			end, _ := inner["end_date"].(string)
			if start == "" || end == "" {
				return nil, false, fmt.Errorf("a date range has a start_date and an end_date")
			}
			return []string{start, end}, true, nil
		default:
			return nil, false, fmt.Errorf("its value doesn't read")
		}
	}
	return values, false, nil
}

// mcpRowsFilterAndSorts reads rows mode's filter and sorts for a reader.
func mcpRowsFilterAndSorts(
	app core.App, source mcpDataSourceLocation, principalID string, raw map[string]any, sorts []notionMCPRowsSort,
) (*databaseQueryFilterGroup, []databaseQuerySort, error) {
	scope, err := mcpViewReaderScope(app, source, principalID)
	if err != nil {
		return nil, nil, failedBecause(errMCPToolFailed, err)
	}
	change := mcpViewConfigure{app: app, scope: scope}
	var group *databaseQueryFilterGroup
	if raw != nil {
		encoded, err := json.Marshal(raw)
		if err != nil {
			return nil, nil, failedBecause(errMCPToolFailed, err)
		}
		filter := notionMCPRowsFilter{}
		if err := json.Unmarshal(encoded, &filter); err != nil {
			return nil, nil, mcpInvalid("filter doesn't read: a group with its filters, or a property filter")
		}
		expression, err := filter.expression()
		if err != nil {
			return nil, nil, err
		}
		if group, err = change.filterGroup(expression); err != nil {
			return nil, nil, mcpRowsFilterError(err)
		}
	}
	result := make([]databaseQuerySort, 0, len(sorts))
	for _, sort := range sorts {
		property, err := change.property(sort.Property)
		if timestamp, isTimestamp := mcpRowsTimestampSorts[sort.Property]; err != nil && isTimestamp {
			// When a row was created or last edited, as the sql mode's
			// createdTime column and Notion's timestamp sorts name it, unless
			// the source has a property of that name.
			property, err = notionViewProperty{ID: timestamp}, nil
		}
		if err != nil {
			return nil, nil, mcpRowsFilterError(err)
		}
		direction, known := map[string]string{
			"ascending": databaseQuerySortAscending, "descending": databaseQuerySortDescending,
		}[sort.Direction]
		if !known {
			return nil, nil, mcpInvalid("a sort's direction is ascending or descending")
		}
		result = append(result, databaseQuerySort{PropertyID: property.ID, Direction: direction})
	}
	return group, result, nil
}

// mcpRowsTimestampSorts are the page timestamps rows mode sorts by, under
// the sql mode's column name and Notion's timestamp names.
var mcpRowsTimestampSorts = map[string]string{
	databaseSQLSyntheticCreatedColumn: databaseQueryRowCreatedTimeID,
	"created_time":                    databaseQueryRowCreatedTimeID,
	"lastEditedTime":                  databaseQueryRowEditedTimeID,
	"last_edited_time":                databaseQueryRowEditedTimeID,
}

// mcpRowsFilterError says what a view's conditions refused, without the
// configure string this call didn't send.
func mcpRowsFilterError(err error) error {
	if !errors.Is(err, errMCPToolInvalid) {
		return err
	}
	message := strings.TrimPrefix(err.Error(), errMCPToolInvalid.Error()+": ")
	return mcpInvalid("%s", strings.TrimPrefix(message, "configure: "))
}
