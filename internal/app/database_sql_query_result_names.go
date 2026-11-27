package app

import "strings"

// A result column the query doesn't name with AS is named by SQLite: a
// column by the name it is read by, anything else by its text as the query
// writes it — SELECT CAST("Price" AS TEXT) gives a column
// CAST("Price" AS TEXT), as Notion's SQL names it. The query runs rewritten,
// its quoted names in backquotes and its words and signs spaced apart
// (renderDatabaseSQLTokens), so SQLite names such a column by the rewritten
// text, CAST ( `Price` AS TEXT ) (2026-11-19). Each column of the query's
// first SELECT takes back the name its text as written gives, the ":1"
// SQLite adds to a name it gave already included; the columns of a * keep
// theirs.

// databaseSQLResultName is a result column of the query's first SELECT: the
// text SQLite reads it by as it runs, and as the query writes it. Ran is
// empty for one named with AS; star is a * or table.*.
type databaseSQLResultName struct {
	ran, written string
	star         bool
}

// databaseSQLResultNames are the result columns of the query's first
// SELECT outside parentheses, after WITH's tables: the query's result's;
// parts are its tokens as they run (renderDatabaseSQLTokens).
func databaseSQLResultNames(query string, tokens []databaseSQLToken, parts []string) []databaseSQLResultName {
	if len(parts) != len(tokens) {
		return nil
	}
	at, depth := -1, 0
	for index := 0; index < len(tokens) && at < 0; index++ {
		depth += databaseSQLDepthChange(tokens[index])
		if depth == 0 && tokens[index].Kind == databaseSQLTokenWord && tokens[index].Text == "SELECT" {
			at = index + 1
		}
	}
	if at < 0 {
		return nil
	}
	if at < len(tokens) && tokens[at].Kind == databaseSQLTokenWord && (tokens[at].Text == "DISTINCT" || tokens[at].Text == "ALL") {
		at++
	}
	names := []databaseSQLResultName{}
	first := at
	for index := at; index <= len(tokens); index++ {
		if index < len(tokens) {
			depth += databaseSQLDepthChange(tokens[index])
			if depth != 0 || tokens[index].Kind == databaseSQLTokenRightParen ||
				tokens[index].Kind != databaseSQLTokenComma && !databaseSQLResultListEnds(tokens, index) {
				continue
			}
		}
		if first >= index {
			return nil
		}
		names = append(names, databaseSQLResultNameOf(query, tokens[first:index], parts[first:index]))
		if index == len(tokens) || tokens[index].Kind != databaseSQLTokenComma {
			break
		}
		first = index + 1
	}
	return names
}

func databaseSQLDepthChange(token databaseSQLToken) int {
	switch token.Kind {
	case databaseSQLTokenLeftParen:
		return 1
	case databaseSQLTokenRightParen:
		return -1
	default:
		return 0
	}
}

// databaseSQLResultListEnds says whether a word outside parentheses ends a
// SELECT's result columns: the next clause, or the next SELECT of a
// compound one; IS DISTINCT FROM's FROM is a comparison's.
func databaseSQLResultListEnds(tokens []databaseSQLToken, index int) bool {
	if tokens[index].Kind != databaseSQLTokenWord {
		return false
	}
	switch tokens[index].Text {
	case "FROM":
		return index < 2 || tokens[index-1].Text != "DISTINCT" ||
			tokens[index-2].Text != "IS" && tokens[index-2].Text != "NOT"
	case "WHERE", "GROUP", "HAVING", "WINDOW", "ORDER", "LIMIT", "UNION", "INTERSECT", "EXCEPT":
		return true
	default:
		return false
	}
}

// databaseSQLResultNameOf is one result column, by its tokens and their
// parts as they run: a ? runs as one.
func databaseSQLResultNameOf(query string, tokens []databaseSQLToken, parts []string) databaseSQLResultName {
	last := tokens[len(tokens)-1]
	if last.Kind == databaseSQLTokenOperator && last.Text == "*" &&
		(len(tokens) == 1 || len(tokens) == 3 && tokens[1].Kind == databaseSQLTokenDot) {
		return databaseSQLResultName{star: true}
	}
	depth := 0
	for _, token := range tokens {
		depth += databaseSQLDepthChange(token)
		if depth == 0 && token.Kind == databaseSQLTokenWord && token.Text == "AS" {
			return databaseSQLResultName{}
		}
	}
	start, end := tokens[0].Start, last.End
	if start < 0 || end <= start || end > len(query) {
		return databaseSQLResultName{}
	}
	ran := make([]string, len(parts))
	for index, part := range parts {
		if tokens[index].Kind == databaseSQLTokenParameter {
			part = "?"
		}
		ran[index] = part
	}
	return databaseSQLResultName{ran: strings.Join(ran, " "), written: query[start:end]}
}

// databaseSQLWrittenResultColumns are the result's column names with each
// of the first SELECT's columns named as written: those before a * from the
// first, those after the last * from the last.
func databaseSQLWrittenResultColumns(columns []string, names []databaseSQLResultName) []string {
	written := append([]string(nil), columns...)
	leading, trailing, starred := len(names), 0, false
	for index, name := range names {
		if name.star && !starred {
			leading, starred = index, true
		}
		if name.star {
			trailing = len(names) - index - 1
		}
	}
	if !starred && len(names) != len(columns) || len(columns) < leading+trailing {
		return written
	}
	for index := 0; index < leading; index++ {
		written[index] = names[index].named(columns[index])
	}
	for index := len(names) - trailing; index < len(names); index++ {
		column := len(columns) - (len(names) - index)
		written[column] = names[index].named(columns[column])
	}
	return written
}

// named is the name SQLite gave a column, as the column's text as written
// would give it.
func (name databaseSQLResultName) named(column string) string {
	if name.ran == "" || name.ran == name.written {
		return column
	}
	if column == name.ran {
		return name.written
	}
	if count, ok := strings.CutPrefix(column, name.ran+":"); ok && count != "" && strings.Trim(count, "0123456789") == "" {
		return name.written + ":" + count
	}
	return column
}
