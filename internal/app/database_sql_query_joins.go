package app

import (
	"fmt"
	"strings"
)

// A SELECT's FROM sources that an equality of their columns pairs — a
// column of one equal to a column of another (or to a value read from it),
// in ON, USING or WHERE — are joined by that value: SQLite finds each row's
// partners through an index it builds on the join's column, so the join
// takes about the rows of both, not every row with every row. Sources
// nothing pairs are joined each row with each, their rows multiplied. A side
// names its source by its alias, or by its name where it has none
// (a."No" = b."No"); a bare column could be either source's, so it pairs
// nothing. Measured 2026-11-26 with the server's SQLite on synthetic
// snapshots of 2,000 and 1,000 rows: the equality join and a three-way one
// took 9–11 ms written as databaseSQLSourceTable writes a joined query's
// sources, and 2.0 s as a single source's is written (json_extract in a
// nested loop).

// databaseSQLFrom is one SELECT's FROM: its sources in order, each with the
// names the query reads it by and the rows it gives, and the pairs of them
// an equality joins.
type databaseSQLFrom struct {
	items []databaseSQLFromItem
	pairs [][2]int
}

type databaseSQLFromItem struct {
	names []string
	rows  int
}

// add is the source FROM or JOIN read from start to at, and the alias after
// it if it has one.
func (from *databaseSQLFrom) add(tokens []databaseSQLToken, start, at, rows int) {
	item := databaseSQLFromItem{rows: max(1, rows)}
	end := at
	token := tokens[start]
	switch {
	case token.Kind == databaseSQLTokenWord && databaseSQLSourceWord(tokens, start):
		item.names = append(item.names, databaseSQLTableName(token))
		end = databaseSQLClosingParen(tokens, start+1)
	case token.Kind == databaseSQLTokenWord || token.Kind == databaseSQLTokenQuotedIdentifier:
		item.names = append(item.names, databaseSQLTableName(token))
	}
	if alias, ok := databaseSQLSourceAlias(tokens, end+1); ok {
		// An alias takes the place of its source's name, as SQLite reads it.
		item.names = []string{alias}
	}
	from.items = append(from.items, item)
}

// pairLast is USING: the source just read and the one before it.
func (from *databaseSQLFrom) pairLast() {
	if last := len(from.items) - 1; last > 0 {
		from.pairs = append(from.pairs, [2]int{last - 1, last})
	}
}

// pairCondition reads the condition of ON or WHERE from its first token for
// the equalities that pair this FROM's sources.
func (from *databaseSQLFrom) pairCondition(tokens []databaseSQLToken, start int) {
	from.pairConjuncts(tokens, start, databaseSQLConditionEnd(tokens, start))
}

func (from *databaseSQLFrom) pairConjuncts(tokens []databaseSQLToken, start, end int) {
	for _, conjunct := range databaseSQLConjuncts(tokens, start, end) {
		first, last := conjunct[0], conjunct[1]
		if last-first >= 2 && tokens[first].Kind == databaseSQLTokenLeftParen &&
			databaseSQLClosingParen(tokens, first) == last-1 {
			from.pairConjuncts(tokens, first+1, last-1)
			continue
		}
		equals, ok := databaseSQLEquality(tokens, first, last)
		if !ok {
			continue
		}
		left, right := from.side(tokens, first, equals), from.side(tokens, equals+1, last)
		for _, sides := range [][2]databaseSQLSide{{left, right}, {right, left}} {
			column, value := sides[0], sides[1]
			if column.column < 0 || value.reads[column.column] {
				continue
			}
			for other := range value.reads {
				from.pairs = append(from.pairs, [2]int{column.column, other})
			}
		}
	}
}

// databaseSQLSide is one side of an equality: the source whose column it is
// alone (-1 for none), and the sources it reads.
type databaseSQLSide struct {
	column int
	reads  map[int]bool
}

func (from *databaseSQLFrom) side(tokens []databaseSQLToken, start, end int) databaseSQLSide {
	side := databaseSQLSide{column: -1, reads: map[int]bool{}}
	for at := start; at+2 < end; at++ {
		if !databaseSQLNameToken(tokens[at]) || tokens[at+1].Kind != databaseSQLTokenDot ||
			!databaseSQLNameToken(tokens[at+2]) || at > start && tokens[at-1].Kind == databaseSQLTokenDot ||
			at+3 < end && tokens[at+3].Kind == databaseSQLTokenDot {
			continue
		}
		if item, ok := from.item(databaseSQLTableName(tokens[at])); ok {
			side.reads[item] = true
			if at == start && at+3 == end {
				side.column = item
			}
		}
	}
	return side
}

func (from *databaseSQLFrom) item(name string) (int, bool) {
	for index, item := range from.items {
		for _, each := range item.names {
			if each == name {
				return index, true
			}
		}
	}
	return 0, false
}

// rows is what the FROM gives at most by its estimate: the rows of sources
// an equality pairs added, those of sources nothing pairs multiplied.
func (from *databaseSQLFrom) rows() int {
	group := make([]int, len(from.items))
	for index := range group {
		group[index] = index
	}
	var root func(int) int
	root = func(index int) int {
		for group[index] != index {
			group[index] = group[group[index]]
			index = group[index]
		}
		return index
	}
	for _, pair := range from.pairs {
		group[root(pair[0])] = root(pair[1])
	}
	sums := map[int]int{}
	for index, item := range from.items {
		sums[root(index)] = databaseSQLWorkPlus(sums[root(index)], item.rows)
	}
	product := 1
	for _, sum := range sums {
		product = databaseSQLWorkTimes(product, sum)
	}
	return product
}

// databaseSQLSourceAlias is the alias a FROM source has at a token: AS and a
// name, or a name that isn't the next clause's word.
func databaseSQLSourceAlias(tokens []databaseSQLToken, at int) (string, bool) {
	if at >= len(tokens) {
		return "", false
	}
	if tokens[at].Kind == databaseSQLTokenWord && tokens[at].Text == "AS" {
		at++
		if at >= len(tokens) || !databaseSQLNameToken(tokens[at]) {
			return "", false
		}
		return databaseSQLTableName(tokens[at]), true
	}
	token := tokens[at]
	if token.Kind == databaseSQLTokenQuotedIdentifier ||
		token.Kind == databaseSQLTokenWord && !databaseSQLAfterSourceWords[token.Text] {
		return databaseSQLTableName(token), true
	}
	return "", false
}

// databaseSQLAfterSourceWords are the words that may follow a FROM source
// and are no alias of it.
var databaseSQLAfterSourceWords = map[string]bool{
	"ON": true, "USING": true, "JOIN": true, "LEFT": true, "RIGHT": true, "FULL": true,
	"INNER": true, "OUTER": true, "CROSS": true, "NATURAL": true, "WHERE": true, "GROUP": true,
	"HAVING": true, "ORDER": true, "LIMIT": true, "OFFSET": true, "UNION": true,
	"INTERSECT": true, "EXCEPT": true, "WINDOW": true, "INDEXED": true, "NOT": true,
}

// databaseSQLConditionEnd is where a condition from start ends: the next
// source, clause or the end of its SELECT.
func databaseSQLConditionEnd(tokens []databaseSQLToken, start int) int {
	depth := 0
	for at := start; at < len(tokens); at++ {
		switch token := tokens[at]; token.Kind {
		case databaseSQLTokenLeftParen:
			depth++
		case databaseSQLTokenRightParen:
			if depth == 0 {
				return at
			}
			depth--
		case databaseSQLTokenComma:
			if depth == 0 {
				return at
			}
		case databaseSQLTokenWord:
			if depth == 0 && (databaseSQLAfterSourceWords[token.Text] && token.Text != "NOT") {
				return at
			}
		}
	}
	return len(tokens)
}

// databaseSQLConjuncts are a condition's parts AND joins at its top, each
// from its first token to the one past its last; BETWEEN's AND and those
// inside CASE … END join none.
func databaseSQLConjuncts(tokens []databaseSQLToken, start, end int) [][2]int {
	parts := [][2]int{}
	depth, cases, between, first := 0, 0, false, start
	for at := start; at < end; at++ {
		token := tokens[at]
		switch token.Kind {
		case databaseSQLTokenLeftParen:
			depth++
		case databaseSQLTokenRightParen:
			depth--
		case databaseSQLTokenWord:
			if depth != 0 {
				continue
			}
			switch token.Text {
			case "CASE":
				cases++
			case "END":
				cases--
			case "BETWEEN":
				between = cases == 0
			case "AND":
				if cases != 0 {
					continue
				}
				if between {
					between = false
					continue
				}
				parts = append(parts, [2]int{first, at})
				first = at + 1
			}
		}
	}
	return append(parts, [2]int{first, end})
}

// databaseSQLEquality is where the one = of a part stands when the part is
// one side = the other, nothing else comparing at its top.
func databaseSQLEquality(tokens []databaseSQLToken, start, end int) (int, bool) {
	equals, depth, cases := -1, 0, 0
	for at := start; at < end; at++ {
		token := tokens[at]
		switch token.Kind {
		case databaseSQLTokenLeftParen:
			if at+1 < end && tokens[at+1].Kind == databaseSQLTokenWord &&
				(tokens[at+1].Text == "SELECT" || tokens[at+1].Text == "WITH") {
				return 0, false
			}
			depth++
		case databaseSQLTokenRightParen:
			depth--
		case databaseSQLTokenWord:
			if depth != 0 {
				continue
			}
			switch token.Text {
			case "CASE":
				cases++
			case "END":
				cases--
			case "OR", "NOT", "IS", "IN", "LIKE", "GLOB", "MATCH", "REGEXP", "BETWEEN",
				"ISNULL", "NOTNULL", "ESCAPE", "EXISTS", "AND":
				if cases == 0 {
					return 0, false
				}
			}
		case databaseSQLTokenOperator:
			if depth != 0 || cases != 0 {
				continue
			}
			switch token.Text {
			case "=", "==":
				if equals >= 0 {
					return 0, false
				}
				equals = at
			case "<", "<=", ">", ">=", "!=", "<>":
				return 0, false
			}
		}
	}
	return equals, equals > start && equals < end-1
}

func databaseSQLNameToken(token databaseSQLToken) bool {
	return token.Kind == databaseSQLTokenWord || token.Kind == databaseSQLTokenQuotedIdentifier
}

// databaseSQLClosingParen is the parenthesis that closes the one at open,
// or open itself when it isn't one or doesn't close.
func databaseSQLClosingParen(tokens []databaseSQLToken, open int) int {
	if open >= len(tokens) || tokens[open].Kind != databaseSQLTokenLeftParen {
		return open
	}
	depth := 0
	for at := open; at < len(tokens); at++ {
		switch tokens[at].Kind {
		case databaseSQLTokenLeftParen:
			depth++
		case databaseSQLTokenRightParen:
			depth--
			if depth == 0 {
				return at
			}
		}
	}
	return open
}

// databaseSQLQueryJoinsSources is whether a query pairs FROM sources by an
// equality anywhere in it.
func databaseSQLQueryJoinsSources(plan databaseSQLQueryPlan) bool {
	walk, _, err := walkDatabaseSQLQuery(plan.Tokens, databaseSQLDeclaredSources(plan), nil)
	return err == nil && walk.joins
}

// databaseSQLSourceTable is a data source's rows as the query reads them, a
// common table of its columns. A single source's is SQLite's to fold into
// the query, so it works out only the values a query reads. In a query that
// joins, each is made whole first, read by a range of the job's rows: SQLite
// takes such a table for a large one and builds the join's index on it,
// where one read by the job's key it takes for some ten rows and joins row
// by row. The data database keeps no planner statistics
// (sqlite_planner_statistics.go), so this shape decides the plan.
func databaseSQLSourceTable(name string, projections []string, ordinal int, joined bool) string {
	if !joined {
		return fmt.Sprintf(
			"%s AS (SELECT %s FROM {{%s}} WHERE [[job_id]] = {:sqlJobId} AND [[source_ordinal]] = %d)",
			name, strings.Join(projections, ", "), databaseSQLQueryRowsTable, ordinal,
		)
	}
	return fmt.Sprintf(
		"%s AS MATERIALIZED (SELECT %s FROM {{%s}} WHERE [[job_id]] >= {:sqlJobId} AND [[job_id]] <= {:sqlJobId} AND [[source_ordinal]] = %d)",
		name, strings.Join(projections, ", "), databaseSQLQueryRowsTable, ordinal,
	)
}

// databaseSQLResourceLimitRefusal is the answer to a query past what one
// query may take, and the ways to ask it within that.
func databaseSQLResourceLimitRefusal() notionMCPUnsupportedResult {
	return notionMCPUnsupportedResult{
		Code:    errorCodeDatabaseSQLQueryResourceLimit,
		Message: "The SQL query is more than one Leafen SQL query takes.",
		Reason: "One SQL query reads up to 16 MiB of the data sources' values, combines up to 1,000,000 rows by Leafen's estimate and runs for up to two seconds, and returns up to 256 columns and 16 MiB of results (read 100 at a time). " +
			"Data sources an equality pairs count their rows once each; data sources nothing pairs combine every row of one with every row of the other.",
		Alternative: "To join data sources, pair each with another by an equality of a column of each, both named by the source's alias: FROM \"collection://…\" AS a JOIN \"collection://…\" AS b ON a.\"No\" = b.\"No\" (or USING (\"No\"), or the same equality in WHERE). " +
			"Or ask in two steps: one query that aggregates a data source by its key (GROUP BY), then one that reads the other data source's rows for those keys (WHERE \"No\" IN (…)). " +
			"For other queries, narrow them, select fewer columns, aggregate before returning rows, or read the rows a page at a time in rows mode.",
		Retryable: false,
	}
}
