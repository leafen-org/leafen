package main

import (
	"go/ast"
	"go/token"
	"slices"
	"sort"
	"strings"
)

// The lists a handler checks a value against: a variable holding a list of
// texts written out ([]string{"page.created", …}) or a map keyed by them
// (map[string]bool{"includeFiles": true, …}), at the package's level or in
// the function itself, each entry a text or a constant. A check that a
// value is in one — slices.Contains(list, value), containsString(list,
// value), list[value] — names what the value takes (shapes.go).

// listLiterals are the package's list variables by name, read as their
// constants are (listValues).
var listLiterals = map[string]*ast.CompositeLit{}

// readLists keeps a package-level variable that holds a list written out.
func readLists(spec *ast.ValueSpec) {
	for index, name := range spec.Names {
		if index >= len(spec.Values) {
			continue
		}
		if literal, isList := listLiteral(spec.Values[index]); isList {
			listLiterals[name.Name] = literal
			if kind, isMap := literal.Type.(*ast.MapType); isMap {
				if _, holdsLists := kind.Value.(*ast.ArrayType); holdsLists {
					listsOfLists[name.Name] = literal
				}
			}
		}
		if from := builtFrom(spec.Values[index]); from != "" {
			listsBuiltFrom[name.Name] = from
		}
	}
}

// listsBuiltFrom are the package's lists a function makes from another
// list (var known = func() map[string]bool { … range values … }()), each
// with that list's name.
var listsBuiltFrom = map[string]string{}

// builtFrom is the list a function called in place ranges over to make its
// answer.
func builtFrom(expression ast.Expr) string {
	call, isCall := expression.(*ast.CallExpr)
	if !isCall || len(call.Args) != 0 {
		return ""
	}
	maker, isFunction := call.Fun.(*ast.FuncLit)
	if !isFunction {
		return ""
	}
	from := ""
	for _, step := range maker.Body.List {
		if loop, isRange := step.(*ast.RangeStmt); isRange {
			if list, isIdent := loop.X.(*ast.Ident); isIdent && from == "" {
				from = list.Name
			}
		}
	}
	return from
}

// listLiteral is a list of texts or a map keyed by them, written out.
func listLiteral(expression ast.Expr) (*ast.CompositeLit, bool) {
	literal, isLiteral := expression.(*ast.CompositeLit)
	if !isLiteral {
		return nil, false
	}
	switch kind := literal.Type.(type) {
	case *ast.ArrayType:
		if ident, isIdent := kind.Elt.(*ast.Ident); isIdent && ident.Name == "string" {
			return literal, true
		}
	case *ast.MapType:
		if ident, isIdent := kind.Key.(*ast.Ident); isIdent && ident.Name == "string" {
			return literal, true
		}
	}
	return nil, false
}

// listValues are the texts a list literal holds: a list's entries, a map's
// keys; none where an entry isn't a text or a constant.
func (code source) listValues(literal *ast.CompositeLit) []string {
	values := []string{}
	for _, element := range literal.Elts {
		if pair, isPair := element.(*ast.KeyValueExpr); isPair {
			element = pair.Key
		}
		text, constant := code.constant(element)
		if !constant {
			return nil
		}
		if !slices.Contains(values, text) {
			values = append(values, text)
		}
	}
	sort.Strings(values)
	return values
}

// localLists are the lists a function's body writes out itself, with the
// entries its steps add to a map (allowed["createFolders"] = true).
func (code source) localLists(body *ast.BlockStmt) map[string][]string {
	found := map[string][]string{}
	ast.Inspect(body, func(node ast.Node) bool {
		assignment, isAssignment := node.(*ast.AssignStmt)
		if !isAssignment || len(assignment.Lhs) != len(assignment.Rhs) {
			return true
		}
		for index, left := range assignment.Lhs {
			switch left := left.(type) {
			case *ast.Ident:
				if literal, isList := listLiteral(assignment.Rhs[index]); isList && assignment.Tok == token.DEFINE {
					if values := code.listValues(literal); values != nil {
						found[left.Name] = append(found[left.Name], values...)
					}
				}
			case *ast.IndexExpr:
				owner, isIdent := left.X.(*ast.Ident)
				if text, constant := code.constant(left.Index); isIdent && constant {
					if _, known := found[owner.Name]; known && !slices.Contains(found[owner.Name], text) {
						found[owner.Name] = append(found[owner.Name], text)
					}
				}
			}
		}
		return true
	})
	for name := range found {
		sort.Strings(found[name])
	}
	return found
}

// listsOfLists are the package's maps of lists written out
// (map[string][]string{"user": {"view", "edit"}, …}), by name.
var listsOfLists = map[string]*ast.CompositeLit{}

// listIndexedBy is the key a check looks a list up by in a map of lists,
// and the map's keys: !slices.Contains(levels[input.Kind], input.Level).
func (code source) listIndexedBy(term ast.Expr) (ast.Expr, []string) {
	negated, isNot := term.(*ast.UnaryExpr)
	if !isNot || negated.Op != token.NOT {
		return nil, nil
	}
	call, isCall := negated.X.(*ast.CallExpr)
	if !isCall || len(call.Args) != 2 {
		return nil, nil
	}
	index, isIndex := call.Args[0].(*ast.IndexExpr)
	if !isIndex {
		return nil, nil
	}
	owner, isIdent := index.X.(*ast.Ident)
	if !isIdent || listsOfLists[owner.Name] == nil {
		return nil, nil
	}
	return index.Index, code.listValues(listsOfLists[owner.Name])
}

// listNamed is what a list expression holds: one written in place, the
// function's own list, or the package's, or the one a package's list is
// made from.
func (code source) listNamed(expression ast.Expr, local map[string][]string) ([]string, bool) {
	// One written in place: map[string]bool{"a": true}[value].
	if literal, isList := listLiteral(expression); isList {
		values := code.listValues(literal)
		return values, len(values) > 0
	}
	// One of a map of lists, any of them: levels[input.Kind].
	if index, isIndex := expression.(*ast.IndexExpr); isIndex {
		if owner, isIdent := index.X.(*ast.Ident); isIdent && listsOfLists[owner.Name] != nil {
			values := []string{}
			for _, element := range listsOfLists[owner.Name].Elts {
				if pair, isPair := element.(*ast.KeyValueExpr); isPair {
					if inner, isLiteral := pair.Value.(*ast.CompositeLit); isLiteral {
						for _, value := range code.listValues(&ast.CompositeLit{Elts: inner.Elts}) {
							values = sortedAdd(values, value)
						}
					}
				}
			}
			return values, len(values) > 0
		}
	}
	ident, isIdent := expression.(*ast.Ident)
	if !isIdent {
		return nil, false
	}
	if values, known := local[ident.Name]; known {
		return values, len(values) > 0
	}
	if literal, known := listLiterals[ident.Name]; known {
		values := code.listValues(literal)
		return values, len(values) > 0
	}
	if from, built := listsBuiltFrom[ident.Name]; built {
		return code.listNamed(&ast.Ident{Name: from}, local)
	}
	return nil, false
}

// membership is the value a check finds in a list and that list's texts:
// slices.Contains(list, value), containsString(list, value) — a function
// whose name says it looks for one among others — or list[value].
func (code source) membership(expression ast.Expr, local map[string][]string) (ast.Expr, []string) {
	switch check := expression.(type) {
	case *ast.ParenExpr:
		return code.membership(check.X, local)
	case *ast.CallExpr:
		name := ""
		switch fun := check.Fun.(type) {
		case *ast.SelectorExpr:
			name = fun.Sel.Name
		case *ast.Ident:
			name = fun.Name
		}
		if len(check.Args) != 2 || !strings.Contains(strings.ToLower(name), "contain") {
			return nil, nil
		}
		if values, isList := code.listNamed(check.Args[0], local); isList {
			return check.Args[1], values
		}
	case *ast.IndexExpr:
		if values, isList := code.listNamed(check.X, local); isList {
			return check.Index, values
		}
	}
	return nil, nil
}

// notIn is the value a check turns away for not being in a list, with that
// list's texts: !slices.Contains(list, value), !list[value].
func (code source) notIn(expression ast.Expr, local map[string][]string) (ast.Expr, []string) {
	negated, isNot := expression.(*ast.UnaryExpr)
	if !isNot || negated.Op != token.NOT {
		return nil, nil
	}
	return code.membership(negated.X, local)
}
