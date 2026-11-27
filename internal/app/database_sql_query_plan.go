package app

import (
	"encoding/json"
	"slices"
	"strings"
)

type databaseSQLQueryPlan struct {
	Query             string
	Params            []any
	DataSourceURLs    []string
	ReferencedSources []string
	Tokens            []databaseSQLToken
	// Unread are data_source_urls the query doesn't read, left out.
	Unread []string `json:"-"`
}

var databaseSQLForbiddenWords = map[string]struct{}{
	"ALTER": {}, "ANALYZE": {}, "ATTACH": {}, "CREATE": {}, "DELETE": {},
	"DETACH": {}, "DROP": {}, "INSERT": {}, "PRAGMA": {}, "REINDEX": {},
	"REPLACE": {}, "TRIGGER": {}, "UPDATE": {}, "VACUUM": {},
}

// databaseSQLAllowedFunctions are SQLite's functions of values the query
// may call, as Notion's SQLite takes them (seen 2026-11-16: GROUP_CONCAT and
// json_each of a relation's column ran there); none reads a table or the
// server. json_each and json_tree go in FROM (databaseSQLSourceWord).
var databaseSQLAllowedFunctions = map[string]struct{}{
	"ABS": {}, "AVG": {}, "CAST": {}, "CHAR": {}, "COALESCE": {}, "COUNT": {},
	"DATE": {}, "DATETIME": {}, "GLOB": {}, "GROUP_CONCAT": {}, "HEX": {}, "IFNULL": {}, "IIF": {},
	"INSTR": {}, "JSON": {}, "JSON_ARRAY": {}, "JSON_ARRAY_LENGTH": {}, "JSON_EXTRACT": {},
	"JSON_GROUP_ARRAY": {}, "JSON_GROUP_OBJECT": {}, "JSON_OBJECT": {}, "JSON_QUOTE": {},
	"JSON_TYPE": {}, "JSON_VALID": {}, "JULIANDAY": {}, "LENGTH": {}, "LIKE": {},
	"LOWER": {}, "LTRIM": {}, "MAX": {}, "MIN": {}, "NULLIF": {}, "PRINTF": {}, "QUOTE": {},
	"REPLACE": {}, "ROUND": {}, "RTRIM": {}, "SIGN": {}, "STRFTIME": {},
	"SUBSTR": {}, "SUBSTRING": {}, "SUM": {}, "TIME": {}, "TOTAL": {}, "TRIM": {}, "TYPEOF": {},
	"UNICODE": {}, "UNIXEPOCH": {}, "UPPER": {},
}

// databaseSQLFunctionList names the functions a refusal of another lists.
func databaseSQLFunctionList() string {
	names := make([]string, 0, len(databaseSQLAllowedFunctions))
	for name := range databaseSQLAllowedFunctions {
		names = append(names, name)
	}
	slices.Sort(names)
	return strings.Join(names, ", ")
}

var databaseSQLSourceClauseTerminators = map[string]struct{}{
	"WHERE": {}, "GROUP": {}, "HAVING": {}, "ORDER": {}, "LIMIT": {},
	"OFFSET": {}, "UNION": {}, "INTERSECT": {}, "EXCEPT": {}, "WINDOW": {},
}

func compileDatabaseSQLQueryPlan(input notionMCPQueryDataSourcesData) (databaseSQLQueryPlan, error) {
	plan := databaseSQLQueryPlan{
		Query: strings.TrimSpace(input.Query), Params: append([]any(nil), input.Params...),
	}
	if strings.TrimSpace(input.Mode) != mcpDataSourceQueryModeSQL || plan.Query == "" ||
		len(input.DataSourceURLs) == 0 || len(input.DataSourceURLs) > maxDatabaseSQLQuerySources ||
		strings.TrimSpace(input.DataSourceURL) != "" || input.Limit != 0 ||
		len(plan.Params) > maxDatabaseSQLQueryParameters {
		return databaseSQLQueryPlan{}, databaseSQLQueryInvalid("mode sql takes query, data_source_urls (1 to %d collection:// URLs) and params (at most %d), without data_source_url or limit", maxDatabaseSQLQuerySources, maxDatabaseSQLQueryParameters)
	}
	declared := make(map[string]struct{}, len(input.DataSourceURLs))
	for _, raw := range input.DataSourceURLs {
		id, ok := parseMCPDataSourceURL(raw)
		if !ok {
			return databaseSQLQueryPlan{}, databaseSQLQueryInvalid("each of data_source_urls is a collection://… URL, as notion-fetch gives it")
		}
		url := mcpDataSourceURLPrefix + id
		if local, named := input.sourceNames[url]; named {
			url = local
		}
		if _, exists := declared[url]; exists {
			return databaseSQLQueryPlan{}, databaseSQLQueryInvalid("data_source_urls names %s twice", url)
		}
		declared[url] = struct{}{}
		plan.DataSourceURLs = append(plan.DataSourceURLs, url)
	}
	parameterBytes := 0
	for _, value := range plan.Params {
		if !databaseSQLParameterAllowed(value) {
			return databaseSQLQueryPlan{}, databaseSQLQueryInvalid("params are text, numbers, true or false, or null")
		}
		if text, ok := value.(string); ok {
			parameterBytes += len(text)
			if parameterBytes > maxDatabaseSQLParameterBytes {
				return databaseSQLQueryPlan{}, databaseSQLQueryInvalid("params together are at most %d bytes", maxDatabaseSQLParameterBytes)
			}
		}
	}
	tokens, err := lexDatabaseSQLQuery(plan.Query)
	if err != nil {
		return databaseSQLQueryPlan{}, err
	}
	// A data source named by its public ID is its table by the tools' name.
	for index, token := range tokens {
		if id, ok := parseMCPDataSourceURL(token.Text); ok && token.Kind == databaseSQLTokenQuotedIdentifier {
			if local, named := input.sourceNames[mcpDataSourceURLPrefix+id]; named {
				tokens[index].Text = local
			}
		}
	}
	if len(tokens) == 0 || tokens[0].Kind != databaseSQLTokenWord || tokens[0].Text != "SELECT" && tokens[0].Text != "WITH" {
		return databaseSQLQueryPlan{}, databaseSQLQueryInvalid("the query is one SELECT, after WITH and its tables if it has them")
	}
	plan.Tokens = tokens
	referenced, placeholders, err := validateDatabaseSQLTokens(tokens, declared)
	if err != nil {
		return databaseSQLQueryPlan{}, err
	}
	if placeholders != len(plan.Params) {
		return databaseSQLQueryPlan{}, databaseSQLQueryInvalid("the query has %d ? and params gives %d values", placeholders, len(plan.Params))
	}
	if len(referenced) == 0 {
		return databaseSQLQueryPlan{}, databaseSQLQueryInvalid("the query reads one or more of data_source_urls in FROM or JOIN")
	}
	// A data source listed and not read is left out with a warning
	// (databaseSQLUnreadWarnings), so the query runs as if not listed.
	read := plan.DataSourceURLs[:0:0]
	for _, url := range plan.DataSourceURLs {
		if slices.Contains(referenced, url) {
			read = append(read, url)
		} else {
			plan.Unread = append(plan.Unread, url)
		}
	}
	plan.DataSourceURLs = read
	plan.ReferencedSources = referenced
	return plan, nil
}

// validateDatabaseSQLTokens checks what a query reads and its parameters
// (database_sql_query_walk.go): the data sources it reads, in order, and
// its ? count.
func validateDatabaseSQLTokens(
	tokens []databaseSQLToken,
	declared map[string]struct{},
) ([]string, int, error) {
	walk, _, err := walkDatabaseSQLQuery(tokens, declared, nil)
	if err != nil {
		return nil, 0, err
	}
	return walk.referenced, walk.placeholders, nil
}

// databaseSQLSourceWord is a word FROM or JOIN may read: json_each or
// json_tree of what follows in parentheses, as Notion's SQL takes them to
// read a relation's page URLs (seen 2026-11-16: `json_each("Client")`).
// A SELECT in their argument is read as any other, so they read only values
// of what the query may read.
func databaseSQLSourceWord(tokens []databaseSQLToken, index int) bool {
	word := tokens[index].Text
	return (word == "JSON_EACH" || word == "JSON_TREE") &&
		index+1 < len(tokens) && tokens[index+1].Kind == databaseSQLTokenLeftParen
}

// databaseSQLWordIsSyntax is a word of SQL's own a parenthesis may follow:
// FROM and JOIN a SELECT, USING its columns, IS and BETWEEN a value.
func databaseSQLWordIsSyntax(word string) bool {
	switch word {
	case "AND", "AS", "BETWEEN", "BY", "CASE", "DISTINCT", "ELSE", "END", "EXISTS",
		"FILTER", "FROM", "GROUP", "HAVING", "IN", "IS", "JOIN", "LIMIT", "NOT", "OFFSET", "ON",
		"OR", "ORDER", "OVER", "SELECT", "THEN", "USING", "WHEN", "WHERE":
		return true
	default:
		return false
	}
}

func databaseSQLParameterAllowed(value any) bool {
	switch value.(type) {
	case nil, bool, string, float64, json.Number:
		return true
	default:
		return false
	}
}
