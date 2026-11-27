package app

import (
	"errors"
	"strconv"
	"strings"
)

// A formula is read into a tree before it is evaluated, so if(), ifs(),
// the ternary and the boolean operators evaluate only the branches they
// take, and let()/map() re-evaluate their bodies without reparsing. The web
// editor's evaluator reads the same grammar and must give the same results.

const (
	maxDatabaseFormulaExpressionRunes = 4096
	maxDatabaseFormulaSteps           = 100_000
	maxDatabaseFormulaListItems       = 1_000
	maxDatabaseFormulaListDepth       = 10
	// A value's text, in UTF-16 units: Notion's table showed a 10,000,000
	// character value whole; its Public API counted 100,000,000 but its
	// screen did not live through one (seen 2026-10-14). Past it a formula
	// fails here.
	maxDatabaseFormulaValueText = 10_000_000
	// Notion's own: repeat() writes at most this many copies.
	maxDatabaseFormulaRepeat = 10_000
	// Notion's own: padStart() and padEnd() pad to at most this many
	// characters (seen 2026-10-14).
	maxDatabaseFormulaPadLength = 10_000
)

var errDatabaseFormulaInvalid = errors.New(errorCodeDatabaseQueryInvalid)

// databaseFormulaSyntaxError is a formula that doesn't read, with what
// Notion's parser says of it (seen 2026-11-12), which the Public API passes
// on (notionFormulaParseFault); elsewhere it is errDatabaseFormulaInvalid.
type databaseFormulaSyntaxError struct{ description string }

func (databaseFormulaSyntaxError) Error() string { return errDatabaseFormulaInvalid.Error() }
func (databaseFormulaSyntaxError) Unwrap() error { return errDatabaseFormulaInvalid }

var (
	errDatabaseFormulaExpression = databaseFormulaSyntaxError{"Expression expected."}
	errDatabaseFormulaEnd        = databaseFormulaSyntaxError{"End of input expected."}
	errDatabaseFormulaMember     = databaseFormulaSyntaxError{"A property or function should come next."}
	// Leafen's own bounds, which Notion's parser words otherwise or not at all.
	errDatabaseFormulaLong = databaseFormulaSyntaxError{"Expression too long."}
	errDatabaseFormulaDeep = databaseFormulaSyntaxError{"This formula nests too many levels deep."}
)

func databaseFormulaExpected(token string) error {
	return databaseFormulaSyntaxError{"Expected token “" + token + "”."}
}

type databaseFormulaTokenKind int

const (
	databaseFormulaTokenNumber databaseFormulaTokenKind = iota
	databaseFormulaTokenString
	databaseFormulaTokenIdentifier
	databaseFormulaTokenOperator
	databaseFormulaTokenParen
	databaseFormulaTokenBracket
	databaseFormulaTokenDot
	databaseFormulaTokenComma
	databaseFormulaTokenQuestion
	databaseFormulaTokenColon
)

type databaseFormulaToken struct {
	kind  databaseFormulaTokenKind
	value string
}

func databaseFormulaASCIIDigit(value rune) bool { return value >= '0' && value <= '9' }

func databaseFormulaIdentifierStart(value rune) bool {
	return value == '_' || (value >= 'A' && value <= 'Z') || (value >= 'a' && value <= 'z')
}

// databaseFormulaStringEscape is what a backslash and the next character write
// in a string, as Notion's (seen 2026-11-04): the quote, a backslash, a new
// line and a tab; anything else keeps its backslash, so "\\d" reads as a
// pattern's digit.
func databaseFormulaStringEscape(character, quote rune) []rune {
	switch character {
	case quote, '"', '\\':
		return []rune{character}
	case 'n':
		return []rune{'\n'}
	case 't':
		return []rune{'\t'}
	}
	return []rune{'\\', character}
}

// databaseFormulaNumberEnd is where the number written at index ends: its
// digits, one decimal point with a digit after it and an exponent, as in 1e21
// or 2.5E-3. A point before a name is a method's, as in 1.equal(1) (Notion's,
// seen 2026-11-12), and one with no digit after it ("1.") waits for a
// property or function, as Notion's editor and API read a formula of today
// (seen 2026-11-15: "1." is refused at the point, a property or function
// wanted there). Notion's first kind (legacy) reads "1." and ".5" as numbers.
func databaseFormulaNumberEnd(runes []rune, index int, legacy bool) int {
	seenDot := runes[index] == '.'
	index++
	for index < len(runes) && (databaseFormulaASCIIDigit(runes[index]) || (runes[index] == '.' && !seenDot &&
		(legacy && (index+1 >= len(runes) || !databaseFormulaIdentifierStart(runes[index+1])) ||
			index+1 < len(runes) && databaseFormulaASCIIDigit(runes[index+1])))) {
		if runes[index] == '.' {
			seenDot = true
		}
		index++
	}
	if index < len(runes) && (runes[index] == 'e' || runes[index] == 'E') {
		digits := index + 1
		if digits < len(runes) && (runes[digits] == '+' || runes[digits] == '-') {
			digits++
		}
		if digits < len(runes) && databaseFormulaASCIIDigit(runes[digits]) {
			for index = digits; index < len(runes) && databaseFormulaASCIIDigit(runes[index]); index++ {
			}
		}
	}
	return index
}

// databaseFormulaCommentEnd is where a comment opened at index ("/*") ends
// as Notion's parser reads one (seen 2026-11-14): at the first "*/" after
// its opening, a "*" with something after it read with that character
// ("1 /** x **/ + 2" runs on past "**/" to the end and is 1), at the
// formula's end when it never closes ("1 /* x" is 1), or -1 where there is
// no comment, a lone "*" ending the formula among them ("1 /* a *" reads "/"
// and "*" and does not read at all).
func databaseFormulaCommentEnd(runes []rune, index int) int {
	if index+1 >= len(runes) || runes[index] != '/' || runes[index+1] != '*' {
		return -1
	}
	for index += 2; index < len(runes); {
		switch {
		case runes[index] != '*':
			index++
		case index+1 >= len(runes):
			return -1
		case runes[index+1] == '/':
			return index + 2
		default:
			index += 2
		}
	}
	return len(runes)
}

func tokenizeDatabaseFormula(input string) []databaseFormulaToken {
	return readDatabaseFormulaTokens([]rune(input))
}

type databaseFormulaNodeKind int

const (
	databaseFormulaNodeNumber databaseFormulaNodeKind = iota
	databaseFormulaNodeString
	databaseFormulaNodeIdentifier
	databaseFormulaNodeList
	databaseFormulaNodeCall
	databaseFormulaNodeMethod
	databaseFormulaNodeUnary
	databaseFormulaNodeBinary
	databaseFormulaNodeTernary
)

// A method node's first child is its receiver; a call's children are its
// arguments.
type databaseFormulaNode struct {
	kind     databaseFormulaNodeKind
	text     string
	number   float64
	children []*databaseFormulaNode
}

type databaseFormulaSyntax struct {
	tokens []databaseFormulaToken
	index  int
	depth  int
	// notion reads as Notion's parser does, today's way or with legacy its
	// first kind's (notion_formula_dialect.go).
	notion, legacy bool
}

func parseDatabaseFormula(expression string) (*databaseFormulaNode, error) {
	if len([]rune(expression)) > maxDatabaseFormulaExpressionRunes {
		return nil, errDatabaseFormulaLong
	}
	tokens := tokenizeDatabaseFormula(expression)
	// A formula of comments alone reads as nothing, as Notion's does
	// ("Expression expected"), not as an empty formula.
	if len(tokens) == 0 && strings.Contains(expression, "/*") {
		return nil, errDatabaseFormulaExpression
	}
	return parseDatabaseFormulaTokens(tokens)
}

// parseDatabaseFormulaTokens reads a formula's tokens into its tree.
func parseDatabaseFormulaTokens(tokens []databaseFormulaToken) (*databaseFormulaNode, error) {
	syntax := &databaseFormulaSyntax{tokens: tokens}
	// An empty formula is empty text; a value missing anywhere else is an
	// error, as Notion reads it.
	if len(syntax.tokens) == 0 {
		return &databaseFormulaNode{kind: databaseFormulaNodeString}, nil
	}
	node, err := syntax.expression()
	if err != nil {
		return nil, err
	}
	if syntax.index < len(syntax.tokens) {
		return nil, errDatabaseFormulaEnd
	}
	return node, nil
}

func (syntax *databaseFormulaSyntax) peek() *databaseFormulaToken {
	if syntax.index >= len(syntax.tokens) {
		return nil
	}
	return &syntax.tokens[syntax.index]
}

func (syntax *databaseFormulaSyntax) match(kind databaseFormulaTokenKind, value string) bool {
	token := syntax.peek()
	if token == nil || token.kind != kind || (value != "" && token.value != value) {
		return false
	}
	syntax.index++
	return true
}

// keyword matches and/or/not written as words, which are operators unless a
// call's parenthesis follows.
func (syntax *databaseFormulaSyntax) keyword(value string) bool {
	token := syntax.peek()
	if token == nil || token.kind != databaseFormulaTokenIdentifier || token.value != value {
		return false
	}
	if next := syntax.index + 1; next < len(syntax.tokens) &&
		syntax.tokens[next].kind == databaseFormulaTokenParen && syntax.tokens[next].value == "(" {
		return false
	}
	syntax.index++
	return true
}

// notOperator matches not written as a word as an operator only when a value
// may follow it; alone it reads as a name, as Notion reads it.
func (syntax *databaseFormulaSyntax) notOperator() bool {
	if next := syntax.index + 1; next >= len(syntax.tokens) {
		return false
	} else if token := syntax.tokens[next]; token.kind == databaseFormulaTokenComma ||
		token.kind == databaseFormulaTokenColon || token.kind == databaseFormulaTokenQuestion ||
		token.kind == databaseFormulaTokenParen && token.value == ")" ||
		token.kind == databaseFormulaTokenBracket && token.value == "]" {
		return false
	}
	return syntax.keyword("not")
}

// Notion's parser runs out of room at about 256 parentheses inside one
// another, 254 minuses each with its parentheses, 224 calls and 230 lists —
// around there it fails or not from one request to the next (seen
// 2026-11-12) — and Leafen's spends a fixed share of the same room on each:
// an expression 1000, a call's or method's values 142 more, a list's 112
// more, a minus or "not" 8. The web's parser spends the same
// (formulaSyntax.ts).
const (
	databaseFormulaNestingRoom    = 257_500
	databaseFormulaExpressionCost = 1000
	databaseFormulaCallCost       = 142
	databaseFormulaListCost       = 112
	databaseFormulaUnaryCost      = 8
)

// enter spends cost of the room what is inside the node being read takes,
// until leave gives it back.
func (syntax *databaseFormulaSyntax) enter(cost int) error {
	syntax.depth += cost
	if syntax.depth > databaseFormulaNestingRoom {
		return errDatabaseFormulaDeep
	}
	return nil
}

func (syntax *databaseFormulaSyntax) leave(cost int) { syntax.depth -= cost }

// spelled is how an operator written as a word is kept: as the word where
// Notion's reading writes it back so (notion_formula_dialect.go).
func (syntax *databaseFormulaSyntax) spelled(operator, word string) string {
	if syntax.notion {
		return word
	}
	return operator
}

func databaseFormulaBinary(operator string, left, right *databaseFormulaNode) *databaseFormulaNode {
	return &databaseFormulaNode{kind: databaseFormulaNodeBinary, text: operator, children: []*databaseFormulaNode{left, right}}
}

func (syntax *databaseFormulaSyntax) expression() (*databaseFormulaNode, error) {
	if err := syntax.enter(databaseFormulaExpressionCost); err != nil {
		return nil, err
	}
	defer syntax.leave(databaseFormulaExpressionCost)
	condition, err := syntax.or()
	if err != nil || !syntax.match(databaseFormulaTokenQuestion, "") {
		return condition, err
	}
	if elvis, read, err := syntax.notionElvis(condition); read {
		return elvis, err
	}
	whenTrue, err := syntax.expression()
	if err != nil {
		return nil, err
	}
	if !syntax.match(databaseFormulaTokenColon, "") {
		return nil, databaseFormulaExpected(":")
	}
	whenFalse, err := syntax.expression()
	if err != nil {
		return nil, err
	}
	return &databaseFormulaNode{kind: databaseFormulaNodeTernary, children: []*databaseFormulaNode{condition, whenTrue, whenFalse}}, nil
}

func (syntax *databaseFormulaSyntax) or() (*databaseFormulaNode, error) {
	left, err := syntax.and()
	for err == nil {
		operator := "||"
		if !syntax.match(databaseFormulaTokenOperator, "||") {
			if !syntax.keyword("or") {
				break
			}
			operator = syntax.spelled("||", "or")
		}
		var right *databaseFormulaNode
		if right, err = syntax.and(); err == nil {
			left = databaseFormulaBinary(operator, left, right)
		}
	}
	return left, err
}

func (syntax *databaseFormulaSyntax) and() (*databaseFormulaNode, error) {
	left, err := syntax.equality()
	for err == nil {
		operator := "&&"
		if !syntax.match(databaseFormulaTokenOperator, "&&") {
			if !syntax.keyword("and") {
				break
			}
			operator = syntax.spelled("&&", "and")
		}
		var right *databaseFormulaNode
		if right, err = syntax.equality(); err == nil {
			left = databaseFormulaBinary(operator, left, right)
		}
	}
	return left, err
}

func (syntax *databaseFormulaSyntax) binaryLevel(
	next func() (*databaseFormulaNode, error), operators ...string,
) (*databaseFormulaNode, error) {
	left, err := next()
	for err == nil {
		operator := ""
		for _, candidate := range operators {
			if syntax.match(databaseFormulaTokenOperator, candidate) {
				operator = candidate
				break
			}
		}
		if operator == "" {
			return left, nil
		}
		var right *databaseFormulaNode
		if right, err = next(); err == nil {
			left = databaseFormulaBinary(operator, left, right)
		}
	}
	return nil, err
}

func (syntax *databaseFormulaSyntax) equality() (*databaseFormulaNode, error) {
	return syntax.binaryLevel(syntax.comparison, "==", "!=")
}

func (syntax *databaseFormulaSyntax) comparison() (*databaseFormulaNode, error) {
	return syntax.binaryLevel(syntax.term, ">", ">=", "<", "<=")
}

func (syntax *databaseFormulaSyntax) term() (*databaseFormulaNode, error) {
	return syntax.binaryLevel(syntax.factor, "+", "-")
}

func (syntax *databaseFormulaSyntax) factor() (*databaseFormulaNode, error) {
	if syntax.notion {
		return syntax.notionFactor()
	}
	return syntax.binaryLevel(syntax.power, "*", "/", "%")
}

func (syntax *databaseFormulaSyntax) power() (*databaseFormulaNode, error) {
	left, err := syntax.unary()
	if err != nil || !syntax.match(databaseFormulaTokenOperator, "^") {
		return left, err
	}
	if err := syntax.enter(databaseFormulaExpressionCost); err != nil {
		return nil, err
	}
	defer syntax.leave(databaseFormulaExpressionCost)
	right, err := syntax.power()
	if err != nil {
		return nil, err
	}
	return databaseFormulaBinary("^", left, right), nil
}

func (syntax *databaseFormulaSyntax) unary() (*databaseFormulaNode, error) {
	if syntax.notion {
		if node, read, err := syntax.notionUnary(); read {
			return node, err
		}
	}
	operator := ""
	if syntax.match(databaseFormulaTokenOperator, "-") {
		operator = "-"
	} else if syntax.match(databaseFormulaTokenOperator, "!") || syntax.notOperator() {
		operator = "!"
	}
	if operator == "" {
		return syntax.primary()
	}
	if err := syntax.enter(databaseFormulaUnaryCost); err != nil {
		return nil, err
	}
	defer syntax.leave(databaseFormulaUnaryCost)
	operand, err := syntax.unary()
	if err != nil {
		return nil, err
	}
	return &databaseFormulaNode{kind: databaseFormulaNodeUnary, text: operator, children: []*databaseFormulaNode{operand}}, nil
}

func (syntax *databaseFormulaSyntax) primary() (*databaseFormulaNode, error) {
	var node *databaseFormulaNode
	token := syntax.peek()
	switch {
	case token != nil && token.kind == databaseFormulaTokenNumber:
		syntax.index++
		// A number past the largest is Infinity, as JavaScript reads it
		// (Notion's: 1e309, seen 2026-11-14).
		number, err := strconv.ParseFloat(databaseFormulaNumberLiteral(token.value), 64)
		if err != nil && !errors.Is(err, strconv.ErrRange) {
			return nil, errDatabaseFormulaExpression
		}
		node = &databaseFormulaNode{kind: databaseFormulaNodeNumber, number: number}
	case token != nil && token.kind == databaseFormulaTokenString:
		syntax.index++
		node = &databaseFormulaNode{kind: databaseFormulaNodeString, text: token.value}
	case syntax.match(databaseFormulaTokenBracket, "["):
		items, err := syntax.sequence(databaseFormulaTokenBracket, "]")
		if err != nil {
			return nil, err
		}
		node = &databaseFormulaNode{kind: databaseFormulaNodeList, children: items}
	case token != nil && token.kind == databaseFormulaTokenIdentifier:
		syntax.index++
		node = &databaseFormulaNode{kind: databaseFormulaNodeIdentifier, text: token.value}
		// A block_property reference Notion's API takes stands for a property's value.
		if !notionFormulaBlockReference(token.value) && syntax.match(databaseFormulaTokenParen, "(") {
			arguments, err := syntax.sequence(databaseFormulaTokenParen, ")")
			if err != nil {
				return nil, err
			}
			node = &databaseFormulaNode{kind: databaseFormulaNodeCall, text: token.value, children: arguments}
		}
	case syntax.match(databaseFormulaTokenParen, "("):
		inner, err := syntax.expression()
		if err != nil {
			return nil, err
		}
		if !syntax.match(databaseFormulaTokenParen, ")") {
			return nil, databaseFormulaExpected(")")
		}
		node = inner
	default:
		return nil, errDatabaseFormulaExpression
	}
	for syntax.match(databaseFormulaTokenDot, "") {
		method := syntax.peek()
		if method == nil || method.kind != databaseFormulaTokenIdentifier {
			return nil, errDatabaseFormulaMember
		}
		syntax.index++
		arguments := []*databaseFormulaNode{}
		if syntax.match(databaseFormulaTokenParen, "(") {
			var err error
			if arguments, err = syntax.sequence(databaseFormulaTokenParen, ")"); err != nil {
				return nil, err
			}
		}
		node = &databaseFormulaNode{
			kind: databaseFormulaNodeMethod, text: method.value,
			children: append([]*databaseFormulaNode{node}, arguments...),
		}
	}
	return node, nil
}

// sequence reads comma-separated expressions up to the closing token; an
// empty item is an error. A first item that doesn't begin is the closing
// token missing, and a later one an expression, as Notion's parser words
// them (seen 2026-11-12: "abs(" and "[)" expect the closing token, "abs(1,"
// an expression).
func (syntax *databaseFormulaSyntax) sequence(kind databaseFormulaTokenKind, closing string) ([]*databaseFormulaNode, error) {
	cost := databaseFormulaCallCost
	if kind == databaseFormulaTokenBracket {
		cost = databaseFormulaListCost
	}
	if err := syntax.enter(cost); err != nil {
		return nil, err
	}
	defer syntax.leave(cost)
	items := []*databaseFormulaNode{}
	if syntax.match(kind, closing) {
		return items, nil
	}
	for {
		start := syntax.index
		item, err := syntax.expression()
		if err == errDatabaseFormulaExpression && len(items) == 0 && syntax.index == start {
			return nil, databaseFormulaExpected(closing)
		}
		if err != nil {
			return nil, err
		}
		items = append(items, item)
		if syntax.match(databaseFormulaTokenComma, "") {
			continue
		}
		if syntax.match(kind, closing) {
			return items, nil
		}
		return nil, databaseFormulaExpected(closing)
	}
}

func databaseFormulaNumberLiteral(value string) string {
	if len(value) > 0 && value[len(value)-1] == '.' {
		return value + "0"
	}
	return value
}
