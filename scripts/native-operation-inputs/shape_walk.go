package main

import (
	"go/ast"
	"strings"
)

// How the shape reader goes from a handler to what it hands the input, a
// record of it or a field to (shapes.go).

// walk reads one function: what its variables hold, then its checks, then
// what it hands on.
func (scope *shapeScope) walk(body *ast.BlockStmt, seen map[string]bool) {
	scope.hold(body)
	scope.checks(body, seen)
	scope.walkCalls(body, seen)
}

// walkCalls reads the functions a body hands the input, a record of it or a
// field to, shapeDepth calls deep — not those a switch on a field's cases call,
// which caseChecks reads case by case.
func (scope *shapeScope) walkCalls(body ast.Node, seen map[string]bool) {
	if scope.depth >= shapeDepth {
		return
	}
	ast.Inspect(body, func(node ast.Node) bool {
		if choice, isSwitch := node.(*ast.SwitchStmt); isSwitch && node != body {
			if _, known := scope.subject(choice.Tag); choice.Tag != nil && known {
				return false
			}
		}
		call, isCall := node.(*ast.CallExpr)
		if !isCall {
			return true
		}
		name := calledName(call)
		function := scope.code.functions[name]
		if closure, isClosure := scope.closures[name]; isClosure && function == nil {
			function = &ast.FuncDecl{Name: ast.NewIdent(name), Type: closure.Type, Body: closure.Body}
		}
		if function == nil || function.Body == nil || function.Type.Params == nil {
			return true
		}
		for index, argument := range call.Args {
			parameter := parameterName(function, index)
			if parameter == "" {
				continue
			}
			inner := scope.code.newShapeScope(function.Body, scope.records)
			inner.collect, inner.mention, inner.forbid, inner.depth = scope.collect, scope.mention, scope.forbid, scope.depth+1
			inner.validator = strings.HasPrefix(strings.ToLower(name), "valid")
			inner.validating = validatingName(name) || answersOK(function)
			inner.problems = answersProblem(name, function)
			key := ""
			if record := scope.recordOf(argument); record != "" {
				inner.vars[parameter], key = record, name+"|"+record
			} else if held, known := scope.subject(argument); known {
				inner.named[parameter], key = held, name+"|"+held.field.Name
			} else if list, isIdent := unstarred(argument).(*ast.Ident); isIdent && scope.lists[list.Name] != "" {
				// A list of the input's records it made (unmarked[index] = span).
				inner.lists[parameter], key = scope.lists[list.Name], name+"|list "+scope.lists[list.Name]
			} else if literal, isLiteral := unstarred(argument).(*ast.CompositeLit); isLiteral {
				// validLayout(layoutWire{Tabs: []tabWire{tab}}): a record
				// written out from the input's parts.
				key = scope.madeFrom(literal, inner, parameter, name)
			} else if made, isIdent := unstarred(argument).(*ast.Ident); isIdent {
				// A record written from the input's fields, handed on whole.
				for path, held := range scope.made {
					if rest, mine := strings.CutPrefix(path, made.Name+"."); mine {
						inner.made[parameter+"."+rest], key = held, name+"|"+made.Name
					}
				}
			}
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			inner.walk(function.Body, seen)
		}
		return true
	})
}

// answersOK tells a function that answers whether what it was given holds
// beside what it makes of it, its last result a bool (types, ok := …).
func answersOK(function *ast.FuncDecl) bool {
	results := function.Type.Results
	if results == nil || results.NumFields() < 2 {
		return false
	}
	last, isIdent := results.List[len(results.List)-1].Type.(*ast.Ident)
	return isIdent && last.Name == "bool"
}

// answersProblem tells a function that answers what is wrong with what it
// was given as words, none for nothing: its one result a string and its name
// saying so (suggestionInputProblem).
func answersProblem(name string, function *ast.FuncDecl) bool {
	results := function.Type.Results
	if results == nil || results.NumFields() != 1 {
		return false
	}
	text, isIdent := results.List[0].Type.(*ast.Ident)
	lower := strings.ToLower(name)
	return isIdent && text.Name == "string" && (strings.Contains(lower, "problem") || strings.Contains(lower, "refusal"))
}
