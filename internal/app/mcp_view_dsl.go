package app

import (
	"strings"
	"unicode"
)

// The view language of notion-create-view and notion-update-view, as Notion's
// MCP takes it (notion://docs/view-dsl-spec, read 2026-11-04): directives
// separated by semicolons or new lines, property names in double quotes,
// keywords in any case. This file reads the words; mcp_view_configure.go does
// what they say.

const (
	mcpViewTokenWord   = 'w'
	mcpViewTokenString = 's'
	mcpViewTokenMark   = 'm'
)

type mcpViewToken struct {
	kind byte
	text string
}

// mcpViewDirectives splits a configure string into its directives' words.
func mcpViewDirectives(raw string) ([][]mcpViewToken, error) {
	directives := [][]mcpViewToken{}
	current := []mcpViewToken{}
	depth := 0
	finish := func() {
		if len(current) > 0 {
			directives = append(directives, current)
			current = []mcpViewToken{}
		}
	}
	runes := []rune(raw)
	for at := 0; at < len(runes); {
		character := runes[at]
		switch {
		case character == ';' || character == '\n':
			if depth == 0 {
				finish()
			}
			at++
		case unicode.IsSpace(character):
			at++
		case character == '"' || character == '\'':
			text, next, ok := mcpViewQuoted(runes, at)
			if !ok {
				return nil, mcpInvalid("configure: a quote has no end")
			}
			current = append(current, mcpViewToken{kind: mcpViewTokenString, text: text})
			at = next
		case strings.ContainsRune("(),", character):
			if character == '(' {
				depth++
			} else if character == ')' {
				depth--
			}
			current = append(current, mcpViewToken{kind: mcpViewTokenMark, text: string(character)})
			at++
		case strings.ContainsRune("=!<>", character):
			mark := string(character)
			if at+1 < len(runes) && runes[at+1] == '=' {
				mark += "="
			}
			if mark == "!" {
				return nil, mcpInvalid("configure: ! stands only in !=")
			}
			current = append(current, mcpViewToken{kind: mcpViewTokenMark, text: mark})
			at += len(mark)
		default:
			start := at
			for at < len(runes) && !unicode.IsSpace(runes[at]) && !strings.ContainsRune(`;(),=!<>"'`, runes[at]) {
				at++
			}
			current = append(current, mcpViewToken{kind: mcpViewTokenWord, text: string(runes[start:at])})
		}
	}
	finish()
	return directives, nil
}

// mcpViewQuoted reads a quoted word; a quote is doubled or escaped inside it.
func mcpViewQuoted(runes []rune, at int) (string, int, bool) {
	quote := runes[at]
	var text strings.Builder
	for at++; at < len(runes); at++ {
		switch {
		case runes[at] == '\\' && at+1 < len(runes) && runes[at+1] == quote:
			at++
			text.WriteRune(quote)
		case runes[at] == quote && at+1 < len(runes) && runes[at+1] == quote:
			at++
			text.WriteRune(quote)
		case runes[at] == quote:
			return text.String(), at + 1, true
		default:
			text.WriteRune(runes[at])
		}
	}
	return "", at, false
}

// mcpViewWords walks a directive's words.
type mcpViewWords struct {
	tokens []mcpViewToken
	at     int
}

func (words *mcpViewWords) done() bool { return words.at >= len(words.tokens) }

// keyword takes the given unquoted words when they come next.
func (words *mcpViewWords) keyword(names ...string) bool {
	if words.at+len(names) > len(words.tokens) {
		return false
	}
	for index, name := range names {
		token := words.tokens[words.at+index]
		if token.kind != mcpViewTokenWord || !strings.EqualFold(token.text, name) {
			return false
		}
	}
	words.at += len(names)
	return true
}

func (words *mcpViewWords) mark(mark string) bool {
	if words.done() || words.tokens[words.at].kind != mcpViewTokenMark || words.tokens[words.at].text != mark {
		return false
	}
	words.at++
	return true
}

// quoted takes a word in quotes.
func (words *mcpViewWords) quoted() (string, bool) {
	if words.done() || words.tokens[words.at].kind != mcpViewTokenString {
		return "", false
	}
	words.at++
	return words.tokens[words.at-1].text, true
}

// value takes a word in quotes or not.
func (words *mcpViewWords) value() (string, bool) {
	if words.done() || words.tokens[words.at].kind == mcpViewTokenMark {
		return "", false
	}
	words.at++
	return words.tokens[words.at-1].text, true
}

// oneOf takes a word that is one of the choices, in any case, and gives it as
// the choice writes it.
func (words *mcpViewWords) oneOf(choices ...string) (string, bool) {
	if words.done() || words.tokens[words.at].kind == mcpViewTokenMark {
		return "", false
	}
	for _, choice := range choices {
		if strings.EqualFold(words.tokens[words.at].text, choice) {
			words.at++
			return choice, true
		}
	}
	return "", false
}

// names takes a list of quoted names separated by commas.
func (words *mcpViewWords) names() ([]string, bool) {
	names := []string{}
	for {
		name, ok := words.quoted()
		if !ok {
			return nil, false
		}
		names = append(names, name)
		if !words.mark(",") {
			return names, true
		}
	}
}

// mcpViewExpression is a FILTER's condition, or its AND and OR of others.
type mcpViewExpression struct {
	conjunction string
	children    []mcpViewExpression
	property    string
	operator    string
	values      []string
}

// mcpViewFilterExpression reads conditions joined by AND and OR, AND first.
func mcpViewFilterExpression(words *mcpViewWords, depth int) (mcpViewExpression, bool) {
	if depth > maxDatabaseQueryFilterDepth {
		return mcpViewExpression{}, false
	}
	first, ok := mcpViewFilterAll(words, depth)
	if !ok {
		return first, false
	}
	any := []mcpViewExpression{first}
	for words.keyword("OR") {
		next, ok := mcpViewFilterAll(words, depth)
		if !ok {
			return next, false
		}
		any = append(any, next)
	}
	if len(any) == 1 {
		return first, true
	}
	return mcpViewExpression{conjunction: databaseQueryConjunctionOr, children: any}, true
}

func mcpViewFilterAll(words *mcpViewWords, depth int) (mcpViewExpression, bool) {
	first, ok := mcpViewFilterTerm(words, depth)
	if !ok {
		return first, false
	}
	all := []mcpViewExpression{first}
	for words.keyword("AND") {
		next, ok := mcpViewFilterTerm(words, depth)
		if !ok {
			return next, false
		}
		all = append(all, next)
	}
	if len(all) == 1 {
		return first, true
	}
	return mcpViewExpression{conjunction: databaseQueryConjunctionAnd, children: all}, true
}

func mcpViewFilterTerm(words *mcpViewWords, depth int) (mcpViewExpression, bool) {
	if words.mark("(") {
		inner, ok := mcpViewFilterExpression(words, depth+1)
		return inner, ok && words.mark(")")
	}
	return mcpViewCondition(words)
}

// mcpViewCondition reads "Property" operator value. Beside Notion's operators
// Leafen's filters read DOES NOT CONTAIN and BETWEEN, which its screen offers.
func mcpViewCondition(words *mcpViewWords) (mcpViewExpression, bool) {
	property, ok := words.quoted()
	if !ok {
		return mcpViewExpression{}, false
	}
	condition := mcpViewExpression{property: property}
	switch {
	case words.keyword("IS", "NOT", "EMPTY"):
		condition.operator = "is_not_empty"
		return condition, true
	case words.keyword("IS", "EMPTY"):
		condition.operator = "is_empty"
		return condition, true
	case words.keyword("IN"):
		condition.operator = "in"
		if !words.mark("(") {
			return condition, false
		}
		for {
			value, ok := words.value()
			if !ok {
				return condition, false
			}
			condition.values = append(condition.values, value)
			if !words.mark(",") {
				break
			}
		}
		return condition, words.mark(")")
	case words.keyword("IS", "BETWEEN"), words.keyword("BETWEEN"):
		condition.operator = "is_between"
		start, startOK := words.value()
		if !startOK || !words.keyword("AND") {
			return condition, false
		}
		end, endOK := words.value()
		condition.values = []string{start, end}
		return condition, endOK
	case words.keyword("DOES", "NOT", "CONTAIN"), words.keyword("NOT", "CONTAINS"):
		condition.operator = "does_not_contain"
	case words.keyword("CONTAINS"):
		condition.operator = "contains"
	case words.keyword("STARTS", "WITH"):
		condition.operator = "starts_with"
	case words.keyword("ENDS", "WITH"):
		condition.operator = "ends_with"
	default:
		for _, mark := range []string{"=", "!=", ">=", "<=", ">", "<"} {
			if words.mark(mark) {
				condition.operator = mark
				break
			}
		}
		if condition.operator == "" {
			return condition, false
		}
	}
	value, ok := words.value()
	condition.values = []string{value}
	return condition, ok
}
