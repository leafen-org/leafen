package app

import (
	"strconv"
	"strings"
)

// Notion's API reads a formula as a formula of today and, where that
// doesn't read or doesn't check, as a formula of its first kind, which it
// keeps rewritten as today's — when that rewriting reads and checks by the
// first kind's rules; otherwise the request is refused with today's fault
// (seen 2026-11-12, API 2026-03-11). The first kind reads "+x", a number
// in another base, a number followed by pi or e ("2pi" 2 * pi()), and
// "<number> ? : <otherwise>"; both kinds read a value followed by
// parentheses as a call of that value, which today can't call and the first
// kind multiplies ("2(3)(4)" (2 * 3) * 4). Rewritten, its functions become
// today's: add, subtract, multiply, divide, mod and pow their operators,
// larger, largerEq, smaller, smallerEq, equal, unequal and not theirs,
// unaryMinus a minus, unaryPlus toNumber(), start and end dateStart() and
// dateEnd(), concat() of texts its texts joined in parentheses, slice() of a
// text substring(), and "? :" if(). Neither kind reads a minus right after
// a minus ("- -1", "--1"). The first kind has no lists, methods, "&&", "||",
// "!", nor today's newer functions; its "+" joins two texts or adds two
// numbers, and its "? :" gives one type.

// notionFormulaCalledValue marks a value followed by parentheses.
const notionFormulaCalledValue = "()"

// notionFormulaBlockReference tells a block_property reference, which the
// Notion reading keeps as one name.
func notionFormulaBlockReference(name string) bool {
	return strings.HasPrefix(name, notionFormulaBlockPropertyPrefix)
}

// parseNotionFormulaTokens reads tokens as Notion's parser does, today's way
// or, with legacy, its first kind's.
func parseNotionFormulaTokens(tokens []databaseFormulaToken, legacy bool) (*databaseFormulaNode, error) {
	syntax := &databaseFormulaSyntax{tokens: tokens, notion: true, legacy: legacy}
	node, err := syntax.expression()
	if err != nil {
		return nil, err
	}
	if syntax.index < len(syntax.tokens) {
		return nil, errDatabaseFormulaEnd
	}
	return node, nil
}

func (syntax *databaseFormulaSyntax) nextIs(kind databaseFormulaTokenKind, value string) bool {
	token := syntax.peek()
	return token != nil && token.kind == kind && (value == "" || token.value == value)
}

// notionUnary reads "+x" the first kind's way, and refuses a minus right
// after a minus; read is false where the usual reading goes on.
func (syntax *databaseFormulaSyntax) notionUnary() (*databaseFormulaNode, bool, error) {
	switch {
	case syntax.legacy && syntax.nextIs(databaseFormulaTokenOperator, "+"):
		syntax.index++
		if err := syntax.enter(databaseFormulaUnaryCost); err != nil {
			return nil, true, err
		}
		defer syntax.leave(databaseFormulaUnaryCost)
		operand, err := syntax.unary()
		if err != nil {
			return nil, true, err
		}
		return &databaseFormulaNode{kind: databaseFormulaNodeCall, text: "toNumber", children: []*databaseFormulaNode{operand}}, true, nil
	case syntax.nextIs(databaseFormulaTokenOperator, "-") && syntax.index+1 < len(syntax.tokens) &&
		syntax.tokens[syntax.index+1].kind == databaseFormulaTokenOperator && syntax.tokens[syntax.index+1].value == "-":
		return nil, true, errDatabaseFormulaExpression
	}
	return nil, false, nil
}

// notionFactor reads products and quotients, a value followed by
// parentheses, and in the first kind a number followed by pi or e.
func (syntax *databaseFormulaSyntax) notionFactor() (*databaseFormulaNode, error) {
	left, err := syntax.power()
	for err == nil {
		operator := ""
		for _, candidate := range []string{"*", "/", "%"} {
			if syntax.match(databaseFormulaTokenOperator, candidate) {
				operator = candidate
				break
			}
		}
		token, constant := syntax.peek(), false
		switch {
		case operator == "" && syntax.nextIs(databaseFormulaTokenParen, "("):
			operator = notionFormulaCalledValue
		case operator == "" && syntax.legacy && left.kind == databaseFormulaNodeNumber && token != nil &&
			token.kind == databaseFormulaTokenIdentifier && (token.value == "pi" || token.value == "e"):
			operator, constant = "*", true
		case operator == "":
			return left, nil
		}
		var right *databaseFormulaNode
		if right, err = syntax.power(); err != nil {
			break
		}
		if constant && right.kind == databaseFormulaNodeIdentifier {
			right = &databaseFormulaNode{kind: databaseFormulaNodeCall, text: right.text}
		}
		left = databaseFormulaBinary(operator, left, right)
	}
	return nil, err
}

// notionElvis reads the first kind's "<number> ? : <otherwise>"; read is
// false where the usual reading goes on.
func (syntax *databaseFormulaSyntax) notionElvis(condition *databaseFormulaNode) (*databaseFormulaNode, bool, error) {
	if !syntax.legacy || condition.kind != databaseFormulaNodeNumber || !syntax.match(databaseFormulaTokenColon, "") {
		return nil, false, nil
	}
	otherwise, err := syntax.expression()
	if err != nil {
		return nil, true, err
	}
	return &databaseFormulaNode{kind: databaseFormulaNodeTernary, children: []*databaseFormulaNode{condition, condition, otherwise}}, true, nil
}

// The first kind's functions; a formula calling another isn't one.
var notionFormulaLegacyFunctions = map[string]bool{}

func init() {
	for _, name := range strings.Fields("if add subtract multiply divide pow mod unaryMinus unaryPlus not and or equal unequal " +
		"larger largerEq smaller smallerEq concat join slice length format toNumber contains replace replaceAll test empty " +
		"abs cbrt ceil exp floor ln log10 log2 max min round sign sqrt start end now timestamp fromTimestamp dateAdd " +
		"dateSubtract dateBetween formatDate minute hour day date month year id e pi prop") {
		notionFormulaLegacyFunctions[name] = true
	}
}

var notionFormulaLegacyOperators = map[string]string{
	"add": "+", "subtract": "-", "multiply": "*", "divide": "/", "mod": "%", "pow": "^",
	"larger": ">", "largerEq": ">=", "smaller": "<", "smallerEq": "<=", "equal": "==", "unequal": "!=",
}

var notionFormulaLegacyRenames = map[string]string{"start": "dateStart", "end": "dateEnd", "unaryPlus": "toNumber"}

// notionFormulaMigrated is a formula read the first kind's way written as
// today's, or false where the first kind doesn't take it.
func notionFormulaMigrated(root *databaseFormulaNode, tokens []databaseFormulaToken, properties map[string]string) (string, bool) {
	for _, token := range tokens {
		if token.kind == databaseFormulaTokenBracket || token.kind == databaseFormulaTokenDot ||
			token.kind == databaseFormulaTokenOperator && (token.value == "&&" || token.value == "||" || token.value == "!") {
			return "", false
		}
	}
	converted, ok := notionFormulaConvert(root, properties)
	if !ok {
		return "", false
	}
	checker := &notionFormulaChecker{properties: properties, legacy: true}
	checker.check(converted, map[string]string{})
	if len(checker.problems) > 0 {
		return "", false
	}
	written := notionFormulaWrite(converted)
	// What it writes must read today ("unaryMinus(-1)" would be "--1").
	if reread, _, fault := notionFormulaReadWith(written, false, false); reread == nil || fault != "" {
		return "", false
	}
	return written, true
}

func notionFormulaConvert(node *databaseFormulaNode, properties map[string]string) (*databaseFormulaNode, bool) {
	children := make([]*databaseFormulaNode, len(node.children))
	for index, child := range node.children {
		converted, ok := notionFormulaConvert(child, properties)
		if !ok {
			return nil, false
		}
		children[index] = converted
	}
	next := &databaseFormulaNode{kind: node.kind, text: node.text, number: node.number, children: children}
	switch node.kind {
	case databaseFormulaNodeMethod, databaseFormulaNodeList:
		return nil, false
	case databaseFormulaNodeBinary:
		if node.text == notionFormulaCalledValue {
			next.text = "*"
		}
	case databaseFormulaNodeCall:
		if node.text != "toNumber" && node.text != "" && !notionFormulaLegacyFunctions[node.text] {
			return nil, false
		}
		typeOf := func(value *databaseFormulaNode) string {
			return (&notionFormulaChecker{properties: properties}).check(value, map[string]string{})
		}
		switch {
		case notionFormulaLegacyOperators[node.text] != "" && len(children) == 2:
			return databaseFormulaBinary(notionFormulaLegacyOperators[node.text], children[0], children[1]), true
		case node.text == "not" && len(children) == 1:
			return &databaseFormulaNode{kind: databaseFormulaNodeUnary, text: "!", children: children}, true
		case node.text == "unaryMinus" && len(children) == 1:
			return &databaseFormulaNode{kind: databaseFormulaNodeUnary, text: "-", children: children}, true
		case notionFormulaLegacyRenames[node.text] != "":
			next.text = notionFormulaLegacyRenames[node.text]
		case node.text == "slice" && len(children) > 0 && typeOf(children[0]) == "text":
			next.text = "substring"
		case node.text == "concat" && len(children) > 0:
			for _, child := range children {
				if typeOf(child) != "text" {
					return next, true
				}
			}
			joined := children[0]
			if len(children) > 1 {
				joined = &databaseFormulaNode{kind: databaseFormulaNodeBinary, text: "+", children: children}
			}
			// Its texts joined, in parentheses.
			return &databaseFormulaNode{kind: databaseFormulaNodeCall, children: []*databaseFormulaNode{joined}}, true
		}
	}
	return next, true
}

// notionFormulaWrite writes a rewritten formula as Notion writes it back:
// operators spaced, arguments after ", ", an operation inside another one
// in parentheses, as is a minus after an operator, "? :" as if() with any
// operation in it in parentheses, and joined texts in parentheses.
func notionFormulaWrite(node *databaseFormulaNode) string {
	switch node.kind {
	case databaseFormulaNodeNumber:
		return strconv.FormatFloat(node.number, 'f', -1, 64)
	case databaseFormulaNodeString:
		return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\t", `\t`).Replace(node.text) + `"`
	case databaseFormulaNodeList:
		return "[" + notionFormulaWriteAll(node.children, false) + "]"
	case databaseFormulaNodeCall:
		return node.text + "(" + notionFormulaWriteAll(node.children, false) + ")"
	case databaseFormulaNodeMethod:
		return notionFormulaWriteOperand(node.children[0], true) + "." + node.text + "(" + notionFormulaWriteAll(node.children[1:], false) + ")"
	case databaseFormulaNodeUnary:
		return node.text + notionFormulaWriteOperand(node.children[0], false)
	case databaseFormulaNodeBinary:
		operands := make([]string, len(node.children))
		for index, child := range node.children {
			operands[index] = notionFormulaWriteOperand(child, index > 0)
		}
		return strings.Join(operands, " "+node.text+" ")
	case databaseFormulaNodeTernary:
		return "if(" + notionFormulaWriteAll(node.children, true) + ")"
	}
	return node.text
}

// notionFormulaWriteOperand writes an operation inside another, in
// parentheses; a minus too when it follows an operator.
func notionFormulaWriteOperand(node *databaseFormulaNode, afterOperator bool) string {
	if node.kind == databaseFormulaNodeBinary || node.kind == databaseFormulaNodeTernary ||
		afterOperator && node.kind == databaseFormulaNodeUnary {
		return "(" + notionFormulaWrite(node) + ")"
	}
	return notionFormulaWrite(node)
}

// notionFormulaWriteAll writes values after ", ": in a call as they are but
// a "? :" in parentheses, in "? :" made if() any operation in parentheses.
func notionFormulaWriteAll(nodes []*databaseFormulaNode, conditional bool) string {
	written := make([]string, len(nodes))
	for index, node := range nodes {
		switch {
		case node.kind == databaseFormulaNodeTernary || conditional && node.kind == databaseFormulaNodeBinary:
			written[index] = "(" + notionFormulaWrite(node) + ")"
		default:
			written[index] = notionFormulaWrite(node)
		}
	}
	return strings.Join(written, ", ")
}
