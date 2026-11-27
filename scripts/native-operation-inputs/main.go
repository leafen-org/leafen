// Command native-operation-inputs writes what each native API route without a
// wire contract reads from its request body, from the Go source of its handler:
// the struct it binds, with the JSON names and types of its fields and the
// values its handler compares them with, each record named as its wire
// contract names it (wire_names.go). The native MCP's operation list reads
// the result, so no operation an agent can run hides what it takes.
//
// It reads the files in their names' order, so the same source gives the same
// result; --check fails when the file it writes is not that result, as the web
// build's checks run it.
//
//	go run ./scripts/native-operation-inputs [--check]
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
)

const target = "internal/app/native_operation_inputs.generated.json"

// rawRecords names what a field read later as raw JSON holds.
var rawRecords = map[string]string{"actionDocument": "interactiveAutomationDocument"}

// stringValues names the constants a plain string field takes.
var stringValues = map[string][]string{
	"interactiveAutomationAction.type":        {"interactiveAutomationAction*"},
	"databaseAutomationScope.type":            {"databaseAutomationScope*"},
	"databaseAutomationTrigger.type":          {"databaseAutomationTriggerEvents", "databaseAutomationTriggerSchedule"},
	"databaseAutomationTriggerCondition.type": {"databaseAutomationTriggerRowAdded", "databaseAutomationTriggerPropertyEdited"},
}

type source struct {
	structs   map[string]*ast.StructType
	consts    map[string]string
	typed     map[string][]string
	functions map[string]*ast.FuncDecl
	// methods are every method by its receiver's type and its name
	// (workspaceMoveOut.start), as the operation list names a method handler
	// (nativeOperationHandler in internal/app/native_operations.go).
	methods map[string]*ast.FuncDecl
	// composed are the constants made of others, until resolveConsts reads them.
	composed map[string]ast.Expr
	// choices are what a record's field chooses among, by record and field (choices.go).
	choices map[string]map[string][]string
}

func main() {
	fileSet := token.NewFileSet()
	sources := func(info os.FileInfo) bool { return !strings.HasSuffix(info.Name(), "_test.go") }
	packages, err := parser.ParseDir(fileSet, "internal/app", sources, 0)
	if err != nil {
		fail(err)
	}
	// The organization module, read as the core (module.go).
	modules, err := parser.ParseDir(fileSet, modulePath, sources, parser.SkipObjectResolution)
	if err != nil {
		fail(err)
	}
	code := source{map[string]*ast.StructType{}, map[string]string{}, map[string][]string{}, map[string]*ast.FuncDecl{}, map[string]*ast.FuncDecl{},
		map[string]ast.Expr{}, map[string]map[string][]string{}}
	names := []string{}
	parsed := map[string]*ast.File{}
	for _, pack := range packages {
		for name, file := range pack.Files {
			names = append(names, name)
			parsed[name] = file
		}
	}
	sort.Strings(names)
	files := make([]*ast.File, 0, len(names))
	for _, name := range names {
		files = append(files, parsed[name])
	}
	for _, file := range files {
		code.read(file)
	}
	moduleNames := []string{}
	for _, pack := range modules {
		for name, file := range pack.Files {
			moduleNames = append(moduleNames, name)
			parsed[name] = file
		}
	}
	sort.Strings(moduleNames)
	kernel := kernelNames(parsed[kernelFile])
	for _, name := range moduleNames {
		unqualify(reflect.ValueOf(parsed[name]), kernel)
		code.readModule(parsed[name], name)
		files = append(files, parsed[name])
	}
	code.resolveConsts()
	code.readChoices(files)
	inputs := map[string]input{}
	// A function by its name, a method by its receiver's type and its name.
	for name, function := range code.functions {
		if function.Recv != nil || function.Body == nil || function.Type.Params == nil || !takesEvent(function) {
			continue
		}
		if found, ok := code.bound(function.Body, function.Type.Params, map[string]bool{name: true}, 0, nil); ok {
			inputs[name] = found
		}
	}
	for name, method := range code.methods {
		if method.Body == nil || method.Type.Params == nil || !takesEvent(method) {
			continue
		}
		own := name[strings.LastIndex(name, ".")+1:]
		if found, ok := code.bound(method.Body, method.Type.Params, map[string]bool{own: true}, 0, nil); ok {
			inputs[name] = found
		}
	}
	code.eachRoute(files, func(method, path string, handler ast.Expr, scope *ast.FuncDecl) {
		if method == "GET" {
			return
		}
		// A closure, or a handler in a wrapper, is named for its route.
		name, body := code.handler(handler, method, path, scope)
		if body == nil {
			return
		}
		if found, ok := code.bound(body, nil, map[string]bool{}, 0, nil); ok {
			inputs[closureName(method, path)] = found
			return
		}
		// One that reads no body is written as taking nothing (takes_nothing.go).
		if _, known := inputs[name]; !known && name != "" && code.takesNothing(body, map[string]bool{}, 0) {
			inputs[name] = input{}
		}
	})
	code.addQueries(inputs, files)
	code.addAnswers(inputs, files)
	// Records by their contracts' names (wire_names.go).
	contracts, err := readContractNames()
	if err != nil {
		fail(err)
	}
	contracts.nameRecords(inputs)
	if err := contracts.checkRecordNames(inputs); err != nil {
		fail(err)
	}
	encoded, err := json.MarshalIndent(inputs, "", " ")
	if err != nil {
		fail(err)
	}
	output := append(encoded, '\n')
	if slices.Contains(os.Args[1:], "--check") {
		if written, err := os.ReadFile(target); err != nil || !bytes.Equal(written, output) {
			fail(fmt.Errorf("stale generated output: %s (go run ./scripts/native-operation-inputs)", target))
		}
		return
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		fail(err)
	}
	if err := os.WriteFile(target, output, 0o644); err != nil {
		fail(err)
	}
}

// readsRequest tells a function with a body that reads a request.
func readsRequest(function *ast.FuncDecl) bool {
	return function.Body != nil && function.Type.Params != nil && takesEvent(function)
}

// takesEvent tells a function that reads a request.
func takesEvent(function *ast.FuncDecl) bool {
	for _, group := range function.Type.Params.List {
		if star, isStar := group.Type.(*ast.StarExpr); isStar {
			if selector, isSelector := star.X.(*ast.SelectorExpr); isSelector && selector.Sel.Name == "RequestEvent" {
				return true
			}
		}
	}
	return false
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

// routePath reads a route's path where it is written out: apiRoot and text.
func (code source) routePath(expression ast.Expr) (string, bool) {
	switch expression := expression.(type) {
	case *ast.BinaryExpr:
		left, leftOK := code.routePath(expression.X)
		right, rightOK := code.routePath(expression.Y)
		return left + right, leftOK && rightOK
	case *ast.BasicLit:
		value, err := strconv.Unquote(expression.Value)
		return value, err == nil
	case *ast.Ident:
		if expression.Name == "apiRoot" {
			return "", true
		}
		value, known := code.consts[expression.Name]
		return value, known
	}
	return "", false
}

// handler finds a route's function, registered in scope: its name as the
// operation list names it, and its body. A closure is named for its route,
// as the list names it, and a method by its receiver's type
// (workspaceMoveOut.start), read from the variable it is called on.
func (code source) handler(expression ast.Expr, method, path string, scope *ast.FuncDecl) (string, *ast.BlockStmt) {
	switch expression := expression.(type) {
	case *ast.FuncLit:
		return closureName(method, path), expression.Body
	case *ast.Ident:
		// A variable (ygo) is no handler, though a method has its name.
		if function := code.functions[expression.Name]; function != nil && function.Recv == nil && readsRequest(function) {
			return expression.Name, function.Body
		}
	case *ast.SelectorExpr:
		if variable, named := expression.X.(*ast.Ident); named {
			name := code.variableType(scope, variable.Name) + "." + expression.Sel.Name
			if function := code.methods[name]; function != nil {
				return name, function.Body
			}
		}
		if function := code.functions[expression.Sel.Name]; function != nil {
			return expression.Sel.Name, function.Body
		}
	case *ast.CallExpr:
		// A wrapper takes the handler last; a maker returns it.
		if len(expression.Args) > 0 {
			if name, body := code.handler(expression.Args[len(expression.Args)-1], method, path, scope); body != nil {
				return name, body
			}
		}
		if maker, named := expression.Fun.(*ast.Ident); named && code.functions[maker.Name] != nil {
			return maker.Name, code.functions[maker.Name].Body
		}
	}
	return "", nil
}

// variableType is the type a variable of scope holds: a parameter's or the
// receiver's, or what it is first given — a value written out or one a
// function returns.
func (code source) variableType(scope *ast.FuncDecl, variable string) string {
	for _, fields := range []*ast.FieldList{scope.Recv, scope.Type.Params} {
		if fields == nil {
			continue
		}
		for _, field := range fields.List {
			for _, name := range field.Names {
				if name.Name == variable {
					return typeIdent(field.Type)
				}
			}
		}
	}
	kind := ""
	ast.Inspect(scope.Body, func(node ast.Node) bool {
		assignment, ok := node.(*ast.AssignStmt)
		if !ok || kind != "" {
			return kind == ""
		}
		for index, left := range assignment.Lhs {
			if ident, isIdent := left.(*ast.Ident); !isIdent || ident.Name != variable || index >= len(assignment.Rhs) {
				continue
			}
			value := assignment.Rhs[index]
			if pointer, isPointer := value.(*ast.UnaryExpr); isPointer && pointer.Op == token.AND {
				value = pointer.X
			}
			switch value := value.(type) {
			case *ast.CompositeLit:
				kind = typeIdent(value.Type)
			case *ast.CallExpr:
				if maker, isIdent := value.Fun.(*ast.Ident); isIdent {
					if function := code.functions[maker.Name]; function != nil && function.Recv == nil &&
						function.Type.Results != nil && len(function.Type.Results.List) > 0 {
						kind = typeIdent(function.Type.Results.List[0].Type)
					}
				}
			}
		}
		return kind == ""
	})
	return kind
}

// typeIdent is the name of a type written as a name or a pointer to one.
func typeIdent(expression ast.Expr) string {
	if star, isStar := expression.(*ast.StarExpr); isStar {
		expression = star.X
	}
	if ident, isIdent := expression.(*ast.Ident); isIdent {
		return ident.Name
	}
	return ""
}

func closureName(method, path string) string {
	var words strings.Builder
	words.WriteString(strings.ToLower(method))
	for _, segment := range strings.Split(path, "/") {
		if segment == "" || strings.HasPrefix(segment, "{") {
			continue
		}
		for _, word := range strings.FieldsFunc(segment, func(r rune) bool { return r == '-' || r == '_' || r == '.' }) {
			words.WriteString(strings.ToUpper(word[:1]) + word[1:])
		}
	}
	return words.String()
}

// boundVariable is the first variable a body's JSON is read into, in the
// branches a switch on a constant the caller gave takes (inspectTaken).
func (code source) boundVariable(body *ast.BlockStmt, given map[string]string) string {
	var variable string
	code.inspectTaken(body, given, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || variable != "" {
			return variable == ""
		}
		name := ""
		switch fun := call.Fun.(type) {
		case *ast.SelectorExpr:
			name = fun.Sel.Name
		case *ast.Ident:
			name = fun.Name
		}
		if !code.readsBody(call, name, body) { // body_reads.go
			return true
		}
		for _, argument := range call.Args {
			if pointer, isPointer := argument.(*ast.UnaryExpr); isPointer && pointer.Op == token.AND {
				if ident, isIdent := pointer.X.(*ast.Ident); isIdent {
					variable = ident.Name
				}
			}
		}
		return true
	})
	return variable
}

// boundParameter is a parameter, a pointer to a struct, that a body's JSON is
// read into, as a helper a handler hands the struct to binds it, with its
// struct's name.
func (code source) boundParameter(body *ast.BlockStmt, params *ast.FieldList) (string, string) {
	if params == nil {
		return "", ""
	}
	pointers := map[string]string{}
	for _, field := range params.List {
		if _, isPointer := field.Type.(*ast.StarExpr); !isPointer {
			continue
		}
		if _, known := code.structs[typeIdent(field.Type)]; !known {
			continue
		}
		for _, name := range field.Names {
			pointers[name.Name] = typeIdent(field.Type)
		}
	}
	variable := ""
	ast.Inspect(body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || variable != "" {
			return variable == ""
		}
		name := ""
		switch fun := call.Fun.(type) {
		case *ast.SelectorExpr:
			name = fun.Sel.Name
		case *ast.Ident:
			name = fun.Name
		}
		if !code.readsBody(call, name, body) {
			return true
		}
		for _, argument := range call.Args {
			if ident, isIdent := argument.(*ast.Ident); isIdent && pointers[ident.Name] != "" {
				variable = ident.Name
			}
		}
		return true
	})
	return variable, pointers[variable]
}

// dispatched adds the records a handler reads the body into again by a field
// of what it read first, each named for the field's value.
func (code source) dispatched(body *ast.BlockStmt, variable string, found *input) {
	ast.Inspect(body, func(node ast.Node) bool {
		choice, ok := node.(*ast.SwitchStmt)
		if !ok || selectorField(choice.Tag, variable) == "" {
			return true
		}
		for _, statement := range choice.Body.List {
			clause, isClause := statement.(*ast.CaseClause)
			if !isClause {
				continue
			}
			block := &ast.BlockStmt{List: clause.Body}
			kind, _ := declaredType(block, code.boundVariable(block, nil))
			if _, known := code.structs[kind]; !known {
				continue
			}
			for _, value := range clause.List {
				if action, isConstant := code.constant(value); isConstant {
					if found.Actions == nil {
						found.Actions = map[string]string{}
					}
					found.Actions[action] = kind
					code.addRecord(found.Records, kind)
				}
			}
		}
		return true
	})
}

// bound finds the struct a handler binds its body to, following the functions
// it hands its request to; given are the constants its caller passed it by
// parameter name, so a handler two routes share is read for each route.
func (code source) bound(body *ast.BlockStmt, params *ast.FieldList, seen map[string]bool, depth int, given map[string]string) (input, bool) {
	variable := code.boundVariable(body, given)
	kind := ""
	if variable == "" {
		// A helper handed the struct to read into binds its parameter.
		variable, kind = code.boundParameter(body, params)
	}
	if variable != "" {
		declared, inline := declaredType(body, variable)
		if declared != "" || inline != nil {
			kind = declared
		}
		if inline != nil {
			kind = "body"
			code.structs[kind] = inline
			defer delete(code.structs, kind)
		}
		if _, known := code.structs[kind]; known {
			found := input{Record: kind, Records: map[string][]field{}}
			code.addRecord(found.Records, kind)
			code.values(body, variable, found.Records[kind], map[string]bool{}, 0)
			code.required(body, variable, found.Records[kind], map[string]bool{}, 0, false)
			code.shapes(body, variable, found.Records, kind)
			code.dispatched(body, variable, &found)
			return found, true
		}
		return input{}, false
	}
	if depth >= 3 {
		return input{}, false
	}
	found, ok := input{}, false
	code.inspectTaken(body, given, func(node ast.Node) bool {
		if ok {
			return false
		}
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
		if function == nil || function.Body == nil || seen[name] || !mentionsEvent(call) {
			return true
		}
		seen[name] = true
		found, ok = code.bound(function.Body, function.Type.Params, seen, depth+1, code.givenConstants(function, call))
		return !ok
	})
	return found, ok
}

func mentionsEvent(call *ast.CallExpr) bool {
	for _, argument := range call.Args {
		if ident, isIdent := argument.(*ast.Ident); isIdent && (ident.Name == "event" || ident.Name == "request" || ident.Name == "e") {
			return true
		}
	}
	return false
}
