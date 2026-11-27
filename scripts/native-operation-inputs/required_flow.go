package main

import (
	"go/ast"
	"go/token"
	"slices"
	"strings"
)

// How a field's check is followed past the handler's own steps
// (required_handed.go): the function it is handed to alone or as a record's
// field, the flags a transaction sets, and the numbers checked against the
// server's in a branch.

// refusingFlags are the variables a function's own steps refuse on alone
// (if conflict { return apiError(…) }): a step setting one true refuses.
func refusingFlags(body *ast.BlockStmt) map[string]bool {
	flags := map[string]bool{}
	for _, statement := range ownSteps(body) {
		check, isIf := statement.(*ast.IfStmt)
		if !isIf || !refuses(check.Body) {
			continue
		}
		for _, term := range joined(check.Cond, token.LOR) {
			if ident, isIdent := term.(*ast.Ident); isIdent && ident.Name != "true" && ident.Name != "false" {
				flags[ident.Name] = true
			}
		}
	}
	return flags
}

// refusingBlock tells a branch that refuses, or sets a flag the function refuses on.
func refusingBlock(block *ast.BlockStmt, flags map[string]bool) bool {
	if refuses(block) {
		return true
	}
	return setsFlag(block, flags)
}

// refusesOutright tells a branch refusing with an answer of its own — an
// error it makes or names, or false — or setting a flag the function refuses
// on: not one passing on an error variable that may hold none (if token ==
// "" { return applyAlone(…), err }), as a function handed a field may
// answer a way it goes without it.
func refusesOutright(block *ast.BlockStmt, flags map[string]bool) bool {
	for _, statement := range block.List {
		switch statement := statement.(type) {
		case *ast.ReturnStmt:
			for _, result := range statement.Results {
				if refusal(result) && identOf(result) != "err" {
					return true
				}
			}
		case *ast.AssignStmt:
			if len(statement.Lhs) == 1 && len(statement.Rhs) == 1 && strings.HasPrefix(identOf(statement.Lhs[0]), "err") &&
				refusal(statement.Rhs[0]) && identOf(statement.Rhs[0]) != "err" {
				return true
			}
		}
	}
	return setsFlag(block, flags)
}

// setsFlag tells a branch setting a flag the function refuses on.
func setsFlag(block *ast.BlockStmt, flags map[string]bool) bool {
	for _, statement := range block.List {
		assignment, isAssignment := statement.(*ast.AssignStmt)
		if !isAssignment || assignment.Tok != token.ASSIGN || len(assignment.Lhs) != 1 || len(assignment.Rhs) != 1 {
			continue
		}
		if flags[identOf(assignment.Lhs[0])] && identOf(assignment.Rhs[0]) == "true" {
			return true
		}
	}
	return false
}

// takesApp tells a closure taking the app, as a transaction's does.
func takesApp(closure *ast.FuncLit) bool {
	for _, group := range closure.Type.Params.List {
		if selector, isSelector := group.Type.(*ast.SelectorExpr); isSelector && selector.Sel.Name == "App" {
			return true
		}
	}
	return false
}

// handedCalls are the calls whose refusal a step passes on as its own: one
// it answers with (return apply(…)), and one whose error the step after it,
// or its own check, refuses on (x, err := f(…); if err != nil { return err }).
// A call whose error it reads otherwise (if _, err := find(…); err == nil)
// refuses nothing of the step's.
func handedCalls(statement, next ast.Stmt) []*ast.CallExpr {
	calls := []*ast.CallExpr{}
	// The check: if err != nil { refuse }, or a switch's case err != nil: refuse.
	errorChecked := func(assignment ast.Stmt, check ast.Stmt) *ast.CallExpr {
		set, isAssignment := assignment.(*ast.AssignStmt)
		if !isAssignment || len(set.Rhs) != 1 || len(set.Lhs) == 0 {
			return nil
		}
		call, isCall := set.Rhs[0].(*ast.CallExpr)
		name := identOf(set.Lhs[len(set.Lhs)-1])
		failed := func(condition ast.Expr) bool {
			binary, isBinary := condition.(*ast.BinaryExpr)
			return isBinary && binary.Op == token.NEQ && identOf(binary.Y) == "nil" && name != "" && identOf(binary.X) == name
		}
		if !isCall {
			return nil
		}
		switch check := check.(type) {
		case *ast.IfStmt:
			if failed(check.Cond) && refuses(check.Body) {
				return call
			}
		case *ast.SwitchStmt:
			for _, clause := range check.Body.List {
				if clause, isClause := clause.(*ast.CaseClause); isClause && check.Tag == nil && refuses(&ast.BlockStmt{List: clause.Body}) &&
					slices.ContainsFunc(clause.List, failed) {
					return call
				}
			}
		}
		return nil
	}
	switch statement := statement.(type) {
	case *ast.ReturnStmt:
		for _, result := range statement.Results {
			if call, isCall := result.(*ast.CallExpr); isCall {
				calls = append(calls, call)
			}
		}
	case *ast.AssignStmt:
		if next != nil {
			if call := errorChecked(statement, next); call != nil {
				calls = append(calls, call)
			}
		}
	case *ast.IfStmt:
		if statement.Init != nil {
			if call := errorChecked(statement.Init, statement); call != nil {
				calls = append(calls, call)
			}
		}
	}
	return calls
}

// handedNeeds are the fields a step hands on to a function of the source
// that needs them: alone, or as a record's field (T{Expected: &expected}).
func (code source) handedNeeds(statement, next ast.Stmt, reads fieldReader, depth int) []string {
	needed := []string{}
	if depth > handedDepth {
		return needed
	}
	for _, call := range handedCalls(statement, next) {
		function := code.functions[calledName(call)]
		if function == nil || function.Body == nil || function.Type.Params == nil {
			continue
		}
		for index, argument := range call.Args {
			parameter := parameterName(function, index)
			if parameter == "" {
				continue
			}
			if name := reads(argument); name != "" {
				if code.subjectNeeded(function, aloneReader(parameter), false, numericType(parameterType(function, index)), depth+1) {
					needed = append(needed, name)
				}
				continue
			}
			if pointer, isPointer := argument.(*ast.UnaryExpr); isPointer && pointer.Op == token.AND {
				argument = pointer.X
			}
			literal, isLiteral := argument.(*ast.CompositeLit)
			if !isLiteral {
				continue
			}
			for _, element := range literal.Elts {
				pair, isPair := element.(*ast.KeyValueExpr)
				if !isPair {
					continue
				}
				key, isKey := pair.Key.(*ast.Ident)
				if !isKey {
					continue
				}
				value, address := pair.Value, false
				if pointer, isPointer := value.(*ast.UnaryExpr); isPointer && pointer.Op == token.AND {
					value, address = pointer.X, true
				}
				numeric := numericType(code.structFieldType(typeIdent(parameterType(function, index)), key.Name))
				if name := reads(value); name != "" && code.subjectNeeded(function, keyReader(parameter, key.Name), address, numeric, depth+1) {
					needed = append(needed, name)
				}
			}
		}
	}
	return needed
}

// aloneReader reads a parameter, through a call taking it alone.
func aloneReader(parameter string) fieldReader {
	return func(expression ast.Expr) string {
		if identOf(expression) == parameter {
			return parameter
		}
		return ""
	}
}

// keyReader reads a record parameter's field (transfer.ExpectedCursor, *transfer.ExpectedCursor).
func keyReader(parameter, key string) fieldReader {
	return func(expression ast.Expr) string {
		if selectorField(stripped(expression), parameter) == key {
			return key
		}
		return ""
	}
}

// subjectNeeded tells a function whose steps refuse what reads reads, left
// out: found empty, turned away by a predicate, a number unequal to one the
// server holds (a text or flag compared with one may be another of the
// caller's), or handed on to one that does. A pointer the caller gave by
// its address (given) is never nil there.
func (code source) subjectNeeded(function *ast.FuncDecl, reads fieldReader, given, numeric bool, depth int) bool {
	body := function.Body
	// One set when it comes empty is a default, never missing (defaults.go).
	for _, statement := range ownSteps(body) {
		if check, isIf := statement.(*ast.IfStmt); isIf && !refuses(check.Body) {
			if operand := emptyOperand(check.Cond); operand != nil && reads(operand) != "" && setsOperand(check.Body, operand) {
				return false
			}
		}
	}
	body = untilAdmitted(body)
	steps := steppedThrough(body)
	aliases := map[string]bool{}
	for _, statement := range steps {
		if assignment, isAssignment := statement.(*ast.AssignStmt); isAssignment && len(assignment.Lhs) == 1 && len(assignment.Rhs) == 1 {
			if alias := identOf(assignment.Lhs[0]); alias != "" && alias != "_" && reads(assignment.Rhs[0]) != "" {
				aliases[alias] = true
			}
		}
	}
	subject := func(expression ast.Expr) string {
		if name := reads(expression); name != "" {
			return name
		}
		if aliases[identOf(expression)] {
			return identOf(expression)
		}
		return ""
	}
	held := func(expression ast.Expr) bool {
		if !numeric || empty(expression) || subject(expression) != "" {
			return false
		}
		if text, constant := code.constant(expression); constant {
			return text != ""
		}
		ident, isIdent := expression.(*ast.Ident)
		return !isIdent || ident.Name != "false" && ident.Name != "nil" && parameterIndex(function.Type.Params, ident) < 0
	}
	flags := refusingFlags(body)
	terms := func(condition ast.Expr) []ast.Expr {
		found := []ast.Expr{}
		for _, term := range joined(condition, token.LOR) {
			found = append(found, givenTerm(term, subject, given))
		}
		return found
	}
	for at, statement := range steps {
		switch statement := statement.(type) {
		case *ast.IfStmt:
			if refusesOutright(statement.Body, flags) {
				for _, term := range terms(statement.Cond) {
					if len(code.termNeeds(term, subject, held)) > 0 {
						return true
					}
				}
			}
		case *ast.SwitchStmt:
			if statement.Tag != nil {
				continue
			}
			for _, clause := range statement.Body.List {
				if clause, isClause := clause.(*ast.CaseClause); isClause && refusesOutright(&ast.BlockStmt{List: clause.Body}, flags) {
					for _, condition := range clause.List {
						for _, term := range terms(condition) {
							if len(code.termNeeds(term, subject, held)) > 0 {
								return true
							}
						}
					}
				}
			}
		}
		if len(code.handedNeeds(statement, nextStep(steps, at), subject, depth)) > 0 {
			return true
		}
	}
	return len(code.heldNeeds(steps, subject, held, flags, func(string) bool { return true })) > 0
}

// givenTerm is a check with the nil test of a pointer given by its address
// left out: p.Expected != nil && held != *p.Expected is held != *p.Expected.
func givenTerm(term ast.Expr, subject fieldReader, given bool) ast.Expr {
	parts := joined(term, token.LAND)
	if !given || len(parts) != 2 {
		return term
	}
	for at, part := range parts {
		if binary, isBinary := part.(*ast.BinaryExpr); isBinary && binary.Op == token.NEQ && identOf(binary.Y) == "nil" && subject(binary.X) != "" {
			return parts[1-at]
		}
	}
	return term
}

// heldNeeds are the numbers a branch anywhere in the steps refuses unequal
// to a value the server holds, where no condition on the way reads them.
func (code source) heldNeeds(steps []ast.Stmt, reads fieldReader, held givenValue, flags map[string]bool, number func(string) bool) []string {
	needed := []string{}
	var walk func(statements []ast.Stmt, guards []ast.Expr)
	guarded := func(name string, guards []ast.Expr) bool {
		for _, guard := range guards {
			reading := false
			ast.Inspect(guard, func(node ast.Node) bool {
				if expression, isExpression := node.(ast.Expr); isExpression && reads(expression) == name {
					reading = true
				}
				return !reading
			})
			if reading {
				return true
			}
		}
		return false
	}
	check := func(condition ast.Expr, guards []ast.Expr) {
		for _, term := range joined(condition, token.LOR) {
			for _, name := range code.heldTermNeeds(term, reads, held) {
				if number(name) && !guarded(name, guards) {
					needed = append(needed, name)
				}
			}
		}
	}
	walk = func(statements []ast.Stmt, guards []ast.Expr) {
		for _, statement := range statements {
			switch statement := statement.(type) {
			case *ast.IfStmt:
				if refusesOutright(statement.Body, flags) {
					check(statement.Cond, guards)
				}
				inner := append(append([]ast.Expr{}, guards...), statement.Cond)
				walk(statement.Body.List, inner)
				if statement.Else != nil {
					walk([]ast.Stmt{statement.Else}, inner)
				}
			case *ast.BlockStmt:
				walk(statement.List, guards)
			case *ast.SwitchStmt:
				inner := guards
				if statement.Tag != nil {
					inner = append(append([]ast.Expr{}, guards...), statement.Tag)
				}
				for _, clause := range statement.Body.List {
					clause, isClause := clause.(*ast.CaseClause)
					if !isClause {
						continue
					}
					if statement.Tag == nil && refusesOutright(&ast.BlockStmt{List: clause.Body}, flags) {
						for _, condition := range clause.List {
							check(condition, inner)
						}
					}
					walk(clause.Body, append(append([]ast.Expr{}, inner...), clause.List...))
				}
			}
		}
	}
	walk(steps, nil)
	return needed
}

// heldTermNeeds are the fields a term refuses unequal to a held value:
// held != field, or !matches(held, field) answering current == expected.
func (code source) heldTermNeeds(term ast.Expr, reads fieldReader, held givenValue) []string {
	needed := []string{}
	switch term := term.(type) {
	case *ast.BinaryExpr:
		if term.Op == token.NEQ {
			for _, pair := range [][2]ast.Expr{{term.X, term.Y}, {term.Y, term.X}} {
				if name := reads(pair[0]); name != "" && reads(pair[1]) == "" && held(pair[1]) {
					needed = append(needed, name)
				}
			}
		}
	case *ast.UnaryExpr:
		if call, isCall := term.X.(*ast.CallExpr); isCall && term.Op == token.NOT {
			code.eachArgument(call, reads, func(function *ast.FuncDecl, parameter, name string, _ int) {
				other := equalityPredicate(function, parameter)
				if at := parameterIndex(function.Type.Params, &ast.Ident{Name: other}); other != "" && at >= 0 && at < len(call.Args) &&
					reads(call.Args[at]) == "" && held(call.Args[at]) {
					needed = append(needed, name)
				}
			})
		}
	}
	return needed
}

// untilAdmitted are a function's steps before one that lets the call through
// without those after it (if passwordGiven { return nil }, if !asked {
// return true }): the checks after it hold only some of the time. One
// answering nothing for a check turned down (if !authorize(event) { return
// nil }) has answered the call itself.
func untilAdmitted(body *ast.BlockStmt) *ast.BlockStmt {
	steps := ownSteps(body)
	for at, statement := range steps {
		check, isIf := statement.(*ast.IfStmt)
		if !isIf || len(check.Body.List) != 1 {
			continue
		}
		returned, isReturn := check.Body.List[0].(*ast.ReturnStmt)
		if !isReturn || slices.ContainsFunc(returned.Results, refusal) {
			continue
		}
		admits := len(returned.Results) == 1 && identOf(returned.Results[0]) == "true"
		if negated, isNot := check.Cond.(*ast.UnaryExpr); isNot && negated.Op == token.NOT && !admits {
			if _, isCall := negated.X.(*ast.CallExpr); isCall {
				continue
			}
		}
		return &ast.BlockStmt{List: steps[:at]}
	}
	return body
}

// nextStep is the step after the one at at, or nil.
func nextStep(steps []ast.Stmt, at int) ast.Stmt {
	if at+1 < len(steps) {
		return steps[at+1]
	}
	return nil
}
