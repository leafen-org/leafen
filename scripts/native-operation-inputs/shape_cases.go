package main

import (
	"go/ast"
	"go/token"
	"slices"
	"strings"
)

// The checks of a switch's cases and of a valid… function's answer
// (shape_checks.go).

// caseChecks reads the cases of a switch on a field (switch input.Action),
// each with steps of its own and the functions it calls: what one case's
// checks let a field take holds only where every case that reads the field
// checks it too (2026-11-26: a settings change's value is a policy's choice
// for one action and a group's name for another, and only the choice was
// checked).
func (scope *shapeScope) caseChecks(statement *ast.SwitchStmt, seen map[string]bool) {
	closed := map[*field][]string{}
	subjects := map[*field]shapeSubject{}
	open := map[*field]bool{}
	outerCollect, outerMention, outerForbid := scope.collect, scope.mention, scope.forbid
	defer func() { scope.collect, scope.mention, scope.forbid = outerCollect, outerMention, outerForbid }()
	for _, clause := range statement.Body.List {
		clause, isClause := clause.(*ast.CaseClause)
		if !isClause {
			continue
		}
		mine, read, refused := map[*field][]string{}, map[*field]bool{}, map[*field]bool{}
		scope.collect = func(held shapeSubject, values []string) {
			mine[held.field], subjects[held.field] = append(mine[held.field], values...), held
		}
		scope.mention = func(held *field) { read[held] = true }
		scope.forbid = func(held *field) { refused[held] = true }
		steps := &ast.BlockStmt{List: clause.Body}
		scope.stepChecks(clause.Body, seen)
		ast.Inspect(steps, func(node ast.Node) bool {
			if expression, isExpression := node.(ast.Expr); isExpression {
				scope.subject(expression)
			}
			return true
		})
		// The functions it calls, read afresh for this case.
		scope.walkCalls(steps, map[string]bool{})
		// A case that refuses the field given doesn't take it at all.
		for held := range read {
			if mine[held] == nil && !refused[held] {
				open[held] = true
			}
		}
		for held, values := range mine {
			closed[held] = append(closed[held], values...)
		}
	}
	scope.collect, scope.mention, scope.forbid = outerCollect, outerMention, outerForbid
	for held, values := range closed {
		if !open[held] {
			scope.close(subjects[held], values)
		}
	}
}

// answered reads a valid… function's answer: each part it joins with &&
// holds only for the values it names (value == nil || validDisplay(*value),
// slices.Contains(list, value), value == "a" || value == "b").
func (scope *shapeScope) answered(answer ast.Expr) {
	// What each part lets a field take; a part on it the reader can't read
	// says the others are a field's whole answer no longer, unless it is a
	// check of how the value starts or ends, which narrows them.
	closed := map[*field][]string{}
	subjects := map[*field]shapeSubject{}
	unread := map[*field]bool{}
	narrowed := map[*field][]func(string) bool{}
	for _, part := range joined(answer, token.LAND) {
		named := map[*field][]string{}
		take := func(value ast.Expr, values []string) {
			if held, known := scope.subject(value); known && len(values) > 0 {
				named[held.field], subjects[held.field] = append(named[held.field], values...), held
			}
		}
		whole := true
		for _, either := range joined(part, token.LOR) {
			switch check := unstarred(either).(type) {
			case *ast.BinaryExpr:
				if check.Op != token.EQL {
					whole = false
					continue
				}
				if empty(check.Y) {
					continue
				}
				if text, constant := scope.code.constant(check.Y); constant {
					take(check.X, []string{text})
					continue
				}
				whole = false
			case *ast.CallExpr:
				name := calledName(check)
				if value, values := scope.code.membership(check, scope.local); value != nil {
					take(value, values)
				} else if len(check.Args) == 1 {
					take(check.Args[0], scope.code.predicateValues(name, 0))
				} else if affix, constant := "", false; (name == "HasSuffix" || name == "HasPrefix") && len(check.Args) == 2 {
					if affix, constant = scope.code.constant(check.Args[1]); constant {
						if held, known := scope.subject(check.Args[0]); known {
							suffix := name == "HasSuffix"
							narrowed[held.field] = append(narrowed[held.field], func(value string) bool {
								return suffix && strings.HasSuffix(value, affix) || !suffix && strings.HasPrefix(value, affix)
							})
						}
					}
				} else {
					whole = false
				}
			default:
				whole = false
			}
		}
		if !whole || len(named) != 1 {
			ast.Inspect(part, func(node ast.Node) bool {
				if expression, isExpression := node.(ast.Expr); isExpression {
					if held, known := scope.subject(expression); known {
						unread[held.field] = true
					}
				}
				return true
			})
			continue
		}
		for held, values := range named {
			closed[held] = append(closed[held], values...)
		}
	}
	for held, values := range closed {
		if unread[held] {
			continue
		}
		kept := []string{}
		for _, value := range values {
			if !slices.ContainsFunc(narrowed[held], func(fits func(string) bool) bool { return !fits(value) }) {
				kept = append(kept, value)
			}
		}
		scope.close(subjects[held], kept)
	}
}

// casesReturn tells a switch each of whose cases ends by returning.
func casesReturn(choice *ast.SwitchStmt) bool {
	for _, clause := range choice.Body.List {
		clause, isClause := clause.(*ast.CaseClause)
		if !isClause || len(clause.Body) == 0 {
			return false
		}
		if _, isReturn := clause.Body[len(clause.Body)-1].(*ast.ReturnStmt); !isReturn {
			return false
		}
	}
	return true
}

// casesConstant tells a switch whose cases are all values written out, or
// the empty one (a field left out).
func casesConstant(choice *ast.SwitchStmt, code source) bool {
	for _, clause := range choice.Body.List {
		for _, value := range clause.(*ast.CaseClause).List {
			if _, constant := code.constant(value); !constant && !literal(value, `""`) {
				return false
			}
		}
	}
	return true
}

// defaultClause is a switch's default steps.
func defaultClause(statement *ast.SwitchStmt) []ast.Stmt {
	for _, clause := range statement.Body.List {
		if clause, isClause := clause.(*ast.CaseClause); isClause && clause.List == nil {
			return clause.Body
		}
	}
	return nil
}
