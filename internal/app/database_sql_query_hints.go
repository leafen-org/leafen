package app

import (
	"fmt"
	"strings"
)

// databaseSQLUnqueried is a property a data source's table has no column
// for, such as a formula or a rollup (notAvailableInQuerySql), by its name
// and its type as fetch names it.
type databaseSQLUnqueried struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// databaseSQLUnqueriedProperties are the properties of a source that SQL
// can't read.
func databaseSQLUnqueriedProperties(properties []notionMCPDataSourceProperty) []databaseSQLUnqueried {
	unqueried := []databaseSQLUnqueried{}
	for _, property := range properties {
		if len(mcpSQLiteColumns(property)) == 0 {
			unqueried = append(unqueried, databaseSQLUnqueried{
				Name: strings.TrimSpace(property.Name), Type: mcpNotionPropertyType(property.Type),
			})
		}
	}
	return unqueried
}

// databaseSQLColumnHint follows SQLite's "no such column" as Notion's SQL
// does (seen 2026-11-16): why a property's name isn't a column — a date's
// columns are date:<name>:start and :end, a formula's or rollup's values
// aren't in SQL but come in rows or view mode (a page's fetch gives one
// row's) — and the columns of each source the query reads.
func databaseSQLColumnHint(reason string, sources []databaseSQLAuthorizedSource) string {
	name, missing := strings.CutPrefix(reason, "no such column: ")
	if !missing || len(sources) == 0 {
		return ""
	}
	names := []string{strings.Trim(name, `"`)}
	if _, column, qualified := strings.Cut(names[0], "."); qualified {
		names = append(names, strings.Trim(column, `"`))
	}
	hint := ""
	for _, source := range sources {
		for _, wanted := range names {
			parts := []string{}
			for _, column := range source.Columns {
				prefix := mcpSQLiteColumnPrefix(column)
				if column.Part != "" && prefix != "" && strings.HasPrefix(column.Name, prefix+wanted+":") {
					parts = append(parts, fmt.Sprintf("%q", column.Name))
				}
			}
			if len(parts) > 0 && hint == "" {
				hint = fmt.Sprintf(" %q has no column of its own name; use %s.", wanted, strings.Join(parts, " and "))
			}
		}
	}
	// A formula or rollup named exactly, else in another case, as SQLite
	// reads names; "price" names the formula beside a number "Price".
	for _, same := range []func(string, string) bool{func(a, b string) bool { return a == b }, strings.EqualFold} {
		for _, source := range sources {
			for _, wanted := range names {
				for _, property := range source.Unqueried {
					if hint == "" && same(property.Name, wanted) {
						hint = fmt.Sprintf(" %q is a %s property, which SQL cannot query: rows mode or view mode gives its values.", wanted, property.Type)
					}
				}
			}
		}
	}
	for _, source := range sources {
		listed := make([]string, 0, len(source.Columns))
		for _, column := range source.Columns {
			kind := mcpNotionPropertyType(column.PropertyType)
			if column.Synthetic {
				kind = "metadata"
			}
			listed = append(listed, fmt.Sprintf("%q %s", column.Name, kind))
		}
		hint += fmt.Sprintf(" Columns of %q: %s.", source.URL, strings.Join(listed, ", "))
	}
	return "." + hint + " Use the exact column names above; single quotes for text."
}

// mcpSQLiteColumnPrefix is the kind a property's expanded columns begin
// with: "date" for date:<name>:start, "place" for place:<name>:name.
func mcpSQLiteColumnPrefix(column databaseSQLLogicalColumn) string {
	kind, _, found := strings.Cut(column.Name, ":")
	if !found {
		return ""
	}
	return kind + ":"
}

// databaseSQLUnreadWarnings say which data_source_urls the query didn't
// read and so left out, where a query reading none of them is refused.
func databaseSQLUnreadWarnings(plan databaseSQLQueryPlan) []string {
	warnings := []string{}
	for _, url := range plan.Unread {
		warnings = append(warnings, fmt.Sprintf("data_source_urls names %s, which the query doesn't read; it was left out", url))
	}
	return warnings
}
