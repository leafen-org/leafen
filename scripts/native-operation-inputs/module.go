package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"reflect"
)

// The organization module (organization/, a package of its own) registers
// routes the operation list holds beside the core's, its handlers named as
// the core's are (nativeOperationHandler keeps a function's name without its
// package). It reaches the core only through the names
// internal/app/organization_kernel.go exports (leafen.LegalHoldCreateInput =
// legalHoldCreateInput), so its source is read with each such name put back
// to the core's own, and then as the core's is (2026-11-16: its legal holds,
// exports and policies were described by their whole contract, or as taking
// no body though they read one).

const (
	modulePath = "organization"
	kernelFile = "internal/app/organization_kernel.go"
	// moduleImport is the name the module gives the core.
	moduleImport = "leafen"
)

// kernelNames are the core's names by the name the kernel exports them as:
// a type, a constant or a variable given as another name.
func kernelNames(file *ast.File) map[string]string {
	names := map[string]string{}
	for _, declaration := range file.Decls {
		general, isGeneral := declaration.(*ast.GenDecl)
		if !isGeneral {
			continue
		}
		for _, spec := range general.Specs {
			switch spec := spec.(type) {
			case *ast.TypeSpec:
				if own, named := spec.Type.(*ast.Ident); named {
					names[spec.Name.Name] = own.Name
				}
			case *ast.ValueSpec:
				for index, name := range spec.Names {
					if index < len(spec.Values) {
						if own, named := spec.Values[index].(*ast.Ident); named {
							names[name.Name] = own.Name
						}
					}
				}
			}
		}
	}
	return names
}

var expressionType = reflect.TypeFor[ast.Expr]()

// unqualify puts the core's own name in place of each name the module reads
// through the kernel, everywhere in a file parsed without object resolution
// (whose nodes don't lead back to one another).
func unqualify(node reflect.Value, names map[string]string) {
	switch node.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !node.IsNil() {
			unqualify(node.Elem(), names)
		}
	case reflect.Struct:
		for index := range node.NumField() {
			unqualifyAt(node.Field(index), names)
		}
	case reflect.Slice:
		for index := range node.Len() {
			unqualifyAt(node.Index(index), names)
		}
	}
}

func unqualifyAt(slot reflect.Value, names map[string]string) {
	if slot.Type() == expressionType && slot.CanSet() && !slot.IsNil() {
		if selector, isSelector := slot.Interface().(*ast.SelectorExpr); isSelector {
			if pack, named := selector.X.(*ast.Ident); named && pack.Name == moduleImport {
				if own, known := names[selector.Sel.Name]; known {
					slot.Set(reflect.ValueOf(ast.Expr(&ast.Ident{NamePos: selector.Pos(), Name: own})))
					return
				}
			}
		}
	}
	unqualify(slot, names)
}

// readModule reads a module file as the core's, refusing a function or a
// record named as one of the core's: the operation list names both by their
// name alone, and one would be described as the other.
func (code source) readModule(file *ast.File, name string) {
	for _, declaration := range file.Decls {
		switch declaration := declaration.(type) {
		case *ast.FuncDecl:
			if known, taken := code.functions[declaration.Name.Name]; taken && known.Recv == nil && declaration.Recv == nil {
				fail(fmt.Errorf("%s: %s is also a function of internal/app; the operation list can't tell them apart", name, declaration.Name.Name))
			}
		case *ast.GenDecl:
			if declaration.Tok != token.TYPE {
				continue
			}
			for _, spec := range declaration.Specs {
				if kind, isType := spec.(*ast.TypeSpec); isType {
					if _, taken := code.structs[kind.Name.Name]; taken {
						fail(fmt.Errorf("%s: %s is also a record of internal/app", name, kind.Name.Name))
					}
				}
			}
		}
	}
	code.read(file)
}
