package app

import (
	"slices"
	"strings"
)

// Leafen's SQL reads the data sources it was given and nothing else of the
// server's. A query is read as SQLite reads it: a statement — WITH and its
// common tables, then SELECTs joined by UNION, INTERSECT or EXCEPT — whose
// FROM and JOIN each read a data source of data_source_urls, json_each or
// json_tree of a column, a common table in scope, or a SELECT in
// parentheses; a SELECT in parentheses anywhere else (a value, EXISTS, IN)
// is a statement of its own, read the same way. Notion's SQL takes these
// (seen 2026-11-16: WITH "ids"("x") AS (SELECT 'a' UNION ALL SELECT 'c')
// read by IN, EXISTS and a value SELECT, a SELECT in FROM, WITH inside a
// value SELECT), and its tool text teaches the first for inline rows; it
// refuses WITH RECURSIVE and VALUES.
//
// A common table is in scope only where SQLite reads it as one: in its
// statement and the statements inside it, and in the bodies of the common
// tables after it in the same WITH, never in its own (SQLite reads a name of
// a table out of its scope as the server's table by that name — 2026-11-16:
// a second value SELECT beside a WITH's read the server's own table). Its
// name never takes another table's in scope or a data source's, and nothing
// follows it in FROM that would make it a schema's (main.…) or a function's.

// databaseSQLScope is the common tables a statement reads, by folded name,
// each with the rows it gives at most.
type databaseSQLScope struct {
	parent *databaseSQLScope
	tables map[string]int
}

func (scope *databaseSQLScope) table(name string) (int, bool) {
	for at := scope; at != nil; at = at.parent {
		if rows, ok := at.tables[name]; ok {
			return rows, true
		}
	}
	return 0, false
}

// databaseSQLCount is what a statement gives and takes: the rows it gives
// at most by its estimate (its FROM sources' rows, added where an equality
// pairs them and multiplied where nothing does — database_sql_query_joins.go
// — and its SELECTs' added) and its work, those rows with the work of the
// SELECTs it reads.
type databaseSQLCount struct {
	rows, work int
}

// databaseSQLWalk reads a query's tokens once. rowsOf gives a data source's
// rows for the work a query may take (nil: each counts one).
type databaseSQLWalk struct {
	tokens       []databaseSQLToken
	at           int
	declared     map[string]struct{}
	rowsOf       func(source string) (int, bool)
	referenced   []string
	placeholders int
	// work is what the common tables' bodies take, beside the statement's.
	work int
	// joins is whether a FROM anywhere in the query pairs its sources.
	joins bool
}

// walkDatabaseSQLQuery checks what a query reads and counts the work it may
// take. A value SELECT (in the result, WHERE, EXISTS or IN) adds its work
// rather than multiplying it: SQLite runs one that reads no column of the
// row once, and one that does stays within the execution's time.
func walkDatabaseSQLQuery(
	tokens []databaseSQLToken,
	declared map[string]struct{},
	rowsOf func(source string) (int, bool),
) (*databaseSQLWalk, int, error) {
	walk := &databaseSQLWalk{tokens: tokens, declared: declared, rowsOf: rowsOf, referenced: []string{}}
	count, err := walk.statement(nil)
	if err != nil {
		return nil, 0, err
	}
	if walk.at != len(tokens) {
		return nil, 0, databaseSQLQueryInvalid("a parenthesis closes that didn't open")
	}
	return walk, databaseSQLWorkPlus(walk.work, count.work), nil
}

func (walk *databaseSQLWalk) word(text string) bool {
	return walk.at < len(walk.tokens) && walk.tokens[walk.at].Kind == databaseSQLTokenWord && walk.tokens[walk.at].Text == text
}

func (walk *databaseSQLWalk) kind(kind databaseSQLTokenKind) bool {
	return walk.at < len(walk.tokens) && walk.tokens[walk.at].Kind == kind
}

// opensStatement is whether the walk's token opens a SELECT in parentheses.
func (walk *databaseSQLWalk) opensStatement() bool {
	return walk.at+1 < len(walk.tokens) && walk.tokens[walk.at].Kind == databaseSQLTokenLeftParen &&
		walk.tokens[walk.at+1].Kind == databaseSQLTokenWord &&
		(walk.tokens[walk.at+1].Text == "SELECT" || walk.tokens[walk.at+1].Text == "WITH")
}

// statement reads a statement from its first word to its end: the query's
// end, or the parenthesis that closes it, which it leaves to its caller.
func (walk *databaseSQLWalk) statement(outer *databaseSQLScope) (databaseSQLCount, error) {
	scope := &databaseSQLScope{parent: outer, tables: map[string]int{}}
	if walk.word("WITH") {
		walk.at++
		if err := walk.commonTables(scope); err != nil {
			return databaseSQLCount{}, err
		}
	}
	total := databaseSQLCount{}
	for {
		if !walk.word("SELECT") {
			if walk.word("VALUES") {
				return databaseSQLCount{}, databaseSQLValuesRefused()
			}
			return databaseSQLCount{}, databaseSQLQueryInvalid("the query is SELECTs, joined by UNION, INTERSECT or EXCEPT, after WITH and its tables if it has them")
		}
		count, end, err := walk.selectCore(scope)
		if err != nil {
			return databaseSQLCount{}, err
		}
		total.rows = databaseSQLWorkPlus(total.rows, count.rows)
		total.work = databaseSQLWorkPlus(total.work, count.work)
		if end {
			return total, nil
		}
	}
}

// statementInParentheses reads a SELECT in parentheses from its opening
// one, leaving the walk on its closing one.
func (walk *databaseSQLWalk) statementInParentheses(scope *databaseSQLScope) (databaseSQLCount, error) {
	walk.at++
	count, err := walk.statement(scope)
	if err != nil {
		return databaseSQLCount{}, err
	}
	if !walk.kind(databaseSQLTokenRightParen) {
		return databaseSQLCount{}, databaseSQLQueryInvalid("a parenthesis doesn't close")
	}
	return count, nil
}

func databaseSQLValuesRefused() error {
	return databaseSQLQueryInvalid("VALUES isn't taken, as in Notion's SQL: inline rows as WITH \"ids\"(\"x\") AS (SELECT 'a' UNION ALL SELECT 'b')")
}

// commonTables reads WITH's tables, each a name, its columns if it lists
// them, AS and a SELECT in parentheses, into scope in turn.
func (walk *databaseSQLWalk) commonTables(scope *databaseSQLScope) error {
	if walk.word("RECURSIVE") {
		return databaseSQLQueryInvalid("WITH RECURSIVE isn't taken, as in Notion's SQL: inline rows as WITH \"ids\"(\"x\") AS (SELECT 'a' UNION ALL SELECT 'b')")
	}
	shape := func() error {
		return databaseSQLQueryInvalid("a WITH table is its name, its columns in parentheses if it names them, AS and a SELECT in parentheses: WITH \"ids\"(\"x\") AS (SELECT 'a' UNION ALL SELECT 'b')")
	}
	for {
		if !walk.kind(databaseSQLTokenWord) && !walk.kind(databaseSQLTokenQuotedIdentifier) {
			return shape()
		}
		token := walk.tokens[walk.at]
		if token.Kind == databaseSQLTokenWord && databaseSQLWordIsSyntax(token.Text) {
			return shape()
		}
		name := databaseSQLTableName(token)
		if _, taken := scope.table(name); taken || walk.namesSource(name) {
			return databaseSQLQueryInvalid("WITH names each table once, by a name no data source or table before it has; %s has one", databaseSQLTokenWritten(token))
		}
		walk.at++
		if walk.kind(databaseSQLTokenLeftParen) {
			for walk.at++; ; walk.at++ {
				if !walk.kind(databaseSQLTokenWord) && !walk.kind(databaseSQLTokenQuotedIdentifier) {
					return shape()
				}
				walk.at++
				if walk.kind(databaseSQLTokenRightParen) {
					walk.at++
					break
				}
				if !walk.kind(databaseSQLTokenComma) {
					return shape()
				}
			}
		}
		if !walk.word("AS") {
			return shape()
		}
		walk.at++
		if walk.word("NOT") {
			walk.at++
			if !walk.word("MATERIALIZED") {
				return shape()
			}
		}
		if walk.word("MATERIALIZED") {
			walk.at++
		}
		if !walk.opensStatement() {
			return shape()
		}
		// Its body reads the tables before it, never itself.
		count, err := walk.statementInParentheses(scope)
		if err != nil {
			return err
		}
		walk.at++
		scope.tables[name] = max(1, count.rows)
		walk.work = databaseSQLWorkPlus(walk.work, count.work)
		if !walk.kind(databaseSQLTokenComma) {
			return nil
		}
		walk.at++
	}
}

// selectCore reads one SELECT from its first word to UNION, INTERSECT or
// EXCEPT (past which it stops, end false) or its statement's end (end true).
func (walk *databaseSQLWalk) selectCore(scope *databaseSQLScope) (count databaseSQLCount, end bool, err error) {
	from, inner, depth := &databaseSQLFrom{}, 0, 0
	expectingSource, sourceList := false, false
	finish := func() databaseSQLCount {
		rows := from.rows()
		walk.joins = walk.joins || len(from.pairs) > 0
		return databaseSQLCount{rows: rows, work: databaseSQLWorkPlus(rows, inner)}
	}
	for walk.at++; walk.at < len(walk.tokens); walk.at++ {
		token := walk.tokens[walk.at]
		if expectingSource {
			start := walk.at
			source, err := walk.source(scope)
			if err != nil {
				return databaseSQLCount{}, false, err
			}
			from.add(walk.tokens, start, walk.at, source.rows)
			inner = databaseSQLWorkPlus(inner, source.work)
			expectingSource = false
			continue
		}
		switch token.Kind {
		case databaseSQLTokenLeftParen:
			if walk.opensStatement() {
				value, err := walk.statementInParentheses(scope)
				if err != nil {
					return databaseSQLCount{}, false, err
				}
				inner = databaseSQLWorkPlus(inner, value.work)
				continue
			}
			depth++
		case databaseSQLTokenRightParen:
			if depth == 0 {
				return finish(), true, nil
			}
			depth--
		case databaseSQLTokenParameter:
			if previous := walk.at - 1; previous >= 0 && walk.tokens[previous].Kind == databaseSQLTokenWord &&
				(walk.tokens[previous].Text == "LIMIT" || walk.tokens[previous].Text == "OFFSET") {
				return databaseSQLCount{}, false, databaseSQLQueryInvalid("LIMIT and OFFSET take numbers, not ?")
			}
			walk.placeholders++
		case databaseSQLTokenWord:
			if err := walk.checkWord(); err != nil {
				return databaseSQLCount{}, false, err
			}
			switch {
			case token.Text == "WITH":
				return databaseSQLCount{}, false, databaseSQLQueryInvalid("WITH goes first: at the start of the query, or of a SELECT in parentheses")
			case token.Text == "FROM" || token.Text == "JOIN":
				expectingSource, sourceList = true, true
			case depth == 0 && sourceList && token.Text == "ON":
				from.pairCondition(walk.tokens, walk.at+1)
			case depth == 0 && sourceList && token.Text == "USING":
				from.pairLast()
			case depth == 0 && token.Text == "WHERE":
				sourceList = false
				from.pairCondition(walk.tokens, walk.at+1)
			case depth == 0 && (token.Text == "UNION" || token.Text == "INTERSECT" || token.Text == "EXCEPT"):
				walk.at++
				if walk.word("ALL") {
					walk.at++
				}
				return finish(), false, nil
			default:
				if _, terminator := databaseSQLSourceClauseTerminators[token.Text]; terminator && depth == 0 {
					sourceList = false
				}
			}
		case databaseSQLTokenComma:
			if sourceList && depth == 0 {
				expectingSource = true
			}
		}
	}
	if expectingSource {
		return databaseSQLCount{}, false, databaseSQLQueryInvalid("FROM names no data source: name each as \"collection://…\" in double quotes, one of data_source_urls")
	}
	return finish(), true, nil
}

// source reads what FROM or JOIN reads, at the walk's token, leaving the
// walk on its last token: its rows, and the work of a SELECT it is.
func (walk *databaseSQLWalk) source(scope *databaseSQLScope) (databaseSQLCount, error) {
	token := walk.tokens[walk.at]
	rows := 1
	switch {
	case walk.opensStatement():
		count, err := walk.statementInParentheses(scope)
		return count, err
	case token.Kind == databaseSQLTokenLeftParen:
		// Sources in parentheses would hide a list from this check.
		return databaseSQLCount{}, databaseSQLQueryInvalid("FROM and JOIN name their data sources without parentheses, or read a SELECT in them")
	case token.Kind == databaseSQLTokenWord && databaseSQLSourceWord(walk.tokens, walk.at):
		// json_each or json_tree of a column reads that row's values, within
		// the execution's time; what is in its parentheses is read as any
		// value is.
		return databaseSQLCount{rows: 1}, nil
	case token.Kind == databaseSQLTokenQuotedIdentifier:
		if _, declared := walk.declared[token.Text]; declared {
			if !slices.Contains(walk.referenced, token.Text) {
				walk.referenced = append(walk.referenced, token.Text)
			}
			if walk.rowsOf != nil {
				captured, ok := walk.rowsOf(token.Text)
				if !ok {
					return databaseSQLCount{}, databaseSQLQueryInvalid()
				}
				rows = captured
			}
			break
		}
		fallthrough
	case token.Kind == databaseSQLTokenWord:
		common, ok := scope.table(databaseSQLTableName(token))
		if !ok {
			return databaseSQLCount{}, databaseSQLQueryInvalid("FROM and JOIN read only data_source_urls, as \"collection://…\" in double quotes, a WITH table, a SELECT in parentheses, or json_each(…) or json_tree(…) of a column; %s isn't one", databaseSQLTokenWritten(token))
		}
		rows = common
	default:
		return databaseSQLCount{}, databaseSQLQueryInvalid("FROM and JOIN name a data source as \"collection://…\" in double quotes, one of data_source_urls")
	}
	// A schema's table (main.…) or a function by a table's name reads past it.
	if next := walk.at + 1; next < len(walk.tokens) &&
		(walk.tokens[next].Kind == databaseSQLTokenDot || walk.tokens[next].Kind == databaseSQLTokenLeftParen) {
		return databaseSQLCount{}, databaseSQLQueryInvalid("FROM and JOIN name a data source or a WITH table alone, without a dot or parentheses after it")
	}
	return databaseSQLCount{rows: rows}, nil
}

// checkWord refuses a word Leafen's SQL doesn't take where it stands.
func (walk *databaseSQLWalk) checkWord() error {
	token := walk.tokens[walk.at]
	next := walk.at + 1
	if token.Text == "NATURAL" || token.Text == "OVER" {
		return databaseSQLQueryInvalid("%s isn't taken", token.Text)
	}
	// SQLite reads `x IN name` as the rows of a table by that name.
	if token.Text == "IN" && next < len(walk.tokens) && walk.tokens[next].Kind != databaseSQLTokenLeftParen {
		return databaseSQLQueryInvalid("IN takes its values in parentheses")
	}
	if token.Text == "VALUES" {
		return databaseSQLValuesRefused()
	}
	callsFunction := next < len(walk.tokens) && walk.tokens[next].Kind == databaseSQLTokenLeftParen
	_, allowedFunction := databaseSQLAllowedFunctions[token.Text]
	if _, forbidden := databaseSQLForbiddenWords[token.Text]; forbidden && (!callsFunction || !allowedFunction) {
		return databaseSQLQueryInvalid("%s isn't taken here", token.Text)
	}
	if callsFunction && !allowedFunction && !databaseSQLWordIsSyntax(token.Text) {
		return databaseSQLQueryInvalid("%s() isn't a function Leafen's SQL takes: it takes %s, and json_each and json_tree in FROM", token.Text, databaseSQLFunctionList())
	}
	return nil
}

// namesSource is whether a WITH table's name would take a data source's.
func (walk *databaseSQLWalk) namesSource(name string) bool {
	if strings.HasPrefix(name, "collection:") {
		return true
	}
	for source := range walk.declared {
		if databaseSQLFoldName(source) == name {
			return true
		}
	}
	return false
}

// databaseSQLTableName is a table's name as SQLite matches it: a word as
// written or a quoted name, its ASCII letters in either case alike.
func databaseSQLTableName(token databaseSQLToken) string {
	if token.Kind == databaseSQLTokenWord && token.Raw != "" {
		return databaseSQLFoldName(token.Raw)
	}
	return databaseSQLFoldName(token.Text)
}

// databaseSQLFoldName folds ASCII letters only, as SQLite does for names: a
// name folded further would match one SQLite reads as another table.
func databaseSQLFoldName(name string) string {
	return strings.Map(func(letter rune) rune {
		if letter >= 'A' && letter <= 'Z' {
			return letter + ('a' - 'A')
		}
		return letter
	}, name)
}

// databaseSQLTokenWritten is a name as a refusal quotes it.
func databaseSQLTokenWritten(token databaseSQLToken) string {
	if token.Kind == databaseSQLTokenWord && token.Raw != "" {
		return token.Raw
	}
	return `"` + token.Text + `"`
}

// databaseSQLWorkPlus and databaseSQLWorkTimes count work up to just past
// the most a query may take.
func databaseSQLWorkPlus(a, b int) int {
	if a > maxDatabaseSQLPlanWork-b {
		return maxDatabaseSQLPlanWork + 1
	}
	return a + b
}

func databaseSQLWorkTimes(a, b int) int {
	if b > 0 && a > maxDatabaseSQLPlanWork/b {
		return maxDatabaseSQLPlanWork + 1
	}
	return a * b
}
