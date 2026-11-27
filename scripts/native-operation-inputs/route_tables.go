package main

import (
	"go/ast"
	"go/token"
)

// A route registered for each row of a table written in place —
// for _, route := range []struct{ method, path string; resolve bool }{…} —
// takes its method, path and handler from the row: the loop's body is read
// once a row, its row's fields in place of route.method and the like, an if
// on a row's field taking the branch the row's value takes, and a variable
// holding the handler (handle := func…; if route.resolve { handle = … };
// handle = wrapper(handle, handle)) read as it stands at the registration
// (2026-11-16: a page's suggestions, registered so, were described by the
// comments contract's 37 records, 11.5 KB).

// tableRoute is one route a table registers.
type tableRoute struct {
	method, path string
	handler      ast.Expr
}

// tableRoutes are the routes a range over a table written in place
// registers, one or more a row.
func (code source) tableRoutes(loop *ast.RangeStmt, scope *ast.FuncDecl) []tableRoute {
	table, isTable := loop.X.(*ast.CompositeLit)
	rowName, named := loop.Value.(*ast.Ident)
	if !isTable || !named {
		return nil
	}
	list, isList := table.Type.(*ast.ArrayType)
	if !isList {
		return nil
	}
	shape, isStruct := list.Elt.(*ast.StructType)
	if !isStruct {
		return nil
	}
	fields := []string{}
	for _, group := range shape.Fields.List {
		for _, name := range group.Names {
			fields = append(fields, name.Name)
		}
	}
	routes := []tableRoute{}
	for _, element := range table.Elts {
		literal, isLiteral := element.(*ast.CompositeLit)
		if !isLiteral {
			continue
		}
		row := map[string]ast.Expr{}
		for index, value := range literal.Elts {
			if keyed, isKeyed := value.(*ast.KeyValueExpr); isKeyed {
				if key, isKey := keyed.Key.(*ast.Ident); isKey {
					row[key.Name] = keyed.Value
				}
			} else if index < len(fields) {
				row[fields[index]] = value
			}
		}
		reading := tableRow{code: code, row: rowName.Name, fields: row, variables: map[string]ast.Expr{}, scope: scope}
		reading.statements(loop.Body.List, &routes)
	}
	return routes
}

// tableRow reads a loop's body for one row.
type tableRow struct {
	code      source
	row       string
	fields    map[string]ast.Expr
	variables map[string]ast.Expr
	scope     *ast.FuncDecl
}

func (reading tableRow) statements(list []ast.Stmt, routes *[]tableRoute) {
	for _, statement := range list {
		switch statement := statement.(type) {
		case *ast.AssignStmt:
			for index, left := range statement.Lhs {
				if variable, isVariable := left.(*ast.Ident); isVariable && index < len(statement.Rhs) && len(statement.Lhs) == len(statement.Rhs) {
					reading.variables[variable.Name] = reading.value(statement.Rhs[index])
				}
			}
		case *ast.IfStmt:
			if taken, known := reading.condition(statement.Cond); known && taken {
				reading.statements(statement.Body.List, routes)
			} else if known && statement.Else != nil {
				if block, isBlock := statement.Else.(*ast.BlockStmt); isBlock {
					reading.statements(block.List, routes)
				}
			}
		default:
			ast.Inspect(statement, func(node ast.Node) bool {
				call, isCall := node.(*ast.CallExpr)
				if !isCall {
					return true
				}
				method, path, handler, isRoute := reading.code.routeCall(reading.value(call), reading.scope)
				if isRoute {
					*routes = append(*routes, tableRoute{method, path, handler})
					return false
				}
				return true
			})
		}
	}
}

// value is an expression with the row's fields and the variables read so far
// in place; a closure is kept as written.
func (reading tableRow) value(expression ast.Expr) ast.Expr {
	switch expression := expression.(type) {
	case *ast.SelectorExpr:
		if row, isRow := expression.X.(*ast.Ident); isRow && row.Name == reading.row {
			if value, known := reading.fields[expression.Sel.Name]; known {
				return value
			}
		}
		return &ast.SelectorExpr{X: reading.value(expression.X), Sel: expression.Sel}
	case *ast.Ident:
		if value, known := reading.variables[expression.Name]; known {
			return value
		}
	case *ast.BinaryExpr:
		return &ast.BinaryExpr{X: reading.value(expression.X), OpPos: expression.OpPos, Op: expression.Op, Y: reading.value(expression.Y)}
	case *ast.CallExpr:
		arguments := make([]ast.Expr, len(expression.Args))
		for index, argument := range expression.Args {
			arguments[index] = reading.value(argument)
		}
		return &ast.CallExpr{Fun: reading.value(expression.Fun), Lparen: expression.Lparen, Args: arguments, Rparen: expression.Rparen}
	}
	return expression
}

// condition is what an if on the row's fields takes: a field written true or
// false, or its negation.
func (reading tableRow) condition(expression ast.Expr) (bool, bool) {
	switch expression := expression.(type) {
	case *ast.UnaryExpr:
		if expression.Op == token.NOT {
			taken, known := reading.condition(expression.X)
			return !taken, known
		}
	case *ast.ParenExpr:
		return reading.condition(expression.X)
	case *ast.Ident:
		switch expression.Name {
		case "true":
			return true, true
		case "false":
			return false, true
		}
	case *ast.SelectorExpr:
		if row, isRow := expression.X.(*ast.Ident); isRow && row.Name == reading.row {
			if value, known := reading.fields[expression.Sel.Name]; known {
				return reading.condition(value)
			}
		}
	}
	return false, false
}
