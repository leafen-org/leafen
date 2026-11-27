package main

import (
	"go/ast"
	"go/token"
)

// A field its handler gives a value of its own when it comes empty is
// optional, whatever checks follow (2026-11-23: a guest invite link's
// expiresInDays, 0 or left out for seven days, read as required, and
// leafen_call's refusals asked an agent for it): the checks after it see the
// default, never the field missing. The handler says so by a check that the
// field is empty — alone, in its own steps, through a variable given it
// (days := input.ExpiresInDays; if days == 0 { days = pageGuestLinkDefaultDays })
// or on the field itself (if input.Limit <= 0 { input.Limit = 50 }) — whose
// branch sets it and doesn't refuse.

// defaulted are the fields of variable a function's own steps set when they
// come empty.
func (code source) defaulted(body *ast.BlockStmt, variable string, scalars map[string]string) map[string]bool {
	found := map[string]bool{}
	for _, statement := range ownSteps(body) {
		check, isIf := statement.(*ast.IfStmt)
		if !isIf || refuses(check.Body) {
			continue
		}
		operand := emptyOperand(check.Cond)
		if operand == nil || !setsOperand(check.Body, operand) {
			continue
		}
		if name := selectorField(operand, variable); name != "" {
			found[name] = true
		} else if ident, isIdent := operand.(*ast.Ident); isIdent && scalars[ident.Name] != "" {
			found[scalars[ident.Name]] = true
		}
	}
	for name := range code.prefilled(body, variable) {
		found[name] = true
	}
	return found
}

// prefilled are the fields a step fills, before a later one reads the body
// into variable, from elsewhere (fillAdministratorSettingsInput(&input,
// current) setting *input = T{SignupPolicy: current.SignupPolicy, …}): one
// left out keeps the value it was filled with (2026-11-26, the settings
// change a screen sends whole and an agent in part).
func (code source) prefilled(body *ast.BlockStmt, variable string) map[string]bool {
	found := map[string]bool{}
	filled := map[string]bool{}
	for _, statement := range ownSteps(body) {
		var call *ast.CallExpr
		if expression, isExpression := statement.(*ast.ExprStmt); isExpression {
			call, _ = expression.X.(*ast.CallExpr)
		}
		function := (*ast.FuncDecl)(nil)
		if call != nil {
			function = code.functions[calledName(call)]
		}
		if call == nil || function == nil || function.Body == nil || calledName(call) == "BindBody" || calledName(call) == "Unmarshal" {
			if mentionsAddress(statement, variable) {
				// The body is read now: what was filled before stays where left out.
				if readsBodyInto(statement, variable) {
					for name := range filled {
						found[name] = true
					}
				}
				break
			}
			continue
		}
		for index, argument := range call.Args {
			pointer, isPointer := argument.(*ast.UnaryExpr)
			if !isPointer || pointer.Op != token.AND || identOf(pointer.X) != variable {
				continue
			}
			parameter := parameterName(function, index)
			ast.Inspect(function.Body, func(node ast.Node) bool {
				assignment, isAssignment := node.(*ast.AssignStmt)
				if !isAssignment || len(assignment.Lhs) != 1 || len(assignment.Rhs) != 1 || mentions(assignment.Rhs[0], parameter) {
					return true
				}
				if name := selectorField(assignment.Lhs[0], parameter); name != "" {
					filled[name] = true
				}
				if star, isStar := assignment.Lhs[0].(*ast.StarExpr); isStar && identOf(star.X) == parameter {
					if literal, isLiteral := assignment.Rhs[0].(*ast.CompositeLit); isLiteral {
						for _, element := range literal.Elts {
							if pair, isPair := element.(*ast.KeyValueExpr); isPair && identOf(pair.Key) != "" {
								filled[identOf(pair.Key)] = true
							}
						}
					}
				}
				return true
			})
		}
	}
	return found
}

// readsBodyInto tells a step reading the request's body into variable.
func readsBodyInto(statement ast.Stmt, variable string) bool {
	found := false
	ast.Inspect(statement, func(node ast.Node) bool {
		if call, isCall := node.(*ast.CallExpr); isCall && (calledName(call) == "BindBody" || calledName(call) == "Unmarshal") {
			for _, argument := range call.Args {
				if pointer, isPointer := argument.(*ast.UnaryExpr); isPointer && pointer.Op == token.AND && identOf(pointer.X) == variable {
					found = true
				}
			}
		}
		return !found
	})
	return found
}

// mentions tells an expression reading a variable.
func mentions(expression ast.Expr, name string) bool {
	found := false
	ast.Inspect(expression, func(node ast.Node) bool {
		if ident, isIdent := node.(*ast.Ident); isIdent && ident.Name == name {
			found = true
		}
		return !found
	})
	return found
}

// mentionsAddress tells a step handing on variable's address: the step
// reading the body into it, once it isn't a fill.
func mentionsAddress(statement ast.Stmt, variable string) bool {
	found := false
	ast.Inspect(statement, func(node ast.Node) bool {
		if pointer, isPointer := node.(*ast.UnaryExpr); isPointer && pointer.Op == token.AND && identOf(pointer.X) == variable {
			found = true
		}
		return !found
	})
	return found
}

// setsOperand tells a branch that sets what its check found empty: the
// same variable, or the same field of the same variable.
func setsOperand(block *ast.BlockStmt, operand ast.Expr) bool {
	for _, statement := range block.List {
		assignment, isAssignment := statement.(*ast.AssignStmt)
		if !isAssignment || assignment.Tok != token.ASSIGN || len(assignment.Lhs) != 1 {
			continue
		}
		if sameOperand(assignment.Lhs[0], operand) {
			return true
		}
	}
	return false
}

func sameOperand(left, right ast.Expr) bool {
	switch left := left.(type) {
	case *ast.Ident:
		ident, isIdent := right.(*ast.Ident)
		return isIdent && ident.Name == left.Name
	case *ast.SelectorExpr:
		selector, isSelector := right.(*ast.SelectorExpr)
		return isSelector && selector.Sel.Name == left.Sel.Name && sameOperand(left.X, selector.X)
	}
	return false
}
