package main

import "go/ast"

// A handler two routes share is told which route it answers by a constant
// (mcpTokenSettingsRoute(event, "refresh")) and switches on it: each route's
// body is the one its own branch binds, not the first branch's.

// givenConstants are the constants call passes function, by its parameters'
// names.
func (code source) givenConstants(function *ast.FuncDecl, call *ast.CallExpr) map[string]string {
	names := []string{}
	for _, group := range function.Type.Params.List {
		for _, name := range group.Names {
			names = append(names, name.Name)
		}
	}
	given := map[string]string{}
	for index, argument := range call.Args {
		if index >= len(names) {
			break
		}
		if value, constant := code.constant(argument); constant {
			given[names[index]] = value
		}
	}
	return given
}

// inspectTaken walks node as ast.Inspect does, but goes into a switch on a
// given parameter only through the case its constant takes (the default when
// none does).
func (code source) inspectTaken(node ast.Node, given map[string]string, visit func(ast.Node) bool) {
	ast.Inspect(node, func(current ast.Node) bool {
		choice, isSwitch := current.(*ast.SwitchStmt)
		tag, isIdent := (ast.Expr)(nil), false
		if isSwitch {
			tag = choice.Tag
			_, isIdent = tag.(*ast.Ident)
		}
		if !isIdent {
			return visit(current)
		}
		value, known := given[tag.(*ast.Ident).Name]
		if !known {
			return visit(current)
		}
		if !visit(current) {
			return false
		}
		if choice.Init != nil {
			code.inspectTaken(choice.Init, given, visit)
		}
		if taken := code.takenClause(choice, value); taken != nil {
			for _, statement := range taken.Body {
				code.inspectTaken(statement, given, visit)
			}
		}
		return false
	})
}

// takenClause is the case of choice value takes, or its default, or nil.
func (code source) takenClause(choice *ast.SwitchStmt, value string) *ast.CaseClause {
	var fallback *ast.CaseClause
	for _, statement := range choice.Body.List {
		clause, isClause := statement.(*ast.CaseClause)
		if !isClause {
			continue
		}
		if clause.List == nil {
			fallback = clause
		}
		for _, expression := range clause.List {
			if written, constant := code.constant(expression); constant && written == value {
				return clause
			}
		}
	}
	return fallback
}

// scopedRoutePath reads a route's path as routePath does, and a path kept in
// a variable of the function that registers it (path := apiRoot + "…").
func (code source) scopedRoutePath(expression ast.Expr, scope *ast.FuncDecl) (string, bool) {
	if path, ok := code.routePath(expression); ok {
		return path, true
	}
	switch expression := expression.(type) {
	case *ast.BinaryExpr:
		left, leftOK := code.scopedRoutePath(expression.X, scope)
		right, rightOK := code.scopedRoutePath(expression.Y, scope)
		return left + right, leftOK && rightOK
	case *ast.Ident:
		var value ast.Expr
		ast.Inspect(scope.Body, func(node ast.Node) bool {
			assignment, isAssignment := node.(*ast.AssignStmt)
			if !isAssignment || value != nil || len(assignment.Lhs) != len(assignment.Rhs) {
				return value == nil
			}
			for index, left := range assignment.Lhs {
				if ident, isIdent := left.(*ast.Ident); isIdent && ident.Name == expression.Name {
					value = assignment.Rhs[index]
				}
			}
			return value == nil
		})
		if value != nil {
			return code.routePath(value)
		}
	}
	return "", false
}
