package main

import (
	"go/ast"
	"strings"
)

// The records a function writes from the input's parts, and the lists it
// makes of them (shapes.go): what a field holds there is the field's.

// madeFrom gives a function's parameter the fields of a record written out
// from the input's parts (T{Sections: preset.Sections}), and of the records
// a list it holds was written from ([]tabWire{tab}); the key it is read
// under, or none.
func (scope *shapeScope) madeFrom(literal *ast.CompositeLit, inner *shapeScope, parameter, name string) string {
	key := ""
	for _, element := range literal.Elts {
		pair, isPair := element.(*ast.KeyValueExpr)
		if !isPair {
			continue
		}
		field, isField := pair.Key.(*ast.Ident)
		if !isField {
			continue
		}
		if held, known := scope.subject(pair.Value); known {
			inner.made[parameter+"."+field.Name], key = held, name+"|"+field.Name
		}
		if list, isList := pair.Value.(*ast.CompositeLit); isList {
			for _, entry := range list.Elts {
				if from, isIdent := entry.(*ast.Ident); isIdent && len(scope.madeOf(from.Name)) > 0 {
					inner.madeLists[parameter+"."+field.Name], key = scope.madeOf(from.Name), name+"|"+field.Name
				}
			}
		}
	}
	return key
}

// madeOf are the fields a record written out from the input's parts holds,
// by their names.
func (scope *shapeScope) madeOf(variable string) map[string]shapeSubject {
	fields := map[string]shapeSubject{}
	for path, held := range scope.made {
		if rest, mine := strings.CutPrefix(path, variable+"."); mine {
			fields[rest] = held
		}
	}
	return fields
}

// shapePath is a variable's field written as a path (layout.Tabs).
func shapePath(expression ast.Expr) string {
	selector, isSelector := expression.(*ast.SelectorExpr)
	if !isSelector {
		return ""
	}
	owner, isIdent := selector.X.(*ast.Ident)
	if !isIdent {
		return ""
	}
	return owner.Name + "." + selector.Sel.Name
}

// listHeld notes a list a function makes of the input's records: made for
// them (make([]graphTextSpan, n)), or given one (unmarked[index] = span,
// kept = append(kept, span)).
func (scope *shapeScope) listHeld(assignment *ast.AssignStmt) {
	for index, left := range assignment.Lhs {
		if index >= len(assignment.Rhs) {
			return
		}
		right := assignment.Rhs[index]
		target, entry := "", ast.Expr(nil)
		switch left := left.(type) {
		case *ast.Ident:
			target = left.Name
			if call, isCall := right.(*ast.CallExpr); isCall && len(call.Args) > 0 {
				switch calledName(call) {
				case "make":
					if kind, isList := call.Args[0].(*ast.ArrayType); isList {
						if record, _ := scope.recordNamed(typeIdent(kind.Elt)); record != "" {
							scope.lists[target] = record
						}
					}
				case "append":
					if len(call.Args) > 1 {
						entry = call.Args[1]
					}
				}
			}
		case *ast.IndexExpr:
			if owner, isIdent := left.X.(*ast.Ident); isIdent {
				target, entry = owner.Name, right
			}
		}
		if entry != nil {
			if record := scope.recordOf(entry); record != "" {
				scope.lists[target] = record
			}
		}
	}
}
