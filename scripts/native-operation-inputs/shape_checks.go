package main

import (
	"go/ast"
	"go/token"
	"slices"
	"strings"
)

// checks reads a function's checks — those every request goes through
// (checkedSteps): the values they let through; and anywhere, a map's keys
// and those it needs.
func (scope *shapeScope) checks(body *ast.BlockStmt, seen map[string]bool) {
	scope.stepChecks(body.List, seen)
	ast.Inspect(body, func(node ast.Node) bool {
		// A key written out.
		if index, isIndex := node.(*ast.IndexExpr); isIndex {
			if held, known := scope.subject(index.X); known && held.mode == shapeSelf && strings.HasPrefix(held.field.Type, "map of ") {
				if text, constant := scope.code.constant(index.Index); constant {
					held.field.Keys = sortedAdd(held.field.Keys, text)
				}
			}
		}
		return true
	})
	scope.code.neededKeys(ownSteps(body), scope.subject)
}

// stepChecks reads the checks of steps every request goes through, and of
// the cases of a switch on a field (caseChecks).
func (scope *shapeScope) stepChecks(steps []ast.Stmt, seen map[string]bool) {
	// A valid… function's answer says what a value takes only where it is
	// its one answer: one that answers true first for some requests
	// (if span.Provider == "" { return true }) holds for the others alone.
	answers := 0
	for _, step := range scope.checkedSteps(steps) {
		if returned, isReturn := step.(*ast.ReturnStmt); isReturn && len(returned.Results) == 1 && identOf(returned.Results[0]) != "false" {
			answers++
		}
	}
	ast.Inspect(&ast.BlockStmt{List: steps}, func(node ast.Node) bool {
		if check, isIf := node.(*ast.IfStmt); isIf && answersTrue(check.Body) {
			answers++
		}
		return true
	})
	// switch value { case "a": return …; case "b": return … } then a
	// refusal: only its cases are taken.
	for index, step := range steps {
		choice, isSwitch := step.(*ast.SwitchStmt)
		if !isSwitch || index+1 >= len(steps) || defaultClause(choice) != nil || !scope.refuses(&ast.BlockStmt{List: steps[index+1 : index+2]}) {
			continue
		}
		if held, known := scope.subject(choice.Tag); known && choice.Tag != nil && casesReturn(choice) && casesConstant(choice, scope.code) {
			values := []string{}
			for _, clause := range choice.Body.List {
				for _, value := range clause.(*ast.CaseClause).List {
					if text, constant := scope.code.constant(value); constant {
						values = append(values, text)
					}
				}
			}
			scope.close(held, values)
		}
	}
	outerGuard := scope.guard
	defer func() { scope.guard = outerGuard }()
	for _, step := range scope.checkedSteps(steps) {
		scope.guard = outerGuard
		if guard := scope.guards[step]; guard != nil {
			scope.guard = guard
		}
		switch step := step.(type) {
		case *ast.IfStmt:
			if scope.refuses(step.Body) {
				if step.Init != nil {
					scope.hold(&ast.BlockStmt{List: []ast.Stmt{step.Init}})
				}
				scope.refusing(step.Cond)
			}
		case *ast.SwitchStmt:
			scope.switchChecks(step)
			if _, known := scope.subject(step.Tag); step.Tag != nil && known {
				scope.caseChecks(step, seen)
			}
		case *ast.ReturnStmt:
			if scope.validator && len(step.Results) == 1 && answers == 1 {
				scope.answered(step.Results[0])
			}
		}
	}
}

// close keeps values as all a subject's field takes, or hands them to the
// switch case reading them (caseChecks).
func (scope *shapeScope) close(held shapeSubject, values []string) {
	if scope.collect != nil {
		scope.collect(held, values)
		return
	}
	held.close(values)
}

// checkedSteps are the steps every request that gives a field goes through:
// a function's own, a loop's over what it ranges (each entry is checked),
// a branch taken when a field is given (if input.Mode != nil { … }), and
// those of a closure it hands RunInTransaction — not another branch's,
// whose check holds for that branch alone (as required.go reads them).
func (scope *shapeScope) checkedSteps(steps []ast.Stmt) []ast.Stmt {
	if len(steps) > 0 {
		if returned, isReturn := steps[len(steps)-1].(*ast.ReturnStmt); isReturn && len(returned.Results) == 1 {
			if handler, isHandler := returned.Results[0].(*ast.FuncLit); isHandler {
				steps = handler.Body.List
			}
		}
	}
	all := []ast.Stmt{}
	for _, step := range steps {
		all = append(all, step)
		switch step := step.(type) {
		case *ast.RangeStmt:
			all = append(all, scope.checkedSteps(step.Body.List)...)
		case *ast.ForStmt:
			all = append(all, scope.checkedSteps(step.Body.List)...)
		case *ast.BlockStmt:
			all = append(all, scope.checkedSteps(step.List)...)
		case *ast.IfStmt:
			// What its checks turn away by name holds for the field it finds
			// given alone (if input.ViewID != nil { … input.Type == "dashboard"
			// … } says nothing of a type sent without a view).
			if step.Else == nil && scope.givenCheck(step.Cond) {
				guard := scope.givenField(step.Cond)
				for _, inner := range scope.checkedSteps(step.Body.List) {
					if _, guarded := scope.guards[inner]; !guarded {
						scope.guards[inner] = guard
					}
					all = append(all, inner)
				}
			}
		}
		ast.Inspect(step, func(node ast.Node) bool {
			call, isCall := node.(*ast.CallExpr)
			if !isCall {
				return true
			}
			if calledName(call) != "RunInTransaction" || len(call.Args) != 1 {
				return true
			}
			if closure, isClosure := call.Args[0].(*ast.FuncLit); isClosure {
				all = append(all, scope.checkedSteps(closure.Body.List)...)
			}
			return false
		})
	}
	return all
}

// givenField is the field a check that finds fields given finds first.
func (scope *shapeScope) givenField(condition ast.Expr) *field {
	for _, part := range joined(condition, token.LAND) {
		if binary, isBinary := unstarred(part).(*ast.BinaryExpr); isBinary {
			if held, known := scope.subject(binary.X); known {
				return held.field
			}
		}
	}
	return nil
}

// refusing reads a condition that refuses the request: each of its terms
// joined by || turns away what it finds.
func (scope *shapeScope) refusing(condition ast.Expr) {
	// value == "code" among them: that one is turned away.
	excluded := map[*field][]string{}
	for _, either := range joined(condition, token.LOR) {
		if check, isBinary := unstarred(either).(*ast.BinaryExpr); isBinary && check.Op == token.EQL {
			if text, constant := scope.code.constant(check.Y); constant {
				if held, known := scope.subject(check.X); known {
					excluded[held.field] = append(excluded[held.field], text)
				}
			}
		}
	}
	closeLeft := func(held shapeSubject, values []string) {
		kept := []string{}
		for _, value := range values {
			if !slices.Contains(excluded[held.field], value) {
				kept = append(kept, value)
			}
		}
		if scope.guard == nil || scope.guard == held.field {
			for _, value := range excluded[held.field] {
				held.field.refusedValues = sortedAdd(held.field.refusedValues, value)
			}
		}
		scope.close(held, kept)
	}
	for _, either := range joined(condition, token.LOR) {
		// input.Frequency != "": refused given, a field this step doesn't take.
		if binary, isBinary := unstarred(either).(*ast.BinaryExpr); isBinary && scope.forbid != nil && scope.givenCheck(either) {
			if held, known := scope.subject(binary.X); known {
				scope.forbid(held.field)
			}
			continue
		}
		// value != "a" && value != "b": it is one of them.
		if value, values := scope.code.notOneOf(either); value != nil {
			if held, known := scope.subject(value); known {
				scope.close(held, values)
			}
			continue
		}
		// A value given and not in a list (input.F != nil && !valid(*input.F))
		// takes the list as one alone does, with any value let through beside
		// it (input.F != "none" && !valid(input.F)); a check joined with
		// anything else (capabilities && !validCapability(value)) holds only
		// then.
		parts := joined(either, token.LAND)
		for _, term := range parts {
			value, values := scope.closing(term)
			held, known := scope.subject(value)
			if value == nil || len(values) == 0 || !known {
				continue
			}
			// !slices.Contains(levels[input.Kind], input.Level): the kind is one
			// of the map's keys too.
			if key, keys := scope.code.listIndexedBy(term); key != nil {
				if keyed, isKnown := scope.subject(key); isKnown {
					scope.close(keyed, keys)
				}
			}
			if besides, fits := scope.besides(parts, term, held); fits {
				closeLeft(held, append(values, besides...))
			}
		}
	}
}

// closing is the value a check turns away for not being in a list, and the
// list: !slices.Contains(list, value), !list[value], list[value] == 0, !known
// after a lookup (_, known := list[value]), !validMode(value).
func (scope *shapeScope) closing(term ast.Expr) (ast.Expr, []string) {
	value, values := scope.code.notIn(term, scope.local)
	// limit == 0 where limit := intervalLimit(input.Frequency), or
	// roleFromName(input.Role) == roleNone: its answer for any other value.
	if check, isBinary := unstarred(term).(*ast.BinaryExpr); isBinary && check.Op == token.EQL {
		mapped, from := check.X, ast.Expr(nil)
		if ident, isIdent := mapped.(*ast.Ident); isIdent {
			if given, known := scope.mapped[ident.Name]; known {
				mapped, from = given, nil
			}
		}
		if call, isCall := mapped.(*ast.CallExpr); isCall && len(call.Args) == 1 {
			from = call.Args[0]
			if cases, otherwise := scope.code.mappingValues(calledName(call)); len(cases) > 0 && sameAnswer(otherwise, check.Y) {
				return from, cases
			}
		}
	}
	if check, isBinary := unstarred(term).(*ast.BinaryExpr); isBinary && check.Op == token.EQL && (empty(check.Y) || identOf(check.Y) == "false") {
		if index, isIndex := check.X.(*ast.IndexExpr); isIndex {
			if listed, isList := scope.code.listNamed(index.X, scope.local); isList {
				return index.Index, listed
			}
		}
	}
	if negated, isNot := term.(*ast.UnaryExpr); isNot && negated.Op == token.NOT && value == nil {
		if ident, isIdent := negated.X.(*ast.Ident); isIdent {
			if lookup, known := scope.lookups[ident.Name]; known && lookup.values != nil {
				return lookup.value, lookup.values
			}
		}
		// !validMode(value), or !levelFits(page, value) where one value is the
		// input's.
		if call, isCall := negated.X.(*ast.CallExpr); isCall {
			found := -1
			for index, argument := range call.Args {
				if _, known := scope.subject(argument); known {
					if found >= 0 {
						return value, values
					}
					found = index
				}
			}
			if found >= 0 {
				return call.Args[found], scope.code.predicateValuesAt(calledName(call), found, 0)
			}
		}
	}
	return value, values
}

// besides reads the checks joined by && with a closing one: each finds a
// field given (input.Mode != nil), or lets a value of the same field through
// (input.Role != "none"), which it names; any other and they don't fit.
func (scope *shapeScope) besides(parts []ast.Expr, closing ast.Expr, held shapeSubject) ([]string, bool) {
	values := []string{}
	for _, part := range parts {
		if part == closing || scope.givenCheck(part) {
			continue
		}
		value, allowed := scope.code.notOneOf(part)
		if other, known := scope.subject(value); value == nil || !known || other.field != held.field {
			return nil, false
		}
		values = append(values, allowed...)
	}
	return values, true
}

// givenCheck tells checks joined by && that only find fields given
// (input.Mode != nil, value != "", len(input.Types) > 0).
func (scope *shapeScope) givenCheck(condition ast.Expr) bool {
	for _, part := range joined(condition, token.LAND) {
		operand := ast.Expr(nil)
		if binary, isBinary := unstarred(part).(*ast.BinaryExpr); isBinary {
			switch {
			case binary.Op == token.NEQ && empty(binary.Y), binary.Op == token.GTR && literal(binary.Y, "0"):
				operand = binary.X
			}
		}
		if _, known := scope.subject(operand); operand == nil || !known {
			return false
		}
	}
	return true
}

// refuses tells a branch that turns the request away: it returns a refusal
// or an error of its own (errInvalid…, not a plain err that may be nil), or
// false in a function that validates a value.
func (scope *shapeScope) refuses(block *ast.BlockStmt) bool {
	for _, statement := range block.List {
		// A plan's checker that notes a problem (c.problem(…)).
		if step, isCall := statement.(*ast.ExprStmt); isCall {
			if call, isCall := step.X.(*ast.CallExpr); isCall && calledName(call) == "problem" {
				return true
			}
		}
		returned, isReturn := statement.(*ast.ReturnStmt)
		if !isReturn {
			continue
		}
		for _, result := range returned.Results {
			switch result := result.(type) {
			case *ast.Ident:
				if result.Name == "false" && scope.validating || result.Name != "err" && strings.HasPrefix(result.Name, "err") ||
					scope.refusals[result.Name] {
					return true
				}
			case *ast.CallExpr:
				if refusal(result) || scope.problems && calledName(result) == "Sprintf" {
					return true
				}
			case *ast.BasicLit:
				// A function naming what is wrong with a request ("blockType
				// isn't …"), empty for nothing.
				if scope.problems && !literal(result, `""`) {
					return true
				}
			}
		}
	}
	return false
}

// validatingName tells a function whose name says it checks a value.
func validatingName(name string) bool {
	lower := strings.ToLower(name)
	for _, word := range []string{"valid", "normalize", "parse", "check", "allowed", "accept", "sanitize", "clean"} {
		if strings.Contains(lower, word) {
			return true
		}
	}
	return false
}

// switchChecks reads a switch: on a value, one whose default refuses lets
// only its cases through; with no tag, a case that refuses is a check.
func (scope *shapeScope) switchChecks(statement *ast.SwitchStmt) {
	if statement.Tag == nil {
		for _, clause := range statement.Body.List {
			if clause, isClause := clause.(*ast.CaseClause); isClause && scope.refuses(&ast.BlockStmt{List: clause.Body}) {
				for _, condition := range clause.List {
					scope.refusing(condition)
				}
			}
		}
		return
	}
	if !casesConstant(statement, scope.code) || defaultClause(statement) == nil || !scope.refuses(&ast.BlockStmt{List: defaultClause(statement)}) {
		return
	}
	held, known := scope.subject(statement.Tag)
	if !known {
		return
	}
	values := []string{}
	for _, clause := range statement.Body.List {
		if clause, isClause := clause.(*ast.CaseClause); isClause {
			for _, value := range clause.List {
				if text, constant := scope.code.constant(value); constant {
					values = append(values, text)
				}
			}
		}
	}
	scope.close(held, values)
}

// neededKeys marks the keys a map is refused without: present read by
// _, present := values["k"] (alone, or in an if's start) and then a branch
// that refuses on !present.
func (code source) neededKeys(steps []ast.Stmt, subject func(ast.Expr) (shapeSubject, bool)) {
	reads := map[string]struct {
		held shapeSubject
		key  string
	}{}
	note := func(statement ast.Stmt) {
		assignment, isAssignment := statement.(*ast.AssignStmt)
		if !isAssignment || len(assignment.Lhs) != 2 || len(assignment.Rhs) != 1 {
			return
		}
		index, isIndex := assignment.Rhs[0].(*ast.IndexExpr)
		present, isIdent := assignment.Lhs[1].(*ast.Ident)
		if !isIndex || !isIdent {
			return
		}
		held, known := subject(index.X)
		key, constant := code.constant(index.Index)
		if known && constant && held.mode == shapeSelf {
			reads[present.Name] = struct {
				held shapeSubject
				key  string
			}{held, key}
		}
	}
	for _, statement := range steps {
		note(statement)
		check, isIf := statement.(*ast.IfStmt)
		if !isIf {
			continue
		}
		if check.Init != nil {
			note(check.Init)
		}
		if !refuses(check.Body) {
			continue
		}
		for _, term := range joined(check.Cond, token.LOR) {
			negated, isNot := term.(*ast.UnaryExpr)
			if !isNot || negated.Op != token.NOT {
				continue
			}
			if ident, isIdent := negated.X.(*ast.Ident); isIdent {
				if read, known := reads[ident.Name]; known {
					read.held.field.Keys = sortedAdd(read.held.field.Keys, read.key)
					read.held.field.RequiredKeys = sortedAdd(read.held.field.RequiredKeys, read.key)
				}
			}
		}
	}
}
