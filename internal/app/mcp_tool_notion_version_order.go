package app

import (
	"maps"
	"slices"
	"strings"
)

// The version tools write a database's state with its keys in Notion's
// order (seen 2026-11-14), not sorted as a map's: a property's, an option's,
// a view's and a row's values.

// valuesInOrder lists a row's values as Notion's version tools do (seen
// 2026-11-14): in its schema's order (mcpSQLiteTableOrder), a date's start,
// end and is_datetime in turn, a formula's where its schema has it; one the
// schema doesn't name follows, sorted.
func (source mcpVersionSource) valuesInOrder(values map[string]any) mcpOrderedObject {
	ordered := make(mcpOrderedObject, 0, len(values))
	placed := make(map[string]bool, len(values))
	for _, property := range mcpSQLiteTableOrder(mcpSQLiteVisibleProperties(source.Properties), source.IDs) {
		columns := mcpSQLiteColumns(property)
		if len(columns) == 0 {
			columns = []mcpSQLiteColumn{{Name: mcpSQLiteColumnName(property.Name)}}
		}
		for _, column := range columns {
			if value, found := values[column.Name]; found && !placed[column.Name] {
				placed[column.Name] = true
				ordered = append(ordered, mcpOrderedEntry{Key: column.Name, Value: value})
			}
		}
	}
	for _, key := range slices.Sorted(maps.Keys(values)) {
		if !placed[key] {
			ordered = append(ordered, mcpOrderedEntry{Key: key, Value: values[key]})
		}
	}
	return ordered
}

// mcpVersionKeys is the order Notion's version tools write a property's
// keys in (seen 2026-11-14): its type, name and description, then what its
// type adds — a formula's formula and result type before its description; a
// number's format; a select's options, each its url, name, description and
// color; a status's default option, then its groups, to do first; a relation's data
// source; a rollup's relation, target, aggregation and target's type; a
// date's query columns, their usage first, each column its name and SQL
// type. Keys it doesn't name follow, sorted.
var mcpVersionKeys = []string{
	"type", "url", "name", "formula", "resultType", "description", "number_format", "options", "color", "defaultOption", "groups",
	"to_do", "in_progress", "complete", "current", "future", "dataSourceUrl", "propertyUrl",
	"relationPropertyUrl", "targetPropertyUrl", "aggregation", "targetPropertyType",
	"querySqlColumns", "usage", "columns", "sqlType",
}

// mcpVersionKeyOrder writes a property's state with its keys, and those of
// what it holds, in Notion's order (mcpVersionKeys).
func mcpVersionKeyOrder(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		keys := slices.Collect(maps.Keys(typed))
		slices.SortFunc(keys, func(left, right string) int {
			leftRank, rightRank := mcpVersionKeyRank(left), mcpVersionKeyRank(right)
			if leftRank != rightRank {
				return leftRank - rightRank
			}
			return strings.Compare(left, right)
		})
		ordered := make(mcpOrderedObject, 0, len(keys))
		for _, key := range keys {
			ordered = append(ordered, mcpOrderedEntry{Key: key, Value: mcpVersionKeyOrder(typed[key])})
		}
		return ordered
	case []any:
		ordered := make([]any, len(typed))
		for index, item := range typed {
			ordered[index] = mcpVersionKeyOrder(item)
		}
		return ordered
	}
	return value
}

func mcpVersionKeyRank(key string) int {
	if rank := slices.Index(mcpVersionKeys, key); rank >= 0 {
		return rank
	}
	return len(mcpVersionKeys)
}

// mcpVersionViewKeys lead a view's keys in view-version's configuration, as
// Notion's (seen 2026-11-14); the rest follow them sorted.
var mcpVersionViewKeys = []string{"type", "name", "dataSourceUrl", "displayProperties"}

// mcpVersionViewOrder is a view's state with its keys in that order.
func mcpVersionViewOrder(config map[string]any) mcpOrderedObject {
	ordered := make(mcpOrderedObject, 0, len(config))
	for _, key := range mcpVersionViewKeys {
		if value, found := config[key]; found {
			ordered = append(ordered, mcpOrderedEntry{Key: key, Value: value})
		}
	}
	for _, key := range slices.Sorted(maps.Keys(config)) {
		if !slices.Contains(mcpVersionViewKeys, key) {
			ordered = append(ordered, mcpOrderedEntry{Key: key, Value: config[key]})
		}
	}
	return ordered
}
