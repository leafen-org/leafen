package main

import (
	"go/ast"
	"go/token"
	"slices"
	"sort"
	"strings"
)

// What a record's kind, action or operation field — the field that says what
// a change does — chooses among, wherever the package reads the record: the
// constants a switch on it names or a check compares it with, in any function
// that holds the record — a parameter of its type, a list of it ranged over or
// indexed, or a field holding such a list. A record a body holds inside a
// list (a toolbar change's operations[].kind, a content change's
// operations[].kind) is read so far from where the handler binds the body
// that values.go doesn't reach it (2026-11-24: leafen_operations found
// nothing of "duplicate template", the toolbar change's duplicateTemplate,
// and the describe answer named no kind a change takes). Other fields aren't
// read so: a switch on a property's type names the few types it treats apart,
// not all a property takes.

// choosingFields are the fields that say what a change does.
var choosingFields = map[string]bool{"kind": true, "action": true, "operation": true}

// readChoices finds every record's choosing fields' choices in files.
func (code source) readChoices(files []*ast.File) {
	for _, file := range files {
		for _, declaration := range file.Decls {
			if function, isFunction := declaration.(*ast.FuncDecl); isFunction && function.Body != nil {
				code.choicesIn(function)
			}
		}
	}
	for _, fields := range code.choices {
		for name := range fields {
			sort.Strings(fields[name])
		}
	}
}

// choicesIn reads one function: what record each of its variables holds, and
// the switches and checks on their choosing fields.
func (code source) choicesIn(function *ast.FuncDecl) {
	// records are the variables holding a record, lists those holding a list
	// of one, each by the record's name.
	records, lists := map[string]string{}, map[string]string{}
	hold := func(name string, kind ast.Expr) {
		if record, isList := code.recordOf(kind); record != "" && isList {
			lists[name] = record
		} else if record != "" {
			records[name] = record
		}
	}
	for _, group := range function.Type.Params.List {
		for _, name := range group.Names {
			hold(name.Name, group.Type)
		}
	}
	// listOf is the record a list expression holds: a list variable, or a
	// held record's field holding a list.
	listOf := func(expression ast.Expr) string {
		switch expression := expression.(type) {
		case *ast.Ident:
			return lists[expression.Name]
		case *ast.SelectorExpr:
			if owner, isIdent := expression.X.(*ast.Ident); isIdent && records[owner.Name] != "" {
				if kind := code.fieldType(records[owner.Name], expression.Sel.Name); kind != nil {
					if record, isList := code.recordOf(kind); isList {
						return record
					}
				}
			}
		}
		return ""
	}
	// choose adds a constant compared with a held record's choosing field.
	choose := func(field, value ast.Expr) {
		selector, isSelector := field.(*ast.SelectorExpr)
		if !isSelector || !choosingFields[strings.ToLower(selector.Sel.Name)] {
			return
		}
		owner, isIdent := selector.X.(*ast.Ident)
		if !isIdent || records[owner.Name] == "" || code.fieldType(records[owner.Name], selector.Sel.Name) == nil {
			return
		}
		if text, isConstant := code.constant(value); isConstant {
			code.addChoice(records[owner.Name], selector.Sel.Name, text)
		}
	}
	ast.Inspect(function.Body, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.RangeStmt:
			record := listOf(node.X)
			if record == "" {
				return true
			}
			if value, isIdent := node.Value.(*ast.Ident); isIdent {
				records[value.Name] = record
			}
		case *ast.AssignStmt:
			// operation := &operations[index], or operations[index] itself.
			for index, right := range node.Rhs {
				if pointer, isPointer := right.(*ast.UnaryExpr); isPointer {
					right = pointer.X
				}
				indexed, isIndex := right.(*ast.IndexExpr)
				left, isIdent := node.Lhs[min(index, len(node.Lhs)-1)].(*ast.Ident)
				if isIndex && isIdent && len(node.Lhs) == len(node.Rhs) {
					if record := listOf(indexed.X); record != "" {
						records[left.Name] = record
					}
				}
			}
		case *ast.SwitchStmt:
			for _, statement := range node.Body.List {
				if clause, isClause := statement.(*ast.CaseClause); isClause && node.Tag != nil {
					for _, value := range clause.List {
						choose(node.Tag, value)
					}
				}
			}
		case *ast.BinaryExpr:
			if node.Op == token.EQL || node.Op == token.NEQ {
				choose(node.X, node.Y)
				choose(node.Y, node.X)
			}
		}
		return true
	})
}

// recordOf is the record a type names, and whether it is a list of it.
func (code source) recordOf(kind ast.Expr) (string, bool) {
	list := false
	if array, isArray := kind.(*ast.ArrayType); isArray {
		kind, list = array.Elt, true
	}
	name := typeIdent(kind)
	if code.structs[name] == nil {
		return "", false
	}
	return name, list
}

// fieldType is the type of a record's field by its Go name, or nil.
func (code source) fieldType(record, field string) ast.Expr {
	for _, item := range code.structs[record].Fields.List {
		for _, name := range item.Names {
			if name.Name == field {
				return item.Type
			}
		}
	}
	return nil
}

func (code source) addChoice(record, field, value string) {
	if code.choices[record] == nil {
		code.choices[record] = map[string][]string{}
	}
	if !slices.Contains(code.choices[record][field], value) {
		code.choices[record][field] = append(code.choices[record][field], value)
	}
}
