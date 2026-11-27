package main

import (
	"go/ast"
	"go/token"
)

// A function the body is handed to is a validator, too, where the handler
// refuses the request when its answer is false — !normalize…(&input, …)
// among a refusing branch's checks, whatever its name: its answer's checks
// joined with && are what the body needs (2026-11-26, a real agent: the
// name change was refused "Check the profile." without its requestId, which
// normalizeAccountProfileInput answers false for, and no description said
// it was needed, so the sweep of required inputs, built on these
// descriptions, never called it).

// refusedUnless are the functions whose answer the steps refuse on when it
// is false: called negated among a refusing if's or switch case's checks.
func refusedUnless(steps []ast.Stmt, flags map[string]bool) map[string]bool {
	found := map[string]bool{}
	note := func(condition ast.Expr) {
		for _, term := range joined(condition, token.LOR) {
			negated, isNot := term.(*ast.UnaryExpr)
			if !isNot || negated.Op != token.NOT {
				continue
			}
			if call, isCall := negated.X.(*ast.CallExpr); isCall {
				if name := calledName(call); name != "" {
					found[name] = true
				}
			}
		}
	}
	for _, statement := range steps {
		switch statement := statement.(type) {
		case *ast.IfStmt:
			if refusingBlock(statement.Body, flags) {
				note(statement.Cond)
			}
		case *ast.SwitchStmt:
			if statement.Tag != nil {
				continue
			}
			for _, clause := range statement.Body.List {
				if clause, isClause := clause.(*ast.CaseClause); isClause && refusingBlock(&ast.BlockStmt{List: clause.Body}, flags) {
					for _, condition := range clause.List {
						note(condition)
					}
				}
			}
		}
	}
	return found
}
