package main

import (
	"go/ast"
	"go/token"
	"slices"
)

// A field is required, too, where a refusing check takes only a value its
// missing one can't be (2026-11-26, real agents: a member added without a
// role was refused as "Check the member email.", and a page's version
// restored without baseRevision as a page that changed meanwhile, as
// neither check read as a need):
//
//   - it must be one written out (input.Role != workspaceRoleMember), or
//     equal one the server holds (frontier != input.BaseRevision);
//   - it must be in a list that holds no empty value
//     (!slices.Contains(list, input.Format), !list[input.Format],
//     !validFormat(input.Format) answering it is in one);
//   - a switch on it refuses by its default, and no case is empty.
//
// Checks a closure handed to RunInTransaction makes are the function's own
// steps (steppedThrough): it runs them every time.

// neededByCheck is the field of variable a refusing check needs given.
func (code source) neededByCheck(term ast.Expr, variable string, scalars map[string]string, local map[string][]string) string {
	read := func(expression ast.Expr) string {
		if name := fieldOf(expression, variable); name != "" {
			return name
		}
		return scalars[identOf(expression)]
	}
	if binary, isBinary := term.(*ast.BinaryExpr); isBinary && binary.Op == token.NEQ {
		for _, pair := range [][2]ast.Expr{{binary.X, binary.Y}, {binary.Y, binary.X}} {
			if name := read(pair[0]); name != "" && code.takesAGivenValue(pair[1], variable) {
				return name
			}
		}
		return ""
	}
	value, values := code.notIn(term, local)
	if negated, isNot := term.(*ast.UnaryExpr); isNot && negated.Op == token.NOT && value == nil {
		if call, isCall := negated.X.(*ast.CallExpr); isCall && len(call.Args) == 1 {
			value, values = call.Args[0], code.predicateValues(calledName(call), 0)
		}
	}
	if value != nil && len(values) > 0 && !slices.Contains(values, "") {
		return read(value)
	}
	return ""
}

// takesAGivenValue tells what a field must equal that its missing value
// isn't: a text or number written out, true, or a value the server holds —
// not an empty one, nor another of the body's own fields.
func (code source) takesAGivenValue(expression ast.Expr, variable string) bool {
	if empty(expression) {
		return false
	}
	if text, constant := code.constant(expression); constant {
		return text != ""
	}
	switch expression := expression.(type) {
	case *ast.BasicLit:
		return true
	case *ast.Ident:
		if value, known := code.consts[expression.Name]; known {
			return value != ""
		}
		if expression.Name == "false" || expression.Name == "nil" {
			return false
		}
	}
	reads := false
	ast.Inspect(expression, func(node ast.Node) bool {
		if ident, isIdent := node.(*ast.Ident); isIdent && ident.Name == variable {
			reads = true
		}
		return !reads
	})
	return !reads
}

// neededBySwitch is the field a switch on it needs given: its default
// refuses, and no case is the empty value.
func (code source) neededBySwitch(statement *ast.SwitchStmt, variable string, scalars map[string]string) string {
	if statement.Tag == nil {
		return ""
	}
	name := fieldOf(statement.Tag, variable)
	if name == "" {
		name = scalars[identOf(statement.Tag)]
	}
	if name == "" || !switchRefusesOthers(statement, code) {
		return ""
	}
	return name
}

// switchRefusesOthers tells a switch whose default refuses and whose cases
// are all values written out, none of them empty.
func switchRefusesOthers(statement *ast.SwitchStmt, code source) bool {
	refusing := false
	for _, clause := range statement.Body.List {
		clause, isClause := clause.(*ast.CaseClause)
		if !isClause {
			continue
		}
		if clause.List == nil {
			refusing = refuses(&ast.BlockStmt{List: clause.Body})
			continue
		}
		for _, value := range clause.List {
			if text, constant := code.constant(value); !constant || text == "" {
				return false
			}
		}
	}
	return refusing
}

// steppedThrough are a function's own steps (ownSteps) with those of the
// closures taking the app it hands on or keeps to run, as RunInTransaction's
// (required_flow.go).
func steppedThrough(body *ast.BlockStmt) []ast.Stmt {
	steps := ownSteps(body)
	all := slices.Clone(steps)
	for _, statement := range steps {
		ast.Inspect(statement, func(node ast.Node) bool {
			closure, isClosure := node.(*ast.FuncLit)
			if !isClosure {
				return true
			}
			if takesApp(closure) {
				all = append(all, ownSteps(closure.Body)...)
			}
			return false
		})
	}
	return all
}

// constantText tells a text written out, or a constant holding one.
func (code source) constantText(expression ast.Expr) bool {
	_, constant := code.constant(expression)
	return constant
}
