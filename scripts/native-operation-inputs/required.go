package main

import (
	"go/ast"
	"go/token"
	"slices"
	"sort"
	"strings"
)

// A field is required where the handler refuses the request without it
// (2026-11-16, an agent calling every operation: requestId, mutationId,
// baseCursor and others looked optional and each refusal named one at a
// time). The handler says so by a check that the field is empty — alone, or
// among checks joined by || — whose branch refuses: returns an error, a
// refusal or false. A validator it hands the body to (a function named
// valid…, or one whose false answer it refuses on, required_refused_unless.go)
// says so by the checks its answer joins with &&. A check joined by && with
// another is a condition, not a need, and marks nothing.

// required marks the fields of a body's record its handler, or the
// functions it hands the body to, refuses the request without.
func (code source) required(body *ast.BlockStmt, variable string, fields []field, seen map[string]bool, depth int, validator bool) {
	mark := func(name string) {
		for index := range fields {
			if fields[index].named(name) && !fields[index].defaulted {
				fields[index].Required = true
			}
		}
	}
	// A check through a variable given a field (requestID :=
	// strings.TrimSpace(input.RequestID); requestID == "") or a failed
	// predicate on one (!pattern.MatchString(input.FileID)) needs it too.
	scalars := scalarAliases(body, variable)
	// One the steps give a value of their own when empty is optional (defaults.go).
	for name := range code.defaulted(body, variable, scalars) {
		for index := range fields {
			if fields[index].named(name) {
				fields[index].defaulted = true
			}
		}
	}
	local := code.localLists(body)
	// What the body's field reads, and a value its caller doesn't give
	// (required_handed.go).
	reads := func(expression ast.Expr) string {
		if name := fieldOf(expression, variable); name != "" {
			return name
		}
		return scalars[identOf(expression)]
	}
	held := func(expression ast.Expr) bool { return code.takesAGivenValue(expression, variable) }
	markTerm := func(term ast.Expr) {
		mark(emptyField(term, variable))
		mark(scalars[emptyIdent(term)])
		mark(code.failedPredicate(term, variable))
		mark(code.neededByCheck(term, variable, scalars, local)) // required_checks.go
		for _, name := range code.termNeeds(term, reads, held) {
			mark(name)
		}
	}
	flags := refusingFlags(body)
	// Only the function's own steps: a check inside another branch holds
	// for that branch alone. A maker's steps are those of the handler it
	// returns, and a transaction's those of the closure it runs.
	steps := steppedThrough(body)
	// What it hands on, and the numbers it checks in a branch, before a step
	// letting the call through without them (required_flow.go).
	admitted := steppedThrough(untilAdmitted(body))
	for at, statement := range admitted {
		for _, name := range code.handedNeeds(statement, nextStep(admitted, at), reads, depth) {
			mark(name)
		}
	}
	for _, statement := range steps {
		switch statement := statement.(type) {
		case *ast.IfStmt:
			if refusingBlock(statement.Body, flags) {
				for _, term := range joined(statement.Cond, token.LOR) {
					markTerm(term)
				}
			}
			mark(code.handedAloneNeeded(statement.Init, variable))
			for _, name := range code.problemNeeds(statement, variable, reads, flags) { // required_problems.go
				mark(name)
			}
		case *ast.AssignStmt:
			mark(code.handedAloneNeeded(statement, variable))
		case *ast.SwitchStmt:
			// switch input.Kind { case …: default: return refusal }
			mark(code.neededBySwitch(statement, variable, scalars))
			// switch { case input.RequestID == "" || …: return refusal }
			for _, clause := range statement.Body.List {
				if clause, isClause := clause.(*ast.CaseClause); isClause && statement.Tag == nil &&
					refusingBlock(&ast.BlockStmt{List: clause.Body}, flags) {
					for _, condition := range clause.List {
						for _, term := range joined(condition, token.LOR) {
							markTerm(term)
						}
					}
				}
			}
		case *ast.ReturnStmt:
			if validator && len(statement.Results) == 1 {
				for _, term := range joined(statement.Results[0], token.LAND) {
					mark(presentField(term, variable))
				}
			}
		}
	}
	// A number checked against the server's in any refusing branch.
	for _, name := range code.heldNeeds(admitted, reads, held, flags, func(name string) bool { return numericField(fields, name) }) {
		mark(name)
	}
	// A record made from the body's fields (admission := normalize(T{
	// RequestID: input.RequestID, …})) and checked in its place: what it
	// needs, the body needs.
	if depth < 2 {
		for alias, from := range code.aliases(body, variable) {
			made := make([]field, 0, len(from))
			for key := range from {
				made = append(made, field{Name: key})
			}
			code.required(body, alias, made, map[string]bool{}, depth+1, false)
			for _, entry := range made {
				if entry.Required {
					mark(from[entry.Name])
				}
			}
		}
	}
	validators := refusedUnless(steps, flags) // required_refused_unless.go
	ast.Inspect(body, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.FuncLit:
			// A closure taking the app runs as a transaction's steps.
			return takesApp(node)
		case *ast.CallExpr:
			if depth >= 2 {
				return true
			}
			name := ""
			switch fun := node.Fun.(type) {
			case *ast.SelectorExpr:
				name = fun.Sel.Name
			case *ast.Ident:
				name = fun.Name
			}
			function := code.functions[name]
			if function == nil || function.Body == nil || seen[name] {
				return true
			}
			for index, argument := range node.Args {
				if pointer, isPointer := argument.(*ast.UnaryExpr); isPointer {
					argument = pointer.X
				}
				if star, isStar := argument.(*ast.StarExpr); isStar {
					argument = star.X
				}
				// A field handed on alone (levelFits(page, input.Level)):
				// the values the function compares it with are the field's.
				if name := selectorField(argument, variable); name != "" {
					if parameter := parameterName(function, index); parameter != "" {
						for at := range fields {
							if strings.EqualFold(fields[at].Name, name) {
								code.parameterValues(function.Body, parameter, &fields[at])
							}
						}
					}
				}
				if ident, isIdent := argument.(*ast.Ident); isIdent && ident.Name == variable {
					if parameter := parameterName(function, index); parameter != "" {
						seen[name] = true
						code.required(function.Body, parameter, fields, seen, depth+1, strings.Contains(strings.ToLower(name), "valid") || validators[name])
					}
				}
			}
		}
		return true
	})
}

// aliases are the variables a body gives a record written from variable's
// fields, each with its keys' fields of variable.
func (code source) aliases(body *ast.BlockStmt, variable string) map[string]map[string]string {
	found := map[string]map[string]string{}
	for _, statement := range ownSteps(body) {
		assignment, isAssignment := statement.(*ast.AssignStmt)
		if !isAssignment || len(assignment.Lhs) != 1 || len(assignment.Rhs) != 1 {
			continue
		}
		alias, isIdent := assignment.Lhs[0].(*ast.Ident)
		if !isIdent {
			continue
		}
		value := assignment.Rhs[0]
		for {
			if call, isCall := value.(*ast.CallExpr); isCall && len(call.Args) == 1 {
				value = call.Args[0]
				continue
			}
			break
		}
		literal, isLiteral := value.(*ast.CompositeLit)
		if !isLiteral {
			continue
		}
		keys := map[string]string{}
		for _, element := range literal.Elts {
			pair, isPair := element.(*ast.KeyValueExpr)
			if !isPair {
				continue
			}
			key, isKey := pair.Key.(*ast.Ident)
			if name := fieldOf(pair.Value, variable); isKey && name != "" {
				keys[key.Name] = name
			}
		}
		if len(keys) > 0 {
			found[alias.Name] = keys
		}
	}
	return found
}

// ownSteps are a function's statements, or, for one that returns a handler
// (createPage(ygo)), that handler's.
func ownSteps(body *ast.BlockStmt) []ast.Stmt {
	if len(body.List) > 0 {
		if returned, isReturn := body.List[len(body.List)-1].(*ast.ReturnStmt); isReturn && len(returned.Results) == 1 {
			if handler, isHandler := returned.Results[0].(*ast.FuncLit); isHandler {
				return handler.Body.List
			}
		}
	}
	return body.List
}

// parameterValues adds the constants a function compares a parameter with
// to a field's values.
func (code source) parameterValues(body *ast.BlockStmt, parameter string, into *field) {
	add := func(expression ast.Expr, value ast.Expr) {
		ident, isIdent := expression.(*ast.Ident)
		text, constant := code.constant(value)
		if isIdent && ident.Name == parameter && constant && !slices.Contains(into.Values, text) {
			into.Values = append(into.Values, text)
			sort.Strings(into.Values)
		}
	}
	ast.Inspect(body, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.BinaryExpr:
			if node.Op == token.EQL || node.Op == token.NEQ {
				add(node.X, node.Y)
				add(node.Y, node.X)
			}
		case *ast.SwitchStmt:
			if node.Tag != nil {
				for _, statement := range node.Body.List {
					if clause, isClause := statement.(*ast.CaseClause); isClause {
						for _, value := range clause.List {
							add(node.Tag, value)
						}
					}
				}
			}
		}
		return true
	})
}

// joined are the checks an expression joins with op, outermost first.
func joined(expression ast.Expr, op token.Token) []ast.Expr {
	if paren, isParen := expression.(*ast.ParenExpr); isParen {
		return joined(paren.X, op)
	}
	if binary, isBinary := expression.(*ast.BinaryExpr); isBinary && binary.Op == op {
		return append(joined(binary.X, op), joined(binary.Y, op)...)
	}
	return []ast.Expr{expression}
}

// refuses tells a branch that returns an error, a refusal or false, or
// sets the error its function refuses with (err = errCustomEmojiFileID).
func refuses(block *ast.BlockStmt) bool {
	for _, statement := range block.List {
		switch statement := statement.(type) {
		case *ast.ReturnStmt:
			for _, result := range statement.Results {
				if refusal(result) {
					return true
				}
			}
		case *ast.AssignStmt:
			if ident, isIdent := statement.Lhs[0].(*ast.Ident); isIdent && len(statement.Lhs) == 1 && len(statement.Rhs) == 1 &&
				strings.HasPrefix(ident.Name, "err") && refusal(statement.Rhs[0]) {
				return true
			}
		}
	}
	return false
}

func refusal(expression ast.Expr) bool {
	switch expression := expression.(type) {
	case *ast.Ident:
		return expression.Name == "false" || strings.HasPrefix(expression.Name, "err")
	case *ast.CallExpr:
		name := ""
		switch fun := expression.Fun.(type) {
		case *ast.SelectorExpr:
			name = fun.Sel.Name
			// errors.New(errorCodeOrganizationSettingsConflict)
			if pack, named := fun.X.(*ast.Ident); named && pack.Name == "errors" && name == "New" {
				return true
			}
		case *ast.Ident:
			name = fun.Name
		}
		name = strings.ToLower(name)
		for _, word := range []string{"error", "refus", "invalid", "fail", "problem", "denied", "reject"} {
			if strings.Contains(name, word) {
				return true
			}
		}
	}
	return false
}

// emptyField is the field a check finds empty: X.F == "", == nil, == 0,
// len(X.F) == 0, strings.TrimSpace(X.F) == "", X.F <= 0, X.F < 1.
func emptyField(expression ast.Expr, variable string) string {
	if operand := emptyOperand(expression); operand != nil {
		return fieldOf(operand, variable)
	}
	return ""
}

// emptyOperand is what a check finds empty.
func emptyOperand(expression ast.Expr) ast.Expr {
	binary, isBinary := expression.(*ast.BinaryExpr)
	if !isBinary {
		return nil
	}
	switch binary.Op {
	case token.EQL:
		if empty(binary.Y) {
			return binary.X
		}
		if empty(binary.X) {
			return binary.Y
		}
	case token.LEQ:
		if literal(binary.Y, "0") {
			return binary.X
		}
	case token.LSS:
		if literal(binary.Y, "1") {
			return binary.X
		}
	}
	return nil
}

// presentField is the field a check finds given: X.F != "", != nil,
// len(X.F) > 0, X.F > 0, X.F >= 1.
func presentField(expression ast.Expr, variable string) string {
	binary, isBinary := expression.(*ast.BinaryExpr)
	if !isBinary {
		return ""
	}
	switch binary.Op {
	case token.NEQ:
		if empty(binary.Y) {
			return fieldOf(binary.X, variable)
		}
	case token.GTR:
		if literal(binary.Y, "0") {
			return fieldOf(binary.X, variable)
		}
	case token.GEQ:
		if literal(binary.Y, "1") {
			return fieldOf(binary.X, variable)
		}
	}
	return ""
}

func empty(expression ast.Expr) bool {
	if ident, isIdent := expression.(*ast.Ident); isIdent {
		return ident.Name == "nil"
	}
	return literal(expression, `""`) || literal(expression, "0")
}

func literal(expression ast.Expr, value string) bool {
	basic, isBasic := expression.(*ast.BasicLit)
	return isBasic && basic.Value == value
}

// fieldOf is the field of variable an expression reads, through a call that
// takes it alone (strings.TrimSpace, len) or a pointer's value.
func fieldOf(expression ast.Expr, variable string) string {
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
		}
		return selectorField(expression, variable)
	}
}
