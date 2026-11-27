package app

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	maxDatabaseSQLQueryBytes      = 32 * 1024
	maxDatabaseSQLQueryTokens     = 512
	maxDatabaseSQLQueryDepth      = 16
	maxDatabaseSQLQueryParameters = 256
	maxDatabaseSQLParameterBytes  = 256 * 1024
	maxDatabaseSQLQuerySources    = 32

	errorCodeDatabaseSQLQueryInvalid       = "LEAFEN_DATABASE_SQL_QUERY_INVALID"
	errorCodeDatabaseSQLQueryResourceLimit = "LEAFEN_DATABASE_SQL_QUERY_RESOURCE_LIMIT"
)

type databaseSQLTokenKind uint8

const (
	databaseSQLTokenWord databaseSQLTokenKind = iota + 1
	databaseSQLTokenQuotedIdentifier
	databaseSQLTokenString
	databaseSQLTokenNumber
	databaseSQLTokenParameter
	databaseSQLTokenOperator
	databaseSQLTokenLeftParen
	databaseSQLTokenRightParen
	databaseSQLTokenComma
	databaseSQLTokenDot
)

type databaseSQLToken struct {
	Kind databaseSQLTokenKind
	// Text is a word in capitals, to read it by.
	Text string
	// Raw is a word as the query writes it, as it runs: a column a bare
	// word names comes out under that spelling, as Notion's (seen
	// 2026-11-11: SELECT url, id gives "url" and "id").
	Raw string `json:",omitempty"`
	// Start and End are where it is in the query as written, to name a
	// result column by its text (database_sql_query_result_names.go).
	Start, End int `json:"-"`
}

var errDatabaseSQLQueryInvalid = errors.New(errorCodeDatabaseSQLQueryInvalid)

func lexDatabaseSQLQuery(query string) ([]databaseSQLToken, error) {
	// A closing semicolon, as SQL is often written, ends its one statement.
	query = strings.TrimRight(strings.TrimSuffix(strings.TrimRight(query, " \t\r\n"), ";"), " \t\r\n")
	if query == "" || len(query) > maxDatabaseSQLQueryBytes || !utf8.ValidString(query) {
		return nil, databaseSQLQueryInvalid("the query is empty or longer than %d bytes", maxDatabaseSQLQueryBytes)
	}
	tokens := make([]databaseSQLToken, 0, min(len(query)/2, maxDatabaseSQLQueryTokens))
	depth, parameters, start := 0, 0, 0
	appendToken := func(kind databaseSQLTokenKind, text string) error {
		if len(tokens) >= maxDatabaseSQLQueryTokens {
			return databaseSQLQueryInvalid("the query is longer than %d words and signs", maxDatabaseSQLQueryTokens)
		}
		tokens = append(tokens, databaseSQLToken{Kind: kind, Text: text, Start: start})
		return nil
	}
	// A token ends where the next turn starts: each turn reads one.
	ended := func(offset int) {
		if last := len(tokens) - 1; last >= 0 && tokens[last].End == 0 {
			tokens[last].End = offset
		}
	}
	for offset := 0; offset < len(query); {
		ended(offset)
		start = offset
		value := query[offset]
		if isDatabaseSQLSpace(value) {
			offset++
			continue
		}
		// A name in brackets, as Notion's examples write a data source's.
		if value == '[' {
			end := strings.IndexByte(query[offset:], ']')
			if end < 2 || appendToken(databaseSQLTokenQuotedIdentifier, query[offset+1:offset+end]) != nil {
				return nil, databaseSQLQueryInvalid("a name in brackets doesn't close")
			}
			offset += end + 1
			continue
		}
		if value == ';' || value == '`' || value == ']' || value == 0 ||
			value == ':' || value == '$' || value == '@' {
			return nil, databaseSQLQueryInvalid("the query has %q, which Leafen's SQL doesn't take: one statement, names in double quotes, values as ? with params", string(value))
		}
		if value == '-' && offset+1 < len(query) && query[offset+1] == '-' ||
			value == '/' && offset+1 < len(query) && query[offset+1] == '*' {
			return nil, databaseSQLQueryInvalid("the query has a comment: leave comments out")
		}
		switch value {
		case '(':
			depth++
			if depth > maxDatabaseSQLQueryDepth || appendToken(databaseSQLTokenLeftParen, "(") != nil {
				return nil, databaseSQLQueryInvalid("parentheses go at most %d deep", maxDatabaseSQLQueryDepth)
			}
			offset++
			continue
		case ')':
			depth--
			if depth < 0 || appendToken(databaseSQLTokenRightParen, ")") != nil {
				return nil, databaseSQLQueryInvalid("a parenthesis closes that didn't open")
			}
			offset++
			continue
		case ',':
			if appendToken(databaseSQLTokenComma, ",") != nil {
				return nil, databaseSQLQueryInvalid("the query is too long")
			}
			offset++
			continue
		case '.':
			if offset+1 < len(query) && isDatabaseSQLDigit(query[offset+1]) {
				end := scanDatabaseSQLNumber(query, offset)
				if _, err := strconv.ParseFloat(query[offset:end], 64); err != nil ||
					appendToken(databaseSQLTokenNumber, query[offset:end]) != nil {
					return nil, databaseSQLQueryInvalid("a number didn't read")
				}
				offset = end
				continue
			}
			if appendToken(databaseSQLTokenDot, ".") != nil {
				return nil, databaseSQLQueryInvalid("the query is too long")
			}
			offset++
			continue
		case '?':
			if offset+1 < len(query) && isDatabaseSQLDigit(query[offset+1]) {
				return nil, databaseSQLQueryInvalid("parameters are a plain ?, given in order in params, not ?1")
			}
			parameters++
			if parameters > maxDatabaseSQLQueryParameters || appendToken(databaseSQLTokenParameter, "?") != nil {
				return nil, databaseSQLQueryInvalid("the query has more than %d parameters", maxDatabaseSQLQueryParameters)
			}
			offset++
			continue
		case '\'':
			text, end, ok := scanDatabaseSQLDelimited(query, offset, '\'')
			if !ok || appendToken(databaseSQLTokenString, text) != nil {
				return nil, databaseSQLQueryInvalid("a text in single quotes doesn't close")
			}
			offset = end
			continue
		case '"':
			text, end, ok := scanDatabaseSQLDelimited(query, offset, '"')
			if !ok || text == "" || appendToken(databaseSQLTokenQuotedIdentifier, text) != nil {
				return nil, databaseSQLQueryInvalid("a name in double quotes doesn't close, or is empty")
			}
			offset = end
			continue
		}
		if isDatabaseSQLDigit(value) {
			end := scanDatabaseSQLNumber(query, offset)
			if _, err := strconv.ParseFloat(query[offset:end], 64); err != nil ||
				appendToken(databaseSQLTokenNumber, query[offset:end]) != nil {
				return nil, databaseSQLQueryInvalid("a number didn't read")
			}
			offset = end
			continue
		}
		if isDatabaseSQLWordStart(value) {
			end := offset + 1
			for end < len(query) && isDatabaseSQLWordPart(query[end]) {
				end++
			}
			if appendToken(databaseSQLTokenWord, strings.ToUpper(query[offset:end])) != nil {
				return nil, databaseSQLQueryInvalid("the query is too long")
			}
			tokens[len(tokens)-1].Raw = query[offset:end]
			offset = end
			continue
		}
		if strings.ContainsRune("+-*/%=<>!|&~", rune(value)) {
			end := offset + 1
			if end < len(query) && strings.ContainsRune("=<>|", rune(query[end])) {
				end++
			}
			operator := query[offset:end]
			if !databaseSQLAllowedOperator(operator) || appendToken(databaseSQLTokenOperator, operator) != nil {
				return nil, databaseSQLQueryInvalid("%q isn't an operator Leafen's SQL takes", operator)
			}
			offset = end
			continue
		}
		return nil, databaseSQLQueryInvalid("the query has %q, which Leafen's SQL doesn't read", string(value))
	}
	if depth != 0 || len(tokens) == 0 {
		return nil, databaseSQLQueryInvalid("the query is empty, or a parenthesis doesn't close")
	}
	ended(len(query))
	return tokens, nil
}

func scanDatabaseSQLDelimited(query string, offset int, delimiter byte) (string, int, bool) {
	var value strings.Builder
	for index := offset + 1; index < len(query); index++ {
		if query[index] != delimiter {
			value.WriteByte(query[index])
			continue
		}
		if index+1 < len(query) && query[index+1] == delimiter {
			value.WriteByte(delimiter)
			index++
			continue
		}
		return value.String(), index + 1, true
	}
	return "", 0, false
}

func scanDatabaseSQLNumber(query string, offset int) int {
	index, seenDot, seenExponent := offset, false, false
	for index < len(query) {
		switch value := query[index]; {
		case isDatabaseSQLDigit(value):
			index++
		case value == '.' && !seenDot && !seenExponent:
			seenDot = true
			index++
		case (value == 'e' || value == 'E') && !seenExponent:
			seenExponent = true
			index++
			if index < len(query) && (query[index] == '+' || query[index] == '-') {
				index++
			}
		default:
			return index
		}
	}
	return index
}

func databaseSQLAllowedOperator(value string) bool {
	switch value {
	case "+", "-", "*", "/", "%", "=", "==", "!=", "<>", "<", "<=", ">", ">=", "||", "&", "|", "~":
		return true
	default:
		return false
	}
}

func isDatabaseSQLSpace(value byte) bool {
	return value == ' ' || value == '\n' || value == '\r' || value == '\t' || value == '\f'
}

func isDatabaseSQLDigit(value byte) bool {
	return value >= '0' && value <= '9'
}

func isDatabaseSQLWordStart(value byte) bool {
	// Past ASCII is a name's letter: Notion's SQL takes a bare 이름.
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value == '_' || value >= utf8.RuneSelf
}

func isDatabaseSQLWordPart(value byte) bool {
	return isDatabaseSQLWordStart(value) || isDatabaseSQLDigit(value)
}

// databaseSQLQueryInvalid is a query Leafen can't run, saying why where it
// knows (mcp_tool_errors.go).
func databaseSQLQueryInvalid(reason ...any) error {
	if len(reason) == 0 {
		return errDatabaseSQLQueryInvalid
	}
	format, _ := reason[0].(string)
	return fmt.Errorf("%w: "+format, append([]any{errDatabaseSQLQueryInvalid}, reason[1:]...)...)
}

// databaseSQLQueryReason is the reason a refused query was given, "" for none.
func databaseSQLQueryReason(err error) string {
	if !errors.Is(err, errDatabaseSQLQueryInvalid) {
		return ""
	}
	return strings.TrimPrefix(strings.TrimPrefix(err.Error(), errorCodeDatabaseSQLQueryInvalid), ": ")
}
