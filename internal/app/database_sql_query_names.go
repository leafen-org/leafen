package app

import "strings"

// A quoted name in a query names a property's column by its exact name, as
// Notion names properties: one data source may hold "Price" and "price",
// and Notion's API tells them apart by case, finding neither as "PRICE"
// (measured 2026-11-19 on the public API: a number "Price" and a formula
// "price" in one data source, each filtered by its own name only). SQLite
// reads names without case, so where a quoted name would reach another
// property's column than the one it names, the query is given that column
// or refused here:
//   - a name fetch's table gives one of two columns whose names differ only
//     by case reads that column (Written; databaseSQLLogicalColumns);
//   - a formula's or rollup's exact name, which SQL can't query, is refused
//     as no such column, with the hint that says so (databaseSQLColumnHint),
//     rather than read as the column SQLite finds for it in another case.
// A name with no column in another case, an unquoted word and a name the
// query gives a result or a table with AS are SQLite's to read.

// databaseSQLExactNames is a query's tokens with its quoted names read by
// their exact names, or the refusal of one that names a property SQL can't
// query.
func databaseSQLExactNames(tokens []databaseSQLToken, sources []databaseSQLAuthorizedSource) ([]databaseSQLToken, error) {
	exact, folded, written := map[string]bool{}, map[string]bool{}, map[string][]string{}
	for _, source := range sources {
		for _, column := range source.Columns {
			exact[column.Name] = true
			folded[databaseSQLFoldName(column.Name)] = true
			if column.Written != "" {
				written[column.Written] = append(written[column.Written], column.Name)
			}
		}
	}
	unqueried := map[string]bool{}
	for _, source := range sources {
		for _, property := range source.Unqueried {
			unqueried[property.Name] = true
		}
	}
	named := databaseSQLNamedWithAs(tokens)
	read := make([]databaseSQLToken, len(tokens))
	copy(read, tokens)
	for index, token := range read {
		name := token.Text
		if token.Kind != databaseSQLTokenQuotedIdentifier || exact[name] || named[databaseSQLFoldName(name)] {
			continue
		}
		if columns := written[name]; len(columns) == 1 {
			read[index].Text = columns[0]
			continue
		}
		if unqueried[name] && folded[databaseSQLFoldName(name)] {
			reason := "no such column: " + name
			return nil, databaseSQLQueryInvalid("the query didn't run: %s%s", reason, databaseSQLColumnHint(reason, sources))
		}
	}
	return read, nil
}

// databaseSQLNamedWithAs are the names a query gives with AS — a result's,
// a table's or a common table's — folded as SQLite reads them.
func databaseSQLNamedWithAs(tokens []databaseSQLToken) map[string]bool {
	named := map[string]bool{}
	for index, token := range tokens {
		if token.Kind != databaseSQLTokenWord || !strings.EqualFold(token.Text, "AS") || index+1 >= len(tokens) {
			continue
		}
		next := tokens[index+1]
		switch {
		case next.Kind == databaseSQLTokenWord && !strings.EqualFold(next.Text, "MATERIALIZED") &&
			!strings.EqualFold(next.Text, "NOT"), next.Kind == databaseSQLTokenQuotedIdentifier:
			// … AS name: a result's or a table's.
			named[databaseSQLTableName(next)] = true
		case index > 0:
			// WITH name AS (…): a common table's, before its AS.
			named[databaseSQLTableName(tokens[index-1])] = true
		}
	}
	return named
}
