package app

import (
	"errors"
	"strconv"
)

// A minus right before a number, in parentheses or not, is that number's
// sign, which a method after it is called on ("-10.abs()" and "-(10).abs()"
// are 10, "-1.5.round()" -1, "-10.add(1)" -9); before anything else a
// method is called first and the minus taken of what it gives
// ("-prop("Price").abs()" and "-(1 + 2).abs()" are -3, "-(-(10)).abs()"
// -10), as Notion reads them (seen 2026-11-28 through its API and in its
// editor). The web editor reads them the same (formulaAst.ts).

// signedNumber reads, right after a minus, a number it is the sign of with
// the methods called on it; signed is false where the minus is read as an
// operator.
func (syntax *databaseFormulaSyntax) signedNumber() (*databaseFormulaNode, bool, error) {
	operand, depth := syntax.index, 0
	for operand+depth < len(syntax.tokens) && syntax.tokens[operand+depth].kind == databaseFormulaTokenParen && syntax.tokens[operand+depth].value == "(" {
		depth++
	}
	after := operand + depth*2 + 1
	if after >= len(syntax.tokens) || syntax.tokens[operand+depth].kind != databaseFormulaTokenNumber || syntax.tokens[after].kind != databaseFormulaTokenDot {
		return nil, false, nil
	}
	for closing := operand + depth + 1; closing < after; closing++ {
		if syntax.tokens[closing].kind != databaseFormulaTokenParen || syntax.tokens[closing].value != ")" {
			return nil, false, nil
		}
	}
	if err := syntax.enter(databaseFormulaUnaryCost); err != nil {
		return nil, true, err
	}
	defer syntax.leave(databaseFormulaUnaryCost)
	number, err := databaseFormulaWrittenNumber(syntax.tokens[operand+depth].value)
	if err != nil {
		return nil, true, err
	}
	syntax.index = after
	sign := &databaseFormulaNode{kind: databaseFormulaNodeUnary, text: "-", children: []*databaseFormulaNode{number}}
	node, err := syntax.methods(sign)
	return node, true, err
}

// methods reads the methods called on a value.
func (syntax *databaseFormulaSyntax) methods(node *databaseFormulaNode) (*databaseFormulaNode, error) {
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

// databaseFormulaWrittenNumber is a number written in a formula; one past the
// largest is Infinity, as JavaScript reads it (Notion's: 1e309, seen
// 2026-11-14).
func databaseFormulaWrittenNumber(written string) (*databaseFormulaNode, error) {
	number, err := strconv.ParseFloat(databaseFormulaNumberLiteral(written), 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return nil, errDatabaseFormulaExpression
	}
	return &databaseFormulaNode{kind: databaseFormulaNodeNumber, number: number}, nil
}
