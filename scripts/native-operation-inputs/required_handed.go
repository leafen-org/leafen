package main

import (
	"go/ast"
	"go/token"
	"strings"
)

// A field is required, too, where its check sits past the handler's own
// steps (2026-11-26, the wider scorecard: an account suspended, a cleanup
// finished, a suggestion resolved, a page's sharing changed or a sprint
// updated without requestId or expectedRevision was told the state had
// changed — a 409, "check it again" — or named no field, and the operation
// list showed both as optional):
//
//   - a function the handler hands the field to alone (return
//     apply(event, input.RequestID, input.ExpectedRevision, …)) whose steps
//     refuse that parameter empty, unequal to a value the server holds, or
//     by one of the checks below, or hand it on alone to one that does;
//   - a predicate turning it away (!bounded(input.MutationID, max) answering
//     value != "" && …, !matches(row.Revision, input.ExpectedRevision)
//     answering current == expected), and a function naming its problem
//     (commentRevisionProblem(input.ExpectedRevision) != "") whose branch for
//     it empty answers one;
//   - the steps of a closure taking the app (func(txApp core.App) error)
//     that a step hands on or keeps to run, as a transaction's;
//   - a flag such a step sets (conflict = true) that the function refuses on;
//   - a number checked against the one the server holds — the revision or
//     cursor it was read at — in any refusing branch, unless a condition on
//     the way reads that number itself (it is checked only when given): one
//     left out is refused whenever the server's has moved from 0, so a
//     caller sends what it read, 0 where there was none.

// handedDepth bounds how far a field handed on alone is followed.
const handedDepth = 3

// fieldReader is the field of the body an expression reads, or "".
type fieldReader func(ast.Expr) string

// givenValue tells a value the caller doesn't give: one the server holds or
// one written out, not empty.
type givenValue func(ast.Expr) bool

// termNeeds are the fields a refusing check's term needs given.
func (code source) termNeeds(term ast.Expr, reads fieldReader, held givenValue) []string {
	needed := code.heldTermNeeds(term, reads, held)
	if operand := emptyOperand(term); operand != nil {
		if name := reads(operand); name != "" {
			needed = append(needed, name)
		}
	}
	switch term := term.(type) {
	case *ast.BinaryExpr:
		// f(…, field, …) != "": a function naming its problem.
		for _, pair := range [][2]ast.Expr{{term.X, term.Y}, {term.Y, term.X}} {
			if call, isCall := pair[0].(*ast.CallExpr); isCall && term.Op == token.NEQ && literal(pair[1], `""`) {
				code.eachArgument(call, reads, func(function *ast.FuncDecl, parameter, name string, _ int) {
					if problemWhenEmpty(function.Body, parameter) {
						needed = append(needed, name)
					}
				})
			}
		}
	case *ast.UnaryExpr:
		// !f(…, field, …) answering value != "" && …
		if call, isCall := term.X.(*ast.CallExpr); isCall && term.Op == token.NOT {
			code.eachArgument(call, reads, func(function *ast.FuncDecl, parameter, name string, _ int) {
				if answersPresent(function.Body, parameter) {
					needed = append(needed, name)
				}
			})
		}
	}
	return needed
}

// eachArgument visits each argument of a call to a function of the source
// that reads a field, with the parameter it is given as.
func (code source) eachArgument(call *ast.CallExpr, reads fieldReader, visit func(*ast.FuncDecl, string, string, int)) {
	function := code.functions[calledName(call)]
	if function == nil || function.Body == nil || function.Type.Params == nil {
		return
	}
	for index, argument := range call.Args {
		if name := reads(argument); name != "" {
			if parameter := parameterName(function, index); parameter != "" {
				visit(function, parameter, name, index)
			}
		}
	}
}

// equalityPredicate is the other parameter a function's answer compares a
// parameter with alone: return current == expected.
func equalityPredicate(function *ast.FuncDecl, parameter string) string {
	steps := ownSteps(function.Body)
	if len(steps) != 1 {
		return ""
	}
	returned, isReturn := steps[0].(*ast.ReturnStmt)
	if !isReturn || len(returned.Results) != 1 {
		return ""
	}
	binary, isBinary := returned.Results[0].(*ast.BinaryExpr)
	if !isBinary || binary.Op != token.EQL {
		return ""
	}
	left, right := identOf(binary.X), identOf(binary.Y)
	for _, pair := range [][2]string{{left, right}, {right, left}} {
		if pair[0] == parameter && pair[1] != "" && pair[1] != parameter && parameterIndex(function.Type.Params, &ast.Ident{Name: pair[1]}) >= 0 {
			return pair[1]
		}
	}
	return ""
}

// problemWhenEmpty tells a function answering a problem's text when its
// parameter is empty: a branch of its steps finding it empty answers a text.
func problemWhenEmpty(body *ast.BlockStmt, parameter string) bool {
	for _, statement := range ownSteps(body) {
		switch statement := statement.(type) {
		case *ast.IfStmt:
			if answersText(statement.Body) && findsEmpty(statement.Cond, parameter) {
				return true
			}
		case *ast.SwitchStmt:
			if statement.Tag != nil {
				continue
			}
			for _, clause := range statement.Body.List {
				clause, isClause := clause.(*ast.CaseClause)
				if !isClause || !answersText(&ast.BlockStmt{List: clause.Body}) {
					continue
				}
				for _, condition := range clause.List {
					if findsEmpty(condition, parameter) {
						return true
					}
				}
			}
		}
	}
	return false
}

// findsEmpty tells a condition finding a variable empty among checks it joins with ||.
func findsEmpty(condition ast.Expr, name string) bool {
	for _, term := range joined(condition, token.LOR) {
		if emptyIdent(term) == name {
			return true
		}
	}
	return false
}

// answersText tells a branch answering a text that isn't empty.
func answersText(block *ast.BlockStmt) bool {
	for _, statement := range block.List {
		if returned, isReturn := statement.(*ast.ReturnStmt); isReturn && len(returned.Results) > 0 {
			return !literal(returned.Results[0], `""`)
		}
	}
	return false
}

// numericField tells a field holding a number.
func numericField(fields []field, name string) bool {
	for _, field := range fields {
		if strings.EqualFold(field.Name, name) {
			kind := strings.TrimPrefix(field.Type, "*")
			return strings.HasPrefix(kind, "int") || strings.HasPrefix(kind, "uint") || strings.HasPrefix(kind, "float")
		}
	}
	return false
}

// parameterType is the type a function's parameter at index is declared with.
func parameterType(function *ast.FuncDecl, index int) ast.Expr {
	at := 0
	for _, group := range function.Type.Params.List {
		for range group.Names {
			if at == index {
				return group.Type
			}
			at++
		}
	}
	return nil
}

// structFieldType is the type a struct of the source declares a field with.
func (code source) structFieldType(record, name string) ast.Expr {
	body := code.structs[record]
	if body == nil {
		return nil
	}
	for _, field := range body.Fields.List {
		for _, ident := range field.Names {
			if ident.Name == name {
				return field.Type
			}
		}
	}
	return nil
}

// numericType tells a number's type, or a pointer to one.
func numericType(expression ast.Expr) bool {
	if star, isStar := expression.(*ast.StarExpr); isStar {
		expression = star.X
	}
	ident, isIdent := expression.(*ast.Ident)
	return isIdent && (strings.HasPrefix(ident.Name, "int") || strings.HasPrefix(ident.Name, "uint") || strings.HasPrefix(ident.Name, "float"))
}
