package main

import (
	"go/ast"
	"go/token"
	"regexp"
	"strconv"
)

// read takes a file's functions, methods, structs and string constants, and
// the patterns its variables compile.
func (code source) read(file *ast.File) {
	for _, declaration := range file.Decls {
		switch declaration := declaration.(type) {
		case *ast.FuncDecl:
			if declaration.Recv != nil && len(declaration.Recv.List) == 1 {
				if receiver := typeIdent(declaration.Recv.List[0].Type); receiver != "" {
					code.methods[receiver+"."+declaration.Name.Name] = declaration
				}
			}
			// A method a function calls by its name alone is the one no
			// function has; of methods sharing a name, the first that reads
			// a request.
			known, isKnown := code.functions[declaration.Name.Name]
			switch {
			case declaration.Recv == nil, !isKnown:
				code.functions[declaration.Name.Name] = declaration
			case known.Recv != nil && !readsRequest(known) && readsRequest(declaration):
				code.functions[declaration.Name.Name] = declaration
			}
		case *ast.GenDecl:
			for _, spec := range declaration.Specs {
				switch spec := spec.(type) {
				case *ast.TypeSpec:
					if body, isStruct := spec.Type.(*ast.StructType); isStruct {
						code.structs[spec.Name.Name] = body
					}
				case *ast.ValueSpec:
					if declaration.Tok == token.VAR {
						readPatterns(spec)
						readLists(spec)
						readPackageVars(spec)
						continue
					}
					if declaration.Tok != token.CONST {
						continue
					}
					for index, name := range spec.Names {
						if index >= len(spec.Values) {
							continue
						}
						literal, isLiteral := spec.Values[index].(*ast.BasicLit)
						if !isLiteral {
							// One made of others (formPath + "/places/search")
							// is read once they all are (resolveConsts).
							code.composed[name.Name] = spec.Values[index]
							continue
						}
						if literal.Kind != token.STRING {
							continue
						}
						value, err := strconv.Unquote(literal.Value)
						if err != nil {
							continue
						}
						code.consts[name.Name] = value
						if kind, named := spec.Type.(*ast.Ident); named {
							code.typed[kind.Name] = append(code.typed[kind.Name], value)
						}
					}
				}
			}
		}
	}
}

// patterns are the variables holding a pattern written out
// (regexp.MustCompile("^…$")), by name, compiled.
var patterns = map[string]*regexp.Regexp{}

func readPatterns(spec *ast.ValueSpec) {
	for index, name := range spec.Names {
		if index >= len(spec.Values) {
			continue
		}
		call, isCall := spec.Values[index].(*ast.CallExpr)
		if !isCall || len(call.Args) != 1 {
			continue
		}
		selector, isSelector := call.Fun.(*ast.SelectorExpr)
		if !isSelector || selector.Sel.Name != "MustCompile" {
			continue
		}
		literal, isLiteral := call.Args[0].(*ast.BasicLit)
		if !isLiteral || literal.Kind != token.STRING {
			continue
		}
		text, err := strconv.Unquote(literal.Value)
		if err != nil {
			continue
		}
		if compiled, err := regexp.Compile(text); err == nil {
			patterns[name.Name] = compiled
		}
	}
}

// resolveConsts reads the string constants made of others — a path joined
// from apiRoot, another path and words (2026-11-16: the routes registered
// with them, a form's place search among them, went unread and were said to
// take no body) — as far as they are made of constants read.
func (code source) resolveConsts() {
	for progress := true; progress; {
		progress = false
		for name, expression := range code.composed {
			if value, ok := code.routePath(expression); ok {
				code.consts[name] = value
				delete(code.composed, name)
				progress = true
			}
		}
	}
}
