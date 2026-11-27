package main

import (
	"go/ast"
)

// A body is what a request sends, so what a handler reads it into is the
// first variable a call reads the request's body into: BindBody, a decoder
// on the request's body, or json.Unmarshal of bytes read from it — not of a
// stored value read back (2026-11-26: a Notion import's cancel was described
// as taking the job's stored payload, json.Unmarshal([]byte(job.PayloadJSON),
// &payload), its rootKinds among them, though it takes no body).

// packageVars are the package's variables by name, which hold no request.
var packageVars = map[string]bool{}

func readPackageVars(spec *ast.ValueSpec) {
	for _, name := range spec.Names {
		packageVars[name.Name] = true
	}
}

// readsBody tells a call that reads the request's body into a variable.
func (code source) readsBody(call *ast.CallExpr, name string, body *ast.BlockStmt) bool {
	switch name {
	case "BindBody", "bindNotionStrictJSON":
		return true
	case "Decode":
		// json.NewDecoder(event.Request.Body).Decode(&input), or a decoder
		// made so before.
		selector, isSelector := call.Fun.(*ast.SelectorExpr)
		if !isSelector {
			return false
		}
		source := selector.X
		if ident, isIdent := source.(*ast.Ident); isIdent {
			if given := assignedTo(body, ident.Name); given != nil {
				source = given
			}
		}
		if made, isCall := source.(*ast.CallExpr); isCall && calledName(made) == "NewDecoder" && len(made.Args) == 1 {
			return bodyBytes(made.Args[0], body, 0)
		}
		return true
	case "Unmarshal":
		return len(call.Args) == 2 && bodyBytes(call.Args[0], body, 0)
	}
	return false
}

// bodyBytes tells bytes or a reader that come from the request: its
// body, or a variable given it, or a parameter the caller hands it in
// (raw, body) — not a stored record's field (job.PayloadJSON), a package's
// value or bytes decoded from elsewhere (a cursor's).
func bodyBytes(expression ast.Expr, body *ast.BlockStmt, depth int) bool {
	if depth > 3 {
		return true
	}
	switch expression := expression.(type) {
	case *ast.CallExpr:
		// []byte(x), io.LimitReader(x, n), bytes.TrimSpace(x), io.ReadAll(x)
		if len(expression.Args) > 0 {
			return bodyBytes(expression.Args[0], body, depth+1)
		}
		if selector, isSelector := expression.Fun.(*ast.SelectorExpr); isSelector {
			return bodyBytes(selector.X, body, depth+1)
		}
		return false
	case *ast.SelectorExpr:
		// A field of what the function was handed (input.CreateDatabase, a
		// raw part of the body bound before) is the request's; one of a value
		// it got itself (job := load(…); job.PayloadJSON) isn't.
		if mentionsRequest(expression) {
			return true
		}
		root := expression.X
		for {
			inner, isSelector := root.(*ast.SelectorExpr)
			if !isSelector {
				break
			}
			root = inner.X
		}
		ident, isIdent := root.(*ast.Ident)
		return isIdent && assignedTo(body, ident.Name) == nil && !packageVars[ident.Name] && !declaredIn(body, ident.Name)
	case *ast.Ident:
		if given := assignedTo(body, expression.Name); given != nil {
			return bodyBytes(given, body, depth+1)
		}
		return !packageVars[expression.Name]
	case *ast.ParenExpr:
		return bodyBytes(expression.X, body, depth+1)
	case *ast.BasicLit:
		return false
	}
	return true
}

// mentionsRequest tells a field read off the request's body:
// event.Request.Body, r.Body.
func mentionsRequest(selector *ast.SelectorExpr) bool {
	for current := ast.Expr(selector); current != nil; {
		node, isSelector := current.(*ast.SelectorExpr)
		if !isSelector {
			return false
		}
		if node.Sel.Name == "Body" {
			return true
		}
		current = node.X
	}
	return false
}

// assignedTo is what a function's steps give a variable first.
func assignedTo(body *ast.BlockStmt, name string) ast.Expr {
	var given ast.Expr
	ast.Inspect(body, func(node ast.Node) bool {
		assignment, isAssignment := node.(*ast.AssignStmt)
		if !isAssignment || given != nil {
			return given == nil
		}
		for index, left := range assignment.Lhs {
			if ident, isIdent := left.(*ast.Ident); isIdent && ident.Name == name {
				if len(assignment.Rhs) == len(assignment.Lhs) {
					given = assignment.Rhs[index]
				} else if len(assignment.Rhs) == 1 {
					given = assignment.Rhs[0]
				}
			}
		}
		return given == nil
	})
	return given
}

// declaredIn tells a variable a function's steps declare (var stored T).
func declaredIn(body *ast.BlockStmt, name string) bool {
	declared := false
	ast.Inspect(body, func(node ast.Node) bool {
		if spec, isSpec := node.(*ast.ValueSpec); isSpec {
			for _, each := range spec.Names {
				declared = declared || each.Name == name
			}
		}
		if loop, isRange := node.(*ast.RangeStmt); isRange {
			for _, each := range []ast.Expr{loop.Key, loop.Value} {
				if ident, isIdent := each.(*ast.Ident); isIdent && ident.Name == name {
					declared = true
				}
			}
		}
		return !declared
	})
	return declared
}
