package main

import (
	"go/ast"
	"slices"
	"strings"
)

// routeMethods are the methods a route is registered with.
var routeMethods = []string{"GET", "POST", "PATCH", "PUT", "DELETE"}

// routeCall reads a route's registration in scope: Router.POST(path, handler)
// by the method's name, and Router.Route(method, path, handler) with the
// method a constant (commentCreateMethod, http.MethodPost), as a contract's
// routes are registered — the operations those registered were described by
// their whole contract, an agent's description of a comment's call 12 KB
// (2026-11-16). It gives the method, the path and the handler's expression.
func (code source) routeCall(node ast.Node, scope *ast.FuncDecl) (string, string, ast.Expr, bool) {
	call, isCall := node.(*ast.CallExpr)
	if !isCall {
		return "", "", nil, false
	}
	selector, isSelector := call.Fun.(*ast.SelectorExpr)
	if !isSelector {
		return "", "", nil, false
	}
	method, arguments := selector.Sel.Name, call.Args
	if method == "Route" {
		if len(arguments) < 3 {
			return "", "", nil, false
		}
		method, arguments = code.routeMethod(arguments[0]), arguments[1:]
	}
	if !slices.Contains(routeMethods, method) || len(arguments) < 2 {
		return "", "", nil, false
	}
	path, isRoute := code.scopedRoutePath(arguments[0], scope)
	if !isRoute {
		return "", "", nil, false
	}
	return method, path, arguments[len(arguments)-1], true
}

// routeMethod is the method a Route call names: a constant written out or
// named, or net/http's (http.MethodPost).
func (code source) routeMethod(expression ast.Expr) string {
	if selector, isSelector := expression.(*ast.SelectorExpr); isSelector {
		if pack, named := selector.X.(*ast.Ident); named && pack.Name == "http" {
			return strings.ToUpper(strings.TrimPrefix(selector.Sel.Name, "Method"))
		}
		return ""
	}
	method, constant := code.constant(expression)
	if !constant {
		return ""
	}
	return method
}

// eachRoute visits every route the files register: one written out
// (routeCall), and a table's once a row (route_tables.go).
func (code source) eachRoute(files []*ast.File, visit func(method, path string, handler ast.Expr, scope *ast.FuncDecl)) {
	for _, file := range files {
		for _, declaration := range file.Decls {
			scope, isFunction := declaration.(*ast.FuncDecl)
			if !isFunction || scope.Body == nil {
				continue
			}
			ast.Inspect(scope.Body, func(node ast.Node) bool {
				if loop, isLoop := node.(*ast.RangeStmt); isLoop {
					if routes := code.tableRoutes(loop, scope); len(routes) > 0 {
						for _, route := range routes {
							visit(route.method, route.path, route.handler, scope)
						}
						return false
					}
				}
				if method, path, handler, isRoute := code.routeCall(node, scope); isRoute {
					visit(method, path, handler, scope)
				}
				return true
			})
		}
	}
}
