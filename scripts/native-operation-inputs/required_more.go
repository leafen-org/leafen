package main

import (
	"go/ast"
	"go/token"
	"strings"
)

// A field is required where a check in a helper finds it missing too
// (2026-11-16, an agent calling every operation: sign-in without requestId
// and a custom emoji without fileId answered 400 naming no field, as their
// checks sat inside a function answering false or an error): a variable
// given the field and then found empty (requestID :=
// strings.TrimSpace(input.RequestID); requestID == ""), a pattern or a
// valid… function that turns it away (!pattern.MatchString(input.FileID)),
// and a function handed the field alone, in the handler's own steps, that
// refuses it empty (name, key, err := normalizeCustomEmojiName(input.Name)).

// scalarAliases are the variables a function's own steps give a field of
// variable, each with its field.
func scalarAliases(body *ast.BlockStmt, variable string) map[string]string {
	found := map[string]string{}
	for _, statement := range steppedThrough(body) {
		assignment, isAssignment := statement.(*ast.AssignStmt)
		if !isAssignment || len(assignment.Lhs) != 1 || len(assignment.Rhs) != 1 {
			continue
		}
		alias, isIdent := assignment.Lhs[0].(*ast.Ident)
		if !isIdent || alias.Name == "_" {
			continue
		}
		if name := fieldOf(assignment.Rhs[0], variable); name != "" {
			found[alias.Name] = name
		}
	}
	return found
}

// emptyIdent is the variable a check finds empty, through a call taking it
// alone (len, strings.TrimSpace).
func emptyIdent(expression ast.Expr) string {
	operand := emptyOperand(expression)
	for {
		switch current := operand.(type) {
		case *ast.CallExpr:
			if len(current.Args) != 1 {
				return ""
			}
			operand = current.Args[0]
			continue
		case *ast.ParenExpr:
			operand = current.X
			continue
		case *ast.Ident:
			return current.Name
		}
		return ""
	}
}

// failedPredicate is the field a check turns away by a pattern or a valid…
// function: !pattern.MatchString(input.F), !validID(input.F).
func (code source) failedPredicate(expression ast.Expr, variable string) string {
	negated, isNot := expression.(*ast.UnaryExpr)
	if !isNot || negated.Op != token.NOT {
		return ""
	}
	call, isCall := negated.X.(*ast.CallExpr)
	if !isCall || len(call.Args) != 1 {
		return ""
	}
	name := ""
	switch function := call.Fun.(type) {
	case *ast.SelectorExpr:
		name = function.Sel.Name
	case *ast.Ident:
		name = function.Name
	}
	field := fieldOf(call.Args[0], variable)
	if field == "" {
		return ""
	}
	// Only a pattern that turns away an empty value, and a valid… function
	// that does.
	if selector, isSelector := call.Fun.(*ast.SelectorExpr); isSelector && name == "MatchString" {
		if pattern, named := selector.X.(*ast.Ident); named && patterns[pattern.Name] != nil && !patterns[pattern.Name].MatchString("") {
			return field
		}
		return ""
	}
	function := code.functions[name]
	if !strings.HasPrefix(strings.ToLower(name), "valid") || function == nil || function.Body == nil || function.Type.Params == nil {
		return ""
	}
	if parameter := parameterName(function, 0); parameter != "" && (refusesEmpty(function.Body, parameter) || answersPresent(function.Body, parameter)) {
		return field
	}
	return ""
}

// answersPresent tells a function answering a check that its parameter is
// given, among those its answer joins with && (return value != "" && …).
func answersPresent(body *ast.BlockStmt, parameter string) bool {
	for _, statement := range ownSteps(body) {
		returned, isReturn := statement.(*ast.ReturnStmt)
		if !isReturn || len(returned.Results) != 1 {
			continue
		}
		for _, term := range joined(returned.Results[0], token.LAND) {
			if binary, isBinary := term.(*ast.BinaryExpr); isBinary {
				operand := binary.X
				switch {
				case binary.Op == token.NEQ && empty(binary.Y),
					binary.Op == token.GTR && literal(binary.Y, "0"),
					binary.Op == token.GEQ && literal(binary.Y, "1"):
					if identOf(operand) == parameter {
						return true
					}
				}
			}
		}
	}
	return false
}

// handedAloneNeeded is the field of variable a step hands alone to a function
// of the source that refuses it empty.
func (code source) handedAloneNeeded(statement ast.Stmt, variable string) string {
	assignment, isAssignment := statement.(*ast.AssignStmt)
	if !isAssignment || len(assignment.Rhs) != 1 {
		return ""
	}
	call, isCall := assignment.Rhs[0].(*ast.CallExpr)
	if !isCall {
		return ""
	}
	name := ""
	switch function := call.Fun.(type) {
	case *ast.SelectorExpr:
		name = function.Sel.Name
	case *ast.Ident:
		name = function.Name
	}
	function := code.functions[name]
	if function == nil || function.Body == nil || function.Type.Params == nil {
		return ""
	}
	for index, argument := range call.Args {
		field := selectorField(argument, variable)
		parameter := parameterName(function, index)
		if field != "" && parameter != "" && refusesEmpty(function.Body, parameter) {
			return field
		}
	}
	return ""
}

// refusesEmpty tells a function whose own steps refuse a parameter empty.
func refusesEmpty(body *ast.BlockStmt, parameter string) bool {
	names := map[string]bool{parameter: true}
	for _, statement := range ownSteps(body) {
		switch statement := statement.(type) {
		case *ast.AssignStmt:
			// value := strings.TrimSpace(value), or a variable given it.
			if len(statement.Lhs) == 1 && len(statement.Rhs) == 1 {
				if alias, isIdent := statement.Lhs[0].(*ast.Ident); isIdent && names[identOf(statement.Rhs[0])] {
					names[alias.Name] = true
				}
			}
		case *ast.IfStmt:
			if !refuses(statement.Body) {
				// Set when it comes empty, before any check refuses it (defaults.go).
				if operand := emptyOperand(statement.Cond); operand != nil && names[identOf(operand)] && setsOperand(statement.Body, operand) {
					return false
				}
				continue
			}
			for _, term := range joined(statement.Cond, token.LOR) {
				if names[emptyIdent(term)] {
					return true
				}
			}
		}
	}
	return false
}

// identOf is the variable an expression reads, through calls taking it alone.
func identOf(expression ast.Expr) string {
	for {
		switch current := expression.(type) {
		case *ast.CallExpr:
			if len(current.Args) != 1 {
				return ""
			}
			expression = current.Args[0]
			continue
		case *ast.ParenExpr:
			expression = current.X
			continue
		case *ast.Ident:
			return current.Name
		}
		return ""
	}
}
