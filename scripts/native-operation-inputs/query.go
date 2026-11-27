package main

import (
	"go/ast"
	"go/token"
	"slices"
	"sort"
)

// The query names a handler reads, with the values it compares them with
// and whether it refuses the request without one (2026-11-16: an agent
// couldn't find that a list took workspaceId, not workspaces, or which
// scope= a log read took; several reads it never found). They are read
// where the handler, or a function it hands the request or the query to,
// calls Get or Has on the request's query, or takes a value from it by
// name.

// addQueries adds the query each route's handler reads to what it binds,
// for every handler a route names: functions and methods that take a
// request, and closures registered for a route.
func (code source) addQueries(inputs map[string]input, files []*ast.File) {
	add := func(name string, body *ast.BlockStmt, params *ast.FieldList) {
		query := code.queries(body, params, map[string]bool{}, 0)
		if len(query) == 0 {
			return
		}
		found := inputs[name]
		if found.Records == nil {
			found.Records = map[string][]field{}
		}
		found.Query = query
		inputs[name] = found
	}
	for name, function := range code.functions {
		if function.Recv == nil && readsRequest(function) {
			add(name, function.Body, function.Type.Params)
		}
	}
	for name, method := range code.methods {
		if readsRequest(method) {
			add(name, method.Body, method.Type.Params)
		}
	}
	code.eachRoute(files, func(method, path string, handler ast.Expr, _ *ast.FuncDecl) {
		if literal, isClosure := handler.(*ast.FuncLit); isClosure {
			add(closureName(method, path), literal.Body, literal.Type.Params)
		}
	})
}

// queries reads the query names a body reads, following the functions it
// hands the request or the query to.
func (code source) queries(body *ast.BlockStmt, params *ast.FieldList, seen map[string]bool, depth int) []field {
	// Variables that hold the query, and variables that hold one of its values.
	holders := map[string]bool{}
	if params != nil {
		for _, group := range params.List {
			if selector, isSelector := group.Type.(*ast.SelectorExpr); isSelector && selector.Sel.Name == "Values" {
				for _, name := range group.Names {
					holders[name.Name] = true
				}
			}
		}
	}
	read := map[string]*field{}
	order := []string{}
	named := map[string]string{}
	note := func(name string) *field {
		if read[name] == nil {
			read[name] = &field{Name: name, Type: "string"}
			order = append(order, name)
		}
		return read[name]
	}
	// nameOf is the query name an expression reads: query.Get("x"),
	// X.Query().Get("x"), query["x"], or a variable given one, or a record's
	// field given one (scope := T{Kind: query.Get("kind")}; scope.Kind).
	nameOf := func(expression ast.Expr) string {
		switch expression := expression.(type) {
		case *ast.Ident:
			return named[expression.Name]
		case *ast.SelectorExpr:
			if owner, isIdent := expression.X.(*ast.Ident); isIdent {
				return named[owner.Name+"."+expression.Sel.Name]
			}
		case *ast.CallExpr:
			selector, isSelector := expression.Fun.(*ast.SelectorExpr)
			if !isSelector || (selector.Sel.Name != "Get" && selector.Sel.Name != "Has") || len(expression.Args) != 1 || !code.queryHolder(selector.X, holders) {
				return ""
			}
			name, constant := code.constant(expression.Args[0])
			if !constant {
				return ""
			}
			return name
		case *ast.IndexExpr:
			if code.queryHolder(expression.X, holders) {
				if name, constant := code.constant(expression.Index); constant {
					return name
				}
			}
		}
		return ""
	}
	ast.Inspect(body, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.AssignStmt:
			for index, left := range node.Lhs {
				ident, isIdent := left.(*ast.Ident)
				if !isIdent || index >= len(node.Rhs) && len(node.Rhs) != 1 {
					continue
				}
				right := node.Rhs[min(index, len(node.Rhs)-1)]
				if call, isCall := right.(*ast.CallExpr); isCall {
					if selector, isSelector := call.Fun.(*ast.SelectorExpr); isSelector && selector.Sel.Name == "Query" && len(call.Args) == 0 {
						holders[ident.Name] = true
						continue
					}
				}
				if name := nameOf(stripped(right)); name != "" {
					named[ident.Name] = name
				}
				if literal, isLiteral := right.(*ast.CompositeLit); isLiteral {
					for _, element := range literal.Elts {
						pair, isPair := element.(*ast.KeyValueExpr)
						if !isPair {
							continue
						}
						if key, isKey := pair.Key.(*ast.Ident); isKey {
							if name := nameOf(stripped(pair.Value)); name != "" {
								named[ident.Name+"."+key.Name] = name
							}
						}
					}
				}
			}
		case *ast.CallExpr:
			if name := nameOf(node); name != "" {
				note(name)
			}
		case *ast.IndexExpr:
			if name := nameOf(node); name != "" {
				note(name)
			}
		case *ast.BinaryExpr:
			if node.Op == token.EQL || node.Op == token.NEQ {
				for _, pair := range [][2]ast.Expr{{node.X, node.Y}, {node.Y, node.X}} {
					if name := nameOf(stripped(pair[0])); name != "" {
						if value, constant := code.constant(pair[1]); constant && !slices.Contains(note(name).Values, value) {
							note(name).Values = append(note(name).Values, value)
						}
					}
				}
			}
		case *ast.SwitchStmt:
			if name := nameOf(stripped(node.Tag)); node.Tag != nil && name != "" {
				for _, statement := range node.Body.List {
					if clause, isClause := statement.(*ast.CaseClause); isClause {
						for _, value := range clause.List {
							if text, constant := code.constant(value); constant && !slices.Contains(note(name).Values, text) {
								note(name).Values = append(note(name).Values, text)
							}
						}
					}
				}
			}
		}
		return true
	})
	// What it refuses the request without, by its own steps: a check that
	// it is empty, or that it is one written out (required_checks.go), and
	// a switch on it whose default refuses.
	for _, statement := range ownSteps(body) {
		if check, isIf := statement.(*ast.IfStmt); isIf && refuses(check.Body) {
			for _, term := range joined(check.Cond, token.LOR) {
				binary, isBinary := term.(*ast.BinaryExpr)
				if !isBinary {
					continue
				}
				if binary.Op == token.EQL && literal(binary.Y, `""`) || binary.Op == token.NEQ && !empty(binary.Y) && code.constantText(binary.Y) {
					if name := nameOf(stripped(binary.X)); name != "" {
						note(name).Required = true
						// It takes only that one (shapes.go's closed).
						note(name).Closed = note(name).Closed || binary.Op == token.NEQ
					}
				}
			}
		}
		if choice, isSwitch := statement.(*ast.SwitchStmt); isSwitch && choice.Tag != nil && switchRefusesOthers(choice, code) {
			if name := nameOf(stripped(choice.Tag)); name != "" {
				note(name).Required, note(name).Closed = true, true
			}
		}
	}
	// The functions it hands the request or the query to.
	if depth < 2 {
		ast.Inspect(body, func(node ast.Node) bool {
			call, isCall := node.(*ast.CallExpr)
			if !isCall {
				return true
			}
			name := ""
			switch fun := call.Fun.(type) {
			case *ast.SelectorExpr:
				name = fun.Sel.Name
			case *ast.Ident:
				name = fun.Name
			}
			function := code.functions[name]
			if function == nil || function.Body == nil || seen[name] || function.Type.Params == nil {
				return true
			}
			hands := mentionsEvent(call)
			for _, argument := range call.Args {
				if ident, isIdent := argument.(*ast.Ident); isIdent && holders[ident.Name] {
					hands = true
				}
			}
			if !hands {
				return true
			}
			seen[name] = true
			for _, found := range code.queries(function.Body, function.Type.Params, seen, depth+1) {
				own := note(found.Name)
				own.Required, own.Closed = own.Required || found.Required, own.Closed || found.Closed
				for _, value := range found.Values {
					if !slices.Contains(own.Values, value) {
						own.Values = append(own.Values, value)
					}
				}
			}
			return true
		})
	}
	sort.Strings(order)
	fields := make([]field, 0, len(order))
	for _, name := range order {
		sort.Strings(read[name].Values)
		fields = append(fields, *read[name])
	}
	return fields
}

// queryHolder tells an expression that is the request's query: a variable
// that holds it, or X.Query().
func (code source) queryHolder(expression ast.Expr, holders map[string]bool) bool {
	switch expression := expression.(type) {
	case *ast.Ident:
		return holders[expression.Name]
	case *ast.CallExpr:
		selector, isSelector := expression.Fun.(*ast.SelectorExpr)
		return isSelector && selector.Sel.Name == "Query" && len(expression.Args) == 0
	}
	return false
}

// stripped is an expression without a call that takes it alone around it
// (strings.TrimSpace(query.Get("x"))).
func stripped(expression ast.Expr) ast.Expr {
	for {
		call, isCall := expression.(*ast.CallExpr)
		if !isCall || len(call.Args) != 1 {
			return expression
		}
		if selector, isSelector := call.Fun.(*ast.SelectorExpr); isSelector && (selector.Sel.Name == "Get" || selector.Sel.Name == "Has") {
			return expression
		}
		expression = call.Args[0]
	}
}
