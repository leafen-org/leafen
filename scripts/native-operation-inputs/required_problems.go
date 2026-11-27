package main

import (
	"go/ast"
	"go/token"
)

// A field is required, too, where a refusing check takes the reason a
// problem function gives (if problem := durableRequestIDProblem(input.RequestID);
// problem != "" { refuse }, or several of them through firstNonempty), and
// that function gives a reason for the field left out: its check of the
// value empty, alone among a switch case's or an if's checks, answers words
// (case requestID == "": return "requestId is empty: …"). The handler says
// what is missing, so its description says it too (2026-11-26, a sweep of
// every operation sent empty: an import without requestId was told
// "requestId is empty", and its description said it wasn't needed).

// problemNeeds are the fields a refusing check on a problem function's
// reason needs given.
func (code source) problemNeeds(statement *ast.IfStmt, variable string, reads fieldReader, flags map[string]bool) []string {
	assignment, isAssignment := statement.Init.(*ast.AssignStmt)
	condition, isBinary := statement.Cond.(*ast.BinaryExpr)
	if !isAssignment || !isBinary || len(assignment.Lhs) != 1 || len(assignment.Rhs) != 1 ||
		condition.Op != token.NEQ || !literal(condition.Y, `""`) || identOf(condition.X) != identOf(assignment.Lhs[0]) ||
		!refusingBlock(statement.Body, flags) {
		return nil
	}
	call, isCall := assignment.Rhs[0].(*ast.CallExpr)
	if !isCall {
		return nil
	}
	calls := []*ast.CallExpr{call}
	if calledName(call) == "firstNonempty" {
		calls = calls[:0]
		for _, argument := range call.Args {
			if inner, isInner := argument.(*ast.CallExpr); isInner {
				calls = append(calls, inner)
			}
		}
	}
	needed := []string{}
	for _, called := range calls {
		function := code.functions[calledName(called)]
		if function == nil || function.Body == nil || function.Type.Params == nil {
			continue
		}
		for index, argument := range called.Args {
			name, parameter := reads(argument), parameterName(function, index)
			if name != "" && parameter != "" && code.answersEmptyProblem(function.Body, parameter, nil, 0) {
				needed = append(needed, name)
			}
			// The record itself, whose fields left out it gives reasons for
			// (case input.PackageHash == "": return "packageHash is empty: …").
			if identOf(argument) == variable && parameter != "" {
				code.answersEmptyProblem(function.Body, parameter, func(field string) { needed = append(needed, field) }, 0)
			}
		}
	}
	return needed
}

// answersEmptyProblem tells a problem function giving a reason for its
// parameter left out: a switch case or an if whose check of it empty stands
// alone among the case's or the if's checks joined by ||, answering words,
// before any step answering none (if viewType != chart { return "" }), after
// which a check holds for some calls alone.
// For a record parameter, record is told each of its fields so. One it
// hands to another problem function, whose reason it answers with
// (if problem := commentIdentifierProblem("id", id); problem != "" { return
// problem + …}), is that function's to tell.
func (code source) answersEmptyProblem(body *ast.BlockStmt, parameter string, record func(string), depth int) bool {
	acceptsEarly := func(block []ast.Stmt) bool {
		for _, statement := range block {
			if returned, isReturn := statement.(*ast.ReturnStmt); isReturn && len(returned.Results) > 0 {
				return literal(returned.Results[0], `""`)
			}
		}
		return false
	}
	answers := func(block []ast.Stmt) bool {
		for _, statement := range block {
			if returned, isReturn := statement.(*ast.ReturnStmt); isReturn && len(returned.Results) > 0 {
				return !literal(returned.Results[0], `""`)
			}
		}
		return false
	}
	checks := func(condition ast.Expr) bool {
		for _, term := range joined(condition, token.LOR) {
			if record != nil {
				if field := emptyField(term, parameter); field != "" {
					record(field)
				}
				continue
			}
			if emptyIdent(term) == parameter {
				return true
			}
		}
		return false
	}
	for _, statement := range body.List {
		switch statement := statement.(type) {
		case *ast.IfStmt:
			if answers(statement.Body.List) && checks(statement.Cond) {
				return true
			}
			if record == nil && depth < 2 && answers(statement.Body.List) && code.handsOnProblem(statement, parameter, depth) {
				return true
			}
			if acceptsEarly(statement.Body.List) {
				return false
			}
		case *ast.SwitchStmt:
			if statement.Tag != nil {
				continue
			}
			for _, clause := range statement.Body.List {
				if clause, isClause := clause.(*ast.CaseClause); isClause && answers(clause.Body) &&
					func() bool {
						for _, condition := range clause.List {
							if checks(condition) {
								return true
							}
						}
						return false
					}() {
					return true
				}
			}
		}
	}
	return false
}

// handsOnProblem tells a step answering the reason another problem function
// gives for the parameter, handed to it alone, left out.
func (code source) handsOnProblem(statement *ast.IfStmt, parameter string, depth int) bool {
	assignment, isAssignment := statement.Init.(*ast.AssignStmt)
	condition, isBinary := statement.Cond.(*ast.BinaryExpr)
	if !isAssignment || !isBinary || len(assignment.Lhs) != 1 || len(assignment.Rhs) != 1 ||
		condition.Op != token.NEQ || !literal(condition.Y, `""`) || identOf(condition.X) != identOf(assignment.Lhs[0]) {
		return false
	}
	call, isCall := assignment.Rhs[0].(*ast.CallExpr)
	if !isCall {
		return false
	}
	function := code.functions[calledName(call)]
	if function == nil || function.Body == nil || function.Type.Params == nil {
		return false
	}
	for index, argument := range call.Args {
		if inner := parameterName(function, index); identOf(argument) == parameter && inner != "" &&
			code.answersEmptyProblem(function.Body, inner, nil, depth+1) {
			return true
		}
	}
	return false
}
