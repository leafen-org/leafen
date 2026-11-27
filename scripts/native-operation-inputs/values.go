package main

import (
	"go/ast"
	"go/token"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// values finds what a function compares a body's fields with: the values its
// checks name, in it and in the functions it hands the body to.
func (code source) values(body *ast.BlockStmt, variable string, fields []field, seen map[string]bool, depth int) {
	add := func(selector ast.Expr, value ast.Expr) {
		name := selectorField(selector, variable)
		text, ok := code.constant(value)
		if name == "" || !ok {
			return
		}
		for index := range fields {
			if strings.EqualFold(fields[index].Name, name) && !slices.Contains(fields[index].Values, text) {
				fields[index].Values = append(fields[index].Values, text)
				sort.Strings(fields[index].Values)
			}
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
				ident, isIdent := argument.(*ast.Ident)
				if !isIdent || ident.Name != variable {
					continue
				}
				if parameter := parameterName(function, index); parameter != "" {
					seen[name] = true
					code.values(function.Body, parameter, fields, seen, depth+1)
				}
			}
		}
		return true
	})
}

func parameterName(function *ast.FuncDecl, index int) string {
	at := 0
	for _, group := range function.Type.Params.List {
		for _, name := range group.Names {
			if at == index {
				return name.Name
			}
			at++
		}
	}
	return ""
}

func selectorField(expression ast.Expr, variable string) string {
	if star, isStar := expression.(*ast.StarExpr); isStar {
		expression = star.X
	}
	selector, isSelector := expression.(*ast.SelectorExpr)
	if !isSelector {
		return ""
	}
	if ident, isIdent := selector.X.(*ast.Ident); !isIdent || ident.Name != variable {
		return ""
	}
	return selector.Sel.Name
}

func (code source) constant(expression ast.Expr) (string, bool) {
	switch expression := expression.(type) {
	case *ast.BasicLit:
		if expression.Kind != token.STRING {
			return "", false
		}
		value, err := strconv.Unquote(expression.Value)
		return value, err == nil && value != ""
	case *ast.Ident:
		value, known := code.consts[expression.Name]
		return value, known && value != ""
	}
	return "", false
}
