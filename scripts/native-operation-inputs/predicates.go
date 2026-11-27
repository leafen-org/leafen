package main

import (
	"go/ast"
	"go/token"
	"slices"
	"strings"
)

// The values a check lets through, read from how it is written: a predicate
// of one value (validTLSMode(mode)) and a value compared with several
// written out (shapes.go, required_checks.go).

// predicateValues are the values a function of one value answers true for:
// return slices.Contains(list, value); _, ok := list[value]; return ok;
// return value == "a" || value == "b" (or value == "c" && some other
// condition: c is taken there); a switch on it whose cases answer true and
// whose default (or the step after it) answers false; another such
// function's answer; each after any if value == "d" { return true }.
func (code source) predicateValues(name string, depth int) []string {
	return code.predicateValuesAt(name, 0, depth)
}

// predicateValuesAt is predicateValues for the value a function takes at
// that place among others (levelFits(page, level)).
func (code source) predicateValuesAt(name string, at, depth int) []string {
	function := code.functions[name]
	if function == nil || function.Body == nil || function.Type.Params == nil || len(function.Body.List) == 0 || depth > 2 {
		return nil
	}
	parameter := parameterName(function, at)
	steps := function.Body.List
	taken := []string{}
	for len(steps) > 1 {
		check, isIf := steps[0].(*ast.IfStmt)
		if !isIf || check.Init != nil || check.Else != nil || !answersTrue(check.Body) {
			break
		}
		values := code.equalsAny(check.Cond, parameter)
		if values == nil {
			return nil
		}
		taken, steps = append(taken, values...), steps[1:]
	}
	values := code.predicateSteps(function, steps, parameter, depth)
	if values == nil && len(taken) == 0 {
		return nil
	}
	return append(taken, values...)
}

// answersTrue tells a branch that answers true alone.
func answersTrue(block *ast.BlockStmt) bool {
	if len(block.List) != 1 {
		return false
	}
	returned, isReturn := block.List[0].(*ast.ReturnStmt)
	return isReturn && len(returned.Results) == 1 && identOf(returned.Results[0]) == "true"
}

// equalsAny are the values a condition finds a parameter is one of:
// value == "a" || value == "b", each perhaps with another condition
// (value == "c" && isDatabase(page)).
func (code source) equalsAny(condition ast.Expr, parameter string) []string {
	values := []string{}
	for _, either := range joined(condition, token.LOR) {
		check, isBinary := unstarred(joined(either, token.LAND)[0]).(*ast.BinaryExpr)
		if !isBinary || check.Op != token.EQL || identOf(check.X) != parameter {
			return nil
		}
		text, constant := code.constant(check.Y)
		if !constant {
			return nil
		}
		values = append(values, text)
	}
	return values
}

// predicateSteps are the values a predicate's last steps answer true for.
func (code source) predicateSteps(function *ast.FuncDecl, steps []ast.Stmt, parameter string, depth int) []string {
	if parameter == "" || len(steps) > 2 {
		return nil
	}
	local := code.localLists(function.Body)
	// switch value { case "a", "b": return true; default: return false }
	if choice, isSwitch := steps[0].(*ast.SwitchStmt); isSwitch && identOf(choice.Tag) == parameter {
		values := []string{}
		for _, clause := range choice.Body.List {
			clause, isClause := clause.(*ast.CaseClause)
			if !isClause || len(clause.Body) != 1 {
				return nil
			}
			returned, isReturn := clause.Body[0].(*ast.ReturnStmt)
			if !isReturn || len(returned.Results) != 1 {
				return nil
			}
			answer, isIdent := returned.Results[0].(*ast.Ident)
			if !isIdent || answer.Name != "true" && answer.Name != "false" {
				return nil
			}
			for _, value := range clause.List {
				text, constant := code.constant(value)
				if !constant {
					return nil
				}
				if answer.Name == "true" {
					values = append(values, text)
				}
			}
		}
		return values
	}
	returned, isReturn := steps[len(steps)-1].(*ast.ReturnStmt)
	if !isReturn || len(returned.Results) != 1 {
		return nil
	}
	answer := returned.Results[0]
	if len(steps) == 1 {
		if value, values := code.membership(answer, local); identOf(value) == parameter {
			return values
		}
		// return value == "a" || value == "b"
		if values := code.equalsAny(answer, parameter); len(values) > 0 {
			return values
		}
		if call, isCall := answer.(*ast.CallExpr); isCall && len(call.Args) == 1 && identOf(call.Args[0]) == parameter {
			return code.predicateValues(calledName(call), depth+1)
		}
		return code.narrowedPredicate(answer, parameter, depth)
	}
	// _, ok := list[value]; return ok
	lookup, isAssignment := steps[0].(*ast.AssignStmt)
	present, isIdent := answer.(*ast.Ident)
	if !isAssignment || !isIdent || len(lookup.Lhs) != 2 || len(lookup.Rhs) != 1 {
		return nil
	}
	named, isNamed := lookup.Lhs[1].(*ast.Ident)
	if index, isIndex := lookup.Rhs[0].(*ast.IndexExpr); isIndex && isNamed && named.Name == present.Name && identOf(index.Index) == parameter {
		values, _ := code.listNamed(index.X, local)
		return values
	}
	return nil
}

// notOneOf is the value a check finds isn't any of those written out, and
// them: value != "a" && value != "b", one or more, on the same value, and
// given (value != "").
func (code source) notOneOf(check ast.Expr) (ast.Expr, []string) {
	var value ast.Expr
	values := []string{}
	for _, part := range joined(check, token.LAND) {
		binary, isBinary := unstarred(part).(*ast.BinaryExpr)
		if !isBinary || binary.Op != token.NEQ {
			return nil, nil
		}
		// value != "" (or != nil) says it is given.
		if literal(binary.Y, `""`) || identOf(binary.Y) == "nil" {
			continue
		}
		text, constant := code.constant(binary.Y)
		if !constant {
			return nil, nil
		}
		if value != nil && !sameOperand(value, binary.X) {
			return nil, nil
		}
		value = binary.X
		values = append(values, text)
	}
	return value, values
}

// mappingValues are the values a function of one value maps to something
// (switch value { case "daily": return 365 … }), and what it answers for any
// other, after the switch or as its default: a check that its answer is that
// one (limit == 0, role == pageAccessRoleNone) turns the others away.
func (code source) mappingValues(name string) ([]string, ast.Expr) {
	function := code.functions[name]
	if function == nil || function.Body == nil || function.Type.Params == nil || len(function.Body.List) == 0 || len(function.Body.List) > 2 {
		return nil, nil
	}
	choice, isSwitch := function.Body.List[0].(*ast.SwitchStmt)
	if !isSwitch || identOf(choice.Tag) != parameterName(function, 0) {
		return nil, nil
	}
	values, otherwise := []string{}, ast.Expr(nil)
	for _, clause := range choice.Body.List {
		clause, isClause := clause.(*ast.CaseClause)
		if !isClause || len(clause.Body) != 1 {
			return nil, nil
		}
		returned, isReturn := clause.Body[0].(*ast.ReturnStmt)
		if !isReturn || len(returned.Results) != 1 {
			return nil, nil
		}
		if clause.List == nil {
			otherwise = returned.Results[0]
			continue
		}
		for _, value := range clause.List {
			text, constant := code.constant(value)
			if !constant {
				return nil, nil
			}
			values = append(values, text)
		}
	}
	if len(function.Body.List) == 2 {
		returned, isReturn := function.Body.List[1].(*ast.ReturnStmt)
		if !isReturn || len(returned.Results) != 1 || otherwise != nil {
			return nil, nil
		}
		otherwise = returned.Results[0]
	}
	return values, otherwise
}

// sameAnswer tells two answers written alike: both empty, or the same name.
func sameAnswer(one, other ast.Expr) bool {
	if one == nil || other == nil {
		return false
	}
	if empty(one) && empty(other) {
		return true
	}
	first, isIdent := unstarred(one).(*ast.Ident)
	second, alsoIdent := unstarred(other).(*ast.Ident)
	return isIdent && alsoIdent && first.Name == second.Name
}

// fieldAnswered is the field of a record a function of that record answers,
// as it is or a default in its stead (action := strings.TrimSpace(person.Action);
// if action != "" { return action }; return "keep").
func (code source) fieldAnswered(name string) string {
	function := code.functions[name]
	if function == nil || function.Body == nil || function.Type.Params == nil {
		return ""
	}
	parameter := parameterName(function, 0)
	aliases := map[string]string{}
	answered := ""
	ast.Inspect(function.Body, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.AssignStmt:
			for index, left := range node.Lhs {
				if ident, isIdent := left.(*ast.Ident); isIdent && index < len(node.Rhs) {
					if field := fieldOf(node.Rhs[index], parameter); field != "" {
						aliases[ident.Name] = field
					}
				}
			}
		case *ast.ReturnStmt:
			if len(node.Results) != 1 {
				return true
			}
			field := fieldOf(node.Results[0], parameter)
			if field == "" {
				field = aliases[identOf(node.Results[0])]
			}
			if field != "" && answered != "" && answered != field {
				answered = "-"
			} else if field != "" {
				answered = field
			}
		}
		return true
	})
	if answered == "-" {
		return ""
	}
	return answered
}

// narrowedPredicate are the values another predicate answers true for,
// narrowed by checks of how the value starts or ends:
// strings.HasSuffix(value, "_background") && validColor(value).
func (code source) narrowedPredicate(answer ast.Expr, parameter string, depth int) []string {
	var values []string
	keep := []func(string) bool{}
	for _, part := range joined(answer, token.LAND) {
		call, isCall := unstarred(part).(*ast.CallExpr)
		if !isCall {
			return nil
		}
		switch name := calledName(call); {
		case (name == "HasSuffix" || name == "HasPrefix") && len(call.Args) == 2 && identOf(call.Args[0]) == parameter:
			affix, constant := code.constant(call.Args[1])
			if !constant {
				return nil
			}
			if name == "HasSuffix" {
				keep = append(keep, func(value string) bool { return strings.HasSuffix(value, affix) })
			} else {
				keep = append(keep, func(value string) bool { return strings.HasPrefix(value, affix) })
			}
		case len(call.Args) == 1 && identOf(call.Args[0]) == parameter && values == nil:
			values = code.predicateValues(name, depth+1)
		default:
			return nil
		}
	}
	kept := []string{}
	for _, value := range values {
		if !slices.ContainsFunc(keep, func(fits func(string) bool) bool { return !fits(value) }) {
			kept = append(kept, value)
		}
	}
	return kept
}
